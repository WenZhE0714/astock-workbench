package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

type acceptance struct {
	CheckedAt        time.Time           `json:"checked_at"`
	Status           string              `json:"status"`
	Detail           string              `json:"detail"`
	Archive          string              `json:"archive"`
	PlanFingerprint  string              `json:"plan_fingerprint"`
	FirstDate        string              `json:"first_date,omitempty"`
	FirstFingerprint string              `json:"first_fingerprint,omitempty"`
	SealedDays       int                 `json:"sealed_days"`
	PendingOrders    []int               `json:"pending_orders,omitempty"`
	PromotionAllowed bool                `json:"promotion_allowed"`
	Execution        executionAcceptance `json:"execution"`
}

func verifyFirstDay(plan backtest.VolumeForwardPlan, day backtest.VolumeForwardDay) error {
	if day.PreviousHash != "" || len(day.Runs) != 4 || day.PromotionAllowed || day.SampleReady {
		return fmt.Errorf("首日封存链、账户数量或晋级状态异常")
	}
	date, err := time.ParseInLocation(time.DateOnly, day.Date, shanghai)
	if err != nil || day.Date < plan.Baseline.Start.In(shanghai).Format(time.DateOnly) {
		return fmt.Errorf("首日日期早于冻结起点或格式错误")
	}
	captured := day.CapturedAt.In(shanghai)
	if captured.Format(time.DateOnly) != day.Date || captured.Hour()*60+captured.Minute() < 15*60+30 || captured.Hour() >= 19 {
		return fmt.Errorf("首日采集时间不在当日15:30至19:00内")
	}
	symbols := append([]string{plan.Baseline.Benchmark}, plan.Baseline.Tickers...)
	if len(day.Bars) != len(symbols) {
		return fmt.Errorf("首日股票池与基准数据不完整")
	}
	for _, symbol := range symbols {
		bars := day.Bars[symbol]
		if len(bars) < plan.Baseline.Technical.SlowMA+2 || bars[len(bars)-1].Date != day.Date {
			return fmt.Errorf("%s 首日日K或预热数据不足", symbol)
		}
	}
	for i, run := range day.Runs {
		expected := plan.Baseline
		if i%2 == 1 {
			expected = plan.Candidate
		}
		expected.End = date
		expected.Tickers = slices.Clone(expected.Tickers)
		slices.Sort(expected.Tickers)
		if i >= 2 {
			expected.CommissionRate *= 2
			expected.MinimumCommission *= 2
			expected.StampDutyRate *= 2
			expected.TransferFeeRate *= 2
			expected.SlippageBPS *= 2
		}
		want, err := backtest.ForwardFingerprint(expected)
		if err != nil {
			return err
		}
		got, err := backtest.ForwardFingerprint(run.Request)
		if err != nil || got != want {
			return fmt.Errorf("账户%d的规则或成本与冻结配置不一致", i)
		}
		if len(run.Trades) != 0 || len(run.OpenPositions) != 0 || len(run.Equity) != 1 || run.Metrics.Trades != 0 || run.Metrics.TotalFees != 0 || run.Metrics.FinalEquity != expected.InitialCash || run.Metrics.TotalReturn != 0 || run.Metrics.MaxDrawdown != 0 || run.Metrics.Turnover != 0 {
			return fmt.Errorf("账户%d首日出现成交、持仓、费用或资金变化", i)
		}
		point := run.Equity[0]
		if point.Date != day.Date || point.Cash != expected.InitialCash || point.Equity != expected.InitialCash || point.Positions != 0 || point.Drawdown != 0 {
			return fmt.Errorf("账户%d首日净值不等于初始现金", i)
		}
		seen := map[string]bool{}
		for _, order := range run.PendingOrders {
			if order.Side != "buy" || order.Signal.Date != day.Date || !slices.Contains(expected.Tickers, order.Symbol) || seen[order.Symbol] {
				return fmt.Errorf("账户%d首日待执行信号的日期、方向或股票异常", i)
			}
			seen[order.Symbol] = true
		}
	}
	for _, base := range []int{0, 2} {
		baseline, candidate := day.Runs[base], day.Runs[base+1]
		var wanted []backtest.ForwardPendingOrder
		var blocked []backtest.EntryVolumeBlock
		for _, order := range baseline.PendingOrders {
			ratio := order.Signal.VolumeRatio
			if ratio >= plan.Candidate.EntryVolumeFloor {
				wanted = append(wanted, order)
			} else {
				blocked = append(blocked, backtest.EntryVolumeBlock{Symbol: order.Symbol, SignalDate: day.Date, VolumeRatio: ratio, Minimum: plan.Candidate.EntryVolumeFloor})
			}
		}
		if !slices.EqualFunc(wanted, candidate.PendingOrders, func(a, b backtest.ForwardPendingOrder) bool { return reflect.DeepEqual(a, b) }) || !slices.Equal(blocked, candidate.VolumeBlockedEntries) || len(baseline.VolumeBlockedEntries) != 0 {
			return fmt.Errorf("首日量比过滤结果与原规则信号不一致")
		}
	}
	for i := 0; i < 2; i++ {
		if !slices.EqualFunc(day.Runs[i].PendingOrders, day.Runs[i+2].PendingOrders, func(a, b backtest.ForwardPendingOrder) bool { return reflect.DeepEqual(a, b) }) {
			return fmt.Errorf("首日双倍成本组改变了入场信号")
		}
	}
	return nil
}

func inspect(archive, binary, fingerprint string, now time.Time) (acceptance, error) {
	result := acceptance{CheckedAt: now.In(shanghai), Archive: archive, PlanFingerprint: fingerprint, Status: "waiting", Execution: newExecutionAcceptance()}
	plan, latest, count, err := storage.NewVolumeForwardStore(archive).Load()
	if err != nil {
		return result, err
	}
	if plan.Fingerprint != fingerprint {
		return result, fmt.Errorf("观察配置已变化，需要重新指定验收对象")
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != plan.ExecutableSHA256 {
		return result, fmt.Errorf("采集程序与冻结指纹不一致")
	}
	result.SealedDays = count
	statusBytes, err := os.ReadFile(filepath.Join(archive, "status.json"))
	if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	if err == nil {
		var status struct{ Status, Detail string }
		if err := json.Unmarshal(statusBytes, &status); err != nil {
			return result, err
		}
		if status.Status == "error" {
			result.Execution.Status, result.Execution.Detail = "paused", "采集任务报错，暂停成交验收"
			return result, fmt.Errorf("采集任务报错: %s", status.Detail)
		}
	}
	if count == 0 {
		result.Detail = "配置、源研究和采集程序校验通过，等待首份真实日档"
		local := now.In(shanghai)
		start := plan.Baseline.Start.In(shanghai).Format(time.DateOnly)
		if local.Format(time.DateOnly) > start || local.Format(time.DateOnly) == start && local.Hour() >= 19 {
			result.Status = "attention"
			result.Detail = "起始日期采集窗口已过，仍无首日日档；请核对交易日安排和采集日志"
		}
		return result, nil
	}
	entries, err := os.ReadDir(filepath.Join(archive, "days"))
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var previous *backtest.VolumeForwardDay
	processed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if processed == count {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		data, err := os.ReadFile(filepath.Join(archive, "days", entry.Name()))
		if err != nil {
			return result, err
		}
		var day backtest.VolumeForwardDay
		if err := json.Unmarshal(data, &day); err != nil {
			return result, err
		}
		if err := backtest.ValidateVolumeForwardDay(plan, day); err != nil {
			return result, err
		}
		if day.CapturedAt.After(now) {
			return result, fmt.Errorf("日档采集时间晚于验收时间")
		}
		if previous == nil {
			if err := verifyFirstDay(plan, day); err != nil {
				return result, err
			}
			result.Status, result.Detail = "passed", "首日四组账户验收通过；成交验收进度见各组记录，不代表策略效果通过"
			result.FirstDate, result.FirstFingerprint = day.Date, day.Fingerprint
			result.Execution.Status, result.Execution.Detail = "waiting-next-day", "首日仅记录信号，等待后续日档验证执行"
			for _, run := range day.Runs {
				result.PendingOrders = append(result.PendingOrders, len(run.PendingOrders))
			}
		} else {
			changes, err := verifyExecutionDay(ctx, plan, *previous, day)
			if err != nil {
				result.Execution.Status, result.Execution.Detail = "error", err.Error()
				return result, err
			}
			result.Execution.record(day.Date, changes)
		}
		previous = &day
		processed++
	}
	if previous == nil || latest == nil || processed != count || previous.Fingerprint != latest.Fingerprint {
		return result, fmt.Errorf("验收期间归档快照发生变化，请重试")
	}
	return result, nil
}

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".acceptance-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func execute() error {
	archive := flag.String("archive", "", "frozen forward archive")
	binary := flag.String("binary", "", "frozen collector executable")
	output := flag.String("output", "", "separate acceptance output directory")
	fingerprint := flag.String("fingerprint", "", "expected frozen plan fingerprint")
	flag.Parse()
	if *archive == "" || *binary == "" || *output == "" || *fingerprint == "" || flag.NArg() != 0 {
		return fmt.Errorf("required: --archive --binary --output --fingerprint")
	}
	archivePath, err := filepath.Abs(*archive)
	if err != nil {
		return err
	}
	archivePath, err = filepath.EvalSymlinks(archivePath)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(outputPath); os.IsNotExist(err) {
		parent, err := filepath.EvalSymlinks(filepath.Dir(outputPath))
		if err != nil {
			return err
		}
		outputPath = filepath.Join(parent, filepath.Base(outputPath))
	} else {
		outputPath, err = filepath.EvalSymlinks(outputPath)
		if err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(archivePath, outputPath)
	if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("验收输出必须位于前瞻归档之外")
	}
	if err := os.MkdirAll(outputPath, 0o700); err != nil {
		return err
	}
	result, inspectErr := inspect(archivePath, *binary, *fingerprint, time.Now())
	if inspectErr != nil {
		result.Status, result.Detail = "error", inspectErr.Error()
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(outputPath, "status.json"), data); err != nil {
		return err
	}
	firstDate, firstHash, orders := result.FirstDate, result.FirstFingerprint, fmt.Sprint(result.PendingOrders)
	if firstDate == "" {
		firstDate, firstHash, orders = "尚无", "尚无", "等待日档"
	}
	report := fmt.Sprintf("# 前瞻采集与成交自动验收\n\n检查时间：%s\n\n归档与首日状态：%s\n\n%s。\n\n观察目录：%s\n\n已封存交易日：%d；首日：%s\n\n四组首日待执行信号数：%s\n\n配置指纹：%s\n\n首日日档指纹：%s\n\n## 成交验收\n\n状态：%s；已核对%d次相邻日档转换，核验至：%s\n\n%s。\n\n| 账户 | 首次买入 | 首次卖出 | 买入次数 | 卖出次数 | 一字板顺延次数 |\n|---|---|---|---:|---:|---:|\n", result.CheckedAt.Format(time.RFC3339), result.Status, result.Detail, result.Archive, result.SealedDays, firstDate, orders, result.PlanFingerprint, firstHash, result.Execution.Status, result.Execution.Transitions, displayDate(result.Execution.VerifiedThrough), result.Execution.Detail)
	for _, account := range result.Execution.Accounts {
		report += fmt.Sprintf("| %s | %s | %s | %d | %d | %d |\n", account.Account, displayDate(account.FirstBuy), displayDate(account.FirstSell), account.Buys, account.Sells, account.DeferredOrders)
	}
	report += "\n本程序只读复算封存日K并核对账务，不下载行情、不写入交易、不修改策略、不迁移历史演练样本。成交passed要求四组各有至少一次已核验买入；尚未发生卖出的账户不代表已获得实际卖出或T+1观察证据。成交价是既有日K开盘模拟，不代表真实订单成交，策略效果仍需按原样本门槛复核。\n"
	if err := writeAtomic(filepath.Join(outputPath, "report.md"), []byte(report)); err != nil {
		return err
	}
	fmt.Printf("%s %s: %s\n", result.CheckedAt.Format(time.RFC3339), result.Status, result.Detail)
	return inspectErr
}

func displayDate(date string) string {
	if date == "" {
		return "尚无"
	}
	return date
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

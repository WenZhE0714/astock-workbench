package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func forwardExecutableHash() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (app *App) runBacktestVolumeForward(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: astock backtest volume-forward init --source 量比研究目录 --output 新目录 | update/status/audit 观察目录")
	}
	if args[0] == "init" {
		flags := flag.NewFlagSet("volume-forward init", flag.ContinueOnError)
		flags.SetOutput(app.errOut)
		source := flags.String("source", "", "量比研究目录")
		output := flags.String("output", "", "新建独立观察目录")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" || *output == "" || flags.NArg() != 0 {
			return fmt.Errorf("必须指定 --source 和 --output")
		}
		data, err := os.ReadFile(filepath.Join(*source, "result.json"))
		if err != nil {
			return err
		}
		var study backtest.VolumeStudyResult
		if err = json.Unmarshal(data, &study); err != nil {
			return err
		}
		executableHash, err := forwardExecutableHash()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		plan, err := backtest.NewVolumeForwardPlan(study, hex.EncodeToString(sum[:]), executableHash, time.Now())
		if err != nil {
			return err
		}
		if err := os.Mkdir(*output, 0o700); err != nil {
			return fmt.Errorf("观察目录必须不存在: %w", err)
		}
		store := storage.NewVolumeForwardStore(*output)
		unlock, err := store.Acquire()
		if err != nil {
			return err
		}
		defer unlock()
		if err := store.Initialize(plan, data); err != nil {
			return err
		}
		report := volumeForwardReport(plan, nil)
		if err := store.SaveReport(report); err != nil {
			return err
		}
		fmt.Fprintln(app.out, report)
		return nil
	}
	if len(args) != 2 || (args[0] != "update" && args[0] != "status" && args[0] != "audit" && args[0] != "preflight") {
		return fmt.Errorf("用法: astock backtest volume-forward update/status/audit/preflight 观察目录")
	}
	store := storage.NewVolumeForwardStore(args[1])
	if args[0] == "update" {
		unlock, err := store.Acquire()
		if err != nil {
			return err
		}
		defer unlock()
	}
	plan, previous, count, err := store.Load()
	if err != nil {
		return err
	}
	if args[0] == "preflight" {
		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		checks, err := backtest.ProbeVolumeForwardSource(probeCtx, plan, market.TencentClient{}, time.Now())
		for _, check := range checks {
			fmt.Fprintf(app.out, "%s: %d根日K，最新%s\n", check.Symbol, check.Bars, check.Latest)
		}
		if err == nil {
			fmt.Fprintln(app.out, "数据源预检通过；未写入观察日或成交。")
		}
		return err
	}
	if args[0] != "update" {
		fmt.Fprintln(app.out, volumeForwardReport(plan, previous))
		fmt.Fprintf(app.out, "配置、源研究与%d份日档指纹/哈希链校验通过。\n", count)
		return nil
	}
	executableHash, err := forwardExecutableHash()
	if err != nil {
		return err
	}
	if executableHash != plan.ExecutableSHA256 {
		return fmt.Errorf("程序指纹与冻结版本不一致；请使用本实验专用二进制")
	}
	now := time.Now().In(shanghaiLocation)
	saveStatus := func(status, detail string) error {
		fmt.Fprintf(app.out, "%s %s: %s\n", now.Format(time.RFC3339), status, detail)
		return store.SaveStatus(map[string]any{"checked_at": now, "status": status, "detail": detail, "sealed_days": count, "promotion_allowed": false})
	}
	if due, reason := backtest.ForwardCollectionDue(plan, previous, now); !due {
		if err := store.SaveReport(volumeForwardReport(plan, previous)); err != nil {
			return err
		}
		return saveStatus("waiting", reason)
	}
	// A direct source prevents a completed-range cache from hiding stale data.
	// All four runs use the single collected snapshot, never separate downloads.
	collectCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	day, err := backtest.BuildVolumeForwardDay(collectCtx, plan, previous, market.TencentClient{}, now)
	if err != nil {
		if statusErr := saveStatus("error", err.Error()); statusErr != nil {
			return fmt.Errorf("%v；状态保存失败: %w", err, statusErr)
		}
		return err
	}
	if err := store.Append(plan, day); err != nil {
		return err
	}
	count++
	if err := store.SaveReport(volumeForwardReport(plan, &day)); err != nil {
		return err
	}
	return saveStatus("sealed", day.Date+"四组结果和输入已封存")
}

func volumeForwardReport(plan backtest.VolumeForwardPlan, day *backtest.VolumeForwardDay) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# P008量比前瞻对照\n\n冻结时间：%s；起始边界：%s（含当日，按实际交易日历）。\n\n原规则和量比至少1.5倍各100万元，单股上限10%%，另设各自双倍成本影子计算。固定%d只股票、MA20/60、量能基线1.2、止损8%%、止盈20%%、最长40个交易日保持固定。\n\n", plan.CreatedAt.Format(time.RFC3339), plan.Baseline.Start.Format("2006-01-02"), len(plan.Baseline.Tickers))
	fmt.Fprintf(&out, "首次复核要求至少%d个已封存交易日，并且候选至少%d笔平仓。数量达标后仍需检查净收益、沪深300超额、回撤、双倍成本、暴露和错失赢家。不会自动修改其他账户或晋级。\n\n", plan.Policy.MinimumTradingDays, plan.Policy.MinimumTrades)
	if day == nil {
		out.WriteString("当前状态：已冻结，等待新交易日；0个观察日、0笔成交，尚无策略效果结论。\n\n")
	} else {
		fmt.Fprintf(&out, "封存至：%s；观察%d个交易日。%s。\n\n| 账户 | 净收益 | 收盘回撤 | 平仓 | 持仓数 | 待执行信号 | 费用 |\n|---|---:|---:|---:|---:|---:|---:|\n", day.Date, len(day.Runs[0].Equity), day.Assessment.Verdict)
		for i, name := range []string{"原规则", "量比1.5", "原规则双倍成本", "量比1.5双倍成本"} {
			run := day.Runs[i]
			fmt.Fprintf(&out, "| %s | %+.4f%% | %.4f%% | %d | %d | %d | %.2f |\n", name, run.Metrics.TotalReturn, run.Metrics.MaxDrawdown, run.Metrics.Trades, len(run.OpenPositions), len(run.PendingOrders), run.Metrics.TotalFees)
		}
		v := day.NormalImpact
		fmt.Fprintf(&out, "\n常规成本：净值差%+.2f元，平均持仓比例%.2f%% → %.2f%%，量比拒绝%d条信号；避开%d笔已实现亏损、错过%d笔已实现盈利。完整逐笔归因及净值见当日日档。\n\n", v.EquityDifference, v.BaselineExposure, v.FilteredExposure, v.BlockedSignals, v.AvoidedLosses, v.MissedWins)
		out.WriteString("| 复核项目 | 达标 | 说明 |\n|---|---|---|\n")
		for _, check := range day.Assessment.Checks {
			fmt.Fprintf(&out, "| %s | %v | %s |\n", check.Name, check.Passed, check.Detail)
		}
		out.WriteString("\n")
	}
	out.WriteString("这是收盘后逐日封存的日线模拟：先前封存的收盘信号按后续完整日K的开盘价撮合，整手、T+1、一字板顺延和费用沿用研究引擎，不代表真实开盘成交。每日收盘不强制清仓。\n\n只接受当日15:30至19:00的数据；股票停牌/缺档与基准日历不齐、漏采交易日、历史价格或量能变化均停止推进，不补记为成功前瞻。应查看status.json及采集日志识别假日等待或实际异常。\n\n日K无法还原涨跌停排队；未复权价格存在除权除息影响；股票池固定且样本偏少。当前试验是技术规则比较，不能直接推断真实账户盈利。\n\n")
	fmt.Fprintf(&out, "配置指纹：`%s`\n\n固定程序：`%s`\n", plan.Fingerprint, plan.ExecutableSHA256)
	return out.String()
}

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

const (
	maximumStrategySymbols = 20
	strategyRunTimeout     = 3 * time.Minute
)

var strategyLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type strategyBacktestInput struct {
	Symbols            []string `json:"symbols"`
	Start              string   `json:"start"`
	End                string   `json:"end"`
	EntryMode          string   `json:"entry_mode"`
	FastMA             int      `json:"fast_ma"`
	SlowMA             int      `json:"slow_ma"`
	BreakoutDays       int      `json:"breakout_days"`
	VolumeRatioMin     float64  `json:"volume_ratio_min"`
	StopLossPercent    float64  `json:"stop_loss_percent"`
	TakeProfitPercent  float64  `json:"take_profit_percent"`
	MaxHoldingDays     int      `json:"max_holding_days"`
	MaxPositionPercent float64  `json:"max_position_percent"`
	InitialCash        float64  `json:"initial_cash"`
	CommissionBPS      *float64 `json:"commission_bps"`
	MinimumCommission  *float64 `json:"minimum_commission"`
	StampDutyBPS       *float64 `json:"stamp_duty_bps"`
	TransferFeeBPS     *float64 `json:"transfer_fee_bps"`
	SlippageBPS        *float64 `json:"slippage_bps"`
	Benchmark          string   `json:"benchmark"`
	LiquidateAtEnd     *bool    `json:"liquidate_at_end"`
}

type strategyResearchCheck struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type strategyResearchAssessment struct {
	Stage     string                  `json:"stage"`
	Verdict   string                  `json:"verdict"`
	Passed    bool                    `json:"passed"`
	NextStage string                  `json:"next_stage"`
	Checks    []strategyResearchCheck `json:"checks"`
}

type strategyBacktestResponse struct {
	Result     backtest.Result                `json:"result"`
	Assessment strategyResearchAssessment     `json:"assessment"`
	EntryModes []backtest.EntryModeDescriptor `json:"entry_modes,omitempty"`
}

type strategyRunSummary struct {
	RunID       string            `json:"run_id"`
	GeneratedAt time.Time         `json:"generated_at"`
	Tickers     []string          `json:"tickers"`
	Names       map[string]string `json:"names,omitempty"`
	Start       string            `json:"start"`
	End         string            `json:"end"`
	EntryMode   string            `json:"entry_mode"`
	Metrics     backtest.Metrics  `json:"metrics"`
	Verdict     string            `json:"verdict"`
}

type strategyRunListResponse struct {
	Items      []strategyRunSummary           `json:"items"`
	EntryModes []backtest.EntryModeDescriptor `json:"entry_modes"`
}

func (s *Server) handleStrategyBacktests(writer http.ResponseWriter, request *http.Request) {
	if s.strategyEngine == nil || s.strategyArchive == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "量化研究服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.writeStrategyBacktests(writer, request)
	case http.MethodPost:
		s.runStrategyBacktest(writer, request)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "量化回测只支持 GET、POST"})
	}
}

func (s *Server) writeStrategyBacktests(writer http.ResponseWriter, request *http.Request) {
	if id := strings.TrimSpace(request.URL.Query().Get("id")); id != "" {
		result, err := s.strategyArchive.Load(id)
		if err != nil {
			writeJSON(writer, http.StatusNotFound, errorResponse{Error: err.Error()})
			return
		}
		backtest.EnrichRiskMetrics(&result)
		backtest.EnrichMarketRegimeMetrics(&result)
		writeJSON(writer, http.StatusOK, strategyBacktestResponse{Result: result, Assessment: assessStrategyResult(result), EntryModes: backtest.EntryModeDescriptors()})
		return
	}
	limit := 20
	if value := strings.TrimSpace(request.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 50 {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "历史数量必须在 1 到 50 之间"})
			return
		}
		limit = parsed
	}
	items, err := s.strategyArchive.List(limit)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	response := strategyRunListResponse{Items: make([]strategyRunSummary, 0, len(items)), EntryModes: backtest.EntryModeDescriptors()}
	for _, item := range items {
		result, loadError := s.strategyArchive.Load(item.RunID)
		if loadError != nil {
			continue
		}
		backtest.EnrichRiskMetrics(&result)
		assessment := assessStrategyResult(result)
		response.Items = append(response.Items, strategyRunSummary{
			RunID: result.RunID, GeneratedAt: result.GeneratedAt,
			Tickers: append([]string(nil), result.Request.Tickers...), Names: result.Request.Names,
			Start: result.Request.Start.Format("2006-01-02"), End: result.Request.End.Format("2006-01-02"),
			EntryMode: result.Request.Technical.EffectiveEntryMode(), Metrics: result.Metrics, Verdict: assessment.Verdict,
		})
	}
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) runStrategyBacktest(writer http.ResponseWriter, request *http.Request) {
	s.strategyMu.Lock()
	if s.strategyRunning {
		s.strategyMu.Unlock()
		writeJSON(writer, http.StatusConflict, errorResponse{Error: "已有量化回测正在运行，请稍后再试"})
		return
	}
	s.strategyRunning = true
	s.strategyMu.Unlock()
	defer func() {
		s.strategyMu.Lock()
		s.strategyRunning = false
		s.strategyMu.Unlock()
	}()

	var input strategyBacktestInput
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "回测请求格式无效: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), strategyRunTimeout)
	defer cancel()
	backtestRequest, err := s.strategyRequest(ctx, input)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	result, err := s.strategyEngine.Run(ctx, backtestRequest)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "回测执行失败: " + err.Error()})
		return
	}
	backtest.EnrichRiskMetrics(&result)
	backtest.EnrichMarketRegimeMetrics(&result)
	result, err = s.strategyArchive.Save(result)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "回测归档失败: " + err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, strategyBacktestResponse{Result: result, Assessment: assessStrategyResult(result), EntryModes: backtest.EntryModeDescriptors()})
}

func (s *Server) strategyRequest(ctx context.Context, input strategyBacktestInput) (backtest.Request, error) {
	if s.resolver == nil {
		return backtest.Request{}, fmt.Errorf("证券解析服务未初始化")
	}
	if len(input.Symbols) < 1 || len(input.Symbols) > maximumStrategySymbols {
		return backtest.Request{}, fmt.Errorf("股票池必须包含 1 到 %d 只 A 股", maximumStrategySymbols)
	}
	start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(input.Start), strategyLocation)
	if err != nil {
		return backtest.Request{}, fmt.Errorf("开始日期无效，格式应为 YYYY-MM-DD")
	}
	end, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(input.End), strategyLocation)
	if err != nil {
		return backtest.Request{}, fmt.Errorf("结束日期无效，格式应为 YYYY-MM-DD")
	}
	today := time.Now().In(strategyLocation)
	latestCompleted := time.Date(today.Year(), today.Month(), today.Day()-1, 0, 0, 0, 0, strategyLocation)
	for latestCompleted.Weekday() == time.Saturday || latestCompleted.Weekday() == time.Sunday {
		latestCompleted = latestCompleted.AddDate(0, 0, -1)
	}
	if start.After(end) || end.After(latestCompleted) {
		return backtest.Request{}, fmt.Errorf("回测结束日期不得晚于最近已完成交易日 %s", latestCompleted.Format("2006-01-02"))
	}
	if end.Sub(start) < 180*24*time.Hour {
		return backtest.Request{}, fmt.Errorf("回测区间至少需要 180 个自然日")
	}
	if end.Sub(start) > 15*365*24*time.Hour {
		return backtest.Request{}, fmt.Errorf("单次回测区间不能超过 15 年")
	}

	defaults := backtest.DefaultTechnicalParameters()
	if input.EntryMode == "" {
		input.EntryMode = defaults.EntryMode
	}
	if input.FastMA == 0 {
		input.FastMA = defaults.FastMA
	}
	if input.SlowMA == 0 {
		input.SlowMA = defaults.SlowMA
	}
	if input.BreakoutDays == 0 {
		input.BreakoutDays = defaults.BreakoutDays
	}
	if input.VolumeRatioMin == 0 {
		input.VolumeRatioMin = defaults.VolumeRatioMin
	}
	if input.StopLossPercent == 0 {
		input.StopLossPercent = defaults.StopLoss * 100
	}
	if input.TakeProfitPercent == 0 {
		input.TakeProfitPercent = defaults.TakeProfit * 100
	}
	if input.MaxHoldingDays == 0 {
		input.MaxHoldingDays = defaults.MaxHoldingDays
	}
	if input.MaxPositionPercent == 0 {
		input.MaxPositionPercent = defaults.MaxPosition * 100
	}
	if input.InitialCash == 0 {
		input.InitialCash = 1_000_000
	}
	parameters := backtest.TechnicalParameters{
		EntryMode: input.EntryMode, FastMA: input.FastMA, SlowMA: input.SlowMA,
		BreakoutDays: input.BreakoutDays, VolumeRatioMin: input.VolumeRatioMin,
		StopLoss: input.StopLossPercent / 100, TakeProfit: input.TakeProfitPercent / 100,
		MaxHoldingDays: input.MaxHoldingDays, MaxPosition: input.MaxPositionPercent / 100,
	}
	if err := backtest.ValidateTechnicalParameters(parameters); err != nil {
		return backtest.Request{}, err
	}
	if !finiteStrategyValues(input.InitialCash, valueOr(input.CommissionBPS, 3), valueOr(input.MinimumCommission, 5),
		valueOr(input.StampDutyBPS, 5), valueOr(input.TransferFeeBPS, .1), valueOr(input.SlippageBPS, 5)) || input.InitialCash < 10_000 {
		return backtest.Request{}, fmt.Errorf("资金、费用或滑点参数无效")
	}
	commissionBPS := valueOr(input.CommissionBPS, 3)
	minimumCommission := valueOr(input.MinimumCommission, 5)
	stampDutyBPS := valueOr(input.StampDutyBPS, 5)
	transferFeeBPS := valueOr(input.TransferFeeBPS, .1)
	slippageBPS := valueOr(input.SlippageBPS, 5)
	if commissionBPS < 0 || commissionBPS > 100 || minimumCommission < 0 || minimumCommission > 100 ||
		stampDutyBPS < 0 || stampDutyBPS > 100 || transferFeeBPS < 0 || transferFeeBPS > 100 || slippageBPS < 0 || slippageBPS > 200 {
		return backtest.Request{}, fmt.Errorf("费用或滑点超出受控范围")
	}

	tickers := make([]string, 0, len(input.Symbols))
	seen := make(map[string]bool, len(input.Symbols))
	for _, raw := range input.Symbols {
		symbol, resolveError := s.resolver.Resolve(ctx, strings.TrimSpace(raw))
		if resolveError != nil {
			return backtest.Request{}, resolveError
		}
		if market.AssetKindOf(symbol) != domain.AssetKindStock || strategyIndexSymbol(symbol) {
			return backtest.Request{}, fmt.Errorf("%s 不是可用于 A 股交易回测的股票", raw)
		}
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		tickers = append(tickers, symbol)
	}
	if len(tickers) == 0 {
		return backtest.Request{}, fmt.Errorf("股票池为空")
	}
	names := make(map[string]string, len(tickers))
	if s.quotes != nil {
		if quotes, _ := s.fetchQuoteBatch(ctx, tickers); len(quotes) > 0 {
			for symbol, quote := range quotes {
				if quote.Name != "" {
					names[symbol] = quote.Name
				}
			}
		}
	}
	for _, symbol := range tickers {
		if names[symbol] == "" {
			names[symbol] = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(symbol, "sh"), "sz"), "bj")
		}
	}
	benchmark := strings.TrimSpace(input.Benchmark)
	if benchmark == "" {
		benchmark = "sh000300"
	}
	if benchmark != "sh000300" {
		return backtest.Request{}, fmt.Errorf("当前 Web 研究基准仅支持沪深300")
	}
	liquidate := true
	if input.LiquidateAtEnd != nil {
		liquidate = *input.LiquidateAtEnd
	}
	return backtest.Request{
		Strategy: "technical-breakout", StrategyVersion: "web-v2", Tickers: tickers, Names: names,
		Start: start, End: end, InitialCash: input.InitialCash,
		CommissionRate: commissionBPS / 10_000, MinimumCommission: minimumCommission,
		StampDutyRate: stampDutyBPS / 10_000, TransferFeeRate: transferFeeBPS / 10_000,
		SlippageBPS: slippageBPS, Adjustment: backtest.AdjustmentNone, Benchmark: benchmark,
		NoFutureData: true, PointInTimePool: false, LiquidateAtEnd: liquidate, Technical: parameters,
	}, nil
}

func assessStrategyResult(result backtest.Result) strategyResearchAssessment {
	coveragePassed := len(result.DataCoverage) > 0
	minimumCoverage := 1.0
	for _, coverage := range result.DataCoverage {
		minimumCoverage = math.Min(minimumCoverage, coverage.CoverageRatio)
		if coverage.CoverageRatio < .85 {
			coveragePassed = false
		}
	}
	samplePassed := len(result.Equity) >= 252
	tradePassed := result.Metrics.Trades >= 30
	benchmarkPassed := result.Metrics.BenchmarkAvailable
	riskPassed := result.Metrics.Sharpe >= 1 && math.Abs(result.Metrics.MaxDrawdown) <= 20
	excessPassed := benchmarkPassed && result.Metrics.ExcessReturn > 0
	checks := []strategyResearchCheck{
		{Key: "coverage", Name: "数据覆盖", Passed: coveragePassed, Required: true, Detail: fmt.Sprintf("最低覆盖率 %.1f%%", minimumCoverage*100)},
		{Key: "sample", Name: "样本长度", Passed: samplePassed, Required: true, Detail: fmt.Sprintf("%d 个交易日", len(result.Equity))},
		{Key: "trades", Name: "交易样本", Passed: tradePassed, Required: true, Detail: fmt.Sprintf("%d 笔，研究门槛 30 笔", result.Metrics.Trades)},
		{Key: "benchmark", Name: "基准数据", Passed: benchmarkPassed, Required: true, Detail: "沪深300基准与超额收益"},
		{Key: "risk", Name: "风险收益", Passed: riskPassed, Required: false, Detail: fmt.Sprintf("Sharpe %.2f，最大回撤 %.2f%%", result.Metrics.Sharpe, result.Metrics.MaxDrawdown)},
		{Key: "excess", Name: "超额收益", Passed: excessPassed, Required: false, Detail: fmt.Sprintf("相对沪深300 %+.2f%%", result.Metrics.ExcessReturn)},
	}
	requiredPassed := coveragePassed && samplePassed && tradePassed && benchmarkPassed
	allPassed := requiredPassed && riskPassed && excessPassed
	verdict := "仅供探索，尚未通过研究门禁"
	if !coveragePassed || !samplePassed {
		verdict = "基础数据质量不足"
	} else if allPassed {
		verdict = "可进入样本外与滚动验证"
	}
	return strategyResearchAssessment{
		Stage: "基础历史回测", Verdict: verdict, Passed: allPassed,
		NextStage: "训练/验证拆分、一次性样本外检验与 Walk-Forward 滚动验证", Checks: checks,
	}
}

func finiteStrategyValues(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func valueOr(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func strategyIndexSymbol(symbol string) bool {
	switch strings.ToLower(symbol) {
	case "sh000001", "sz399001", "sz399006", "sz399106", "bj899050", "sh000300":
		return true
	default:
		return false
	}
}

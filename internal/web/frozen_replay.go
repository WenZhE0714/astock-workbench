package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type frozenReplaySource struct{ ID, Name, Kind, Directory string }

func WithFrozenReplay(id, name, kind, directory string) ServerOption {
	return func(s *Server) {
		s.frozenReplaySources = append(s.frozenReplaySources, frozenReplaySource{id, name, kind, directory})
	}
}

type replaySourceView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Kind        string             `json:"kind"`
	Dates       []string           `json:"dates"`
	Symbols     []domain.Candidate `json:"symbols"`
	Fingerprint string             `json:"fingerprint"`
	Start       string             `json:"start"`
	Error       string             `json:"error,omitempty"`
}

type replayCandle struct {
	Time   string  `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

func replayCandles(bars []domain.DailyBar) []replayCandle {
	result := make([]replayCandle, 0, len(bars))
	for _, bar := range bars {
		result = append(result, replayCandle{bar.Date, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume})
	}
	return result
}

type replayDayView struct {
	Source      replaySourceView                `json:"source"`
	Date        string                          `json:"date"`
	Symbol      string                          `json:"symbol"`
	CapturedAt  time.Time                       `json:"captured_at"`
	Fingerprint string                          `json:"fingerprint"`
	Plan        backtest.VolumeForwardPlan      `json:"plan"`
	Bars        []replayCandle                  `json:"bars"`
	Weekly      []replayCandle                  `json:"weekly"`
	Benchmark   []replayCandle                  `json:"benchmark"`
	Runs        []backtest.Result               `json:"runs"`
	Analysis    domain.ChartAnalysis            `json:"analysis"`
	Timeframes  domain.ChartTimeframeComparison `json:"timeframes"`
}

func replaySourceSummary(source frozenReplaySource, plan backtest.VolumeForwardPlan, latest *backtest.VolumeForwardDay) replaySourceView {
	view := replaySourceView{ID: source.ID, Name: source.Name, Kind: source.Kind, Dates: []string{}, Symbols: []domain.Candidate{}, Fingerprint: plan.Fingerprint, Start: plan.Baseline.Start.Format(time.DateOnly)}
	for _, symbol := range plan.Baseline.Tickers {
		view.Symbols = append(view.Symbols, domain.Candidate{Symbol: symbol, Name: plan.Baseline.Names[symbol]})
	}
	if latest != nil {
		for _, point := range latest.Runs[0].Equity {
			view.Dates = append(view.Dates, point.Date)
		}
	}
	return view
}

func (s *Server) handleReplayCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "复盘目录只支持GET"})
		return
	}
	views := []replaySourceView{}
	for _, source := range s.frozenReplaySources {
		plan, day, _, err := storage.NewVolumeForwardStore(source.Directory).Load()
		view := replaySourceSummary(source, plan, day)
		if err != nil {
			view.Error = "封存归档校验失败"
			view.Dates = []string{}
		}
		views = append(views, view)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, struct {
		Sources []replaySourceView `json:"sources"`
	}{views})
}

func (s *Server) handleReplayDay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "冻结复盘只支持GET"})
		return
	}
	var source *frozenReplaySource
	for i := range s.frozenReplaySources {
		if s.frozenReplaySources[i].ID == r.URL.Query().Get("source") {
			source = &s.frozenReplaySources[i]
			break
		}
	}
	if source == nil {
		writeJSON(w, 404, errorResponse{Error: "复盘来源不存在"})
		return
	}
	plan, latest, _, err := storage.NewVolumeForwardStore(source.Directory).Load()
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: "封存归档校验失败，请刷新后重试"})
		return
	}
	view := replaySourceSummary(*source, plan, latest)
	date, symbol := r.URL.Query().Get("date"), r.URL.Query().Get("symbol")
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		writeJSON(w, 400, errorResponse{Error: "封存日期无效"})
		return
	}
	if r.URL.Query().Get("fingerprint") != plan.Fingerprint {
		writeJSON(w, 409, errorResponse{Error: "冻结配置已变化，请重新加载来源"})
		return
	}
	if !slices.Contains(view.Dates, date) || !slices.Contains(plan.Baseline.Tickers, symbol) {
		writeJSON(w, 400, errorResponse{Error: "日期或股票不在已封存范围内"})
		return
	}
	data, err := os.ReadFile(filepath.Join(source.Directory, "days", date+".json"))
	var day backtest.VolumeForwardDay
	if err == nil {
		err = json.Unmarshal(data, &day)
	}
	if err == nil {
		err = backtest.ValidateVolumeForwardDay(plan, day)
	}
	if err != nil || day.Date != date {
		writeJSON(w, 409, errorResponse{Error: "所选日档校验失败"})
		return
	}
	// The archive must never expose later bars, fills, signals or equity through replay.
	if err := validateReplayCutoff(day); err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	if source.Kind == "forward" && day.CapturedAt.After(s.currentTime()) {
		writeJSON(w, 409, errorResponse{Error: "日档采集时间尚未到达"})
		return
	}
	bars := day.Bars[symbol]
	analysis, err := strategy.AnalyzeChart(symbol, bars, day.CapturedAt, date)
	if err != nil {
		writeJSON(w, 422, errorResponse{Error: err.Error()})
		return
	}
	timeframes, err := strategy.AnalyzeChartTimeframes(symbol, bars, day.CapturedAt, date)
	if err != nil {
		writeJSON(w, 422, errorResponse{Error: err.Error()})
		return
	}
	view.Dates = nil
	result := replayDayView{Source: view, Date: date, Symbol: symbol, CapturedAt: day.CapturedAt, Fingerprint: day.Fingerprint, Plan: plan, Bars: replayCandles(bars), Weekly: replayCandles(strategy.CompletedChartWeeks(bars)), Benchmark: replayCandles(day.Bars[plan.Baseline.Benchmark]), Runs: day.Runs, Analysis: analysis, Timeframes: timeframes}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}

func validateReplayCutoff(day backtest.VolumeForwardDay) error {
	bad := func() error { return fmt.Errorf("日档包含晚于%s的数据，停止回放", day.Date) }
	for _, bars := range day.Bars {
		for _, bar := range bars {
			if bar.Date > day.Date {
				return bad()
			}
		}
	}
	for _, run := range day.Runs {
		if run.Request.End.Format(time.DateOnly) > day.Date {
			return bad()
		}
		for _, point := range run.Equity {
			if point.Date > day.Date {
				return bad()
			}
		}
		for _, point := range run.BenchmarkEquity {
			if point.Date > day.Date {
				return bad()
			}
		}
		for _, trade := range run.Trades {
			if trade.Entry.Date > day.Date || trade.Exit.Date > day.Date || trade.EntrySignal.Date > day.Date || trade.ExitSignal.Date > day.Date {
				return bad()
			}
		}
		for _, position := range run.OpenPositions {
			if position.Entry.Date > day.Date || position.EntrySignal.Date > day.Date || position.LastDate > day.Date {
				return bad()
			}
		}
		for _, order := range run.PendingOrders {
			if order.Signal.Date > day.Date {
				return bad()
			}
		}
		for _, block := range run.VolumeBlockedEntries {
			if block.SignalDate > day.Date {
				return bad()
			}
		}
	}
	return nil
}

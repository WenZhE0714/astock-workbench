package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

const shadowExecutionTimeout = 2 * time.Minute

const (
	shadowProfileBalanced     = "balanced"
	shadowProfileConservative = "conservative"
	shadowProfileAggressive   = "aggressive"
	shadowProfileMonster      = "monster"
	shadowProfileAdaptive     = "adaptive"
)

var shadowProfileOrder = []string{shadowProfileBalanced, shadowProfileConservative, shadowProfileAggressive, shadowProfileMonster, shadowProfileAdaptive}

type shadowProfileSummary struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Strategy           string       `json:"strategy"`
	Description        string       `json:"description"`
	Config             paper.Config `json:"config"`
	Available          bool         `json:"available"`
	AsOf               string       `json:"as_of,omitempty"`
	CheckpointPhase    string       `json:"checkpoint_phase,omitempty"`
	EngineVersion      string       `json:"engine_version,omitempty"`
	TotalEquity        float64      `json:"total_equity"`
	TotalProfit        float64      `json:"total_profit"`
	TotalReturnPercent float64      `json:"total_return_percent"`
	RemainingCash      float64      `json:"remaining_cash"`
	TotalMarketValue   float64      `json:"total_market_value"`
	InvestedPercent    float64      `json:"invested_percent"`
	OpenPositions      int          `json:"open_positions"`
	FilledEntries      int          `json:"filled_entries"`
	CompletedTrades    int          `json:"completed_trades"`
	OrderCount         int          `json:"order_count"`
}

type shadowResponse struct {
	Report         *paper.Report          `json:"report,omitempty"`
	Account        *shadowProfileSummary  `json:"account,omitempty"`
	Profiles       []shadowProfileSummary `json:"profiles,omitempty"`
	Cached         bool                   `json:"cached,omitempty"`
	Preserved      bool                   `json:"preserved,omitempty"`
	PreserveReason string                 `json:"preserve_reason,omitempty"`
}

func (s *Server) handleShadowExecution(writer http.ResponseWriter, request *http.Request) {
	if s.shadowEvaluator == nil || len(s.shadowProfiles) == 0 {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "影子执行服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		profileID, err := s.shadowRequestedProfile(request, false)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		response, err := s.shadowExecutionResponse(request.Context(), profileID, true, false, "")
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取影子执行结果失败: " + err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, response)
	case http.MethodPost:
		if s.realtimeArchive == nil {
			writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时信号归档未初始化"})
			return
		}
		requestedProfile, err := s.shadowRequestedProfile(request, true)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		selectedProfile := requestedProfile
		if requestedProfile == "all" {
			selectedProfile = strings.TrimSpace(request.URL.Query().Get("selected"))
			if selectedProfile == "" {
				selectedProfile = shadowProfileBalanced
			}
			if _, ok := s.shadowProfiles[selectedProfile]; !ok {
				writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "未知影子账户: " + selectedProfile})
				return
			}
		}
		s.shadowMu.Lock()
		defer s.shadowMu.Unlock()
		ctx, cancel := context.WithTimeout(request.Context(), shadowExecutionTimeout)
		defer cancel()
		profileIDs := []string{requestedProfile}
		if requestedProfile == "all" {
			profileIDs = s.orderedShadowProfileIDs()
		}
		var signals []realtime.Signal
		signalsLoaded := false
		loadSignals := func(limit int) ([]realtime.Signal, error) {
			if signalsLoaded {
				return signals, nil
			}
			items, listErr := s.realtimeArchive.List(limit)
			if listErr != nil {
				return nil, listErr
			}
			signals = items
			signalsLoaded = true
			return signals, nil
		}
		allCached, anyPreserved := true, false
		anyPreserveReason := ""
		for _, profileID := range profileIDs {
			profile := s.shadowProfiles[profileID]
			options, optionErr := s.shadowOptionsWithCalendar(ctx, request, profile.Config)
			if optionErr != nil {
				writeJSON(writer, http.StatusBadRequest, errorResponse{Error: optionErr.Error()})
				return
			}
			now := s.currentTime()
			session := s.marketSession(ctx, now)
			options.Realtime = session.State == realtime.MarketStateTrading || session.State == realtime.MarketStateAuction
			options.RealtimeAt = now
			if options.Realtime {
				if snapshot, snapshotErr := s.latestRealtimeSnapshot(); snapshotErr == nil {
					options.RealtimeQuotes = s.shadowRealtimeQuotes(ctx, snapshot)
				}
			}
			cached, preserved, preserveReason, syncErr := s.syncShadowProfile(ctx, request, profile, options, loadSignals)
			if syncErr != nil {
				writeJSON(writer, http.StatusBadGateway, errorResponse{Error: profile.Name + "同步失败: " + syncErr.Error()})
				return
			}
			allCached = allCached && cached
			anyPreserved = anyPreserved || preserved
			if preserved && preserveReason != "" {
				if anyPreserveReason == "" {
					anyPreserveReason = profile.Name + "：" + preserveReason
				}
			}
		}
		response, err := s.shadowExecutionResponse(request.Context(), selectedProfile, allCached, anyPreserved, anyPreserveReason)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取影子执行结果失败: " + err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, response)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "影子执行只支持 GET、POST"})
	}
}

func (s *Server) shadowRealtimeQuotes(ctx context.Context, snapshot realtime.ScanResult) []paper.PositionQuote {
	if s == nil || s.quotes == nil {
		return nil
	}
	symbols := make([]string, 0, len(snapshot.Signals))
	seen := make(map[string]bool)
	for _, signal := range snapshot.Signals {
		if signal.Symbol != "" && !seen[signal.Symbol] {
			seen[signal.Symbol] = true
			symbols = append(symbols, signal.Symbol)
		}
	}
	if len(symbols) == 0 {
		return nil
	}
	quotes, err := s.fetchQuoteBatch(ctx, symbols)
	if err != nil && len(quotes) == 0 {
		return nil
	}
	result := make([]paper.PositionQuote, 0, len(quotes))
	for symbol, quote := range quotes {
		price, err := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if err != nil || price <= 0 {
			continue
		}
		result = append(result, paper.PositionQuote{
			Symbol: symbol, Price: price, PreviousClose: parseQuoteFloat(quote.PreviousClose),
			Open: parseQuoteFloat(quote.Open), LimitUp: parseQuoteFloat(quote.LimitUp),
			High: parseQuoteFloat(quote.High), Low: parseQuoteFloat(quote.Low),
			AveragePrice: parseQuoteFloat(quote.AveragePrice), Volume: quote.Volume,
			LimitDown: parseQuoteFloat(quote.LimitDown), Amount: quote.Amount,
			QuoteTime: strings.TrimSpace(quote.QuoteTime), Source: strings.TrimSpace(quote.Source),
		})
	}
	return result
}

func parseQuoteFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return parsed
}

func (s *Server) extendShadowRealtimeQuotes(ctx context.Context, quotes []paper.PositionQuote, positions []paper.ShadowOpenPosition) []paper.PositionQuote {
	seen := make(map[string]bool, len(quotes)+len(positions))
	for _, quote := range quotes {
		seen[quote.Symbol] = true
	}
	missing := make([]string, 0)
	for _, position := range positions {
		if position.Symbol != "" && !seen[position.Symbol] {
			seen[position.Symbol] = true
			missing = append(missing, position.Symbol)
		}
	}
	if len(missing) == 0 || s == nil || s.quotes == nil {
		return quotes
	}
	items, err := s.fetchQuoteBatch(ctx, missing)
	if err != nil && len(items) == 0 {
		return quotes
	}
	for symbol, quote := range items {
		price, parseErr := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if parseErr != nil || price <= 0 {
			continue
		}
		quotes = append(quotes, paper.PositionQuote{
			Symbol: symbol, Price: price, PreviousClose: parseQuoteFloat(quote.PreviousClose), Open: parseQuoteFloat(quote.Open),
			High: parseQuoteFloat(quote.High), Low: parseQuoteFloat(quote.Low), AveragePrice: parseQuoteFloat(quote.AveragePrice), Volume: quote.Volume,
			LimitUp: parseQuoteFloat(quote.LimitUp), LimitDown: parseQuoteFloat(quote.LimitDown), Amount: quote.Amount,
			QuoteTime: strings.TrimSpace(quote.QuoteTime), Source: strings.TrimSpace(quote.Source),
		})
	}
	return quotes
}

func singleShadowExecutionProfile(archive shadowArchive) map[string]shadowExecutionProfile {
	return defaultShadowExecutionProfiles(archive, nil, nil)
}

func defaultShadowExecutionProfiles(balanced, conservative, aggressive shadowArchive) map[string]shadowExecutionProfile {
	profiles := make(map[string]shadowExecutionProfile, 3)
	balancedConfig := paper.DefaultConfig()
	if balanced != nil {
		profiles[shadowProfileBalanced] = shadowExecutionProfile{
			ID: shadowProfileBalanced, Name: "均衡型", Strategy: "55 分门槛 · 80% 上限 · 8 持仓",
			Description: "延续现有账户，在信号覆盖、风险预算和现金余量之间取平衡。",
			Config:      balancedConfig, Archive: balanced,
		}
	}
	conservativeConfig := paper.DefaultConfig()
	conservativeConfig.MinimumScore = 62
	conservativeConfig.MaxPositionPercent = 12
	conservativeConfig.MaxIndustryPercent = 22
	conservativeConfig.MaxPortfolioPercent = 60
	conservativeConfig.CashReservePercent = 40
	conservativeConfig.MaxDailyDeploymentPercent = 20
	conservativeConfig.InitialEntryPercent = 40
	conservativeConfig.AdditionScoreStep = 6
	conservativeConfig.MaxOpenPositions = 6
	conservativeConfig.MaxDailyRotations = 1
	conservativeConfig.RotationScoreGap = 10
	conservativeConfig.RotationMinimumHoldDays = 3
	conservativeConfig.MaxPortfolioRiskPercent = 3.5
	conservativeConfig.MaxPositionRiskPercent = .9
	conservativeConfig.MaxLossPercent = 7
	conservativeConfig.MinimumRiskDistancePercent = 4
	conservativeConfig.RiskCooldownDays = 5
	conservativeConfig.MaxParticipationPercent = 5
	conservativeConfig.HoldingDays = 7
	conservativeConfig.TMaxDailyRounds = 1
	conservativeConfig.TVWAPDeviationPercent = 1.0
	conservativeConfig.TMinimumPriceGapPercent = 1.0
	conservativeConfig.TMinimumNetProfitPercent = 0.3
	conservativeConfig.TCooldownMinutes = 25
	if conservative != nil {
		profiles[shadowProfileConservative] = shadowExecutionProfile{
			ID: shadowProfileConservative, Name: "稳健型", Strategy: "62 分门槛 · 60% 上限 · 6 持仓",
			Description: "提高信号门槛并压低单股、行业和组合风险，保留更多现金。",
			Config:      conservativeConfig, Archive: conservative,
		}
	}
	aggressiveConfig := paper.DefaultConfig()
	aggressiveConfig.MinimumScore = 52
	aggressiveConfig.MaxPositionPercent = 25
	aggressiveConfig.MaxIndustryPercent = 40
	aggressiveConfig.MaxPortfolioPercent = 90
	aggressiveConfig.CashReservePercent = 10
	aggressiveConfig.MaxDailyDeploymentPercent = 50
	aggressiveConfig.InitialEntryPercent = 60
	aggressiveConfig.MaxEntryTranches = 4
	aggressiveConfig.AdditionScoreStep = 3
	aggressiveConfig.MaxOpenPositions = 10
	aggressiveConfig.MaxDailyRotations = 3
	aggressiveConfig.RotationScoreGap = 6
	aggressiveConfig.RotationMinimumHoldDays = 1
	aggressiveConfig.MaxPortfolioRiskPercent = 9
	aggressiveConfig.MaxPositionRiskPercent = 2.25
	aggressiveConfig.MaxLossPercent = 12
	aggressiveConfig.MinimumRiskDistancePercent = 2.5
	aggressiveConfig.RiskCooldownDays = 2
	aggressiveConfig.MaxParticipationPercent = 15
	aggressiveConfig.HoldingDays = 4
	aggressiveConfig.TMaxDailyRounds = 3
	aggressiveConfig.TVWAPDeviationPercent = 0.4
	aggressiveConfig.TMinimumPriceGapPercent = 0.5
	aggressiveConfig.TMinimumNetProfitPercent = 0.1
	aggressiveConfig.TCooldownMinutes = 10
	if aggressive != nil {
		profiles[shadowProfileAggressive] = shadowExecutionProfile{
			ID: shadowProfileAggressive, Name: "进取型", Strategy: "52 分门槛 · 90% 上限 · 10 持仓",
			Description: "扩大候选覆盖和分批空间，用更高风险预算换取更积极的仓位响应。",
			Config:      aggressiveConfig, Archive: aggressive,
		}
	}
	return profiles
}

func adaptiveShadowExecutionProfile(archive shadowArchive) shadowExecutionProfile {
	cfg := paper.DefaultConfig()
	cfg.UseCalibratedScore = true
	return shadowExecutionProfile{
		ID: shadowProfileAdaptive, Name: "自适应校准型", Strategy: "滚动验证门控 · Champion/Challenger",
		Description: "使用通过时间留出门禁的候选分数与组件权重，和均衡型并行观察，不直接改写基线。",
		Config:      cfg, Archive: archive,
	}
}

func monsterShadowExecutionProfile(archive shadowArchive) shadowExecutionProfile {
	cfg := paper.DefaultConfig()
	// The radar is deliberately observed in its own, lower-capacity ledger. It
	// gets enough cash and tranches to measure the signal, while retaining a
	// larger reserve because high-volatility candidates have wider slippage.
	cfg.UseMonsterRadar = true
	cfg.MinimumScore = 58
	cfg.MaxPositionPercent = 15
	cfg.MaxIndustryPercent = 25
	cfg.MaxPortfolioPercent = 70
	cfg.CashReservePercent = 30
	cfg.MaxDailyDeploymentPercent = 30
	cfg.InitialEntryPercent = 40
	cfg.MaxEntryTranches = 3
	cfg.AdditionScoreStep = 5
	cfg.MaxOpenPositions = 6
	cfg.MaxDailyRotations = 2
	cfg.RotationScoreGap = 8
	cfg.RotationMinimumHoldDays = 2
	cfg.MaxPortfolioRiskPercent = 5
	cfg.MaxPositionRiskPercent = 1.25
	cfg.MaxLossPercent = 9
	cfg.MinimumRiskDistancePercent = 3
	cfg.RiskCooldownDays = 3
	cfg.MaxParticipationPercent = 8
	cfg.HoldingDays = 4
	cfg.TMaxDailyRounds = 2
	cfg.TVWAPDeviationPercent = .8
	cfg.TMinimumPriceGapPercent = .8
	cfg.TMinimumNetProfitPercent = .2
	cfg.TCooldownMinutes = 15
	return shadowExecutionProfile{
		ID: shadowProfileMonster, Name: "抓妖实验型", Strategy: "雷达≥58 · 09:40后入场 · 6持仓",
		Description: "仅使用抓妖雷达的独立纸面账本；开盘前10分钟只观察，09:40后验证潜伏、启动和加速阶段，不影响基线账户。",
		Config:      cfg, Archive: archive,
	}
}

func (s *Server) orderedShadowProfileIDs() []string {
	ids := make([]string, 0, len(s.shadowProfiles))
	seen := make(map[string]bool, len(s.shadowProfiles))
	for _, id := range shadowProfileOrder {
		if _, ok := s.shadowProfiles[id]; ok {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	extra := make([]string, 0)
	for id := range s.shadowProfiles {
		if !seen[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	return append(ids, extra...)
}

func (s *Server) shadowRequestedProfile(request *http.Request, allowAll bool) (string, error) {
	id := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("profile")))
	if id == "" {
		id = shadowProfileBalanced
	}
	if allowAll && id == "all" {
		return id, nil
	}
	if _, ok := s.shadowProfiles[id]; !ok {
		return "", &queryError{"未知影子账户: " + id}
	}
	return id, nil
}

func (s *Server) syncShadowProfile(ctx context.Context, request *http.Request, profile shadowExecutionProfile, options paper.Options, loadSignals func(int) ([]realtime.Signal, error)) (bool, bool, string, error) {
	return s.syncShadowProfileSignals(ctx, request, profile, options, loadSignals, nil, false)
}

func (s *Server) syncShadowProfileFromSnapshot(ctx context.Context, request *http.Request, profile shadowExecutionProfile, options paper.Options, loadSignals func(int) ([]realtime.Signal, error), signals []realtime.Signal) (bool, bool, string, error) {
	return s.syncShadowProfileSignals(ctx, request, profile, options, loadSignals, signals, true)
}

func (s *Server) syncShadowProfileSignals(ctx context.Context, request *http.Request, profile shadowExecutionProfile, options paper.Options, loadSignals func(int) ([]realtime.Signal, error), snapshotSignals []realtime.Signal, useSnapshotSignals bool) (bool, bool, string, error) {
	previous, err := profile.Archive.Load()
	if err != nil {
		return false, false, "", fmt.Errorf("读取账户失败: %w", err)
	}
	if options.Realtime {
		options.RealtimeQuotes = s.extendShadowRealtimeQuotes(ctx, options.RealtimeQuotes, previous.Positions)
	}
	checkpoint, err := s.shadowCheckpointForReport(ctx, previous)
	if err != nil {
		return false, false, "", fmt.Errorf("读取交易日历失败: %w", err)
	}
	if !options.Realtime && !shadowRebuildRequested(request) && shadowReportMatches(previous, options, checkpoint) {
		return true, false, "", nil
	}
	var report paper.Report
	if !shadowRebuildRequested(request) && shadowCanAdvanceRealtime(previous, options, checkpoint) {
		if !shadowRealtimeQuotesAdvance(previous, options.RealtimeQuotes, options.RealtimeAt) {
			return true, false, "", nil
		}
		if advancer, ok := s.shadowEvaluator.(shadowRealtimeAdvancer); ok {
			signals := snapshotSignals
			if !useSnapshotSignals {
				signals, err = loadSignals(options.Limit)
				if err != nil {
					return false, false, "", fmt.Errorf("读取实时信号失败: %w", err)
				}
			}
			report, err = advancer.AdvanceRealtime(ctx, previous, signals, options)
		} else {
			signals, loadErr := loadSignals(options.Limit)
			if loadErr != nil {
				return false, false, "", fmt.Errorf("读取实时信号失败: %w", loadErr)
			}
			if advancer, ok := s.shadowEvaluator.(shadowAdvancer); ok {
				report, err = advancer.Advance(ctx, previous, signals, options)
			} else {
				report, err = s.shadowEvaluator.Evaluate(ctx, signals, options)
			}
		}
	} else {
		if options.Realtime && len(options.RealtimeQuotes) == 0 {
			options.Realtime = false
		}
		signals, loadErr := loadSignals(options.Limit)
		if loadErr != nil {
			return false, false, "", fmt.Errorf("读取实时信号失败: %w", loadErr)
		}
		if !shadowRebuildRequested(request) && shadowCanAdvance(previous, options) {
			if advancer, ok := s.shadowEvaluator.(shadowAdvancer); ok {
				report, err = advancer.Advance(ctx, previous, signals, options)
			} else {
				report, err = s.shadowEvaluator.Evaluate(ctx, signals, options)
			}
		} else {
			report, err = s.shadowEvaluator.Evaluate(ctx, signals, options)
		}
	}
	if err != nil {
		return false, false, "", err
	}
	if transitionErr := paper.ValidateTransition(previous, report); transitionErr != nil {
		// A stale realtime snapshot can select the fast path while the account
		// is still catching up from an older checkpoint. Retry once through the
		// complete daily incremental path before preserving the ledger. This
		// keeps historical orders immutable without freezing all future trades.
		fallbackOptions := options
		fallbackOptions.Realtime = false
		fallbackOptions.RealtimeAt = time.Time{}
		fallbackOptions.RealtimeQuotes = nil
		if fullSignals, loadErr := loadSignals(0); loadErr == nil {
			if advancer, ok := s.shadowEvaluator.(shadowAdvancer); ok {
				if fallbackReport, fallbackErr := advancer.Advance(ctx, previous, fullSignals, fallbackOptions); fallbackErr == nil {
					if validateErr := paper.ValidateTransition(previous, fallbackReport); validateErr == nil {
						report = fallbackReport
						transitionErr = nil
					}
				}
			}
		}
		if transitionErr != nil && !shadowRebuildRequested(request) &&
			(strings.Contains(transitionErr.Error(), "成交单数量从") || strings.Contains(transitionErr.Error(), "账户现金、费用或成交统计被改写")) {
			// Some early v11 realtime snapshots were written before all daily
			// events had landed. Never replace that durable history with a shorter
			// replay. Keep the previous ledger and move only the checkpoint forward;
			// the next cycle can append new events from this preserved state.
			preserved := previous
			preserved.Config = options.Config
			preserved.ConfigFingerprint = paper.OptionsFingerprint(options.Config, options.Limit)
			if report.AsOf > preserved.AsOf {
				preserved.AsOf = report.AsOf
			}
			preserved.CheckpointPhase = report.CheckpointPhase
			preserved.GeneratedAt = report.GeneratedAt
			preserved.SignalLimit = options.Limit
			report = preserved
			transitionErr = nil
		}
		if transitionErr == nil {
			if err := profile.Archive.Save(report); err != nil {
				return false, false, "", fmt.Errorf("保存账户失败: %w", err)
			}
			return false, false, "", nil
		}
		reason := "账户连续性保护：本次同步未覆盖持仓，" + transitionErr.Error()
		previous.Warnings = appendUniqueShadowWarning(previous.Warnings, reason)
		return true, true, reason, nil
	}
	if err := profile.Archive.Save(report); err != nil {
		return false, false, "", fmt.Errorf("保存账户失败: %w", err)
	}
	return false, false, "", nil
}

func appendUniqueShadowWarning(warnings []string, value string) []string {
	for _, warning := range warnings {
		if warning == value {
			return warnings
		}
	}
	return append(warnings, value)
}

func (s *Server) shadowOptionsWithCalendar(ctx context.Context, request *http.Request, base paper.Config) (paper.Options, error) {
	options, err := shadowOptions(request, base)
	if err != nil {
		return paper.Options{}, err
	}
	dates, calendarErr := s.tradingCalendar(ctx, s.currentTime())
	if calendarErr != nil {
		return options, nil
	}
	options.CalendarDates = dates
	return options, nil
}

func (s *Server) shadowExecutionResponse(ctx context.Context, selectedID string, cached, preserved bool, preserveReason string) (shadowResponse, error) {
	reports := make(map[string]paper.Report, len(s.shadowProfiles))
	for _, id := range s.orderedShadowProfileIDs() {
		report, err := s.shadowProfiles[id].Archive.Load()
		if err != nil {
			return shadowResponse{}, fmt.Errorf("%s: %w", s.shadowProfiles[id].Name, err)
		}
		reports[id] = report
	}
	reports = s.markShadowReports(ctx, reports)
	summaries := make([]shadowProfileSummary, 0, len(reports))
	var account *shadowProfileSummary
	for _, id := range s.orderedShadowProfileIDs() {
		profile := s.shadowProfiles[id]
		summary := makeShadowProfileSummary(profile, reports[id])
		summaries = append(summaries, summary)
		if id == selectedID {
			selected := summary
			account = &selected
		}
	}
	var report *paper.Report
	if account != nil && account.Available {
		selected := reports[selectedID]
		report = &selected
	}
	return shadowResponse{Report: report, Account: account, Profiles: summaries, Cached: cached, Preserved: preserved, PreserveReason: preserveReason}, nil
}

func makeShadowProfileSummary(profile shadowExecutionProfile, report paper.Report) shadowProfileSummary {
	config := profile.Config
	if report.Config.InitialCash > 0 {
		config = report.Config
	}
	investedPercent := 0.0
	if report.TotalEquity > 0 {
		investedPercent = report.TotalMarketValue / report.TotalEquity * 100
	}
	available := !report.GeneratedAt.IsZero() || report.AsOf != "" || report.ConfigFingerprint != "" || len(report.Orders) > 0 || len(report.Positions) > 0 || len(report.Trades) > 0
	return shadowProfileSummary{
		ID: profile.ID, Name: profile.Name, Strategy: profile.Strategy, Description: profile.Description,
		Config: config, Available: available, AsOf: report.AsOf, CheckpointPhase: report.CheckpointPhase,
		EngineVersion: report.EngineVersion, TotalEquity: report.TotalEquity, TotalProfit: report.TotalProfit,
		TotalReturnPercent: report.TotalReturnPercent, RemainingCash: report.RemainingCash,
		TotalMarketValue: report.TotalMarketValue, InvestedPercent: investedPercent,
		OpenPositions: report.OpenPositions, FilledEntries: report.FilledEntries,
		CompletedTrades: report.CompletedTrades, OrderCount: len(report.Orders),
	}
}

func (s *Server) markShadowPositions(ctx context.Context, report paper.Report) paper.Report {
	marked := s.markShadowReports(ctx, map[string]paper.Report{"selected": report})
	return marked["selected"]
}

func (s *Server) markShadowReports(ctx context.Context, reports map[string]paper.Report) map[string]paper.Report {
	if s == nil || s.quotes == nil || len(reports) == 0 {
		return reports
	}
	seen := make(map[string]struct{})
	symbols := make([]string, 0)
	for _, report := range reports {
		for _, position := range report.Positions {
			if position.Symbol == "" {
				continue
			}
			if _, ok := seen[position.Symbol]; ok {
				continue
			}
			seen[position.Symbol] = struct{}{}
			symbols = append(symbols, position.Symbol)
		}
	}
	if len(symbols) == 0 {
		return reports
	}
	quoteContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	quotes, err := s.quotes.Fetch(quoteContext, symbols)
	if err != nil {
		return reports
	}
	itemsBySymbol := make(map[string]paper.PositionQuote, len(quotes))
	staleSymbols := make(map[string]bool)
	for _, quote := range quotes {
		price, parseErr := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if parseErr != nil || price <= 0 {
			continue
		}
		if !shadowQuoteFresh(quote.QuoteTime, s.currentTime()) {
			staleSymbols[quote.Symbol] = true
			continue
		}
		itemsBySymbol[quote.Symbol] = paper.PositionQuote{
			Symbol: quote.Symbol, Price: price,
			QuoteTime: strings.TrimSpace(quote.QuoteTime), Source: strings.TrimSpace(quote.Source),
		}
	}
	valuedAt := s.currentTime()
	for id, report := range reports {
		items := make([]paper.PositionQuote, 0, len(report.Positions))
		stale := 0
		for _, position := range report.Positions {
			if item, ok := itemsBySymbol[position.Symbol]; ok {
				items = append(items, item)
			}
			if staleSymbols[position.Symbol] {
				stale++
			}
		}
		if stale > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%d 只持仓行情超过 30 分钟，保留日线估值", stale))
		}
		if len(items) > 0 {
			report = paper.RevaluePositions(report, items, valuedAt)
		}
		reports[id] = report
	}
	return reports
}

func (s *Server) shadowCheckpoint(ctx context.Context) (paper.Checkpoint, error) {
	now := s.currentTime()
	dates, err := s.tradingCalendar(ctx, now)
	if err != nil {
		return paper.Checkpoint{}, err
	}
	return paper.TradingCheckpointAt(now, dates), nil
}

func (s *Server) shadowCheckpointForReport(ctx context.Context, report paper.Report) (paper.Checkpoint, error) {
	// Always consult the shared calendar before declaring a cached checkpoint
	// current. A weekday-only shortcut would let a statutory holiday reuse a
	// same-date report and silently advance T+1 state.
	return s.shadowCheckpoint(ctx)
}

// tradingCalendar is shared by realtime session classification and shadow
// checkpoints. Benchmark K-line dates preserve statutory holidays and
// exchange make-up Saturdays whenever they are in the provider's coverage.
func (s *Server) tradingCalendar(ctx context.Context, now time.Time) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.shadowCalendarMu.Lock()
	if !s.shadowCalendarFetchedAt.IsZero() && now.Sub(s.shadowCalendarFetchedAt) >= 0 && now.Sub(s.shadowCalendarFetchedAt) < shadowCalendarCacheTTL {
		dates := append([]string(nil), s.shadowCalendarDates...)
		s.shadowCalendarMu.Unlock()
		return dates, nil
	}
	s.shadowCalendarMu.Unlock()
	providerConfigured := s.tradingCalendarProvider != nil
	if providerConfigured {
		dates, err := s.tradingCalendarProvider(ctx, now)
		if err == nil {
			dates = realtime.NormalizeTradingDates(dates)
			if len(dates) > 0 {
				s.shadowCalendarMu.Lock()
				s.shadowCalendarDates = append([]string(nil), dates...)
				s.shadowCalendarFetchedAt = now
				s.shadowCalendarMu.Unlock()
				return dates, nil
			}
		}
	}
	if s.history == nil {
		if providerConfigured {
			// The optional file provider is advisory. Keep the service usable
			// with weekday fallback when both it and benchmark history are absent.
			return nil, nil
		}
		return nil, nil
	}

	calendarContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	bars, err := s.history.FetchDailyBars(calendarContext, "sh000300")
	if err != nil {
		if providerConfigured {
			return nil, nil
		}
		return nil, err
	}
	dates := realtime.TradingDatesFromBars(bars)
	if len(dates) == 0 {
		// Keep the explicit degradation path when the benchmark endpoint is
		// reachable but returns no usable rows. The evaluator surfaces a warning
		// and falls back to symbol-level trading dates.
		return nil, nil
	}
	s.shadowCalendarMu.Lock()
	s.shadowCalendarDates = append([]string(nil), dates...)
	s.shadowCalendarFetchedAt = now
	s.shadowCalendarMu.Unlock()
	return dates, nil
}

func (s *Server) marketSession(ctx context.Context, now time.Time) realtime.MarketSession {
	dates, err := s.tradingCalendar(ctx, now)
	if err != nil {
		return realtime.MarketSessionAt(now)
	}
	return realtime.MarketSessionAtWithCalendar(now, dates)
}

func shadowOptions(request *http.Request, base paper.Config) (paper.Options, error) {
	// A zero limit means all archived signals. The shadow account must not let
	// frequent intraday scans crowd older T+1 signals out of the replay set.
	options := paper.Options{Config: base, Limit: 0}
	values := request.URL.Query()
	var err error
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		options.Limit, err = strconv.Atoi(raw)
		if err != nil || options.Limit < 0 || options.Limit > 20000 {
			return paper.Options{}, &queryError{"影子评估信号数量必须在 0 到 20000 之间，0 表示全部归档"}
		}
	}
	if raw := strings.TrimSpace(values.Get("minimum_score")); raw != "" {
		options.Config.MinimumScore, err = strconv.ParseFloat(raw, 64)
		if err != nil || options.Config.MinimumScore < 0 || options.Config.MinimumScore > 100 {
			return paper.Options{}, &queryError{"最低综合分必须在 0 到 100 之间"}
		}
	}
	if raw := strings.TrimSpace(values.Get("holding_days")); raw != "" {
		options.Config.HoldingDays, err = strconv.Atoi(raw)
		if err != nil || options.Config.HoldingDays < 1 || options.Config.HoldingDays > 60 {
			return paper.Options{}, &queryError{"影子持有窗口必须在 1 到 60 个交易日之间"}
		}
	}
	return options, nil
}

func shadowRebuildRequested(request *http.Request) bool {
	if request == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("rebuild")))
	return value == "1" || value == "true" || value == "yes"
}

func shadowReportMatches(report paper.Report, options paper.Options, checkpoint paper.Checkpoint) bool {
	return report.EngineVersion == paper.ShadowEngineVersion && report.ConfigFingerprint == paper.OptionsFingerprint(options.Config, options.Limit) && report.AsOf == checkpoint.Date && report.CheckpointPhase == checkpoint.Phase
}

func shadowCanAdvance(report paper.Report, options paper.Options) bool {
	hasLedger := len(report.Orders) > 0 || len(report.Positions) > 0 || len(report.Trades) > 0
	if report.EngineVersion != paper.ShadowEngineVersion && hasLedger {
		// Shadow engine upgrades are forward-compatible when the persisted
		// ledger uses the same effective risk configuration. Canonicalizing the
		// old config fills fields introduced by v9-v11 with current defaults,
		// allowing Advance to migrate the ledger instead of replaying history.
		previousConfig := report.Config
		if report.EngineVersion == "tplus1-v8" {
			// v9 introduced the rotation controls; adopt the configured values
			// while preserving all earlier ledger-affecting parameters.
			previousConfig.MaxOpenPositions = options.Config.MaxOpenPositions
			previousConfig.MaxDailyRotations = options.Config.MaxDailyRotations
			previousConfig.RotationScoreGap = options.Config.RotationScoreGap
			previousConfig.RotationMinimumHoldDays = options.Config.RotationMinimumHoldDays
		} else if report.EngineVersion != "" {
			// v9/v10 reports predate the realtime-T and risk-budget fields. Their
			// zero values mean "field absent", so adopt the current profile's
			// values during migration rather than treating them as intentional
			// overrides that would force a rebuild.
			previousConfig.EnableIntradayT = options.Config.EnableIntradayT
			previousConfig.TCorePositionPercent = options.Config.TCorePositionPercent
			previousConfig.TTranchePercent = options.Config.TTranchePercent
			previousConfig.TMaxDailyRounds = options.Config.TMaxDailyRounds
			previousConfig.TVWAPDeviationPercent = options.Config.TVWAPDeviationPercent
			previousConfig.TMinimumPriceGapPercent = options.Config.TMinimumPriceGapPercent
			previousConfig.TMinimumNetProfitPercent = options.Config.TMinimumNetProfitPercent
			previousConfig.TCooldownMinutes = options.Config.TCooldownMinutes
			previousConfig.SignalRebalanceCooldownMinutes = options.Config.SignalRebalanceCooldownMinutes
			previousConfig.MaxPortfolioRiskPercent = options.Config.MaxPortfolioRiskPercent
			previousConfig.MaxPositionRiskPercent = options.Config.MaxPositionRiskPercent
			previousConfig.MaxLossPercent = options.Config.MaxLossPercent
			previousConfig.MinimumRiskDistancePercent = options.Config.MinimumRiskDistancePercent
			previousConfig.RiskCooldownDays = options.Config.RiskCooldownDays
		}
		previousConfig = paper.CanonicalConfigForMigration(previousConfig)
		return paper.ConfigFingerprint(previousConfig) == paper.ConfigFingerprint(options.Config)
	}
	if report.ConfigFingerprint != "" && report.ConfigFingerprint != paper.OptionsFingerprint(options.Config, options.Limit) {
		return false
	}
	if report.ConfigFingerprint == "" && paper.ConfigFingerprint(report.Config) != paper.ConfigFingerprint(options.Config) {
		return false
	}
	return report.EngineVersion == paper.ShadowEngineVersion || hasLedger
}

func shadowCanAdvanceRealtime(report paper.Report, options paper.Options, checkpoint paper.Checkpoint) bool {
	if !options.Realtime || checkpoint.Phase != paper.CheckpointOpen || report.EngineVersion != paper.ShadowEngineVersion || report.AsOf != checkpoint.Date || report.CheckpointPhase != paper.CheckpointOpen {
		return false
	}
	return report.ConfigFingerprint == paper.OptionsFingerprint(options.Config, options.Limit)
}

func shadowRealtimeQuotesAdvance(report paper.Report, quotes []paper.PositionQuote, now time.Time) bool {
	if len(quotes) == 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	watermark, hasWatermark := parseShadowQuoteTime(report.LastRealtimeAt)
	for _, quote := range quotes {
		quoteTime, ok := parseShadowQuoteTime(quote.QuoteTime)
		if ok && shadowRealtimeQuoteUsable(quote, quoteTime, now) && (!hasWatermark || quoteTime.After(watermark)) {
			return true
		}
	}
	return false
}

func shadowRealtimeQuoteUsable(quote paper.PositionQuote, quoteTime, now time.Time) bool {
	if quote.Price <= 0 || quoteTime.IsZero() {
		return false
	}
	localQuote := quoteTime.In(realtimeWebLocation)
	localNow := now.In(realtimeWebLocation)
	if localQuote.Format("2006-01-02") != localNow.Format("2006-01-02") {
		return false
	}
	delta := localNow.Sub(localQuote)
	if delta < -2*time.Minute || delta > 10*time.Minute {
		return false
	}
	minutes := localQuote.Hour()*60 + localQuote.Minute()
	return minutes >= 9*60+30 && minutes <= 11*60+30 || minutes >= 13*60 && minutes < 15*60
}

func parseShadowQuoteTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, value, realtimeWebLocation)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func shadowQuoteFresh(raw string, now time.Time) bool {
	quoteTime, ok := parseShadowQuoteTime(raw)
	if !ok {
		return false
	}
	delta := now.In(realtimeWebLocation).Sub(quoteTime)
	return delta >= -2*time.Minute && delta <= 30*time.Minute
}

type queryError struct{ message string }

func (err *queryError) Error() string { return err.message }

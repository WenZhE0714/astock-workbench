package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type boardDailyFlowClient interface {
	FetchBoardDailyFlows(context.Context, string) ([]domain.BoardDailyFlow, error)
}
type boardDailyCache struct {
	at   time.Time
	rows []domain.BoardDailyFlow
}

func WithBoardWorkbench(store *storage.BoardSnapshotStore, daily boardDailyFlowClient) ServerOption {
	return func(s *Server) {
		s.boardSnapshots = store
		s.boardDailyFlows = daily
		s.boardDailyCache = map[string]boardDailyCache{}
	}
}

func (s *Server) runBoardSnapshotLoop(ctx context.Context) {
	if s.boardSnapshots == nil || s.industryFlows == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.captureBoardSnapshot(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) captureBoardSnapshot(parent context.Context) {
	if !s.boardSnapshotCycleMu.TryLock() {
		return
	}
	defer s.boardSnapshotCycleMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	now := s.currentTime()
	session := s.marketSession(ctx, now)
	if !session.TradingDay || (session.State != realtime.MarketStateTrading && !session.FinalizationAllowed) {
		return
	}
	setError := func(err error) {
		s.boardSnapshotMu.Lock()
		defer s.boardSnapshotMu.Unlock()
		s.boardSnapshotError = err.Error()
	}
	if !session.CalendarKnown {
		quotes, err := s.fetchQuoteBatch(ctx, []string{"sh000001"})
		quote, ok := quotes["sh000001"]
		at := notificationTime(quote.QuoteTime)
		if err != nil || !ok || at.In(realtimeWebLocation).Format(time.DateOnly) != session.TradingDate || now.Sub(at) > 2*time.Minute || at.After(now.Add(5*time.Second)) {
			setError(fmt.Errorf("交易日历未覆盖且缺少当日有效指数报价，暂停板块快照采集"))
			return
		}
	}
	flows, err := s.industryFlows.FetchIndustryFlows(ctx)
	if err != nil {
		setError(err)
		return
	}
	if len(flows) == 0 {
		setError(fmt.Errorf("板块资金未返回有效数据"))
		return
	}
	captured := s.currentTime()
	local := captured.In(realtimeWebLocation)
	finishedSession := realtime.MarketSessionAtWithCalendar(captured, []string{session.TradingDate})
	if local.Format(time.DateOnly) != session.TradingDate || (finishedSession.State != realtime.MarketStateTrading && !finishedSession.FinalizationAllowed) {
		return
	}
	snapshot := domain.BoardSnapshot{Version: 1, ID: local.Format("1504"), Date: session.TradingDate, CapturedAt: captured, CalendarKnown: session.CalendarKnown, Items: []domain.BoardObservation{}, Signals: []domain.BoardSignal{}, Holdings: []domain.BoardHolding{}}
	if !session.CalendarKnown {
		snapshot.Warnings = append(snapshot.Warnings, "交易日由当日有效指数报价确认")
	}
	for _, flow := range flows {
		if !market.BoardCodePattern.MatchString(flow.Code) {
			continue
		}
		snapshot.Items = append(snapshot.Items, domain.BoardObservation{Code: flow.Code, Name: flow.Name, Kind: flow.Kind, Source: flow.Source, Percent: finitePointer(flow.Percent), MainNet: finitePointer(flow.MainNet), MainRatio: finitePointer(flow.MainRatio), Turnover: finitePointer(flow.Turnover), RiseCount: flow.RiseCount, FallCount: flow.FallCount, Leader: flow.LeaderName})
	}
	sort.Slice(snapshot.Items, func(i, j int) bool { return snapshot.Items[i].Code < snapshot.Items[j].Code })
	result, err := s.latestRealtimeSnapshot()
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "关联信号读取失败")
	}
	if err == nil && !result.GeneratedAt.After(captured) && captured.Sub(result.GeneratedAt) <= 3*time.Minute {
		for _, signal := range result.Signals {
			if signal.AsOf.After(captured) {
				continue
			}
			reason := ""
			if len(signal.Reasons) > 0 {
				reason = signal.Reasons[0]
			}
			snapshot.Signals = append(snapshot.Signals, domain.BoardSignal{Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, State: string(signal.State), Score: signal.Score, Price: signal.Price, Reason: reason, AsOf: signal.AsOf})
		}
	}
	for _, id := range s.orderedShadowProfileIDs() {
		profile := s.shadowProfiles[id]
		report, err := profile.Archive.Load()
		if err != nil {
			snapshot.Warnings = append(snapshot.Warnings, profile.Name+"持仓读取失败")
			continue
		}
		if report.GeneratedAt.After(captured) {
			continue
		}
		for _, position := range report.Positions {
			snapshot.Holdings = append(snapshot.Holdings, domain.BoardHolding{Profile: id, ProfileName: profile.Name, Symbol: position.Symbol, Name: position.Name, Industry: position.Industry, Quantity: position.Quantity, Available: position.AvailableQuantity, MarketValue: position.MarketValue, AsOf: report.GeneratedAt})
		}
	}
	if err := s.boardSnapshots.Save(snapshot); err != nil {
		setError(err)
		return
	}
	s.boardSnapshotMu.Lock()
	s.boardSnapshotError, s.boardSnapshotAt = "", captured
	s.boardSnapshotMu.Unlock()
}

func (s *Server) handleBoardSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "快照只支持 GET"})
		return
	}
	if s.boardSnapshots == nil {
		writeJSON(w, 503, errorResponse{Error: "板块快照未配置"})
		return
	}
	dates, err := s.boardSnapshots.Dates()
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.currentTime().In(realtimeWebLocation).Format(time.DateOnly)
	}
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil || parsed.Format(time.DateOnly) != date {
		writeJSON(w, 400, errorResponse{Error: "日期格式应为 YYYY-MM-DD"})
		return
	}
	items, err := s.boardSnapshots.Day(date)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	if after := r.URL.Query().Get("after"); after != "" {
		parsed, err := time.Parse("1504", after)
		if err != nil || parsed.Format("1504") != after {
			writeJSON(w, 400, errorResponse{Error: "分钟标识无效"})
			return
		}
		filtered := []domain.BoardSnapshot{}
		for _, item := range items {
			if item.ID > after {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	s.boardSnapshotMu.Lock()
	warning, last := s.boardSnapshotError, s.boardSnapshotAt
	s.boardSnapshotMu.Unlock()
	writeJSON(w, 200, struct {
		Date      string                 `json:"date"`
		Dates     []string               `json:"dates"`
		Snapshots []domain.BoardSnapshot `json:"snapshots"`
		Last      time.Time              `json:"last_captured_at"`
		Warning   string                 `json:"warning,omitempty"`
	}{date, dates, items, last, warning})
}

func (s *Server) handleBoardDailyFlows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "日线资金只支持 GET"})
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if !market.BoardCodePattern.MatchString(code) {
		writeJSON(w, 400, errorResponse{Error: "板块代码无效"})
		return
	}
	if s.boardDailyFlows == nil {
		writeJSON(w, 503, errorResponse{Error: "资金日线服务未配置"})
		return
	}
	now := s.currentTime()
	through := r.URL.Query().Get("through")
	if through == "" {
		through = now.In(realtimeWebLocation).Format(time.DateOnly)
	}
	if date, err := time.Parse(time.DateOnly, through); err != nil || date.Format(time.DateOnly) != through {
		writeJSON(w, 400, errorResponse{Error: "截止日期无效"})
		return
	}
	s.boardDailyMu.Lock()
	cached, found := s.boardDailyCache[code]
	s.boardDailyMu.Unlock()
	var err error
	age := now.Sub(cached.at)
	localNow, localFetched := now.In(realtimeWebLocation), cached.at.In(realtimeWebLocation)
	// A pre-close response can contain today's unfinished row. Fetch it again
	// before allowing that row into the completed-day view after 15:00.
	sameSession := localNow.Format(time.DateOnly) == localFetched.Format(time.DateOnly) &&
		(localNow.Hour() >= 15) == (localFetched.Hour() >= 15)
	if !found || age < 0 || age >= 10*time.Minute || !sameSession {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		cached.rows, err = s.boardDailyFlows.FetchBoardDailyFlows(ctx, code)
		if err != nil {
			writeJSON(w, 502, errorResponse{Error: "板块资金日线不可用: " + err.Error()})
			return
		}
		cached.at = now
		s.boardDailyMu.Lock()
		s.boardDailyCache[code] = cached
		s.boardDailyMu.Unlock()
	}
	rows := []domain.BoardDailyFlow{}
	local := now.In(realtimeWebLocation)
	for _, row := range cached.rows {
		if row.Date > through || row.Date > local.Format(time.DateOnly) || (row.Date == local.Format(time.DateOnly) && local.Hour() < 15) {
			continue
		}
		rows = append(rows, row)
	}
	writeJSON(w, 200, struct {
		Code      string                  `json:"code"`
		Source    string                  `json:"source"`
		FetchedAt time.Time               `json:"fetched_at"`
		Items     []domain.BoardDailyFlow `json:"items"`
	}{code, "东方财富日线资金（当前供应商历史）", cached.at, rows})
}

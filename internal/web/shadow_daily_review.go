package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type shadowDailyScanCache struct {
	readAt time.Time
	key    string
	read   storage.RealtimeDayRead
	items  map[string]paper.ShadowScanReview
}

type shadowDailyRow struct {
	ID                  string                       `json:"id"`
	Name                string                       `json:"name"`
	BaselineID          string                       `json:"baseline_id,omitempty"`
	TrialHash           string                       `json:"trial_hash,omitempty"`
	ConfigHash          string                       `json:"config_hash"`
	LedgerRevision      int                          `json:"ledger_revision"`
	ConfigBasis         string                       `json:"config_basis"`
	TradingStartAt      time.Time                    `json:"trading_start_at,omitzero"`
	FrozenConfigMatches *bool                        `json:"frozen_config_matches,omitempty"`
	Policy              string                       `json:"policy"`
	Scans               paper.ShadowScanReview       `json:"scans"`
	Ledger              *paper.ShadowDailyReview     `json:"ledger"`
	LedgerIssues        []paper.LedgerIssue          `json:"ledger_issues"`
	Snapshot            *storage.ShadowDailySnapshot `json:"snapshot"`
	ZeroTradeReason     string                       `json:"zero_trade_reason,omitempty"`
	RealizedDifference  *float64                     `json:"realized_difference,omitempty"`
	ReturnDifference    *float64                     `json:"return_difference,omitempty"`
}

type shadowDailyResponse struct {
	Date         string                  `json:"date"`
	Today        string                  `json:"today"`
	Dates        []string                `json:"dates"`
	GeneratedAt  time.Time               `json:"generated_at"`
	Source       storage.RealtimeDayRead `json:"source"`
	Rows         []shadowDailyRow        `json:"rows"`
	Warnings     []string                `json:"warnings"`
	ArchiveError string                  `json:"archive_error,omitempty"`
	Runtime      AutomationStatus        `json:"runtime"`
	Check        paper.ShadowDailyCheck  `json:"check"`
}

func WithShadowDailyReviews(store *storage.ShadowDailyReviewStore, scans *storage.RealtimeSignalStore) ServerOption {
	return func(s *Server) { s.shadowDailyStore, s.shadowDailyScans = store, scans }
}

func (s *Server) captureShadowDailyReviews(now time.Time) error {
	if s.shadowDailyStore == nil {
		return nil
	}
	errors := []string{}
	for _, id := range s.orderedShadowProfileIDs() {
		profile := s.shadowProfiles[id]
		report, err := profile.Archive.Load()
		if err == nil {
			err = s.shadowDailyStore.Capture(id, profile.TrialHash, report, now)
		}
		if err != nil {
			errors = append(errors, profile.Name+": "+err.Error())
		}
	}
	message := strings.Join(errors, "; ")
	s.shadowDailyMu.Lock()
	s.shadowDailyError = message
	s.shadowDailyMu.Unlock()
	if message != "" {
		return fmt.Errorf("%s", message)
	}
	return nil
}

func (s *Server) dailyScanReviews(ctx context.Context, date string, configs map[string]paper.Config, now time.Time) (storage.RealtimeDayRead, map[string]paper.ShadowScanReview, error) {
	var empty storage.RealtimeDayRead
	signature, err := s.shadowDailyScans.DaySignature(date)
	if err != nil {
		return empty, nil, err
	}
	ids := []string{}
	for id := range configs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	configKey := ""
	for _, id := range ids {
		configKey += "|" + id + ":" + paper.ConfigFingerprint(configs[id])
	}
	s.shadowDailyMu.Lock()
	defer s.shadowDailyMu.Unlock()
	key := signature + configKey
	if cached, ok := s.shadowDailyCache[date]; ok && cached.key == key && !now.Before(cached.readAt) {
		return cached.read, cached.items, nil
	}
	builders := map[string]*paper.ShadowScanReviewBuilder{}
	for _, id := range ids {
		builders[id] = paper.NewShadowScanReviewBuilder(date, configs[id])
	}
	futureRecords := 0
	read, err := s.shadowDailyScans.WalkDay(ctx, date, func(scan realtime.ScanResult) {
		if scan.GeneratedAt.After(now) {
			futureRecords++
			return
		}
		for _, builder := range builders {
			builder.Add(scan)
		}
	})
	if err != nil {
		return read, nil, err
	}
	read.FutureRecords = futureRecords
	items := map[string]paper.ShadowScanReview{}
	for id, builder := range builders {
		items[id] = builder.Result()
	}
	if s.shadowDailyCache == nil {
		s.shadowDailyCache = map[string]shadowDailyScanCache{}
	}
	if len(s.shadowDailyCache) >= 16 {
		for old := range s.shadowDailyCache {
			delete(s.shadowDailyCache, old)
			break
		}
	}
	if futureRecords == 0 {
		s.shadowDailyCache[date] = shadowDailyScanCache{readAt: now, key: read.Signature + configKey, read: read, items: items}
	}
	return read, items, nil
}

func (s *Server) handleShadowDailyReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "每日复盘仅支持只读查询"})
		return
	}
	if s.shadowDailyStore == nil || s.shadowDailyScans == nil {
		writeJSON(w, 503, errorResponse{Error: "每日复盘服务未配置"})
		return
	}
	now := s.currentTime()
	today := now.In(realtimeWebLocation).Format(time.DateOnly)
	date := r.URL.Query().Get("date")
	if date == "" {
		date = today
	}
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil || parsed.Format(time.DateOnly) != date || date > today {
		writeJSON(w, 400, errorResponse{Error: "复盘日期无效或尚未到达"})
		return
	}
	response, err := s.buildShadowDailyResponse(r.Context(), date, now)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: "读取每日复盘失败: " + err.Error()})
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="shadow-review-%s.json"`, date))
	}
	writeJSON(w, 200, response)
}

func (s *Server) buildShadowDailyResponse(ctx context.Context, date string, now time.Time) (response shadowDailyResponse, err error) {
	response = shadowDailyResponse{Date: date, Today: now.In(realtimeWebLocation).Format(time.DateOnly), GeneratedAt: now, Rows: []shadowDailyRow{}, Warnings: []string{}, Runtime: s.automationStatus()}
	snapshots, err := s.shadowDailyStore.Day(date)
	if err != nil {
		return response, err
	}
	configs := map[string]paper.Config{}
	comparisonValid := map[string]bool{}
	for _, id := range s.orderedShadowProfileIDs() {
		profile := s.shadowProfiles[id]
		report, err := profile.Archive.Load()
		if err != nil {
			return response, err
		}
		cfg := profile.Config
		row := shadowDailyRow{ID: id, Name: profile.Name, BaselineID: profile.BaselineID, TrialHash: profile.TrialHash, ConfigBasis: "current-rule-recheck", Policy: profile.Strategy, LedgerIssues: paper.AuditShadowLedger(report)}
		if snapshot, ok := snapshots[id]; ok && !snapshot.GeneratedAt.After(now) {
			cfg = snapshot.Config
			row.Snapshot, row.ConfigBasis = &snapshot, "archived-daily"
		} else if report.AsOf == date && report.ConfigFingerprint != "" {
			cfg, row.ConfigBasis = report.Config, "current-ledger"
		}
		configs[id], row.ConfigHash = cfg, paper.ConfigFingerprint(cfg)
		row.TradingStartAt = profile.Config.TradingStartAt
		row.LedgerRevision = report.LedgerRevision
		comparisonValid[id] = row.ConfigHash == paper.ConfigFingerprint(profile.Config) && paper.ConfigFingerprint(report.Config) == paper.ConfigFingerprint(profile.Config)
		if row.TrialHash != "" {
			matches := comparisonValid[id]
			row.FrozenConfigMatches = &matches
		}
		if row.TrialHash != "" && report.AsOf != "" && !comparisonValid[id] {
			response.Warnings = append(response.Warnings, profile.Name+"的账本或当日规则与冻结配置不一致，暂停收益比较")
		}
		if report.AsOf >= date && !report.GeneratedAt.After(now) && len(row.LedgerIssues) == 0 {
			row.Ledger = paper.ShadowDailyReviewForDate(report, date)
		}
		response.Rows = append(response.Rows, row)
	}
	read, scans, err := s.dailyScanReviews(ctx, date, configs, now)
	if err != nil {
		return response, err
	}
	response.Source = read
	if !read.Available {
		response.Warnings = append(response.Warnings, "所选日期没有扫描档案，不能将缺少数据视为零候选")
	}
	if read.InvalidLines > 0 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("扫描档案中%d条记录不可解析，仅统计完整记录", read.InvalidLines))
	}
	if read.PartialTail {
		response.Warnings = append(response.Warnings, "扫描档案末尾仍在写入，待下一次刷新补齐")
	}
	if read.FutureRecords > 0 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("已排除%d条时间晚于当前时点的扫描记录", read.FutureRecords))
	}
	byID := map[string]int{}
	for i := range response.Rows {
		row := &response.Rows[i]
		row.Scans = scans[row.ID]
		row.ZeroTradeReason = shadowZeroTradeReason(*row, read)
		byID[row.ID] = i
	}
	for i := range response.Rows {
		row := &response.Rows[i]
		index, found := byID[row.BaselineID]
		if !found || row.TrialHash == "" || !comparisonValid[row.ID] || !comparisonValid[row.BaselineID] {
			continue
		}
		baseline := response.Rows[index]
		if baseline.TrialHash != row.TrialHash || !configs[row.ID].TradingStartAt.Equal(configs[baseline.ID].TradingStartAt) || date < configs[row.ID].TradingStartAt.In(realtimeWebLocation).Format(time.DateOnly) {
			continue
		}
		if row.Ledger != nil && baseline.Ledger != nil {
			diff := row.Ledger.RealizedProfit - baseline.Ledger.RealizedProfit
			row.RealizedDifference = &diff
		}
		if a, b := row.Snapshot, baseline.Snapshot; a != nil && b != nil && a.Fresh && b.Fresh && a.Sealed == b.Sealed && a.Revision == row.LedgerRevision && b.Revision == baseline.LedgerRevision && len(a.LedgerIssues) == 0 && len(b.LedgerIssues) == 0 && a.TrialHash == b.TrialHash {
			diff := a.Return - b.Return
			row.ReturnDifference = &diff
		}
	}
	dates, err := s.shadowDailyScans.Dates()
	if err != nil {
		return response, err
	}
	archived, err := s.shadowDailyStore.Dates()
	if err != nil {
		return response, err
	}
	unique := map[string]bool{response.Today: true}
	for _, d := range append(dates, archived...) {
		if d <= response.Today {
			unique[d] = true
		}
	}
	for d := range unique {
		response.Dates = append(response.Dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(response.Dates)))
	s.shadowDailyMu.Lock()
	response.ArchiveError = s.shadowDailyError
	s.shadowDailyMu.Unlock()
	response.Check = checkShadowDaily(response)
	return response, nil
}

func shadowZeroTradeReason(row shadowDailyRow, source storage.RealtimeDayRead) string {
	if len(row.LedgerIssues) > 0 {
		return "账本校验未通过，成交统计待核对"
	}
	if row.Ledger == nil {
		return "账本尚未覆盖所选日期"
	}
	if row.Ledger.OrderCount > 0 {
		return ""
	}
	if !source.Available {
		return "未留存扫描档案，无法判断候选情况"
	}
	if row.Scans.Scans == 0 && row.Scans.BeforeStart > 0 {
		return "所选日期尚无对照起点之后的盘中扫描"
	}
	if row.Scans.Scans == 0 {
		return "没有可核验的连续交易时段扫描"
	}
	if row.Scans.Observations == 0 {
		return "扫描未包含可核验的当日信号"
	}
	if row.Scans.Eligible == 0 {
		if len(row.Scans.Gates) > 0 {
			return "已读取信号均未通过入场筛选：" + row.Scans.Gates[0].Label
		}
		return "已读取信号未通过入场筛选"
	}
	if len(row.Ledger.TopBlockers) > 0 {
		return "已有合格候选，执行记录主要原因为：" + row.Ledger.TopBlockers[0].Reason
	}
	if row.Ledger.DecisionCount > 0 {
		return "已有合格候选和决策记录，当日未产生模拟成交"
	}
	return "已有合格候选，但没有当日决策或成交记录，需核对影子调度"
}

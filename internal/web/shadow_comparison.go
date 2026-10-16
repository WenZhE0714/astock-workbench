package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type shadowComparisonPoint struct {
	Date   string   `json:"date"`
	Equity *float64 `json:"equity"`
	Return *float64 `json:"return_percent"`
	Reason string   `json:"reason,omitempty"`
}

type shadowComparisonActivity struct {
	Orders         int      `json:"orders"`
	ClosedTrades   int      `json:"closed_trades"`
	Fees           float64  `json:"fees"`
	RealizedProfit float64  `json:"realized_profit"`
	WinRate        *float64 `json:"win_rate_percent"`
}

type shadowComparisonRow struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	Policy           string                    `json:"policy"`
	BaselineID       string                    `json:"baseline_id,omitempty"`
	TrialHash        string                    `json:"trial_hash"`
	StartedAt        time.Time                 `json:"started_at"`
	AsOf             string                    `json:"as_of"`
	ExpectedDays     int                       `json:"expected_days"`
	SnapshotDays     int                       `json:"snapshot_days"`
	MissingDates     []string                  `json:"missing_dates"`
	Points           []shadowComparisonPoint   `json:"points"`
	Return           *float64                  `json:"return_percent"`
	ReturnDifference *float64                  `json:"return_difference"`
	MaxDrawdown      *float64                  `json:"max_drawdown_percent"`
	Activity         *shadowComparisonActivity `json:"activity"`
	Status           string                    `json:"status"`
	Warnings         []string                  `json:"warnings"`
}

type shadowComparisonResponse struct {
	Start         string                `json:"start"`
	End           string                `json:"end"`
	Today         string                `json:"today"`
	GeneratedAt   time.Time             `json:"generated_at"`
	CalendarKnown bool                  `json:"calendar_known"`
	Rows          []shadowComparisonRow `json:"rows"`
	Warnings      []string              `json:"warnings"`
}

func WithShadowComparisonCalendarFile(path string) ServerOption {
	return func(s *Server) { s.shadowComparisonCalendarFile = path }
}

func (s *Server) handleShadowComparison(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "多日对照仅支持只读查询"})
		return
	}
	if s.shadowDailyStore == nil {
		writeJSON(w, 503, errorResponse{Error: "收盘快照服务未配置"})
		return
	}
	now := s.currentTime()
	today := now.In(realtimeWebLocation).Format(time.DateOnly)
	start, end := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if end == "" {
		end = today
	}
	if start == "" {
		start = end
		for _, profile := range s.shadowProfiles {
			if profile.TrialHash != "" {
				date := profile.Config.TradingStartAt.In(realtimeWebLocation).Format(time.DateOnly)
				if date < start {
					start = date
				}
			}
		}
	}
	a, ae := time.Parse(time.DateOnly, start)
	b, be := time.Parse(time.DateOnly, end)
	if ae != nil || be != nil || a.Format(time.DateOnly) != start || b.Format(time.DateOnly) != end || start > end || end > today || b.Sub(a) > 366*24*time.Hour {
		writeJSON(w, 400, errorResponse{Error: "对照日期范围无效、尚未到达或超过366天"})
		return
	}
	report, err := s.buildShadowComparison(r.Context(), start, end, now)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: "读取多日对照失败: " + err.Error()})
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="shadow-comparison-%s-%s.json"`, start, end))
	}
	writeJSON(w, 200, report)
}

// This calendar is read from an existing benchmark cache, never fetched by a
// comparison request. Its bounds determine whether missing days can be proven.
func comparisonCalendar(path string, now time.Time) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		FetchedAt time.Time `json:"fetched_at"`
		Bars      []struct {
			Date   string `json:"date"`
			Symbol string `json:"symbol"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload.FetchedAt.IsZero() || payload.FetchedAt.After(now) {
		return nil, fmt.Errorf("日历缓存时点无效")
	}
	dates := []string{}
	for _, bar := range payload.Bars {
		if bar.Symbol != "sh000300" {
			return nil, fmt.Errorf("日历缓存并非沪深300")
		}
		if bar.Date <= now.In(realtimeWebLocation).Format(time.DateOnly) {
			dates = append(dates, bar.Date)
		}
	}
	return realtime.NormalizeTradingDates(dates), nil
}

func (s *Server) buildShadowComparison(ctx context.Context, start, end string, now time.Time) (shadowComparisonResponse, error) {
	result := shadowComparisonResponse{Start: start, End: end, Today: now.In(realtimeWebLocation).Format(time.DateOnly), GeneratedAt: now, Rows: []shadowComparisonRow{}, Warnings: []string{}}
	calendar, calendarErr := comparisonCalendar(s.shadowComparisonCalendarFile, now)
	result.CalendarKnown = calendarErr == nil && len(calendar) > 0 && calendar[0] <= start && calendar[len(calendar)-1] >= end
	if !result.CalendarKnown {
		result.Warnings = append(result.Warnings, "交易日日历未完整覆盖所选范围，缺档与回撤暂不可完整评估")
	}
	storedDates, err := s.shadowDailyStore.Dates()
	if err != nil {
		return result, err
	}
	unique := map[string]bool{}
	for _, date := range append(append([]string{}, calendar...), storedDates...) {
		if date >= start && date <= end {
			unique[date] = true
		}
	}
	dates := []string{}
	for date := range unique {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	snapshots := map[string]map[string]storage.ShadowDailySnapshot{}
	badDates := map[string]bool{}
	for _, date := range dates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		items, err := s.shadowDailyStore.Day(date)
		if err != nil {
			badDates[date] = true
			result.Warnings = append(result.Warnings, date+" 收盘档案校验失败")
			continue
		}
		snapshots[date] = items
	}
	for _, id := range s.orderedShadowProfileIDs() {
		profile := s.shadowProfiles[id]
		if profile.TrialHash == "" {
			continue
		}
		row := shadowComparisonRow{ID: id, Name: profile.Name, Policy: profile.Strategy, BaselineID: profile.BaselineID, TrialHash: profile.TrialHash, StartedAt: profile.Config.TradingStartAt, MissingDates: []string{}, Points: []shadowComparisonPoint{}, Warnings: []string{}}
		ledger, err := profile.Archive.Load()
		if err != nil {
			return result, err
		}
		validLedger := len(paper.AuditShadowLedger(ledger)) == 0 && paper.ConfigFingerprint(ledger.Config) == paper.ConfigFingerprint(profile.Config) && !ledger.GeneratedAt.After(now)
		if !validLedger {
			row.Warnings = append(row.Warnings, "当前账本或冻结配置校验未通过")
		}
		firstSession := ""
		for _, date := range calendar {
			day, _ := time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
			if profile.Config.TradingStartAt.Before(day.Add(15 * time.Hour)) {
				firstSession = date
				break
			}
		}
		for _, date := range dates {
			day, _ := time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
			if !profile.Config.TradingStartAt.Before(day.Add(15*time.Hour)) || now.Before(day.Add(15*time.Hour+30*time.Minute)) {
				continue
			}
			row.ExpectedDays++
			point := shadowComparisonPoint{Date: date}
			snapshot, ok := snapshots[date][id]
			switch {
			case badDates[date]:
				point.Reason = "档案校验失败"
			case !ok:
				point.Reason = "未留存快照"
			case !snapshot.Sealed:
				point.Reason = "尚未封存"
			case !validLedger || snapshot.Revision != ledger.LedgerRevision || snapshot.TrialHash != profile.TrialHash || snapshot.Engine != paper.ShadowEngineVersion || snapshot.ConfigHash != paper.ConfigFingerprint(profile.Config):
				point.Reason = "修订或冻结配置不一致"
			case !snapshot.Fresh || len(snapshot.LedgerIssues) != 0 || snapshot.GeneratedAt.After(now) || snapshot.GeneratedAt.Before(day.Add(15*time.Hour)) || snapshot.GeneratedAt.In(realtimeWebLocation).Format(time.DateOnly) != date:
				point.Reason = "估值或记录时点无效"
			case snapshot.Config.InitialCash <= 0 || math.Abs(snapshot.Equity-snapshot.Cash-snapshot.MarketValue) > .02 || math.Abs(snapshot.Return-(snapshot.Equity/snapshot.Config.InitialCash-1)*100) > 1e-6:
				point.Reason = "权益恒等式不一致"
			default:
				equity, value := snapshot.Equity, snapshot.Return
				point.Equity, point.Return = &equity, &value
				row.SnapshotDays++
				row.AsOf, row.Return = date, &value
			}
			if point.Reason != "" {
				row.MissingDates = append(row.MissingDates, date)
			}
			row.Points = append(row.Points, point)
		}
		if row.SnapshotDays >= 2 && len(row.MissingDates) == 0 && result.CalendarKnown {
			peak := 0.0
			if firstSession != "" && start <= firstSession {
				peak = profile.Config.InitialCash
			}
			value := comparisonDrawdown(row.Points, peak)
			row.MaxDrawdown = &value
		}
		if validLedger && row.AsOf != "" && ledger.AsOf >= row.AsOf {
			row.Activity = comparisonActivity(ledger, start, row.AsOf)
		}
		switch {
		case !validLedger || len(row.MissingDates) > 0:
			row.Status = "数据待补齐"
		case row.SnapshotDays == 0:
			row.Status = "尚无收盘样本"
		case row.Activity == nil || row.Activity.ClosedTrades == 0:
			row.Status = "暂无平仓样本"
		case row.SnapshotDays < 10:
			row.Status = "观察天数不足"
		default:
			row.Status = "持续观察中"
		}
		result.Rows = append(result.Rows, row)
	}
	byID := map[string]shadowComparisonRow{}
	for _, row := range result.Rows {
		byID[row.ID] = row
	}
	for i := range result.Rows {
		row := &result.Rows[i]
		if baseline, ok := byID[row.BaselineID]; ok && baseline.TrialHash == row.TrialHash && baseline.StartedAt.Equal(row.StartedAt) && baseline.AsOf == row.AsOf && row.Return != nil && baseline.Return != nil {
			value := *row.Return - *baseline.Return
			row.ReturnDifference = &value
		}
	}
	return result, nil
}

func comparisonDrawdown(points []shadowComparisonPoint, peak float64) float64 {
	maxDrawdown := 0.0
	for _, point := range points {
		if point.Equity == nil {
			continue
		}
		value := *point.Equity
		if value > peak {
			peak = value
		}
		if peak > 0 {
			maxDrawdown = math.Max(maxDrawdown, (peak-value)/peak*100)
		}
	}
	return maxDrawdown
}

func comparisonActivity(report paper.Report, start, end string) *shadowComparisonActivity {
	activity := &shadowComparisonActivity{}
	day, _ := time.Parse(time.DateOnly, start)
	for date := start; date <= end; date = day.Format(time.DateOnly) {
		review := paper.ShadowDailyReviewForDate(report, date)
		if review != nil {
			activity.Orders += review.OrderCount
			activity.Fees += review.Fees
			activity.RealizedProfit += review.RealizedProfit
		}
		day = day.AddDate(0, 0, 1)
	}
	wins := 0
	for _, trade := range report.Trades {
		if trade.ExitDate >= start && trade.ExitDate <= end {
			activity.ClosedTrades++
			if trade.NetProfit > 0 {
				wins++
			}
		}
	}
	if activity.ClosedTrades > 0 {
		value := float64(wins) / float64(activity.ClosedTrades) * 100
		activity.WinRate = &value
	}
	return activity
}

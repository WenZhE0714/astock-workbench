package strategy

import (
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func addPatternCoverageDiagnostics(report *domain.PatternValidationReport, clean map[string][]domain.DailyBar) {
	benchmark := clean["sh000300"]
	dates := []string{}
	for _, bar := range benchmark {
		if bar.Date >= report.Request.Start && bar.Date <= report.Cutoff {
			dates = append(dates, bar.Date)
		}
	}
	for i := range report.Coverage {
		coverage := &report.Coverage[i]
		coverage.Status = "unavailable"
		coverage.WarmupRequired = classicLookback
		if coverage.Symbol == "sh000300" {
			coverage.WarmupRequired = 60
		}
		available := map[string]bool{}
		for _, bar := range clean[coverage.Symbol] {
			if bar.Date < report.Request.Start {
				coverage.WarmupBars++
			} else {
				available[bar.Date] = true
			}
		}
		if coverage.WarmupBars < coverage.WarmupRequired {
			coverage.Notes = append(coverage.Notes, fmt.Sprintf("起始预热不足：%d / %d根", coverage.WarmupBars, coverage.WarmupRequired))
		}
		if len(dates) == 0 {
			coverage.Notes = append(coverage.Notes, "所选区间没有基准交易日，无法核对覆盖率")
			continue
		}
		expected := len(dates)
		coverage.ExpectedDays = &expected
		coverage.ReferenceStart, coverage.ReferenceEnd = dates[0], dates[len(dates)-1]
		for _, date := range dates {
			if available[date] {
				coverage.AvailableDays++
			} else {
				coverage.MissingDates = append(coverage.MissingDates, date)
			}
		}
		coverage.Status = "aligned"
		if len(coverage.MissingDates) > 0 || coverage.Error != "" {
			coverage.Status = "partial"
			coverage.Notes = append(coverage.Notes, "缺失日期原因未确认，可能涉及停牌、上市区间或数据源缺漏；未用零值或前值补齐")
		}
		if coverage.Symbol == "sh000300" {
			coverage.Status = "benchmark"
			coverage.Notes = append(coverage.Notes, "以本次取得的指数日K日期为参照，尚未接入独立交易日历核验")
		}
		start, _ := time.Parse(time.DateOnly, report.Request.Start)
		end, _ := time.Parse(time.DateOnly, report.Cutoff)
		if coverage.ReferenceStart > start.AddDate(0, 0, 14).Format(time.DateOnly) || coverage.ReferenceEnd < end.AddDate(0, 0, -14).Format(time.DateOnly) {
			coverage.Notes = append(coverage.Notes, "基准起止覆盖不足，所选区间的交易日总数尚不完整")
		} else if coverage.ReferenceStart != report.Request.Start || coverage.ReferenceEnd != report.Cutoff {
			coverage.Notes = append(coverage.Notes, "基准起止与请求边界不同，可能为非交易日；仅在基准实际覆盖范围内核对")
		}
	}
}

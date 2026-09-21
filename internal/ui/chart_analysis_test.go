package ui

import (
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestChartAnalysisCLIRendersSameLevelsAndExplicitStates(t *testing.T) {
	pivot := 49.0
	analysis := domain.ChartAnalysis{
		Levels:     []domain.ChartLevel{{Key: "pivot", Label: "前日枢轴", Value: &pivot}, {Key: "gap", Label: "未回补缺口"}},
		Structures: []domain.ChartStructure{{Name: "区间突破", State: "forming"}},
		Weekly:     domain.ChartWeeklyTrend{State: "bullish", DataDate: "2026-09-11", Weeks: 15},
	}
	text := strings.Join(chartAnalysisLines(analysis, 120, false), "\n")
	for _, value := range []string{"前日枢轴 49.00", "未回补缺口 无", "区间突破 待确认", "完整周线", "偏多", "2026-09-11"} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %q in %s", value, text)
		}
	}
}

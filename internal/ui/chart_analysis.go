package ui

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func chartAnalysisLines(analysis domain.ChartAnalysis, width int, color bool) []string {
	levels := make([]string, 0, len(analysis.Levels))
	for _, item := range analysis.Levels {
		if item.Key != "pivot" && item.Key != "gap" {
			continue
		}
		value := "--"
		if item.Key == "gap" && item.Value == nil {
			value = "无"
		}
		if item.Value != nil {
			value = fmt.Sprintf("%.2f", *item.Value)
			if item.Upper != nil {
				value += fmt.Sprintf(" / %.2f", *item.Upper)
			}
		}
		levels = append(levels, item.Label+" "+value)
	}
	states := map[string]string{"watching": "观察中", "forming": "待确认", "confirmed": "日线已确认", "invalidated": "结构失效"}
	for _, item := range analysis.Structures {
		levels = append(levels, item.Name+" "+states[item.State])
	}
	lines := labeledTechnicalLines("结构观察", strings.Join(levels, "  ·  "), width, color)
	weekly := map[string]string{"bullish": "偏多", "bearish": "偏空", "sideways": "整理", "insufficient": "样本不足"}
	if analysis.Weekly.DataDate != "" {
		lines = append(lines, labeledTechnicalLines("完整周线", fmt.Sprintf("%s  ·  截至 %s  ·  %d周", weekly[analysis.Weekly.State], analysis.Weekly.DataDate, analysis.Weekly.Weeks), width, color)...)
	}
	return lines
}

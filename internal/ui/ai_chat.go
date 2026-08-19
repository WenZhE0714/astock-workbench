package ui

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func BuildAIChatFrame(symbol, name string, turns []domain.AIChatTurn, controls string, terminalWidth int, moyu bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	code := symbol
	if len(code) == 8 {
		code = code[2:]
	}
	title := fmt.Sprintf("ASTOCK AI CHAT HISTORY  %s %s  CODEX AGENT", code, name)
	if !moyu {
		title = fmt.Sprintf("ASTOCK AI 咨询历史  ·  %s %s  ·  Codex Agent", code, name)
	}
	lines := []string{truncateWidth(title, terminalWidth), truncateWidth(controls, terminalWidth), ""}
	for index := len(turns) - 1; index >= 0; index-- {
		turn := turns[index]
		questionTitle := fmt.Sprintf("## YOU  %s", turn.AskedAt.Format("15:04:05"))
		answerTitle := "## AGENT"
		if !moyu {
			questionTitle = fmt.Sprintf("## 你  %s", turn.AskedAt.Format("15:04:05"))
			answerTitle = "## AI"
		}
		if !turn.FactsAt.IsZero() {
			successful := 0
			for _, run := range turn.Agents {
				if run.Status == "ok" {
					successful++
				}
			}
			if moyu {
				answerTitle = fmt.Sprintf("## AGENT  FACTS %s  ROLES %d/%d", turn.FactsAt.Format("15:04:05"), successful, len(turn.Agents))
			} else {
				answerTitle = fmt.Sprintf("## AI  数据 %s  ·  多Agent %d/%d", turn.FactsAt.Format("15:04:05"), successful, len(turn.Agents))
			}
		}
		lines = append(lines, wrapReportMarkdown(questionTitle, terminalWidth)...)
		lines = append(lines, wrapReportMarkdown(turn.Question, terminalWidth)...)
		lines = append(lines, "")
		lines = append(lines, wrapReportMarkdown(answerTitle, terminalWidth)...)
		lines = append(lines, wrapReportMarkdown(turn.Answer, terminalWidth)...)
		if index != 0 {
			lines = append(lines, "", strings.Repeat("-", min(terminalWidth, 48)), "")
		}
	}
	return strings.Join(lines, "\n")
}

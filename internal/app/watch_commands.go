package app

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type watchCommandKind int

const (
	watchCommandNone watchCommandKind = iota
	watchCommandAdd
	watchCommandDelete
	watchCommandJump
	watchCommandHistory
	watchCommandRanking
	watchCommandFundMonitor
	watchCommandGroupCreate
	watchCommandGroupDelete
	watchCommandAIChat
)

type watchCommand struct {
	kind              watchCommandKind
	buffer            string
	confirm           bool
	symbol            string
	name              string
	group             string
	candidates        []domain.Candidate
	candidateSelected int
}

func (command *watchCommand) active() bool {
	return command.kind != watchCommandNone
}

func (command *watchCommand) begin(kind watchCommandKind) {
	*command = watchCommand{kind: kind}
}

func (command *watchCommand) confirmDelete(symbol, name, group string) {
	command.begin(watchCommandDelete)
	command.symbol = symbol
	command.name = name
	command.group = group
	command.confirm = true
}

func (command *watchCommand) confirmGroupDelete(name string) {
	command.begin(watchCommandGroupDelete)
	command.name = name
	command.confirm = true
}

func (command *watchCommand) reset() {
	*command = watchCommand{}
}

func (command *watchCommand) chooseCandidates(input string, candidates []domain.Candidate) {
	command.buffer = input
	command.candidates = append([]domain.Candidate(nil), candidates...)
	command.candidateSelected = 0
}

func (command *watchCommand) choosing() bool {
	return len(command.candidates) > 0
}

func (command *watchCommand) moveCandidate(delta int) {
	if !command.choosing() {
		return
	}
	command.candidateSelected += delta
	if command.candidateSelected < 0 {
		command.candidateSelected = 0
	}
	if command.candidateSelected >= len(command.candidates) {
		command.candidateSelected = len(command.candidates) - 1
	}
}

func (command *watchCommand) selectCandidate(index int) {
	if !command.choosing() {
		return
	}
	command.candidateSelected = index
	command.moveCandidate(0)
}

func (command *watchCommand) selectedCandidate() (domain.Candidate, bool) {
	if !command.choosing() || command.candidateSelected < 0 || command.candidateSelected >= len(command.candidates) {
		return domain.Candidate{}, false
	}
	return command.candidates[command.candidateSelected], true
}

func (command *watchCommand) candidateWindow() (int, int) {
	const candidateWindowSize = 10
	if len(command.candidates) <= candidateWindowSize {
		return 0, len(command.candidates)
	}
	start := command.candidateSelected - candidateWindowSize/2
	if start < 0 {
		start = 0
	}
	if start+candidateWindowSize > len(command.candidates) {
		start = len(command.candidates) - candidateWindowSize
	}
	return start, start + candidateWindowSize
}

func (command *watchCommand) status(moyu, cursorVisible bool) string {
	if !command.active() {
		return ""
	}
	if command.confirm {
		if command.kind == watchCommandGroupDelete {
			if moyu {
				return "DELETE GROUP " + command.name + " ? UNIQUE STOCKS MOVE TO DEFAULT"
			}
			return "确认删除分组“" + command.name + "”？独有股票将移到默认分组"
		}
		if moyu {
			return "DELETE " + command.symbol[2:] + " " + command.name + " ?"
		}
		if command.group != "" && command.group != storage.AllWatchlistGroup && command.group != temporaryWatchlistGroup {
			return "确认从“" + command.group + "”移出 " + command.symbol[2:] + " " + command.name + "？"
		}
		return "确认删除 " + command.symbol[2:] + " " + command.name + "？"
	}
	if command.choosing() {
		var builder strings.Builder
		start, end := command.candidateWindow()
		if command.kind == watchCommandHistory {
			if moyu {
				builder.WriteString("RECENTLY VIEWED (NEWEST FIRST):")
				if len(command.candidates) > end-start {
					fmt.Fprintf(&builder, " %d-%d/%d", start+1, end, len(command.candidates))
				}
			} else {
				builder.WriteString("最近查看（最新在前）")
				if len(command.candidates) > end-start {
					fmt.Fprintf(&builder, " %d-%d/%d", start+1, end, len(command.candidates))
				}
				builder.WriteString("：")
			}
		} else if moyu {
			fmt.Fprintf(&builder, "SELECT MATCH FOR %s:", command.buffer)
			if len(command.candidates) > end-start {
				fmt.Fprintf(&builder, " %d-%d/%d", start+1, end, len(command.candidates))
			}
		} else {
			fmt.Fprintf(&builder, "名称“%s”匹配到多个证券或板块，请选择：", command.buffer)
			if len(command.candidates) > end-start {
				fmt.Fprintf(&builder, "（%d-%d/%d）", start+1, end, len(command.candidates))
			}
		}
		for index := start; index < end; index++ {
			candidate := command.candidates[index]
			marker := "  "
			if index == command.candidateSelected {
				marker = "> "
			}
			fmt.Fprintf(&builder, "\n%s%s  %s  %s", marker, candidateDisplayCode(candidate.Symbol), candidate.Name, market.MarketText(candidate.Symbol))
		}
		return builder.String()
	}
	prefix := ""
	switch command.kind {
	case watchCommandAdd:
		prefix = "添加自选，请输入代码或完整名称："
	case watchCommandJump:
		prefix = "查看详情，请输入代码或完整名称："
	case watchCommandGroupCreate:
		prefix = "新建分组，请输入名称："
	case watchCommandAIChat:
		prefix = "咨询AI（自动带入当前股票行情、资金、板块与技术面）："
	}
	if moyu {
		switch command.kind {
		case watchCommandAdd:
			prefix = "ADD CODE/NAME: "
		case watchCommandJump:
			prefix = "VIEW CODE/NAME: "
		case watchCommandGroupCreate:
			prefix = "NEW GROUP NAME: "
		case watchCommandAIChat:
			prefix = "ASK AI WITH LIVE STOCK CONTEXT: "
		}
	}
	cursor := " "
	if cursorVisible {
		cursor = "▌"
	}
	return prefix + command.buffer + cursor
}

func (command *watchCommand) controls(moyu bool) string {
	if !command.active() {
		return ""
	}
	if command.confirm {
		if moyu {
			return "ENTER/Y CONFIRM  ESC/N CANCEL"
		}
		return "Enter/y确认  Esc/n取消"
	}
	if command.choosing() {
		if command.kind == watchCommandHistory {
			if moyu {
				return "UP/DOWN SELECT  [/]/PGUP/PGDN JUMP  ENTER OPEN  ESC CANCEL"
			}
			return "↑/↓ 选择  [/]跳选  Enter打开  Esc取消"
		}
		if moyu {
			return "↑/↓ SELECT  [/] PAGE  ENTER CONFIRM  ESC CANCEL"
		}
		return "↑/↓ 选择  [/]翻页  Enter确认  Esc取消"
	}
	if moyu {
		return "ENTER CONFIRM  ESC CANCEL"
	}
	return "Enter确认  Esc取消"
}

func removeLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func commandText(value string) string {
	return strings.TrimSpace(value)
}

func candidateDisplayCode(symbol string) string {
	if len(symbol) >= 2 {
		prefix := strings.ToLower(symbol[:2])
		if prefix == "sh" || prefix == "sz" || prefix == "th" {
			return symbol[2:]
		}
	}
	return symbol
}

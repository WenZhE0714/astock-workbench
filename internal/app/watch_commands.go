package app

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

type watchCommandKind int

const (
	watchCommandNone watchCommandKind = iota
	watchCommandAdd
	watchCommandDelete
	watchCommandJump
)

type watchCommand struct {
	kind              watchCommandKind
	buffer            string
	confirm           bool
	symbol            string
	name              string
	candidates        []domain.Candidate
	candidateSelected int
}

func (command *watchCommand) active() bool {
	return command.kind != watchCommandNone
}

func (command *watchCommand) begin(kind watchCommandKind) {
	*command = watchCommand{kind: kind}
}

func (command *watchCommand) confirmDelete(symbol, name string) {
	command.begin(watchCommandDelete)
	command.symbol = symbol
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

func (command *watchCommand) status(moyu, cursorVisible bool) string {
	if !command.active() {
		return ""
	}
	if command.confirm {
		if moyu {
			return "DELETE " + command.symbol[2:] + " " + command.name + " ?"
		}
		return "确认删除 " + command.symbol[2:] + " " + command.name + "？"
	}
	if command.choosing() {
		var builder strings.Builder
		if moyu {
			fmt.Fprintf(&builder, "SELECT MATCH FOR %s:", command.buffer)
		} else {
			fmt.Fprintf(&builder, "名称“%s”匹配到多只沪深 A 股，请选择：", command.buffer)
		}
		for index, candidate := range command.candidates {
			marker := "  "
			if index == command.candidateSelected {
				marker = "> "
			}
			fmt.Fprintf(&builder, "\n%s%s  %s  %s", marker, candidate.Symbol[2:], candidate.Name, market.MarketText(candidate.Symbol))
		}
		return builder.String()
	}
	prefix := ""
	switch command.kind {
	case watchCommandAdd:
		prefix = "添加自选，请输入代码或完整名称："
	case watchCommandJump:
		prefix = "查看详情，请输入代码或完整名称："
	}
	if moyu {
		switch command.kind {
		case watchCommandAdd:
			prefix = "ADD CODE/NAME: "
		case watchCommandJump:
			prefix = "VIEW CODE/NAME: "
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
		if moyu {
			return "↑/↓ SELECT  ENTER CONFIRM  ESC CANCEL"
		}
		return "↑/↓ 选择  Enter确认  Esc取消"
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

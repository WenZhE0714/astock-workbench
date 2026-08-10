package market

import (
	"math"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestParseAndSelectBoardMemberships(t *testing.T) {
	raw := `{"ssbk":[
		{"BOARD_CODE":"438","BOARD_NAME":"食品饮料","BOARD_RANK":1,"IS_PRECISE":"0"},
		{"BOARD_CODE":"1277","BOARD_NAME":"白酒Ⅱ","BOARD_RANK":2,"IS_PRECISE":null},
		{"BOARD_CODE":"1575","BOARD_NAME":"白酒Ⅲ","BOARD_RANK":3,"IS_PRECISE":null},
		{"BOARD_CODE":"173","BOARD_NAME":"贵州板块","BOARD_RANK":4,"IS_PRECISE":"0"},
		{"BOARD_CODE":"1653","BOARD_NAME":"味蕾经济","BOARD_RANK":23,"IS_PRECISE":"1"},
		{"BOARD_CODE":"896","BOARD_NAME":"白酒","BOARD_RANK":24,"IS_PRECISE":"1"},
		{"BOARD_CODE":"811","BOARD_NAME":"超级品牌","BOARD_RANK":25,"IS_PRECISE":"1"},
		{"BOARD_CODE":"683","BOARD_NAME":"央国企改革","BOARD_RANK":26,"IS_PRECISE":"1"},
		{"BOARD_CODE":"665","BOARD_NAME":"电商概念","BOARD_RANK":27,"IS_PRECISE":"1"}
	]}`
	selected := selectBoardMemberships(ParseBoardMembershipPayload(raw))
	if len(selected) != 6 {
		t.Fatalf("expected six selected boards, got %#v", selected)
	}
	wantCodes := []string{"BK0438", "BK1277", "BK1575", "BK1653", "BK0896", "BK0811"}
	for index, code := range wantCodes {
		if selected[index].Code != code {
			t.Fatalf("board %d: got %s want %s", index, selected[index].Code, code)
		}
	}
	if selected[0].Kind != domain.BoardKindIndustry || selected[3].Kind != domain.BoardKindConcept {
		t.Fatalf("unexpected board kinds: %#v", selected)
	}
}

func TestParseBoardFlowPayload(t *testing.T) {
	raw := `{"data":{"diff":[{"f12":"BK0438","f14":"食品饮料","f3":2.25,"f62":1199132256,"f184":4.62,"f128":"金达威","f140":"002626","f136":10.03},{"f12":"BK0896","f14":"白酒","f3":"-","f62":null,"f184":"-","f128":"-","f140":"-","f136":"-"}]}}`
	flows := ParseBoardFlowPayload(raw)
	food := flows["BK0438"]
	if food.Name != "食品饮料" || food.Percent != 2.25 || food.MainNet != 1199132256 || food.MainRatio != 4.62 || food.LeaderName != "金达威" || food.LeaderPercent != 10.03 {
		t.Fatalf("unexpected food board: %#v", food)
	}
	whiteWine := flows["BK0896"]
	if !math.IsNaN(whiteWine.Percent) || !math.IsNaN(whiteWine.MainNet) || !math.IsNaN(whiteWine.MainRatio) {
		t.Fatalf("unavailable board values should be NaN: %#v", whiteWine)
	}
}

func TestBoardAddresses(t *testing.T) {
	if got := eastmoneyF10Code("sh600519"); got != "SH600519" {
		t.Fatalf("unexpected F10 code: %s", got)
	}
	address := boardFlowAddress("https://example.test?ids={secids}", []boardMembership{{Code: "BK0438"}, {Code: "BK0896"}})
	if address != "https://example.test?ids=90.BK0438%2C90.BK0896" {
		t.Fatalf("unexpected board flow address: %s", address)
	}
}

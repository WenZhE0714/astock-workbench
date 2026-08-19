package app

import (
	"fmt"
	"math"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type watchBoardDetail struct {
	viewing    bool
	loading    bool
	symbol     string
	flow       domain.BoardFlow
	leaders    []domain.MarketStockSnapshot
	refreshed  time.Time
	refreshErr string
}

func (state *watchBoardDetail) open(symbol string) {
	state.viewing = true
	state.loading = true
	state.symbol = symbol
	state.flow = domain.BoardFlow{
		Percent: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(),
		Turnover: math.NaN(), LeaderPercent: math.NaN(),
	}
	state.leaders = nil
	state.refreshed = time.Time{}
	state.refreshErr = ""
}

func (state *watchBoardDetail) complete(flow domain.BoardFlow, leaders []domain.MarketStockSnapshot) {
	state.loading = false
	state.flow = flow
	state.leaders = leaders
	state.refreshed = time.Now()
	state.refreshErr = ""
}

func (state *watchBoardDetail) fail(err error) {
	state.loading = false
	if err == nil {
		state.refreshErr = "板块行情不可用"
		return
	}
	state.refreshErr = err.Error()
}

func (state *watchBoardDetail) close() {
	state.viewing = false
}

func (state watchBoardDetail) name() string {
	if state.flow.Name != "" {
		return state.flow.Name
	}
	return state.symbol
}

func (state watchBoardDetail) status(moyu bool) string {
	if state.loading {
		if moyu {
			return "LOADING INDUSTRY BOARD; QUOTES KEEP REFRESHING"
		}
		return "正在加载同花顺行业板块，股票行情继续刷新"
	}
	if state.refreshErr != "" {
		return "板块查看失败: " + state.refreshErr
	}
	if !state.refreshed.IsZero() && math.IsNaN(state.flow.Percent) {
		return "板块行情字段不完整，数据源暂不可用"
	}
	return ""
}

func (state watchBoardDetail) controls(moyu bool) string {
	if moyu {
		return "UP/DOWN SCROLL  [/] PAGE  g/G TOP/END  ESC BACK"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  Esc返回"
}

func (state watchBoardDetail) title() string {
	return fmt.Sprintf("%s  %s", state.name(), state.symbol)
}

package app

import (
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const globalMarketRefreshInterval = 30 * time.Second

type watchGlobalMarkets struct {
	viewing      bool
	loading      bool
	indices      []domain.GlobalIndex
	refreshedAt  time.Time
	refreshError string
}

func (state *watchGlobalMarkets) open() {
	state.viewing = true
}

func (state *watchGlobalMarkets) close() {
	state.viewing = false
}

func (state *watchGlobalMarkets) beginRefresh() {
	state.loading = true
	state.refreshError = ""
}

func (state *watchGlobalMarkets) complete(indices []domain.GlobalIndex) {
	state.loading = false
	state.indices = append([]domain.GlobalIndex(nil), indices...)
	state.refreshedAt = time.Now()
	state.refreshError = ""
}

func (state *watchGlobalMarkets) fail(err error) {
	state.loading = false
	if err == nil {
		state.refreshError = "外盘指数暂不可用"
		return
	}
	state.refreshError = err.Error()
}

func (state watchGlobalMarkets) status(moyu bool) string {
	if state.loading {
		if moyu {
			return "REFRESHING GLOBAL MARKETS; KEEPING A-SHARE QUOTES LIVE"
		}
		return "正在刷新外盘指数，A股行情继续在后台刷新"
	}
	if state.refreshError == "" {
		return ""
	}
	prefix := "外盘指数刷新失败"
	if !state.refreshedAt.IsZero() {
		prefix += "，保留 " + state.refreshedAt.Format("15:04:05") + " 数据"
	}
	if moyu {
		prefix = "GLOBAL MARKET REFRESH FAILED"
	}
	return fmt.Sprintf("%s: %s", prefix, state.refreshError)
}

func (state watchGlobalMarkets) controls(moyu bool) string {
	if moyu {
		return "UP/DOWN SCROLL  [/]/PGUP/PGDN PAGE  W REFRESH  ESC BACK  Q QUIT"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  w刷新  Esc返回  q退出"
}

package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	boardFundBoardLimit      = 5
	boardFundLeaderLimit     = 3
	boardFundRefreshInterval = time.Minute
	boardFundLeaderWorkers   = 4
)

type watchBoardFunds struct {
	viewing      bool
	loading      bool
	refreshedAt  time.Time
	inflows      []domain.BoardFundRankingItem
	outflows     []domain.BoardFundRankingItem
	warnings     []string
	refreshError string
}

func (state *watchBoardFunds) open() {
	state.viewing = true
}

func (state *watchBoardFunds) close() {
	state.viewing = false
}

func (state *watchBoardFunds) beginRefresh() {
	state.loading = true
	state.refreshError = ""
}

func (state *watchBoardFunds) complete(dashboard domain.BoardFundDashboard) {
	state.loading = false
	state.refreshedAt = dashboard.RefreshedAt
	state.inflows = dashboard.Inflows
	state.outflows = dashboard.Outflows
	state.warnings = dashboard.Warnings
	state.refreshError = ""
}

func (state *watchBoardFunds) fail(err error) {
	state.loading = false
	state.refreshError = err.Error()
}

func (state watchBoardFunds) dashboard() domain.BoardFundDashboard {
	return domain.BoardFundDashboard{
		RefreshedAt: state.refreshedAt,
		Inflows:     state.inflows,
		Outflows:    state.outflows,
		Warnings:    state.warnings,
	}
}

func (state watchBoardFunds) status(moyu bool) string {
	if state.loading {
		if moyu {
			return "LOADING BOARD FUNDS; QUOTES KEEP REFRESHING"
		}
		return "正在加载板块资金看板，行情继续刷新"
	}
	if state.refreshError != "" {
		prefix := "板块资金刷新失败"
		if !state.refreshedAt.IsZero() {
			prefix += "，保留 " + state.refreshedAt.Format("15:04:05") + " 数据"
		}
		if moyu {
			prefix = "BOARD FUND REFRESH FAILED"
		}
		return prefix + ": " + state.refreshError
	}
	if len(state.warnings) > 0 {
		if moyu {
			return fmt.Sprintf("%d BOARD LEADER LISTS UNAVAILABLE", len(state.warnings))
		}
		return fmt.Sprintf("%d 个板块的成交额龙头暂不可用，其余数据已保留", len(state.warnings))
	}
	return ""
}

func (state watchBoardFunds) controls(moyu bool) string {
	if moyu {
		return "UP/DOWN SCROLL  [/]/PGUP/PGDN PAGE  G/G ENDPOINTS  Y REFRESH  T STRATEGY  ESC BACK  Q QUIT"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  y刷新  t策略研究  Esc返回  q退出"
}

type boardFundRankResult struct {
	items []domain.BoardFlow
	err   error
}

type boardFundLeaderJob struct {
	inflow bool
	index  int
	board  domain.BoardFlow
}

type boardFundLeaderResult struct {
	boardFundLeaderJob
	leaders []domain.MarketStockSnapshot
	err     error
}

func (app *App) fetchBoardFundDashboard(ctx context.Context) (domain.BoardFundDashboard, error) {
	if app.marketScan == nil {
		return domain.BoardFundDashboard{}, fmt.Errorf("板块资金接口暂不可用")
	}
	if app.industryLeaders == nil {
		return domain.BoardFundDashboard{}, fmt.Errorf("板块成份股接口暂不可用")
	}

	inflowResult := make(chan boardFundRankResult, 1)
	outflowResult := make(chan boardFundRankResult, 1)
	go func() {
		items, err := app.marketScan.FetchIndustryRanking(ctx, domain.MarketScanByMainNet, true, boardFundBoardLimit)
		inflowResult <- boardFundRankResult{items: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchIndustryRanking(ctx, domain.MarketScanByMainNet, false, boardFundBoardLimit)
		outflowResult <- boardFundRankResult{items: items, err: err}
	}()

	inflows := <-inflowResult
	outflows := <-outflowResult
	if inflows.err != nil {
		return domain.BoardFundDashboard{}, fmt.Errorf("流入板块加载失败: %w", inflows.err)
	}
	if outflows.err != nil {
		return domain.BoardFundDashboard{}, fmt.Errorf("流出板块加载失败: %w", outflows.err)
	}
	if len(inflows.items) > boardFundBoardLimit {
		inflows.items = inflows.items[:boardFundBoardLimit]
	}
	if len(outflows.items) > boardFundBoardLimit {
		outflows.items = outflows.items[:boardFundBoardLimit]
	}
	if len(inflows.items) == 0 || len(outflows.items) == 0 {
		return domain.BoardFundDashboard{}, fmt.Errorf("未返回完整的板块资金排名")
	}

	dashboard := domain.BoardFundDashboard{
		RefreshedAt: time.Now(),
		Inflows:     make([]domain.BoardFundRankingItem, len(inflows.items)),
		Outflows:    make([]domain.BoardFundRankingItem, len(outflows.items)),
	}
	jobs := make(chan boardFundLeaderJob)
	results := make(chan boardFundLeaderResult, len(inflows.items)+len(outflows.items))
	workers := boardFundLeaderWorkers
	if total := len(inflows.items) + len(outflows.items); workers > total {
		workers = total
	}
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for job := range jobs {
				leaders, err := app.industryLeaders.FetchIndustryLeaders(ctx, job.board.Code, boardFundLeaderLimit)
				results <- boardFundLeaderResult{boardFundLeaderJob: job, leaders: leaders, err: err}
			}
		}()
	}
	go func() {
		for index, board := range inflows.items {
			jobs <- boardFundLeaderJob{inflow: true, index: index, board: board}
		}
		for index, board := range outflows.items {
			jobs <- boardFundLeaderJob{index: index, board: board}
		}
		close(jobs)
		wait.Wait()
		close(results)
	}()

	for result := range results {
		item := domain.BoardFundRankingItem{Board: result.board, Leaders: result.leaders}
		if len(item.Leaders) > boardFundLeaderLimit {
			item.Leaders = item.Leaders[:boardFundLeaderLimit]
		}
		if result.err != nil {
			dashboard.Warnings = append(dashboard.Warnings, result.board.Name+": "+result.err.Error())
			item.Leaders = nil
		}
		if result.inflow {
			dashboard.Inflows[result.index] = item
		} else {
			dashboard.Outflows[result.index] = item
		}
	}
	return dashboard, nil
}

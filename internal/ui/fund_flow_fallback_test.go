package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

func TestFundFlowFallbackFramesShowPartialCoverageAndSource(t *testing.T) {
	quote := dashboardQuote()
	data := LiveData{
		Quotes: []domain.Quote{quote}, Symbols: []string{quote.Symbol},
		Flows: map[string]domain.FundFlow{
			quote.Symbol: {Symbol: quote.Symbol, Source: market.THSFundFlowSource, MainNet: 4e8, MainRatio: 9, Speed: math.NaN(), FetchedAt: time.Date(2026, 10, 12, 16, 0, 0, 0, time.Local)},
		},
		FlowError: "指数资金暂不可用: sh000001,sz399001,sz399006", MarketStatus: "已收盘",
	}
	for _, width := range []int{60, 79, 120} {
		for _, moyu := range []bool{false, true} {
			frame := BuildLiveFrame(data, ViewOptions{Moyu: moyu}, width, 24)
			for _, expected := range []string{market.THSFundFlowSource, "4.00亿", "指数资金暂不可用"} {
				if !strings.Contains(frame, expected) {
					t.Fatalf("missing %q at width %d:\n%s", expected, width, frame)
				}
			}
			for _, line := range strings.Split(frame, "\n") {
				if displayWidth(line) > width {
					t.Fatalf("line exceeds width %d: %s", width, line)
				}
			}
		}
	}
	frame := BuildSnapshotFrame(data, ViewOptions{}, 79)
	for _, expected := range []string{"大单净额", "大单净占比", "同花顺大单净额", "获取 10-12 16:00:00", "指数资金暂不可用"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("snapshot missing %q:\n%s", expected, frame)
		}
	}
}

func TestFundFlowFallbackRadarShowsPlainAmount(t *testing.T) {
	items := []domain.FundMovement{{Symbol: "sz300364", Name: "中文在线", Source: market.THSFundFlowSource, MainNet: 4e8, State: "采样中", Delta1Minute: math.NaN()}}
	for _, width := range []int{60, 79, 120} {
		frame := buildFundMonitorTable(items, 0, width, false, false)
		if !strings.Contains(frame, "+4.00亿") || strings.Contains(frame, "*") {
			t.Fatalf("radar should show the amount without a source marker: %s", frame)
		}
		for _, line := range strings.Split(frame, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("radar exceeds width %d: %s", width, line)
			}
		}
	}
}

func TestFundSnapshotShowsIndicesAndMinuteSpeedWithDataTime(t *testing.T) {
	quote := dashboardQuote()
	flows := map[string]domain.FundFlow{
		quote.Symbol: {Symbol: quote.Symbol, Source: market.THSFundFlowSource, MainNet: 4e8, Speed: 0.25, SpeedSource: market.THSMinuteSpeedSource, SpeedTime: "2026-10-12 15:00"},
	}
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		flows[symbol] = domain.FundFlow{Symbol: symbol, Source: market.THSFundFlowSource, MainNet: -1e8, Speed: math.NaN(), DataTime: "2026-10-12 15:00"}
	}
	data := LiveData{Quotes: []domain.Quote{quote}, Flows: flows}
	for _, width := range []int{79, 120} {
		frame := BuildLiveFrame(data, ViewOptions{}, width, 24)
		for _, expected := range []string{"+0.25%", "同花顺大单净额", "同花顺分时1分钟", "指数数据 2026-10-12 15:00", "↓ 1.00亿"} {
			if !strings.Contains(frame, expected) {
				t.Fatalf("missing %q:\n%s", expected, frame)
			}
		}
		if strings.Contains(frame, "指数资金暂不可用") {
			t.Fatal("covered indices still reported unavailable")
		}
		if strings.Contains(frame, "*") {
			t.Fatalf("source markers should not appear in the frame: %s", frame)
		}
		for _, line := range strings.Split(frame, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("line exceeds width %d: %s", width, line)
			}
		}
	}
	flow := flows[quote.Symbol]
	flow.Speed = math.NaN()
	flows[quote.Symbol] = flow
	if got := fundSpeedSourceOverview(flows, false); got != "" {
		t.Fatalf("missing speed advertised as available: %s", got)
	}
}

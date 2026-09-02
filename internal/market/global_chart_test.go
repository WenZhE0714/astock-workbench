package market

import (
	"context"
	"errors"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestParseYahooGlobalDailyPayloadUsesExchangeTimezone(t *testing.T) {
	raw := `{"chart":{"result":[{"meta":{"exchangeTimezoneName":"America/New_York"},"timestamp":[1787947200,1788033600,1788120000],"indicators":{"quote":[{"open":[100,101,null],"close":[101,102,null],"high":[102,103,null],"low":[99,100,null],"volume":[1000,1200,null]}]}}],"error":null}}`
	bars := ParseYahooGlobalDailyPayload(raw, "gb_inx")
	if len(bars) != 2 {
		t.Fatalf("unexpected bar count: %#v", bars)
	}
	if bars[0].Symbol != "gb_inx" || bars[0].Source != "Yahoo Finance" || bars[0].Open != 100 || bars[1].Close != 102 {
		t.Fatalf("unexpected Yahoo bars: %#v", bars)
	}
	if bars[0].Date == "" || bars[1].Date <= bars[0].Date {
		t.Fatalf("bars are not chronological: %#v", bars)
	}
}

func TestParseYahooGlobalMinutePayloadKeepsNullRowsOut(t *testing.T) {
	raw := `{"chart":{"result":[{"meta":{"exchangeTimezoneName":"Asia/Tokyo"},"timestamp":[1787947200,1787947500,1787947800],"indicators":{"quote":[{"open":[100,101,102],"close":[101,null,103],"high":[102,102,104],"low":[99,100,101],"volume":[10,12,14]}]}}],"error":null}}`
	points := ParseYahooGlobalMinutePayload(raw, "b_NKY")
	if len(points) != 2 {
		t.Fatalf("unexpected point count: %#v", points)
	}
	if points[0].TradeDate == "" || points[0].Time == "" || points[1].CumulativeVolume != 24 {
		t.Fatalf("unexpected Yahoo minute points: %#v", points)
	}
}

func TestParseEastmoneyGlobalPayloads(t *testing.T) {
	daily := ParseEastmoneyGlobalDailyPayload(`{"data":{"klines":["2026-08-28,100,102,104,99,1000,204000","bad,row"]}}`, "rt_hkHSI")
	if len(daily) != 1 || daily[0].Amount != 204000 || daily[0].Date != "2026-08-28" {
		t.Fatalf("unexpected Eastmoney daily bars: %#v", daily)
	}
	minute := ParseEastmoneyGlobalMinutePayload(`{"data":{"klines":["2026-08-28 09:35,100,101,102,99,10,1005","2026-08-28 09:40,101,100,103,98,8,800"]}}`, "rt_hkHSI")
	if len(minute) != 2 || minute[0].Time != "09:35" || minute[1].CumulativeAmount != 1805 {
		t.Fatalf("unexpected Eastmoney minute points: %#v", minute)
	}
}

type globalChartTestStub struct {
	daily  []domain.DailyBar
	minute []domain.MinutePoint
	err    error
}

func (stub globalChartTestStub) FetchGlobalDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return append([]domain.DailyBar(nil), stub.daily...), nil
}

func (stub globalChartTestStub) FetchGlobalMinutePoints(context.Context, string) ([]domain.MinutePoint, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return append([]domain.MinutePoint(nil), stub.minute...), nil
}

func TestFallbackGlobalChartClientTriesNextSource(t *testing.T) {
	want := []domain.DailyBar{{Symbol: "gb_inx", Date: "2026-08-28", Close: 100}}
	client := NewFallbackGlobalChartClient(
		globalChartTestStub{err: errors.New("primary down")},
		globalChartTestStub{daily: want},
	)
	bars, err := client.FetchGlobalDailyBars(context.Background(), "gb_inx")
	if err != nil || len(bars) != 1 || bars[0].Close != 100 {
		t.Fatalf("fallback did not return secondary source: %#v %v", bars, err)
	}
}

func TestGlobalChartDefinitionIncludesHangSengTechProxy(t *testing.T) {
	definition, ok := GlobalChartDefinitionFor("rt_hkHSTECH")
	if !ok || definition.YahooProxySymbol != "3033.HK" {
		t.Fatalf("missing Hang Seng Tech proxy: %#v %v", definition, ok)
	}
}

func TestGlobalChartDefinitionsIncludeKoreanIndices(t *testing.T) {
	tests := map[string]struct {
		name      string
		yahoo     string
		eastmoney string
	}{
		"b_KOSPI":  {name: "KOSPI", yahoo: "^KS11", eastmoney: "100.KS11"},
		"b_KOSDAQ": {name: "KOSDAQ", yahoo: "^KQ11", eastmoney: "100.KQ11"},
	}
	for symbol, want := range tests {
		definition, ok := GlobalChartDefinitionFor(symbol)
		if !ok || definition.Region != "韩国" || definition.Name != want.name || definition.Timezone != "Asia/Seoul" || definition.YahooSymbol != want.yahoo || definition.EastmoneySecurityID != want.eastmoney {
			t.Fatalf("unexpected Korean chart definition for %s: %#v %v", symbol, definition, ok)
		}
	}
}

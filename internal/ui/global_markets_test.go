package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestGlobalMarketsFrameShowsRegionsIndicesAndSourceTimes(t *testing.T) {
	items := []domain.GlobalIndex{
		{Region: "港股", Name: "恒生指数", Current: 25479.139, Delta: 7.99, Percent: .03, Open: 25346.9, High: 25537.19, Low: 25315.32, QuoteTime: "2026-08-19 15:24:10"},
		{Region: "日本", Name: "日经225", Current: 65326.2, Delta: -2134.53, Percent: -3.16, Open: 66812.27, High: 66833.51, Low: 65133.98, QuoteTime: "2026-08-19 14:12:00"},
		{Region: "韩国", Name: "KOSPI", Current: math.NaN(), Delta: math.NaN(), Percent: math.NaN(), Open: math.NaN(), High: math.NaN(), Low: math.NaN()},
		{
			Region: "美国", Name: "标普500", Current: 7691.76, Delta: -53.3, Percent: -.69,
			Open: 7700.04, High: 7713.95, Low: 7688.63, QuoteTime: "2026-08-19 04:38:44",
			Extended: &domain.GlobalExtendedQuote{Session: "盘后", Symbol: "SPY", Price: 767.08, Delta: -.37, Percent: -.05, QuoteTime: "Aug 18 07:59PM EDT"},
		},
	}
	frame := BuildGlobalMarketsFrame(items, time.Date(2026, 8, 19, 15, 30, 0, 0, time.Local), false, "", "w刷新  Esc返回", 118, false, true)
	for _, expected := range []string{"ASTOCK 外盘指数", "港股", "恒生指数", "日本", "日经225", "韩国", "KOSPI", "美国", "标普500", "2026-08-19 04:38:44", "盘后·SPY代理", "767.08", "Aug 18 07:59PM EDT", "ETF代理", "w刷新"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("global markets frame missing %q:\n%s", expected, frame)
		}
	}
	if !strings.Contains(frame, "\x1b[31m") || !strings.Contains(frame, "\x1b[32m") {
		t.Fatalf("global market directions should use red/green colors:\n%q", frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if displayWidth(line) > 118 {
			t.Fatalf("line exceeds terminal width: %d %q", displayWidth(line), line)
		}
	}
}

func TestGlobalMarketsFrameUsesCompactColumns(t *testing.T) {
	frame := BuildGlobalMarketsFrame([]domain.GlobalIndex{{
		Region: "美国", Name: "纳斯达克", Current: 26289.71, Delta: -355.2, Percent: -1.33,
		QuoteTime: "2026-08-19 05:30:00",
	}}, time.Time{}, false, "", "Esc返回", 52, false, false)
	if !strings.Contains(frame, "纳斯达克") || !strings.Contains(frame, "05:30:00") || strings.Contains(frame, "今开") {
		t.Fatalf("unexpected compact global frame:\n%s", frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if displayWidth(line) > 52 {
			t.Fatalf("compact line exceeds terminal width: %d %q", displayWidth(line), line)
		}
	}
}

func TestGlobalMarketTableSeparatesRegionsAndDoesNotRepeatMarketLabel(t *testing.T) {
	items := []domain.GlobalIndex{
		{Region: "港股", Name: "恒生指数", Current: 25491.24, Delta: 20.09, Percent: .08},
		{Region: "港股", Name: "恒生国企", Current: 8474.67, Delta: 21.47, Percent: .25},
		{Region: "日本", Name: "日经225", Current: 65326.2, Delta: -2134.53, Percent: -3.16},
		{Region: "日本", Name: "东证指数", Current: 4012.31, Delta: -127.91, Percent: -3.09},
		{Region: "韩国", Name: "KOSPI", Current: 6471.17, Delta: -398.66, Percent: -5.8},
		{Region: "美国", Name: "道琼斯", Current: 53343.4, Delta: -116.38, Percent: -.22},
	}
	lines := globalMarketTable(items, 118, false, true)
	plain := ansiPattern.ReplaceAllString(strings.Join(lines, "\n"), "")
	for _, region := range []string{"港股", "日本", "韩国", "美国"} {
		if strings.Count(plain, region) != 1 {
			t.Fatalf("region %s should appear once per group:\n%s", region, plain)
		}
	}
	if strings.Count(plain, "\n\n") != 3 {
		t.Fatalf("expected one blank separator between four markets:\n%s", plain)
	}
	for _, code := range []string{"\x1b[1;36m", "\x1b[1;35m", "\x1b[1;34m", "\x1b[1;33m"} {
		if !strings.Contains(strings.Join(lines, "\n"), code) {
			t.Fatalf("missing market group color %q", code)
		}
	}
}

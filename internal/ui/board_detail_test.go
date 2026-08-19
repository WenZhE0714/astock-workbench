package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestBoardDetailFrameShowsTHSIndustryAndLeaders(t *testing.T) {
	frame := BuildBoardDetailFrame(domain.BoardFlow{
		Code: "th881155", Name: "银行", Kind: domain.BoardKindIndustry,
		Percent: 1.22, MainNet: 27.9e8, MainRatio: math.NaN(), Turnover: math.NaN(), RiseCount: 41,
	}, []domain.MarketStockSnapshot{{
		Symbol: "sh601998", Name: "中信银行", Price: 8.08, Percent: 3.84, Amount: 3.33e8,
	}}, "th881155", "", "Esc返回", 79, false, false)
	for _, expected := range []string{"同花顺行业板块", "银行", "881155", "+1.22%", "27.90亿", "上涨 41", "下跌 0", "涨幅排序前十", "中信银行", "601998", "3.33亿", "Esc返回"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("board detail missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if displayWidth(line) > 79 {
			t.Fatalf("line exceeds terminal width: %q", line)
		}
	}
}

func TestBoardDetailWideFrameShowsConstituentDimensions(t *testing.T) {
	frame := BuildBoardDetailFrame(domain.BoardFlow{
		Code: "th881155", Name: "银行", Percent: 1.22, MainNet: 27.9e8,
		Quote:      &domain.BoardQuoteSnapshot{Price: 1332.93, Delta: 12.71, Open: 1324.4, PreviousClose: 1320.22, High: 1340.48, Low: 1324.4, Volume: 3182.86, Amount: 253.9e8},
		ChangeRank: 3, UniverseSize: 90, RiseCount: 39, FallCount: 1,
	}, []domain.MarketStockSnapshot{{
		Symbol: "sh601998", Name: "中信银行", Price: 8.08, Percent: 3.84,
		Speed: -.37, Turnover: .17, VolumeRatio: 2.52, Amount: 5.56e8,
	}}, "th881155", "", "Esc返回", 120, false, false)
	for _, expected := range []string{"板块概览", "1332.93", "+12.71", "253.90亿", "3/90", "涨速", "换手", "量比", "-0.37%", "2.52", "5.56亿"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("wide board detail missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if displayWidth(line) > 120 {
			t.Fatalf("wide line exceeds terminal width: %q", line)
		}
	}
}

func TestBoardDetailWideFrameColumnsAlignWithChineseNames(t *testing.T) {
	leaders := []domain.MarketStockSnapshot{
		{Symbol: "sh601998", Name: "中信银行", Price: 8.14, Percent: 4.22, Speed: .12, Turnover: .2, VolumeRatio: 2.25, Amount: 6.56e8},
		{Symbol: "sh601229", Name: "上海银行股份", Price: 9.76, Percent: 2.41, Speed: -.1, Turnover: .76, VolumeRatio: 2.07, Amount: 10.52e8},
	}
	frame := BuildBoardDetailFrame(domain.BoardFlow{Name: "银行"}, leaders, "th881155", "", "Esc返回", 140, false, false)
	lines := strings.Split(ansiPattern.ReplaceAllString(frame, ""), "\n")
	headerIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "代码") && strings.Contains(line, "成交额") {
			headerIndex = index
			break
		}
	}
	if headerIndex < 0 || headerIndex+2 >= len(lines) {
		t.Fatalf("constituent table not found:\n%s", frame)
	}
	header := lines[headerIndex]
	first := lines[headerIndex+1]
	second := lines[headerIndex+2]
	for _, token := range []string{"代码", "名称", "现价", "涨跌", "涨速", "换手", "量比", "成交额"} {
		column := displayWidth(header[:strings.Index(header, token)])
		var value string
		switch token {
		case "代码":
			value = "601998"
		case "名称":
			value = "中信银行"
		case "现价":
			value = "8.14"
		case "涨跌":
			value = "+4.22%"
		case "涨速":
			value = "+0.12%"
		case "换手":
			value = "0.20%"
		case "量比":
			value = "2.25"
		case "成交额":
			value = "6.56亿"
		}
		valueColumn := displayWidth(first[:strings.Index(first, value)])
		if token == "代码" || token == "名称" {
			if valueColumn != column {
				t.Fatalf("left column %s misaligned: header=%d value=%d\n%s", token, column, valueColumn, frame)
			}
		} else if valueColumn+displayWidth(value) != column+displayWidth(token) {
			t.Fatalf("numeric column %s right edge misaligned: header=%d value=%d\n%s", token, column+displayWidth(token), valueColumn+displayWidth(value), frame)
		}
	}
	if displayWidth(first) != displayWidth(second) {
		t.Fatalf("row widths differ with Chinese names: %d/%d\n%s", displayWidth(first), displayWidth(second), frame)
	}
}

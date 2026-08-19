package ui

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func globalIndexNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.2f", value)
}

func globalIndexDelta(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%+.2f", value)
}

func globalIndexTime(value string, full bool) string {
	if value == "" {
		return "--"
	}
	if !full && len(value) >= 8 {
		return value[len(value)-8:]
	}
	return value
}

type globalMarketColumn struct {
	title  string
	width  int
	right  bool
	region bool
	name   bool
	value  func(domain.GlobalIndex) string
}

func globalMarketDisplayName(value string, pinyin bool) string {
	if !pinyin {
		return value
	}
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			return storage.ToPinyin(value)
		}
	}
	return value
}

func globalMarketColumns(width int, pinyin bool) []globalMarketColumn {
	regionValue := func(item domain.GlobalIndex) string { return globalMarketDisplayName(item.Region, pinyin) }
	nameValue := func(item domain.GlobalIndex) string { return globalMarketDisplayName(item.Name, pinyin) }
	if pinyin && width >= 118 {
		return []globalMarketColumn{
			{title: "MARKET", width: 7, region: true, value: regionValue},
			{title: "INDEX", width: 18, name: true, value: nameValue},
			{title: "LAST", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "CHANGE", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexDelta(item.Delta) }},
			{title: "%", width: 8, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "OPEN", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Open) }},
			{title: "HIGH", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.High) }},
			{title: "LOW", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Low) }},
			{title: "TIME", width: 19, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, true) }},
		}
	}
	if pinyin && width >= 79 {
		return []globalMarketColumn{
			{title: "MARKET", width: 7, region: true, value: regionValue},
			{title: "INDEX", width: 18, name: true, value: nameValue},
			{title: "LAST", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "%", width: 9, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "TIME", width: 19, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, true) }},
		}
	}
	if pinyin && width >= 52 {
		return []globalMarketColumn{
			{title: "INDEX", width: 18, name: true, value: nameValue},
			{title: "LAST", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "%", width: 9, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "TIME", width: 8, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, false) }},
		}
	}
	if pinyin {
		return []globalMarketColumn{
			{title: "INDEX", width: 10, name: true, value: nameValue},
			{title: "LAST", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "%", width: 8, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
		}
	}
	if width >= 118 {
		return []globalMarketColumn{
			{title: "市场", width: 6, region: true, value: func(item domain.GlobalIndex) string { return item.Region }},
			{title: "指数", width: 14, name: true, value: func(item domain.GlobalIndex) string { return item.Name }},
			{title: "最新", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "涨跌", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexDelta(item.Delta) }},
			{title: "涨幅", width: 9, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "今开", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Open) }},
			{title: "最高", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.High) }},
			{title: "最低", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Low) }},
			{title: "数据时间", width: 19, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, true) }},
		}
	}
	if width >= 79 {
		return []globalMarketColumn{
			{title: "市场", width: 6, region: true, value: func(item domain.GlobalIndex) string { return item.Region }},
			{title: "指数", width: 14, name: true, value: func(item domain.GlobalIndex) string { return item.Name }},
			{title: "最新", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "涨跌", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexDelta(item.Delta) }},
			{title: "涨幅", width: 9, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "数据时间", width: 19, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, true) }},
		}
	}
	if width >= 52 {
		return []globalMarketColumn{
			{title: "市场", width: 4, region: true, value: func(item domain.GlobalIndex) string { return item.Region }},
			{title: "指数", width: 12, name: true, value: func(item domain.GlobalIndex) string { return item.Name }},
			{title: "最新", width: 10, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
			{title: "涨幅", width: 9, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
			{title: "时间", width: 8, value: func(item domain.GlobalIndex) string { return globalIndexTime(item.QuoteTime, false) }},
		}
	}
	return []globalMarketColumn{
		{title: "指数", width: 10, name: true, value: func(item domain.GlobalIndex) string { return item.Name }},
		{title: "最新", width: 9, right: true, value: func(item domain.GlobalIndex) string { return globalIndexNumber(item.Current) }},
		{title: "涨幅", width: 8, right: true, value: func(item domain.GlobalIndex) string { return signedPercent(item.Percent) }},
	}
}

func globalMarketRegionCode(region string) string {
	switch region {
	case "港股":
		return "1;36"
	case "日本":
		return "1;35"
	case "韩国":
		return "1;34"
	case "美国":
		return "1;33"
	default:
		return "1;37"
	}
}

func globalExtendedIndex(item domain.GlobalIndex, pinyin bool) (domain.GlobalIndex, bool) {
	if item.Extended == nil {
		return domain.GlobalIndex{}, false
	}
	extended := item.Extended
	name := extended.Session + "·" + extended.Symbol + "代理"
	if pinyin {
		name = storage.ToPinyin(extended.Session) + " " + extended.Symbol + " proxy"
	}
	return domain.GlobalIndex{
		Region: item.Region, Name: name,
		Current: extended.Price, Delta: extended.Delta, Percent: extended.Percent,
		Open: math.NaN(), PreviousClose: math.NaN(), High: math.NaN(), Low: math.NaN(),
		QuoteTime: extended.QuoteTime, Source: extended.Source,
	}, true
}

func globalMarketTable(items []domain.GlobalIndex, width int, moyu, color, pinyin bool) []string {
	columns := globalMarketColumns(width, pinyin)
	showsRegion := false
	for _, column := range columns {
		if column.region {
			showsRegion = true
			break
		}
	}
	row := func(item *domain.GlobalIndex, showRegion, secondary bool) string {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			value := column.title
			if item != nil {
				if column.region && !showRegion {
					value = ""
				} else {
					value = column.value(*item)
				}
			}
			value = truncateWidth(value, column.width)
			alignment := "left"
			if column.right {
				alignment = "right"
			}
			value = padWidth(value, column.width, alignment)
			if item != nil && color && !moyu {
				if column.region && showRegion {
					value = style(value, globalMarketRegionCode(item.Region), true)
				} else if column.name && secondary {
					value = style(value, "90", true)
				} else if column.right && !math.IsNaN(item.Percent) {
					value = style(value, trendCode(item.Percent, false), true)
				}
			}
			values = append(values, value)
		}
		return strings.Join(values, "  ")
	}
	lines := []string{style(row(nil, true, false), "90", color && !moyu)}
	previousRegion := ""
	for index := range items {
		item := &items[index]
		newRegion := item.Region != previousRegion
		if newRegion && previousRegion != "" {
			lines = append(lines, "")
		}
		if newRegion && !showsRegion {
			region := globalMarketDisplayName(item.Region, pinyin)
			lines = append(lines, style(region, globalMarketRegionCode(item.Region), color && !moyu))
		}
		lines = append(lines, row(item, newRegion, false))
		if extended, ok := globalExtendedIndex(*item, pinyin); ok {
			lines = append(lines, row(&extended, false, true))
		}
		previousRegion = item.Region
	}
	return lines
}

func BuildGlobalMarketsFrame(items []domain.GlobalIndex, refreshedAt time.Time, loading bool, status, controls string, terminalWidth int, moyu, color, pinyin bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	refreshed := "--:--:--"
	if !refreshedAt.IsZero() {
		refreshed = refreshedAt.Format("15:04:05")
	}
	title := "GLOBAL MARKETS  " + refreshed
	source := "SINA GLOBAL INDICES  |  US EXTENDED HOURS: DIA / QQQ / SPY ETF PROXIES"
	if !moyu {
		title = "ASTOCK 外盘指数  ·  更新 " + refreshed
		source = "新浪全球指数  ·  源站市场时间  ·  美股盘前/盘后为 DIA/QQQ/SPY ETF代理"
	}
	lines := []string{
		style(truncateWidth(title, terminalWidth), "1;36", color && !moyu),
		style(truncateWidth(controls, terminalWidth), "90", color && !moyu),
		style(truncateWidth(source, terminalWidth), "90", color && !moyu),
		"",
	}
	if loading && len(items) == 0 {
		message := "LOADING GLOBAL MARKETS; A-SHARE QUOTES CONTINUE"
		if !moyu {
			message = "正在加载外盘指数，A股行情继续在后台刷新"
		}
		lines = append(lines, style(message, "33", color && !moyu))
	} else {
		lines = append(lines, globalMarketTable(items, terminalWidth, moyu, color, pinyin)...)
	}
	if status != "" {
		lines = append(lines, "", style(truncateWidth(status, terminalWidth), "33", color && !moyu))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

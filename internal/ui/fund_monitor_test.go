package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func fundMonitorTestRows(count int) []domain.FundMovement {
	rows := make([]domain.FundMovement, count)
	for index := range rows {
		rows[index] = domain.FundMovement{
			Symbol: fmt.Sprintf("sz%06d", 300000+index), Name: fmt.Sprintf("测试股%d", index+1),
			Industry: "半导体", Price: 25.31, Percent: 2.14,
			MainNet: 328500000, MainRatio: 6.72,
			Delta1Minute: 72500000, Delta3Minutes: 151200000, Delta5Minutes: 243100000,
			IndustryNet: 1856000000, IndustryPercent: 1.83,
			State: "个股板块共振",
		}
	}
	return rows
}

func TestFundMonitorFrameFits79ColumnsAndShowsCoreEvidence(t *testing.T) {
	frame := BuildLiveFrame(LiveData{
		FundMonitorActive: true, FundMonitorSource: "自选 · 科技",
		FundMovements: fundMonitorTestRows(2), FundMonitorSelected: 1,
		FundMonitorRefreshedAt:  time.Date(2026, 8, 11, 10, 8, 30, 0, time.Local),
		FundIndustryRefreshedAt: time.Date(2026, 8, 11, 10, 8, 0, 0, time.Local),
		Footer:                  "↑/↓选择  [/]跳选  Enter详情  v刷新  Esc返回  q退出",
	}, ViewOptions{}, 79, 24)
	for _, expected := range []string{
		"资金雷达", "自选 · 科技", "1分钟↓", "样本 10:08:30", "行业 10:08:00",
		"代码", "名称", "行业/板块", "涨幅", "主力净额", "1分钟", "状态",
		"> 300001", "测试股2", "↑ 半导体", "+2.14%", "+3.29亿", "+7250万", "共振流入", "v刷新",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("fund monitor frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("fund monitor line width %d exceeds terminal:\n%s", width, line)
		}
	}
}

func TestFundMonitorIndustryShowsFullNameWithoutMissingFlowPrefix(t *testing.T) {
	missing := domain.FundMovement{
		Industry: "工程咨询服务Ⅱ", IndustryNet: math.NaN(), IndustryPercent: math.NaN(),
	}
	if got := fundMonitorIndustry(missing); got != "工程咨询服务Ⅱ" {
		t.Fatalf("missing industry flow should not add a placeholder prefix: %q", got)
	}
	available := missing
	available.IndustryNet = 1e8
	if got := fundMonitorIndustry(available); got != "↑ 工程咨询服务Ⅱ" {
		t.Fatalf("available industry direction should keep the full name: %q", got)
	}
}

func TestFundMonitorWideTableKeepsFullIndustryName(t *testing.T) {
	rows := fundMonitorTestRows(1)
	rows[0].Industry = "工程咨询服务Ⅱ"
	rows[0].IndustryNet = math.NaN()
	rows[0].IndustryPercent = math.NaN()
	table := buildFundMonitorTable(rows, 0, 136, false, false)
	if !strings.Contains(table, "工程咨询服务Ⅱ") || strings.Contains(table, "工程咨询服…") {
		t.Fatalf("wide fund monitor truncated the industry name:\n%s", table)
	}
	if !strings.Contains(table, "板块资金") || !strings.Contains(table, "--") {
		t.Fatalf("wide fund monitor did not keep the independent board-flow column:\n%s", table)
	}
}

func TestFundMonitorWideTableShowsIndustryAmountSeparately(t *testing.T) {
	rows := fundMonitorTestRows(1)
	rows[0].Industry = "电池"
	rows[0].IndustryNet = 567000000
	table := buildFundMonitorTable(rows, 0, 136, false, false)
	for _, expected := range []string{"↑ 电池", "板块资金", "+5.67亿"} {
		if !strings.Contains(table, expected) {
			t.Fatalf("wide fund monitor missing %q:\n%s", expected, table)
		}
	}
}

func TestFundMonitorFrameKeepsSelectionWithin24Rows(t *testing.T) {
	frame := BuildLiveFrame(LiveData{
		FundMonitorActive: true, FundMonitorSource: "涨幅榜前20",
		FundMovements: fundMonitorTestRows(20), FundMonitorSelected: 19,
		Footer: "↑/↓选择  [/]跳选  Enter详情  v刷新  Esc返回  q退出",
	}, ViewOptions{}, 79, 24)
	if !strings.Contains(frame, "> 300019") || !strings.Contains(frame, "9-20/20") || strings.Contains(frame, "  300000") {
		t.Fatalf("fund monitor viewport did not follow selection:\n%s", frame)
	}
	if rows := strings.Count(frame, "\n") + 1; rows > 24 {
		t.Fatalf("fund monitor frame has %d rows for a 24-row terminal:\n%s", rows, frame)
	}
}

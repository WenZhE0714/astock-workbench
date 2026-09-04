package market

import (
	"math"
	"net/url"
	"testing"
)

func TestDecodeLimitPoolAndTopicPool(t *testing.T) {
	primary := `{"success":true,"result":{"data":[{"CONTINUOUS_LIMIT_UP_NUM":5},{"CONTINUOUS_LIMIT_UP_NUM":3},{"CONTINUOUS_LIMIT_UP_NUM":3}]}}`
	rows := decodeLimitPool(primary)
	if len(rows) != 3 || int(limitRowNumber(rows[0], "CONTINUOUS_LIMIT_UP_NUM")) != 5 {
		t.Fatalf("unexpected primary pool: %#v", rows)
	}
	topic := `{"data":{"pool":[{"c":"600001","n":"测试","lbc":7},{"c":"600002","n":"测试2","lbc":2}]}}`
	topicRows := decodeTopicPool(topic)
	if len(topicRows) != 2 || int(limitRowNumber(topicRows[0], "LBC")) != 7 {
		t.Fatalf("unexpected topic pool: %#v", topicRows)
	}
}

func TestTopicPoolAddressUsesEastmoneyDateFormat(t *testing.T) {
	address := topicPoolAddress("https://push2ex.eastmoney.com", "/getTopicZTPool", "2026-09-04")
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("date"); got != "20260904" {
		t.Fatalf("unexpected topic-pool date: %s", got)
	}
}

func TestLimitStatsLadderCalculation(t *testing.T) {
	rows := []map[string]any{
		{"CONTINUOUS_LIMIT_UP_NUM": 5},
		{"CONTINUOUS_LIMIT_UP_NUM": 3},
		{"CONTINUOUS_LIMIT_UP_NUM": 3},
		{"CONTINUOUS_LIMIT_UP_NUM": 1},
	}
	ladder := map[int]int{}
	highest := 0
	for _, row := range rows {
		streak := int(limitRowNumber(row, "CONTINUOUS_LIMIT_UP_NUM", "LBC"))
		if streak > 0 {
			ladder[streak]++
			if streak > highest {
				highest = streak
			}
		}
	}
	if highest != 5 || ladder[3] != 2 || ladder[5] != 1 {
		t.Fatalf("unexpected ladder: highest=%d ladder=%v", highest, ladder)
	}
	brokenRate := float64(4) / float64(4+2) * 100
	if math.Abs(brokenRate-66.6667) > 0.001 {
		t.Fatalf("unexpected broken rate: %v", brokenRate)
	}
}

func TestLimitStatsSnapshotIncludesStreakStocks(t *testing.T) {
	rows := [][]map[string]any{
		{
			{"c": "600001", "m": 1, "n": "测试股份", "zdp": 10.01, "lbc": 5},
			{"c": "000002", "m": 0, "n": "深市测试", "zdp": 9.98, "lbc": 3},
			{"c": "", "n": "无代码", "lbc": 3},
		},
		{},
		{},
	}
	snapshot := limitStatsSnapshotFromRows(rows, "2026-09-04", "test")
	if len(snapshot.StreakLadder) != 2 {
		t.Fatalf("unexpected streak ladder: %#v", snapshot.StreakLadder)
	}
	if snapshot.StreakLadder[0].Streak != 5 || snapshot.StreakLadder[0].Count != 1 || len(snapshot.StreakLadder[0].Stocks) != 1 {
		t.Fatalf("unexpected five-board group: %#v", snapshot.StreakLadder[0])
	}
	stock := snapshot.StreakLadder[0].Stocks[0]
	if stock.Symbol != "sh600001" || stock.Name != "测试股份" || stock.Percent != 10.01 {
		t.Fatalf("unexpected streak stock: %#v", stock)
	}
}

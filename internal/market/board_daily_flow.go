package market

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var BoardCodePattern = regexp.MustCompile(`^BK[0-9]{4,6}$`)

func (EastmoneyClient) FetchBoardDailyFlows(ctx context.Context, code string) ([]domain.BoardDailyFlow, error) {
	if !BoardCodePattern.MatchString(code) {
		return nil, fmt.Errorf("板块代码无效")
	}
	base := os.Getenv("ASTOCK_BOARD_DAILY_FLOW_URL")
	if base == "" {
		base = "https://push2his.eastmoney.com/api/qt/stock/fflow/daykline/get"
	}
	address, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	query := address.Query()
	query.Set("secid", "90."+code)
	query.Set("lmt", "60")
	query.Set("klt", "101")
	query.Set("fields1", "f1,f2,f3,f7")
	query.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61,f62,f63")
	address.RawQuery = query.Encode()
	raw, err := fetchDecodedDirectFirst(ctx, address.String(), nil, map[string]string{"Referer": "https://quote.eastmoney.com/"})
	if err != nil {
		return nil, err
	}
	return ParseBoardDailyFlows(raw, code)
}

func ParseBoardDailyFlows(raw, code string) ([]domain.BoardDailyFlow, error) {
	var payload struct {
		Data *struct {
			Code   string   `json:"code"`
			Klines []string `json:"klines"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	if payload.Data == nil || payload.Data.Code != code || len(payload.Data.Klines) == 0 {
		return nil, fmt.Errorf("板块日线资金暂无有效数据")
	}
	result := make([]domain.BoardDailyFlow, 0, len(payload.Data.Klines))
	seen := map[string]bool{}
	for _, line := range payload.Data.Klines {
		row, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil || len(row) < 7 {
			return nil, fmt.Errorf("板块日线资金字段不完整")
		}
		date, dateErr := time.Parse(time.DateOnly, row[0])
		net, netErr := strconv.ParseFloat(row[1], 64)
		ratio, ratioErr := strconv.ParseFloat(row[6], 64)
		if dateErr != nil || date.Format(time.DateOnly) != row[0] || netErr != nil || ratioErr != nil || math.IsNaN(net) || math.IsInf(net, 0) || math.IsNaN(ratio) || math.IsInf(ratio, 0) || seen[row[0]] {
			return nil, fmt.Errorf("板块日线资金存在无效或重复记录")
		}
		seen[row[0]] = true
		result = append(result, domain.BoardDailyFlow{Date: row[0], MainNet: net, MainRatio: ratio})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

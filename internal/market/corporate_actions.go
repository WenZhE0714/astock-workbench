package market

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type CorporateActionClient interface {
	FetchCorporateActions(context.Context, string, string, string) (domain.CorporateActionSnapshot, error)
}

type distributionRow struct {
	Code        string          `json:"SECURITY_CODE"`
	ReportDate  string          `json:"REPORT_DATE"`
	ExDate      string          `json:"EX_DIVIDEND_DATE"`
	RecordDate  string          `json:"EQUITY_RECORD_DATE"`
	NoticeDate  string          `json:"NOTICE_DATE"`
	Progress    string          `json:"ASSIGN_PROGRESS"`
	Description string          `json:"IMPL_PLAN_PROFILE"`
	Cash        json.RawMessage `json:"PRETAX_BONUS_RMB"`
	Bonus       json.RawMessage `json:"BONUS_RATIO"`
	Transfer    json.RawMessage `json:"IT_RATIO"`
}

type distributionPayload struct {
	Success bool `json:"success"`
	Result  *struct {
		Pages *int              `json:"pages"`
		Count *int              `json:"count"`
		Data  []distributionRow `json:"data"`
	} `json:"result"`
}

func distributionDate(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	for _, layout := range []string{time.DateOnly, "2006-01-02 15:04:05", time.RFC3339} {
		if date, err := time.Parse(layout, value); err == nil {
			return date.Format(time.DateOnly), nil
		}
	}
	return "", fmt.Errorf("无法解析事件日期 %q", value)
}

func distributionAmount(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("分配比例字段缺失")
	}
	if string(raw) == "null" {
		return 0, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, fmt.Errorf("分配比例无效")
	}
	value, err := number.Float64()
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("分配比例无效")
	}
	return value, nil
}

func distributionEvent(row distributionRow, symbol, sourceURL string) (*domain.CorporateAction, error) {
	if row.Code != symbol[2:] {
		return nil, fmt.Errorf("分红记录证券代码不匹配")
	}
	if row.Progress == "" {
		return nil, fmt.Errorf("分红记录缺少方案进度")
	}
	if row.Progress != "实施分配" {
		switch row.Progress {
		case "董事会预案", "股东大会预案", "股东大会通过", "不分配不转增", "不分配", "取消分配", "终止实施":
			return nil, nil
		default:
			return nil, fmt.Errorf("无法识别分红方案进度 %q", row.Progress)
		}
	}
	kinds := []string{}
	amounts := [3]float64{}
	for i, raw := range []json.RawMessage{row.Cash, row.Bonus, row.Transfer} {
		amount, err := distributionAmount(raw)
		if err != nil {
			return nil, err
		}
		amounts[i] = amount
		if amount > 0 {
			kinds = append(kinds, []string{"cash-dividend", "bonus-share", "capitalization"}[i])
		}
	}
	if len(kinds) == 0 {
		if row.ExDate != "" {
			return nil, fmt.Errorf("已实施分配未提供有效分配比例")
		}
		return nil, nil
	}
	exDate, err := distributionDate(row.ExDate)
	if err != nil || exDate == "" {
		return nil, fmt.Errorf("已实施分配缺少有效除权除息日")
	}
	recordDate, err := distributionDate(row.RecordDate)
	if err != nil {
		return nil, err
	}
	noticeDate, err := distributionDate(row.NoticeDate)
	if err != nil {
		return nil, err
	}
	if recordDate != "" && recordDate > exDate {
		return nil, fmt.Errorf("登记日晚于除权除息日")
	}
	hash := sha256.Sum256([]byte(symbol + "|" + row.ReportDate + "|" + exDate))
	return &domain.CorporateAction{ID: hex.EncodeToString(hash[:]), Symbol: symbol, ExDate: exDate, RecordDate: recordDate, NoticeDate: noticeDate, Kinds: kinds, Description: row.Description, Source: "东方财富分红送配", SourceURL: sourceURL,
		Terms: &domain.DistributionTerms{CashPer10: &amounts[0], BonusPer10: &amounts[1], CapitalizationPer10: &amounts[2]}}, nil
}

func distributionAddress(base, symbol string, page int) string {
	values := url.Values{
		"reportName": {"RPT_SHAREBONUS_DET"}, "columns": {"SECURITY_CODE,REPORT_DATE,EX_DIVIDEND_DATE,EQUITY_RECORD_DATE,NOTICE_DATE,ASSIGN_PROGRESS,IMPL_PLAN_PROFILE,PRETAX_BONUS_RMB,BONUS_RATIO,IT_RATIO"},
		"filter":     {fmt.Sprintf(`(SECURITY_CODE="%s")`, symbol[2:])},
		"pageNumber": {strconv.Itoa(page)}, "pageSize": {"100"}, "sortColumns": {"REPORT_DATE"}, "sortTypes": {"-1"}, "source": {"WEB"}, "client": {"WEB"},
	}
	return base + "?" + values.Encode()
}

func (EastmoneyClient) FetchCorporateActions(ctx context.Context, symbol, start, end string) (domain.CorporateActionSnapshot, error) {
	snapshot := domain.CorporateActionSnapshot{Symbol: symbol, Start: start, End: end, Status: "unavailable", Scope: domain.CorporateActionScope, Source: "东方财富分红送配", FetchedAt: time.Now(), Events: []domain.CorporateAction{}}
	if !historyCacheSymbol.MatchString(symbol) {
		return snapshot, fmt.Errorf("无效事件证券代码")
	}
	first, firstErr := time.Parse(time.DateOnly, start)
	last, lastErr := time.Parse(time.DateOnly, end)
	if firstErr != nil || lastErr != nil || first.After(last) {
		return snapshot, fmt.Errorf("无效事件查询区间")
	}
	snapshot.SourceURL = "https://data.eastmoney.com/yjfp/detail/" + symbol[2:] + ".html"
	base := os.Getenv("ASTOCK_CORPORATE_ACTION_API_URL")
	if base == "" {
		base = dragonTigerAPIURL
	}
	pages, count, received := 1, -1, 0
	seen := map[string]string{}
	seenRows := map[string]bool{}
	for page := 1; page <= pages; page++ {
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		raw, err := fetchDecodedDirectFirst(ctx, distributionAddress(base, symbol, page), nil, nil)
		if err != nil {
			return snapshot, err
		}
		var payload distributionPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil || !payload.Success || payload.Result == nil || payload.Result.Pages == nil || payload.Result.Count == nil || payload.Result.Data == nil {
			return snapshot, fmt.Errorf("分红送配接口未返回可核验的分页数据")
		}
		result := payload.Result
		if page == 1 {
			pages, count = *result.Pages, *result.Count
			if pages < 0 || pages > 10 || count < 0 || (count > 0 && pages != (count+99)/100) || (count == 0 && pages > 1) {
				return snapshot, fmt.Errorf("分红送配分页数量异常")
			}
		} else if pages != *result.Pages || count != *result.Count {
			return snapshot, fmt.Errorf("分红送配分页在查询中发生变化")
		}
		received += len(result.Data)
		for _, row := range result.Data {
			rowJSON, _ := json.Marshal(row)
			if seenRows[string(rowJSON)] {
				return snapshot, fmt.Errorf("分红送配重复分页或记录")
			}
			seenRows[string(rowJSON)] = true
			event, err := distributionEvent(row, symbol, snapshot.SourceURL)
			if err != nil {
				return snapshot, err
			}
			if event == nil {
				continue
			}
			encoded, _ := json.Marshal(event)
			if previous, exists := seen[event.ID]; exists {
				if previous != string(encoded) {
					return snapshot, fmt.Errorf("同一除权除息事件存在冲突记录")
				}
				return snapshot, fmt.Errorf("分红送配存在重复记录，分页完整性无法核验")
			}
			seen[event.ID] = string(encoded)
			if event.ExDate >= start && event.ExDate <= end {
				snapshot.Events = append(snapshot.Events, *event)
			}
		}
	}
	if received != count {
		return snapshot, fmt.Errorf("分红送配条数不完整：%d / %d", received, count)
	}
	sort.Slice(snapshot.Events, func(i, j int) bool {
		return snapshot.Events[i].ExDate+snapshot.Events[i].ID < snapshot.Events[j].ExDate+snapshot.Events[j].ID
	})
	snapshot.Status = "checked"
	return snapshot, nil
}

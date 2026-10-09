package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type IndustryFlowClient interface {
	FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error)
}

type industryFlowPayload struct {
	Data *struct {
		Total int `json:"total"`
		Diff  []struct {
			Code          string          `json:"f12"`
			Name          string          `json:"f14"`
			Percent       json.RawMessage `json:"f3"`
			MainNet       json.RawMessage `json:"f62"`
			RiseCount     int             `json:"f104"`
			FallCount     int             `json:"f105"`
			FlatCount     int             `json:"f106"`
			LeaderName    string          `json:"f128"`
			LeaderPercent json.RawMessage `json:"f136"`
			LeaderCode    string          `json:"f140"`
			MainRatio     json.RawMessage `json:"f184"`
		} `json:"diff"`
	} `json:"data"`
}

func ParseIndustryFlowPayload(raw string) map[string]domain.BoardFlow {
	_, result := parseIndustryFlowPage(raw)
	return result
}

func parseIndustryFlowPage(raw string) (int, map[string]domain.BoardFlow) {
	var payload industryFlowPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return 0, nil
	}
	result := make(map[string]domain.BoardFlow, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		name := strings.TrimSpace(item.Name)
		if name == "" || item.Code == "" {
			continue
		}
		result[name] = domain.BoardFlow{
			Code: item.Code, Name: name, Kind: domain.BoardKindIndustry, Source: "东方财富",
			Percent: rawNumber(item.Percent), MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
			RiseCount: item.RiseCount, FallCount: item.FallCount, FlatCount: item.FlatCount,
			LeaderName: strings.TrimSpace(item.LeaderName), LeaderCode: strings.TrimSpace(item.LeaderCode),
			LeaderPercent: rawNumber(item.LeaderPercent),
		}
	}
	return payload.Data.Total, result
}

func industryFlowAddress(base string) string {
	return industryFlowPageAddress(base, 1)
}

func industryFlowPageAddress(base string, page int) string {
	if page < 1 {
		page = 1
	}
	values := url.Values{
		"fields": {"f3,f12,f14,f62,f100,f104,f105,f106,f128,f136,f140,f184"},
		// Stable ordering prevents moving prices from shifting page boundaries.
		"fid":  {"f12"},
		"fltt": {"2"},
		"fs":   {"m:90+t:2+f:!50"},
		"invt": {"2"},
		"np":   {"1"},
		"pn":   {fmt.Sprintf("%d", page)},
		"po":   {"1"},
		"pz":   {"100"},
		"ut":   {"bd1d9ddb04089700cf9c27f6f7426281"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func industryFlowBases() []string {
	if configured := os.Getenv("ASTOCK_INDUSTRY_FLOW_API_URL"); configured != "" {
		return []string{configured}
	}
	return []string{marketRankingAPIURL, boardRankDelayURL, boardRankAPIURL, boardRankFallbackURL}
}

func (EastmoneyClient) FetchIndustryFlows(ctx context.Context) (map[string]domain.BoardFlow, error) {
	return fetchIndustryFlows(ctx, industryFlowBases())
}

func fetchIndustryFlows(ctx context.Context, bases []string) (map[string]domain.BoardFlow, error) {
	var lastError error
	for index, base := range bases {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Keep half the remaining budget for all later nodes, rather than
		// dividing a multi-page download into ever smaller equal shares.
		nodeCtx, cancelNode := fallbackContext(ctx, min(2, len(bases)-index), 4*time.Second)
		flows, err := fetchIndustryFlowNode(nodeCtx, base)
		cancelNode()
		if err == nil {
			return flows, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = fmt.Errorf("行业资金流暂不可用")
	}
	return nil, lastError
}

func fetchIndustryFlowNode(ctx context.Context, base string) (map[string]domain.BoardFlow, error) {
	ctx, cancelNode := context.WithCancel(ctx)
	defer cancelNode()
	type pageResult struct {
		total int
		flows map[string]domain.BoardFlow
		err   error
	}
	fetchPage := func(page int) pageResult {
		requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		raw, err := fetchDecodedDirectFirst(requestCtx, industryFlowPageAddress(base, page), nil, nil)
		if err != nil {
			return pageResult{err: err}
		}
		total, flows := parseIndustryFlowPage(raw)
		if len(flows) == 0 {
			return pageResult{err: fmt.Errorf("未解析到行业资金流第 %d 页", page)}
		}
		return pageResult{total: total, flows: flows}
	}
	first := fetchPage(1)
	if first.err != nil {
		return nil, first.err
	}
	pages := (first.total + 99) / 100
	if pages > 10 {
		return nil, fmt.Errorf("行业资金流超过分页上限，未取得完整数据")
	}
	// Fetch at most two pages concurrently; stable code ordering keeps each
	// page independent without increasing the number of upstream requests.
	for page := 2; page <= pages; page += 2 {
		count := min(2, pages-page+1)
		results := make(chan pageResult, count)
		for offset := 0; offset < count; offset++ {
			go func(number int) { results <- fetchPage(number) }(page + offset)
		}
		var batchError error
		for offset := 0; offset < count; offset++ {
			result := <-results
			if result.err != nil {
				if batchError == nil {
					batchError = result.err
					cancelNode()
				}
				continue
			}
			if result.total != first.total {
				if batchError == nil {
					batchError = fmt.Errorf("行业资金流分页总数发生变化，等待完整快照")
					cancelNode()
				}
				continue
			}
			for name, flow := range result.flows {
				first.flows[name] = flow
			}
		}
		if batchError != nil {
			return nil, batchError
		}
	}
	if first.total > 0 && len(first.flows) != first.total {
		return nil, fmt.Errorf("行业资金流仅返回 %d/%d 个行业", len(first.flows), first.total)
	}
	return first.flows, nil
}

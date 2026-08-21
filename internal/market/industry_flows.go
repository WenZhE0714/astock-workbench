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
			Code: item.Code, Name: name, Kind: domain.BoardKindIndustry,
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
		"fid":    {"f3"},
		"fltt":   {"2"},
		"fs":     {"m:90+t:2+f:!50"},
		"invt":   {"2"},
		"np":     {"1"},
		"pn":     {fmt.Sprintf("%d", page)},
		"po":     {"1"},
		"pz":     {"100"},
		"ut":     {"bd1d9ddb04089700cf9c27f6f7426281"},
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
	return []string{boardRankAPIURL, boardRankDelayURL, boardRankFallbackURL}
}

func (EastmoneyClient) FetchIndustryFlows(ctx context.Context) (map[string]domain.BoardFlow, error) {
	var lastError error
	for _, base := range industryFlowBases() {
		flows := make(map[string]domain.BoardFlow)
		complete := true
		for page := 1; page <= 10; page++ {
			requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
			raw, fetchError := fetchDecoded(requestContext, industryFlowPageAddress(base, page), nil)
			cancel()
			if fetchError != nil {
				lastError = fetchError
				complete = false
				break
			}
			total, pageFlows := parseIndustryFlowPage(raw)
			if len(pageFlows) == 0 {
				lastError = fmt.Errorf("未解析到行业资金流第 %d 页", page)
				complete = false
				break
			}
			for name, flow := range pageFlows {
				flows[name] = flow
			}
			pages := (total + 99) / 100
			if total <= 0 || page >= pages {
				break
			}
		}
		if complete && len(flows) > 0 {
			return flows, nil
		}
	}
	if lastError == nil {
		lastError = fmt.Errorf("行业资金流暂不可用")
	}
	return nil, lastError
}

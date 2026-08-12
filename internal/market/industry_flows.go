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
		Diff []struct {
			Code      string          `json:"f12"`
			Name      string          `json:"f14"`
			Percent   json.RawMessage `json:"f3"`
			MainNet   json.RawMessage `json:"f62"`
			RiseCount int             `json:"f104"`
			FallCount int             `json:"f105"`
			MainRatio json.RawMessage `json:"f184"`
		} `json:"diff"`
	} `json:"data"`
}

func ParseIndustryFlowPayload(raw string) map[string]domain.BoardFlow {
	var payload industryFlowPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
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
			RiseCount: item.RiseCount, FallCount: item.FallCount,
		}
	}
	return result
}

func industryFlowAddress(base string) string {
	values := url.Values{
		"fields": {"f3,f12,f14,f62,f100,f104,f105,f184"},
		"fid":    {"f3"},
		"fltt":   {"2"},
		"fs":     {"m:90+t:2+f:!50"},
		"invt":   {"2"},
		"np":     {"1"},
		"pn":     {"1"},
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
		requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		raw, fetchError := fetchDecoded(requestContext, industryFlowAddress(base), nil)
		cancel()
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		flows := ParseIndustryFlowPayload(raw)
		if len(flows) == 0 {
			lastError = fmt.Errorf("未解析到行业资金流")
			continue
		}
		return flows, nil
	}
	if lastError == nil {
		lastError = fmt.Errorf("行业资金流暂不可用")
	}
	return nil, lastError
}

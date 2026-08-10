package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	boardMembershipAPIURL = "https://emweb.securities.eastmoney.com/PC_HSF10/CoreConception/PageAjax"
	boardFlowAPIURL       = "https://push2.eastmoney.com/api/qt/ulist.np/get"
	boardFlowFallbackURL  = "https://push2delay.eastmoney.com/api/qt/ulist.np/get"
)

type BoardFlowClient interface {
	FetchBoards(context.Context, string) ([]domain.BoardFlow, error)
}

type boardMembership struct {
	Code      string
	Name      string
	Kind      string
	Rank      int
	IsPrecise bool
}

type boardMembershipPayload struct {
	Boards []struct {
		Code      string  `json:"BOARD_CODE"`
		Name      string  `json:"BOARD_NAME"`
		Rank      int     `json:"BOARD_RANK"`
		IsPrecise *string `json:"IS_PRECISE"`
	} `json:"ssbk"`
}

func eastmoneyF10Code(symbol string) string {
	if len(symbol) != 8 {
		return ""
	}
	prefix := "SZ"
	if strings.HasPrefix(symbol, "sh") {
		prefix = "SH"
	}
	return prefix + symbol[2:]
}

func normalizeBoardCode(value string) string {
	value = strings.TrimSpace(strings.ToUpper(value))
	if strings.HasPrefix(value, "BK") && len(value) >= 6 {
		return value
	}
	value = strings.TrimPrefix(value, "BK")
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return ""
	}
	return fmt.Sprintf("BK%04d", number)
}

func ParseBoardMembershipPayload(raw string) []boardMembership {
	var payload boardMembershipPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	result := make([]boardMembership, 0, len(payload.Boards))
	for _, item := range payload.Boards {
		code := normalizeBoardCode(item.Code)
		if code == "" || item.Name == "" {
			continue
		}
		precise := item.IsPrecise != nil && *item.IsPrecise == "1"
		kind := ""
		switch {
		case item.Rank >= 1 && item.Rank <= 3:
			kind = domain.BoardKindIndustry
		case precise:
			kind = domain.BoardKindConcept
		default:
			continue
		}
		result = append(result, boardMembership{
			Code: code, Name: item.Name, Kind: kind, Rank: item.Rank, IsPrecise: precise,
		})
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].Rank < result[right].Rank })
	return result
}

func selectBoardMemberships(items []boardMembership) []boardMembership {
	result := make([]boardMembership, 0, 6)
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.Code] {
			continue
		}
		seen[item.Code] = true
		result = append(result, item)
		if len(result) == 6 {
			break
		}
	}
	return result
}

type boardFlowPayload struct {
	Data *struct {
		Diff []struct {
			Code          string          `json:"f12"`
			Name          string          `json:"f14"`
			Percent       json.RawMessage `json:"f3"`
			MainNet       json.RawMessage `json:"f62"`
			MainRatio     json.RawMessage `json:"f184"`
			LeaderName    string          `json:"f128"`
			LeaderCode    string          `json:"f140"`
			LeaderPercent json.RawMessage `json:"f136"`
		} `json:"diff"`
	} `json:"data"`
}

func ParseBoardFlowPayload(raw string) map[string]domain.BoardFlow {
	var payload boardFlowPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make(map[string]domain.BoardFlow, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		code := normalizeBoardCode(item.Code)
		if code == "" {
			continue
		}
		result[code] = domain.BoardFlow{
			Code: code, Name: item.Name,
			Percent: rawNumber(item.Percent), MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
			LeaderName: item.LeaderName, LeaderCode: item.LeaderCode, LeaderPercent: rawNumber(item.LeaderPercent),
		}
	}
	return result
}

func boardMembershipAddress(base, symbol string) string {
	code := eastmoneyF10Code(symbol)
	if strings.Contains(base, "{code}") {
		return strings.Replace(base, "{code}", url.QueryEscape(code), 1)
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + "code=" + url.QueryEscape(code)
}

func boardFlowAddress(base string, memberships []boardMembership) string {
	securityIDs := make([]string, 0, len(memberships))
	for _, item := range memberships {
		securityIDs = append(securityIDs, "90."+item.Code)
	}
	joined := strings.Join(securityIDs, ",")
	if strings.Contains(base, "{secids}") {
		return strings.Replace(base, "{secids}", url.QueryEscape(joined), 1)
	}
	values := url.Values{
		"fltt":   {"2"},
		"invt":   {"2"},
		"fields": {"f12,f13,f14,f3,f62,f184,f128,f136,f140,f141"},
		"secids": {joined},
		"ut":     {"b2884a393a59ad64002292a3e90d46a5"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) FetchBoards(ctx context.Context, symbol string) ([]domain.BoardFlow, error) {
	if eastmoneyF10Code(symbol) == "" {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	membershipBase := os.Getenv("ASTOCK_BOARD_MEMBERSHIP_API_URL")
	if membershipBase == "" {
		membershipBase = boardMembershipAPIURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	raw, err := fetchDecoded(requestContext, boardMembershipAddress(membershipBase, symbol), nil)
	cancel()
	if err != nil {
		return nil, err
	}
	memberships := selectBoardMemberships(ParseBoardMembershipPayload(raw))
	if len(memberships) == 0 {
		return nil, fmt.Errorf("未解析到关联板块")
	}

	configuredBase := os.Getenv("ASTOCK_BOARD_FLOW_API_URL")
	bases := []string{configuredBase}
	if configuredBase == "" {
		bases = []string{boardFlowAPIURL, boardFlowFallbackURL}
	}
	var flows map[string]domain.BoardFlow
	var lastError error
	for _, base := range bases {
		requestContext, cancel = context.WithTimeout(ctx, 4*time.Second)
		raw, err = fetchDecoded(requestContext, boardFlowAddress(base, memberships), nil)
		cancel()
		if err != nil {
			lastError = err
			continue
		}
		flows = ParseBoardFlowPayload(raw)
		if flows == nil {
			lastError = fmt.Errorf("未解析到板块资金流")
			continue
		}
		break
	}
	if flows == nil {
		return nil, lastError
	}

	result := make([]domain.BoardFlow, 0, len(memberships))
	for _, membership := range memberships {
		item, ok := flows[membership.Code]
		if !ok {
			item = domain.BoardFlow{
				Code: membership.Code, Name: membership.Name,
				Percent: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(), LeaderPercent: math.NaN(),
			}
		}
		item.Kind = membership.Kind
		if item.Name == "" {
			item.Name = membership.Name
		}
		result = append(result, item)
	}
	return result, nil
}

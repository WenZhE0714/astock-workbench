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
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	boardMembershipAPIURL = "https://emweb.securities.eastmoney.com/PC_HSF10/CoreConception/PageAjax"
	boardFlowAPIURL       = "https://push2.eastmoney.com/api/qt/ulist.np/get"
	boardFlowFallbackURL  = "https://push2delay.eastmoney.com/api/qt/ulist.np/get"
	boardRankAPIURL       = "https://17.push2.eastmoney.com/api/qt/clist/get"
	boardRankFallbackURL  = "https://push2.eastmoney.com/api/qt/clist/get"
	boardRankDelayURL     = "https://push2delay.eastmoney.com/api/qt/clist/get"
	boardRankPageSize     = 100
	boardRankCacheTTL     = 5 * time.Minute
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
			Turnover      json.RawMessage `json:"f8"`
			MainNet       json.RawMessage `json:"f62"`
			MainRatio     json.RawMessage `json:"f184"`
			RiseCount     int             `json:"f104"`
			FallCount     int             `json:"f105"`
			FlatCount     int             `json:"f106"`
			LeaderName    string          `json:"f128"`
			LeaderCode    string          `json:"f140"`
			LeaderPercent json.RawMessage `json:"f136"`
		} `json:"diff"`
	} `json:"data"`
}

type boardRankSnapshot struct {
	Code         string
	Percent      float64
	MainNet      float64
	Turnover     float64
	RiseCount    int
	FallCount    int
	FlatCount    int
	ChangeRank   int
	FlowRank     int
	TurnoverRank int
	UniverseSize int
}

type boardRankUniverse struct {
	Total int
	Items map[string]boardRankSnapshot
}

var boardRankCache = struct {
	sync.Mutex
	fetchedAt time.Time
	ranks     map[string]boardRankUniverse
}{}

type boardRankPayload struct {
	Data *struct {
		Total int `json:"total"`
		Diff  []struct {
			Code      string          `json:"f12"`
			Percent   json.RawMessage `json:"f3"`
			Turnover  json.RawMessage `json:"f8"`
			MainNet   json.RawMessage `json:"f62"`
			RiseCount int             `json:"f104"`
			FallCount int             `json:"f105"`
			FlatCount int             `json:"f106"`
		} `json:"diff"`
	} `json:"data"`
}

func rankBoardSnapshots(items []boardRankSnapshot, value func(boardRankSnapshot) float64, assign func(*boardRankSnapshot, int)) {
	indices := make([]int, 0, len(items))
	for index, item := range items {
		metric := value(item)
		if !math.IsNaN(metric) && !math.IsInf(metric, 0) {
			indices = append(indices, index)
		}
	}
	sort.SliceStable(indices, func(left, right int) bool {
		return value(items[indices[left]]) > value(items[indices[right]])
	})
	for rank, index := range indices {
		assign(&items[index], rank+1)
	}
}

func parseBoardRankPage(raw string) (int, []boardRankSnapshot, bool) {
	var payload boardRankPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return 0, nil, false
	}
	items := make([]boardRankSnapshot, 0, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		code := normalizeBoardCode(item.Code)
		if code == "" {
			continue
		}
		items = append(items, boardRankSnapshot{
			Code: code, Percent: rawNumber(item.Percent), MainNet: rawNumber(item.MainNet), Turnover: rawNumber(item.Turnover),
			RiseCount: item.RiseCount, FallCount: item.FallCount, FlatCount: item.FlatCount,
		})
	}
	return payload.Data.Total, items, true
}

func buildBoardRankUniverse(total int, items []boardRankSnapshot) boardRankUniverse {
	rankBoardSnapshots(items, func(item boardRankSnapshot) float64 { return item.Percent }, func(item *boardRankSnapshot, rank int) { item.ChangeRank = rank })
	rankBoardSnapshots(items, func(item boardRankSnapshot) float64 { return item.MainNet }, func(item *boardRankSnapshot, rank int) { item.FlowRank = rank })
	rankBoardSnapshots(items, func(item boardRankSnapshot) float64 { return item.Turnover }, func(item *boardRankSnapshot, rank int) { item.TurnoverRank = rank })
	if total < len(items) {
		total = len(items)
	}
	result := make(map[string]boardRankSnapshot, len(items))
	for _, item := range items {
		item.UniverseSize = total
		result[item.Code] = item
	}
	return boardRankUniverse{Total: total, Items: result}
}

func ParseBoardRankPayload(raw string) map[string]boardRankSnapshot {
	total, items, ok := parseBoardRankPage(raw)
	if !ok {
		return nil
	}
	return buildBoardRankUniverse(total, items).Items
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
			Percent: rawNumber(item.Percent), Turnover: rawNumber(item.Turnover),
			MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
			RiseCount: item.RiseCount, FallCount: item.FallCount, FlatCount: item.FlatCount,
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
		"fields": {"f12,f13,f14,f3,f8,f62,f104,f105,f106,f184,f128,f136,f140,f141"},
		"secids": {joined},
		"ut":     {"b2884a393a59ad64002292a3e90d46a5"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func boardRankAddress(base, kind, metric string) string {
	boardType := "2"
	if kind == domain.BoardKindConcept {
		boardType = "3"
	}
	if strings.Contains(base, "{type}") {
		address := strings.ReplaceAll(base, "{type}", boardType)
		return strings.ReplaceAll(address, "{metric}", url.QueryEscape(metric))
	}
	values := url.Values{
		"fields": {"f3,f8,f12,f14,f62,f104,f105,f106"},
		"fid":    {metric},
		"fltt":   {"2"},
		"fs":     {"m:90+t:" + boardType + "+f:!50"},
		"invt":   {"2"},
		"np":     {"1"},
		"pn":     {"1"},
		"po":     {"1"},
		"pz":     {strconv.Itoa(boardRankPageSize)},
		"ut":     {"bd1d9ddb04089700cf9c27f6f7426281"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) fetchBoardFlows(ctx context.Context, memberships []boardMembership) (map[string]domain.BoardFlow, error) {
	configuredBase := os.Getenv("ASTOCK_BOARD_FLOW_API_URL")
	bases := []string{configuredBase}
	if configuredBase == "" {
		bases = []string{boardFlowAPIURL, boardFlowFallbackURL}
	}
	var lastError error
	for _, base := range bases {
		requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		raw, err := fetchDecoded(requestContext, boardFlowAddress(base, memberships), nil)
		cancel()
		if err != nil {
			lastError = err
			continue
		}
		flows := ParseBoardFlowPayload(raw)
		if flows == nil {
			lastError = fmt.Errorf("未解析到板块资金流")
			continue
		}
		return flows, nil
	}
	return nil, lastError
}

func boardRankBases(kind string) []string {
	configuredBase := os.Getenv("ASTOCK_BOARD_RANK_API_URL")
	if configuredBase != "" {
		return []string{configuredBase}
	}
	if kind == domain.BoardKindConcept {
		return []string{boardRankDelayURL, boardRankAPIURL, boardRankFallbackURL}
	}
	return []string{boardRankAPIURL, boardRankDelayURL, boardRankFallbackURL}
}

func (EastmoneyClient) fetchBoardRankMetric(ctx context.Context, kind, metric string) (int, []boardRankSnapshot, error) {
	var lastError error
	for _, base := range boardRankBases(kind) {
		requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		raw, err := fetchDecoded(requestContext, boardRankAddress(base, kind, metric), nil)
		cancel()
		if err != nil {
			lastError = err
			continue
		}
		total, items, ok := parseBoardRankPage(raw)
		if !ok {
			lastError = fmt.Errorf("未解析到板块排行")
			continue
		}
		return total, items, nil
	}
	return 0, nil, lastError
}

func mergeBoardRankMetric(universe *boardRankUniverse, total int, items []boardRankSnapshot, metric string) {
	if universe.Items == nil {
		universe.Items = make(map[string]boardRankSnapshot)
	}
	if total > universe.Total {
		universe.Total = total
	}
	for index, item := range items {
		current := universe.Items[item.Code]
		current.Code = item.Code
		current.Percent = item.Percent
		current.MainNet = item.MainNet
		current.Turnover = item.Turnover
		current.RiseCount = item.RiseCount
		current.FallCount = item.FallCount
		current.FlatCount = item.FlatCount
		current.UniverseSize = universe.Total
		switch metric {
		case "f62":
			current.FlowRank = index + 1
		case "f8":
			current.TurnoverRank = index + 1
		default:
			current.ChangeRank = index + 1
		}
		universe.Items[item.Code] = current
	}
}

func (client EastmoneyClient) fetchBoardRankKind(ctx context.Context, kind string) (boardRankUniverse, error) {
	universe := boardRankUniverse{}
	var lastError error
	succeeded := 0
	for _, metric := range []string{"f3", "f62", "f8"} {
		total, items, fetchError := client.fetchBoardRankMetric(ctx, kind, metric)
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		mergeBoardRankMetric(&universe, total, items, metric)
		succeeded++
	}
	if succeeded == 0 {
		return boardRankUniverse{}, lastError
	}
	return universe, nil
}

func cachedBoardRanks(needed map[string]bool) (map[string]boardRankUniverse, bool) {
	boardRankCache.Lock()
	defer boardRankCache.Unlock()
	if boardRankCache.ranks == nil || time.Since(boardRankCache.fetchedAt) >= boardRankCacheTTL {
		return nil, false
	}
	result := make(map[string]boardRankUniverse, len(needed))
	for kind := range needed {
		ranks, ok := boardRankCache.ranks[kind]
		if !ok {
			return nil, false
		}
		result[kind] = ranks
	}
	return result, true
}

func storeBoardRanks(ranks map[string]boardRankUniverse) {
	if len(ranks) == 0 {
		return
	}
	boardRankCache.Lock()
	boardRankCache.fetchedAt = time.Now()
	boardRankCache.ranks = ranks
	boardRankCache.Unlock()
}

func (client EastmoneyClient) fetchBoardRanks(ctx context.Context, memberships []boardMembership) map[string]boardRankUniverse {
	needed := make(map[string]bool)
	for _, item := range memberships {
		needed[item.Kind] = true
	}
	if os.Getenv("ASTOCK_BOARD_RANK_API_URL") == "" {
		if ranks, ok := cachedBoardRanks(needed); ok {
			return ranks
		}
	}
	type rankResult struct {
		kind  string
		ranks boardRankUniverse
		err   error
	}
	results := make(chan rankResult, len(needed))
	for kind := range needed {
		kind := kind
		go func() {
			ranks, fetchError := client.fetchBoardRankKind(ctx, kind)
			results <- rankResult{kind: kind, ranks: ranks, err: fetchError}
		}()
	}
	all := make(map[string]boardRankUniverse, len(needed))
	for range needed {
		result := <-results
		if result.err == nil {
			all[result.kind] = result.ranks
		}
	}
	if os.Getenv("ASTOCK_BOARD_RANK_API_URL") == "" && len(all) == len(needed) {
		storeBoardRanks(all)
	}
	return all
}

func (client EastmoneyClient) FetchBoards(ctx context.Context, symbol string) ([]domain.BoardFlow, error) {
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

	type flowResult struct {
		flows map[string]domain.BoardFlow
		err   error
	}
	flowResults := make(chan flowResult, 1)
	rankResults := make(chan map[string]boardRankUniverse, 1)
	go func() {
		flows, fetchError := client.fetchBoardFlows(ctx, memberships)
		flowResults <- flowResult{flows: flows, err: fetchError}
	}()
	go func() { rankResults <- client.fetchBoardRanks(ctx, memberships) }()
	fetchedFlows := <-flowResults
	if fetchedFlows.err != nil {
		return nil, fetchedFlows.err
	}
	ranks := <-rankResults
	flows := fetchedFlows.flows

	result := make([]domain.BoardFlow, 0, len(memberships))
	for _, membership := range memberships {
		item, ok := flows[membership.Code]
		if !ok {
			item = domain.BoardFlow{
				Code: membership.Code, Name: membership.Name,
				Percent: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(), Turnover: math.NaN(), LeaderPercent: math.NaN(),
			}
		}
		item.Kind = membership.Kind
		if item.Name == "" {
			item.Name = membership.Name
		}
		universe, hasUniverse := ranks[membership.Kind]
		if hasUniverse {
			item.UniverseSize = universe.Total
		}
		if rank, ok := universe.Items[membership.Code]; ok {
			item.ChangeRank = rank.ChangeRank
			item.FlowRank = rank.FlowRank
			item.TurnoverRank = rank.TurnoverRank
		}
		result = append(result, item)
	}
	return result, nil
}

package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type AdjustmentFactorClient interface {
	FetchAdjustmentFactors(context.Context, string, string, string) (domain.AdjustmentSnapshot, error)
}

type SinaAdjustmentClient struct{}

// The endpoint wraps a JSON object in a JS assignment followed by a comment.
// Decode only the JSON value; never evaluate provider JavaScript.
func parseSinaAdjustmentFactors(raw, symbol string) ([]domain.AdjustmentFactor, error) {
	prefix := "var " + symbol + "hfq="
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, prefix) {
		return nil, fmt.Errorf("复权因子证券或响应格式不匹配")
	}
	var payload struct {
		Total *int `json:"total"`
		Data  []struct {
			Date   string      `json:"d"`
			Factor json.Number `json:"f"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(raw, prefix)))
	if err := decoder.Decode(&payload); err != nil || payload.Total == nil || *payload.Total != len(payload.Data) || len(payload.Data) == 0 {
		return nil, fmt.Errorf("复权因子缺失、格式无效或条数不完整")
	}
	factors := make([]domain.AdjustmentFactor, 0, len(payload.Data))
	seen := map[string]bool{}
	for _, row := range payload.Data {
		_, dateErr := time.Parse(time.DateOnly, row.Date)
		value, err := row.Factor.Float64()
		if dateErr != nil || err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || seen[row.Date] {
			return nil, fmt.Errorf("复权因子包含无效日期、非正数或重复日期")
		}
		seen[row.Date] = true
		factors = append(factors, domain.AdjustmentFactor{Date: row.Date, Value: value})
	}
	sort.Slice(factors, func(i, j int) bool { return factors[i].Date < factors[j].Date })
	return factors, nil
}

func (SinaAdjustmentClient) FetchAdjustmentFactors(ctx context.Context, symbol, start, end string) (domain.AdjustmentSnapshot, error) {
	snapshot := domain.AdjustmentSnapshot{Symbol: symbol, Start: start, End: end, Status: "unavailable", Source: "新浪后复权因子", FetchedAt: time.Now(), Factors: []domain.AdjustmentFactor{}}
	first, firstErr := time.Parse(time.DateOnly, start)
	last, lastErr := time.Parse(time.DateOnly, end)
	if !historyCacheSymbol.MatchString(symbol) || firstErr != nil || lastErr != nil || first.After(last) {
		return snapshot, fmt.Errorf("无效复权因子证券或区间")
	}
	snapshot.SourceURL = "https://finance.sina.com.cn/realstock/company/" + symbol + "/hfq.js"
	address := os.Getenv("ASTOCK_ADJUSTMENT_API_URL")
	if address == "" {
		address = snapshot.SourceURL
	}
	raw, err := fetchDecodedDirectFirst(ctx, address, nil, nil)
	if err != nil {
		return snapshot, err
	}
	factors, err := parseSinaAdjustmentFactors(raw, symbol)
	if err != nil {
		return snapshot, err
	}
	if factors[0].Date > start {
		return snapshot, fmt.Errorf("复权因子未覆盖起始日期")
	}
	// Keep the predecessor that seeds the range, and exclude future factors.
	begin := sort.Search(len(factors), func(i int) bool { return factors[i].Date > start }) - 1
	through := sort.Search(len(factors), func(i int) bool { return factors[i].Date > end })
	snapshot.Factors = append(snapshot.Factors, factors[begin:through]...)
	snapshot.Status = "checked"
	return snapshot, nil
}

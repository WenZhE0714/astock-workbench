package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const tushareAPIURL = "https://api.tushare.pro"

type tushareResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		Fields []string            `json:"fields"`
		Items  [][]json.RawMessage `json:"items"`
	} `json:"data"`
}

func tushareIndexCode(symbol string) string {
	switch symbol {
	case "sh000001":
		return "000001.SH"
	case "sz399001":
		return "399001.SZ"
	default:
		return ""
	}
}

func rawText(value json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(value)), `"`)
}

// ParseTusharePreviousAmountPayload parses index_daily.amount, whose unit is
// thousand yuan, and normalizes it to the ten-thousand-yuan quote convention.
func ParseTusharePreviousAmountPayload(raw, tsCode, referenceDate string) (float64, bool) {
	var payload tushareResponse
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Code != 0 || payload.Data == nil {
		return math.NaN(), false
	}
	dateColumn, amountColumn := -1, -1
	for index, field := range payload.Data.Fields {
		switch field {
		case "trade_date":
			dateColumn = index
		case "amount":
			amountColumn = index
		}
	}
	if dateColumn < 0 || amountColumn < 0 {
		return math.NaN(), false
	}
	cutoff := strings.ReplaceAll(referenceDate, "-", "")
	latestDate := ""
	latestAmount := math.NaN()
	for _, row := range payload.Data.Items {
		if dateColumn >= len(row) || amountColumn >= len(row) {
			continue
		}
		date := rawText(row[dateColumn])
		if date == "" || date >= cutoff || date <= latestDate {
			continue
		}
		amount, err := strconv.ParseFloat(rawText(row[amountColumn]), 64)
		if err != nil || amount <= 0 {
			continue
		}
		if tsCode != "" {
			// The endpoint is requested per index; retaining this argument keeps
			// the parser strict when a future batch response adds more codes.
			_ = tsCode
		}
		latestDate = date
		latestAmount = amount / 10
	}
	if latestDate == "" || math.IsNaN(latestAmount) {
		return math.NaN(), false
	}
	return latestAmount, true
}

func fetchTusharePreviousAmounts(ctx context.Context, symbols []string, token string) (map[string]float64, error) {
	base := os.Getenv("ASTOCK_TUSHARE_API_URL")
	if base == "" {
		base = tushareAPIURL
	}
	local := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	startDate := local.AddDate(0, 0, -14).Format("20060102")
	endDate := local.Format("20060102")
	result := make(map[string]float64, len(symbols))
	var lastError error
	for _, symbol := range symbols {
		tsCode := tushareIndexCode(symbol)
		if tsCode == "" {
			continue
		}
		requestBody := struct {
			APIName string            `json:"api_name"`
			Token   string            `json:"token"`
			Params  map[string]string `json:"params"`
			Fields  string            `json:"fields"`
		}{
			APIName: "index_daily",
			Token:   token,
			Params: map[string]string{
				"ts_code": tsCode, "start_date": startDate, "end_date": endDate,
			},
			Fields: "ts_code,trade_date,amount",
		}
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			lastError = err
			continue
		}
		requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		request, err := http.NewRequestWithContext(requestContext, http.MethodPost, base, bytes.NewReader(encoded))
		if err != nil {
			cancel()
			lastError = err
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "Mozilla/5.0 astock-workbench/0.3")
		response, err := httpClient.Do(request)
		if err != nil {
			cancel()
			lastError = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		cancel()
		if readErr != nil {
			lastError = readErr
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastError = fmt.Errorf("Tushare HTTP %s", response.Status)
			continue
		}
		amount, ok := ParseTusharePreviousAmountPayload(string(body), tsCode, local.Format("2006-01-02"))
		if !ok {
			lastError = fmt.Errorf("未解析到 %s 的 Tushare 历史成交额", symbol)
			continue
		}
		result[symbol] = amount
	}
	if len(result) == 0 && lastError != nil {
		return nil, lastError
	}
	return result, nil
}

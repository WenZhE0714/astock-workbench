package market

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// FallbackQuoteClient keeps a preferred quote provider independent from the
// existing Tencent/TDX adapters. A partial response is accepted only when it
// contains every requested symbol; otherwise the next provider is tried.
type FallbackQuoteClient struct{ clients []QuoteClient }

func NewFallbackQuoteClient(clients ...QuoteClient) FallbackQuoteClient {
	usable := make([]QuoteClient, 0, len(clients))
	for _, client := range clients {
		if client != nil {
			usable = append(usable, client)
		}
	}
	return FallbackQuoteClient{clients: usable}
}

func (client FallbackQuoteClient) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	if len(client.clients) == 0 {
		return nil, fmt.Errorf("行情回退源未初始化")
	}
	errors := make([]string, 0, len(client.clients))
	for sourceIndex, source := range client.clients {
		quotes, err := source.Fetch(ctx, symbols)
		if err == nil && quotesCoverSymbols(quotes, symbols) {
			if len(client.clients) > 1 && sourceIndex == 0 {
				if metadata, metadataErr := client.clients[1].Fetch(ctx, symbols); metadataErr == nil {
					quotes = enrichPreferredQuotes(quotes, metadata)
				}
			}
			return quotes, nil
		}
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			errors = append(errors, fmt.Sprintf("仅返回%d/%d个行情", len(quotes), len(symbols)))
		}
	}
	return nil, fmt.Errorf("行情数据源均不可用: %s", strings.Join(errors, "；"))
}

func enrichPreferredQuotes(preferred, metadata []domain.Quote) []domain.Quote {
	bySymbol := make(map[string]domain.Quote, len(metadata))
	for _, quote := range metadata {
		bySymbol[strings.ToLower(strings.TrimSpace(quote.Symbol))] = quote
	}
	result := append([]domain.Quote(nil), preferred...)
	missing := func(value string) bool {
		value = strings.TrimSpace(value)
		return value == "" || value == "--" || value == "0"
	}
	for index := range result {
		fallback, ok := bySymbol[strings.ToLower(strings.TrimSpace(result[index].Symbol))]
		if !ok {
			continue
		}
		if result[index].Name == "" {
			result[index].Name = fallback.Name
		}
		if result[index].TaskName == "" {
			result[index].TaskName = fallback.TaskName
		}
		if result[index].Code == "" {
			result[index].Code = fallback.Code
		}
		if missing(result[index].LimitUp) {
			result[index].LimitUp = fallback.LimitUp
		}
		if missing(result[index].LimitDown) {
			result[index].LimitDown = fallback.LimitDown
		}
		if missing(result[index].VolumeRatio) {
			result[index].VolumeRatio = fallback.VolumeRatio
		}
		if missing(result[index].AveragePrice) {
			result[index].AveragePrice = fallback.AveragePrice
		}
		if missing(result[index].PETTM) {
			result[index].PETTM = fallback.PETTM
		}
		if missing(result[index].PEStatic) {
			result[index].PEStatic = fallback.PEStatic
		}
		if missing(result[index].PB) {
			result[index].PB = fallback.PB
		}
		if missing(result[index].Amplitude) {
			result[index].Amplitude = fallback.Amplitude
		}
		if result[index].MarketCap <= 0 {
			result[index].MarketCap = fallback.MarketCap
		}
		if result[index].FloatMarketCap <= 0 {
			result[index].FloatMarketCap = fallback.FloatMarketCap
		}
		if len(result[index].Bids) == 0 {
			result[index].Bids = fallback.Bids
		}
		if len(result[index].Asks) == 0 {
			result[index].Asks = fallback.Asks
		}
	}
	return result
}

func quotesCoverSymbols(quotes []domain.Quote, symbols []string) bool {
	if len(symbols) == 0 {
		return true
	}
	seen := make(map[string]bool, len(quotes))
	for _, quote := range quotes {
		price, err := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			continue
		}
		seen[strings.ToLower(strings.TrimSpace(quote.Symbol))] = true
	}
	for _, symbol := range symbols {
		if !seen[strings.ToLower(strings.TrimSpace(symbol))] {
			return false
		}
	}
	return true
}

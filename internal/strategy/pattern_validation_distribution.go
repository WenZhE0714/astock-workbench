package strategy

import (
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func patternPoolDistribution(pool domain.PatternPool, samples []domain.PatternValidationSample, horizon int) ([]domain.PatternStockSummary, []domain.PatternIndustrySummary) {
	bySymbol := map[string][]domain.PatternValidationSample{}
	for _, sample := range samples {
		bySymbol[sample.Symbol] = append(bySymbol[sample.Symbol], sample)
	}
	stocks := []domain.PatternStockSummary{}
	industries := []domain.PatternIndustrySummary{}
	industrySamples := map[string][]domain.PatternValidationSample{}
	industryStocks := map[string]int{}
	share := func(count int) float64 {
		if len(samples) == 0 {
			return 0
		}
		return float64(count) / float64(len(samples)) * 100
	}
	for _, member := range pool.Members {
		industry := member.Industry
		if industry == "" {
			industry = "未标注"
		}
		items := bySymbol[member.Symbol]
		stocks = append(stocks, domain.PatternStockSummary{Symbol: member.Symbol, Name: member.Name, Industry: industry, SampleShare: share(len(items)), Totals: SummarizePatternValidation(items, horizon)})
		industryStocks[industry]++
		industrySamples[industry] = append(industrySamples[industry], items...)
	}
	for industry, items := range industrySamples {
		industries = append(industries, domain.PatternIndustrySummary{Industry: industry, Stocks: industryStocks[industry], SampleShare: share(len(items)), Totals: SummarizePatternValidation(items, horizon)})
	}
	sort.Slice(stocks, func(i, j int) bool {
		if stocks[i].Totals.Samples != stocks[j].Totals.Samples {
			return stocks[i].Totals.Samples > stocks[j].Totals.Samples
		}
		return stocks[i].Symbol < stocks[j].Symbol
	})
	sort.Slice(industries, func(i, j int) bool {
		if industries[i].Totals.Samples != industries[j].Totals.Samples {
			return industries[i].Totals.Samples > industries[j].Totals.Samples
		}
		return industries[i].Industry < industries[j].Industry
	})
	return stocks, industries
}

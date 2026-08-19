package storage

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

const viewHistoryLimit = 100

func cleanViewHistoryName(value string) string {
	value = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

// LoadViewHistory returns the most recently viewed asset first. Malformed and
// duplicate records are ignored so a partially edited file remains usable.
func LoadViewHistory(file string) ([]domain.Candidate, error) {
	data, err := readOptionalFile(file)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Candidate, 0, viewHistoryLimit)
	seen := make(map[string]bool)
	for _, rawLine := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(strings.TrimSuffix(rawLine, "\r"), "\t", 2)
		if len(fields) != 2 {
			continue
		}
		symbol := strings.ToLower(strings.TrimSpace(fields[0]))
		if !market.ValidAssetSymbol(symbol) || seen[symbol] {
			continue
		}
		seen[symbol] = true
		result = append(result, domain.Candidate{
			Symbol: symbol,
			Name:   cleanViewHistoryName(fields[1]),
		})
		if len(result) == viewHistoryLimit {
			break
		}
	}
	return result, nil
}

// RecordViewHistory inserts or moves an asset to the front of the persisted
// MRU list. An empty new name keeps the previously recorded name.
func RecordViewHistory(file, symbol, name string) error {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if !market.ValidAssetSymbol(symbol) {
		return fmt.Errorf("无效历史证券或板块代码 %q", symbol)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		return err
	}
	name = cleanViewHistoryName(name)
	result := make([]domain.Candidate, 0, min(len(items)+1, viewHistoryLimit))
	for _, item := range items {
		if item.Symbol == symbol {
			if name == "" {
				name = item.Name
			}
			break
		}
	}
	result = append(result, domain.Candidate{Symbol: symbol, Name: name})
	for _, item := range items {
		if item.Symbol == symbol {
			continue
		}
		result = append(result, item)
		if len(result) == viewHistoryLimit {
			break
		}
	}

	var builder strings.Builder
	for _, item := range result {
		fmt.Fprintf(&builder, "%s\t%s\n", item.Symbol, cleanViewHistoryName(item.Name))
	}
	return atomicWrite(file, []byte(builder.String()), 0o600)
}

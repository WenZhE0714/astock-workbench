package storage

import (
	"fmt"
	"io"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/market"
)

const watchlistHeader = "# astock 自选股：每行一个 sh/sz 前缀代码\n"

func LoadWatchlist(file string) ([]string, []string, error) {
	data, err := readOptionalFile(file)
	if err != nil {
		return nil, nil, err
	}
	result := make([]string, 0)
	warnings := make([]string, 0)
	seen := make(map[string]bool)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		if line == "" {
			continue
		}
		symbol, status := market.InspectSymbol(line)
		if status != "ok" {
			warnings = append(warnings, "忽略自选股中的无效代码: "+line)
			continue
		}
		if !seen[symbol] {
			seen[symbol] = true
			result = append(result, symbol)
		}
	}
	return result, warnings, nil
}

func SaveWatchlist(file string, symbols []string) error {
	var builder strings.Builder
	builder.WriteString(watchlistHeader)
	for _, symbol := range symbols {
		builder.WriteString(symbol)
		builder.WriteByte('\n')
	}
	return atomicWrite(file, []byte(builder.String()), 0o600)
}

func AddWatchlist(file string, additions []string) ([]bool, error) {
	symbols, _, err := LoadWatchlist(file)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		seen[symbol] = true
	}
	added := make([]bool, len(additions))
	for index, symbol := range additions {
		if !seen[symbol] {
			seen[symbol] = true
			symbols = append(symbols, symbol)
			added[index] = true
		}
	}
	return added, SaveWatchlist(file, symbols)
}

func RemoveWatchlist(file string, removals []string) ([]bool, error) {
	symbols, _, err := LoadWatchlist(file)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	removing := make(map[string]bool)
	for _, symbol := range symbols {
		existing[symbol] = true
	}
	for _, symbol := range removals {
		removing[symbol] = true
	}
	kept := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if !removing[symbol] {
			kept = append(kept, symbol)
		}
	}
	removed := make([]bool, len(removals))
	for index, symbol := range removals {
		removed[index] = existing[symbol]
	}
	return removed, SaveWatchlist(file, kept)
}

func PrintWatchlist(output io.Writer, file string, cache *NameCache) error {
	symbols, warnings, err := LoadWatchlist(file)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintf(output, "警告: %s\n", warning)
	}
	if len(symbols) == 0 {
		fmt.Fprintln(output, "自选股为空。可运行: astock add 600519 000001")
		return nil
	}
	for _, symbol := range symbols {
		name := cache.LookupName(symbol)
		if name != "" {
			fmt.Fprintf(output, "%s  %s  %s\n", symbol[2:], market.MarketText(symbol), name)
		} else {
			fmt.Fprintf(output, "%s  %s\n", symbol[2:], market.MarketText(symbol))
		}
	}
	return nil
}

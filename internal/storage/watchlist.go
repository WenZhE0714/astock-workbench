package storage

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wenzhe/astock-workbench/internal/market"
)

const (
	DefaultWatchlistGroup = "默认"
	AllWatchlistGroup     = "全部"
	watchlistHeader       = "# astock 自选股分组：[分组名] 后每行一个 sh/sz 前缀代码\n"
)

type WatchlistGroup struct {
	Name    string
	Symbols []string
}

func validateWatchlistGroupName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", fmt.Errorf("分组名称不能为空")
	}
	if name == AllWatchlistGroup {
		return "", fmt.Errorf("“%s”是保留分组名称", AllWatchlistGroup)
	}
	if utf8.RuneCountInString(name) > 24 {
		return "", fmt.Errorf("分组名称最多 24 个字符")
	}
	if strings.ContainsAny(name, "[]#\r\n") {
		return "", fmt.Errorf("分组名称不能包含 [ ] # 或换行")
	}
	return name, nil
}

func findWatchlistGroup(groups []WatchlistGroup, name string) int {
	for index := range groups {
		if groups[index].Name == name {
			return index
		}
	}
	return -1
}

func ensureDefaultWatchlistGroup(groups []WatchlistGroup) []WatchlistGroup {
	index := findWatchlistGroup(groups, DefaultWatchlistGroup)
	if index == 0 {
		return groups
	}
	if index > 0 {
		group := groups[index]
		groups = append(groups[:index], groups[index+1:]...)
		return append([]WatchlistGroup{group}, groups...)
	}
	return append([]WatchlistGroup{{Name: DefaultWatchlistGroup}}, groups...)
}

func LoadWatchlistGroups(file string) ([]WatchlistGroup, []string, error) {
	data, err := readOptionalFile(file)
	if err != nil {
		return nil, nil, err
	}
	groups := []WatchlistGroup{{Name: DefaultWatchlistGroup}}
	warnings := make([]string, 0)
	current := 0
	seen := map[string]map[string]bool{DefaultWatchlistGroup: {}}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name, nameError := validateWatchlistGroupName(strings.TrimSpace(line[1 : len(line)-1]))
			if nameError != nil {
				warnings = append(warnings, "忽略无效自选分组: "+line)
				continue
			}
			current = findWatchlistGroup(groups, name)
			if current < 0 {
				groups = append(groups, WatchlistGroup{Name: name})
				current = len(groups) - 1
				seen[name] = make(map[string]bool)
			}
			continue
		}
		symbol, status := market.InspectSymbol(line)
		if status != "ok" {
			warnings = append(warnings, "忽略自选股中的无效代码: "+line)
			continue
		}
		groupName := groups[current].Name
		if !seen[groupName][symbol] {
			seen[groupName][symbol] = true
			groups[current].Symbols = append(groups[current].Symbols, symbol)
		}
	}
	return ensureDefaultWatchlistGroup(groups), warnings, nil
}

func SaveWatchlistGroups(file string, groups []WatchlistGroup) error {
	groups = ensureDefaultWatchlistGroup(append([]WatchlistGroup(nil), groups...))
	var builder strings.Builder
	builder.WriteString(watchlistHeader)
	for _, group := range groups {
		name, err := validateWatchlistGroupName(group.Name)
		if err != nil {
			return err
		}
		builder.WriteByte('[')
		builder.WriteString(name)
		builder.WriteString("]\n")
		seen := make(map[string]bool)
		for _, rawSymbol := range group.Symbols {
			symbol, status := market.InspectSymbol(rawSymbol)
			if status != "ok" {
				return fmt.Errorf("分组 %s 包含无效股票代码 %q", name, rawSymbol)
			}
			if seen[symbol] {
				continue
			}
			seen[symbol] = true
			builder.WriteString(symbol)
			builder.WriteByte('\n')
		}
	}
	return atomicWrite(file, []byte(builder.String()), 0o600)
}

func WatchlistSymbols(groups []WatchlistGroup, groupName string) []string {
	if groupName != "" && groupName != AllWatchlistGroup {
		if index := findWatchlistGroup(groups, groupName); index >= 0 {
			return append([]string(nil), groups[index].Symbols...)
		}
		return nil
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, symbol := range group.Symbols {
			if !seen[symbol] {
				seen[symbol] = true
				result = append(result, symbol)
			}
		}
	}
	return result
}

func LoadWatchlist(file string) ([]string, []string, error) {
	groups, warnings, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, nil, err
	}
	return WatchlistSymbols(groups, AllWatchlistGroup), warnings, nil
}

func SaveWatchlist(file string, symbols []string) error {
	return SaveWatchlistGroups(file, []WatchlistGroup{{Name: DefaultWatchlistGroup, Symbols: symbols}})
}

func AddWatchlist(file string, additions []string) ([]bool, error) {
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	for _, symbol := range WatchlistSymbols(groups, AllWatchlistGroup) {
		existing[symbol] = true
	}
	defaultIndex := findWatchlistGroup(groups, DefaultWatchlistGroup)
	added := make([]bool, len(additions))
	for index, symbol := range additions {
		if !existing[symbol] {
			existing[symbol] = true
			groups[defaultIndex].Symbols = append(groups[defaultIndex].Symbols, symbol)
			added[index] = true
		}
	}
	return added, SaveWatchlistGroups(file, groups)
}

func AddWatchlistToGroup(file, groupName string, additions []string) ([]bool, error) {
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, err
	}
	groupIndex := findWatchlistGroup(groups, groupName)
	if groupIndex < 0 {
		return nil, fmt.Errorf("自选分组不存在: %s", groupName)
	}
	existing := make(map[string]bool)
	for _, symbol := range groups[groupIndex].Symbols {
		existing[symbol] = true
	}
	added := make([]bool, len(additions))
	for index, symbol := range additions {
		if !existing[symbol] {
			existing[symbol] = true
			groups[groupIndex].Symbols = append(groups[groupIndex].Symbols, symbol)
			added[index] = true
		}
	}
	return added, SaveWatchlistGroups(file, groups)
}

func RemoveWatchlist(file string, removals []string) ([]bool, error) {
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool)
	removing := make(map[string]bool)
	for _, symbol := range WatchlistSymbols(groups, AllWatchlistGroup) {
		existing[symbol] = true
	}
	for _, symbol := range removals {
		removing[symbol] = true
	}
	for groupIndex := range groups {
		kept := groups[groupIndex].Symbols[:0]
		for _, symbol := range groups[groupIndex].Symbols {
			if !removing[symbol] {
				kept = append(kept, symbol)
			}
		}
		groups[groupIndex].Symbols = kept
	}
	removed := make([]bool, len(removals))
	for index, symbol := range removals {
		removed[index] = existing[symbol]
	}
	return removed, SaveWatchlistGroups(file, groups)
}

func RemoveWatchlistFromGroup(file, groupName string, removals []string) ([]bool, error) {
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, err
	}
	groupIndex := findWatchlistGroup(groups, groupName)
	if groupIndex < 0 {
		return nil, fmt.Errorf("自选分组不存在: %s", groupName)
	}
	existing := make(map[string]bool)
	removing := make(map[string]bool)
	for _, symbol := range groups[groupIndex].Symbols {
		existing[symbol] = true
	}
	for _, symbol := range removals {
		removing[symbol] = true
	}
	kept := groups[groupIndex].Symbols[:0]
	for _, symbol := range groups[groupIndex].Symbols {
		if !removing[symbol] {
			kept = append(kept, symbol)
		}
	}
	groups[groupIndex].Symbols = kept
	removed := make([]bool, len(removals))
	for index, symbol := range removals {
		removed[index] = existing[symbol]
	}
	return removed, SaveWatchlistGroups(file, groups)
}

// SetWatchlistSymbolGroups replaces one stock's group memberships while
// preserving its position in groups where it remains selected. An empty
// selection falls back to the default group so the stock cannot disappear.
func SetWatchlistSymbolGroups(file, rawSymbol string, groupNames []string) ([]string, error) {
	symbol, status := market.InspectSymbol(rawSymbol)
	if status != "ok" {
		return nil, fmt.Errorf("无效股票代码 %q", rawSymbol)
	}
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(groupNames))
	for _, rawName := range groupNames {
		name, nameError := validateWatchlistGroupName(rawName)
		if nameError != nil {
			return nil, nameError
		}
		if findWatchlistGroup(groups, name) < 0 {
			return nil, fmt.Errorf("自选分组不存在: %s", name)
		}
		wanted[name] = true
	}
	if len(wanted) == 0 {
		wanted[DefaultWatchlistGroup] = true
	}

	assigned := make([]string, 0, len(wanted))
	for groupIndex := range groups {
		group := &groups[groupIndex]
		contains := false
		kept := group.Symbols[:0]
		for _, existing := range group.Symbols {
			if existing == symbol {
				contains = true
				if !wanted[group.Name] {
					continue
				}
			}
			kept = append(kept, existing)
		}
		group.Symbols = kept
		if wanted[group.Name] {
			assigned = append(assigned, group.Name)
			if !contains {
				group.Symbols = append(group.Symbols, symbol)
			}
		}
	}
	return assigned, SaveWatchlistGroups(file, groups)
}

func SaveWatchlistGroupOrder(file, groupName string, symbols []string) error {
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return err
	}
	groupIndex := findWatchlistGroup(groups, groupName)
	if groupIndex < 0 {
		return fmt.Errorf("自选分组不存在: %s", groupName)
	}
	existing := make(map[string]int)
	for _, symbol := range groups[groupIndex].Symbols {
		existing[symbol]++
	}
	for _, symbol := range symbols {
		existing[symbol]--
	}
	if len(symbols) != len(groups[groupIndex].Symbols) {
		return fmt.Errorf("分组内容已变化，请重新进入排序")
	}
	for _, count := range existing {
		if count != 0 {
			return fmt.Errorf("分组内容已变化，请重新进入排序")
		}
	}
	groups[groupIndex].Symbols = append([]string(nil), symbols...)
	return SaveWatchlistGroups(file, groups)
}

func CreateWatchlistGroup(file, groupName string) (bool, error) {
	name, err := validateWatchlistGroupName(groupName)
	if err != nil {
		return false, err
	}
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return false, err
	}
	if findWatchlistGroup(groups, name) >= 0 {
		return false, nil
	}
	groups = append(groups, WatchlistGroup{Name: name})
	return true, SaveWatchlistGroups(file, groups)
}

func DeleteWatchlistGroup(file, groupName string) (deleted bool, movedToDefault int, err error) {
	if groupName == DefaultWatchlistGroup || groupName == AllWatchlistGroup {
		return false, 0, fmt.Errorf("不能删除%s分组", groupName)
	}
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		return false, 0, err
	}
	groupIndex := findWatchlistGroup(groups, groupName)
	if groupIndex < 0 {
		return false, 0, nil
	}
	defaultIndex := findWatchlistGroup(groups, DefaultWatchlistGroup)
	existingElsewhere := make(map[string]bool)
	for index, group := range groups {
		if index == groupIndex {
			continue
		}
		for _, symbol := range group.Symbols {
			existingElsewhere[symbol] = true
		}
	}
	for _, symbol := range groups[groupIndex].Symbols {
		if !existingElsewhere[symbol] {
			existingElsewhere[symbol] = true
			groups[defaultIndex].Symbols = append(groups[defaultIndex].Symbols, symbol)
			movedToDefault++
		}
	}
	groups = append(groups[:groupIndex], groups[groupIndex+1:]...)
	return true, movedToDefault, SaveWatchlistGroups(file, groups)
}

func PrintWatchlist(output io.Writer, file string, cache *NameCache) error {
	groups, warnings, err := LoadWatchlistGroups(file)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintf(output, "警告: %s\n", warning)
	}
	if len(WatchlistSymbols(groups, AllWatchlistGroup)) == 0 {
		fmt.Fprintln(output, "自选股为空。可运行: astock add 600519 000001")
		return nil
	}
	for groupIndex, group := range groups {
		fmt.Fprintf(output, "[%s] %d只\n", group.Name, len(group.Symbols))
		for _, symbol := range group.Symbols {
			name := cache.LookupName(symbol)
			if name != "" {
				fmt.Fprintf(output, "  %s  %s  %s\n", symbol[2:], market.MarketText(symbol), name)
			} else {
				fmt.Fprintf(output, "  %s  %s\n", symbol[2:], market.MarketText(symbol))
			}
		}
		if groupIndex < len(groups)-1 {
			fmt.Fprintln(output)
		}
	}
	return nil
}

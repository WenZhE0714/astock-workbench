package storage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	pinyinlib "github.com/mozillazg/go-pinyin"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

type nameRecord struct {
	Symbol    string
	Name      string
	Timestamp int64
}

type NameCache struct {
	file    string
	records map[string]nameRecord
}

func LoadNameCache(file string) (*NameCache, error) {
	result := &NameCache{file: file, records: make(map[string]nameRecord)}
	data, err := readOptionalFile(file)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		timestamp, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || !market.ValidAssetSymbol(fields[0]) || fields[1] == "" {
			continue
		}
		result.records[fields[0]] = nameRecord{Symbol: fields[0], Name: fields[1], Timestamp: timestamp}
	}
	return result, nil
}

func (cache *NameCache) LookupSymbol(name string) string {
	now := time.Now().Unix()
	for _, record := range cache.records {
		if record.Name == name && now-record.Timestamp <= 24*60*60 {
			return record.Symbol
		}
	}
	return ""
}

func (cache *NameCache) LookupName(symbol string) string {
	return cache.records[symbol].Name
}

func (cache *NameCache) Remember(candidates []domain.Candidate) error {
	now := time.Now().Unix()
	changed := false
	for _, item := range candidates {
		if !market.ValidAssetSymbol(item.Symbol) || item.Name == "" {
			continue
		}
		previous, exists := cache.records[item.Symbol]
		if exists && previous.Name == item.Name && now-previous.Timestamp < 60 {
			continue
		}
		cache.records[item.Symbol] = nameRecord{Symbol: item.Symbol, Name: item.Name, Timestamp: now}
		changed = true
	}
	if !changed {
		return nil
	}
	return cache.save()
}

func (cache *NameCache) save() error {
	keys := make([]string, 0, len(cache.records))
	for key := range cache.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		record := cache.records[key]
		fmt.Fprintf(&builder, "%s\t%s\t%d\n", record.Symbol, record.Name, record.Timestamp)
	}
	return atomicWrite(cache.file, []byte(builder.String()), 0o600)
}

type PinyinCache struct {
	file    string
	records map[string]string
}

func LoadPinyinCache(file string) (*PinyinCache, error) {
	result := &PinyinCache{file: file, records: make(map[string]string)}
	data, err := readOptionalFile(file)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) == 2 && fields[0] != "" && fields[1] != "" {
			result.records[fields[0]] = fields[1]
		}
	}
	return result, nil
}

func ToPinyin(name string) string {
	original := name
	arguments := pinyinlib.NewArgs()
	arguments.Style = pinyinlib.Normal
	arguments.Fallback = func(character rune, _ pinyinlib.Args) []string {
		return []string{string(character)}
	}
	phrases := []struct {
		text  string
		value string
	}{
		{text: "银行", value: "yin hang"},
		{text: "重庆", value: "chong qing"},
		{text: "厦门", value: "xia men"},
		{text: "西藏", value: "xi zang"},
		{text: "长", value: "chang"},
	}
	parts := make([]string, 0, len([]rune(name)))
	for len(name) > 0 {
		matched := false
		for _, phrase := range phrases {
			if strings.HasPrefix(name, phrase.text) {
				parts = append(parts, strings.Fields(phrase.value)...)
				name = strings.TrimPrefix(name, phrase.text)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		character, size := utf8.DecodeRuneInString(name)
		if character <= unicode.MaxASCII && (unicode.IsLetter(character) || unicode.IsDigit(character)) {
			end := size
			for end < len(name) {
				next, nextSize := utf8.DecodeRuneInString(name[end:])
				if next > unicode.MaxASCII || (!unicode.IsLetter(next) && !unicode.IsDigit(next)) {
					break
				}
				end += nextSize
			}
			parts = append(parts, name[:end])
			name = name[end:]
			continue
		}
		parts = append(parts, pinyinlib.LazyPinyin(string(character), arguments)...)
		name = name[size:]
	}
	value := strings.ToLower(strings.Join(parts, " "))
	value = strings.Join(strings.FieldsFunc(value, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}), " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return original
	}
	return value
}

func (cache *PinyinCache) Decorate(quotes []domain.Quote) error {
	changed := false
	for index := range quotes {
		value := cache.records[quotes[index].Name]
		if value == "" {
			value = ToPinyin(quotes[index].Name)
			cache.records[quotes[index].Name] = value
			changed = true
		}
		quotes[index].TaskName = value
	}
	if !changed {
		return nil
	}
	keys := make([]string, 0, len(cache.records))
	for key := range cache.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&builder, "%s\t%s\n", key, cache.records[key])
	}
	return atomicWrite(cache.file, []byte(builder.String()), 0o600)
}

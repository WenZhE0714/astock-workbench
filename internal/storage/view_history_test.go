package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRecordViewHistoryMaintainsMRUOrder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	for _, item := range []struct {
		symbol string
		name   string
	}{
		{symbol: "sh600519", name: "贵州茅台"},
		{symbol: "sz000001", name: "平安银行"},
		{symbol: "sh600519", name: "贵州茅台"},
	} {
		if err := RecordViewHistory(file, item.symbol, item.name); err != nil {
			t.Fatal(err)
		}
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Symbol)
	}
	if !reflect.DeepEqual(got, []string{"sh600519", "sz000001"}) {
		t.Fatalf("unexpected MRU order: %#v", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected history permissions: %o", info.Mode().Perm())
	}
}

func TestRecordViewHistoryPreservesExistingName(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	if err := RecordViewHistory(file, "sh600519", "贵州\t茅台"); err != nil {
		t.Fatal(err)
	}
	if err := RecordViewHistory(file, "sh600519", ""); err != nil {
		t.Fatal(err)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "贵州 茅台" {
		t.Fatalf("existing name was not preserved: %#v", items)
	}
}

func TestRecordViewHistorySupportsTHSIndustryAndMovesItToFront(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	if err := RecordViewHistory(file, "sh600519", "贵州茅台"); err != nil {
		t.Fatal(err)
	}
	if err := RecordViewHistory(file, "th881155", "银行"); err != nil {
		t.Fatal(err)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Symbol != "th881155" || items[0].Name != "银行" || items[1].Symbol != "sh600519" {
		t.Fatalf("unexpected mixed asset history: %#v", items)
	}
}

func TestLoadViewHistoryIgnoresInvalidAndDuplicateLines(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	data := strings.Join([]string{
		"sh600519\t贵州茅台",
		"600000\t浦发银行",
		"invalid",
		"sz000001\t平安银行",
		"sh600519\t重复记录",
		"sh60051x\t无效代码",
	}, "\n")
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Symbol != "sh600519" || items[0].Name != "贵州茅台" || items[1].Symbol != "sz000001" {
		t.Fatalf("unexpected parsed history: %#v", items)
	}
}

func TestLoadViewHistoryIsBounded(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	var builder strings.Builder
	for index := 0; index < viewHistoryLimit+5; index++ {
		fmt.Fprintf(&builder, "sh%06d\t股票%d\n", 600000+index, index)
	}
	if err := os.WriteFile(file, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != viewHistoryLimit || items[0].Symbol != "sh600000" || items[len(items)-1].Symbol != "sh600099" {
		t.Fatalf("unexpected bounded history: first=%#v last=%#v count=%d", items[0], items[len(items)-1], len(items))
	}
}

func TestRecordViewHistoryDropsOldestItemAtLimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "view-history.tsv")
	var builder strings.Builder
	for index := 0; index < viewHistoryLimit; index++ {
		fmt.Fprintf(&builder, "sh%06d\t股票%d\n", 600000+index, index)
	}
	if err := os.WriteFile(file, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordViewHistory(file, "sz000001", "平安银行"); err != nil {
		t.Fatal(err)
	}
	items, err := LoadViewHistory(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != viewHistoryLimit || items[0].Symbol != "sz000001" || items[len(items)-1].Symbol != "sh600098" {
		t.Fatalf("oldest history item was not dropped: first=%#v last=%#v count=%d", items[0], items[len(items)-1], len(items))
	}
}

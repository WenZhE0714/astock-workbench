package app

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type partialFundFlowMock struct{}

func (partialFundFlowMock) Fetch(context.Context, []string) (map[string]domain.FundFlow, error) {
	return map[string]domain.FundFlow{
		"sh600519": {Symbol: "sh600519", Source: market.THSFundFlowSource, MainNet: 4e8, MainRatio: 8},
	}, fmt.Errorf("指数资金暂不可用")
}

func TestWatchSnapshotRetainsPartialFundFlow(t *testing.T) {
	var output bytes.Buffer
	names, err := storage.LoadNameCache(filepath.Join(t.TempDir(), "names.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{out: &output, names: names, quotes: stockReportQuoteMock{}, flows: partialFundFlowMock{}}
	if err := app.watchLoop(context.Background(), []string{"sh600519"}, watchOptions{Once: true}, "默认"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"4.00亿", market.THSFundFlowSource, "指数资金暂不可用"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("partial snapshot missing %q:\n%s", expected, output.String())
		}
	}
}

func TestStockReportPreservesFallbackSourceAndRejectsMixedDeltas(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: partialFundFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{}, news: stockReportNewsMock{},
	}
	movement := domain.FundMovement{Symbol: "sh600519", Source: "东方财富主力净额", Delta1Minute: 1e8}
	facts, err := app.collectStockReportFacts(context.Background(), "sh600519", &movement, nil)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Fund.Source != market.THSFundFlowSource || facts.Fund.MainNet != 4e8 || facts.FundMovement != nil {
		t.Fatalf("fallback facts or source boundary lost: fund=%+v movement=%+v", facts.Fund, facts.FundMovement)
	}
	warnings := strings.Join(facts.Warnings, "|")
	for _, expected := range []string{"指数资金暂不可用", "同花顺公开大单净额", "已排除跨来源增量"} {
		if !strings.Contains(warnings, expected) {
			t.Fatalf("missing warning %q: %s", expected, warnings)
		}
	}
	if report := renderDeterministicStockReport(facts, ""); !strings.Contains(report, "资金来源："+market.THSFundFlowSource) {
		t.Fatal("fallback report omitted provenance")
	}
}

package market

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type fundFlowFunc func(context.Context, []string) (map[string]domain.FundFlow, error)

func (fn fundFlowFunc) Fetch(ctx context.Context, symbols []string) (map[string]domain.FundFlow, error) {
	return fn(ctx, symbols)
}

func TestFallbackFundFlowFillsOnlyMissingAndInvalidRows(t *testing.T) {
	primary := fundFlowFunc(func(context.Context, []string) (map[string]domain.FundFlow, error) {
		return map[string]domain.FundFlow{
			"sh600519": {Symbol: "sh600519", Source: "primary", MainNet: 0},
			"sz300364": {Symbol: "sz300364", MainNet: math.NaN()},
			"sz000001": {Symbol: "sz000001", MainNet: math.Inf(1)},
			"sh000001": {Symbol: "sz000001", MainNet: 200},
		}, errors.New("partial primary")
	})
	backup := fundFlowFunc(func(_ context.Context, symbols []string) (map[string]domain.FundFlow, error) {
		if !reflect.DeepEqual(symbols, []string{"sz300364", "sz000001", "sh000001"}) {
			t.Fatalf("unexpected fallback request: %v", symbols)
		}
		return map[string]domain.FundFlow{
			"sh600519": {Symbol: "sh600519", MainNet: 99},
			"sz300364": {Symbol: "sz300364", Source: THSFundFlowSource, MainNet: 123},
			"sz000001": {Symbol: "sz000001", Source: THSFundFlowSource, MainNet: -42},
		}, errors.New("indices unsupported")
	})
	flows, err := NewFallbackFundFlowClient(primary, backup).Fetch(context.Background(), []string{"sh600519", "sz300364", "sz300364", "sz000001", "sh000001"})
	if len(flows) != 3 || flows["sh600519"].Source != "primary" || flows["sh600519"].MainNet != 0 || flows["sz300364"].MainNet != 123 {
		t.Fatalf("incorrect source merge: %v", flows)
	}
	if err == nil || !strings.Contains(err.Error(), "指数资金暂不可用") {
		t.Fatalf("missing index coverage was hidden: %v", err)
	}
}

func TestFallbackFundFlowSkipsBackupWhenComplete(t *testing.T) {
	primary := fundFlowFunc(func(context.Context, []string) (map[string]domain.FundFlow, error) {
		return map[string]domain.FundFlow{"sz300364": {Symbol: "sz300364", MainNet: 1}}, nil
	})
	backup := fundFlowFunc(func(context.Context, []string) (map[string]domain.FundFlow, error) {
		t.Fatal("backup called despite complete primary")
		return nil, nil
	})
	if _, err := NewFallbackFundFlowClient(primary, backup).Fetch(context.Background(), []string{"sz300364"}); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackFundFlowReservesDeadlineForBackup(t *testing.T) {
	primary := fundFlowFunc(func(ctx context.Context, _ []string) (map[string]domain.FundFlow, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	backup := fundFlowFunc(func(ctx context.Context, _ []string) (map[string]domain.FundFlow, error) {
		if ctx.Err() != nil {
			t.Fatal("primary consumed the fallback deadline")
		}
		return map[string]domain.FundFlow{"sz300364": {Symbol: "sz300364", MainNet: 1}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	flows, err := NewFallbackFundFlowClient(primary, backup).Fetch(ctx, []string{"sz300364"})
	if err != nil || len(flows) != 1 {
		t.Fatalf("deadline fallback failed: flows=%v err=%v", flows, err)
	}
}

func TestFallbackFundFlowReportsAllFailuresAndCancellation(t *testing.T) {
	failed := func(message string) fundFlowFunc {
		return func(context.Context, []string) (map[string]domain.FundFlow, error) { return nil, errors.New(message) }
	}
	client := NewFallbackFundFlowClient(nil, failed("eastmoney EOF"), failed("ths forbidden"))
	flows, err := client.Fetch(context.Background(), []string{"sz300364"})
	if len(flows) != 0 || err == nil || !strings.Contains(err.Error(), "eastmoney EOF") || !strings.Contains(err.Error(), "ths forbidden") {
		t.Fatalf("source failures lost: flows=%v err=%v", flows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Fetch(ctx, []string{"sz300364"}); err != context.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
}

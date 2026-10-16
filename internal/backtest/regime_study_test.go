package backtest

import (
	"context"
	"testing"
)

func TestRegimeStudyPreservesFixedCapAndReproducesReference(t *testing.T) {
	source := positionStudySource()
	source.Request.BaseRequest.Benchmark = "sh000300"
	engine := &positionStudyEngine{coverage: true}
	prior, err := RunPositionStudy(context.Background(), engine, source, "P008", []float64{.2, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.requests = nil
	result, err := RunRegimeStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || !result.BaselineReproduced || result.PromotionAllowed || len(result.Comparisons) != 3 || len(engine.requests) != 12 {
		t.Fatalf("invalid paired study: %+v", result)
	}
	for i, request := range engine.requests {
		if request.Technical.MaxPosition != .1 || request.Technical.EntryMode != EntryModeReclaim || request.Technical.StopLoss != .08 || !request.NoFutureData {
			t.Fatal("fixed strategy changed")
		}
		if i < 6 && request.EntryMarketFilter != "" || i >= 6 && request.EntryMarketFilter != EntryMarketNonBear {
			t.Fatal("unexpected filter pairing")
		}
	}
	prior.Variants[1].Windows[0].Normal.Metrics.TotalReturn++
	result, err = RunRegimeStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err != nil || result.BaselineReproduced || len(result.BaselineDifferences) == 0 {
		t.Fatal("reference drift not reported")
	}
}

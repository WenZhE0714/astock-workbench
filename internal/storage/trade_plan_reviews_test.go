package storage

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func reviewPrice(value float64) *float64 { return &value }

func TestTradePlanReviewStoreAppendsHistoryAndCalculatesRealizedR(t *testing.T) {
	store := NewTradePlanReviewStore(t.TempDir())
	plan := tradePlanFixture()
	entryAt := time.Date(2026, 9, 21, 10, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	exitAt := entryAt.Add(24 * time.Hour)
	revision := domain.TradePlanReviewRevision{
		Note: "按计划等待确认", Tags: []string{"突破", "突破"}, ExecutionStatus: "followed", Discipline: "followed",
		ActualEntry: reviewPrice(100), ActualExit: reviewPrice(104), EntryAt: entryAt, ExitAt: exitAt, ExitReason: "达到观察目标",
	}
	first, err := store.Update(plan, revision, exitAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || len(first.Revisions) != 1 || len(first.Current.Tags) != 1 || first.Current.RealizedR == nil || math.Abs(*first.Current.RealizedR-2) > 1e-9 {
		t.Fatalf("initial review is incorrect: %+v", first)
	}
	revision.Note = "复核后发现盘中追高"
	revision.ExecutionStatus = "deviated"
	second, err := store.Update(plan, revision, exitAt.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 || len(second.Revisions) != 2 || second.Revisions[0].Note != "按计划等待确认" || second.Current.Note != revision.Note {
		t.Fatalf("review history was overwritten: %+v", second)
	}
	repeated, err := store.Update(plan, revision, exitAt.Add(3*time.Hour))
	if err != nil || repeated.Sequence != 2 {
		t.Fatalf("identical review created a duplicate revision: %v %+v", err, repeated)
	}
	loaded, err := NewTradePlanReviewStore(store.root).Load(plan.Symbol, plan.ID)
	if err != nil || loaded.Sequence != 2 {
		t.Fatalf("restart lost review history: %v %+v", err, loaded)
	}
	items, err := store.List(plan.Symbol)
	if err != nil || len(items) != 1 || items[0].PlanID != plan.ID {
		t.Fatalf("review listing failed: %v %+v", err, items)
	}
	if _, err := store.Update(plan, revision, exitAt); err == nil {
		t.Fatal("review timestamp was allowed to move backwards")
	}
}

func TestTradePlanReviewStoreRejectsInvalidFillTimeline(t *testing.T) {
	store := NewTradePlanReviewStore(t.TempDir())
	plan := tradePlanFixture()
	entryAt := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	_, err := store.Update(plan, domain.TradePlanReviewRevision{
		ExecutionStatus: "followed", ActualEntry: reviewPrice(100), ActualExit: reviewPrice(101),
		EntryAt: entryAt, ExitAt: entryAt.Add(-time.Minute),
	}, entryAt.Add(time.Hour))
	if err == nil {
		t.Fatal("exit before entry was accepted")
	}
}

func TestTradePlanReviewStoreAllListsAcrossSymbols(t *testing.T) {
	store := NewTradePlanReviewStore(t.TempDir())
	first := tradePlanFixture()
	second := tradePlanFixture()
	second.ID = strings.Repeat("c", 64)
	second.Symbol = "sz000001"
	second.Analysis.Symbol = second.Symbol
	second.Analysis.Fingerprint = strings.Repeat("d", 64)
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	for index, plan := range []domain.TradePlan{first, second} {
		if _, err := store.Update(plan, domain.TradePlanReviewRevision{ExecutionStatus: "watching"}, base.Add(time.Duration(index)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.All(10)
	if err != nil || len(items) != 2 || items[0].Symbol != second.Symbol || items[1].Symbol != first.Symbol {
		t.Fatalf("cross-symbol reviews were not listed: %v %+v", err, items)
	}
}

func TestTradePlanReviewStoreRejectsFillsForUntradedPlan(t *testing.T) {
	store := NewTradePlanReviewStore(t.TempDir())
	for _, status := range []string{"watching", "skipped", "not_traded"} {
		if _, err := store.Update(tradePlanFixture(), domain.TradePlanReviewRevision{
			ExecutionStatus: status, ActualEntry: reviewPrice(100), ActualExit: reviewPrice(102),
		}, time.Now()); err == nil {
			t.Errorf("accepted a fill with %s status", status)
		}
	}
}

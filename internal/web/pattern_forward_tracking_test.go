package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestForwardTrackingAPIAndRestartPreserveMatureResults(t *testing.T) {
	s, f, clock, dates, index := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	base := "/api/pattern-forwards?id=" + task.ID + "&tracking=1"
	clock.Set(clock.Now().Add(2 * time.Hour))
	s.runPatternForwardCycle(context.Background())
	first, err := s.patternForwards.Tracking(task.ID)
	if err != nil || len(first) != 1 || len(first[0].Records) == 0 {
		t.Fatalf("first tracking: %+v %v", first, err)
	}
	id := ""
	for _, record := range first[0].Records {
		if record.Sample.PatternID == "head-shoulders-bottom" {
			id = record.Sample.ID
		}
	}
	if id == "" {
		t.Fatal("no tracking sample")
	}
	for _, date := range dates[index+1:] {
		day, _ := time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
		clock.Set(day.Add(16 * time.Hour))
		s.runPatternForwardCycle(context.Background())
	}
	logs, err := s.patternForwards.Tracking(task.ID)
	if err != nil || len(logs) != len(dates)-index || logs[0].Hash != first[0].Hash {
		t.Fatalf("tracking chain: %d %v", len(logs), err)
	}
	response := validationHTTPRequest(s, http.MethodGet, base+"&horizon=20&pattern=head-shoulders-bottom", "")
	var view struct {
		Through string                          `json:"through"`
		Total   int                             `json:"total"`
		Totals  domain.PatternValidationTotals  `json:"totals"`
		Records []domain.PatternForwardProgress `json:"records"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Totals.Mature != 2 || view.Totals.BenchmarkPaired != 2 || len(view.Records) != 2 {
		t.Fatalf("result endpoint: %d %s", response.Code, response.Body.String())
	}
	for _, record := range view.Records {
		if !record.Finished || record.Sample.Outcomes[2].State != "mature" {
			t.Fatal("sample failed to mature")
		}
	}
	asOf := validationHTTPRequest(s, http.MethodGet, base+"&date="+dates[index], "")
	if asOf.Code != 200 || json.Unmarshal(asOf.Body.Bytes(), &view) != nil || view.Totals.Confirmed != 0 || view.Totals.Mature != 0 {
		t.Fatal("as-of query leaked later confirmation or results")
	}
	history := validationHTTPRequest(s, http.MethodGet, base+"&sample="+id, "")
	var trail struct {
		History []domain.PatternForwardProgress `json:"history"`
	}
	if history.Code != 200 || json.Unmarshal(history.Body.Bytes(), &trail) != nil || len(trail.History) < 21 {
		t.Fatalf("history: %s", history.Body.String())
	}
	for _, params := range []string{"&date=bad", "&horizon=7", "&offset=bad", "&limit=101"} {
		if r := validationHTTPRequest(s, http.MethodGet, base+params, ""); r.Code != 400 {
			t.Fatalf("invalid query accepted: %s", params)
		}
	}
	exported := validationHTTPRequest(s, http.MethodGet, base+"&download=1", "")
	var archive struct {
		Logs     []domain.PatternForwardTracking `json:"logs"`
		Evidence []domain.PatternForwardEvidence `json:"evidence"`
	}
	if exported.Code != 200 || json.Unmarshal(exported.Body.Bytes(), &archive) != nil || len(archive.Logs) != len(logs) || len(archive.Evidence[0].Stocks[0].Input.Bars) < 120 {
		t.Fatal("tracking export omitted immutable input evidence")
	}
	s2, f2, clock2, _, _ := forwardServerFixture(t)
	s2.patternForwards, s2.patternBatches = s.patternForwards, s.patternBatches
	clock2.Set(clock.Now())
	s2.runPatternForwardCycle(context.Background())
	reloaded, err := s2.patternForwards.Tracking(task.ID)
	if err != nil || !reflect.DeepEqual(logs, reloaded) || f2.count("sh600100") != 0 || f.count("sh600100") != len(dates)-index {
		t.Fatal("restart fetched or rewrote archived evidence")
	}
}

type failingForwardTrackingStore struct {
	patternForwardArchive
	fail bool
}

func (s *failingForwardTrackingStore) SaveTracking(v domain.PatternForwardTracking) (domain.PatternForwardTracking, error) {
	if s.fail {
		s.fail = false
		return v, fmt.Errorf("tracking write interrupted")
	}
	return s.patternForwardArchive.SaveTracking(v)
}

func TestForwardTrackingRetriesLedgerWithoutRefetchAndHonorsPause(t *testing.T) {
	s, f, clock, _, _ := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	store := &failingForwardTrackingStore{patternForwardArchive: s.patternForwards, fail: true}
	s.patternForwards = store
	clock.Set(clock.Now().Add(2 * time.Hour))
	s.runPatternForwardCycle(context.Background())
	status, _ := store.Status(task.ID)
	if status.TrackingError == "" {
		t.Fatal("tracking write failure was hidden")
	}
	if err := store.SetControl(task.ID, domain.PatternForwardControl{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	s.runPatternForwardCycle(context.Background())
	logs, _ := store.Tracking(task.ID)
	if len(logs) != 0 {
		t.Fatal("paused task published tracking record")
	}
	if err := store.SetControl(task.ID, domain.PatternForwardControl{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.runPatternForwardCycle(context.Background())
	logs, _ = store.Tracking(task.ID)
	status, _ = store.Status(task.ID)
	if len(logs) != 1 || status.TrackingError != "" || f.count("sh600100") != 1 {
		t.Fatal("ledger retry did not recover independently of immutable capture")
	}
}

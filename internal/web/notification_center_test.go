package web

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestNotificationActionsPreserveDeliveryAndPersistFollowMute(t *testing.T) {
	root := t.TempDir()
	records := storage.NewNativeNotificationStore(filepath.Join(root, "mac.json"))
	reviews := storage.NewNotificationReviewStore(filepath.Join(root, "reviews.json"))
	if err := records.Save(notify.State{Records: map[string]notify.Record{"one": {Message: notify.Message{ID: "one", Symbol: "sh600000", At: time.Now()}, Status: "submitted"}}}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, nil, nil, nil, "", WithNotificationCenter(records, reviews))
	s.now = func() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, realtimeWebLocation) }
	for _, input := range []map[string]string{{"action": "read", "id": "one"}, {"action": "handle", "id": "one"}, {"action": "note", "id": "one", "note": "等待板块确认"}, {"action": "follow", "symbol": "sh600000"}, {"action": "mute", "symbol": "sh600000"}} {
		data, _ := json.Marshal(input)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/native-notifications", bytes.NewReader(data)))
		if rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
	}
	state, err := reviews.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Items["one"].HandledAt.IsZero() || state.Items["one"].Note != "等待板块确认" || !state.Symbols["sh600000"].Followed || state.Symbols["sh600000"].MutedUntil.Format(time.DateOnly) != "2026-10-17" {
		t.Fatalf("actions lost: %+v", state)
	}
	deliveries, err := records.Load()
	if err != nil || deliveries.Records["one"].Status != "submitted" {
		t.Fatal("processing changed delivery evidence")
	}
	for _, body := range []string{`{"action":"read","id":"missing"}`, `{"action":"mute","symbol":"../../secret"}`, `{"action":"unknown"}`} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/native-notifications", bytes.NewBufferString(body)))
		if rec.Code == 200 {
			t.Fatal("invalid action accepted")
		}
	}
}

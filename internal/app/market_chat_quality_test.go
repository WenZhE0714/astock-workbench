package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func readyMarketChatFacts() domain.MarketChatFacts {
	now := time.Date(2026, 10, 21, 20, 0, 0, 0, shanghaiLocation)
	var bars []domain.MarketChatBar
	for date := now.AddDate(0, 0, -40); !date.After(now); date = date.AddDate(0, 0, 1) {
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			continue
		}
		volume, amount := 1000.0, 1e8
		bars = append(bars, domain.MarketChatBar{Date: date.Format(time.DateOnly), Close: 100, Volume: &volume, Amount: &amount})
	}
	facts := domain.MarketChatFacts{GeneratedAt: now, MarketStatus: "已收盘",
		Breadth: domain.MarketChatBreadth{Available: true, Industries: 80, Rising: 30, Falling: 40, Flat: 10},
		Amount:  domain.MarketChatAmount{Basis: "shanghai-shenzhen", Available: true, Comparable: true, QuoteDate: "2026-10-21", PreviousDate: "2026-10-20", Current: 120, Previous: 100, ChangePercent: 20}}
	for _, bar := range bars {
		facts.TradingDates = append(facts.TradingDates, bar.Date)
	}
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		facts.Indices = append(facts.Indices, domain.MarketChatIndex{
			Quote:      domain.StockQuoteSnapshot{Symbol: symbol, Price: 100, QuoteTime: "2026-10-21 16:15:00"},
			Technical:  domain.TechnicalSignal{Symbol: symbol, Status: domain.TechnicalStatusReady, DataDate: "2026-10-21", Price: 100, MA5: 101, MA20: 105, MA60: 110},
			RecentBars: append([]domain.MarketChatBar(nil), bars...)})
	}
	return facts
}

func TestMarketQualityGateRejectsIncompleteOrInconsistentFacts(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.MarketChatFacts)
	}{
		{"missing-index", func(f *domain.MarketChatFacts) { f.Indices = f.Indices[:2] }},
		{"duplicate-index", func(f *domain.MarketChatFacts) { f.Indices = append(f.Indices, f.Indices[0]) }},
		{"wrong-security", func(f *domain.MarketChatFacts) { f.Indices[0].Technical.Symbol = "sz000001" }},
		{"old-quote", func(f *domain.MarketChatFacts) { f.Indices[0].Quote.QuoteTime = "2026-10-20 16:15:00" }},
		{"future-quote", func(f *domain.MarketChatFacts) { f.Indices[0].Quote.QuoteTime = "2026-10-22 16:15:00" }},
		{"intraday-quote-after-close", func(f *domain.MarketChatFacts) { f.Indices[0].Quote.QuoteTime = "2026-10-21 14:30:00" }},
		{"quote-close-mismatch", func(f *domain.MarketChatFacts) { f.Indices[0].Quote.Price = 150 }},
		{"bar-close-mismatch", func(f *domain.MarketChatFacts) { f.Indices[0].RecentBars[len(f.Indices[0].RecentBars)-1].Close = 50 }},
		{"future-bar", func(f *domain.MarketChatFacts) {
			f.Indices[0].RecentBars[len(f.Indices[0].RecentBars)-1].Date = "2026-10-22"
		}},
		{"unordered-bars", func(f *domain.MarketChatFacts) { f.Indices[0].RecentBars[0].Date = "2026-10-21" }},
		{"different-turnover-day", func(f *domain.MarketChatFacts) { f.Amount.QuoteDate = "2026-10-20" }},
		{"different-turnover-basis", func(f *domain.MarketChatFacts) { f.Amount.Basis = "shanghai-shenzhen-beijing" }},
		{"non-comparable-turnover", func(f *domain.MarketChatFacts) { f.Amount.Comparable = false }},
		{"wrong-turnover-change", func(f *domain.MarketChatFacts) { f.Amount.ChangePercent = 90 }},
		{"wrong-previous-session", func(f *domain.MarketChatFacts) { f.Amount.PreviousDate = "2026-10-19" }},
		{"insufficient-industries", func(f *domain.MarketChatFacts) {
			f.Breadth.Industries = 2
			f.Breadth.Rising = 1
			f.Breadth.Falling = 1
			f.Breadth.Flat = 0
		}},
		{"insufficient-valid-fields", func(f *domain.MarketChatFacts) { f.Breadth.Missing = 30; f.Breadth.Falling = 10 }},
		{"invalid-breadth-totals", func(f *domain.MarketChatFacts) { f.Breadth.Rising = 200 }},
		{"stale-breadth", func(f *domain.MarketChatFacts) { f.Breadth.Available = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := readyMarketChatFacts()
			tc.change(&f)
			q := assessMarketChatQuality(f)
			if q.Status != "limited" || len(q.Reasons) == 0 {
				t.Fatalf("bad facts passed: %+v", q)
			}
		})
	}
	f := readyMarketChatFacts()
	q := assessMarketChatQuality(f)
	if q.Status != "ready" || len(q.ReadyIndices) != 3 {
		t.Fatalf("valid facts rejected: %+v", q)
	}
	for _, idx := range []int{0, 1, 2} {
		f.Indices[idx].Fund = nil
	}
	if assessMarketChatQuality(f).Status != "ready" {
		t.Fatal("optional minute/fund data blocked otherwise valid evidence")
	}
}

func TestMarketQualityCalendarAndIntradayBoundary(t *testing.T) {
	f := readyMarketChatFacts()
	f.GeneratedAt = time.Date(2026, 10, 9, 10, 0, 0, 0, shanghaiLocation)
	f.TradingDates = []string{"2026-09-29", "2026-09-30", "2026-10-09"}
	quote, daily, closed := marketChatExpectedDates(f)
	if quote != "2026-10-09" || daily != "2026-09-30" || closed {
		t.Fatalf("holiday gap mishandled: %s %s", quote, daily)
	}
	f.GeneratedAt = time.Date(2026, 10, 24, 10, 0, 0, 0, shanghaiLocation)
	f.TradingDates = nil
	quote, daily, closed = marketChatExpectedDates(f)
	if quote != "2026-10-23" || daily != "2026-10-23" || closed {
		t.Fatal("weekend treated as a session")
	}
	f = readyMarketChatFacts()
	f.GeneratedAt = time.Date(2026, 10, 21, 10, 0, 0, 0, shanghaiLocation)
	f.MarketStatus = "交易中"
	if assessMarketChatQuality(f).Status != "limited" {
		t.Fatal("incomplete daily data passed intraday")
	}
	if len(assessMarketChatQuality(f).UsableIndices) != 0 {
		t.Fatal("unfinished bars leaked into deterministic summary")
	}
}

type neverMarketModel struct{ calls int }

func (m *neverMarketModel) Synthesize(context.Context, string) (string, error) {
	m.calls++
	return "模型不应运行", nil
}

func TestMarketQualityGateStopsModelAndScoring(t *testing.T) {
	f := readyMarketChatFacts()
	f.Indices = f.Indices[:1]
	m := &neverMarketModel{}
	s := storage.NewAIChatStore(t.TempDir())
	a := &App{marketReportAI: m, aiChats: s}
	r, err := a.answerMarketChatSnapshot(context.Background(), nil, "大盘后续看涨还是看跌？", f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 0 || r.Fallback || r.Assessment == nil || r.Assessment.Evaluable || r.Assessment.Consistency.Status != "not_run" {
		t.Fatalf("gate bypassed: %+v calls=%d", r, m.calls)
	}
	if !strings.Contains(r.Answer, "不能概括全市场") || !strings.Contains(r.Answer, "未来5个交易日：证据不足") {
		t.Fatal(r.Answer)
	}
	record, err := s.LoadMarketRecord(r.Assessment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Facts.Quality.Status != "limited" || record.Assessment.Predictions[0].Direction != "unknown" {
		t.Fatal("limited result archived as forecast")
	}
	// The review path also enforces the gate if a caller mistakenly toggles eligibility.
	record.Assessment.Evaluable = true
	if marketAssessmentCanEvaluate(record.Assessment) {
		t.Fatal("review ignored recorded gate")
	}
}

type consistencyModelMock struct {
	generated int
	audited   int
	mode      string
	cancel    context.CancelFunc
}

func (m *consistencyModelMock) Synthesize(context.Context, string) (string, error) {
	return "已有证据", nil
}
func (m *consistencyModelMock) SynthesizeJSON(ctx context.Context, prompt string, _ []byte, out any) error {
	switch v := out.(type) {
	case *marketChatModelAnswer:
		m.generated++
		*v = marketChatModelAnswer{ShortDirection: "bullish", MediumDirection: "bearish", ShortReason: "短线修复", MediumReason: "中期承压", Answer: "未来5个交易日偏多，20个交易日偏空。"}
	case *marketChatAnswerAudit:
		m.audited++
		if m.mode == "timeout" {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 90*time.Second {
				return fmt.Errorf("missing audit timeout")
			}
			return context.DeadlineExceeded
		}
		if m.cancel != nil {
			m.cancel()
			return context.Canceled
		}
		if m.mode == "offline" {
			return fmt.Errorf("judge offline")
		}
		*v = marketChatAnswerAudit{Consistent: true, EvidenceSupported: true, ShortDirection: "bullish", MediumDirection: "bearish"}
		if m.mode == "reject" || m.mode == "repair" && m.audited == 1 {
			v.ShortDirection = "bearish"
			v.Consistent = false
			v.Issues = []string{"5日正文偏空，但归档偏多"}
		}
		if m.generated == 2 && !strings.Contains(prompt, "context") {
			return fmt.Errorf("lost facts on retry")
		}
	default:
		return fmt.Errorf("unexpected target %T", out)
	}
	return nil
}

func TestMarketConsistencyRetryAndFailClosed(t *testing.T) {
	for _, mode := range []string{"pass", "repair", "reject", "offline", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			m := &consistencyModelMock{mode: mode}
			s := storage.NewAIChatStore(t.TempDir())
			app := &App{marketReportAI: m, aiChats: s}
			r, err := app.answerMarketChatSnapshot(context.Background(), nil, "大盘未来怎么样", readyMarketChatFacts(), nil)
			if err != nil {
				t.Fatal(err)
			}
			passed := mode == "pass" || mode == "repair"
			if r.Fallback == passed || r.Assessment.Evaluable != passed {
				t.Fatalf("invalid acceptance: %+v", r.Assessment)
			}
			wantCalls := 1
			if mode == "repair" || mode == "reject" {
				wantCalls = 2
			}
			if m.generated != wantCalls || m.audited != wantCalls {
				t.Fatalf("unbounded retry: %d/%d", m.generated, m.audited)
			}
			record, err := s.LoadMarketRecord(r.Assessment.ID)
			if err != nil {
				t.Fatal(err)
			}
			if passed {
				if record.Assessment.Consistency.Status != "passed" || record.Assessment.Predictions[0].Direction != "bullish" {
					t.Fatal("accepted direction lost")
				}
			} else {
				if record.Assessment.Predictions[0].Direction != "unknown" || strings.Contains(record.Answer, "未来5个交易日偏多") {
					t.Fatal("rejected draft was published")
				}
			}
		})
	}
}

func TestMarketConsistencyCancellationDoesNotWriteRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &consistencyModelMock{cancel: cancel}
	app := &App{marketReportAI: m, aiChats: storage.NewAIChatStore(t.TempDir())}
	r, err := app.answerMarketChatSnapshot(ctx, nil, "大盘未来怎么样", readyMarketChatFacts(), nil)
	if err != context.Canceled || r.Assessment != nil {
		t.Fatalf("canceled audit saved: %+v %v", r, err)
	}
}

func TestMarketAuditRequiresBothIndependentDirections(t *testing.T) {
	model := marketChatModelAnswer{ShortDirection: "bullish", MediumDirection: "bearish"}
	prompt, err := marketChatConsistencyPrompt(marketChatContext{}, model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, `"short_direction":"bullish"`) || strings.Contains(prompt, `"medium_direction":"bearish"`) {
		t.Fatal("stored labels leaked to independent reader")
	}
	for _, raw := range []string{
		`{"consistent":true,"evidence_supported":true,"short_direction":"bearish","medium_direction":"bearish"}`,
		`{"consistent":true,"evidence_supported":true,"short_direction":"bullish","medium_direction":"missing"}`,
		`{"consistent":true,"evidence_supported":false,"short_direction":"bullish","medium_direction":"bearish"}`,
		`{"consistent":true,"evidence_supported":true,"short_direction":"bullish","medium_direction":"bearish","issues":["理由矛盾"]}`,
	} {
		var audit marketChatAnswerAudit
		if err := json.Unmarshal([]byte(raw), &audit); err != nil {
			t.Fatal(err)
		}
		if len(marketChatAuditIssues(model, audit)) == 0 {
			t.Fatal("invalid audit passed")
		}
	}
}

func TestMarketFixedQuestionsCannotBypassQualityGate(t *testing.T) {
	data, err := os.ReadFile("../testdata/assistant_questions.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Question string
		GateCase       string `json:"gate_case"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if tc.GateCase == "" {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			f := readyMarketChatFacts()
			switch tc.GateCase {
			case "missing-index":
				f.Indices = f.Indices[:1]
			case "stale-quote":
				f.Indices[0].Quote.QuoteTime = "2026-10-20 16:15:00"
			case "turnover-mismatch":
				f.Amount.Basis = "shanghai-only"
			case "missing-breadth":
				f.Breadth.Available = false
			default:
				t.Fatalf("unknown gate fixture %s", tc.GateCase)
			}
			m := &neverMarketModel{}
			app := &App{marketReportAI: m, aiChats: storage.NewAIChatStore(t.TempDir())}
			r, err := app.answerMarketChatSnapshot(context.Background(), nil, tc.Question, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			if m.calls != 0 || r.Assessment.Evaluable || r.Assessment.Predictions[0].Direction != "unknown" || r.Assessment.Predictions[1].Direction != "unknown" {
				t.Fatal("question wording bypassed gate")
			}
		})
	}
}

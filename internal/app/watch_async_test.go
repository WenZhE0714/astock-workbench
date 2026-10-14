package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type watchQuoteFunc func(context.Context, []string) ([]domain.Quote, error)

func (fetch watchQuoteFunc) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	return fetch(ctx, symbols)
}

type watchMinuteFunc func(context.Context, string) ([]domain.MinutePoint, error)

func (fetch watchMinuteFunc) FetchMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	return fetch(ctx, symbol)
}

type watchRankingFunc func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error)

func (fetch watchRankingFunc) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	return fetch(ctx, kind, limit)
}

func receiveWatchTest[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case result := <-channel:
		return result
	case <-time.After(time.Second):
		t.Fatal("watch event loop did not respond within one second")
		var zero T
		return zero
	}
}

func runAsyncWatchTest(t *testing.T, app *App) (chan<- terminalEvent, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan terminalEvent, 16)
	done := make(chan struct{})
	var loopError error
	go func() {
		defer close(done)
		loopError = app.watchLoopWithEventSource(ctx, []string{"sh600519", "sz000001"}, watchOptions{Interval: 1}, "默认", func(context.Context, bool) (<-chan terminalEvent, func() error, error) {
			return events, func() error { return nil }, nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		receiveWatchTest(t, done)
		if loopError != nil {
			t.Errorf("watch loop failed: %v", loopError)
		}
	})
	return events, done
}

func newAsyncWatchTestApp(t *testing.T, output io.Writer) *App {
	t.Helper()
	dir := t.TempDir()
	names, err := storage.LoadNameCache(filepath.Join(dir, "names.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	return &App{out: output, errOut: io.Discard, names: names, paths: storage.Paths{WatchlistFile: filepath.Join(dir, "watchlist.tsv")}}
}

func TestWatchLoopQuitsWhileQuotesAndMinutesAreBlocked(t *testing.T) {
	var output bytes.Buffer
	app := newAsyncWatchTestApp(t, &output)
	quoteStarted, minuteStarted := make(chan struct{}), make(chan struct{})
	quoteStopped, minuteStopped := make(chan struct{}), make(chan struct{})
	app.quotes = watchQuoteFunc(func(ctx context.Context, _ []string) ([]domain.Quote, error) {
		close(quoteStarted)
		<-ctx.Done()
		close(quoteStopped)
		return nil, ctx.Err()
	})
	app.minutes = watchMinuteFunc(func(ctx context.Context, _ string) ([]domain.MinutePoint, error) {
		close(minuteStarted)
		<-ctx.Done()
		close(minuteStopped)
		return nil, ctx.Err()
	})
	events, done := runAsyncWatchTest(t, app)
	receiveWatchTest(t, quoteStarted)
	receiveWatchTest(t, minuteStarted)
	started := time.Now()
	events <- terminalEvent{Key: terminalKeyDown}
	events <- terminalEvent{Key: terminalKeyQuit}
	receiveWatchTest(t, done)
	t.Logf("navigation and quit while both sources blocked: %s", time.Since(started))
	receiveWatchTest(t, quoteStopped)
	receiveWatchTest(t, minuteStopped)
	if strings.Count(output.String(), "ASTOCK") < 2 {
		t.Fatal("navigation did not repaint")
	}
}

type watchFrameBuffer struct {
	bytes.Buffer
	frames chan string
}

func (output *watchFrameBuffer) Write(data []byte) (int, error) {
	select {
	case output.frames <- string(data):
	default:
	}
	return output.Buffer.Write(data)
}

func waitWatchFrame(t *testing.T, frames <-chan string, text string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case frame := <-frames:
			if strings.Contains(frame, text) {
				return
			}
		case <-deadline:
			t.Fatalf("frame did not show %q", text)
		}
	}
}

func TestWatchLoopQueuesForcedRefreshAndThrottlesFailedMinutes(t *testing.T) {
	output := &watchFrameBuffer{frames: make(chan string, 32)}
	app := newAsyncWatchTestApp(t, output)
	if err := storage.SaveWatchlistGroups(app.paths.WatchlistFile, []storage.WatchlistGroup{{Name: "默认", Symbols: []string{"sh600519", "sz000001"}}}); err != nil {
		t.Fatal(err)
	}
	replies := make(chan chan struct{}, 8)
	app.quotes = watchQuoteFunc(func(ctx context.Context, _ []string) ([]domain.Quote, error) {
		reply := make(chan struct{})
		replies <- reply
		select {
		case <-reply:
			return []domain.Quote{{Symbol: "sh600519", Name: "测试股票"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	var minuteCalls atomic.Int32
	minuteStarted := make(chan struct{}, 8)
	app.minutes = watchMinuteFunc(func(context.Context, string) ([]domain.MinutePoint, error) {
		minuteCalls.Add(1)
		minuteStarted <- struct{}{}
		return nil, fmt.Errorf("temporarily unavailable")
	})
	events, done := runAsyncWatchTest(t, app)
	firstReply := receiveWatchTest(t, replies)
	receiveWatchTest(t, minuteStarted)
	// Reopening the same group forces fresh data even with a poll in flight.
	events <- terminalEvent{Text: "f"}
	events <- terminalEvent{Key: terminalKeyEnter}
	waitWatchFrame(t, output.frames, "当前分组: 默认")
	select {
	case <-replies:
		t.Fatal("forced refresh overlapped the active request")
	default:
	}
	close(firstReply)
	receiveWatchTest(t, replies)
	events <- terminalEvent{Key: terminalKeyQuit}
	receiveWatchTest(t, done)
	if minuteCalls.Load() != 1 {
		t.Fatalf("failed minute request was retried without throttling: %d", minuteCalls.Load())
	}
}

func TestWatchLoopCancelsQuotesWhenGroupChanges(t *testing.T) {
	output := &watchFrameBuffer{frames: make(chan string, 32)}
	app := newAsyncWatchTestApp(t, output)
	if err := storage.SaveWatchlistGroups(app.paths.WatchlistFile, []storage.WatchlistGroup{
		{Name: "默认", Symbols: []string{"sh600519", "sz000001"}},
		{Name: "新组", Symbols: []string{"sz300001"}},
	}); err != nil {
		t.Fatal(err)
	}
	type quoteCall struct {
		ctx     context.Context
		symbols []string
	}
	calls := make(chan quoteCall, 8)
	app.quotes = watchQuoteFunc(func(ctx context.Context, symbols []string) ([]domain.Quote, error) {
		calls <- quoteCall{ctx, symbols}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	events, done := runAsyncWatchTest(t, app)
	first := receiveWatchTest(t, calls)
	events <- terminalEvent{Text: "f"}
	events <- terminalEvent{Key: terminalKeyDown}
	events <- terminalEvent{Key: terminalKeyEnter}
	second := receiveWatchTest(t, calls)
	receiveWatchTest(t, first.ctx.Done())
	if !strings.Contains(strings.Join(second.symbols, ","), "sz300001") || strings.Contains(strings.Join(second.symbols, ","), "sh600519") {
		t.Fatalf("request still uses old group: %v", second.symbols)
	}
	waitWatchFrame(t, output.frames, "当前分组: 新组")
	events <- terminalEvent{Key: terminalKeyQuit}
	receiveWatchTest(t, done)
	receiveWatchTest(t, second.ctx.Done())
}

func TestWatchLoopSwitchesAndCancelsLoadingRankings(t *testing.T) {
	var output bytes.Buffer
	app := newAsyncWatchTestApp(t, &output)
	app.quotes = watchQuoteFunc(func(ctx context.Context, _ []string) ([]domain.Quote, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	type rankingCall struct {
		kind domain.MarketRankingKind
		ctx  context.Context
	}
	calls := make(chan rankingCall, 4)
	app.rankings = watchRankingFunc(func(ctx context.Context, kind domain.MarketRankingKind, _ int) ([]domain.MarketRankingItem, error) {
		calls <- rankingCall{kind, ctx}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	events, done := runAsyncWatchTest(t, app)
	events <- terminalEvent{Text: "3"}
	first := receiveWatchTest(t, calls)
	events <- terminalEvent{Text: "2"}
	second := receiveWatchTest(t, calls)
	if first.kind != domain.MarketRankingRapidRise || second.kind != domain.MarketRankingLosers {
		t.Fatalf("wrong ranking requests: %s %s", first.kind, second.kind)
	}
	receiveWatchTest(t, first.ctx.Done())
	events <- terminalEvent{Key: terminalKeyBack}
	receiveWatchTest(t, second.ctx.Done())
	events <- terminalEvent{Key: terminalKeyQuit}
	receiveWatchTest(t, done)
	for _, expected := range []string{"快速涨幅榜", "跌幅榜", "正在加载个股榜单"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing loading view %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "保留 00:00:00") {
		t.Fatal("loading view claimed to retain a nonexistent snapshot")
	}
}

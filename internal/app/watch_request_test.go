package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchRequestRejectsSupersededCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request := newWatchRequest[string]()
	firstStarted := make(chan context.Context, 1)
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	request.start(ctx, time.Second, func(ctx context.Context) (string, error) {
		firstStarted <- ctx
		select {
		case <-releaseFirst:
		case <-ctx.Done():
		}
		return "old", nil
	})
	firstCtx := <-firstStarted
	request.start(ctx, time.Second, func(ctx context.Context) (string, error) {
		select {
		case <-releaseSecond:
		case <-ctx.Done():
		}
		return "new", nil
	})
	if !errors.Is(firstCtx.Err(), context.Canceled) {
		t.Fatal("previous request was not canceled")
	}
	close(releaseFirst)
	old := <-request.results
	if request.finish(old) || !request.running {
		t.Fatal("old completion changed the active request")
	}
	close(releaseSecond)
	latest := <-request.results
	if !request.finish(latest) || latest.value != "new" || latest.err != nil || request.running {
		t.Fatalf("latest result not accepted: %+v", latest)
	}
}

func TestWatchRequestDeliversTimeoutAndRejectsCanceledResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := newWatchRequest[string]()
	request.start(ctx, 10*time.Millisecond, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	result := <-request.results
	if !request.finish(result) || !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("timeout was not delivered: %+v", result)
	}
	request.start(ctx, time.Second, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	request.cancel()
	if request.finish(<-request.results) || request.running {
		t.Fatal("canceled result reactivated the request")
	}
}

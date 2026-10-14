package app

import (
	"context"
	"time"
)

type watchResponse[T any] struct {
	id    uint64
	value T
	err   error
}

// Request state belongs to the event loop. Workers only publish immutable
// results; a canceled or superseded request can never update the active view.
type watchRequest[T any] struct {
	results chan watchResponse[T]
	id      uint64
	running bool
	stop    context.CancelFunc
}

func newWatchRequest[T any]() *watchRequest[T] {
	return &watchRequest[T]{results: make(chan watchResponse[T], 1)}
}

func (request *watchRequest[T]) cancel() {
	if request.stop != nil {
		request.stop()
		request.stop = nil
	}
	request.id++
	request.running = false
}

func (request *watchRequest[T]) start(ctx context.Context, timeout time.Duration, fetch func(context.Context) (T, error)) {
	request.cancel()
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	request.stop = cancel
	request.running = true
	id, results := request.id, request.results
	go func() {
		defer cancel()
		value, err := fetch(requestCtx)
		if err == nil {
			err = requestCtx.Err()
		}
		select {
		case results <- watchResponse[T]{id: id, value: value, err: err}:
		case <-ctx.Done():
		}
	}()
}

func (request *watchRequest[T]) finish(result watchResponse[T]) bool {
	if !request.running || result.id != request.id {
		return false
	}
	request.cancel()
	return true
}

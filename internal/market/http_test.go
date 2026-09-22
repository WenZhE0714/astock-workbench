package market

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type httpRetryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn httpRetryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFetchDecodedRetriesTransientEOFOnce(t *testing.T) {
	originalClient := httpClient
	t.Cleanup(func() { httpClient = originalClient })
	calls := 0
	httpClient = &http.Client{Transport: httpRetryRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.EOF
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":"ok"}`)),
		}, nil
	})}
	result, err := fetchDecoded(context.Background(), "https://example.test/transient", nil)
	if err != nil || result != `{"data":"ok"}` || calls != 2 {
		t.Fatalf("transient EOF was not retried exactly once: calls=%d result=%q err=%v", calls, result, err)
	}
}

func TestFetchDecodedDoesNotRetryPermanentHTTPStatus(t *testing.T) {
	originalClient := httpClient
	t.Cleanup(func() { httpClient = originalClient })
	calls := 0
	httpClient = &http.Client{Transport: httpRetryRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("missing")),
		}, nil
	})}
	if _, err := fetchDecoded(context.Background(), "https://example.test/missing", nil); err == nil || calls != 1 {
		t.Fatalf("permanent status should not be retried: calls=%d err=%v", calls, err)
	}
}

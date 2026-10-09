package market

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestFetchDecodedProxyFailureFallsBackWithinDeadline(t *testing.T) {
	for _, failure := range []string{"gateway", "timeout", "connection-refused", "not-found"} {
		t.Run(failure, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "gateway":
					w.WriteHeader(http.StatusBadGateway)
				case "not-found":
					w.WriteHeader(http.StatusNotFound)
				default:
					<-r.Context().Done()
				}
			}))
			defer proxy.Close()
			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "connection-refused" {
				proxy.Close()
			}
			original, originalDirect := httpClient, directHTTPClient
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			httpClient = &http.Client{Transport: transport}
			t.Cleanup(func() { httpClient, directHTTPClient = original, originalDirect })
			directCalls := 0
			directHTTPClient = &http.Client{Transport: httpRetryRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				directCalls++
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				if r.Header.Get("Referer") != "https://example.test/" {
					t.Error("direct fallback lost request headers")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("live")), Header: make(http.Header)}, nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			body, err := fetchDecodedWithHeaders(ctx, "http://example.test/quotes", nil, map[string]string{"Referer": "https://example.test/"})
			if failure == "not-found" {
				if err == nil || directCalls != 0 {
					t.Fatalf("permanent failure retried directly: calls=%d err=%v", directCalls, err)
				}
			} else if err != nil || body != "live" || directCalls != 1 || ctx.Err() != nil {
				t.Fatalf("proxy fallback: body=%q calls=%d err=%v", body, directCalls, err)
			}
		})
	}
}

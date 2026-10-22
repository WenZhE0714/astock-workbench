package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func thsAuthResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTHSExpiredAccessTokenRefreshesOnceAndRetries(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var refreshes, dataCalls, persisted atomic.Int32
			client := NewTHSQuantClient(THSQuantOptions{AccessToken: "expired-secret", RefreshToken: "refresh-secret", BaseURL: "https://ths.test/api/v1", MinRequestGap: time.Nanosecond,
				OnAccessToken: func(old, next string) error {
					if old != "expired-secret" || next != "fresh-secret" {
						t.Error("unexpected token transition")
					}
					persisted.Add(1)
					return nil
				},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/api/v1/get_access_token" {
						refreshes.Add(1)
						if r.Header.Get("refresh_token") != "refresh-secret" || r.Header.Get("access_token") != "" {
							t.Error("refresh header mismatch")
						}
						return thsAuthResponse(200, `{"errorcode":0,"data":{"access_token":"fresh-secret"}}`), nil
					}
					dataCalls.Add(1)
					if r.Header.Get("refresh_token") != "" {
						t.Error("refresh token sent to data endpoint")
					}
					if r.Header.Get("access_token") == "expired-secret" {
						return thsAuthResponse(status, `{"errorcode":-1302,"errmsg":"Access_Token is expired or ilegal."}`), nil
					}
					return thsDailyHistoryResponse(t, 65), nil
				})}})
			bars, err := client.FetchDailyBars(context.Background(), "sz300364")
			if err != nil || len(bars) != 65 || refreshes.Load() != 1 || dataCalls.Load() != 2 || persisted.Load() != 1 || client.AccessToken() != "fresh-secret" {
				t.Fatalf("refresh flow failed: bars=%d err=%v refreshes=%d calls=%d", len(bars), err, refreshes.Load(), dataCalls.Load())
			}
		})
	}
}

func TestTHSConcurrentExpirationSharesRefresh(t *testing.T) {
	var refreshes atomic.Int32
	var expired sync.WaitGroup
	expired.Add(8)
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "old", RefreshToken: "refresh", BaseURL: "https://ths.test", MinRequestGap: time.Nanosecond,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/get_access_token" {
				refreshes.Add(1)
				return thsAuthResponse(200, `{"data":{"access_token":"new"}}`), nil
			}
			if r.Header.Get("access_token") == "old" {
				expired.Done()
				expired.Wait()
				return thsAuthResponse(401, `{"errorcode":-1302}`), nil
			}
			return thsAuthResponse(200, `{"errorcode":0}`), nil
		})}})
	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if _, err := client.do(context.Background(), "test", map[string]any{}); err != nil {
				t.Error(err)
			}
		}()
	}
	callers.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh storm: %d", refreshes.Load())
	}
}

func TestTHSRefreshFailureIsBoundedAndRedacted(t *testing.T) {
	var refreshes atomic.Int32
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "access-secret", RefreshToken: "refresh-secret", BaseURL: "https://ths.test", MinRequestGap: time.Nanosecond,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/get_access_token" {
				refreshes.Add(1)
				return thsAuthResponse(401, `{"errorcode":-1301,"errmsg":"bad refresh-secret and access-secret"}`), nil
			}
			return thsAuthResponse(401, `{"errorcode":-1302,"errmsg":"bad access-secret"}`), nil
		})}})
	for range 3 {
		_, err := client.do(context.Background(), "test", nil)
		if err == nil || strings.Contains(err.Error(), "access-secret") || strings.Contains(err.Error(), "refresh-secret") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatal("failed refresh was repeatedly retried")
	}
}

func TestTHSRefreshOnlyConfigurationAndPersistenceFailure(t *testing.T) {
	client := NewTHSQuantClient(THSQuantOptions{RefreshToken: "refresh-secret", BaseURL: "https://ths.test",
		OnAccessToken: func(old, next string) error { return fmt.Errorf("cannot persist %s", next) },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/get_access_token" {
				t.Fatal("data called before credential persistence")
			}
			return thsAuthResponse(200, `{"data":{"access_token":"new-secret"}}`), nil
		})}})
	_, err := client.do(context.Background(), "test", nil)
	if err == nil || strings.Contains(err.Error(), "new-secret") || client.AccessToken() != "" {
		t.Fatalf("bad persistence behavior: %v", err)
	}
}

func TestTHSPermissionErrorDoesNotRefresh(t *testing.T) {
	var calls int
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "old", RefreshToken: "refresh", BaseURL: "https://ths.test", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return thsAuthResponse(401, `{"status_code":-4210,"status_msg":"permission denied"}`), nil
	})}})
	_, err := client.do(context.Background(), "test", nil)
	if err == nil || calls != 1 {
		t.Fatal("permission error triggered refresh")
	}
}

func TestTHSMonthlyQuotaFailureBacksOffWithoutRefreshing(t *testing.T) {
	calls := 0
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "old", RefreshToken: "refresh", BaseURL: "https://ths.test", MinRequestGap: time.Nanosecond,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Path == "/get_access_token" {
				t.Fatal("quota failure refreshed credentials")
			}
			return thsAuthResponse(200, `{"errorcode":-4318,"errmsg":"month exceeded"}`), nil
		})}})
	for range 3 {
		_, err := client.do(context.Background(), "history", nil)
		if err == nil || !strings.Contains(err.Error(), "本月数据额度已用尽") {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("quota endpoint repeatedly called: %d", calls)
	}
	_, _ = client.do(context.Background(), "quote", nil)
	if calls != 2 {
		t.Fatal("unrelated endpoint incorrectly disabled")
	}
}

func TestTHSRefreshWaitCanBeCanceledAndRedirectCannotLeakCredentials(t *testing.T) {
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "old", RefreshToken: "refresh"})
	client.refreshGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.renewAccessToken(ctx, "old"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-client.refreshGate
	first, _ := http.NewRequest("POST", "https://quantapi.51ifind.com/api/v1/get_access_token", nil)
	next, _ := http.NewRequest("GET", "https://other.test/", nil)
	if err := client.httpClient.CheckRedirect(next, []*http.Request{first}); err == nil {
		t.Fatal("cross-origin redirect allowed")
	}
}

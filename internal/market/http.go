package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

var directHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}()

func fetchDecoded(ctx context.Context, address string, decoder encoding.Encoding) (string, error) {
	return fetchDecodedWithHeaders(ctx, address, decoder, nil)
}

func fetchDecodedWithHeaders(ctx context.Context, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	proxied := false
	if configured, ok := transport.(*http.Transport); ok && configured.Proxy != nil {
		proxy, proxyErr := configured.Proxy(request)
		proxied = proxyErr == nil && proxy != nil
	}
	if !proxied {
		return fetchDecodedWithClient(ctx, httpClient, address, decoder, headers)
	}
	proxyCtx, cancel := fallbackContext(ctx, 2, 3*time.Second)
	result, proxyErr := fetchDecodedWithClient(proxyCtx, httpClient, address, decoder, headers)
	cancel()
	if proxyErr == nil || ctx.Err() != nil {
		return result, proxyErr
	}
	var statusError *httpStatusError
	var networkError *net.OpError
	if !transientHTTPReadError(proxyErr) && !errors.Is(proxyErr, context.DeadlineExceeded) &&
		!errors.As(proxyErr, &networkError) &&
		!(errors.As(proxyErr, &statusError) && statusError.code >= 500) {
		return "", proxyErr
	}
	result, directErr := fetchDecodedDirectWithHeaders(ctx, address, decoder, headers)
	if directErr == nil {
		return result, nil
	}
	return "", fmt.Errorf("代理请求: %v；直连回退: %w", proxyErr, directErr)
}

type httpStatusError struct {
	code   int
	status string
}

func (err *httpStatusError) Error() string { return "HTTP " + err.status }

func fetchDecodedDirectWithHeaders(ctx context.Context, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	return fetchDecodedWithClient(ctx, directHTTPClient, address, decoder, headers)
}

func fetchDecodedDirectFirst(ctx context.Context, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	directCtx, cancel := fallbackContext(ctx, 2, 3*time.Second)
	result, directErr := fetchDecodedDirectWithHeaders(directCtx, address, decoder, headers)
	cancel()
	if directErr == nil || ctx.Err() != nil {
		return result, directErr
	}
	// Call the configured route once; do not re-enter its direct fallback.
	result, proxyErr := fetchDecodedWithClient(ctx, httpClient, address, decoder, headers)
	if proxyErr == nil {
		return result, nil
	}
	return "", fmt.Errorf("直连请求: %v；代理回退: %w", directErr, proxyErr)
}

func fetchDecodedWithClient(ctx context.Context, client *http.Client, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	var lastError error
	for attempt := 0; attempt < 2; attempt++ {
		result, err := fetchDecodedOnce(ctx, client, address, decoder, headers)
		if err == nil {
			return result, nil
		}
		lastError = err
		if attempt > 0 || !transientHTTPReadError(err) {
			break
		}
		timer := time.NewTimer(80 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", lastError
}

func fetchDecodedOnce(ctx context.Context, client *http.Client, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 astock-workbench/0.1")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", &httpStatusError{code: response.StatusCode, status: response.Status}
	}
	var reader io.Reader = response.Body
	if decoder != nil {
		reader = transform.NewReader(response.Body, decoder.NewDecoder())
	}
	data, err := io.ReadAll(io.LimitReader(reader, 8<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func transientHTTPReadError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

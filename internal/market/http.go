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
	return fetchDecodedWithClient(ctx, httpClient, address, decoder, headers)
}

func fetchDecodedDirectWithHeaders(ctx context.Context, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	return fetchDecodedWithClient(ctx, directHTTPClient, address, decoder, headers)
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
		return "", fmt.Errorf("HTTP %s", response.Status)
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

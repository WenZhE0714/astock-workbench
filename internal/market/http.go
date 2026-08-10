package market

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func fetchDecoded(ctx context.Context, address string, decoder encoding.Encoding) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 astock-workbench/0.1")
	response, err := httpClient.Do(request)
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

package analysis

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestConfiguredRunnerCallsOpenAICompatibleProvider(t *testing.T) {
	var gotAuth, gotModel string
	previousFactory := providerHTTPClientFactory
	defer func() { providerHTTPClientFactory = previousFactory }()
	providerHTTPClientFactory = func(domain.AIConfig) *http.Client {
		return &http.Client{Transport: providerRoundTripper(func(request *http.Request) (*http.Response, error) {
			gotAuth = request.Header.Get("Authorization")
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			gotModel = body.Model
			return providerResponse(http.StatusOK, `{"choices":[{"message":{"content":"## OK"}}]}`), nil
		})}
	}
	config := domain.DefaultAIConfig()
	config.ExecutionMode = domain.AIExecutionAPI
	config.Provider = "openai"
	config.Model = "test-model"
	config.BaseURL = "http://provider.test/v1"
	runner := NewConfiguredCodexRunner("", func() (domain.AIConfig, string, error) {
		return config, "sk-provider-test", nil
	})
	answer, err := runner.Synthesize(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(answer) != "## OK" || gotAuth != "Bearer sk-provider-test" || gotModel != "test-model" {
		t.Fatalf("unexpected provider call: answer=%q auth=%q model=%q", answer, gotAuth, gotModel)
	}
}

func TestConfiguredRunnerParsesProviderStructuredJSON(t *testing.T) {
	previousFactory := providerHTTPClientFactory
	defer func() { providerHTTPClientFactory = previousFactory }()
	providerHTTPClientFactory = func(domain.AIConfig) *http.Client {
		return &http.Client{Transport: providerRoundTripper(func(*http.Request) (*http.Response, error) {
			return providerResponse(http.StatusOK, "{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"value\\\":7}\\n```\"}}]}"), nil
		})}
	}
	config := domain.DefaultAIConfig()
	config.ExecutionMode = domain.AIExecutionAPI
	config.Provider = "ollama"
	config.Model = "local"
	config.BaseURL = "http://provider.test"
	runner := NewConfiguredCodexRunner("", func() (domain.AIConfig, string, error) {
		return config, "", nil
	})
	var target struct {
		Value int `json:"value"`
	}
	if err := runner.SynthesizeJSON(context.Background(), "return value", []byte("{\"type\":\"object\",\"properties\":{\"value\":{\"type\":\"integer\"}},\"required\":[\"value\"],\"additionalProperties\":false}"), &target); err != nil {
		t.Fatal(err)
	}
	if target.Value != 7 {
		t.Fatalf("target = %+v", target)
	}
}

func TestProviderHTTPErrorRedactsToken(t *testing.T) {
	previousFactory := providerHTTPClientFactory
	defer func() { providerHTTPClientFactory = previousFactory }()
	providerHTTPClientFactory = func(domain.AIConfig) *http.Client {
		return &http.Client{Transport: providerRoundTripper(func(*http.Request) (*http.Response, error) {
			return providerResponse(http.StatusUnauthorized, `{"error":"bad sk-secret-token"}`), nil
		})}
	}
	config := domain.DefaultAIConfig()
	config.ExecutionMode = domain.AIExecutionAPI
	config.BaseURL = "http://provider.test"
	runner := NewConfiguredCodexRunner("", func() (domain.AIConfig, string, error) {
		return config, "sk-secret-token", nil
	})
	_, err := runner.Synthesize(context.Background(), "hello")
	if err == nil || strings.Contains(err.Error(), "sk-secret-token") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type providerRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper providerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func providerResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

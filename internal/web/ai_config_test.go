package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type aiConfigServiceStub struct {
	snapshot domain.AIConfigSnapshot
	updated  domain.AIConfigUpdate
}

func (stub *aiConfigServiceStub) Load(context.Context) (domain.AIConfigSnapshot, error) {
	return stub.snapshot, nil
}

func (stub *aiConfigServiceStub) Save(_ context.Context, update domain.AIConfigUpdate) (domain.AIConfigSnapshot, error) {
	stub.updated = update
	return stub.snapshot, nil
}

func (stub *aiConfigServiceStub) Test(context.Context, domain.AIConfigUpdate) (domain.AIConfigTestResult, error) {
	return domain.AIConfigTestResult{OK: true, Message: "ok"}, nil
}

func (stub *aiConfigServiceStub) Reset(context.Context) (domain.AIConfigSnapshot, error) {
	return stub.snapshot, nil
}

func TestAIConfigEndpointRedactsTokenAndSupportsSave(t *testing.T) {
	config := domain.DefaultAIConfig()
	config.Provider = "deepseek"
	stub := &aiConfigServiceStub{snapshot: domain.AIConfigSnapshot{
		Config: config, TokenConfigured: true, TokenHint: "sk-••••1234", TokenSource: "本地秘密文件",
		Providers: domain.AIProviderDefinitions(),
	}}
	server := NewServer(nil, nil, nil, nil, "600519", WithAIConfigService(stub))
	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/ai/config", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "sk-provider-secret") || !strings.Contains(get.Body.String(), "token_configured") {
		t.Fatalf("unexpected redaction response: %s", get.Body.String())
	}
	request := httptest.NewRequest(http.MethodPut, "/api/ai/config", strings.NewReader(`{"config":{"execution_mode":"api","provider":"deepseek","model":"deepseek-chat","timeout_seconds":60},"token":"sk-provider-secret"}`))
	save := httptest.NewRecorder()
	server.Handler().ServeHTTP(save, request)
	if save.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", save.Code, save.Body.String())
	}
	if stub.updated.Token == nil || *stub.updated.Token != "sk-provider-secret" || stub.updated.Config.Provider != "deepseek" {
		t.Fatalf("unexpected update: %+v", stub.updated)
	}
	var response map[string]any
	if err := json.Unmarshal(save.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if _, exists := response["token"]; exists {
		t.Fatal("save response returned raw token")
	}
}

func TestAIConfigEndpointAcceptsFlatPayloadAndConnectionTest(t *testing.T) {
	stub := &aiConfigServiceStub{snapshot: domain.AIConfigSnapshot{Config: domain.DefaultAIConfig()}}
	server := NewServer(nil, nil, nil, nil, "600519", WithAIConfigService(stub))
	request := httptest.NewRequest(http.MethodPost, "/api/ai/config", strings.NewReader(`{"execution_mode":"api","provider":"ollama","model":"qwen3:latest","timeout_seconds":60}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || stub.updated.Config.Provider != "ollama" || stub.updated.Config.ExecutionMode != "api" {
		t.Fatalf("flat payload failed: status=%d update=%+v body=%s", recorder.Code, stub.updated, recorder.Body.String())
	}
	testRequest := httptest.NewRequest(http.MethodPost, "/api/ai/config/test", strings.NewReader(`{"config":{"execution_mode":"api","provider":"ollama","model":"qwen3:latest","timeout_seconds":60}}`))
	testRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(testRecorder, testRequest)
	if testRecorder.Code != http.StatusOK || !strings.Contains(testRecorder.Body.String(), `"ok":true`) {
		t.Fatalf("test endpoint failed: status=%d body=%s", testRecorder.Code, testRecorder.Body.String())
	}
}

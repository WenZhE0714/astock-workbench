package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// AIConfigService owns persistence and provider probing. The Web layer only
// handles the redacted HTTP contract and never stores a token in browser state.
type AIConfigService interface {
	Load(context.Context) (domain.AIConfigSnapshot, error)
	Save(context.Context, domain.AIConfigUpdate) (domain.AIConfigSnapshot, error)
	Test(context.Context, domain.AIConfigUpdate) (domain.AIConfigTestResult, error)
	Reset(context.Context) (domain.AIConfigSnapshot, error)
}

type aiConfigRequest struct {
	Config     *domain.AIConfig `json:"config,omitempty"`
	Token      *string          `json:"token,omitempty"`
	ClearToken bool             `json:"clear_token,omitempty"`

	// Flat aliases keep the endpoint convenient for curl and older browser
	// bundles that sent settings without a nested config object.
	Enabled        *bool   `json:"enabled,omitempty"`
	ExecutionMode  *string `json:"execution_mode,omitempty"`
	Provider       *string `json:"provider,omitempty"`
	Model          *string `json:"model,omitempty"`
	QuickModel     *string `json:"quick_model,omitempty"`
	DeepModel      *string `json:"deep_model,omitempty"`
	BaseURL        *string `json:"base_url,omitempty"`
	CodexBin       *string `json:"codex_bin,omitempty"`
	CodexHome      *string `json:"codex_home,omitempty"`
	CodexProfile   *string `json:"codex_profile,omitempty"`
	TimeoutSeconds *int    `json:"timeout_seconds,omitempty"`
	Reasoning      *string `json:"reasoning_effort,omitempty"`
}

func (request aiConfigRequest) update() domain.AIConfigUpdate {
	return request.updateFrom(domain.DefaultAIConfig())
}

func (request aiConfigRequest) updateFrom(base domain.AIConfig) domain.AIConfigUpdate {
	config := base
	if request.Config != nil {
		config = *request.Config
	}
	if request.Enabled != nil {
		config.Enabled = *request.Enabled
	}
	if request.ExecutionMode != nil {
		config.ExecutionMode = *request.ExecutionMode
	}
	if request.Provider != nil {
		config.Provider = *request.Provider
	}
	if request.Model != nil {
		config.Model = *request.Model
	}
	if request.QuickModel != nil {
		config.QuickModel = *request.QuickModel
	}
	if request.DeepModel != nil {
		config.DeepModel = *request.DeepModel
	}
	if request.BaseURL != nil {
		config.BaseURL = *request.BaseURL
	}
	if request.CodexBin != nil {
		config.CodexBin = *request.CodexBin
	}
	if request.CodexHome != nil {
		config.CodexHome = *request.CodexHome
	}
	if request.CodexProfile != nil {
		config.CodexProfile = *request.CodexProfile
	}
	if request.TimeoutSeconds != nil {
		config.TimeoutSeconds = *request.TimeoutSeconds
	}
	if request.Reasoning != nil {
		config.Reasoning = *request.Reasoning
	}
	return domain.AIConfigUpdate{Config: config, Token: request.Token, ClearToken: request.ClearToken}
}

type aiConfigResponse struct {
	Config          domain.AIConfig               `json:"config"`
	TokenConfigured bool                          `json:"token_configured"`
	TokenHint       string                        `json:"token_hint,omitempty"`
	TokenSource     string                        `json:"token_source,omitempty"`
	Providers       []domain.AIProviderDefinition `json:"providers"`
	Profiles        []domain.AIProfile            `json:"profiles,omitempty"`

	// Top-level aliases make the response easy to inspect from a terminal and
	// preserve compatibility with the reference project's flat config shape.
	Enabled        bool   `json:"enabled"`
	ExecutionMode  string `json:"execution_mode"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	QuickModel     string `json:"quick_model"`
	DeepModel      string `json:"deep_model"`
	BaseURL        string `json:"base_url,omitempty"`
	CodexBin       string `json:"codex_bin,omitempty"`
	CodexHome      string `json:"codex_home,omitempty"`
	CodexProfile   string `json:"codex_profile,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Reasoning      string `json:"reasoning_effort,omitempty"`
}

func redactAIConfig(snapshot domain.AIConfigSnapshot) aiConfigResponse {
	config := snapshot.Config.Normalized()
	return aiConfigResponse{
		Config: config, TokenConfigured: snapshot.TokenConfigured, TokenHint: snapshot.TokenHint,
		TokenSource: snapshot.TokenSource, Providers: snapshot.Providers, Profiles: snapshot.Profiles,
		Enabled: config.Enabled, ExecutionMode: config.ExecutionMode, Provider: config.Provider,
		Model: config.Model, QuickModel: config.QuickModel, DeepModel: config.DeepModel,
		BaseURL: config.BaseURL, CodexBin: config.CodexBin, CodexHome: config.CodexHome,
		CodexProfile: config.CodexProfile, TimeoutSeconds: config.TimeoutSeconds, Reasoning: config.Reasoning,
	}
}

func (s *Server) handleAIConfig(writer http.ResponseWriter, request *http.Request) {
	if s.aiConfigService == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "AI 配置服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		snapshot, err := s.aiConfigService.Load(request.Context())
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取 AI 配置失败: " + err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, redactAIConfig(snapshot))
	case http.MethodPost, http.MethodPut:
		input, err := decodeAIConfigRequest(writer, request)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		update, updateErr := s.aiConfigUpdate(request.Context(), input)
		if updateErr != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取当前 AI 配置失败: " + updateErr.Error()})
			return
		}
		snapshot, err := s.aiConfigService.Save(request.Context(), update)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "保存 AI 配置失败: " + err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, redactAIConfig(snapshot))
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI 配置只支持 GET、PUT、POST"})
	}
}

func (s *Server) handleAIConfigTest(writer http.ResponseWriter, request *http.Request) {
	if s.aiConfigService == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "AI 配置服务未初始化"})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI 连接测试只支持 POST"})
		return
	}
	input, err := decodeAIConfigRequest(writer, request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	ctx := request.Context()
	update, updateErr := s.aiConfigUpdate(ctx, input)
	if updateErr != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取当前 AI 配置失败: " + updateErr.Error()})
		return
	}
	result, testErr := s.aiConfigService.Test(ctx, update)
	if testErr != nil {
		result.OK = false
		if strings.TrimSpace(result.Message) == "" {
			result.Message = testErr.Error()
		}
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) aiConfigUpdate(ctx context.Context, input aiConfigRequest) (domain.AIConfigUpdate, error) {
	if input.Config != nil {
		return input.update(), nil
	}
	base := domain.DefaultAIConfig()
	if s != nil && s.aiConfigService != nil {
		snapshot, err := s.aiConfigService.Load(ctx)
		if err != nil {
			return domain.AIConfigUpdate{}, err
		}
		base = snapshot.Config
	}
	return input.updateFrom(base), nil
}

func (s *Server) handleAIConfigReset(writer http.ResponseWriter, request *http.Request) {
	if s.aiConfigService == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "AI 配置服务未初始化"})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI 配置恢复默认只支持 POST"})
		return
	}
	snapshot, err := s.aiConfigService.Reset(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "恢复 AI 默认配置失败: " + err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, redactAIConfig(snapshot))
}

func decodeAIConfigRequest(writer http.ResponseWriter, request *http.Request) (aiConfigRequest, error) {
	if request.Body == nil {
		return aiConfigRequest{}, fmt.Errorf("AI 配置请求不能为空")
	}
	var input aiConfigRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 64<<10))
	if err := decoder.Decode(&input); err != nil {
		return aiConfigRequest{}, fmt.Errorf("AI 配置请求格式无效")
	}
	return input, nil
}

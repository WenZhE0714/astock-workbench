package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type thsAPIError struct {
	httpStatus, code int
	message          string
}

func (err *thsAPIError) Error() string {
	if err.code == -4318 {
		return "同花顺本月数据额度已用尽（错误 -4318），续期令牌不能恢复额度"
	}
	if err.httpStatus != 0 {
		return fmt.Sprintf("同花顺 Quant API HTTP %d，错误 %d: %s", err.httpStatus, err.code, err.message)
	}
	return fmt.Sprintf("同花顺 Quant API 错误 %d: %s", err.code, err.message)
}

func (client *THSQuantClient) quotaError(path string) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if time.Now().Before(client.quotaRetryAt[path]) {
		return client.quotaErrors[path]
	}
	return nil
}

func (client *THSQuantClient) rememberQuotaError(path string, err *thsAPIError) {
	if err.code != -4318 {
		return
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.quotaRetryAt == nil {
		client.quotaRetryAt = map[string]time.Time{}
		client.quotaErrors = map[string]error{}
	}
	client.quotaRetryAt[path] = time.Now().Add(time.Minute)
	client.quotaErrors[path] = err
}
func thsTokenExpired(err error) bool {
	var apiErr *thsAPIError
	return errors.As(err, &apiErr) && (apiErr.code == -1302 || apiErr.httpStatus == http.StatusUnauthorized && apiErr.code == 0)
}
func redactTHSTokens(text string, tokens ...string) string {
	for _, token := range tokens {
		if token != "" {
			text = strings.ReplaceAll(text, token, "[redacted]")
		}
	}
	return text
}

// get_access_token reuses a valid token and renews an expired one. It does not
// forcibly invalidate other sessions like update_access_token does.
func (client *THSQuantClient) renewAccessToken(ctx context.Context, failedToken string) (err error) {
	select {
	case client.refreshGate <- struct{}{}:
		defer func() { <-client.refreshGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if token := client.AccessToken(); token != "" && token != failedToken {
		return nil
	}
	if client.refreshToken == "" {
		return errors.New("同花顺未配置刷新令牌，请在官方账号详情中获取")
	}
	if time.Now().Before(client.refreshRetryAt) && client.refreshError != nil {
		return client.refreshError
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			client.refreshError = err
			client.refreshRetryAt = time.Now().Add(time.Minute)
		}
	}()
	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, err := client.request(refreshCtx, "get_access_token", nil, "refresh_token", client.refreshToken)
	if err != nil {
		return fmt.Errorf("同花顺访问令牌续期失败，请核对刷新令牌及账号有效期: %w", err)
	}
	var response struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return errors.New("同花顺令牌响应格式无效")
	}
	next := strings.TrimSpace(response.Data.AccessToken)
	if next == "" || next == failedToken {
		return errors.New("同花顺未返回新的有效访问令牌，请在官方账号详情核对刷新令牌及账号权限")
	}
	if client.onAccessToken != nil {
		if err := client.onAccessToken(failedToken, next); err != nil {
			return fmt.Errorf("同花顺新令牌保存失败: %s", redactTHSTokens(err.Error(), failedToken, next, client.refreshToken))
		}
	}
	client.tokenMu.Lock()
	client.accessToken = next
	client.tokenMu.Unlock()
	client.refreshError = nil
	client.refreshRetryAt = time.Time{}
	return nil
}

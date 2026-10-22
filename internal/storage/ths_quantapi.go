package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// THSQuantAPIConfig is the local configuration for the optional Tonghuashun
// Quant API adapter. The file is ignored and should be chmod 0600 because it
// may contain the two credentials.
type THSQuantAPIConfig struct {
	Enabled           bool   `json:"enabled"`
	AccessToken       string `json:"access_token,omitempty"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	BaseURL           string `json:"base_url,omitempty"`
	MinRequestGapMS   int    `json:"min_request_gap_ms,omitempty"`
	QuoteIndicators   string `json:"quote_indicators,omitempty"`
	HistoryIndicators string `json:"history_indicators,omitempty"`
	MinuteIndicators  string `json:"minute_indicators,omitempty"`
}

// SaveTHSAccessToken preserves the existing credential location and all other
// JSON fields. A changed credential is never replaced with an older refresh result.
func SaveTHSAccessToken(configFile, accessTokenFile, previous, next string) error {
	if strings.TrimSpace(next) == "" {
		return errors.New("访问令牌为空")
	}
	lockPath := configFile
	if lockPath == "" {
		lockPath = accessTokenFile
	}
	if lockPath == "" {
		return errors.New("同花顺凭证存储位置未配置")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(lockPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("同花顺凭证正在更新，请稍后重试: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	document := map[string]json.RawMessage{}
	var data []byte
	if configFile != "" {
		data, err = os.ReadFile(configFile)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
	}
	var inline string
	if raw, ok := document["access_token"]; ok {
		if err := json.Unmarshal(raw, &inline); err != nil {
			return err
		}
	}
	if strings.TrimSpace(inline) != "" {
		if inline == next {
			return nil
		}
		if strings.TrimSpace(inline) != previous {
			return fmt.Errorf("本地访问令牌已被其他进程修改，请重新加载配置")
		}
		document["access_token"], _ = json.Marshal(next)
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return err
		}
		return atomicWrite(configFile, append(data, '\n'), 0o600)
	}
	if accessTokenFile == "" {
		return errors.New("访问令牌文件位置未配置")
	}
	old, err := os.ReadFile(accessTokenFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if strings.TrimSpace(string(old)) == next {
		return nil
	}
	if strings.TrimSpace(string(old)) != previous {
		return errors.New("本地访问令牌文件已被其他进程修改，请重新加载配置")
	}
	return atomicWrite(accessTokenFile, []byte(next+"\n"), 0o600)
}

func LoadTHSQuantAPIConfig(configFile, accessTokenFile, refreshTokenFile string) (THSQuantAPIConfig, string, string, error) {
	config := THSQuantAPIConfig{MinRequestGapMS: 100}
	if strings.TrimSpace(configFile) != "" {
		data, err := os.ReadFile(configFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return THSQuantAPIConfig{}, "", "", err
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &config); err != nil {
				return THSQuantAPIConfig{}, "", "", err
			}
		}
	}
	if config.MinRequestGapMS < 0 {
		config.MinRequestGapMS = 100
	}
	readSecret := func(path string) (string, error) {
		if strings.TrimSpace(path) == "" {
			return "", nil
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	access, err := readSecret(accessTokenFile)
	if err != nil {
		return THSQuantAPIConfig{}, "", "", err
	}
	refresh, err := readSecret(refreshTokenFile)
	if err != nil {
		return THSQuantAPIConfig{}, "", "", err
	}
	if strings.TrimSpace(config.AccessToken) != "" {
		access = strings.TrimSpace(config.AccessToken)
	}
	if strings.TrimSpace(config.RefreshToken) != "" {
		refresh = strings.TrimSpace(config.RefreshToken)
	}
	return config, access, refresh, nil
}

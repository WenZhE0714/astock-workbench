package storage

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
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

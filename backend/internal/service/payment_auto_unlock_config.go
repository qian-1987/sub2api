package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	paymentAutoUnlockEnabledEnv    = "CUSTOM_PAYMENT_AUTO_UNLOCK_ENABLED"
	paymentAutoUnlockThresholdEnv  = "CUSTOM_PAYMENT_AUTO_UNLOCK_THRESHOLD"
	paymentAutoUnlockGroupIDEnv    = "CUSTOM_PAYMENT_AUTO_UNLOCK_GROUP_ID"
	paymentAutoUnlockConfigFileEnv = "CUSTOM_PAYMENT_AUTO_UNLOCK_CONFIG_FILE"

	paymentAutoUnlockConfigFilename   = "payment_auto_unlock.json"
	paymentAutoUnlockDefaultConfigDir = "/app/data"
)

type paymentAutoUnlockConfig struct {
	Enabled   bool
	Threshold float64
	GroupID   int64
}

type paymentAutoUnlockConfigSource struct {
	Enabled   *bool   `json:"custom_payment_auto_unlock_enabled"`
	Threshold *float64 `json:"custom_payment_auto_unlock_threshold"`
	GroupID   *int64  `json:"custom_payment_auto_unlock_group_id"`
}

func loadPaymentAutoUnlockConfig() (paymentAutoUnlockConfig, error) {
	cfg := paymentAutoUnlockConfig{}

	fileSource, err := loadPaymentAutoUnlockConfigFile(resolvePaymentAutoUnlockConfigFile())
	if err != nil {
		return cfg, err
	}
	applyPaymentAutoUnlockConfigSource(&cfg, fileSource)

	envSource, err := loadPaymentAutoUnlockConfigFromEnv()
	if err != nil {
		return cfg, err
	}
	applyPaymentAutoUnlockConfigSource(&cfg, envSource)

	if err := validatePaymentAutoUnlockConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func resolvePaymentAutoUnlockConfigFile() string {
	if path := strings.TrimSpace(os.Getenv(paymentAutoUnlockConfigFileEnv)); path != "" {
		return path
	}
	if dataDir := strings.TrimSpace(os.Getenv("DATA_DIR")); dataDir != "" {
		return filepath.Join(dataDir, paymentAutoUnlockConfigFilename)
	}
	return filepath.Join(paymentAutoUnlockDefaultConfigDir, paymentAutoUnlockConfigFilename)
}

func loadPaymentAutoUnlockConfigFile(path string) (paymentAutoUnlockConfigSource, error) {
	var source paymentAutoUnlockConfigSource
	if strings.TrimSpace(path) == "" {
		return source, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return source, nil
		}
		return source, fmt.Errorf("read payment auto unlock config file %q: %w", path, err)
	}
	if len(data) == 0 {
		return source, nil
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return source, fmt.Errorf("parse payment auto unlock config file %q: %w", path, err)
	}
	return source, nil
}

func loadPaymentAutoUnlockConfigFromEnv() (paymentAutoUnlockConfigSource, error) {
	var source paymentAutoUnlockConfigSource

	if raw, ok := os.LookupEnv(paymentAutoUnlockEnabledEnv); ok {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return source, fmt.Errorf("parse %s: %w", paymentAutoUnlockEnabledEnv, err)
		}
		source.Enabled = &value
	}
	if raw, ok := os.LookupEnv(paymentAutoUnlockThresholdEnv); ok {
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return source, fmt.Errorf("parse %s: %w", paymentAutoUnlockThresholdEnv, err)
		}
		source.Threshold = &value
	}
	if raw, ok := os.LookupEnv(paymentAutoUnlockGroupIDEnv); ok {
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return source, fmt.Errorf("parse %s: %w", paymentAutoUnlockGroupIDEnv, err)
		}
		source.GroupID = &value
	}

	return source, nil
}

func applyPaymentAutoUnlockConfigSource(cfg *paymentAutoUnlockConfig, source paymentAutoUnlockConfigSource) {
	if cfg == nil {
		return
	}
	if source.Enabled != nil {
		cfg.Enabled = *source.Enabled
	}
	if source.Threshold != nil {
		cfg.Threshold = *source.Threshold
	}
	if source.GroupID != nil {
		cfg.GroupID = *source.GroupID
	}
}

func validatePaymentAutoUnlockConfig(cfg paymentAutoUnlockConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Threshold <= 0 {
		return fmt.Errorf("payment auto unlock threshold must be greater than 0")
	}
	if cfg.GroupID <= 0 {
		return fmt.Errorf("payment auto unlock group_id must be greater than 0")
	}
	return nil
}

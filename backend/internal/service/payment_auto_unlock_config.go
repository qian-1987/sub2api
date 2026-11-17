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
	Rules     []paymentAutoUnlockRule
}

type paymentAutoUnlockRule struct {
	Key       string  `json:"key"`
	Threshold float64 `json:"threshold"`
	GroupID   int64   `json:"group_id"`
	GroupName string  `json:"group_name"`
}

type paymentAutoUnlockConfigSource struct {
	Enabled   *bool                    `json:"custom_payment_auto_unlock_enabled"`
	Threshold *float64                 `json:"custom_payment_auto_unlock_threshold"`
	GroupID   *int64                   `json:"custom_payment_auto_unlock_group_id"`
	Rules     *[]paymentAutoUnlockRule `json:"custom_payment_auto_unlock_rules"`
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
	if source.Rules != nil {
		cfg.Rules = make([]paymentAutoUnlockRule, 0, len(*source.Rules))
		for _, rule := range *source.Rules {
			cfg.Rules = append(cfg.Rules, normalizePaymentAutoUnlockRule(rule))
		}
	}
}

func validatePaymentAutoUnlockConfig(cfg paymentAutoUnlockConfig) error {
	if !cfg.Enabled {
		return nil
	}

	rules := cfg.effectiveRules()
	if len(rules) == 0 {
		return fmt.Errorf("payment auto unlock requires at least one rule when enabled")
	}

	usingLegacyRule := len(cfg.Rules) == 0
	for _, rule := range rules {
		label := paymentAutoUnlockRuleLabel(rule)
		if usingLegacyRule {
			if rule.Threshold <= 0 {
				return fmt.Errorf("payment auto unlock threshold must be greater than 0")
			}
		} else if rule.Threshold < 0 {
			return fmt.Errorf("payment auto unlock rule %s threshold must be greater than or equal to 0", label)
		}

		if rule.GroupID <= 0 && rule.GroupName == "" {
			return fmt.Errorf("payment auto unlock rule %s must set group_id or group_name", label)
		}
	}
	return nil
}

func (cfg paymentAutoUnlockConfig) effectiveRules() []paymentAutoUnlockRule {
	if len(cfg.Rules) > 0 {
		rules := make([]paymentAutoUnlockRule, 0, len(cfg.Rules))
		for _, rule := range cfg.Rules {
			rules = append(rules, normalizePaymentAutoUnlockRule(rule))
		}
		return rules
	}
	if cfg.Threshold > 0 || cfg.GroupID > 0 {
		return []paymentAutoUnlockRule{
			normalizePaymentAutoUnlockRule(paymentAutoUnlockRule{
				Key:       "legacy",
				Threshold: cfg.Threshold,
				GroupID:   cfg.GroupID,
			}),
		}
	}
	return nil
}

func normalizePaymentAutoUnlockRule(rule paymentAutoUnlockRule) paymentAutoUnlockRule {
	rule.Key = strings.TrimSpace(rule.Key)
	rule.GroupName = strings.TrimSpace(rule.GroupName)
	return rule
}

func paymentAutoUnlockRuleLabel(rule paymentAutoUnlockRule) string {
	rule = normalizePaymentAutoUnlockRule(rule)
	if rule.Key != "" {
		return strconv.Quote(rule.Key)
	}
	return strconv.Quote(paymentAutoUnlockRuleTargetDescription(rule))
}

func paymentAutoUnlockRuleTargetDescription(rule paymentAutoUnlockRule) string {
	rule = normalizePaymentAutoUnlockRule(rule)
	switch {
	case rule.GroupID > 0:
		return fmt.Sprintf("group_id=%d", rule.GroupID)
	case rule.GroupName != "":
		return "group_name=" + rule.GroupName
	default:
		return "missing_target"
	}
}

func paymentAutoUnlockRuleMatchesGroup(rule paymentAutoUnlockRule, group *Group) bool {
	if group == nil {
		return false
	}
	rule = normalizePaymentAutoUnlockRule(rule)
	if rule.GroupID > 0 && group.ID == rule.GroupID {
		return true
	}
	return rule.GroupName != "" && strings.EqualFold(strings.TrimSpace(group.Name), rule.GroupName)
}

func paymentAutoUnlockRuleQualifies(rule paymentAutoUnlockRule, totalRecharged float64) bool {
	rule = normalizePaymentAutoUnlockRule(rule)
	return totalRecharged >= rule.Threshold
}

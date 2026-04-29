//go:build unit

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPaymentAutoUnlockConfigEnvOnly(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	t.Setenv(paymentAutoUnlockConfigFileEnv, filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv(paymentAutoUnlockEnabledEnv, "true")
	t.Setenv(paymentAutoUnlockThresholdEnv, "188.8")
	t.Setenv(paymentAutoUnlockGroupIDEnv, "23")

	cfg, err := loadPaymentAutoUnlockConfig()
	require.NoError(t, err)
	assert.Equal(t, paymentAutoUnlockConfig{
		Enabled:   true,
		Threshold: 188.8,
		GroupID:   23,
	}, cfg)
}

func TestLoadPaymentAutoUnlockConfigFileOnlyFromDataDir(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	dataDir := t.TempDir()
	writePaymentAutoUnlockConfigFile(t, filepath.Join(dataDir, paymentAutoUnlockConfigFilename), `{
		"custom_payment_auto_unlock_enabled": true,
		"custom_payment_auto_unlock_threshold": 99.5,
		"custom_payment_auto_unlock_group_id": 8
	}`)
	t.Setenv("DATA_DIR", dataDir)

	cfg, err := loadPaymentAutoUnlockConfig()
	require.NoError(t, err)
	assert.Equal(t, paymentAutoUnlockConfig{
		Enabled:   true,
		Threshold: 99.5,
		GroupID:   8,
	}, cfg)
}

func TestLoadPaymentAutoUnlockConfigEnvOverridesFile(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	path := filepath.Join(t.TempDir(), paymentAutoUnlockConfigFilename)
	writePaymentAutoUnlockConfigFile(t, path, `{
		"custom_payment_auto_unlock_enabled": false,
		"custom_payment_auto_unlock_threshold": 50,
		"custom_payment_auto_unlock_group_id": 3
	}`)

	t.Setenv(paymentAutoUnlockConfigFileEnv, path)
	t.Setenv(paymentAutoUnlockEnabledEnv, "true")
	t.Setenv(paymentAutoUnlockThresholdEnv, "200")
	t.Setenv(paymentAutoUnlockGroupIDEnv, "12")

	cfg, err := loadPaymentAutoUnlockConfig()
	require.NoError(t, err)
	assert.Equal(t, paymentAutoUnlockConfig{
		Enabled:   true,
		Threshold: 200,
		GroupID:   12,
	}, cfg)
}

func TestLoadPaymentAutoUnlockConfigRejectsInvalidEnabledConfig(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	path := filepath.Join(t.TempDir(), paymentAutoUnlockConfigFilename)
	writePaymentAutoUnlockConfigFile(t, path, `{
		"custom_payment_auto_unlock_enabled": true,
		"custom_payment_auto_unlock_threshold": 0,
		"custom_payment_auto_unlock_group_id": 9
	}`)
	t.Setenv(paymentAutoUnlockConfigFileEnv, path)

	_, err := loadPaymentAutoUnlockConfig()
	require.Error(t, err)
	assert.ErrorContains(t, err, "threshold")
}

func writePaymentAutoUnlockConfigFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func resetPaymentAutoUnlockEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		paymentAutoUnlockEnabledEnv,
		paymentAutoUnlockThresholdEnv,
		paymentAutoUnlockGroupIDEnv,
		paymentAutoUnlockConfigFileEnv,
		"DATA_DIR",
	}

	original := make(map[string]*string, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			copied := value
			original[key] = &copied
		} else {
			original[key] = nil
		}
		require.NoError(t, os.Unsetenv(key))
	}

	t.Cleanup(func() {
		for _, key := range keys {
			if original[key] == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *original[key])
		}
	})
}

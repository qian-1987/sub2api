//go:build unit

package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type apiKeyPaymentAutoUnlockUserSubRepoStub struct {
	userSubRepoNoop
	activeSubscriptions []UserSubscription
}

func (s *apiKeyPaymentAutoUnlockUserSubRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	out := make([]UserSubscription, len(s.activeSubscriptions))
	copy(out, s.activeSubscriptions)
	return out, nil
}

func TestAPIKeyServiceGetAvailableGroupsIncludesRechargeUnlockedGroups(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	configPath := filepath.Join(t.TempDir(), paymentAutoUnlockConfigFilename)
	writePaymentAutoUnlockConfigFile(t, configPath, `{
		"custom_payment_auto_unlock_enabled": true,
		"custom_payment_auto_unlock_rules": [
			{
				"key": "VIP",
				"threshold": 0.1,
				"group_name": "VIP"
			},
			{
				"key": "5.5-VIP",
				"threshold": 0.2,
				"group_name": "5.5-VIP"
			}
		]
	}`)
	t.Setenv(paymentAutoUnlockConfigFileEnv, configPath)

	svc := &APIKeyService{
		userRepo: &mockUserRepo{
			getByIDUser: &User{
				ID:             7,
				TotalRecharged: 0.28,
			},
		},
		groupRepo: &stubGroupRepoForAvailable{
			activeGroups: []Group{
				{ID: 1, Name: "default", Status: StatusActive, IsExclusive: false, SubscriptionType: SubscriptionTypeStandard},
				{ID: 2, Name: "VIP", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard},
				{ID: 3, Name: "5.5-VIP", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard},
			},
		},
		userSubRepo: &apiKeyPaymentAutoUnlockUserSubRepoStub{},
	}

	groups, err := svc.GetAvailableGroups(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, []string{"default", "VIP", "5.5-VIP"}, collectGroupNames(groups))
}

func TestAPIKeyServiceCanUserBindGroupAllowsRechargeUnlockedExclusiveGroup(t *testing.T) {
	resetPaymentAutoUnlockEnv(t)

	configPath := filepath.Join(t.TempDir(), paymentAutoUnlockConfigFilename)
	writePaymentAutoUnlockConfigFile(t, configPath, `{
		"custom_payment_auto_unlock_enabled": true,
		"custom_payment_auto_unlock_rules": [
			{
				"key": "VIP",
				"threshold": 0.1,
				"group_name": "VIP"
			}
		]
	}`)
	t.Setenv(paymentAutoUnlockConfigFileEnv, configPath)

	svc := &APIKeyService{}
	allowed := svc.canUserBindGroup(context.Background(), &User{
		ID:             7,
		TotalRecharged: 0.28,
	}, &Group{
		ID:               2,
		Name:             "VIP",
		Status:           StatusActive,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
	})

	require.True(t, allowed)
}

func collectGroupNames(groups []Group) []string {
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.Name)
	}
	return names
}

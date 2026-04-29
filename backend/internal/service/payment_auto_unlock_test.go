//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryPaymentAutoUnlockAfterBalanceRechargeBelowThresholdUsesRedeemCodeAggregation(t *testing.T) {
	userRepo := &paymentAutoUnlockUserRepoStub{}
	groupRepo := &paymentAutoUnlockGroupRepoStub{}
	rechargeHistoryRepo := &paymentAutoUnlockRechargeHistoryRepoStub{total: 99}

	attempt := tryPaymentAutoUnlockAfterBalanceRecharge(context.Background(), paymentAutoUnlockDependencies{
		userRepo:            userRepo,
		groupRepo:           groupRepo,
		rechargeHistoryRepo: rechargeHistoryRepo,
		loadConfig: func() (paymentAutoUnlockConfig, error) {
			return paymentAutoUnlockConfig{Enabled: true, Threshold: 100, GroupID: 6}, nil
		},
	}, &dbent.PaymentOrder{
		UserID:    42,
		OrderType: payment.OrderTypeBalance,
		Amount:    20,
		PayAmount: 999,
	})

	assert.Equal(t, paymentAutoUnlockStatusSkippedBelowThreshold, attempt.Status)
	assert.Equal(t, 99.0, attempt.TotalRecharged)
	assert.Equal(t, []int64{42}, rechargeHistoryRepo.requestedUserIDs)
	assert.Len(t, groupRepo.requestedGroupIDs, 0)
	assert.Empty(t, userRepo.added)
}

func TestTryPaymentAutoUnlockAfterBalanceRechargeRejectsInvalidTargetGroups(t *testing.T) {
	tests := []struct {
		name   string
		group  *Group
		reason string
	}{
		{
			name:   "inactive",
			group:  &Group{ID: 9, Name: "VIP", Status: StatusDisabled, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard},
			reason: "active",
		},
		{
			name:   "not exclusive",
			group:  &Group{ID: 9, Name: "VIP", Status: StatusActive, IsExclusive: false, SubscriptionType: SubscriptionTypeStandard},
			reason: "exclusive",
		},
		{
			name:   "subscription group",
			group:  &Group{ID: 9, Name: "VIP", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription},
			reason: "standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userRepo := &paymentAutoUnlockUserRepoStub{}
			groupRepo := &paymentAutoUnlockGroupRepoStub{group: tt.group}
			rechargeHistoryRepo := &paymentAutoUnlockRechargeHistoryRepoStub{total: 200}
			authCache := &paymentAutoUnlockAuthCacheInvalidatorStub{}

			attempt := tryPaymentAutoUnlockAfterBalanceRecharge(context.Background(), paymentAutoUnlockDependencies{
				userRepo:             userRepo,
				groupRepo:            groupRepo,
				rechargeHistoryRepo:  rechargeHistoryRepo,
				authCacheInvalidator: authCache,
				loadConfig: func() (paymentAutoUnlockConfig, error) {
					return paymentAutoUnlockConfig{Enabled: true, Threshold: 100, GroupID: 9}, nil
				},
			}, &dbent.PaymentOrder{
				UserID:    11,
				OrderType: payment.OrderTypeBalance,
				Amount:    20,
			})

			assert.Equal(t, paymentAutoUnlockStatusInvalidGroup, attempt.Status)
			assert.Equal(t, 200.0, attempt.TotalRecharged)
			assert.Contains(t, attempt.Reason, tt.reason)
			assert.Equal(t, []int64{11}, rechargeHistoryRepo.requestedUserIDs)
			assert.Empty(t, userRepo.added)
			assert.Empty(t, authCache.userIDs)
		})
	}
}

func TestTryPaymentAutoUnlockAfterBalanceRechargeGrantsGroupWhenCumulativeThresholdMet(t *testing.T) {
	userRepo := &paymentAutoUnlockUserRepoStub{}
	groupRepo := &paymentAutoUnlockGroupRepoStub{
		group: &Group{
			ID:               15,
			Name:             "VIP Exclusive",
			Status:           StatusActive,
			IsExclusive:      true,
			SubscriptionType: SubscriptionTypeStandard,
		},
	}
	rechargeHistoryRepo := &paymentAutoUnlockRechargeHistoryRepoStub{total: 100}
	authCache := &paymentAutoUnlockAuthCacheInvalidatorStub{}

	attempt := tryPaymentAutoUnlockAfterBalanceRecharge(context.Background(), paymentAutoUnlockDependencies{
		userRepo:             userRepo,
		groupRepo:            groupRepo,
		rechargeHistoryRepo:  rechargeHistoryRepo,
		authCacheInvalidator: authCache,
		loadConfig: func() (paymentAutoUnlockConfig, error) {
			return paymentAutoUnlockConfig{Enabled: true, Threshold: 100, GroupID: 15}, nil
		},
	}, &dbent.PaymentOrder{
		UserID:    21,
		OrderType: payment.OrderTypeBalance,
		Amount:    20,
	})

	require.Equal(t, paymentAutoUnlockStatusGranted, attempt.Status)
	assert.Equal(t, "VIP Exclusive", attempt.GroupName)
	assert.Equal(t, 100.0, attempt.TotalRecharged)
	assert.Equal(t, []int64{21}, rechargeHistoryRepo.requestedUserIDs)
	assert.Equal(t, []paymentAutoUnlockGrantCall{{userID: 21, groupID: 15}}, userRepo.added)
	assert.Equal(t, []int64{21}, authCache.userIDs)
}

func TestTryPaymentAutoUnlockAfterBalanceRechargeReportsGrantFailure(t *testing.T) {
	userRepo := &paymentAutoUnlockUserRepoStub{err: errors.New("insert failed")}
	groupRepo := &paymentAutoUnlockGroupRepoStub{
		group: &Group{
			ID:               15,
			Name:             "VIP Exclusive",
			Status:           StatusActive,
			IsExclusive:      true,
			SubscriptionType: SubscriptionTypeStandard,
		},
	}
	rechargeHistoryRepo := &paymentAutoUnlockRechargeHistoryRepoStub{total: 128}
	authCache := &paymentAutoUnlockAuthCacheInvalidatorStub{}

	attempt := tryPaymentAutoUnlockAfterBalanceRecharge(context.Background(), paymentAutoUnlockDependencies{
		userRepo:             userRepo,
		groupRepo:            groupRepo,
		rechargeHistoryRepo:  rechargeHistoryRepo,
		authCacheInvalidator: authCache,
		loadConfig: func() (paymentAutoUnlockConfig, error) {
			return paymentAutoUnlockConfig{Enabled: true, Threshold: 100, GroupID: 15}, nil
		},
	}, &dbent.PaymentOrder{
		UserID:    21,
		OrderType: payment.OrderTypeBalance,
		Amount:    20,
	})

	require.Equal(t, paymentAutoUnlockStatusGrantFailed, attempt.Status)
	assert.Equal(t, 128.0, attempt.TotalRecharged)
	assert.Equal(t, []int64{21}, rechargeHistoryRepo.requestedUserIDs)
	assert.Contains(t, attempt.Reason, "insert failed")
	assert.Empty(t, authCache.userIDs)
}

func TestTryPaymentAutoUnlockAfterBalanceRechargeReportsRechargeHistoryFailure(t *testing.T) {
	userRepo := &paymentAutoUnlockUserRepoStub{}
	groupRepo := &paymentAutoUnlockGroupRepoStub{}
	rechargeHistoryRepo := &paymentAutoUnlockRechargeHistoryRepoStub{err: errors.New("redeem code sum failed")}

	attempt := tryPaymentAutoUnlockAfterBalanceRecharge(context.Background(), paymentAutoUnlockDependencies{
		userRepo:            userRepo,
		groupRepo:           groupRepo,
		rechargeHistoryRepo: rechargeHistoryRepo,
		loadConfig: func() (paymentAutoUnlockConfig, error) {
			return paymentAutoUnlockConfig{Enabled: true, Threshold: 100, GroupID: 15}, nil
		},
	}, &dbent.PaymentOrder{
		UserID:    21,
		OrderType: payment.OrderTypeBalance,
		Amount:    20,
	})

	require.Equal(t, paymentAutoUnlockStatusGrantFailed, attempt.Status)
	assert.Equal(t, []int64{21}, rechargeHistoryRepo.requestedUserIDs)
	assert.Contains(t, attempt.Reason, "redeem code sum failed")
	assert.Empty(t, groupRepo.requestedGroupIDs)
}

type paymentAutoUnlockGrantCall struct {
	userID  int64
	groupID int64
}

type paymentAutoUnlockUserRepoStub struct {
	added []paymentAutoUnlockGrantCall
	err   error
}

func (s *paymentAutoUnlockUserRepoStub) AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	if s.err != nil {
		return s.err
	}
	s.added = append(s.added, paymentAutoUnlockGrantCall{userID: userID, groupID: groupID})
	return nil
}

type paymentAutoUnlockRechargeHistoryRepoStub struct {
	total            float64
	err              error
	requestedUserIDs []int64
}

func (s *paymentAutoUnlockRechargeHistoryRepoStub) SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error) {
	s.requestedUserIDs = append(s.requestedUserIDs, userID)
	if s.err != nil {
		return 0, s.err
	}
	return s.total, nil
}

type paymentAutoUnlockGroupRepoStub struct {
	group             *Group
	err               error
	requestedGroupIDs []int64
}

func (s *paymentAutoUnlockGroupRepoStub) GetByID(ctx context.Context, id int64) (*Group, error) {
	s.requestedGroupIDs = append(s.requestedGroupIDs, id)
	if s.err != nil {
		return nil, s.err
	}
	return s.group, nil
}

type paymentAutoUnlockAuthCacheInvalidatorStub struct {
	userIDs []int64
}

func (s *paymentAutoUnlockAuthCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {}

func (s *paymentAutoUnlockAuthCacheInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (s *paymentAutoUnlockAuthCacheInvalidatorStub) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {}

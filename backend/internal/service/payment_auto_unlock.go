package service

import (
	"context"
	"fmt"
	"log/slog"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	paymentAutoUnlockAuditGranted       = "PAYMENT_AUTO_UNLOCK_GRANTED"
	paymentAutoUnlockAuditFailed        = "PAYMENT_AUTO_UNLOCK_FAILED"
	paymentAutoUnlockAuditInvalidGroup  = "PAYMENT_AUTO_UNLOCK_INVALID_GROUP"
	paymentAutoUnlockAuditInvalidConfig = "PAYMENT_AUTO_UNLOCK_INVALID_CONFIG"
)

type paymentAutoUnlockStatus string

const (
	paymentAutoUnlockStatusSkippedDisabled       paymentAutoUnlockStatus = "skipped_disabled"
	paymentAutoUnlockStatusSkippedBelowThreshold paymentAutoUnlockStatus = "skipped_below_threshold"
	paymentAutoUnlockStatusGranted               paymentAutoUnlockStatus = "granted"
	paymentAutoUnlockStatusInvalidConfig         paymentAutoUnlockStatus = "invalid_config"
	paymentAutoUnlockStatusInvalidGroup          paymentAutoUnlockStatus = "invalid_group"
	paymentAutoUnlockStatusGrantFailed           paymentAutoUnlockStatus = "grant_failed"
)

type paymentAutoUnlockAttempt struct {
	Status         paymentAutoUnlockStatus
	Threshold      float64
	GroupID        int64
	GroupName      string
	OrderAmount    float64
	TotalRecharged float64
	Reason         string
}

type paymentAutoUnlockUserRepo interface {
	AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error
}

type paymentAutoUnlockGroupLookupRepo interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
}

type paymentAutoUnlockRechargeHistoryRepo interface {
	SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error)
}

type paymentAutoUnlockDependencies struct {
	userRepo             paymentAutoUnlockUserRepo
	groupRepo            paymentAutoUnlockGroupLookupRepo
	rechargeHistoryRepo  paymentAutoUnlockRechargeHistoryRepo
	authCacheInvalidator APIKeyAuthCacheInvalidator
	loadConfig           func() (paymentAutoUnlockConfig, error)
}

func (s *PaymentService) tryAutoUnlockGroupAfterBalanceRecharge(ctx context.Context, o *dbent.PaymentOrder) {
	if o == nil {
		return
	}

	attempt := tryPaymentAutoUnlockAfterBalanceRecharge(ctx, paymentAutoUnlockDependencies{
		userRepo:  s.userRepo,
		groupRepo: s.groupRepo,
		rechargeHistoryRepo: func() paymentAutoUnlockRechargeHistoryRepo {
			if s.redeemService == nil {
				return nil
			}
			return s.redeemService.redeemRepo
		}(),
		loadConfig: loadPaymentAutoUnlockConfig,
		authCacheInvalidator: func() APIKeyAuthCacheInvalidator {
			if s.redeemService == nil {
				return nil
			}
			return s.redeemService.authCacheInvalidator
		}(),
	}, o)

	switch attempt.Status {
	case paymentAutoUnlockStatusSkippedDisabled, paymentAutoUnlockStatusSkippedBelowThreshold:
		return
	case paymentAutoUnlockStatusGranted:
		slog.Info("payment auto unlock granted",
			"orderID", o.ID,
			"userID", o.UserID,
			"groupID", attempt.GroupID,
			"groupName", attempt.GroupName,
			"orderAmount", attempt.OrderAmount,
			"totalRecharged", attempt.TotalRecharged,
			"threshold", attempt.Threshold,
		)
		s.writeAuditLog(ctx, o.ID, paymentAutoUnlockAuditGranted, "system", paymentAutoUnlockAuditDetail(o, attempt))
	case paymentAutoUnlockStatusInvalidConfig:
		slog.Warn("payment auto unlock skipped due to invalid config",
			"orderID", o.ID,
			"userID", o.UserID,
			"reason", attempt.Reason,
		)
		s.writeAuditLog(ctx, o.ID, paymentAutoUnlockAuditInvalidConfig, "system", paymentAutoUnlockAuditDetail(o, attempt))
	case paymentAutoUnlockStatusInvalidGroup:
		slog.Warn("payment auto unlock skipped due to invalid target group",
			"orderID", o.ID,
			"userID", o.UserID,
			"groupID", attempt.GroupID,
			"totalRecharged", attempt.TotalRecharged,
			"reason", attempt.Reason,
		)
		s.writeAuditLog(ctx, o.ID, paymentAutoUnlockAuditInvalidGroup, "system", paymentAutoUnlockAuditDetail(o, attempt))
	case paymentAutoUnlockStatusGrantFailed:
		slog.Warn("payment auto unlock grant failed",
			"orderID", o.ID,
			"userID", o.UserID,
			"groupID", attempt.GroupID,
			"totalRecharged", attempt.TotalRecharged,
			"reason", attempt.Reason,
		)
		s.writeAuditLog(ctx, o.ID, paymentAutoUnlockAuditFailed, "system", paymentAutoUnlockAuditDetail(o, attempt))
	}
}

func tryPaymentAutoUnlockAfterBalanceRecharge(ctx context.Context, deps paymentAutoUnlockDependencies, o *dbent.PaymentOrder) paymentAutoUnlockAttempt {
	attempt := paymentAutoUnlockAttempt{}
	if o == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = "payment order is nil"
		return attempt
	}

	attempt.OrderAmount = o.Amount

	if o.OrderType != payment.OrderTypeBalance {
		attempt.Status = paymentAutoUnlockStatusSkippedDisabled
		return attempt
	}
	if deps.loadConfig == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = "payment auto unlock config loader is nil"
		return attempt
	}

	cfg, err := deps.loadConfig()
	if err != nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = err.Error()
		return attempt
	}

	attempt.Threshold = cfg.Threshold
	attempt.GroupID = cfg.GroupID
	if !cfg.Enabled {
		attempt.Status = paymentAutoUnlockStatusSkippedDisabled
		return attempt
	}
	if deps.userRepo == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = "payment auto unlock user repository is nil"
		return attempt
	}
	if deps.groupRepo == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = "payment auto unlock group repository is nil"
		return attempt
	}
	if deps.rechargeHistoryRepo == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidConfig
		attempt.Reason = "payment auto unlock recharge history repository is nil"
		return attempt
	}

	totalRecharged, err := deps.rechargeHistoryRepo.SumPositiveBalanceByUser(ctx, o.UserID)
	if err != nil {
		attempt.Status = paymentAutoUnlockStatusGrantFailed
		attempt.Reason = fmt.Sprintf("sum positive balance by user %d from redeem codes: %v", o.UserID, err)
		return attempt
	}
	attempt.TotalRecharged = totalRecharged
	if totalRecharged < cfg.Threshold {
		attempt.Status = paymentAutoUnlockStatusSkippedBelowThreshold
		return attempt
	}

	group, err := deps.groupRepo.GetByID(ctx, cfg.GroupID)
	if err != nil {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("get group %d: %v", cfg.GroupID, err)
		return attempt
	}
	if group == nil {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("group %d not found", cfg.GroupID)
		return attempt
	}

	attempt.GroupName = group.Name
	if !group.IsActive() {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("group %d must be active", cfg.GroupID)
		return attempt
	}
	if !group.IsExclusive {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("group %d must be exclusive", cfg.GroupID)
		return attempt
	}
	if group.IsSubscriptionType() {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("group %d must be standard", cfg.GroupID)
		return attempt
	}

	if err := deps.userRepo.AddGroupToAllowedGroups(ctx, o.UserID, cfg.GroupID); err != nil {
		attempt.Status = paymentAutoUnlockStatusGrantFailed
		attempt.Reason = fmt.Sprintf("grant group %d to user %d: %v", cfg.GroupID, o.UserID, err)
		return attempt
	}
	if deps.authCacheInvalidator != nil {
		deps.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, o.UserID)
	}

	attempt.Status = paymentAutoUnlockStatusGranted
	return attempt
}

func paymentAutoUnlockAuditDetail(o *dbent.PaymentOrder, attempt paymentAutoUnlockAttempt) map[string]any {
	detail := map[string]any{
		"status":         string(attempt.Status),
		"userID":         o.UserID,
		"amount":         attempt.OrderAmount,
		"totalRecharged": attempt.TotalRecharged,
		"threshold":      attempt.Threshold,
		"groupID":        attempt.GroupID,
		"rechargeCode":   o.RechargeCode,
	}
	if attempt.GroupName != "" {
		detail["groupName"] = attempt.GroupName
	}
	if attempt.Reason != "" {
		detail["reason"] = attempt.Reason
	}
	return detail
}

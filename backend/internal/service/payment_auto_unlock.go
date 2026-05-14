package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
	GrantedGroups  []paymentAutoUnlockGrantedGroup
}

type paymentAutoUnlockGrantedGroup struct {
	RuleKey   string
	Threshold float64
	GroupID   int64
	GroupName string
}

type paymentAutoUnlockUserRepo interface {
	AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error
}

type paymentAutoUnlockGroupLookupRepo interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
	ListActive(ctx context.Context) ([]Group, error)
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

	if !cfg.Enabled {
		attempt.Status = paymentAutoUnlockStatusSkippedDisabled
		return attempt
	}

	rules := cfg.effectiveRules()
	if len(rules) == 1 {
		attempt.Threshold = rules[0].Threshold
		attempt.GroupID = rules[0].GroupID
		attempt.GroupName = rules[0].GroupName
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

	qualifiedRules := make([]paymentAutoUnlockRule, 0, len(rules))
	for _, rule := range rules {
		if paymentAutoUnlockRuleQualifies(rule, totalRecharged) {
			qualifiedRules = append(qualifiedRules, normalizePaymentAutoUnlockRule(rule))
		}
	}
	if len(qualifiedRules) == 0 {
		attempt.Status = paymentAutoUnlockStatusSkippedBelowThreshold
		return attempt
	}

	activeGroups, err := deps.groupRepo.ListActive(ctx)
	if err != nil {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = fmt.Sprintf("list active groups: %v", err)
		return attempt
	}

	activeGroupsByID := make(map[int64]*Group, len(activeGroups))
	activeGroupsByName := make(map[string]*Group, len(activeGroups))
	for i := range activeGroups {
		group := activeGroups[i]
		groupCopy := group
		activeGroupsByID[group.ID] = &groupCopy
		if name := strings.ToLower(strings.TrimSpace(group.Name)); name != "" {
			activeGroupsByName[name] = &groupCopy
		}
	}

	grantedGroups := make([]paymentAutoUnlockGrantedGroup, 0, len(qualifiedRules))
	invalidReasons := make([]string, 0)
	for _, rule := range qualifiedRules {
		group, err := resolvePaymentAutoUnlockRuleGroup(ctx, deps.groupRepo, rule, activeGroupsByID, activeGroupsByName)
		if err != nil {
			invalidReasons = append(invalidReasons, err.Error())
			continue
		}
		if !group.IsActive() {
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s must be active", paymentAutoUnlockRuleTargetDescription(rule)))
			continue
		}
		if !group.IsExclusive {
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s must target an exclusive group", paymentAutoUnlockRuleTargetDescription(rule)))
			continue
		}
		if group.IsSubscriptionType() {
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s must target a standard group", paymentAutoUnlockRuleTargetDescription(rule)))
			continue
		}

		if err := deps.userRepo.AddGroupToAllowedGroups(ctx, o.UserID, group.ID); err != nil {
			attempt.Status = paymentAutoUnlockStatusGrantFailed
			attempt.Threshold = rule.Threshold
			attempt.GroupID = group.ID
			attempt.GroupName = group.Name
			attempt.GrantedGroups = grantedGroups
			attempt.Reason = fmt.Sprintf("grant group %d to user %d: %v", group.ID, o.UserID, err)
			return attempt
		}

		attempt.Threshold = rule.Threshold
		attempt.GroupID = group.ID
		attempt.GroupName = group.Name
		grantedGroups = append(grantedGroups, paymentAutoUnlockGrantedGroup{
			RuleKey:   rule.Key,
			Threshold: rule.Threshold,
			GroupID:   group.ID,
			GroupName: group.Name,
		})
	}

	if len(grantedGroups) == 0 {
		attempt.Status = paymentAutoUnlockStatusInvalidGroup
		attempt.Reason = strings.Join(invalidReasons, "; ")
		if attempt.Reason == "" {
			attempt.Reason = "no payment auto unlock rules resolved to a grantable group"
		}
		return attempt
	}

	if deps.authCacheInvalidator != nil {
		deps.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, o.UserID)
	}

	attempt.GrantedGroups = grantedGroups
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
	if len(attempt.GrantedGroups) > 0 {
		granted := make([]map[string]any, 0, len(attempt.GrantedGroups))
		for _, group := range attempt.GrantedGroups {
			entry := map[string]any{
				"threshold": group.Threshold,
				"groupID":   group.GroupID,
				"groupName": group.GroupName,
			}
			if group.RuleKey != "" {
				entry["key"] = group.RuleKey
			}
			granted = append(granted, entry)
		}
		detail["grantedGroups"] = granted
	}
	if attempt.Reason != "" {
		detail["reason"] = attempt.Reason
	}
	return detail
}

func resolvePaymentAutoUnlockRuleGroup(
	ctx context.Context,
	groupRepo paymentAutoUnlockGroupLookupRepo,
	rule paymentAutoUnlockRule,
	activeGroupsByID map[int64]*Group,
	activeGroupsByName map[string]*Group,
) (*Group, error) {
	rule = normalizePaymentAutoUnlockRule(rule)
	if rule.GroupID > 0 {
		if group, ok := activeGroupsByID[rule.GroupID]; ok {
			return group, nil
		}

		group, err := groupRepo.GetByID(ctx, rule.GroupID)
		if err != nil {
			return nil, fmt.Errorf("get %s: %v", paymentAutoUnlockRuleTargetDescription(rule), err)
		}
		if group == nil {
			return nil, fmt.Errorf("%s not found", paymentAutoUnlockRuleTargetDescription(rule))
		}
		return group, nil
	}

	if rule.GroupName != "" {
		if group, ok := activeGroupsByName[strings.ToLower(rule.GroupName)]; ok {
			return group, nil
		}
		return nil, fmt.Errorf("%s not found in active groups", paymentAutoUnlockRuleTargetDescription(rule))
	}

	return nil, fmt.Errorf("%s is missing a target group", paymentAutoUnlockRuleTargetDescription(rule))
}

package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionquotagrant"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
)

const (
	quotaGrantStatusActive    = SubscriptionQuotaGrantStatusActive
	quotaGrantStatusAmbiguous = SubscriptionQuotaGrantStatusAmbiguous
	quotaGrantStatusRefunded  = SubscriptionQuotaGrantStatusRefunded
)

// doSubscriptionTopup attaches a paid package to the exact subscription cycle
// snapshotted when the order was created. The subscription row is locked while
// the grant is inserted, so a renewal cannot race a delayed webhook.
func (s *PaymentService) doSubscriptionTopup(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease) error {
	if o == nil || o.SubscriptionID == nil || o.SubscriptionGroupID == nil || o.SubscriptionCycleStart == nil || o.QuotaUsd == nil || *o.QuotaUsd <= 0 {
		return s.requireTopupRefund(ctx, o, "top-up order is missing its subscription cycle binding")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin top-up fulfillment tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	// The unique payment_order_id makes replay idempotent. Reading it inside the
	// same transaction also ensures a repeated worker does not create a second
	// grant before the first transaction commits.
	existing, err := tx.SubscriptionQuotaGrant.Query().Where(subscriptionquotagrant.PaymentOrderIDEQ(o.ID)).Only(txCtx)
	if err == nil {
		// A previous fulfillment may have committed the grant, then failed while
		// invalidating caches or marking the order complete. Usage can legitimately
		// move that grant to consumption_ambiguous before the webhook is retried;
		// both active and ambiguous are still the same paid grant and must remain
		// idempotently fulfillable. Refunded or mismatched grants are conflicts.
		if (existing.Status != quotaGrantStatusActive && existing.Status != quotaGrantStatusAmbiguous) || existing.SubscriptionID != *o.SubscriptionID || !existing.CycleStart.Equal(*o.SubscriptionCycleStart) {
			return s.requireTopupRefund(ctx, o, "top-up order already has a conflicting grant")
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit idempotent top-up fulfillment: %w", err)
		}
		if s.subscriptionSvc != nil && o.SubscriptionGroupID != nil {
			if err := s.subscriptionSvc.invalidateSubscriptionCaches(o.UserID, *o.SubscriptionGroupID); err != nil {
				return fmt.Errorf("invalidate idempotent top-up subscription cache: %w", err)
			}
		}
		return s.markCompleted(ctx, o, lease, "SUBSCRIPTION_TOPUP_SUCCESS")
	}
	if !dbent.IsNotFound(err) {
		return fmt.Errorf("check top-up grant: %w", err)
	}

	sub, err := tx.UserSubscription.Query().Where(
		usersubscription.IDEQ(*o.SubscriptionID),
		usersubscription.UserIDEQ(o.UserID),
		usersubscription.GroupIDEQ(*o.SubscriptionGroupID),
	).ForUpdate().Only(txCtx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return s.requireTopupRefund(ctx, o, "the original subscription no longer exists")
		}
		return fmt.Errorf("load original subscription for top-up: %w", err)
	}
	now := time.Now()
	if sub.Status != SubscriptionStatusActive || !sub.ExpiresAt.After(now) || !sub.StartsAt.Equal(*o.SubscriptionCycleStart) {
		return s.requireTopupRefund(ctx, o, "the original subscription cycle has expired or changed")
	}

	_, err = tx.SubscriptionQuotaGrant.Create().
		SetSubscriptionID(sub.ID).
		SetPaymentOrderID(o.ID).
		SetCycleStart(sub.StartsAt).
		SetGrantedUsd(*o.QuotaUsd).
		SetUsedUsd(0).
		SetStatus(quotaGrantStatusActive).
		Save(txCtx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			// A concurrent webhook won the unique order claim. It is safe to
			// retry as an idempotent fulfillment after the transaction rolls back.
			return fmt.Errorf("top-up grant claim raced; retry fulfillment: %w", err)
		}
		return fmt.Errorf("create top-up grant: %w", err)
	}
	detail := fmt.Sprintf(`{"subscription_id":%d,"cycle_start":%q,"granted_usd":%.8f}`, sub.ID, sub.StartsAt.UTC().Format(time.RFC3339Nano), *o.QuotaUsd)
	if _, err := tx.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(o.ID, 10)).
		SetAction("SUBSCRIPTION_TOPUP_GRANTED").
		SetDetail(detail).
		SetOperator("system").
		Save(txCtx); err != nil {
		return fmt.Errorf("record top-up grant audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit top-up fulfillment: %w", err)
	}
	if s.subscriptionSvc != nil {
		if err := s.subscriptionSvc.invalidateSubscriptionCaches(o.UserID, sub.GroupID); err != nil {
			return fmt.Errorf("invalidate top-up subscription cache: %w", err)
		}
	}
	return s.markCompleted(ctx, o, lease, "SUBSCRIPTION_TOPUP_SUCCESS")
}

// requireTopupRefund leaves a paid top-up order visible as REFUND_REQUESTED.
// It deliberately does not claim that a gateway refund happened; operators can
// use the normal refund flow after reviewing the cycle and grant state.
func (s *PaymentService) requireTopupRefund(ctx context.Context, o *dbent.PaymentOrder, reason string) error {
	return s.requireSubscriptionOrderRefund(ctx, o, reason, "SUBSCRIPTION_TOPUP_REFUND_REQUIRED")
}

func (s *PaymentService) requireSubscriptionOrderRefund(ctx context.Context, o *dbent.PaymentOrder, reason, action string) error {
	if o == nil {
		return errors.New(reason)
	}
	now := time.Now()
	_, updateErr := s.entClient.PaymentOrder.Update().
		Where(paymentorder.IDEQ(o.ID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRecharging, OrderStatusPaid, OrderStatusFailed, OrderStatusRefundRequested, OrderStatusRefundPending, OrderStatusRefundFailed)).
		SetStatus(OrderStatusRefundRequested).
		SetRefundAmount(o.Amount).
		SetRefundRequestedAt(now).
		SetRefundRequestReason(reason).
		SetRefundRequestedBy("system").
		SetFailedAt(now).
		SetFailedReason(reason).
		Save(ctx)
	if updateErr == nil {
		s.writeAuditLog(ctx, o.ID, action, "system", map[string]any{
			"reason": reason,
			"amount": o.Amount,
		})
	}
	if updateErr != nil {
		return fmt.Errorf("%s; unable to mark manual refund: %w", reason, updateErr)
	}
	return fmt.Errorf("%s; order marked for manual refund", reason)
}

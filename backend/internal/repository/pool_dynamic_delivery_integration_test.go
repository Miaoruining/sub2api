//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func poolDynamicOrderFixture(t *testing.T, plan, mode string) (*poolRepository, int64, int64, []int64) {
	t.Helper()
	r, oldOrderID, users := poolFixture(t)
	resourceID := poolResourceID(t, oldOrderID)
	p := service.PoolProduct{
		PoolCreditConfig: service.PoolCreditConfig{QuotaMode: mode, PlanType: plan},
		Title:            fmt.Sprintf("动态 %s %s", plan, uuid.NewString()),
		Seats:            2,
		Price:            10,
		DurationDays:     30,
		FormationDays:    2,
		Concurrency:      1,
		Status:           "active",
	}
	if mode == "dynamic_shadow" {
		p.TotalCredit = 20
	}
	pid, err := r.SaveProduct(context.Background(), 0, p)
	require.NoError(t, err)
	oid, err := r.PurchaseProduct(context.Background(), pid, users[0], uuid.NewString(), 1)
	require.NoError(t, err)
	_, err = r.Join(context.Background(), oid, users[1])
	require.NoError(t, err)
	return r, oid, resourceID, users
}

func poolResourceAccountID(t *testing.T, resourceID int64) int64 {
	t.Helper()
	var accountID int64
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM pool_resources WHERE id=$1`, resourceID).Scan(&accountID))
	return accountID
}

func setPoolResourceOAuth(t *testing.T, resourceID int64, plan string) int64 {
	t.Helper()
	accountID := poolResourceAccountID(t, resourceID)
	credentials := fmt.Sprintf(`{"access_token":"pool-test-oauth-%d","plan_type":"%s"}`, accountID, plan)
	_, err := integrationDB.Exec(`UPDATE accounts SET type='oauth',parent_account_id=NULL,credentials=$2::jsonb WHERE id=$1`, accountID, credentials)
	require.NoError(t, err)
	return accountID
}

func applyPoolDynamicSnapshot(t *testing.T, accountID int64, observed time.Time, blocked bool, windows ...service.PoolDynamicSnapshotWindow) {
	t.Helper()
	repo := NewPoolDynamicRepository(integrationDB)
	err := repo.ApplyDynamicSnapshot(context.Background(), service.PoolDynamicSnapshot{
		AccountID:  accountID,
		ObservedAt: observed,
		Blocked:    blocked,
		Windows:    windows,
	})
	require.NoError(t, err)
}

func poolDynamicWindow(key string, used float64, observed time.Time) service.PoolDynamicSnapshotWindow {
	return service.PoolDynamicSnapshotWindow{
		Key:           key,
		UsedPercent:   used,
		ResetsAt:      observed.Add(5 * time.Hour),
		WindowSeconds: map[string]int64{"codex/300": 5 * 60 * 60, "codex/10080": 7 * 24 * 60 * 60}[key],
	}
}

func poolDynamicCounts(t *testing.T, orderID int64) (keys, delivered int) {
	t.Helper()
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(api_key_id) FROM pool_members WHERE order_id=$1`, orderID).Scan(&keys))
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_notifications WHERE order_id=$1 AND kind='delivered'`, orderID).Scan(&delivered))
	return keys, delivered
}

func TestPoolDynamicDeliveryRejectsWithoutSnapshotBeforeCreatingKeys(t *testing.T) {
	r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "plus", "dynamic")
	setPoolResourceOAuth(t, resourceID, "plus")

	_, err := r.Deliver(context.Background(), orderID, resourceID)
	require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
	keys, delivered := poolDynamicCounts(t, orderID)
	require.Zero(t, keys)
	require.Zero(t, delivered)
	var status string
	require.NoError(t, integrationDB.QueryRow(`SELECT status FROM pool_orders WHERE id=$1`, orderID).Scan(&status))
	require.Equal(t, "awaiting_delivery", status)
}

func TestPoolDynamicDeliveryRejectsAPIKeyEvenWithSnapshot(t *testing.T) {
	r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "plus", "dynamic")
	accountID := poolResourceAccountID(t, resourceID)
	now := time.Now().UTC()
	applyPoolDynamicSnapshot(t, accountID, now, false, poolDynamicWindow("codex/300", 20, now))

	_, err := r.Deliver(context.Background(), orderID, resourceID)
	require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
	keys, delivered := poolDynamicCounts(t, orderID)
	require.Zero(t, keys)
	require.Zero(t, delivered)
}

func TestPoolDynamicDeliveryInitializesFreshOAuthWindowEntitlements(t *testing.T) {
	r, orderID, resourceID, users := poolDynamicOrderFixture(t, "plus", "dynamic")
	accountID := setPoolResourceOAuth(t, resourceID, "plus")
	now := time.Now().UTC()
	applyPoolDynamicSnapshot(t, accountID, now, false,
		poolDynamicWindow("codex/300", 20, now),
		poolDynamicWindow("codex/10080", 40, now),
	)

	affected, err := r.Deliver(context.Background(), orderID, resourceID)
	require.NoError(t, err)
	require.ElementsMatch(t, users[:2], affected)
	keys, delivered := poolDynamicCounts(t, orderID)
	require.Equal(t, 2, keys)
	require.Equal(t, 2, delivered)

	type entitlement struct {
		userID   int64
		key      string
		used     float64
		reserved float64
		entitled float64
	}
	rows, err := integrationDB.Query(`
		SELECT m.user_id,l.key,ml.used_percent,ml.reserved_percent,ml.entitlement_percent
		FROM pool_dynamic_window_ledgers l
		JOIN pool_dynamic_member_ledgers ml ON ml.ledger_id=l.id
		JOIN pool_members m ON m.id=ml.member_id
		WHERE l.order_id=$1 ORDER BY m.user_id,l.key`, orderID)
	require.NoError(t, err)
	defer rows.Close()
	var got []entitlement
	for rows.Next() {
		var e entitlement
		require.NoError(t, rows.Scan(&e.userID, &e.key, &e.used, &e.reserved, &e.entitled))
		got = append(got, e)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 4)
	for _, e := range got {
		require.Contains(t, users[:2], e.userID)
		require.Zero(t, e.used)
		require.Zero(t, e.reserved)
		want := map[string]float64{"codex/300": (100 - 20 - 2) / 2, "codex/10080": (100 - 40 - 2) / 2}[e.key]
		require.InDelta(t, want, e.entitled-e.used-e.reserved, 0.0001)
	}
}

func TestPoolDynamicDeliveryProKeepsObservedFiveHourWindow(t *testing.T) {
	r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "pro", "dynamic")
	accountID := setPoolResourceOAuth(t, resourceID, "pro")
	now := time.Now().UTC()
	applyPoolDynamicSnapshot(t, accountID, now, false, poolDynamicWindow("codex/300", 55, now))

	_, err := r.Deliver(context.Background(), orderID, resourceID)
	require.NoError(t, err)
	var count int
	var key string
	var baseline float64
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*),MIN(key),MIN(baseline_used_percent) FROM pool_dynamic_window_ledgers WHERE order_id=$1`, orderID).Scan(&count, &key, &baseline))
	require.Equal(t, 1, count)
	require.Equal(t, "codex/300", key)
	require.InDelta(t, 55, baseline, 0.0001)
}

func TestPoolDynamicDeliveryRejectsBlockedAndStaleSnapshots(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "plus", "dynamic")
		accountID := setPoolResourceOAuth(t, resourceID, "plus")
		now := time.Now().UTC()
		applyPoolDynamicSnapshot(t, accountID, now, true, poolDynamicWindow("codex/300", 20, now))

		_, err := r.Deliver(context.Background(), orderID, resourceID)
		require.ErrorIs(t, err, service.ErrPoolDynamicBlocked)
		keys, delivered := poolDynamicCounts(t, orderID)
		require.Zero(t, keys)
		require.Zero(t, delivered)
	})

	t.Run("stale", func(t *testing.T) {
		r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "plus", "dynamic")
		accountID := setPoolResourceOAuth(t, resourceID, "plus")
		observed := time.Now().UTC().Add(-3 * time.Minute)
		applyPoolDynamicSnapshot(t, accountID, observed, false, poolDynamicWindow("codex/300", 20, observed))

		_, err := r.Deliver(context.Background(), orderID, resourceID)
		require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
		keys, delivered := poolDynamicCounts(t, orderID)
		require.Zero(t, keys)
		require.Zero(t, delivered)
	})
}

func TestPoolDynamicShadowDeliveryAllowsMissingSnapshot(t *testing.T) {
	r, orderID, resourceID, _ := poolDynamicOrderFixture(t, "pro", "dynamic_shadow")

	_, err := r.Deliver(context.Background(), orderID, resourceID)
	require.NoError(t, err)
	keys, delivered := poolDynamicCounts(t, orderID)
	require.Equal(t, 2, keys)
	require.Equal(t, 2, delivered)

	var snapshotCount int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_snapshots WHERE account_id=$1`, poolResourceAccountID(t, resourceID)).Scan(&snapshotCount))
	require.Zero(t, snapshotCount)
}

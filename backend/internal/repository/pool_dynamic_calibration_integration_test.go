//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func calibrationFixture(t *testing.T) (*poolRepository, int64, int64, []int64, service.PoolDynamicSnapshotWindow) {
	t.Helper()
	r, oid, aid, users := dynamicCoreFixture(t, "dynamic")
	now := time.Now().UTC().Add(-time.Second)
	w := service.PoolDynamicSnapshotWindow{Key: "codex/300", WindowSeconds: 18000, ResetsAt: now.Add(5 * time.Hour)}
	applyCoreSnapshot(t, r, aid, now, false, w)
	return r, oid, aid, users, w
}

func calibrationLedger(t *testing.T, oid, mid int64) (used, reserved, remaining, external, pending float64) {
	t.Helper()
	require.NoError(t, integrationDB.QueryRow(`SELECT m.used_percent,m.reserved_percent,GREATEST(0,m.entitlement_percent-m.used_percent-m.reserved_percent),l.external_used_percent,l.pending_delta_percent FROM pool_dynamic_member_ledgers m JOIN pool_dynamic_window_ledgers l ON l.id=m.ledger_id WHERE l.order_id=$1 AND m.member_id=$2 ORDER BY l.id DESC LIMIT 1`, oid, mid).Scan(&used, &reserved, &remaining, &external, &pending))
	return
}

func TestPoolDynamicCalibrationAttributesFullDeltaAndKeepsOtherShare(t *testing.T) {
	r, oid, aid, users, w := calibrationFixture(t)
	ctx := context.Background()
	mid, kid, _ := poolMemberID(t, oid, users[0])
	other := coreMember(t, oid, users[1])
	id, err := r.Reserve(ctx, mid, 8192, 0.01)
	require.NoError(t, err)
	settleCredit(t, id, kid, 0.01)
	u, _, _, _, _ := calibrationLedger(t, oid, mid)
	require.Equal(t, 1.0, u)
	w.UsedPercent = 20
	observed := time.Now().UTC()
	applyCoreSnapshot(t, r, aid, observed, false, w)
	applyCoreSnapshot(t, r, aid, observed, false, w) // 同一快照重试不能重复扣除。
	u, res, rem, external, pending := calibrationLedger(t, oid, mid)
	require.InDelta(t, 20, u, 1e-8)
	require.Zero(t, res)
	require.InDelta(t, 29, rem, 1e-8)
	require.Zero(t, external)
	require.Zero(t, pending)
	otherUse, _, otherRem, _, _ := calibrationLedger(t, oid, other)
	require.Zero(t, otherUse)
	require.InDelta(t, 49, otherRem, 1e-8)
	settleCredit(t, id, kid, 0.01) // 迟到的重复结算不能覆盖已校准的20pp。
	u, _, _, _, _ = calibrationLedger(t, oid, mid)
	require.InDelta(t, 20, u, 1e-8)
	listed, err := r.List(ctx, users[0], false)
	require.NoError(t, err)
	var quota *service.PoolDynamicQuota
	for _, order := range listed {
		if order.ID == oid {
			require.NotNil(t, order.Mine)
			quota = order.Mine.DynamicQuota
		}
	}
	require.NotNil(t, quota)
	require.Equal(t, "ready", quota.Status)
	require.InDelta(t, 29, quota.Windows[0].RemainingPercent, 1e-8)
	history, err := r.DynamicUsage(ctx, users[0])
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "calibrated", history[0].Status)
	require.InDelta(t, 20, history[0].Windows[0].UsedPercent, 1e-8)
	history, err = r.DynamicUsage(ctx, users[1])
	require.NoError(t, err)
	require.Empty(t, history)
	// 真实校准后，小额请求允许使用低于1pp的份额，并附加预占余量。
	tiny, err := r.Reserve(ctx, mid, 8192, 0.000001)
	require.NoError(t, err)
	var held float64
	require.NoError(t, integrationDB.QueryRow(`SELECT reserved_percent FROM pool_dynamic_request_windows WHERE request_id=$1`, tiny).Scan(&held))
	require.Greater(t, held, 0.002)
	require.Less(t, held, 0.01)
}

func TestPoolDynamicCalibrationRetainsZeroDeltaAndPendingMeasurements(t *testing.T) {
	for _, pendingAtSnapshot := range []bool{false, true} {
		name := "settled_zero_delta"
		if pendingAtSnapshot {
			name = "inflight_positive_delta"
		}
		t.Run(name, func(t *testing.T) {
			r, oid, aid, users, w := calibrationFixture(t)
			mid, kid, _ := poolMemberID(t, oid, users[0])
			id, err := r.Reserve(context.Background(), mid, 8192, 0.01)
			require.NoError(t, err)
			if !pendingAtSnapshot {
				settleCredit(t, id, kid, 0.01)
			} else {
				w.UsedPercent = 20
			}
			applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
			_, _, _, external, delta := calibrationLedger(t, oid, mid)
			require.Zero(t, external)
			if pendingAtSnapshot {
				require.InDelta(t, 20, delta, 1e-8)
				settleCredit(t, id, kid, 0.01)
			}
			w.UsedPercent = 20
			applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
			used, _, _, external, delta := calibrationLedger(t, oid, mid)
			require.InDelta(t, 20, used, 1e-8)
			require.Zero(t, external)
			require.Zero(t, delta)
		})
	}
}

func TestPoolDynamicCalibrationExternalConsumptionAndResetCarry(t *testing.T) {
	r, oid, aid, users, w := calibrationFixture(t)
	ctx := context.Background()
	mid, kid, _ := poolMemberID(t, oid, users[0])
	w.UsedPercent = 10
	applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
	_, _, rem, external, _ := calibrationLedger(t, oid, mid)
	require.InDelta(t, 10, external, 1e-8)
	require.InDelta(t, 44, rem, 1e-8)
	id, err := r.Reserve(ctx, mid, 8192, 0.01)
	require.NoError(t, err)
	var oldID int64
	require.NoError(t, integrationDB.QueryRow(`SELECT ledger_id FROM pool_dynamic_request_windows WHERE request_id=$1`, id).Scan(&oldID))
	// 模拟上一个窗口已到重置边界，只有上游新代次能触发迁移。
	_, err = integrationDB.Exec(`UPDATE pool_dynamic_window_ledgers SET reset_at=NOW()-INTERVAL '2 seconds' WHERE id=$1`, oldID)
	require.NoError(t, err)
	w.ResetsAt = time.Now().UTC().Add(5 * time.Hour)
	w.UsedPercent = 0
	observed := time.Now().UTC()
	applyCoreSnapshot(t, r, aid, observed, false, w)
	var newID int64
	require.NoError(t, integrationDB.QueryRow(`SELECT ledger_id FROM pool_dynamic_request_windows WHERE request_id=$1`, id).Scan(&newID))
	require.NotEqual(t, oldID, newID)
	var oldReserved float64
	require.NoError(t, integrationDB.QueryRow(`SELECT reserved_percent FROM pool_dynamic_member_ledgers WHERE ledger_id=$1 AND member_id=$2`, oldID, mid).Scan(&oldReserved))
	require.Zero(t, oldReserved)
	_, reserved, _, external, _ := calibrationLedger(t, oid, mid)
	require.Equal(t, 1.0, reserved)
	require.Zero(t, external)
	// 乱序的旧快照不会再生成窗口或覆盖新一代预占。
	old := w
	old.ResetsAt = observed.Add(-time.Second)
	old.UsedPercent = 90
	applyCoreSnapshot(t, r, aid, observed.Add(-500*time.Millisecond), false, old)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_window_ledgers WHERE order_id=$1`, oid).Scan(&count))
	require.Equal(t, 2, count)
	settleCredit(t, id, kid, 0.01)
	w.UsedPercent = 5
	applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
	used, reserved, _, _, _ := calibrationLedger(t, oid, mid)
	require.InDelta(t, 5, used, 1e-8)
	require.Zero(t, reserved)
}

func TestPoolDynamicEarlyResetChangeCannotRefill(t *testing.T) {
	r, oid, aid, users, w := calibrationFixture(t)
	w.ResetsAt = w.ResetsAt.Add(time.Hour)
	applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_window_ledgers WHERE order_id=$1`, oid).Scan(&count))
	require.Equal(t, 1, count)
	_, err := r.Reserve(context.Background(), coreMember(t, oid, users[0]), 8192, 0.01)
	require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
}

func TestPoolDynamicCalibrationBatchUsesActualCostWeights(t *testing.T) {
	r, oid, aid, users, w := calibrationFixture(t)
	for i, cost := range []float64{0.01, 0.04} {
		mid, kid, _ := poolMemberID(t, oid, users[i])
		id, err := r.Reserve(context.Background(), mid, 8192, 0.01)
		require.NoError(t, err)
		settleCredit(t, id, kid, cost)
	}
	w.UsedPercent = 20
	applyCoreSnapshot(t, r, aid, time.Now().UTC(), false, w)
	a, _, _, _, _ := calibrationLedger(t, oid, coreMember(t, oid, users[0]))
	b, _, _, _, _ := calibrationLedger(t, oid, coreMember(t, oid, users[1]))
	require.InDelta(t, 4, a, 1e-8)
	require.InDelta(t, 16, b, 1e-8)
	var linked int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_request_windows w JOIN pool_requests r ON r.id=w.request_id JOIN pool_members m ON m.id=r.member_id WHERE m.order_id=$1 AND w.calibrated_snapshot_id IS NOT NULL`, oid).Scan(&linked))
	require.Equal(t, 2, linked)
}

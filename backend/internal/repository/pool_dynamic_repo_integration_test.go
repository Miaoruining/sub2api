//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// dynamicCoreFixture creates an active order directly after joining.  The
// delivery path has its own integration coverage; these tests focus on the
// snapshot/ledger/request boundary and therefore do not invoke Deliver.
func dynamicCoreFixture(t *testing.T, mode string) (*poolRepository, int64, int64, []int64) {
	t.Helper()
	ctx := context.Background()
	r := NewPoolRepository(integrationDB).(*poolRepository)
	rid, err := r.CreateResource(ctx, service.PoolResourceCreate{
		Name: "动态 OAuth " + time.Now().Format("150405.000000000"), Type: "oauth",
		Credentials: json.RawMessage(`{"access_token":"dynamic-test-token","plan_type":"plus"}`), Concurrency: 2,
	})
	require.NoError(t, err)
	var aid, gid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id,group_id FROM pool_resources WHERE id=$1`, rid).Scan(&aid, &gid))
	users := make([]int64, 2)
	for i := range users {
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, fmt.Sprintf("dynamic-%d-%d@example.test", aid, i)).Scan(&users[i]))
	}
	p := service.PoolCreate{PoolCreditConfig: service.PoolCreditConfig{QuotaMode: mode, PlanType: "plus"}, Title: "动态核心", Seats: 2, Price: 10, DurationHours: 24, TotalTokens: 0, TotalRequests: 0, Concurrency: 2, JoinDeadline: time.Now().Add(time.Hour)}
	if mode == "dynamic_shadow" {
		p.TotalCredit = 20
		p.Credit5h = 10
		p.Credit7d = 20
		p.TotalTokens = int64(p.Seats) * 8192
		p.TotalRequests = int64(p.Seats)
	}
	oid, err := r.Create(ctx, p)
	require.NoError(t, err)
	for _, uid := range users {
		_, err = r.Join(ctx, oid, uid)
		require.NoError(t, err)
	}
	_, err = integrationDB.Exec(`UPDATE pool_orders SET group_id=$2,resource_id=$3,status='active',starts_at=NOW(),expires_at=NOW()+INTERVAL '1 day' WHERE id=$1`, oid, gid, rid)
	require.NoError(t, err)
	for _, uid := range users {
		var kid int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,$2,$3,$4) RETURNING id`, uid, fmt.Sprintf("sk-dynamic-%d-%d", aid, uid), "动态测试 Key", gid).Scan(&kid))
		_, err = integrationDB.Exec(`UPDATE pool_members SET api_key_id=$1 WHERE order_id=$2 AND user_id=$3`, kid, oid, uid)
		require.NoError(t, err)
	}
	t.Cleanup(func() { _ = aid })
	return r, oid, aid, users
}

func applyCoreSnapshot(t *testing.T, r *poolRepository, aid int64, observed time.Time, blocked bool, windows ...service.PoolDynamicSnapshotWindow) {
	t.Helper()
	err := r.ApplyDynamicSnapshot(context.Background(), service.PoolDynamicSnapshot{AccountID: aid, ObservedAt: observed, Blocked: blocked, Windows: windows})
	require.NoError(t, err)
}

func coreWindow(key string, observed time.Time, used float64) service.PoolDynamicSnapshotWindow {
	seconds := int64(18000)
	if key == "codex/10080" {
		seconds = 7 * 24 * 60 * 60
	}
	return service.PoolDynamicSnapshotWindow{Key: key, UsedPercent: used, ResetsAt: observed.Add(time.Duration(seconds) * time.Second), WindowSeconds: seconds}
}

func coreMember(t *testing.T, oid, uid int64) int64 {
	t.Helper()
	var mid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT id FROM pool_members WHERE order_id=$1 AND user_id=$2`, oid, uid).Scan(&mid))
	return mid
}

func TestPoolDynamicReserveFairAcrossWindowsAndSettlementIdempotent(t *testing.T) {
	r, oid, aid, users := dynamicCoreFixture(t, "dynamic")
	now := time.Now().UTC().Truncate(time.Microsecond)
	applyCoreSnapshot(t, r, aid, now, false, coreWindow("codex/300", now, 0), coreWindow("codex/10080", now, 0))
	ctx := context.Background()
	midA := coreMember(t, oid, users[0])
	midB := coreMember(t, oid, users[1])
	first, err := r.Reserve(ctx, midA, 10000, 1)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, first, true))
	_, kid, _ := poolMemberID(t, oid, users[0])
	cmd := &service.UsageBillingCommand{PoolReservationID: first, APIKeyID: kid, InputTokens: 10, OutputTokens: 20, PoolCreditCost: 2}
	settleCoreRequest(t, cmd)
	settleCoreRequest(t, cmd)
	var used, reserved float64
	require.NoError(t, integrationDB.QueryRow(`SELECT used_percent,reserved_percent FROM pool_dynamic_member_ledgers WHERE member_id=$1 ORDER BY ledger_id LIMIT 1`, midA).Scan(&used, &reserved))
	require.InDelta(t, 20, used, 1e-8)
	require.Zero(t, reserved)
	// A's prior 20pp leaves roughly 29pp while B keeps roughly 49pp; B can
	// use its own fair share without the first snapshot reallocating A's use.
	second, err := r.Reserve(ctx, midB, 10000, 1)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, second, true))
	var aRemaining, bRemaining float64
	require.NoError(t, integrationDB.QueryRow(`SELECT entitlement_percent-used_percent-reserved_percent FROM pool_dynamic_member_ledgers WHERE member_id=$1 ORDER BY ledger_id LIMIT 1`, midA).Scan(&aRemaining))
	require.NoError(t, integrationDB.QueryRow(`SELECT entitlement_percent-used_percent-reserved_percent FROM pool_dynamic_member_ledgers WHERE member_id=$1 ORDER BY ledger_id LIMIT 1`, midB).Scan(&bRemaining))
	require.Greater(t, bRemaining, aRemaining)
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_request_windows WHERE request_id=$1`, first).Scan(&n))
	require.Equal(t, 2, n)
}

func settleCoreRequest(t *testing.T, cmd *service.UsageBillingCommand) {
	t.Helper()
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, settlePoolRequest(ctx, tx, cmd))
	require.NoError(t, tx.Commit())
}

func TestPoolDynamicStaleMissingAndBlockedFailClosed(t *testing.T) {
	r, oid, aid, users := dynamicCoreFixture(t, "dynamic")
	now := time.Now().UTC().Truncate(time.Microsecond)
	ctx := context.Background()
	applyCoreSnapshot(t, r, aid, now, false, coreWindow("codex/300", now, 0), coreWindow("codex/10080", now, 0))
	mid := coreMember(t, oid, users[0])
	_, err := r.Reserve(ctx, mid, 10000, 1)
	require.NoError(t, err)
	// A fresh response omitting a previously observed Pro window is unknown;
	// it must stop new requests instead of silently dropping that constraint.
	next := now.Add(time.Second)
	applyCoreSnapshot(t, r, aid, next, false, coreWindow("codex/300", next, 1))
	_, err = r.Reserve(ctx, mid, 10000, 1)
	require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
	_, err = integrationDB.Exec(`UPDATE pool_dynamic_snapshots SET observed_at=observed_at-INTERVAL '5 minutes' WHERE account_id=$1`, aid)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE pool_dynamic_window_ledgers SET observed_at=observed_at-INTERVAL '5 minutes' WHERE account_id=$1`, aid)
	require.NoError(t, err)
	_, err = r.Reserve(ctx, mid, 10000, 1)
	require.ErrorIs(t, err, service.ErrPoolDynamicUnavailable)
	// A blocked observation is a distinct state; it must not fabricate 100%
	// usage into either member's ledger.
	blockedAt := time.Now().UTC().Truncate(time.Microsecond)
	applyCoreSnapshot(t, r, aid, blockedAt, true, coreWindow("codex/300", blockedAt, 3), coreWindow("codex/10080", blockedAt, 4))
	_, err = r.Reserve(ctx, mid, 10000, 1)
	require.ErrorIs(t, err, service.ErrPoolDynamicBlocked)
	var observed float64
	require.NoError(t, integrationDB.QueryRow(`SELECT observed_used_percent FROM pool_dynamic_window_ledgers WHERE order_id=$1 AND key='codex/300' ORDER BY id DESC LIMIT 1`, oid).Scan(&observed))
	require.InDelta(t, 3, observed, 1e-8)
}

func TestPoolDynamicShadowUsesLegacyCreditGateWithoutSnapshot(t *testing.T) {
	r, oid, _, users := dynamicCoreFixture(t, "dynamic_shadow")
	ctx := context.Background()
	mid := coreMember(t, oid, users[0])
	request, err := r.Reserve(ctx, mid, 10000, 1)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, request, true))
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_dynamic_request_windows WHERE request_id=$1`, request).Scan(&n))
	require.Zero(t, n)
}

func TestPoolDynamicAccountsOnlyIndependentOAuthAndLease(t *testing.T) {
	r, _, aid, _ := dynamicCoreFixture(t, "dynamic")
	ctx := context.Background()
	accounts, err := r.DynamicAccounts(ctx)
	require.NoError(t, err)
	require.Contains(t, accounts, aid)
	claimed, err := r.ClaimDynamicRefresh(ctx, aid)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = r.ClaimDynamicRefresh(ctx, aid)
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestPoolDynamicConcurrentReservationsDoNotOverAllocate(t *testing.T) {
	r, oid, aid, users := dynamicCoreFixture(t, "dynamic")
	now := time.Now().UTC().Truncate(time.Microsecond)
	applyCoreSnapshot(t, r, aid, now, false, coreWindow("codex/300", now, 0), coreWindow("codex/10080", now, 0))
	members := []int64{coreMember(t, oid, users[0]), coreMember(t, oid, users[1])}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successful int
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			memberID := members[i%2]
			if _, err := r.Reserve(context.Background(), memberID, 10000, 1); err == nil {
				mu.Lock()
				successful++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	// Cold-start account concurrency is intentionally one until a sample
	// calibrates the request-to-window ratio.
	require.Equal(t, 1, successful)
	var reserved float64
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(reserved_percent) FROM pool_dynamic_member_ledgers WHERE ledger_id=(SELECT id FROM pool_dynamic_window_ledgers WHERE order_id=$1 ORDER BY id LIMIT 1)`, oid).Scan(&reserved))
	require.LessOrEqual(t, reserved, 49.0)
}

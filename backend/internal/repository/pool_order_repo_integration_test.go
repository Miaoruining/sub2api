//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var fixtureResources sync.Map

func poolFixture(t *testing.T) (*poolRepository, int64, []int64) {
	t.Helper()
	ctx := context.Background()
	r := NewPoolRepository(integrationDB).(*poolRepository)
	rid, err := r.CreateResource(ctx, service.PoolResourceCreate{Name: "测试独立号池 " + time.Now().Format("150405.000000000"), Type: "apikey", Credentials: json.RawMessage(`{"api_key":"test-pool-credential"}`), Concurrency: 3})
	require.NoError(t, err)
	var users []int64
	for i := 0; i < 3; i++ {
		var uid int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, fmt.Sprintf("pool-%d-%d@example.test", rid, i)).Scan(&uid))
		users = append(users, uid)
	}
	id, err := r.Create(ctx, service.PoolCreate{Title: "公平拼单", Seats: 2, Price: 10, DurationHours: 24, TotalTokens: 100000, TotalRequests: 4, Concurrency: 1, JoinDeadline: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	fixtureResources.Store(id, rid)
	t.Cleanup(func() { fixtureResources.Delete(id) })
	return r, id, users
}
func poolResourceID(t *testing.T, oid int64) int64 {
	t.Helper()
	rid, ok := fixtureResources.Load(oid)
	require.True(t, ok)
	return rid.(int64)
}
func poolDeliver(t *testing.T, r *poolRepository, oid int64) {
	t.Helper()
	_, err := r.Deliver(context.Background(), oid, poolResourceID(t, oid))
	require.NoError(t, err)
}
func poolMemberID(t *testing.T, oid, uid int64) (int64, int64, int64) {
	t.Helper()
	var mid, kid, gid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT m.id,m.api_key_id,p.group_id FROM pool_members m JOIN pool_orders p ON p.id=m.order_id WHERE m.order_id=$1 AND m.user_id=$2`, oid, uid).Scan(&mid, &kid, &gid))
	return mid, kid, gid
}
func TestPoolJoinConcurrencyPaymentAndKeyIsolation(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for _, uid := range u {
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(uid int64) { defer wg.Done(); _, _ = r.Join(ctx, id, uid) }(uid)
		}
	}
	wg.Wait()
	poolDeliver(t, r, id)
	var joined, keys, debits int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*),COUNT(api_key_id) FROM pool_members WHERE order_id=$1`, id).Scan(&joined, &keys))
	require.Equal(t, 2, joined)
	require.Equal(t, 2, keys)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_wallet_entries e JOIN pool_members m ON m.id=e.member_id WHERE m.order_id=$1 AND e.amount=-10`, id).Scan(&debits))
	require.Equal(t, 2, debits)
	var uid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT user_id FROM pool_members WHERE order_id=$1 LIMIT 1`, id).Scan(&uid))
	poolDeliver(t, r, id)
	mid, kid, gid := poolMemberID(t, id, uid)
	require.Positive(t, mid)
	gate, err := r.Gate(ctx, kid, uid, gid)
	require.NoError(t, err)
	require.NotNil(t, gate)
	_, err = r.Gate(ctx, kid, uid+999, gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
	_, err = r.Gate(ctx, 999999, uid, gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
	_, err = integrationDB.Exec(`UPDATE api_keys SET auto_group=true WHERE id=$1`, kid)
	require.Error(t, err)
	_, err = integrationDB.Exec(`UPDATE groups SET fallback_group_id=$2 WHERE id=$1`, gid, gid)
	require.Error(t, err)
	// 普通管理列表和全平台调度不包含拼单账号。
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	accounts, _, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 1000})
	require.NoError(t, err)
	var aid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM pool_resources WHERE group_id=$1`, gid).Scan(&aid))
	for _, a := range accounts {
		require.NotEqual(t, aid, a.ID)
	}
	scheduled, err := repo.ListSchedulableByPlatform(ctx, "openai")
	require.NoError(t, err)
	for _, a := range scheduled {
		require.NotEqual(t, aid, a.ID)
	}
	own, err := repo.ListSchedulableByGroupIDAndPlatform(ctx, gid, "openai")
	require.NoError(t, err)
	require.Len(t, own, 1)
	require.Equal(t, aid, own[0].ID)
	var other int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name) VALUES($1) RETURNING id`, fmt.Sprintf("pool-outsider-%d", id)).Scan(&other))
	_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, aid, other)
	require.Error(t, err)
}
func TestPoolReservationSettlementAndRequestCaps(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	_, err := r.Join(ctx, id, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[1])
	require.NoError(t, err)
	poolDeliver(t, r, id)
	mid, kid, gid := poolMemberID(t, id, u[0])
	first, err := r.Reserve(ctx, mid, 20000)
	require.NoError(t, err)
	_, err = r.Reserve(ctx, mid, 20000)
	require.ErrorIs(t, err, service.ErrPoolLimit)
	// 另一位成员拥有独立并发、配额。
	other, _, _ := poolMemberID(t, id, u[1])
	_, err = r.Reserve(ctx, other, 20000)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, first, true))
	gate, err := r.Gate(ctx, kid, u[0], gid)
	require.NoError(t, err)
	require.EqualValues(t, 30000, gate.TokensRemaining)
	// 模拟异步结算晚于请求关闭，退还预占与实际之间的差额，重复结算不重复扣量。
	cmd := &service.UsageBillingCommand{PoolReservationID: first, APIKeyID: kid, InputTokens: 100, OutputTokens: 200, CacheReadTokens: 50}
	for i := 0; i < 2; i++ {
		tx, e := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, e)
		require.NoError(t, settlePoolRequest(ctx, tx, cmd))
		require.NoError(t, tx.Commit())
	}
	gate, err = r.Gate(ctx, kid, u[0], gid)
	require.NoError(t, err)
	require.EqualValues(t, 49650, gate.TokensRemaining)
	_, err = r.Reserve(ctx, mid, 49651)
	require.ErrorIs(t, err, service.ErrPoolLimit)
	second, err := r.Reserve(ctx, mid, 10000)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, second, false))
	_, err = r.Reserve(ctx, mid, 100)
	require.ErrorIs(t, err, service.ErrPoolLimit)
}
func TestPoolRefundIsAtomicAndIdempotent(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	_, err := r.Join(ctx, id, u[0])
	require.NoError(t, err)
	_, err = r.Leave(ctx, id, u[0])
	require.NoError(t, err)
	_, err = r.Leave(ctx, id, u[0])
	require.NoError(t, err)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[0]).Scan(&balance))
	require.Equal(t, 100.0, balance)
	_, err = r.Join(ctx, id, u[1])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[2])
	require.NoError(t, err)
	_, err = r.Leave(ctx, id, u[1])
	require.ErrorIs(t, err, service.ErrPoolClosed)
	poolDeliver(t, r, id)
	_, err = integrationDB.Exec(`UPDATE pool_orders SET starts_at=NOW()-INTERVAL '12 hours',expires_at=NOW()+INTERVAL '12 hours' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = r.Cancel(ctx, id)
	require.NoError(t, err)
	_, err = r.Cancel(ctx, id)
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[1]).Scan(&balance))
	require.InDelta(t, 95, balance, 0.01)
	_, kid, gid := poolMemberID(t, id, u[1])
	_, err = r.Gate(ctx, kid, u[1], gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
}
func TestPoolInsufficientBalanceRollsBackAndExpiryBlocks(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE users SET balance=1 WHERE id=$1`, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[0])
	require.ErrorIs(t, err, service.ErrPoolBalance)
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_members WHERE order_id=$1`, id).Scan(&n))
	require.Zero(t, n)
	_, err = r.Join(ctx, id, u[1])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[2])
	require.NoError(t, err)
	poolDeliver(t, r, id)
	mid, kid, gid := poolMemberID(t, id, u[1])
	_, err = integrationDB.Exec(`UPDATE pool_orders SET expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = r.Gate(ctx, kid, u[1], gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
	_, err = r.Reserve(ctx, mid, 100)
	require.ErrorIs(t, err, service.ErrPoolAccess)
}

func TestPoolResourceReusedOnlyAfterEndingAndOldKeyStaysBlocked(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	_, err := r.Join(ctx, id, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[1])
	require.NoError(t, err)
	poolDeliver(t, r, id)
	_, kid, gid := poolMemberID(t, id, u[0])
	rid := poolResourceID(t, id)
	next, err := r.Create(ctx, service.PoolCreate{Title: "第二车", Seats: 2, Price: 12, DurationHours: 24, TotalTokens: 100000, TotalRequests: 4, Concurrency: 1, JoinDeadline: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	_, err = r.Join(ctx, next, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, next, u[1])
	require.NoError(t, err)
	_, err = r.Deliver(ctx, next, rid)
	require.ErrorIs(t, err, service.ErrPoolClosed)
	_, err = r.Cancel(ctx, id)
	require.NoError(t, err)
	_, err = r.Deliver(ctx, next, rid)
	require.NoError(t, err)
	_, newKey, newGroup := poolMemberID(t, next, u[0])
	require.NotEqual(t, gid, newGroup)
	require.NotEqual(t, kid, newKey)
	_, err = r.Gate(ctx, kid, u[0], gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
	gate, err := r.Gate(ctx, newKey, u[0], newGroup)
	require.NoError(t, err)
	require.EqualValues(t, 50000, gate.TokensRemaining)
}
func TestPoolFormationDeliveryAndNotifications(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	rid := poolResourceID(t, id)
	_, err := r.Deliver(ctx, id, rid)
	require.ErrorIs(t, err, service.ErrPoolClosed)
	_, err = r.Join(ctx, id, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, id, u[1])
	require.NoError(t, err)
	var status string
	var noKeys, noService bool
	var hours float64
	require.NoError(t, integrationDB.QueryRow(`SELECT status,starts_at IS NULL AND expires_at IS NULL AND group_id IS NULL,EXTRACT(EPOCH FROM(delivery_deadline-formed_at))/3600 FROM pool_orders WHERE id=$1`, id).Scan(&status, &noService, &hours))
	require.Equal(t, "awaiting_delivery", status)
	require.True(t, noService)
	require.Equal(t, 24.0, hours)
	require.NoError(t, integrationDB.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM pool_members WHERE order_id=$1 AND api_key_id IS NOT NULL)`, id).Scan(&noKeys))
	require.True(t, noKeys)
	notices, err := r.Notifications(ctx, u[2], true)
	require.NoError(t, err)
	var notice int64
	for _, n := range notices {
		if n.OrderID == id {
			notice = n.ID
			require.Equal(t, "delivery_required", n.Kind)
		}
	}
	require.Positive(t, notice)
	private, err := r.Notifications(ctx, u[2], false)
	require.NoError(t, err)
	require.Empty(t, private)
	require.NoError(t, r.ReadNotification(ctx, notice, u[2], false))
	notices, err = r.Notifications(ctx, u[2], true)
	require.NoError(t, err)
	for _, n := range notices {
		if n.ID == notice {
			require.False(t, n.Read)
		}
	}
	require.NoError(t, r.SetResourceStatus(ctx, rid, "disabled"))
	_, err = r.Deliver(ctx, id, rid)
	require.ErrorIs(t, err, service.ErrPoolConfig)
	require.NoError(t, r.SetResourceStatus(ctx, rid, "active"))
	// 超时仍能发货，服务从实际发货开始，不扣减等待时间。
	_, err = integrationDB.Exec(`UPDATE pool_orders SET formed_at=NOW()-INTERVAL '25 hours',delivery_deadline=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.Deliver(ctx, id, rid); require.NoError(t, e) }()
	}
	wg.Wait()
	var keys int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(api_key_id) FROM pool_members WHERE order_id=$1`, id).Scan(&keys))
	require.Equal(t, 2, keys)
	var fresh bool
	require.NoError(t, integrationDB.QueryRow(`SELECT starts_at>NOW()-INTERVAL '1 minute' AND expires_at-starts_at=INTERVAL '24 hours' FROM pool_orders WHERE id=$1`, id).Scan(&fresh))
	require.True(t, fresh)
	for _, uid := range u[:2] {
		ns, e := r.Notifications(ctx, uid, false)
		require.NoError(t, e)
		require.Len(t, ns, 1)
		require.Equal(t, "delivered", ns[0].Kind)
		require.NoError(t, r.ReadNotification(ctx, ns[0].ID, uid, false))
		ns, e = r.Notifications(ctx, uid, false)
		require.NoError(t, e)
		require.True(t, ns[0].Read)
	}
	notices, err = r.Notifications(ctx, u[2], true)
	require.NoError(t, err)
	for _, n := range notices {
		require.NotEqual(t, id, n.OrderID)
	}
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	accs, _, err := repo.List(context.WithValue(ctx, service.PoolAccountListContextKey{}, true), pagination.PaginationParams{Page: 1, PageSize: 1000})
	require.NoError(t, err)
	require.NotEmpty(t, accs)
	for _, a := range accs {
		var isPool bool
		require.NoError(t, integrationDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pool_resources WHERE account_id=$1)`, a.ID).Scan(&isPool))
		require.True(t, isPool)
	}
}
func TestPoolPendingCancellationRefundsFullAndPreventsDelivery(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	for _, uid := range u[:2] {
		_, err := r.Join(ctx, id, uid)
		require.NoError(t, err)
	}
	_, err := r.Cancel(ctx, id)
	require.NoError(t, err)
	_, err = r.Cancel(ctx, id)
	require.NoError(t, err)
	_, err = r.Deliver(ctx, id, poolResourceID(t, id))
	require.ErrorIs(t, err, service.ErrPoolClosed)
	for _, uid := range u[:2] {
		var balance float64
		require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, uid).Scan(&balance))
		require.Equal(t, 100.0, balance)
	}
}

func TestPoolNativeAccountCreationIsAtomicAndKeepsSettings(t *testing.T) {
	ctx := context.WithValue(context.Background(), service.PoolAccountCreateContextKey{}, true)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	rate := 0.5
	a := &service.Account{Name: "原生拼单 OAuth", Platform: "openai", Type: "oauth", Credentials: map[string]any{"access_token": "test-only"}, Extra: map[string]any{"privacy_mode": "training_disabled"}, Concurrency: 7, Priority: 9, RateMultiplier: &rate, Status: "active", Schedulable: true, AutoPauseOnExpired: true}
	require.NoError(t, repo.Create(ctx, a))
	require.Len(t, a.GroupIDs, 1)
	var rid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT id FROM pool_resources WHERE account_id=$1 AND group_id=$2`, a.ID, a.GroupIDs[0]).Scan(&rid))
	loaded, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, 7, loaded.Concurrency)
	require.Equal(t, 9, loaded.Priority)
	require.Equal(t, rate, *loaded.RateMultiplier)
	require.Equal(t, "training_disabled", loaded.Extra["privacy_mode"])
	require.Error(t, repo.BindGroups(ctx, a.ID, nil))
	require.NoError(t, repo.BindGroups(ctx, a.ID, a.GroupIDs))
	_, err = integrationDB.Exec(`UPDATE accounts SET platform='anthropic' WHERE id=$1`, a.ID)
	require.Error(t, err)
}

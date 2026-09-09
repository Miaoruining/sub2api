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

func poolFixture(t *testing.T) (*poolRepository, int64, []int64) {
	t.Helper()
	ctx := context.Background()
	r := NewPoolRepository(integrationDB).(*poolRepository)
	rid, err := r.CreateResource(ctx, service.PoolResourceCreate{Name: "测试独立号池 " + time.Now().Format("150405.000000000"), Type: "apikey", Credentials: json.RawMessage(`{"api_key":"test-pool-credential"}`), Concurrency: 3})
	require.NoError(t, err)
	var gid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT group_id FROM pool_resources WHERE id=$1`, rid).Scan(&gid))
	var users []int64
	for i := 0; i < 3; i++ {
		var uid int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, fmt.Sprintf("pool-%d-%d@example.test", rid, i)).Scan(&uid))
		users = append(users, uid)
	}
	id, err := r.Create(ctx, service.PoolCreate{Title: "公平拼单", GroupID: gid, Seats: 2, Price: 10, DurationHours: 24, TotalTokens: 100000, TotalRequests: 4, Concurrency: 1, JoinDeadline: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	return r, id, users
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
	var joined, keys, debits int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*),COUNT(api_key_id) FROM pool_members WHERE order_id=$1`, id).Scan(&joined, &keys))
	require.Equal(t, 2, joined)
	require.Equal(t, 2, keys)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_wallet_entries e JOIN pool_members m ON m.id=e.member_id WHERE m.order_id=$1 AND e.amount=-10`, id).Scan(&debits))
	require.Equal(t, 2, debits)
	var uid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT user_id FROM pool_members WHERE order_id=$1 LIMIT 1`, id).Scan(&uid))
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
	_, kid, gid := poolMemberID(t, id, u[0])
	p := service.PoolCreate{Title: "第二车", GroupID: gid, Seats: 2, Price: 12, DurationHours: 24, TotalTokens: 100000, TotalRequests: 4, Concurrency: 1, JoinDeadline: time.Now().Add(time.Hour)}
	_, err = r.Create(ctx, p)
	require.ErrorIs(t, err, service.ErrPoolClosed)
	_, err = r.Cancel(ctx, id)
	require.NoError(t, err)
	next, err := r.Create(ctx, p)
	require.NoError(t, err)
	require.NotEqual(t, id, next)
	var newGroup int64
	require.NoError(t, integrationDB.QueryRow(`SELECT group_id FROM pool_orders WHERE id=$1`, next).Scan(&newGroup))
	require.NotEqual(t, gid, newGroup)
	_, err = r.Gate(ctx, kid, u[0], gid)
	require.ErrorIs(t, err, service.ErrPoolAccess)
	_, err = r.Join(ctx, next, u[0])
	require.NoError(t, err)
	_, err = r.Join(ctx, next, u[1])
	require.NoError(t, err)
	_, newKey, _ := poolMemberID(t, next, u[0])
	require.NotEqual(t, kid, newKey)
	gate, err := r.Gate(ctx, newKey, u[0], newGroup)
	require.NoError(t, err)
	require.EqualValues(t, 50000, gate.TokensRemaining)
}

func TestPoolDisabledResourceRejectsNewPurchaseWithoutDebit(t *testing.T) {
	r, id, u := poolFixture(t)
	ctx := context.Background()
	var rid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT r.id FROM pool_resources r JOIN pool_orders p ON p.group_id=r.group_id WHERE p.id=$1`, id).Scan(&rid))
	require.NoError(t, r.SetResourceStatus(ctx, rid, "disabled"))
	_, err := r.Join(ctx, id, u[0])
	require.ErrorIs(t, err, service.ErrPoolClosed)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[0]).Scan(&balance))
	require.Equal(t, 100.0, balance)
}

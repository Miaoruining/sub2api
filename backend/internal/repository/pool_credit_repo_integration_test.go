//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func creditFixture(t *testing.T, plan string) (*poolRepository, int64, []int64) {
	t.Helper()
	ctx := context.Background()
	r, old, u := poolFixture(t)
	p := service.PoolProduct{PoolCreditConfig: service.PoolCreditConfig{QuotaMode: "credits", PlanType: plan, TotalCredit: 20}, Title: "额度团", Seats: 2, Price: 10, DurationDays: 30, FormationDays: 2, Concurrency: 2, Status: "active"}
	if plan == "plus" {
		p.Credit5h = 2
		p.Credit7d = 6
	}
	pid, e := r.SaveProduct(ctx, 0, p)
	require.NoError(t, e)
	oid, e := r.PurchaseProduct(ctx, pid, u[0], uuid.NewString(), 1)
	require.NoError(t, e)
	_, e = r.Join(ctx, oid, u[1])
	require.NoError(t, e)
	_, e = r.Deliver(ctx, oid, poolResourceID(t, old))
	require.NoError(t, e)
	return r, oid, u
}
func settleCredit(t *testing.T, id string, kid int64, cost float64) {
	t.Helper()
	ctx := context.Background()
	tx, e := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	require.NoError(t, settlePoolRequest(ctx, tx, &service.UsageBillingCommand{PoolReservationID: id, APIKeyID: kid, InputTokens: 10, OutputTokens: 20, PoolCreditCost: cost}))
	require.NoError(t, tx.Commit())
}
func TestPoolCreditPlusWindowsAndMemberIsolation(t *testing.T) {
	r, oid, u := creditFixture(t, "plus")
	ctx := context.Background()
	mid, kid, gid := poolMemberID(t, oid, u[0])
	other, _, _ := poolMemberID(t, oid, u[1])
	g, e := r.Gate(ctx, kid, u[0], gid)
	require.NoError(t, e)
	require.Equal(t, "credits", g.QuotaMode)
	first, e := r.Reserve(ctx, mid, 900000, 0.8)
	require.NoError(t, e) // Token count no longer restricts credits.
	_, e = r.Reserve(ctx, mid, 8192, 0.3)
	require.ErrorIs(t, e, service.ErrPoolLimit)
	_, e = r.Reserve(ctx, other, 8192, 0.8)
	require.NoError(t, e)
	require.NoError(t, r.Finish(ctx, first, false))
	settleCredit(t, first, kid, 0.6)
	settleCredit(t, first, kid, 0.6)
	var m service.PoolMember
	require.NoError(t, loadPoolCreditUsage(ctx, integrationDB, mid, &m))
	require.InDelta(t, 0.6, m.CreditUsed, 1e-8)
	require.Zero(t, m.ReservedCredit)
	// Align both windows to upstream; moving a request out of the short window must not clear weekly use.
	_, e = integrationDB.Exec(`UPDATE accounts SET extra=jsonb_build_object('codex_5h_reset_at',to_char(NOW()+INTERVAL '5 hours','YYYY-MM-DD"T"HH24:MI:SS"Z"'),'codex_7d_reset_at',to_char(NOW()+INTERVAL '6 days','YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id=(SELECT account_id FROM pool_resources WHERE id=(SELECT resource_id FROM pool_orders WHERE id=$1))`, oid)
	require.NoError(t, e)
	_, e = integrationDB.Exec(`UPDATE pool_requests SET created_at=NOW()-INTERVAL '6 hours' WHERE id=$1`, first)
	require.NoError(t, e)
	id, e := r.Reserve(ctx, mid, 8192, 0.9)
	require.NoError(t, e)
	settleCredit(t, id, kid, 2.5) // Accepted OAuth single-request overrun.
	_, e = r.Reserve(ctx, mid, 8192, 0.01)
	require.ErrorIs(t, e, service.ErrPoolLimit)
	_, e = integrationDB.Exec(`UPDATE pool_requests SET created_at=NOW()-INTERVAL '6 hours' WHERE id=$1`, id)
	require.NoError(t, e)
	_, e = r.Reserve(ctx, mid, 8192, 0.01)
	require.ErrorIs(t, e, service.ErrPoolLimit) // Weekly 3.1 / 3.
	_, e = integrationDB.Exec(`UPDATE pool_requests SET created_at=NOW()-INTERVAL '8 days' WHERE member_id=$1`, mid)
	require.NoError(t, e)
	_, e = r.Reserve(ctx, mid, 8192, 0.9)
	require.NoError(t, e)
}
func TestPoolCreditProSkipsWindowsButEnforcesTotalAndConcurrency(t *testing.T) {
	r, oid, u := creditFixture(t, "pro")
	ctx := context.Background()
	mid, kid, _ := poolMemberID(t, oid, u[0])
	// Even an upstream snapshot cannot turn on a local Plus window for Pro.
	_, e := integrationDB.Exec(`UPDATE accounts SET extra=jsonb_build_object('codex_5h_used_percent',100,'codex_5h_reset_at',to_char(NOW()+INTERVAL '1 hour','YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id=(SELECT account_id FROM pool_resources WHERE id=(SELECT resource_id FROM pool_orders WHERE id=$1))`, oid)
	require.NoError(t, e)
	var wg sync.WaitGroup
	results := make(chan string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, e := r.Reserve(ctx, mid, 8192, 3)
			if e == nil {
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)
	var ids []string
	for id := range results {
		ids = append(ids, id)
	}
	require.Len(t, ids, 2)
	for _, id := range ids {
		settleCredit(t, id, kid, 3)
	}
	_, e = r.Reserve(ctx, mid, 8192, 4.01)
	require.ErrorIs(t, e, service.ErrPoolLimit)
	id, e := r.Reserve(ctx, mid, 8192, 4)
	require.NoError(t, e)
	settleCredit(t, id, kid, 4)
	_, e = r.Reserve(ctx, mid, 8192, 0.01)
	require.ErrorIs(t, e, service.ErrPoolLimit)
	var m service.PoolMember
	require.NoError(t, loadPoolCreditUsage(ctx, integrationDB, mid, &m))
	require.Equal(t, 10.0, m.CreditUsed)
	require.Nil(t, m.Reset5h)
	require.Nil(t, m.Reset7d)
}
func TestPoolCreditUsageRecordsAndStatsAreScoped(t *testing.T) {
	r, oid, u := creditFixture(t, "pro")
	ctx := context.Background()
	mid, kid, gid := poolMemberID(t, oid, u[0])
	id, e := r.Reserve(ctx, mid, 8192, 1)
	require.NoError(t, e)
	settleCredit(t, id, kid, 0.3)
	var aid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM pool_resources WHERE group_id=$1`, gid).Scan(&aid))
	var logID int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,request_id,model,input_tokens,output_tokens,total_cost,actual_cost,billing_type) VALUES($1,$2,$3,$4,$5,'test-model',10,20,0.3,0.3,1) RETURNING id`, u[0], kid, aid, gid, uuid.NewString()).Scan(&logID))
	repo := NewUsageLogRepository(integrationEntClient, integrationDB).(*usageLogRepository)
	logs, page, e := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, usagestats.UsageLogFilters{UserID: u[0], UsageSource: "pool"})
	require.NoError(t, e)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, &oid, logs[0].PoolOrderID)
	log, e := repo.GetByID(ctx, logID)
	require.NoError(t, e)
	require.Equal(t, &oid, log.PoolOrderID)
	filter := usagestats.UsageLogFilters{UserID: u[0], UsageSource: "pool"}
	stats, e := repo.GetStatsWithFilters(ctx, filter)
	require.NoError(t, e)
	require.InDelta(t, 0.3, stats.TotalActualCost, 1e-8)
	filter.UsageSource = "standard"
	stats, e = repo.GetStatsWithFilters(ctx, filter)
	require.NoError(t, e)
	require.Zero(t, stats.TotalRequests)
	filter.UsageSource = "pool"
	filter.UserID = u[1]
	logs, page, e = repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, filter)
	require.NoError(t, e)
	require.Empty(t, logs)
	require.Zero(t, page.Total)
	filter.UserID = u[0]
	start := time.Now().Add(-time.Hour)
	end := time.Now().Add(time.Hour)
	trend, e := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", filter)
	require.NoError(t, e)
	require.Len(t, trend, 1)
	models, e := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filter, usagestats.ModelSourceRequested)
	require.NoError(t, e)
	require.Len(t, models, 1)
	groups, e := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filter)
	require.NoError(t, e)
	require.Len(t, groups, 1)
}

func TestPoolCreditBillingAtomicWalletAndHolds(t *testing.T) {
	r, oid, u := creditFixture(t, "pro")
	ctx := context.Background()
	mid, kid, gid := poolMemberID(t, oid, u[0])
	id, e := r.Reserve(ctx, mid, 8192, 0.4)
	require.NoError(t, e)
	require.NoError(t, r.Finish(ctx, id, false))
	holds, e := r.CreditHolds(ctx, u[0])
	require.NoError(t, e)
	require.Len(t, holds, 1)
	require.Equal(t, "uncertain", holds[0].Status)
	outsider, e := r.CreditHolds(ctx, u[1])
	require.NoError(t, e)
	require.Empty(t, outsider)
	var sub, aid int64
	require.NoError(t, integrationDB.QueryRow(`SELECT id FROM user_subscriptions WHERE user_id=$1 AND group_id=$2`, u[0], gid).Scan(&sub))
	require.NoError(t, integrationDB.QueryRow(`SELECT account_id FROM pool_resources WHERE group_id=$1`, gid).Scan(&aid))
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), PoolReservationID: id, APIKeyID: kid, UserID: u[0], AccountID: aid, AccountType: "apikey", SubscriptionID: &sub, SubscriptionCost: 0.3, PoolCreditCost: 0.3, InputTokens: 10, OutputTokens: 20}
	billing := NewUsageBillingRepository(integrationEntClient, integrationDB)
	applied, e := billing.Apply(ctx, cmd)
	require.NoError(t, e)
	require.True(t, applied.Applied)
	applied, e = billing.Apply(ctx, cmd)
	require.NoError(t, e)
	require.False(t, applied.Applied)
	holds, e = r.CreditHolds(ctx, u[0])
	require.NoError(t, e)
	require.Empty(t, holds)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[0]).Scan(&balance))
	require.Equal(t, 90.0, balance)
	var m service.PoolMember
	require.NoError(t, loadPoolCreditUsage(ctx, integrationDB, mid, &m))
	require.Equal(t, 0.3, m.CreditUsed)
	_, e = integrationDB.Exec(`UPDATE pool_orders SET total_credit=2 WHERE id=$1`, oid)
	require.NoError(t, e)
	_, e = r.Reserve(ctx, mid, 8192, 0.7)
	require.NoError(t, e)
	_, e = r.Reserve(ctx, mid, 8192, 0.00000001)
	require.ErrorIs(t, e, service.ErrPoolLimit)
}

func TestPoolCreditDeliveryRejectsKnownPlanMismatch(t *testing.T) {
	r, old, u := poolFixture(t)
	ctx := context.Background()
	rid := poolResourceID(t, old)
	p := service.PoolProduct{PoolCreditConfig: service.PoolCreditConfig{QuotaMode: "credits", PlanType: "pro", TotalCredit: 20}, Title: "Pro 额度团", Seats: 2, Price: 10, DurationDays: 30, FormationDays: 2, Concurrency: 1, Status: "active"}
	pid, e := r.SaveProduct(ctx, 0, p)
	require.NoError(t, e)
	oid, e := r.PurchaseProduct(ctx, pid, u[0], uuid.NewString(), 1)
	require.NoError(t, e)
	_, e = r.Join(ctx, oid, u[1])
	require.NoError(t, e)
	_, e = integrationDB.Exec(`UPDATE accounts SET credentials=credentials||'{"plan_type":"plus"}'::jsonb WHERE id=(SELECT account_id FROM pool_resources WHERE id=$1)`, rid)
	require.NoError(t, e)
	_, e = r.Deliver(ctx, oid, rid)
	require.ErrorIs(t, e, service.ErrPoolPlan)
	var pending bool
	require.NoError(t, integrationDB.QueryRow(`SELECT status='awaiting_delivery' AND group_id IS NULL FROM pool_orders WHERE id=$1`, oid).Scan(&pending))
	require.True(t, pending)
	_, e = integrationDB.Exec(`UPDATE accounts SET credentials=credentials||'{"plan_type":"pro"}'::jsonb WHERE id=(SELECT account_id FROM pool_resources WHERE id=$1)`, rid)
	require.NoError(t, e)
	_, e = r.Deliver(ctx, oid, rid)
	require.NoError(t, e)
}

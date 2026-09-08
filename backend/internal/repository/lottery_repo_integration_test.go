//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func lotteryFixture(t *testing.T, n int) (*lotteryRepository, []int64, string) {
	t.Helper()
	ctx := context.Background()
	date := "2038-01-02"
	now := time.Date(2038, 1, 2, 2, 0, 0, 0, time.UTC)
	r := NewLotteryRepository(integrationDB).(*lotteryRepository)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return now, nil }
	r.ticket = func() (int, error) { return 9552, nil } // 1 额度。
	_, err := integrationDB.ExecContext(ctx, `UPDATE lottery_settings SET enabled=true WHERE id=1`)
	require.NoError(t, err)
	rows, err := integrationDB.QueryContext(ctx, `INSERT INTO users(email,password_hash,balance)
SELECT $1 || '-' || n || '@example.test','test-hash',3.125 FROM generate_series(1,$2) n RETURNING id`, fmt.Sprintf("lottery-%d", time.Now().UnixNano()), n)
	require.NoError(t, err)
	uids := []int64{}
	for rows.Next() {
		var uid int64
		require.NoError(t, rows.Scan(&uid))
		uids = append(uids, uid)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	t.Cleanup(func() {
		// 仅清理本测试拥有的记录；不操作业务数据库。
		for _, q := range []string{
			`DELETE FROM lottery_config_audits WHERE actor_id=ANY($1)`,
			`DELETE FROM lottery_rules WHERE updated_by=ANY($1)`,
			`DELETE FROM lottery_draws WHERE user_id=ANY($1)`,
			`DELETE FROM payment_orders WHERE user_id=ANY($1)`,
			`DELETE FROM users WHERE id=ANY($1)`,
		} {
			_, e := integrationDB.ExecContext(ctx, q, pq.Array(uids))
			require.NoError(t, e)
		}
		_, e := integrationDB.ExecContext(ctx, `DELETE FROM lottery_days WHERE activity_date IN ('2038-01-02','2038-01-03')`)
		require.NoError(t, e)
		_, e = integrationDB.ExecContext(ctx, `UPDATE lottery_settings SET enabled=false WHERE id=1`)
		require.NoError(t, e)
	})
	return r, uids, date
}

func lotteryPaid(t *testing.T, uids []int64) {
	t.Helper()
	_, err := integrationDB.Exec(`INSERT INTO payment_orders(user_id,amount,pay_amount,payment_type,status,payment_trade_no,paid_at,completed_at,expires_at,out_trade_no,provider_snapshot)
SELECT id,1.4,10,'alipay','COMPLETED','test-payment-' || id,NOW(),NOW(),NOW(),'lottery-order-' || id,'{"currency":"CNY"}'::jsonb FROM users WHERE id=ANY($1)`, pq.Array(uids))
	require.NoError(t, err)
}

func TestLotteryEligibilityCountsOnlyNetActualCNYPayments(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	uid := uids[0]
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE users SET balance=10000 WHERE id=$1`, uid)
	require.NoError(t, err)
	status, err := r.Status(ctx, uid)
	require.NoError(t, err)
	require.False(t, status.Eligible)
	_, err = r.Draw(ctx, uid, date)
	require.ErrorIs(t, err, service.ErrLotteryIneligible)
	lotteryPaid(t, uids)
	// 人民币实付 10 元，即使到账仅 1.4 额度，也符合条件。
	status, err = r.Status(ctx, uid)
	require.NoError(t, err)
	require.True(t, status.Eligible)
	for _, mutation := range []string{
		`pay_amount=9.99`, `status='REFUNDED'`, `status='REFUND_REQUESTED'`,
		`status='PARTIALLY_REFUNDED',refund_amount=0.14`,
		`order_type='subscription'`, `paid_at=NULL`, `payment_trade_no=''`,
		`provider_snapshot='{"currency":"USD"}'::jsonb`,
		`provider_snapshot=NULL,payment_type='stripe'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			_, err := integrationDB.Exec(`UPDATE payment_orders SET pay_amount=10,status='COMPLETED',refund_amount=0,order_type='balance',paid_at=NOW(),payment_trade_no='test',payment_type='alipay',provider_snapshot='{"currency":"CNY"}'::jsonb WHERE user_id=$1`, uid)
			require.NoError(t, err)
			_, err = integrationDB.Exec(`UPDATE payment_orders SET `+mutation+` WHERE user_id=$1`, uid)
			require.NoError(t, err)
			s, err := r.Status(ctx, uid)
			require.NoError(t, err)
			require.False(t, s.Eligible)
		})
	}
	_, err = integrationDB.Exec(`UPDATE payment_orders SET pay_amount=20,status='PARTIALLY_REFUNDED',refund_amount=0.7,provider_snapshot=NULL,payment_type='wxpay' WHERE user_id=$1`, uid)
	require.NoError(t, err)
	status, err = r.Status(ctx, uid)
	require.NoError(t, err)
	require.True(t, status.Eligible)
}

func TestLotteryConcurrentSameUserExactlyOnceAndReplay(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	lotteryPaid(t, uids)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan *service.LotteryDraw, 40)
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d, e := r.Draw(ctx, uids[0], date); results <- d; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var id int64
	for d := range results {
		if id == 0 {
			id = d.ID
		}
		require.Equal(t, id, d.ID)
		require.Equal(t, 1, d.Prize)
	}
	var balance float64
	var spent, count int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, uids[0]).Scan(&balance))
	require.Equal(t, 4.125, balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT spent,draw_count FROM lottery_days WHERE activity_date=$1`, date).Scan(&spent, &count))
	require.Equal(t, 1, spent)
	require.Equal(t, 1, count)
	_, err := integrationDB.Exec(`UPDATE lottery_settings SET enabled=false WHERE id=1`)
	require.NoError(t, err)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 3, 2, 0, 0, 0, time.UTC), nil
	}
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, id, d.ID)
}

func TestLotteryThousandConcurrentUsersCannotExceed100(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1000)
	lotteryPaid(t, uids)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	sem := make(chan struct{}, 24)
	for _, uid := range uids {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_, err := r.Draw(ctx, uid, date)
			errs <- err
		}(uid)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, service.ErrLotteryClosed)
		}
	}
	require.Equal(t, 100, success)
	var spent, count, total int
	require.NoError(t, integrationDB.QueryRow(`SELECT spent,draw_count FROM lottery_days WHERE activity_date=$1`, date).Scan(&spent, &count))
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(prize) FROM lottery_draws WHERE activity_date=$1`, date).Scan(&total))
	require.Equal(t, 100, spent)
	require.Equal(t, 100, count)
	require.Equal(t, 100, total)
	var balanceIncrease float64
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(balance-3.125) FROM users WHERE id=ANY($1)`, pq.Array(uids)).Scan(&balanceIncrease))
	require.Equal(t, 100.0, balanceIncrease)
}

func TestLotteryBudgetOverflowBecomesNoPrizeAndZeroPrizeConsumesAttempt(t *testing.T) {
	r, uids, date := lotteryFixture(t, 2)
	lotteryPaid(t, uids)
	ctx := context.Background()
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, 1, d.Prize)
	r.ticket = func() (int, error) { return 9999, nil }
	d, err = r.Draw(ctx, uids[1], date)
	require.NoError(t, err)
	require.Zero(t, d.Prize)
	r.ticket = func() (int, error) { return 9552, nil }
	replay, err := r.Draw(ctx, uids[1], date)
	require.NoError(t, err)
	require.Equal(t, d.ID, replay.ID)
	require.Zero(t, replay.Prize)
	s, err := r.Status(ctx, uids[1])
	require.NoError(t, err)
	require.Equal(t, "drawn", s.State)
	require.Len(t, s.History, 1)
	a, err := r.AdminStatus(ctx, date)
	require.NoError(t, err)
	require.Equal(t, 1, a.Spent)
	require.Equal(t, 2, a.DrawCount)
}

func TestLotteryTimeGatesAndRandomFailureRollback(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	lotteryPaid(t, uids)
	ctx := context.Background()
	for _, now := range []time.Time{time.Date(2038, 1, 2, 1, 59, 59, 0, time.UTC), time.Date(2038, 1, 2, 16, 0, 0, 0, time.UTC)} {
		r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return now, nil }
		_, err := r.Draw(ctx, uids[0], date)
		require.ErrorIs(t, err, service.ErrLotteryClosed)
	}
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 2, 2, 0, 0, 0, time.UTC), nil
	}
	r.ticket = func() (int, error) { return 0, errors.New("entropy unavailable") }
	_, err := r.Draw(ctx, uids[0], date)
	require.ErrorContains(t, err, "entropy unavailable")
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_draws WHERE user_id=$1`, uids[0]).Scan(&count))
	require.Zero(t, count)
	r.ticket = func() (int, error) { return 9552, nil }
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, 1, d.Prize)
}

func TestLotteryLedgerFailureRollsBackBalanceAndBudget(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	lotteryPaid(t, uids)
	ctx := context.Background()
	// 使用仅拒绝本测试用户的数据库约束，模拟更新余额后的流水写入失败。
	constraint := fmt.Sprintf("lottery_test_reject_%d", uids[0])
	_, err := integrationDB.Exec(fmt.Sprintf(`ALTER TABLE lottery_draws ADD CONSTRAINT %s CHECK(user_id <> %d)`, constraint, uids[0]))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, e := integrationDB.Exec(`ALTER TABLE lottery_draws DROP CONSTRAINT ` + constraint)
		require.NoError(t, e)
	})
	_, err = r.Draw(ctx, uids[0], date)
	require.Error(t, err)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, uids[0]).Scan(&balance))
	require.Equal(t, 3.125, balance)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_days WHERE activity_date=$1`, date).Scan(&count))
	require.Zero(t, count)
}

func TestLotteryAdminNextDayRulesAndAudit(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	lotteryPaid(t, uids)
	ctx := context.Background()
	weights := []int{10000, 0, 0, 0, 0, 0, 0}
	require.NoError(t, r.UpdateConfig(ctx, uids[0], service.LotteryConfigUpdate{Weights: weights}))
	a, err := r.AdminStatus(ctx, date)
	require.NoError(t, err)
	require.Equal(t, 9552, a.Weights[0])
	require.Equal(t, weights, a.NextWeights)
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, 1, d.Prize)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 3, 2, 0, 0, 0, time.UTC), nil
	}
	d, err = r.Draw(ctx, uids[0], "2038-01-03")
	require.NoError(t, err)
	require.Zero(t, d.Prize)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_config_audits WHERE actor_id=$1`, uids[0]).Scan(&count))
	require.Equal(t, 1, count)
}

//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
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
	r.ticket = lotteryOneTicket // 两套概率都固定抽中 1 额度。
	_, err := integrationDB.ExecContext(ctx, `UPDATE lottery_settings SET enabled=true,admin_repeat_enabled=true WHERE id=1`)
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
		_, e = integrationDB.ExecContext(ctx, `UPDATE lottery_settings SET enabled=false,admin_repeat_enabled=true WHERE id=1`)
		require.NoError(t, e)
	})
	return r, uids, date
}

func lotteryOneTicket(scale int) (int, error) {
	if scale == service.LotteryIntroTicketCount {
		return 29900, nil
	}
	return 9552, nil
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
	r.ticket = func(scale int) (int, error) { return scale - 1, nil }
	d, err = r.Draw(ctx, uids[1], date)
	require.NoError(t, err)
	require.Zero(t, d.Prize)
	r.ticket = lotteryOneTicket
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
	for _, now := range []time.Time{time.Date(2038, 1, 2, 0, 59, 59, 0, time.UTC), time.Date(2038, 1, 2, 16, 0, 0, 0, time.UTC)} {
		r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return now, nil }
		_, err := r.Draw(ctx, uids[0], date)
		require.ErrorIs(t, err, service.ErrLotteryClosed)
	}
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 2, 2, 0, 0, 0, time.UTC), nil
	}
	r.ticket = func(int) (int, error) { return 0, errors.New("entropy unavailable") }
	_, err := r.Draw(ctx, uids[0], date)
	require.ErrorContains(t, err, "entropy unavailable")
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_draws WHERE user_id=$1`, uids[0]).Scan(&count))
	require.Zero(t, count)
	r.ticket = lotteryOneTicket
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, 1, d.Prize)
}

func TestLotteryMakeupWindowAndOneDailyAttempt(t *testing.T) {
	date := "2026-09-09"
	// Fixture cleanup removes owned draws first, then this disposable day's summary.
	t.Cleanup(func() {
		_, err := integrationDB.Exec(`DELETE FROM lottery_days WHERE activity_date=$1`, date)
		require.NoError(t, err)
	})
	r, uids, _ := lotteryFixture(t, 2)
	lotteryPaid(t, uids)
	ctx := context.Background()
	now := time.Date(2026, 9, 9, 5, 59, 59, 0, time.UTC)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return now, nil }
	s, err := r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.Equal(t, "not_open", s.State)
	require.Equal(t, "2026-09-09T14:00:00+08:00", s.OpensAt.Format(time.RFC3339))
	require.Equal(t, "2026-09-09T16:00:00+08:00", s.ClosesAt.Format(time.RFC3339))
	require.Equal(t, "2026-09-10T09:00:00+08:00", s.NextOpensAt.Format(time.RFC3339))
	_, err = r.Draw(ctx, uids[0], date)
	require.ErrorIs(t, err, service.ErrLotteryClosed)
	now = now.Add(time.Second)
	d, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	replay, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, d.ID, replay.ID)
	now = now.Add(2 * time.Hour)
	s, err = r.Status(ctx, uids[1])
	require.NoError(t, err)
	require.Equal(t, "ended", s.State)
	_, err = r.Draw(ctx, uids[1], date)
	require.ErrorIs(t, err, service.ErrLotteryClosed)
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
	require.Equal(t, 1, d.Prize) // 前三次独立规则不受次日普通概率影响。
	_, err = integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	d, err = r.Draw(ctx, uids[0], "2038-01-03", uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, 1, d.Prize)
	d, err = r.Draw(ctx, uids[0], "2038-01-03", uuid.NewString())
	require.NoError(t, err)
	require.Zero(t, d.Prize)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_config_audits WHERE actor_id=$1`, uids[0]).Scan(&count))
	require.Equal(t, 1, count)
}

func TestLotteryAdminRepeatBypassesPublicGatesButKeepsSharedBudget(t *testing.T) {
	r, uids, date := lotteryFixture(t, 2)
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE lottery_settings SET enabled=false WHERE id=1`)
	require.NoError(t, err)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 2, 0, 0, 0, 0, time.UTC), nil
	}
	s, err := r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.True(t, s.AdminRepeat)
	require.True(t, s.Eligible)
	require.Equal(t, "ready", s.State)
	_, err = r.Draw(ctx, uids[0], date)
	require.ErrorIs(t, err, service.ErrLotteryRequest)
	_, err = r.Draw(ctx, uids[0], date, "bad-key")
	require.ErrorIs(t, err, service.ErrLotteryRequest)
	_, err = r.Draw(ctx, uids[1], date, uuid.NewString())
	require.ErrorIs(t, err, service.ErrLotteryClosed)
	// 多次无奖也产生不同流水，中奖走真实余额和相同预算。
	r.ticket = func(int) (int, error) { return 0, nil }
	d1, err := r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.Zero(t, d1.Prize)
	d2, err := r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.NotEqual(t, d1.ID, d2.ID)
	s, err = r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.Equal(t, d2.ID, s.Today.ID)
	require.Equal(t, "ready", s.State)
	// 普通用户先获 1 额度后，管理员抽中 100 必须转无奖。
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 2, 2, 0, 0, 0, time.UTC), nil
	}
	_, err = integrationDB.Exec(`UPDATE lottery_settings SET enabled=true WHERE id=1`)
	require.NoError(t, err)
	lotteryPaid(t, []int64{uids[1]})
	r.ticket = lotteryOneTicket
	_, err = r.Draw(ctx, uids[1], date)
	require.NoError(t, err)
	r.ticket = func(scale int) (int, error) { return scale - 1, nil }
	d3, err := r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.Zero(t, d3.Prize)
	// 再让管理员 120 个不同请求并发，最多只能发放剩余 99 额度。
	r.ticket = lotteryOneTicket
	var wg sync.WaitGroup
	errs := make(chan error, 120)
	sem := make(chan struct{}, 24)
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_, e := r.Draw(ctx, uids[0], date, uuid.NewString())
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			require.ErrorIs(t, e, service.ErrLotteryClosed)
		}
	}
	require.Equal(t, 99, success)
	a, err := r.AdminStatus(ctx, date)
	require.NoError(t, err)
	require.Equal(t, 100, a.Spent)
	require.Equal(t, 103, a.DrawCount)
	s, err = r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.Equal(t, "ended", s.State)
	var increase, total float64
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(balance-3.125) FROM users WHERE id=ANY($1)`, pq.Array(uids)).Scan(&increase))
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(prize) FROM lottery_draws WHERE activity_date=$1`, date).Scan(&total))
	require.Equal(t, 100.0, increase)
	require.Equal(t, increase, total)
}

func TestLotteryAdminRepeatRequestReplayAndRevocation(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	key := uuid.NewString()
	var wg sync.WaitGroup
	results := make(chan *service.LotteryDraw, 40)
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d, e := r.Draw(ctx, uids[0], date, key); results <- d; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var first int64
	for d := range results {
		if first == 0 {
			first = d.ID
		}
		require.Equal(t, first, d.ID)
	}
	second, err := r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.NotEqual(t, first, second.ID)
	d, err := r.Draw(ctx, uids[0], date, key)
	require.NoError(t, err)
	require.Equal(t, first, d.ID)
	off := false
	require.NoError(t, r.UpdateConfig(ctx, uids[0], service.LotteryConfigUpdate{AdminRepeatEnabled: &off}))
	a, err := r.AdminStatus(ctx, date)
	require.NoError(t, err)
	require.False(t, a.AdminRepeatEnabled)
	require.True(t, a.Enabled)
	s, err := r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.False(t, s.AdminRepeat)
	require.Equal(t, "drawn", s.State)
	// 关闭开关不能再发奖，同一天已参与按普通规则返回最后结果。
	d, err = r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, second.ID, d.ID)
	on := true
	require.NoError(t, r.UpdateConfig(ctx, uids[0], service.LotteryConfigUpdate{AdminRepeatEnabled: &on}))
	_, err = integrationDB.Exec(`UPDATE users SET role='user' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	s, err = r.Status(ctx, uids[0])
	require.NoError(t, err)
	require.False(t, s.AdminRepeat)
	d, err = r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, second.ID, d.ID)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 3, 0, 0, 0, 0, time.UTC), nil
	}
	d, err = r.Draw(ctx, uids[0], date, key)
	require.NoError(t, err)
	require.Equal(t, first, d.ID)
	_, err = r.Draw(ctx, uids[0], date, uuid.NewString())
	require.NoError(t, err) // 普通模式旧日回放，不新增。
	_, err = integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	_, err = r.Draw(ctx, uids[0], date, uuid.NewString())
	require.ErrorIs(t, err, service.ErrLotteryClosed)
	var spent, count, audits int
	require.NoError(t, integrationDB.QueryRow(`SELECT spent,draw_count FROM lottery_days WHERE activity_date=$1`, date).Scan(&spent, &count))
	require.Equal(t, 2, spent)
	require.Equal(t, 2, count)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_config_audits WHERE actor_id=$1 AND before_config ? 'admin_repeat_enabled' AND after_config ? 'admin_repeat_enabled'`, uids[0]).Scan(&audits))
	require.Equal(t, 2, audits)
}

func TestLotteryElevenOClockCutoffAndAdministratorException(t *testing.T) {
	for _, test := range []struct {
		hour, minute, second int
		state                string
	}{
		{0, 59, 59, "not_open"}, {1, 0, 0, "ready"}, {2, 0, 0, "ready"}, {2, 59, 59, "ready"},
		{3, 0, 0, "ended"}, {3, 0, 1, "ended"}, {15, 59, 59, "ended"},
	} {
		t.Run(fmt.Sprintf("UTC-%02d:%02d:%02d", test.hour, test.minute, test.second), func(t *testing.T) {
			r, uids, date := lotteryFixture(t, 2)
			lotteryPaid(t, []int64{uids[0]})
			ctx := context.Background()
			_, err := integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[1])
			require.NoError(t, err)
			now := time.Date(2038, 1, 2, test.hour, test.minute, test.second, 0, time.UTC)
			r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return now, nil }
			s, err := r.Status(ctx, uids[0])
			require.NoError(t, err)
			require.Equal(t, test.state, s.State)
			require.Equal(t, time.Date(2038, 1, 2, 3, 0, 0, 0, time.UTC), s.ClosesAt.UTC())
			_, err = r.Draw(ctx, uids[0], date)
			if test.state == "ready" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrLotteryClosed)
			}
			a, err := r.Status(ctx, uids[1])
			require.NoError(t, err)
			require.Equal(t, "ready", a.State)
			_, err = r.Draw(ctx, uids[1], date, uuid.NewString())
			require.NoError(t, err)
		})
	}
}

func TestLotteryCutoffRechecksTimeBeforeCreditAndAllowsReplay(t *testing.T) {
	r, uids, date := lotteryFixture(t, 2)
	lotteryPaid(t, uids)
	ctx := context.Background()
	inside := time.Date(2038, 1, 2, 2, 59, 59, 0, time.UTC)
	cutoff := time.Date(2038, 1, 2, 3, 0, 0, 0, time.UTC)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return inside, nil }
	first, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) { return cutoff, nil }
	replay, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)
	// 模拟排队跨过 11:00：请求初检在窗口内，获得锁后的时钟已到截止时间。
	calls := 0
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		calls++
		if calls == 1 {
			return inside, nil
		}
		return cutoff, nil
	}
	_, err = r.Draw(ctx, uids[1], date)
	require.ErrorIs(t, err, service.ErrLotteryClosed)
	require.Equal(t, 2, calls)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, uids[1]).Scan(&balance))
	require.Equal(t, 3.125, balance)
	var count, spent int
	require.NoError(t, integrationDB.QueryRow(`SELECT draw_count,spent FROM lottery_days WHERE activity_date=$1`, date).Scan(&count, &spent))
	require.Equal(t, 1, count)
	require.Equal(t, 1, spent)
}

func TestLotteryIntroFirstThreeAcrossDaysAndZeroPrize(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	lotteryPaid(t, uids)
	ctx := context.Background()
	// 第一次无奖也消耗一次专属概率；网络重试必须返回相同记录。
	r.ticket = func(int) (int, error) { return 0, nil }
	first, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Zero(t, first.Prize)
	replay, err := r.Draw(ctx, uids[0], date)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)
	r.clock = func(context.Context, lotteryQuery) (time.Time, error) {
		return time.Date(2038, 1, 3, 2, 0, 0, 0, time.UTC), nil
	}
	// 同一个票号在专属规则中为 5，在普通规则中为无奖。
	r.ticket = func(scale int) (int, error) {
		if scale == service.LotteryIntroTicketCount {
			return 89900, nil
		}
		return 0, nil
	}
	second, err := r.Draw(ctx, uids[0], "2038-01-03")
	require.NoError(t, err)
	require.Equal(t, 5, second.Prize)
	// 管理员角色不会重置账号的历史次数。
	_, err = integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	third, err := r.Draw(ctx, uids[0], "2038-01-03", uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, 5, third.Prize)
	fourth, err := r.Draw(ctx, uids[0], "2038-01-03", uuid.NewString())
	require.NoError(t, err)
	require.Zero(t, fourth.Prize)
	for i, id := range []int64{first.ID, second.ID, third.ID, fourth.ID} {
		var number int
		var raw []byte
		require.NoError(t, integrationDB.QueryRow(`SELECT intro_draw_number,probability_weights FROM lottery_draws WHERE id=$1`, id).Scan(&number, &raw))
		var weights []int
		require.NoError(t, json.Unmarshal(raw, &weights))
		if i < 3 {
			require.Equal(t, i+1, number)
			require.Equal(t, service.LotteryIntroWeights(), weights)
		} else {
			require.Zero(t, number)
			require.Equal(t, []int{9552, 400, 30, 10, 5, 2, 1}, weights)
		}
	}
	a, err := r.AdminStatus(ctx, "2038-01-03")
	require.NoError(t, err)
	require.Equal(t, 3, a.IntroDrawLimit)
	require.Equal(t, service.LotteryIntroWeights(), a.IntroWeights)
}

func TestLotteryIntroConcurrentAdminRequestsOnlyThreeBoosts(t *testing.T) {
	r, uids, date := lotteryFixture(t, 1)
	_, err := integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
	require.NoError(t, err)
	r.ticket = func(scale int) (int, error) {
		if scale == service.LotteryIntroTicketCount {
			return 29900, nil
		}
		return 0, nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := r.Draw(context.Background(), uids[0], date, uuid.NewString())
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var count, boosted, total int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*),count(*) FILTER (WHERE intro_draw_number>0),sum(prize) FROM lottery_draws WHERE user_id=$1`, uids[0]).Scan(&count, &boosted, &total))
	require.Equal(t, 24, count)
	require.Equal(t, 3, boosted)
	require.Equal(t, 3, total)
}

func TestLotteryIntroCountsLegacyDrawsAndRetainsAttemptsAfterRollback(t *testing.T) {
	for _, historical := range []int{0, 1, 2, 3, 4} {
		t.Run(fmt.Sprintf("historical-%d", historical), func(t *testing.T) {
			r, uids, date := lotteryFixture(t, 1)
			_, err := integrationDB.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uids[0])
			require.NoError(t, err)
			// 模拟旧版流水：没有专属序号和权重快照，但仍占用历史次数。
			_, err = integrationDB.Exec(`INSERT INTO lottery_days(activity_date,weights) VALUES($1,'[9552,400,30,10,5,2,1]')`, date)
			require.NoError(t, err)
			for i := 0; i < historical; i++ {
				_, err = integrationDB.Exec(`INSERT INTO lottery_draws(user_id,activity_date,prize,ticket,budget_before,balance_before,balance_after,admin_repeat,request_id) VALUES($1,$2,0,0,100,3.125,3.125,true,$3)`, uids[0], date, uuid.NewString())
				require.NoError(t, err)
			}
			r.ticket = func(int) (int, error) { return 0, errors.New("entropy unavailable") }
			_, err = r.Draw(context.Background(), uids[0], date, uuid.NewString())
			require.Error(t, err)
			r.ticket = lotteryOneTicket
			d, err := r.Draw(context.Background(), uids[0], date, uuid.NewString())
			require.NoError(t, err)
			var number, count int
			require.NoError(t, integrationDB.QueryRow(`SELECT intro_draw_number FROM lottery_draws WHERE id=$1`, d.ID).Scan(&number))
			if historical < 3 {
				require.Equal(t, historical+1, number)
			} else {
				require.Zero(t, number)
			}
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM lottery_draws WHERE user_id=$1`, uids[0]).Scan(&count))
			require.Equal(t, historical+1, count)
		})
	}
}

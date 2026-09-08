package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type lotteryRepository struct {
	db *sql.DB
	// 仅供包内测试注入；运行时使用数据库时钟和 crypto/rand。
	clock  func(context.Context, lotteryQuery) (time.Time, error)
	ticket func() (int, error)
}

type lotteryQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func NewLotteryRepository(db *sql.DB) service.LotteryRepository {
	return &lotteryRepository{db: db, clock: func(ctx context.Context, q lotteryQuery) (time.Time, error) {
		var now time.Time
		err := q.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now)
		return now, err
	}, ticket: func() (int, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			return 0, err
		}
		return int(n.Int64()), nil
	}}
}

// pay_amount 为支付币种实付值，amount/refund_amount 为到账额度。
// 部分退款按到账退款比例折回人民币；待退款订单保守暂停计入。
// 老订单未存币种快照时，只有明确的人民币支付方式才可回退 CNY，不能把 USD 当 CNY 累加。
const lotteryEligibleSQL = `SELECT COALESCE(SUM(
  CASE WHEN status = 'PARTIALLY_REFUNDED'
    THEN pay_amount * GREATEST(amount - refund_amount, 0) / NULLIF(amount, 0)
    ELSE pay_amount END
), 0) >= 10 FROM payment_orders
WHERE user_id = $1 AND order_type = 'balance'
  AND status IN ('COMPLETED', 'PARTIALLY_REFUNDED')
  AND paid_at IS NOT NULL AND completed_at IS NOT NULL
  AND payment_trade_no <> '' AND amount > 0 AND pay_amount > 0
  AND COALESCE(NULLIF(UPPER(BTRIM(provider_snapshot->>'currency')), ''),
    CASE WHEN payment_type IN ('alipay','wxpay','alipay_direct','wxpay_direct','easypay') THEN 'CNY' ELSE '' END) = 'CNY'`

func lotteryEligible(ctx context.Context, q lotteryQuery, uid int64) (bool, error) {
	var eligible bool
	err := q.QueryRowContext(ctx, lotteryEligibleSQL, uid).Scan(&eligible)
	return eligible, err
}

func lotteryFindDraw(ctx context.Context, q lotteryQuery, uid int64, date string) (*service.LotteryDraw, error) {
	var d service.LotteryDraw
	err := q.QueryRowContext(ctx, `SELECT id, activity_date::text, prize, created_at FROM lottery_draws WHERE user_id=$1 AND activity_date=$2::date`, uid, date).Scan(&d.ID, &d.ActivityDate, &d.Prize, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func lotteryWeights(ctx context.Context, q lotteryQuery, date string) ([]int, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT weights FROM lottery_rules WHERE effective_date <= $1::date ORDER BY effective_date DESC LIMIT 1`, date).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var weights []int
	if err := json.Unmarshal(raw, &weights); err != nil {
		return nil, err
	}
	return weights, service.ValidateLotteryWeights(weights)
}

func (r *lotteryRepository) Status(ctx context.Context, uid int64) (*service.LotteryStatus, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now, err := r.clock(ctx, tx)
	if err != nil {
		return nil, err
	}
	date, open, next := service.LotteryWindow(now)
	out := &service.LotteryStatus{ActivityDate: date, State: "ready", ServerTime: now, OpensAt: open, NextOpensAt: next, Prizes: service.LotteryPrizes(), History: []service.LotteryDraw{}}
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM lottery_settings WHERE id=1`).Scan(&enabled); err != nil {
		return nil, err
	}
	if out.Eligible, err = lotteryEligible(ctx, tx, uid); err != nil {
		return nil, err
	}
	var spent int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT spent FROM lottery_days WHERE activity_date=$1::date),0)`, date).Scan(&spent); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, activity_date::text, prize, created_at FROM lottery_draws WHERE user_id=$1 ORDER BY id DESC LIMIT 30`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d service.LotteryDraw
		if err := rows.Scan(&d.ID, &d.ActivityDate, &d.Prize, &d.CreatedAt); err != nil {
			return nil, err
		}
		out.History = append(out.History, d)
		if d.ActivityDate == date {
			copy := d
			out.Today = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch {
	case out.Today != nil:
		out.State = "drawn"
	case !enabled:
		out.State = "disabled"
	case now.Before(open):
		out.State = "not_open"
	case spent >= service.LotteryDailyBudget:
		out.State = "ended"
	case !out.Eligible:
		out.State = "ineligible"
	}
	return out, tx.Commit()
}

func (r *lotteryRepository) Draw(ctx context.Context, uid int64, requestedDate string) (*service.LotteryDraw, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 允许已完成请求在活动关闭或跨天后重试，但不允许旧页面消耗新一天机会。
	if d, err := lotteryFindDraw(ctx, tx, uid, requestedDate); err != nil {
		return nil, err
	} else if d != nil {
		return d, tx.Commit()
	}
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM lottery_settings WHERE id=1 FOR SHARE`).Scan(&enabled); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, service.ErrLotteryClosed
	}
	now, err := r.clock(ctx, tx)
	if err != nil {
		return nil, err
	}
	date, open, _ := service.LotteryWindow(now)
	if date != requestedDate || now.Before(open) {
		return nil, service.ErrLotteryClosed
	}
	weights, err := lotteryWeights(ctx, tx, date)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(weights)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO lottery_days(activity_date,weights) VALUES ($1::date,$2::jsonb) ON CONFLICT DO NOTHING`, date, string(raw)); err != nil {
		return nil, err
	}
	var spent int
	if err := tx.QueryRowContext(ctx, `SELECT spent,weights FROM lottery_days WHERE activity_date=$1::date FOR UPDATE`, date).Scan(&spent, &raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &weights); err != nil {
		return nil, err
	}
	var before string
	if err := tx.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE`, uid).Scan(&before); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrLotteryIneligible
		}
		return nil, err
	}
	// 所有进程共用当日行锁；等待锁后必须重新检查唯一结果、资格及时间。
	if d, err := lotteryFindDraw(ctx, tx, uid, date); err != nil {
		return nil, err
	} else if d != nil {
		return d, tx.Commit()
	}
	if spent >= service.LotteryDailyBudget {
		return nil, service.ErrLotteryClosed
	}
	eligible, err := lotteryEligible(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, service.ErrLotteryIneligible
	}
	now, err = r.clock(ctx, tx)
	if err != nil {
		return nil, err
	}
	lockedDate, _, _ := service.LotteryWindow(now)
	if lockedDate != date || now.Before(open) {
		return nil, service.ErrLotteryClosed
	}
	ticket, err := r.ticket()
	if err != nil {
		return nil, fmt.Errorf("lottery random source: %w", err)
	}
	prize, err := service.LotteryPrizeForTicket(weights, ticket, service.LotteryDailyBudget-spent)
	if err != nil {
		return nil, err
	}
	after := before
	if prize > 0 {
		if err := tx.QueryRowContext(ctx, `UPDATE users SET balance=balance+$2,updated_at=NOW() WHERE id=$1 RETURNING balance::text`, uid, prize).Scan(&after); err != nil {
			return nil, err
		}
	}
	d := &service.LotteryDraw{ActivityDate: date, Prize: prize}
	if err := tx.QueryRowContext(ctx, `INSERT INTO lottery_draws(user_id,activity_date,prize,ticket,budget_before,balance_before,balance_after,created_at)
VALUES ($1,$2::date,$3,$4,$5,$6::numeric,$7::numeric,$8) RETURNING id,created_at`, uid, date, prize, ticket, service.LotteryDailyBudget-spent, before, after, now).Scan(&d.ID, &d.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lottery_days SET spent=spent+$2,draw_count=draw_count+1 WHERE activity_date=$1::date`, date, prize); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *lotteryRepository) AdminStatus(ctx context.Context, requestedDate string) (*service.LotteryAdminStatus, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now, err := r.clock(ctx, tx)
	if err != nil {
		return nil, err
	}
	date, _, next := service.LotteryWindow(now)
	if requestedDate != "" {
		if _, err := time.Parse("2006-01-02", requestedDate); err != nil {
			return nil, service.ErrLotteryConfig
		}
		date = requestedDate
	}
	out := &service.LotteryAdminStatus{ActivityDate: date, DailyBudget: service.LotteryDailyBudget, NextEffectiveDate: next.Format("2006-01-02"), Distribution: make([]int, 7), Records: []service.LotteryAdminDraw{}}
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM lottery_settings WHERE id=1`).Scan(&out.Enabled); err != nil {
		return nil, err
	}
	if out.Weights, err = lotteryWeights(ctx, tx, date); err != nil {
		return nil, err
	}
	if out.NextWeights, err = lotteryWeights(ctx, tx, out.NextEffectiveDate); err != nil {
		return nil, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT spent,draw_count,weights FROM lottery_days WHERE activity_date=$1::date`, date).Scan(&out.Spent, &out.DrawCount, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &out.Weights); err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT prize,count(*) FROM lottery_draws WHERE activity_date=$1::date GROUP BY prize`, date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var prize, count int
		if err = rows.Scan(&prize, &count); err != nil {
			rows.Close()
			return nil, err
		}
		for i, p := range service.LotteryPrizes() {
			if p == prize {
				out.Distribution[i] = count
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,activity_date::text,prize,created_at,user_id,balance_after FROM lottery_draws WHERE activity_date=$1::date ORDER BY id DESC LIMIT 100`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d service.LotteryAdminDraw
		if err = rows.Scan(&d.ID, &d.ActivityDate, &d.Prize, &d.CreatedAt, &d.UserID, &d.BalanceAfter); err != nil {
			return nil, err
		}
		out.Records = append(out.Records, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (r *lotteryRepository) UpdateConfig(ctx context.Context, actor int64, c service.LotteryConfigUpdate) error {
	if c.Weights != nil {
		if err := service.ValidateLotteryWeights(c.Weights); err != nil {
			return err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM lottery_settings WHERE id=1 FOR UPDATE`).Scan(&enabled); err != nil {
		return err
	}
	now, err := r.clock(ctx, tx)
	if err != nil {
		return err
	}
	_, _, next := service.LotteryWindow(now)
	nextDate := next.Format("2006-01-02")
	weights, err := lotteryWeights(ctx, tx, nextDate)
	if err != nil {
		return err
	}
	before, err := json.Marshal(map[string]any{"enabled": enabled, "effective_date": nextDate, "weights": weights})
	if err != nil {
		return err
	}
	if c.Enabled != nil {
		enabled = *c.Enabled
		if _, err = tx.ExecContext(ctx, `UPDATE lottery_settings SET enabled=$1 WHERE id=1`, enabled); err != nil {
			return err
		}
	}
	if c.Weights != nil {
		weights = c.Weights
		raw, err := json.Marshal(weights)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO lottery_rules(effective_date,weights,updated_by) VALUES ($1::date,$2::jsonb,$3)
ON CONFLICT(effective_date) DO UPDATE SET weights=EXCLUDED.weights,updated_by=EXCLUDED.updated_by,updated_at=NOW()`, nextDate, string(raw), actor); err != nil {
			return err
		}
	}
	after, err := json.Marshal(map[string]any{"enabled": enabled, "effective_date": nextDate, "weights": weights})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO lottery_config_audits(actor_id,before_config,after_config) VALUES ($1,$2::jsonb,$3::jsonb)`, actor, string(before), string(after)); err != nil {
		return err
	}
	return tx.Commit()
}

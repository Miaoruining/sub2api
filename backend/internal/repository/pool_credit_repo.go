package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type poolCreditReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// 额度窗口以共享订单为基准；存在上游 reset_at 时对齐账号窗口。
// 历史快照过期后按相同周期推进，直到下一次上游快照校正。
func poolCreditReset(now, anchor time.Time, raw string, width time.Duration) time.Time {
	reset, err := time.Parse(time.RFC3339, raw)
	if err != nil || reset.After(now.Add(width+time.Minute)) {
		reset = anchor.Add(width)
	}
	if !reset.After(now) {
		reset = reset.Add((now.Sub(reset)/width + 1) * width)
	}
	return reset
}

type poolCreditState struct {
	config    service.PoolCreditConfig
	seats     int
	member    service.PoolMember
	exhausted bool
}

func readPoolCredit(ctx context.Context, db poolCreditReader, mid int64) (poolCreditState, error) {
	var s poolCreditState
	var anchor sql.NullTime
	var extra []byte
	var now time.Time
	err := db.QueryRowContext(ctx, `SELECT p.quota_mode,p.plan_type,p.total_credit,p.credit_5h,p.credit_7d,p.seats,p.starts_at,COALESCE(a.extra,'{}'::jsonb),NOW()
 FROM pool_members m JOIN pool_orders p ON p.id=m.order_id LEFT JOIN pool_resources pr ON pr.id=p.resource_id LEFT JOIN accounts a ON a.id=pr.account_id WHERE m.id=$1`, mid).Scan(&s.config.QuotaMode, &s.config.PlanType, &s.config.TotalCredit, &s.config.Credit5h, &s.config.Credit7d, &s.seats, &anchor, &extra, &now)
	if err != nil {
		return s, err
	}
	start := now
	if anchor.Valid {
		start = anchor.Time
	}
	var snapshot struct {
		Reset5h string  `json:"codex_5h_reset_at"`
		Reset7d string  `json:"codex_7d_reset_at"`
		Used5h  float64 `json:"codex_5h_used_percent"`
		Used7d  float64 `json:"codex_7d_used_percent"`
	}
	_ = json.Unmarshal(extra, &snapshot)
	r5 := poolCreditReset(now, start, snapshot.Reset5h, 5*time.Hour)
	r7 := poolCreditReset(now, start, snapshot.Reset7d, 7*24*time.Hour)
	if s.config.PlanType == "plus" {
		s.member.Reset5h = &r5
		s.member.Reset7d = &r7
		for _, w := range []struct {
			raw  string
			used float64
		}{{snapshot.Reset5h, snapshot.Used5h}, {snapshot.Reset7d, snapshot.Used7d}} {
			if reset, e := time.Parse(time.RFC3339, w.raw); e == nil && reset.After(now) && w.used >= 100 {
				s.exhausted = true
			}
		}
	}
	// 未完成请求始终占用当前窗口，防止跨重置时间堆积并发绕过限额。
	err = db.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN status='settled' THEN actual_credit WHEN status='uncertain' THEN reserved_credit ELSE 0 END),0),
 COALESCE(SUM(reserved_credit) FILTER(WHERE status='pending'),0),
 COALESCE(SUM(CASE WHEN status='settled' THEN actual_credit ELSE reserved_credit END) FILTER(WHERE status<>'released' AND (created_at>=$2 OR status='pending')),0),
 COALESCE(SUM(CASE WHEN status='settled' THEN actual_credit ELSE reserved_credit END) FILTER(WHERE status<>'released' AND (created_at>=$3 OR status='pending')),0)
 FROM pool_requests WHERE member_id=$1`, mid, r5.Add(-5*time.Hour), r7.Add(-7*24*time.Hour)).Scan(&s.member.CreditUsed, &s.member.ReservedCredit, &s.member.Used5h, &s.member.Used7d)
	return s, err
}
func loadPoolCreditUsage(ctx context.Context, db poolCreditReader, mid int64, m *service.PoolMember) error {
	s, err := readPoolCredit(ctx, db, mid)
	if err != nil {
		return err
	}
	m.CreditUsed = s.member.CreditUsed
	m.ReservedCredit = s.member.ReservedCredit
	m.Used5h = s.member.Used5h
	m.Used7d = s.member.Used7d
	m.Reset5h = s.member.Reset5h
	m.Reset7d = s.member.Reset7d
	return nil
}
func checkPoolCredit(ctx context.Context, tx *sql.Tx, mid int64, cost float64) error {
	s, err := readPoolCredit(ctx, tx, mid)
	if err != nil {
		return err
	}
	if cost <= 0 || service.QuantizeUsageBillingAmount(s.member.CreditUsed+s.member.ReservedCredit+cost) > math.Floor(s.config.TotalCredit/float64(s.seats)*1e8)/1e8 {
		return service.ErrPoolLimit
	}
	if s.config.PlanType == "plus" && (s.exhausted || service.QuantizeUsageBillingAmount(s.member.Used5h+cost) > math.Floor(s.config.Credit5h/float64(s.seats)*1e8)/1e8 || service.QuantizeUsageBillingAmount(s.member.Used7d+cost) > math.Floor(s.config.Credit7d/float64(s.seats)*1e8)/1e8) {
		return service.ErrPoolLimit
	}
	return nil
}

func poolUsageSourceCondition(source, alias string) string {
	if source != "pool" && source != "standard" {
		return ""
	}
	clause := "EXISTS(SELECT 1 FROM pool_members pm WHERE pm.api_key_id=" + alias + ".api_key_id AND pm.user_id=" + alias + ".user_id)"
	if source == "standard" {
		return "NOT " + clause
	}
	return clause
}

func (r *poolRepository) CreditHolds(ctx context.Context, uid int64) ([]service.PoolCreditHold, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT q.id,m.order_id,m.api_key_id,q.status,q.reserved_credit,q.created_at FROM pool_requests q JOIN pool_members m ON m.id=q.member_id WHERE m.user_id=$1 AND q.status IN ('pending','uncertain') AND q.reserved_credit>0 ORDER BY q.created_at DESC LIMIT 100`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PoolCreditHold{}
	for rows.Next() {
		var h service.PoolCreditHold
		if err = rows.Scan(&h.ID, &h.OrderID, &h.KeyID, &h.Status, &h.Credit, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

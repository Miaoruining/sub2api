package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const dynamicSnapshotFreshness = 2 * time.Minute
const dynamicSafetyPercent = 2.0
const dynamicResetDrift = time.Minute

// NewPoolDynamicRepository returns the opt-in dynamic quota port.  The same
// SQL repository also implements PoolDynamicRefreshClaimer; keeping the lease
// in a separate interface means old PoolDynamicRepository mocks remain valid.
func NewPoolDynamicRepository(db *sql.DB) service.PoolDynamicRepository {
	return &poolDynamicRepository{db: db}
}

type poolDynamicRepository struct{ db *sql.DB }

var _ service.PoolDynamicRepository = (*poolDynamicRepository)(nil)
var _ service.PoolDynamicRefreshClaimer = (*poolDynamicRepository)(nil)

// Keep the primary repository usable as a single provider for deployments
// that wire one SQL pool repository.  These forwarding methods do not extend
// service.PoolRepository, so existing mocks stay source compatible.
func (r *poolRepository) DynamicAccounts(ctx context.Context) ([]int64, error) {
	return (&poolDynamicRepository{db: r.db}).DynamicAccounts(ctx)
}
func (r *poolRepository) ApplyDynamicSnapshot(ctx context.Context, s service.PoolDynamicSnapshot) error {
	return (&poolDynamicRepository{db: r.db}).ApplyDynamicSnapshot(ctx, s)
}
func (r *poolRepository) DynamicUsage(ctx context.Context, uid int64) ([]service.PoolDynamicUsage, error) {
	return (&poolDynamicRepository{db: r.db}).DynamicUsage(ctx, uid)
}
func (r *poolRepository) ClaimDynamicRefresh(ctx context.Context, accountID int64) (bool, error) {
	return (&poolDynamicRepository{db: r.db}).ClaimDynamicRefresh(ctx, accountID)
}

func (r *poolDynamicRepository) DynamicAccounts(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pr.account_id
		FROM pool_resources pr
		JOIN accounts a ON a.id=pr.account_id
		WHERE a.deleted_at IS NULL AND a.status='active' AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL
		  AND EXISTS(SELECT 1 FROM pool_resources x WHERE x.account_id=a.id)
		ORDER BY COALESCE((SELECT l.claimed_at FROM pool_dynamic_refresh_leases l WHERE l.account_id=pr.account_id), 'epoch'::timestamptz), pr.account_id
		LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]int64, 0, 100)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		accounts = append(accounts, id)
	}
	return accounts, rows.Err()
}

// ClaimDynamicRefresh takes a short lease.  A failed claim is a normal
// scheduling result and is intentionally not an error.
func (r *poolDynamicRepository) ClaimDynamicRefresh(ctx context.Context, accountID int64) (bool, error) {
	if accountID <= 0 {
		return false, service.ErrPoolConfig
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO pool_dynamic_refresh_leases(account_id,lease_until,claimed_at)
		SELECT $1,NOW()+INTERVAL '30 seconds',NOW()
		WHERE EXISTS(SELECT 1 FROM accounts a JOIN pool_resources pr ON pr.account_id=a.id
		              WHERE a.id=$1 AND a.deleted_at IS NULL AND a.status='active' AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL)
		ON CONFLICT(account_id) DO UPDATE SET lease_until=EXCLUDED.lease_until,claimed_at=EXCLUDED.claimed_at
		WHERE pool_dynamic_refresh_leases.lease_until<=NOW()`, accountID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func validateDynamicSnapshot(s service.PoolDynamicSnapshot) error {
	now := time.Now()
	if len(s.Windows) == 0 || s.AccountID <= 0 || s.ObservedAt.IsZero() || s.ObservedAt.After(now.Add(dynamicResetDrift)) {
		return service.ErrPoolConfig
	}
	seen := make(map[string]struct{}, len(s.Windows))
	for _, w := range s.Windows {
		if strings.TrimSpace(w.Key) == "" || w.WindowSeconds <= 0 || w.WindowSeconds > 366*24*3600 ||
			math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent > 100 ||
			w.ResetsAt.IsZero() || !w.ResetsAt.After(s.ObservedAt.Add(-dynamicResetDrift)) {
			return service.ErrPoolConfig
		}
		if _, ok := seen[w.Key]; ok {
			return service.ErrPoolConfig
		}
		seen[w.Key] = struct{}{}
	}
	return nil
}

// ApplyDynamicSnapshot persists an immutable observation, then folds it into
// active ledgers.  No network operation is performed in this transaction.
func (r *poolDynamicRepository) ApplyDynamicSnapshot(ctx context.Context, s service.PoolDynamicSnapshot) error {
	if err := validateDynamicSnapshot(s); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var snapshotID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO pool_dynamic_snapshots(account_id,observed_at,blocked)
		VALUES($1,$2,$3) ON CONFLICT(account_id,observed_at) DO NOTHING RETURNING id`, s.AccountID, s.ObservedAt, s.Blocked).Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		// Same observation is an idempotent retry.  In particular, a later
		// duplicate must not replay a reset or external delta.
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	for _, w := range s.Windows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO pool_dynamic_snapshot_windows(snapshot_id,key,used_percent,reset_at,window_seconds) VALUES($1,$2,$3,$4,$5)`, snapshotID, w.Key, w.UsedPercent, w.ResetsAt, w.WindowSeconds); err != nil {
			return err
		}
	}
	// We only create/fold ledgers for active orders.  A dynamic order waiting
	// for delivery gets a reliable baseline in Deliver, in the same transaction
	// that binds its account and keys.
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id,p.seats
		FROM pool_orders p JOIN pool_resources pr ON pr.id=p.resource_id
		WHERE pr.account_id=$1 AND p.status='active' AND p.expires_at>NOW() AND p.quota_mode IN ('dynamic','dynamic_shadow')
		ORDER BY p.id FOR UPDATE`, s.AccountID)
	if err != nil {
		return err
	}
	type dynamicOrder struct {
		id    int64
		seats int
	}
	orders := []dynamicOrder{}
	for rows.Next() {
		var o dynamicOrder
		if err = rows.Scan(&o.id, &o.seats); err != nil {
			rows.Close()
			return err
		}
		orders = append(orders, o)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, o := range orders {
		for _, w := range s.Windows {
			if err = applyDynamicWindowSnapshot(ctx, tx, o.id, o.seats, s.AccountID, snapshotID, s.ObservedAt, w, s.Blocked); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func applyDynamicWindowSnapshot(ctx context.Context, tx *sql.Tx, orderID int64, seats int, accountID, snapshotID int64, observed time.Time, w service.PoolDynamicSnapshotWindow, blocked bool) error {
	var ledgerID int64
	var resetAt, oldObserved time.Time
	var oldUsed float64
	err := tx.QueryRowContext(ctx, `SELECT id,reset_at,observed_at,observed_used_percent FROM pool_dynamic_window_ledgers WHERE order_id=$1 AND key=$2 ORDER BY observed_at DESC,id DESC LIMIT 1 FOR UPDATE`, orderID, w.Key).Scan(&ledgerID, &resetAt, &oldObserved, &oldUsed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && !observed.After(oldObserved) {
		return nil
	}
	previousID := int64(0)
	newWindow := errors.Is(err, sql.ErrNoRows)
	if !newWindow && math.Abs(w.ResetsAt.Sub(resetAt).Seconds()) > dynamicResetDrift.Seconds() {
		// 重置必须同时获得新的上游代次和已到达旧周期边界的证据。
		// 提前变化或倒退的重置时间只能暂停，不能清空成员历史。
		if !w.ResetsAt.After(resetAt) || observed.Before(resetAt.Add(-dynamicResetDrift)) {
			_, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_window_ledgers SET status='unknown' WHERE id=$1`, ledgerID)
			return err
		}
		previousID = ledgerID
		newWindow = true
	}
	if newWindow {
		err = tx.QueryRowContext(ctx, `INSERT INTO pool_dynamic_window_ledgers(order_id,account_id,key,reset_at,window_seconds,baseline_used_percent,observed_used_percent,observed_at,snapshot_id,status,calibration_ratio,calibration_samples)
   VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,
    COALESCE((SELECT calibration_ratio FROM pool_dynamic_window_ledgers WHERE account_id=$2 AND key=$3 AND calibration_samples>0 ORDER BY observed_at DESC,id DESC LIMIT 1),10),
    COALESCE((SELECT calibration_samples FROM pool_dynamic_window_ledgers WHERE account_id=$2 AND key=$3 AND calibration_samples>0 ORDER BY observed_at DESC,id DESC LIMIT 1),0)) RETURNING id`, orderID, accountID, w.Key, w.ResetsAt, w.WindowSeconds, w.UsedPercent, observed, snapshotID, dynamicLedgerStatus(w.UsedPercent, blocked)).Scan(&ledgerID)
		if err != nil {
			return err
		}
		if err = ensureDynamicMembers(ctx, tx, ledgerID, orderID); err != nil {
			return err
		}
		if previousID != 0 {
			if err = carryDynamicPendingTx(ctx, tx, previousID, ledgerID); err != nil {
				return err
			}
		}
		return rebuildDynamicEntitlementsTx(ctx, tx, ledgerID)
	}
	// 未归属增量持续累计。百分比停滞或请求仍在途时不会清掉水位，
	// 也不会把同一笔上游消耗一边记外部、一边再次扣成员。
	delta := math.Max(0, w.UsedPercent-oldUsed)
	_, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_window_ledgers SET observed_used_percent=GREATEST(observed_used_percent,$2),pending_delta_percent=pending_delta_percent+$3,observed_at=$4,snapshot_id=$5,status=$6 WHERE id=$1`, ledgerID, w.UsedPercent, delta, observed, snapshotID, dynamicLedgerStatus(math.Max(oldUsed, w.UsedPercent), blocked))
	if err != nil {
		return err
	}
	if err = calibrateDynamicWindowTx(ctx, tx, ledgerID, observed); err != nil {
		return err
	}
	return ensureDynamicMembers(ctx, tx, ledgerID, orderID)
}

// carryDynamicPendingTx moves, rather than copies, an in-flight request when
// an upstream window reset is confirmed.  Thus settlement sees one request
// once while the new generation still reserves its capacity.
func carryDynamicPendingTx(ctx context.Context, tx *sql.Tx, oldLedgerID, newLedgerID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT w.request_id,w.reserved_percent,q.member_id FROM pool_dynamic_request_windows w JOIN pool_requests q ON q.id=w.request_id WHERE w.ledger_id=$1 AND w.status='pending' FOR UPDATE`, oldLedgerID)
	if err != nil {
		return err
	}
	type pending struct {
		requestID string
		reserved  float64
		memberID  int64
	}
	var requests []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.requestID, &p.reserved, &p.memberID); err != nil {
			rows.Close()
			return err
		}
		requests = append(requests, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range requests {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET reserved_percent=GREATEST(0,reserved_percent-$3) WHERE ledger_id=$1 AND member_id=$2`, oldLedgerID, p.memberID, p.reserved); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET reserved_percent=reserved_percent+$3,entitlement_percent=GREATEST(entitlement_percent,used_percent+reserved_percent+$3) WHERE ledger_id=$1 AND member_id=$2`, newLedgerID, p.memberID, p.reserved); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_request_windows SET ledger_id=$2 WHERE request_id=$1 AND ledger_id=$3 AND status='pending'`, p.requestID, newLedgerID, oldLedgerID); err != nil {
			return err
		}
	}
	return nil
}

// 校准以持久化的未归属请求集为批次，而不是“上次采样之后创建”的请求。
// 有在途、账单不确定或晚于采样边界的成员时，保留整个批次等待下次探测。
func calibrateDynamicWindowTx(ctx context.Context, tx *sql.Tx, ledgerID int64, observed time.Time) error {
	var delta float64
	if err := tx.QueryRowContext(ctx, `SELECT pending_delta_percent FROM pool_dynamic_window_ledgers WHERE id=$1`, ledgerID).Scan(&delta); err != nil {
		return err
	}
	if delta <= 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT w.request_id,q.member_id,q.status,w.reserved_percent,COALESCE(w.actual_percent,0),COALESCE(q.actual_credit,0),q.finished_at
 FROM pool_dynamic_request_windows w JOIN pool_requests q ON q.id=w.request_id
 WHERE w.ledger_id=$1 AND w.calibration_status<>'calibrated' AND w.status<>'released'
 ORDER BY q.created_at,w.request_id`, ledgerID)
	if err != nil {
		return err
	}
	type sample struct {
		id                       string
		member                   int64
		status                   string
		reserved, actual, credit float64
		finished                 sql.NullTime
	}
	var batch []sample
	weight := 0.0
	ready := true
	for rows.Next() {
		var v sample
		if err = rows.Scan(&v.id, &v.member, &v.status, &v.reserved, &v.actual, &v.credit, &v.finished); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, v)
		if v.status != "settled" || !v.finished.Valid || v.finished.Time.After(observed) {
			ready = false
		}
		weight += v.credit
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(batch) == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_window_ledgers SET external_used_percent=LEAST(100,external_used_percent+pending_delta_percent),pending_delta_percent=0 WHERE id=$1`, ledgerID)
		return err
	}
	if !ready || weight <= 0 {
		return nil
	}
	left := delta
	for i, v := range batch {
		assigned := math.Floor(delta*v.credit/weight*1e8) / 1e8
		if i == len(batch)-1 {
			assigned = left
		}
		left -= assigned
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET used_percent=GREATEST(0,used_percent+$3) WHERE ledger_id=$1 AND member_id=$2`, ledgerID, v.member, assigned-v.actual); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_request_windows SET actual_percent=$3,calibration_status='calibrated',calibrated_snapshot_id=(SELECT snapshot_id FROM pool_dynamic_window_ledgers WHERE id=$2) WHERE request_id=$1 AND ledger_id=$2`, v.id, ledgerID, assigned); err != nil {
			return err
		}
	}
	ratio := math.Max(0.0001, delta/weight)
	_, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_window_ledgers SET pending_delta_percent=0,calibration_ratio=(calibration_ratio*calibration_samples+$2)/(calibration_samples+1),calibration_samples=calibration_samples+1 WHERE id=$1`, ledgerID, ratio)
	return err
}

// 总权益和实际上游余量同时约束放行；尚未覆盖的本地估算继续占用容量。
func dynamicCapacityTx(ctx context.Context, tx *sql.Tx, ledgerID int64) (float64, error) {
	var nominal, physical, loads, uncovered float64
	err := tx.QueryRowContext(ctx, `SELECT GREATEST(0,100-baseline_used_percent-safety_percent-external_used_percent),GREATEST(0,100-observed_used_percent-safety_percent),
 COALESCE((SELECT SUM(used_percent+reserved_percent) FROM pool_dynamic_member_ledgers WHERE ledger_id=$1),0),
 COALESCE((SELECT SUM(CASE WHEN status='pending' OR status='uncertain' THEN reserved_percent ELSE COALESCE(actual_percent,reserved_percent) END) FROM pool_dynamic_request_windows WHERE ledger_id=$1 AND status<>'released' AND calibration_status<>'calibrated'),0)
 FROM pool_dynamic_window_ledgers WHERE id=$1`, ledgerID).Scan(&nominal, &physical, &loads, &uncovered)
	return math.Min(nominal, loads+math.Max(0, physical-uncovered)), err
}

func dynamicLedgerStatus(used float64, blocked bool) string {
	if blocked {
		return "blocked"
	}
	if used+dynamicSafetyPercent >= 100 {
		return "exhausted"
	}
	return "ready"
}

func ensureDynamicMembers(ctx context.Context, tx *sql.Tx, ledgerID, orderID int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pool_dynamic_member_ledgers(ledger_id,member_id)
		SELECT $1,id FROM pool_members WHERE order_id=$2 AND status='joined'
		ON CONFLICT(ledger_id,member_id) DO NOTHING`, ledgerID, orderID)
	if err != nil {
		return err
	}
	return rebuildDynamicEntitlementsTx(ctx, tx, ledgerID)
}

func rebuildDynamicEntitlementsTx(ctx context.Context, tx *sql.Tx, ledgerID int64) error {
	capacity, err := dynamicCapacityTx(ctx, tx, ledgerID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT member_id,used_percent,reserved_percent FROM pool_dynamic_member_ledgers WHERE ledger_id=$1 ORDER BY member_id FOR UPDATE`, ledgerID)
	if err != nil {
		return err
	}
	type mstate struct {
		id             int64
		used, reserved float64
	}
	var members []mstate
	for rows.Next() {
		var m mstate
		if err = rows.Scan(&m.id, &m.used, &m.reserved); err != nil {
			rows.Close()
			return err
		}
		members = append(members, m)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	loads := make([]float64, len(members))
	for i, m := range members {
		loads[i] = m.used + m.reserved
	}
	additional := service.DynamicFairAllocation(capacity, loads)
	for i, m := range members {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET entitlement_percent=$3 WHERE ledger_id=$1 AND member_id=$2`, ledgerID, m.id, m.used+m.reserved+additional[i]); err != nil {
			return err
		}
	}
	return nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// DynamicUsage returns only the caller's own recent requests.  Joining by
// member user_id is intentional: a key id alone is not a sufficient isolation
// boundary after account rotation.
func (r *poolDynamicRepository) DynamicUsage(ctx context.Context, userID int64) ([]service.PoolDynamicUsage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT q.id,m.order_id,COALESCE(m.api_key_id,0),COALESCE(q.actual_credit,q.reserved_credit),q.status,q.created_at
		FROM pool_requests q JOIN pool_members m ON m.id=q.member_id
		JOIN pool_orders p ON p.id=m.order_id
		WHERE m.user_id=$1 AND p.quota_mode IN ('dynamic','dynamic_shadow')
		AND EXISTS(SELECT 1 FROM pool_dynamic_request_windows w WHERE w.request_id=q.id)
		ORDER BY q.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usage := []service.PoolDynamicUsage{}
	for rows.Next() {
		var u service.PoolDynamicUsage
		if err = rows.Scan(&u.ID, &u.OrderID, &u.KeyID, &u.Credit, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		usage = append(usage, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range usage {
		if err = r.loadDynamicUsageWindows(ctx, &usage[i]); err != nil {
			return nil, err
		}
	}
	return usage, nil
}

func (r *poolDynamicRepository) loadDynamicUsageWindows(ctx context.Context, u *service.PoolDynamicUsage) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT w.key,w.reserved_percent,w.actual_percent,w.status,w.calibration_status,
		       l.observed_used_percent,ml.reserved_percent,
		       GREATEST(0,ml.entitlement_percent-ml.used_percent-ml.reserved_percent),
		       ml.entitlement_percent,l.reset_at,l.observed_at
		FROM pool_dynamic_request_windows w
		JOIN pool_dynamic_window_ledgers l ON l.id=w.ledger_id
		JOIN pool_dynamic_member_ledgers ml ON ml.ledger_id=l.id
		JOIN pool_requests q ON q.id=w.request_id
		JOIN pool_members m ON m.id=q.member_id AND m.id=ml.member_id
		WHERE w.request_id=$1 ORDER BY w.key`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var w service.PoolDynamicQuotaWindow
		var reserved, accountUsed, memberReserved, remaining, entitlement float64
		var actual sql.NullFloat64
		var calibrationStatus string
		if err = rows.Scan(&w.Key, &reserved, &actual, &w.Status, &calibrationStatus, &accountUsed, &memberReserved, &remaining, &entitlement, &w.ResetAt, &w.ObservedAt); err != nil {
			return err
		}
		w.AccountRemainingPercent = pp(100 - accountUsed)
		if w.Status == "settled" || w.Status == "uncertain" {
			w.UsedPercent = pp(actual.Float64)
			if !actual.Valid {
				w.UsedPercent = pp(reserved)
			}
			w.ReservedPercent = 0
		} else {
			w.UsedPercent = 0
			w.ReservedPercent = pp(reserved)
		}
		w.RemainingPercent = pp(remaining)
		w.EntitlementPercent = pp(entitlement)
		if calibrationStatus != "" {
			w.Status = calibrationStatus
		} else if w.Status == "pending" {
			w.Status = "estimated"
		}
		u.Windows = append(u.Windows, w)
	}
	err = rows.Err()
	u.Status = "calibrated"
	for _, w := range u.Windows {
		if w.Status != "calibrated" {
			u.Status = w.Status
			break
		}
	}
	return err
}

func pp(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*1e8) / 1e8
}

type dynamicWindowState struct {
	id       int64
	key      string
	resetAt  time.Time
	used     float64
	reserved float64
	actual   float64
}

// reservePoolDynamicTx attaches one request to every currently observed
// window.  The caller already holds the order and member locks; this function
// locks ledgers and member rows in key/member order and never performs I/O.
func reservePoolDynamicTx(ctx context.Context, tx *sql.Tx, memberID int64, requestID string, credit float64, enforce bool) error {
	// 观察模式保留旧计费，但独立 Spark 用量不能参与普通 Codex 窗口学习。
	if model, _ := ctx.Value(service.PoolStandardPricingContextKey{}).(string); !enforce && strings.Contains(strings.ToLower(model), "codex-spark") {
		return nil
	}
	if credit <= 0 || math.IsNaN(credit) || math.IsInf(credit, 0) {
		return service.ErrPoolConfig
	}
	var orderID, accountID int64
	if err := tx.QueryRowContext(ctx, `SELECT p.id,pr.account_id FROM pool_members m JOIN pool_orders p ON p.id=m.order_id JOIN pool_resources pr ON pr.id=p.resource_id WHERE m.id=$1`, memberID).Scan(&orderID, &accountID); err != nil {
		return err
	}
	var observed time.Time
	var blocked bool
	err := tx.QueryRowContext(ctx, `SELECT observed_at,blocked FROM pool_dynamic_snapshots WHERE account_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1`, accountID).Scan(&observed, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		if enforce {
			return service.ErrPoolDynamicUnavailable
		}
		return nil
	}
	if err != nil {
		return err
	}
	if enforce && (blocked || time.Since(observed) > dynamicSnapshotFreshness || time.Since(observed) < -dynamicResetDrift) {
		if blocked {
			return service.ErrPoolDynamicBlocked
		}
		return service.ErrPoolDynamicUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,used_percent,reset_at,window_seconds FROM pool_dynamic_snapshot_windows WHERE snapshot_id=(SELECT id FROM pool_dynamic_snapshots WHERE account_id=$1 AND observed_at=$2) ORDER BY key`, accountID, observed)
	if err != nil {
		return err
	}
	windows := []service.PoolDynamicSnapshotWindow{}
	for rows.Next() {
		var w service.PoolDynamicSnapshotWindow
		if err = rows.Scan(&w.Key, &w.UsedPercent, &w.ResetsAt, &w.WindowSeconds); err != nil {
			rows.Close()
			return err
		}
		windows = append(windows, w)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(windows) == 0 {
		if enforce {
			return service.ErrPoolDynamicUnavailable
		}
		return nil
	}
	if enforce {
		var missing bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pool_dynamic_window_ledgers l WHERE l.order_id=$1 AND l.reset_at>NOW() AND NOT EXISTS(SELECT 1 FROM pool_dynamic_snapshot_windows sw WHERE sw.snapshot_id=(SELECT id FROM pool_dynamic_snapshots WHERE account_id=$2 ORDER BY observed_at DESC,id DESC LIMIT 1) AND sw.key=l.key))`, orderID, accountID).Scan(&missing); err != nil {
			return err
		}
		if missing {
			return service.ErrPoolDynamicUnavailable
		}
		var cold, pending bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pool_dynamic_window_ledgers WHERE order_id=$1 AND calibration_samples=0 AND reset_at>NOW()),EXISTS(SELECT 1 FROM pool_requests q JOIN pool_members m ON m.id=q.member_id WHERE m.order_id=$1 AND q.status='pending' AND q.id<>$2)`, orderID, requestID).Scan(&cold, &pending); err != nil {
			return err
		}
		if cold && pending {
			return service.ErrPoolLimit
		}
	}
	for _, w := range windows {
		if enforce && !w.ResetsAt.After(time.Now()) {
			return service.ErrPoolDynamicUnavailable
		}
		var ledger dynamicWindowState
		err = tx.QueryRowContext(ctx, `
			SELECT id,key,reset_at FROM pool_dynamic_window_ledgers
			WHERE order_id=$1 AND key=$2 AND ABS(EXTRACT(EPOCH FROM(reset_at-$3::timestamptz)))<=60
			ORDER BY ABS(EXTRACT(EPOCH FROM(reset_at-$3::timestamptz))),id DESC LIMIT 1 FOR UPDATE`, orderID, w.Key, w.ResetsAt).Scan(&ledger.id, &ledger.key, &ledger.resetAt)
		if errors.Is(err, sql.ErrNoRows) {
			if enforce {
				return service.ErrPoolDynamicUnavailable
			}
			continue
		}
		if err != nil {
			return err
		}
		if enforce {
			var state string
			if err = tx.QueryRowContext(ctx, `SELECT status FROM pool_dynamic_window_ledgers WHERE id=$1`, ledger.id).Scan(&state); err != nil {
				return err
			}
			if state == "unknown" || state == "stale" {
				return service.ErrPoolDynamicUnavailable
			}
			if state == "blocked" || state == "exhausted" {
				return service.ErrPoolDynamicBlocked
			}
		}
		if err = reserveDynamicWindow(ctx, tx, ledger, memberID, requestID, credit, enforce); err != nil {
			return err
		}
	}
	return nil
}

func reserveDynamicWindow(ctx context.Context, tx *sql.Tx, ledger dynamicWindowState, memberID int64, requestID string, credit float64, enforce bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT member_id,used_percent,reserved_percent FROM pool_dynamic_member_ledgers WHERE ledger_id=$1 ORDER BY member_id FOR UPDATE`, ledger.id)
	if err != nil {
		return err
	}
	type memberState struct {
		id             int64
		used, reserved float64
	}
	members := []memberState{}
	for rows.Next() {
		var m memberState
		if err = rows.Scan(&m.id, &m.used, &m.reserved); err != nil {
			rows.Close()
			return err
		}
		members = append(members, m)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(members) == 0 {
		return service.ErrPoolDynamicUnavailable
	}
	loads := make([]float64, len(members))
	idx := -1
	for i, m := range members {
		loads[i] = m.used + m.reserved
		if m.id == memberID {
			idx = i
		}
	}
	if idx < 0 {
		return service.ErrPoolAccess
	}
	capacity, err := dynamicCapacityTx(ctx, tx, ledger.id)
	if err != nil {
		return err
	}
	var calibration float64
	var samples int64
	if err = tx.QueryRowContext(ctx, `SELECT calibration_ratio,calibration_samples FROM pool_dynamic_window_ledgers WHERE id=$1`, ledger.id).Scan(&calibration, &samples); err != nil {
		return err
	}
	ppReserve := service.DynamicCalibratedCreditPercentage(credit, calibration, samples)
	if samples > 0 {
		ppReserve = math.Ceil(ppReserve*1.25*1e8) / 1e8
	}
	if capacity < 0 {
		capacity = 0
	}
	additional := service.DynamicFairAllocation(capacity, loads)
	for i, m := range members {
		entitlement := m.used + m.reserved + additional[i]
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET entitlement_percent=$2 WHERE ledger_id=$1 AND member_id=$3`, ledger.id, entitlement, m.id); err != nil {
			return err
		}
	}
	if enforce && additional[idx]+1e-8 < ppReserve {
		return service.ErrPoolLimit
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers SET reserved_percent=reserved_percent+$3,entitlement_percent=GREATEST(entitlement_percent,used_percent+reserved_percent+$3) WHERE ledger_id=$1 AND member_id=$2`, ledger.id, memberID, ppReserve); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pool_dynamic_request_windows(request_id,ledger_id,key,reserved_percent) VALUES($1,$2,$3,$4)`, requestID, ledger.id, ledger.key, ppReserve)
	return err
}

// finishDynamicRequestTx converts a request's reservation to conservative
// settled use while the upstream billing result is still uncertain.
func finishDynamicRequestTx(ctx context.Context, tx *sql.Tx, requestID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT ledger_id,key,reserved_percent FROM pool_dynamic_request_windows WHERE request_id=$1 FOR UPDATE`, requestID)
	if err != nil {
		return err
	}
	type requestWindow struct {
		ledgerID int64
		key      string
		reserved float64
	}
	var windows []requestWindow
	for rows.Next() {
		var ledgerID int64
		var key string
		var reserved float64
		if err = rows.Scan(&ledgerID, &key, &reserved); err != nil {
			rows.Close()
			return err
		}
		windows = append(windows, requestWindow{ledgerID: ledgerID, key: key, reserved: reserved})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, w := range windows {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers ml SET reserved_percent=GREATEST(0,ml.reserved_percent-$3),used_percent=ml.used_percent+$3,entitlement_percent=GREATEST(ml.entitlement_percent,ml.used_percent+$3) FROM pool_dynamic_request_windows rw WHERE rw.request_id=$1 AND rw.ledger_id=$2 AND rw.key=$4 AND ml.ledger_id=rw.ledger_id AND ml.member_id=(SELECT member_id FROM pool_requests WHERE id=$1)`, requestID, w.ledgerID, w.reserved, w.key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_request_windows SET status='uncertain',calibration_status=CASE WHEN calibration_status='calibrated' THEN calibration_status ELSE 'uncertain' END WHERE request_id=$1 AND ledger_id=$2 AND key=$3 AND status='pending'`, requestID, w.ledgerID, w.key); err != nil {
			return err
		}
	}
	return nil
}

// settleDynamicRequestTx applies the actual estimated percentage once.  The
// surrounding request status update is the idempotency gate.
func settleDynamicRequestTx(ctx context.Context, tx *sql.Tx, requestID string, actualCredit float64) error {
	rows, err := tx.QueryContext(ctx, `SELECT w.ledger_id,w.key,w.reserved_percent,w.status,l.calibration_ratio,l.calibration_samples FROM pool_dynamic_request_windows w JOIN pool_dynamic_window_ledgers l ON l.id=w.ledger_id WHERE w.request_id=$1 FOR UPDATE OF w`, requestID)
	if err != nil {
		return err
	}
	type requestWindow struct {
		ledgerID int64
		key      string
		reserved float64
		status   string
		ratio    float64
		samples  int64
	}
	var windows []requestWindow
	for rows.Next() {
		var ledgerID int64
		var key string
		var reserved float64
		var status string
		var ratio float64
		var samples int64
		if err = rows.Scan(&ledgerID, &key, &reserved, &status, &ratio, &samples); err != nil {
			rows.Close()
			return err
		}
		windows = append(windows, requestWindow{ledgerID: ledgerID, key: key, reserved: reserved, status: status, ratio: ratio, samples: samples})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, w := range windows {
		var already bool
		if err = tx.QueryRowContext(ctx, `SELECT calibration_status='calibrated' FROM pool_dynamic_request_windows WHERE request_id=$1 AND ledger_id=$2`, requestID, w.ledgerID).Scan(&already); err != nil {
			return err
		}
		if already {
			continue
		}
		actualPP := service.DynamicCalibratedCreditPercentage(actualCredit, w.ratio, w.samples)
		if actualPP <= 0 {
			actualPP = 0.0001
		}
		// finishDynamicRequestTx already moved reserved to used.  For a direct
		// settle, status may still be pending; release that reservation first.
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_member_ledgers ml SET reserved_percent=GREATEST(0,ml.reserved_percent-CASE WHEN rw.status='pending' THEN rw.reserved_percent ELSE 0 END),used_percent=GREATEST(0,ml.used_percent-CASE WHEN rw.status='uncertain' THEN rw.reserved_percent ELSE 0 END)+$4,entitlement_percent=GREATEST(ml.entitlement_percent,ml.used_percent+$4) FROM pool_dynamic_request_windows rw WHERE rw.request_id=$1 AND rw.ledger_id=$2 AND rw.key=$3 AND ml.ledger_id=rw.ledger_id AND ml.member_id=(SELECT member_id FROM pool_requests WHERE id=$1)`, requestID, w.ledgerID, w.key, actualPP); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_dynamic_request_windows SET actual_percent=$4,status='settled',calibration_status='estimated' WHERE request_id=$1 AND ledger_id=$2 AND key=$3`, requestID, w.ledgerID, w.key, actualPP); err != nil {
			return err
		}
		if err = rebuildDynamicEntitlementsTx(ctx, tx, w.ledgerID); err != nil {
			return err
		}
	}
	return nil
}

// loadPoolDynamicQuota is used by the legacy pool list repository so callers
// need not know about the second repository port.
func loadPoolDynamicQuota(ctx context.Context, db *sql.DB, memberID int64, shadow bool) (*service.PoolDynamicQuota, error) {
	var accountID int64
	q := &service.PoolDynamicQuota{Windows: []service.PoolDynamicQuotaWindow{}, Shadow: shadow}
	if err := db.QueryRowContext(ctx, `SELECT pr.account_id FROM pool_members m JOIN pool_orders p ON p.id=m.order_id JOIN pool_resources pr ON pr.id=p.resource_id WHERE m.id=$1`, memberID).Scan(&accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			q.Status = "unknown"
			return q, nil
		}
		return nil, err
	}
	var observed time.Time
	var snapshotID int64
	var blocked bool
	err := db.QueryRowContext(ctx, `SELECT s.id,s.observed_at,s.blocked FROM pool_dynamic_snapshots s WHERE s.account_id=$1 ORDER BY s.observed_at DESC,s.id DESC LIMIT 1`, accountID).Scan(&snapshotID, &observed, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		q.Status = "unknown"
		return q, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT l.key,l.observed_used_percent,
		       COALESCE(ml.used_percent,0),COALESCE(ml.reserved_percent,0),COALESCE(ml.entitlement_percent,0),
		       l.reset_at,l.observed_at,l.status,EXISTS(SELECT 1 FROM pool_dynamic_snapshot_windows sw WHERE sw.snapshot_id=$2 AND sw.key=l.key AND ABS(EXTRACT(EPOCH FROM(sw.reset_at-l.reset_at)))<=60)
		FROM pool_dynamic_window_ledgers l
		JOIN pool_orders p ON p.id=l.order_id
		LEFT JOIN pool_dynamic_member_ledgers ml ON ml.ledger_id=l.id AND ml.member_id=$1
		WHERE l.order_id=(SELECT order_id FROM pool_members WHERE id=$1)
		  AND l.reset_at=(SELECT MAX(x.reset_at) FROM pool_dynamic_window_ledgers x WHERE x.order_id=l.order_id AND x.key=l.key)
		ORDER BY l.key`, memberID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var w service.PoolDynamicQuotaWindow
		var accountUsed float64
		var present bool
		if err = rows.Scan(&w.Key, &accountUsed, &w.UsedPercent, &w.ReservedPercent, &w.EntitlementPercent, &w.ResetAt, &w.ObservedAt, &w.Status, &present); err != nil {
			return nil, err
		}
		w.AccountRemainingPercent = pp(100 - accountUsed)
		w.UsedPercent = pp(w.UsedPercent)
		w.ReservedPercent = pp(w.ReservedPercent)
		w.EntitlementPercent = pp(w.EntitlementPercent)
		w.RemainingPercent = pp(w.EntitlementPercent - w.UsedPercent - w.ReservedPercent)
		if !present || !w.ResetAt.After(time.Now()) {
			w.Status = "unknown"
		}
		if time.Since(w.ObservedAt) > dynamicSnapshotFreshness {
			w.Status = "stale"
		}
		q.Windows = append(q.Windows, w)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if blocked {
		q.Status = "blocked"
	} else if time.Since(observed) > dynamicSnapshotFreshness {
		q.Status = "stale"
	} else if len(q.Windows) == 0 {
		q.Status = "unknown"
	} else {
		q.Status = "ready"
		for _, w := range q.Windows {
			if w.Status != "ready" {
				q.Status = w.Status
				break
			}
		}
	}
	return q, nil
}

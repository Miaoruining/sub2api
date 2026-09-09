package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 发货事务使用同一份可信快照建立所有成员的初始权益，不把发货前消耗计入成员。
// 观察模式允许暂时没有快照；正式动态模式必须有可用的订阅窗口。
func initializeDynamicDelivery(ctx context.Context, tx *sql.Tx, orderID, accountID int64, enforce bool) error {
	var eligible bool
	if err := tx.QueryRowContext(ctx, `SELECT platform='openai' AND type='oauth' AND parent_account_id IS NULL AND status='active' AND deleted_at IS NULL FROM accounts WHERE id=$1`, accountID).Scan(&eligible); err != nil {
		return err
	}
	if !eligible {
		if enforce {
			return service.ErrPoolDynamicUnavailable
		}
		return nil
	}
	var snapshotID int64
	var observed time.Time
	var blocked bool
	err := tx.QueryRowContext(ctx, `SELECT id,observed_at,blocked FROM pool_dynamic_snapshots WHERE account_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1`, accountID).Scan(&snapshotID, &observed, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		if enforce {
			return service.ErrPoolDynamicUnavailable
		}
		return nil
	}
	if err != nil {
		return err
	}
	if time.Since(observed) > dynamicSnapshotFreshness || observed.After(time.Now().Add(dynamicResetDrift)) {
		if enforce {
			return service.ErrPoolDynamicUnavailable
		}
		return nil
	}
	if enforce && blocked {
		return service.ErrPoolDynamicBlocked
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,used_percent,reset_at,window_seconds FROM pool_dynamic_snapshot_windows WHERE snapshot_id=$1 ORDER BY key`, snapshotID)
	if err != nil {
		return err
	}
	var windows []service.PoolDynamicSnapshotWindow
	for rows.Next() {
		var w service.PoolDynamicSnapshotWindow
		if err = rows.Scan(&w.Key, &w.UsedPercent, &w.ResetsAt, &w.WindowSeconds); err != nil {
			rows.Close()
			return err
		}
		windows = append(windows, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if enforce && len(windows) == 0 {
		return service.ErrPoolDynamicUnavailable
	}
	var seats int
	if err = tx.QueryRowContext(ctx, `SELECT seats FROM pool_orders WHERE id=$1`, orderID).Scan(&seats); err != nil {
		return err
	}
	for _, w := range windows {
		if !w.ResetsAt.After(time.Now()) {
			if enforce {
				return service.ErrPoolDynamicUnavailable
			}
			continue
		}
		if enforce && w.UsedPercent+dynamicSafetyPercent >= 100 {
			return service.ErrPoolDynamicBlocked
		}
		if err = applyDynamicWindowSnapshot(ctx, tx, orderID, seats, accountID, snapshotID, observed, w, blocked); err != nil {
			return err
		}
	}
	return nil
}

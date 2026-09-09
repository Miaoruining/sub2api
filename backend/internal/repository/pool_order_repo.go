package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type poolRepository struct{ db *sql.DB }

func NewPoolRepository(db *sql.DB) service.PoolRepository { return &poolRepository{db: db} }

func (r *poolRepository) List(ctx context.Context, uid int64, admin bool) ([]service.PoolOrder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.title,COALESCE(p.group_id,0),p.seats,p.price,p.duration_hours,p.total_tokens,p.total_requests,p.concurrency,p.join_deadline,
 CASE WHEN p.status='active' AND p.expires_at<=NOW() THEN 'expired' WHEN p.status='forming' AND p.join_deadline<=NOW() THEN 'closed' ELSE p.status END,
 p.starts_at,p.expires_at,p.formed_at,p.delivery_deadline,p.resource_id,p.product_id,
 (SELECT COUNT(*) FROM pool_members WHERE order_id=p.id AND status='joined'),
 COALESCE((SELECT SUM(tokens_used) FROM pool_members WHERE order_id=p.id),0),
 COALESCE((SELECT SUM(requests_used) FROM pool_members WHERE order_id=p.id),0),
 COALESCE((SELECT SUM(q.reserved_tokens) FROM pool_requests q JOIN pool_members m ON m.id=q.member_id WHERE m.order_id=p.id AND q.status='pending'),0)
 FROM pool_orders p WHERE $2 OR p.status IN ('forming','active') OR (p.status='awaiting_delivery' AND p.formed_at>=NOW()-INTERVAL '24 hours') OR EXISTS(SELECT 1 FROM pool_members WHERE order_id=p.id AND user_id=$1)
 ORDER BY p.id DESC LIMIT 100`, uid, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PoolOrder{}
	for rows.Next() {
		var p service.PoolOrder
		if err = rows.Scan(&p.ID, &p.Title, &p.GroupID, &p.Seats, &p.Price, &p.DurationHours, &p.TotalTokens, &p.TotalRequests, &p.Concurrency, &p.JoinDeadline, &p.Status, &p.StartsAt, &p.ExpiresAt, &p.FormedAt, &p.DeliveryDeadline, &p.ResourceID, &p.ProductID, &p.Joined, &p.TokensUsed, &p.RequestsUsed, &p.ReservedTokens); err != nil {
			return nil, err
		}
		p.DurationDays = float64(p.DurationHours) / 24
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		var m service.PoolMember
		err = r.db.QueryRowContext(ctx, `SELECT m.id,m.status,m.api_key_id,m.paid,m.refunded,m.tokens_used,m.requests_used,
 COALESCE((SELECT SUM(reserved_tokens) FROM pool_requests WHERE member_id=m.id AND status='pending'),0),
 (SELECT COUNT(*) FROM pool_requests WHERE member_id=m.id AND status='pending') FROM pool_members m WHERE order_id=$1 AND user_id=$2`, out[i].ID, uid).Scan(&m.ID, &m.Status, &m.KeyID, &m.Paid, &m.Refunded, &m.TokensUsed, &m.RequestsUsed, &m.ReservedTokens, &m.Inflight)
		if err == nil {
			out[i].Mine = &m
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	resources, e := r.Resources(ctx)
	if e != nil {
		return nil, e
	}
	for i := range out {
		if out[i].Mine != nil || admin {
			for _, res := range resources {
				if res.GroupID == out[i].GroupID {
					safe := res
					safe.ID = 0
					safe.AccountID = 0
					safe.GroupID = 0
					safe.Name = ""
					out[i].SharedAccount = &safe
				}
			}
		}
	}
	return out, nil
}
func (r *poolRepository) Create(ctx context.Context, p service.PoolCreate) (int64, error) {
	if err := p.Validate(time.Now()); err != nil {
		return 0, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO pool_orders(title,seats,price,duration_hours,total_tokens,total_requests,concurrency,join_deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, p.Title, p.Seats, p.Price, p.DurationHours, p.TotalTokens, p.TotalRequests, p.Concurrency, p.JoinDeadline).Scan(&id)
	return id, err
}
func (r *poolRepository) Join(ctx context.Context, oid, uid int64) ([]int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	affected, err := joinPoolOrder(ctx, tx, oid, uid)
	if err != nil {
		return nil, err
	}
	return affected, tx.Commit()
}
func joinPoolOrder(ctx context.Context, tx *sql.Tx, oid, uid int64) ([]int64, error) {
	var err error
	var status string
	var seats int
	var open bool
	err = tx.QueryRowContext(ctx, `SELECT status,seats,join_deadline>NOW() FROM pool_orders WHERE id=$1 FOR UPDATE`, oid).Scan(&status, &seats, &open)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPoolClosed
	}
	if err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pool_members WHERE order_id=$1 AND user_id=$2)`, oid, uid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return []int64{uid}, nil
	} // 重试不重复扣款；已退出者不重新购买同车。
	if status != "forming" || !open {
		return nil, service.ErrPoolClosed
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pool_members WHERE order_id=$1 AND status='joined'`, oid).Scan(&count); err != nil {
		return nil, err
	}
	if count >= seats {
		return nil, service.ErrPoolClosed
	}
	var balance float64
	err = tx.QueryRowContext(ctx, `UPDATE users SET balance=balance-p.price,updated_at=NOW() FROM pool_orders p WHERE users.id=$1 AND p.id=$2 AND users.deleted_at IS NULL AND users.status='active' AND balance>=p.price RETURNING balance`, uid, oid).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPoolBalance
	}
	if err != nil {
		return nil, err
	}
	var mid int64
	err = tx.QueryRowContext(ctx, `INSERT INTO pool_members(order_id,user_id,paid) SELECT id,$2,price FROM pool_orders WHERE id=$1 RETURNING id`, oid, uid).Scan(&mid)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pool_wallet_entries(member_id,kind,amount,balance_after) SELECT $1,'purchase',-price,$2 FROM pool_orders WHERE id=$3`, mid, balance, oid); err != nil {
		return nil, err
	}
	affected := []int64{uid}
	if count+1 == seats {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_orders SET status='awaiting_delivery',formed_at=NOW(),delivery_deadline=NOW()+INTERVAL '24 hours' WHERE id=$1`, oid); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pool_notifications(order_id,kind) VALUES($1,'delivery_required')`, oid); err != nil {
			return nil, err
		}
	}

	return affected, nil
}

func (r *poolRepository) Leave(ctx context.Context, oid, uid int64) ([]int64, error) {
	return r.refund(ctx, oid, uid, false)
}
func (r *poolRepository) Cancel(ctx context.Context, oid int64) ([]int64, error) {
	return r.refund(ctx, oid, 0, true)
}
func (r *poolRepository) refund(ctx context.Context, oid, uid int64, cancel bool) ([]int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM pool_orders WHERE id=$1 FOR UPDATE`, oid).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPoolClosed
	}
	if err != nil {
		return nil, err
	}
	if !cancel && status != "forming" {
		return nil, service.ErrPoolClosed
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id FROM pool_members WHERE order_id=$1 AND status='joined' AND ($2 OR user_id=$3) ORDER BY user_id FOR UPDATE`, oid, cancel, uid)
	if err != nil {
		return nil, err
	}
	type member struct{ id, uid int64 }
	members := []member{}
	for rows.Next() {
		var m member
		if err = rows.Scan(&m.id, &m.uid); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	affected := []int64{}
	for _, m := range members {
		// 数据库数值运算保留 8 位；按实际未使用服务时长退款，最高不超过实付。
		_, err = tx.ExecContext(ctx, `UPDATE pool_members m SET status='refunded',refunded=ROUND(m.paid*CASE WHEN p.starts_at IS NULL THEN 1 ELSE LEAST(1,GREATEST(0,EXTRACT(EPOCH FROM(p.expires_at-NOW()))/(p.duration_hours*3600))) END,8) FROM pool_orders p WHERE m.id=$1 AND p.id=m.order_id`, m.id)
		if err != nil {
			return nil, err
		}
		var balance float64
		err = tx.QueryRowContext(ctx, `UPDATE users u SET balance=balance+m.refunded,updated_at=NOW() FROM pool_members m WHERE m.id=$1 AND u.id=m.user_id RETURNING u.balance`, m.id).Scan(&balance)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pool_wallet_entries(member_id,kind,amount,balance_after) SELECT id,'refund',refunded,$2 FROM pool_members WHERE id=$1`, m.id, balance); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=(SELECT api_key_id FROM pool_members WHERE id=$1)`, m.id); err != nil {
			return nil, err
		}
		affected = append(affected, m.uid)
	}
	if cancel {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_orders SET status='cancelled' WHERE id=$1`, oid); err != nil {
			return nil, err
		}
	}
	// 商品开团后最后一人退出时结束空团，避免大厅出现无人参与的订单。
	if !cancel {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_orders SET status='cancelled' WHERE id=$1 AND product_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pool_members WHERE order_id=$1 AND status='joined')`, oid); err != nil {
			return nil, err
		}
	}
	return affected, tx.Commit()
}

func (r *poolRepository) Gate(ctx context.Context, kid, uid, gid int64) (*service.PoolGate, error) {
	var g service.PoolGate
	var valid bool
	err := r.db.QueryRowContext(ctx, `SELECT p.id,COALESCE(m.id,0),p.concurrency,
 COALESCE(p.total_tokens/p.seats-m.tokens_used-(SELECT COALESCE(SUM(reserved_tokens),0) FROM pool_requests WHERE member_id=m.id AND status='pending'),0),
 COALESCE(p.status='active' AND p.expires_at>NOW() AND m.status='joined' AND m.user_id=$2 AND m.api_key_id=$1 AND p.group_id=$3 AND EXISTS(SELECT 1 FROM pool_resources pr JOIN accounts a ON a.id=pr.account_id WHERE pr.group_id=p.group_id AND a.status='active' AND a.deleted_at IS NULL) AND EXISTS(SELECT 1 FROM api_keys k WHERE k.id=$1 AND k.status='active' AND k.deleted_at IS NULL),false)
 FROM pool_orders p LEFT JOIN pool_members m ON m.order_id=p.id AND m.api_key_id=$1
 WHERE p.group_id=$3 OR p.id=(SELECT order_id FROM pool_members WHERE api_key_id=$1)
 UNION ALL SELECT 0,0,0,0,false FROM pool_resources pr WHERE pr.group_id=$3 AND NOT EXISTS(SELECT 1 FROM pool_orders WHERE group_id=$3) LIMIT 1`, kid, uid, gid).Scan(&g.OrderID, &g.MemberID, &g.Concurrency, &g.TokensRemaining, &valid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, service.ErrPoolAccess
	}
	return &g, nil
}
func (r *poolRepository) Reserve(ctx context.Context, mid, tokens int64) (string, error) {
	if tokens <= 0 || tokens > 2000000 {
		return "", service.ErrPoolLimit
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// 固定锁序：订单→成员。与退款一致，避免退款与请求互锁。
	var valid bool
	var concurrency int
	var tokenLimit, reqLimit int64
	err = tx.QueryRowContext(ctx, `SELECT p.status='active' AND p.expires_at>NOW(),p.concurrency,p.total_tokens/p.seats,p.total_requests/p.seats FROM pool_orders p JOIN pool_members m ON m.order_id=p.id WHERE m.id=$1 FOR UPDATE OF p`, mid).Scan(&valid, &concurrency, &tokenLimit, &reqLimit)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", service.ErrPoolAccess
	}
	var used, requests int64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT tokens_used,requests_used,status FROM pool_members WHERE id=$1 FOR UPDATE`, mid).Scan(&used, &requests, &status); err != nil {
		return "", err
	}
	if status != "joined" {
		return "", service.ErrPoolAccess
	}
	// 崩溃遗留的预占不会自动归还：转为保守扣量，防止利用断线绕过配额。
	var stale int64
	err = tx.QueryRowContext(ctx, `WITH stale AS (UPDATE pool_requests SET status='uncertain',finished_at=NOW() WHERE member_id=$1 AND status='pending' AND created_at<NOW()-INTERVAL '1 hour' RETURNING reserved_tokens) SELECT COALESCE(SUM(reserved_tokens),0) FROM stale`, mid).Scan(&stale)
	if err != nil {
		return "", err
	}
	if stale > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE pool_members SET tokens_used=tokens_used+$2 WHERE id=$1`, mid, stale); err != nil {
			return "", err
		}
		used += stale
	}
	var pending int
	var reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(reserved_tokens),0) FROM pool_requests WHERE member_id=$1 AND status='pending'`, mid).Scan(&pending, &reserved); err != nil {
		return "", err
	}
	if used+reserved+tokens > tokenLimit || requests >= reqLimit || pending >= concurrency {
		return "", service.ErrPoolLimit
	}
	id := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO pool_requests(id,member_id,reserved_tokens) VALUES($1,$2,$3)`, id, mid, tokens); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pool_members SET requests_used=requests_used+1 WHERE id=$1`, mid); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func (r *poolRepository) Finish(ctx context.Context, id string, _ bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var mid int64
	// 与计费结算使用同一锁序，先成员后请求。
	err = tx.QueryRowContext(ctx, `SELECT m.id FROM pool_members m JOIN pool_requests q ON q.member_id=m.id WHERE q.id=$1 FOR UPDATE OF m`, id).Scan(&mid)
	if err != nil {
		return err
	}
	var tokens int64
	err = tx.QueryRowContext(ctx, `UPDATE pool_requests SET status='uncertain',finished_at=NOW() WHERE id=$1 AND status='pending' RETURNING reserved_tokens`, id).Scan(&tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pool_members SET tokens_used=tokens_used+$2 WHERE id=$1`, mid, tokens); err != nil {
		return err
	}
	return tx.Commit()
}

func settlePoolRequest(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd.PoolReservationID == "" {
		return nil
	}
	var mid int64
	err := tx.QueryRowContext(ctx, `SELECT m.id FROM pool_members m JOIN pool_requests q ON q.member_id=m.id WHERE q.id=$1 AND m.api_key_id=$2 FOR UPDATE OF m`, cmd.PoolReservationID, cmd.APIKeyID).Scan(&mid)
	if err != nil {
		return err
	}
	var status string
	var reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT status,reserved_tokens FROM pool_requests WHERE id=$1 FOR UPDATE`, cmd.PoolReservationID).Scan(&status, &reserved); err != nil {
		return err
	}
	if status == "settled" {
		return nil
	}
	var actual int64
	for _, v := range []int{cmd.InputTokens, cmd.OutputTokens, cmd.CacheCreationTokens, cmd.CacheReadTokens} {
		if v < 0 || int64(v) > 1000000000 {
			return errors.New("invalid pool usage tokens")
		}
		actual += int64(v)
	}
	delta := actual
	if status == "uncertain" {
		delta -= reserved
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pool_members SET tokens_used=tokens_used+$2 WHERE id=$1`, mid, delta); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE pool_requests SET status='settled',actual_tokens=$2,finished_at=NOW() WHERE id=$1`, cmd.PoolReservationID, actual)
	return err
}

func (r *poolRepository) Resources(ctx context.Context) ([]service.PoolResource, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT pr.id,pr.group_id,pr.account_id,a.name,a.type,a.status,(a.extra->>'codex_5h_used_percent')::float8,(a.extra->>'codex_7d_used_percent')::float8 FROM pool_resources pr JOIN accounts a ON a.id=pr.account_id WHERE a.deleted_at IS NULL ORDER BY pr.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PoolResource{}
	for rows.Next() {
		var p service.PoolResource
		if err = rows.Scan(&p.ID, &p.GroupID, &p.AccountID, &p.Name, &p.Type, &p.Status, &p.Used5h, &p.Used7d); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *poolRepository) CreateResource(ctx context.Context, p service.PoolResourceCreate) (int64, error) {
	if p.Platform != "" && p.Platform != "openai" {
		return 0, service.ErrPoolConfig
	}
	if p.RateMultiplier != nil && (*p.RateMultiplier < 0 || math.IsNaN(*p.RateMultiplier) || math.IsInf(*p.RateMultiplier, 0)) {
		return 0, service.ErrPoolConfig
	}
	var expires *time.Time
	if p.ExpiresAt != nil {
		v := time.Unix(*p.ExpiresAt, 0)
		expires = &v
	}

	var cred map[string]any
	if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 80 || p.Concurrency < 1 || p.Concurrency > 50 || len(p.Credentials) > 65536 || json.Unmarshal(p.Credentials, &cred) != nil {
		return 0, service.ErrPoolConfig
	}
	field := "access_token"
	if p.Type == "apikey" {
		field = "api_key"
	} else if p.Type != "oauth" {
		return 0, service.ErrPoolConfig
	}
	if token, ok := cred[field].(string); !ok || strings.TrimSpace(token) == "" {
		return 0, service.ErrPoolConfig
	}
	if raw, ok := cred["base_url"].(string); ok && raw != "" {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return 0, service.ErrPoolConfig
		}
	}
	if len(p.Extra) == 0 {
		p.Extra = json.RawMessage(`{}`)
	}
	if !json.Valid(p.Extra) || len(p.Extra) > 65536 {
		return 0, service.ErrPoolConfig
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var aid, gid, id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,is_exclusive,allow_messages_dispatch) VALUES($1,'openai','subscription',true,true) RETURNING id`, "拼单 / "+p.Name+" / "+uuid.NewString()[:8]).Scan(&gid)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,concurrency,extra,proxy_id,notes,priority,rate_multiplier,load_factor,expires_at,auto_pause_on_expired) VALUES($1,'openai',$2,$3,$4,$5,$6,$7,$8,COALESCE($9,1),$10,$11,COALESCE($12,true)) RETURNING id`, p.Name, p.Type, []byte(p.Credentials), p.Concurrency, []byte(p.Extra), p.ProxyID, p.Notes, p.Priority, p.RateMultiplier, p.LoadFactor, expires, p.AutoPauseOnExpired).Scan(&aid)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO pool_resources(account_id,group_id) VALUES($1,$2) RETURNING id`, aid, gid).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, aid, gid); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (r *poolRepository) SetResourceStatus(ctx context.Context, id int64, status string) error {
	if status != "active" && status != "disabled" {
		return service.ErrPoolConfig
	}
	result, err := r.db.ExecContext(ctx, `UPDATE accounts a SET status=$2,updated_at=NOW() FROM pool_resources pr WHERE pr.id=$1 AND a.id=pr.account_id AND a.deleted_at IS NULL`, id, status)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrPoolConfig
	}
	return nil
}

// 订单行锁串行化发货、取消与重试；资源锁确保一个账号同时只服务一车。
func (r *poolRepository) Deliver(ctx context.Context, oid, resourceID int64) ([]int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	var assigned sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT status,resource_id FROM pool_orders WHERE id=$1 FOR UPDATE`, oid).Scan(&status, &assigned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPoolClosed
	}
	if err != nil {
		return nil, err
	}
	if status == "active" && assigned.Valid && assigned.Int64 == resourceID {
		return []int64{}, nil
	}
	if status != "awaiting_delivery" {
		return nil, service.ErrPoolClosed
	}
	p := service.PoolCreate{}
	// 账号可在旧车到期/终止后继续开新车。为新周期分配新组，保留旧 Key 与账本。
	var aid, rid int64
	err = tx.QueryRowContext(ctx, `SELECT id,account_id,group_id FROM pool_resources WHERE id=$1 FOR UPDATE`, resourceID).Scan(&rid, &aid, &p.GroupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPoolConfig
	}
	if err != nil {
		return nil, err
	}
	var previous int64
	var ended bool
	err = tx.QueryRowContext(ctx, `SELECT id,status='cancelled' OR (status='active' AND expires_at<=NOW()) FROM pool_orders WHERE group_id=$1 FOR UPDATE`, p.GroupID).Scan(&previous, &ended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if !ended {
			return nil, service.ErrPoolClosed
		}
		var pending bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pool_requests q JOIN pool_members m ON m.id=q.member_id WHERE m.order_id=$1 AND q.status='pending')`, previous).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, service.ErrPoolLimit
		}
		oldGroup := p.GroupID
		err = tx.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,is_exclusive,allow_messages_dispatch) VALUES($1,'openai','subscription',true,true) RETURNING id`, "拼单 / "+uuid.NewString()).Scan(&p.GroupID)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_resources SET group_id=$2 WHERE id=$1`, rid, p.GroupID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id=$1`, aid); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, aid, p.GroupID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,group_id) VALUES('group_changed',$1),('group_changed',$2)`, oldGroup, p.GroupID); err != nil {
			return nil, err
		}
	}
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT platform='openai' AND subscription_type='subscription' AND is_exclusive AND status='active'
 AND fallback_group_id IS NULL AND fallback_group_id_on_invalid_request IS NULL AND EXISTS(SELECT 1 FROM pool_resources WHERE group_id=g.id) AND NOT EXISTS(SELECT 1 FROM api_keys WHERE group_id=g.id AND deleted_at IS NULL)
 AND NOT EXISTS(SELECT 1 FROM user_subscriptions WHERE group_id=g.id AND deleted_at IS NULL)
 AND EXISTS(SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id WHERE ag.group_id=g.id AND a.deleted_at IS NULL AND a.status='active')
 FROM groups g WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, p.GroupID).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !valid {
		return nil, service.ErrPoolConfig
	}
	if err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `UPDATE pool_orders SET group_id=$2,resource_id=$3 WHERE id=$1`, oid, p.GroupID, resourceID); err != nil {
		return nil, err
	}
	gid := p.GroupID
	if _, err = tx.ExecContext(ctx, `UPDATE pool_orders SET status='active',starts_at=NOW(),expires_at=NOW()+duration_hours*INTERVAL '1 hour' WHERE id=$1`, oid); err != nil {
		return nil, err
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,user_id FROM pool_members WHERE order_id=$1 AND status='joined' ORDER BY user_id`, oid)
	if e != nil {
		return nil, e
	}
	type member struct{ id, uid int64 }
	members := []member{}
	for rows.Next() {
		var m member
		if e = rows.Scan(&m.id, &m.uid); e != nil {
			rows.Close()
			return nil, e
		}
		members = append(members, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	affected := []int64{}
	for _, m := range members {
		b := make([]byte, 24)
		if _, err = rand.Read(b); err != nil {
			return nil, err
		}
		key := "sk-pool-" + hex.EncodeToString(b)
		var kid int64
		err = tx.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id,expires_at) SELECT $1,$2,$3,group_id,expires_at FROM pool_orders WHERE id=$4 RETURNING id`, m.uid, key, fmt.Sprintf("拼单 #%d 专属 Key", oid), oid).Scan(&kid)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, m.uid, gid); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,notes) SELECT $1,$2,starts_at,expires_at,'拼单自动开通' FROM pool_orders WHERE id=$3`, m.uid, gid, oid); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE pool_members SET api_key_id=$1 WHERE id=$2`, kid, m.id); err != nil {
			return nil, err
		}
		affected = append(affected, m.uid)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pool_notifications(order_id,user_id,kind) SELECT order_id,user_id,'delivered' FROM pool_members WHERE order_id=$1 AND status='joined'`, oid); err != nil {
		return nil, err
	}
	return affected, tx.Commit()
}

func (r *poolRepository) Notifications(ctx context.Context, uid int64, admin bool) ([]service.PoolNotification, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT n.id,n.order_id,p.title,n.kind,p.delivery_deadline,EXISTS(SELECT 1 FROM pool_notification_reads WHERE notification_id=n.id AND user_id=$1) FROM pool_notifications n JOIN pool_orders p ON p.id=n.order_id WHERE (n.user_id=$1 OR ($2 AND n.user_id IS NULL AND p.status='awaiting_delivery')) ORDER BY n.id DESC LIMIT 100`, uid, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PoolNotification{}
	for rows.Next() {
		var n service.PoolNotification
		if err = rows.Scan(&n.ID, &n.OrderID, &n.Title, &n.Kind, &n.Deadline, &n.Read); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func (r *poolRepository) ReadNotification(ctx context.Context, id, uid int64, admin bool) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO pool_notification_reads(notification_id,user_id) SELECT id,$2 FROM pool_notifications WHERE id=$1 AND (user_id=$2 OR ($3 AND user_id IS NULL)) ON CONFLICT DO NOTHING`, id, uid, admin)
	return err
}

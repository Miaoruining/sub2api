package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const productColumns = `id,title,description,seats,price,duration_days,formation_days,total_tokens,total_requests,concurrency,status,version,quota_mode,plan_type,total_credit,credit_5h,credit_7d`

func scanProduct(row interface{ Scan(...any) error }) (service.PoolProduct, error) {
	var p service.PoolProduct
	err := row.Scan(&p.ID, &p.Title, &p.Description, &p.Seats, &p.Price, &p.DurationDays, &p.FormationDays, &p.TotalTokens, &p.TotalRequests, &p.Concurrency, &p.Status, &p.Version, &p.QuotaMode, &p.PlanType, &p.TotalCredit, &p.Credit5h, &p.Credit7d)
	return p, err
}
func (r *poolRepository) Products(ctx context.Context, admin bool) ([]service.PoolProduct, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+productColumns+` FROM pool_products WHERE $1 OR status='active' ORDER BY id DESC`, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PoolProduct{}
	for rows.Next() {
		p, e := scanProduct(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *poolRepository) SaveProduct(ctx context.Context, id int64, p service.PoolProduct) (int64, error) {
	if p.QuotaMode == "credits" {
		p.TotalTokens = int64(p.Seats) * 8192
		p.TotalRequests = int64(p.Seats)
	}
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if p.QuotaMode == "" {
		p.QuotaMode = "tokens"
	}
	if p.PlanType == "" {
		p.PlanType = "plus"
	}
	args := []any{p.Title, p.Description, p.Seats, p.Price, p.DurationDays, p.FormationDays, p.TotalTokens, p.TotalRequests, p.Concurrency, p.Status, p.QuotaMode, p.PlanType, p.TotalCredit, p.Credit5h, p.Credit7d}
	var saved int64
	var err error
	if id == 0 {
		err = r.db.QueryRowContext(ctx, `INSERT INTO pool_products(title,description,seats,price,duration_days,formation_days,total_tokens,total_requests,concurrency,status,quota_mode,plan_type,total_credit,credit_5h,credit_7d) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`, args...).Scan(&saved)
	} else {
		args = append(args, id, p.Version)
		err = r.db.QueryRowContext(ctx, `UPDATE pool_products SET title=$1,description=$2,seats=$3,price=$4,duration_days=$5,formation_days=$6,total_tokens=$7,total_requests=$8,concurrency=$9,status=$10,quota_mode=$11,plan_type=$12,total_credit=$13,credit_5h=$14,credit_7d=$15,version=version+1,updated_at=NOW() WHERE id=$16 AND version=$17 RETURNING id`, args...).Scan(&saved)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrPoolProductChanged
	}
	return saved, err
}

// 商品、付款和首席位在一个事务中提交。请求号在同一用户内唯一，网络重试不会重复开团扣款。
func (r *poolRepository) PurchaseProduct(ctx context.Context, pid, uid int64, requestID string, version int64) (int64, error) {
	parsed, err := uuid.Parse(requestID)
	if err != nil || parsed == uuid.Nil {
		return 0, service.ErrPoolConfig
	}
	requestID = parsed.String()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// 相同用户的同一请求串行；用户行锁同时保护余额，后续订单锁仅针对新建订单。
	var user int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&user); err != nil {
		return 0, service.ErrPoolAccess
	}
	var oid, previousProduct int64
	err = tx.QueryRowContext(ctx, `SELECT order_id,product_id FROM pool_product_purchases WHERE user_id=$1 AND request_id=$2`, uid, requestID).Scan(&oid, &previousProduct)
	if err == nil {
		if previousProduct != pid {
			return 0, service.ErrPoolConfig
		}
		return oid, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	p, err := scanProduct(tx.QueryRowContext(ctx, `SELECT `+productColumns+` FROM pool_products WHERE id=$1 FOR SHARE`, pid))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrPoolProductChanged
	}
	if err != nil {
		return 0, err
	}
	if p.Status != "active" || p.Version != version {
		return 0, service.ErrPoolProductChanged
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO pool_orders(product_id,title,seats,price,duration_hours,total_tokens,total_requests,concurrency,join_deadline,quota_mode,plan_type,total_credit,credit_5h,credit_7d) VALUES($1,$2,$3,$4,$5*24,$6,$7,$8,NOW()+$9*INTERVAL '1 day',$10,$11,$12,$13,$14) RETURNING id`, p.ID, p.Title, p.Seats, p.Price, p.DurationDays, p.TotalTokens, p.TotalRequests, p.Concurrency, p.FormationDays, p.QuotaMode, p.PlanType, p.TotalCredit, p.Credit5h, p.Credit7d).Scan(&oid)
	if err != nil {
		return 0, err
	}
	if _, err = joinPoolOrder(ctx, tx, oid, uid); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pool_product_purchases(user_id,request_id,product_id,order_id) VALUES($1,$2,$3,$4)`, uid, requestID, pid, oid); err != nil {
		return 0, err
	}
	return oid, tx.Commit()
}

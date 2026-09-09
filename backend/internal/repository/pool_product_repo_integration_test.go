//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func productFixture(t *testing.T) (*poolRepository, service.PoolProduct, []int64) {
	t.Helper()
	r, _, u := poolFixture(t)
	p := service.PoolProduct{Title: "Plus 2 人团", Description: "独立 Key", Seats: 2, Price: 10, DurationDays: 30, FormationDays: 2, TotalTokens: 2000000, TotalRequests: 2000, Concurrency: 1, Status: "active"}
	id, err := r.SaveProduct(context.Background(), 0, p)
	require.NoError(t, err)
	p.ID = id
	p.Version = 1
	return r, p, u
}
func TestPoolProductFirstPaymentAtomicAndRetry(t *testing.T) {
	r, p, u := productFixture(t)
	ctx := context.Background()
	req := uuid.NewString()
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_orders WHERE product_id=$1`, p.ID).Scan(&n))
	require.Zero(t, n)
	var wg sync.WaitGroup
	ids := make(chan int64, 5)
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := r.PurchaseProduct(ctx, p.ID, u[0], req, p.Version)
			ids <- id
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var oid int64
	for id := range ids {
		if oid != 0 {
			require.Equal(t, oid, id)
		}
		oid = id
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[0]).Scan(&balance))
	require.Equal(t, 90.0, balance)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_orders WHERE product_id=$1`, p.ID).Scan(&n))
	require.Equal(t, 1, n)
	var hours int
	var status string
	var pending bool
	require.NoError(t, integrationDB.QueryRow(`SELECT duration_hours,status,starts_at IS NULL AND group_id IS NULL AND join_deadline>NOW()+INTERVAL '47 hours' FROM pool_orders WHERE id=$1`, oid).Scan(&hours, &status, &pending))
	require.Equal(t, 720, hours)
	require.Equal(t, "forming", status)
	require.True(t, pending)
	_, err := r.Join(ctx, oid, u[1])
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRow(`SELECT status FROM pool_orders WHERE id=$1`, oid).Scan(&status))
	require.Equal(t, "awaiting_delivery", status)
	notes, err := r.Notifications(ctx, u[2], true)
	require.NoError(t, err)
	found := false
	for _, note := range notes {
		if note.OrderID == oid {
			found = true
		}
	}
	require.True(t, found)
}
func TestPoolProductInsufficientBalanceDoesNotCreateOrder(t *testing.T) {
	r, p, u := productFixture(t)
	_, err := integrationDB.Exec(`UPDATE users SET balance=1 WHERE id=$1`, u[0])
	require.NoError(t, err)
	_, err = r.PurchaseProduct(context.Background(), p.ID, u[0], uuid.NewString(), p.Version)
	require.ErrorIs(t, err, service.ErrPoolBalance)
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_orders WHERE product_id=$1`, p.ID).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM pool_product_purchases WHERE product_id=$1`, p.ID).Scan(&n))
	require.Zero(t, n)
}
func TestPoolProductEditsKeepPaidOrderSnapshot(t *testing.T) {
	r, p, u := productFixture(t)
	ctx := context.Background()
	req := uuid.NewString()
	oid, err := r.PurchaseProduct(ctx, p.ID, u[0], req, p.Version)
	require.NoError(t, err)
	p.Price = 20
	p.DurationDays = 60
	p.Title = "Plus 新套餐"
	_, err = r.SaveProduct(ctx, p.ID, p)
	require.NoError(t, err)
	_, err = r.PurchaseProduct(ctx, p.ID, u[1], uuid.NewString(), 1)
	require.Error(t, err)
	retry, err := r.PurchaseProduct(ctx, p.ID, u[0], req, 1)
	require.NoError(t, err)
	require.Equal(t, oid, retry)
	var price float64
	var hours int
	var title string
	require.NoError(t, integrationDB.QueryRow(`SELECT price,duration_hours,title FROM pool_orders WHERE id=$1`, oid).Scan(&price, &hours, &title))
	require.Equal(t, 10.0, price)
	require.Equal(t, 720, hours)
	require.Equal(t, "Plus 2 人团", title)
	p.Version = 2
	p.Status = "disabled"
	_, err = r.SaveProduct(ctx, p.ID, p)
	require.NoError(t, err)
	_, err = r.PurchaseProduct(ctx, p.ID, u[1], uuid.NewString(), 3)
	require.Error(t, err)
	products, err := r.Products(ctx, false)
	require.NoError(t, err)
	for _, x := range products {
		require.NotEqual(t, p.ID, x.ID)
	}
	// 已开团订单仍使用原条款，商品下架不取消已付席位。
	_, err = r.Join(ctx, oid, u[1])
	require.NoError(t, err)
}
func TestPoolProductValidationAndIndependentNewGroups(t *testing.T) {
	r, p, u := productFixture(t)
	ctx := context.Background()
	bad := p
	bad.DurationDays = 0
	_, err := r.SaveProduct(ctx, 0, bad)
	require.Error(t, err)
	bad = p
	bad.FormationDays = 91
	_, err = r.SaveProduct(ctx, 0, bad)
	require.Error(t, err)
	_, err = r.PurchaseProduct(ctx, p.ID, u[0], "invalid", 1)
	require.Error(t, err)
	first, err := r.PurchaseProduct(ctx, p.ID, u[0], uuid.NewString(), 1)
	require.NoError(t, err)
	second, err := r.PurchaseProduct(ctx, p.ID, u[1], uuid.NewString(), 1)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	_, err = r.Leave(ctx, first, u[0])
	require.NoError(t, err)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u[0]).Scan(&balance))
	require.Equal(t, 100.0, balance)
	_, err = integrationDB.Exec(`UPDATE pool_orders SET join_deadline=$2 WHERE id=$1`, second, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	_, err = r.Join(ctx, second, u[2])
	require.ErrorIs(t, err, service.ErrPoolClosed)
}

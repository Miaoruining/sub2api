//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type lotteryHandlerRepo struct {
	uid    int64
	called bool
}

func (r *lotteryHandlerRepo) Status(_ context.Context, uid int64) (*service.LotteryStatus, error) {
	r.uid = uid
	return &service.LotteryStatus{State: "ready", Prizes: service.LotteryPrizes(), History: []service.LotteryDraw{}}, nil
}
func (r *lotteryHandlerRepo) Draw(_ context.Context, uid int64, date string, _ ...string) (*service.LotteryDraw, error) {
	r.uid = uid
	r.called = true
	return &service.LotteryDraw{ID: 1, ActivityDate: date, Prize: 1}, nil
}
func (r *lotteryHandlerRepo) AdminStatus(context.Context, string) (*service.LotteryAdminStatus, error) {
	r.called = true
	return &service.LotteryAdminStatus{DailyBudget: 100, Weights: []int{9552, 400, 30, 10, 5, 2, 1}}, nil
}
func (r *lotteryHandlerRepo) UpdateConfig(context.Context, int64, service.LotteryConfigUpdate) error {
	r.called = true
	return nil
}

func lotteryContext(t *testing.T, body string, uid int64, role string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/lottery/draw", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if uid > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: uid})
		c.Set(string(middleware.ContextKeyUserRole), role)
	}
	return c, w
}
func TestLotteryHandlerUsesAuthenticatedUserAndHidesInternals(t *testing.T) {
	repo := &lotteryHandlerRepo{}
	h := NewLotteryHandler(service.NewLotteryService(repo, nil, nil))
	c, w := lotteryContext(t, `{"activity_date":"2038-01-02","user_id":999,"prize":100}`, 42, "user")
	h.Draw(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), repo.uid)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, float64(1), result["data"].(map[string]any)["prize"])
	c, w = lotteryContext(t, "", 42, "user")
	h.Status(c)
	require.Equal(t, 200, w.Code)
	for _, field := range []string{"weights", "budget", "spent", "ticket", "distribution", "balance_after"} {
		require.NotContains(t, w.Body.String(), field)
	}
}
func TestLotteryHandlerRejectsAnonymousAndMalformedDate(t *testing.T) {
	repo := &lotteryHandlerRepo{}
	h := NewLotteryHandler(service.NewLotteryService(repo, nil, nil))
	c, w := lotteryContext(t, `{"activity_date":"2038-01-02"}`, 0, "")
	h.Draw(c)
	require.Equal(t, 401, w.Code)
	require.False(t, repo.called)
	for _, body := range []string{`{}`, `{"activity_date":"not-a-date"}`, `{"activity_date":999}`, `{"activity_date":"2038-01-02","request_id":"invalid"}`} {
		c, w = lotteryContext(t, body, 42, "user")
		h.Draw(c)
		require.GreaterOrEqual(t, w.Code, 400)
		require.False(t, repo.called)
	}
}
func TestLotteryAdminHandlersRejectNormalUsers(t *testing.T) {
	repo := &lotteryHandlerRepo{}
	h := NewLotteryHandler(service.NewLotteryService(repo, nil, nil))
	for _, action := range []func(*gin.Context){h.AdminStatus, h.UpdateConfig} {
		c, w := lotteryContext(t, `{"admin_repeat_enabled":true}`, 42, "user")
		action(c)
		require.Equal(t, 403, w.Code)
		require.False(t, repo.called)
	}
	c, w := lotteryContext(t, `{"enabled":true}`, 1, "admin")
	h.UpdateConfig(c)
	require.Equal(t, 200, w.Code)
	require.True(t, repo.called)
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type poolDynamicUsageHandlerRepo struct {
	service.PoolRepository
	usage     []service.PoolDynamicUsage
	err       error
	gotUserID int64
}

func (r *poolDynamicUsageHandlerRepo) DynamicAccounts(context.Context) ([]int64, error) {
	return nil, nil
}

func (r *poolDynamicUsageHandlerRepo) ApplyDynamicSnapshot(context.Context, service.PoolDynamicSnapshot) error {
	return nil
}

func (r *poolDynamicUsageHandlerRepo) DynamicUsage(_ context.Context, userID int64) ([]service.PoolDynamicUsage, error) {
	r.gotUserID = userID
	return r.usage, r.err
}

type poolLegacyHandlerRepo struct {
	service.PoolRepository
}

var _ service.PoolRepository = (*poolDynamicUsageHandlerRepo)(nil)
var _ service.PoolDynamicRepository = (*poolDynamicUsageHandlerRepo)(nil)
var _ service.PoolRepository = (*poolLegacyHandlerRepo)(nil)

func newPoolDynamicHandlerContext(path string, userID *int64) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	if userID != nil {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: *userID})
	}
	return c, recorder
}

func TestPoolOrderHandlerDynamicUsageRequiresLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPoolOrderHandler(&poolLegacyHandlerRepo{}, nil, nil, nil)
	c, recorder := newPoolDynamicHandlerContext("/api/v1/pool-dynamic-usage", nil)

	h.DynamicUsage(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	var response struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestPoolOrderHandlerDynamicUsageUsesAuthenticatedSubjectAndReturnsWindows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createdAt := time.Date(2026, 9, 9, 8, 30, 0, 0, time.UTC)
	want := []service.PoolDynamicUsage{{
		ID:        "request-1",
		OrderID:   17,
		KeyID:     23,
		Credit:    1.25,
		Status:    "settled",
		CreatedAt: createdAt,
		Windows: []service.PoolDynamicQuotaWindow{{
			Key:                     "codex/300",
			AccountRemainingPercent: 72,
			UsedPercent:             28,
			ReservedPercent:         4,
			RemainingPercent:        68,
			EntitlementPercent:      25,
			ResetAt:                 createdAt.Add(time.Hour),
			ObservedAt:              createdAt.Add(-time.Minute),
			Status:                  "active",
		}},
	}}
	repo := &poolDynamicUsageHandlerRepo{usage: want}
	h := NewPoolOrderHandler(repo, nil, nil, nil)
	userID := int64(42)
	c, recorder := newPoolDynamicHandlerContext("/api/v1/pool-dynamic-usage?user_id=999", &userID)

	h.DynamicUsage(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, userID, repo.gotUserID)
	var response struct {
		Code int                        `json:"code"`
		Data []service.PoolDynamicUsage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 0, response.Code)
	require.Equal(t, want, response.Data)
}

func TestPoolOrderHandlerDynamicUsageReturnsRepositoryError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &poolDynamicUsageHandlerRepo{err: errors.New("dynamic usage database unavailable")}
	h := NewPoolOrderHandler(repo, nil, nil, nil)
	userID := int64(42)
	c, recorder := newPoolDynamicHandlerContext("/api/v1/pool-dynamic-usage", &userID)

	h.DynamicUsage(c)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestPoolOrderHandlerDynamicUsageKeepsLegacyRepositoryCompatible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPoolOrderHandler(&poolLegacyHandlerRepo{}, nil, nil, nil)
	userID := int64(42)
	c, recorder := newPoolDynamicHandlerContext("/api/v1/pool-dynamic-usage", &userID)

	h.DynamicUsage(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Code int                        `json:"code"`
		Data []service.PoolDynamicUsage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 0, response.Code)
	require.Empty(t, response.Data)
}

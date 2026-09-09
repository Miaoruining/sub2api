package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dynamicGateSpy struct {
	service.PoolRepository
	mode     string
	credits  []float64
	model    any
	finished bool
}

func (s *dynamicGateSpy) Gate(context.Context, int64, int64, int64) (*service.PoolGate, error) {
	return &service.PoolGate{MemberID: 1, QuotaMode: s.mode, TokensRemaining: 2000000}, nil
}
func (s *dynamicGateSpy) Reserve(ctx context.Context, _ int64, _ int64, credits ...float64) (string, error) {
	s.credits = credits
	s.model = ctx.Value(service.PoolStandardPricingContextKey{})
	return "reservation", nil
}
func (s *dynamicGateSpy) Finish(context.Context, string, bool) error { s.finished = true; return nil }

func TestPoolDynamicMiddlewareUsesStandardEstimateAndModelBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		mode, model string
		status      int
	}{
		{"dynamic", "gpt-5.4", 200}, {"dynamic_shadow", "gpt-5.4", 200},
		{"dynamic", "gpt-5.3-codex-spark", 400}, {"dynamic_shadow", "gpt-5.3-codex-spark", 200},
	} {
		t.Run(tc.mode+tc.model, func(t *testing.T) {
			spy := &dynamicGateSpy{mode: tc.mode}
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 1})
				c.Set(string(ContextKeySubscription), &service.UserSubscription{})
				c.Next()
			})
			engine.Use(PoolQuota(spy, func(context.Context, *service.APIKey, []byte, int64) (float64, error) { return 0.123, nil }))
			engine.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusOK) })
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+tc.model+`","input":"test"}`))
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			if tc.status == 200 {
				require.Equal(t, []float64{0.123}, spy.credits)
				require.Equal(t, tc.model, spy.model)
				require.True(t, spy.finished)
			} else {
				require.Empty(t, spy.credits)
				require.False(t, spy.finished)
			}
		})
	}
}

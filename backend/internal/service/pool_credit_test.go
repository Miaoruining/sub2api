//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestPoolCreditConfigPlanRules(t *testing.T) {
	p := PoolCreditConfig{QuotaMode: "credits", PlanType: "plus", TotalCredit: 100, Credit5h: 10, Credit7d: 50}
	require.True(t, p.ValidateCredits())
	p.PlanType = "pro"
	require.False(t, p.ValidateCredits())
	p.Credit5h = 0
	p.Credit7d = 0
	require.True(t, p.ValidateCredits())
	p.TotalCredit = math.NaN()
	require.False(t, p.ValidateCredits())
	p.TotalCredit = 100
	p.PlanType = "free"
	require.False(t, p.ValidateCredits())
}
func TestPoolCreditEstimationUsesModelPriceAndOutputBudget(t *testing.T) {
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	key := &APIKey{}
	small, e := svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"gpt-5.4-mini","max_output_tokens":1000}`), 10000)
	require.NoError(t, e)
	large, e := svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"gpt-5.4","max_output_tokens":1000}`), 10000)
	require.NoError(t, e)
	require.Greater(t, large, small)
	longer, e := svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"gpt-5.4","max_output_tokens":5000}`), 14000)
	require.NoError(t, e)
	require.Greater(t, longer, large)
	_, e = svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"nonexistent-pool-test-model","max_output_tokens":1000}`), 10000)
	require.Error(t, e)
}

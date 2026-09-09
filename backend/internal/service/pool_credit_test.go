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

func TestPoolStandardCatalogAndSettlement(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	// 普通中转的默认价被修改也不能改变拼团官方价。
	billing.fallbackPrices["gpt-5.4"].InputPricePerToken = 999
	cost, err := billing.calculatePoolStandardCost("gpt-5.4", UsageTokens{InputTokens: 10000, OutputTokens: 2000})
	require.NoError(t, err)
	require.InDelta(t, 0.055, cost.TotalCost, 1e-10)
	require.Equal(t, cost.TotalCost, cost.ActualCost)
	// 输入互斥，缓存不按普通输入重复计费；独立 cache write 单价。
	cost, err = billing.calculatePoolStandardCost("gpt-5.6-sol", UsageTokens{InputTokens: 1000, CacheReadTokens: 2000, CacheCreationTokens: 3000, OutputTokens: 4000})
	require.NoError(t, err)
	require.InDelta(t, 0.004+0.0008+0.015+0.08, cost.TotalCost, 1e-10)
	for _, count := range []int{272000, 272001} {
		cost, err = billing.calculatePoolStandardCost("gpt-5.4", UsageTokens{CacheReadTokens: count, OutputTokens: 2000})
		require.NoError(t, err)
		if count == 272000 {
			require.False(t, cost.LongContextBillingApplied)
			require.InDelta(t, 0.068+0.03, cost.TotalCost, 1e-10)
		} else {
			require.True(t, cost.LongContextBillingApplied)
			require.InDelta(t, float64(count)*0.5e-6+0.045, cost.TotalCost, 1e-10)
		}
	}
	svc := &OpenAIGatewayService{billingService: billing}
	// 渠道计费模型及返回 Fast 档不影响实际转发模型的 Standard 价。
	fast := "fast"
	cost, err = svc.poolStandardUsageCost(context.Background(), &OpenAIForwardResult{Model: "gpt-5.4-mini", UpstreamModel: "gpt-5.4", BillingModel: "gpt-5.5-pro", ServiceTier: &fast}, UsageTokens{InputTokens: 10000, OutputTokens: 2000})
	require.NoError(t, err)
	require.InDelta(t, 0.055, cost.TotalCost, 1e-10)
	_, err = svc.poolStandardUsageCost(context.Background(), &OpenAIForwardResult{Model: "gpt-5.4", UpstreamModel: "unknown"}, UsageTokens{InputTokens: 10})
	require.Error(t, err)
	p, err := poolModelPrice("gpt-5.4-2026-03-05")
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", p.Model)
	_, err = poolModelPrice("gpt-5.4-expensive-unknown")
	require.Error(t, err)
	for _, p := range PoolPricingCatalog().Models {
		cost, err = billing.calculatePoolStandardCost(p.Model, UsageTokens{InputTokens: 1000000})
		require.NoError(t, err)
		factor := 1.0
		if p.LongContextThreshold > 0 {
			factor = 2
		}
		require.InDelta(t, p.Input*factor, cost.TotalCost, 1e-10)
	}
}

func TestPoolStandardReservationIgnoresTierAndPersonalRate(t *testing.T) {
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	key := &APIKey{Group: &Group{RateMultiplier: 100}}
	standard, err := svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"gpt-5.4","max_output_tokens":2000}`), 12000)
	require.NoError(t, err)
	for _, tier := range []string{"fast", "priority", "flex"} {
		cost, err := svc.EstimatePoolCredit(context.Background(), key, []byte(`{"model":"gpt-5.4","max_output_tokens":2000,"service_tier":"`+tier+`"}`), 12000)
		require.NoError(t, err)
		require.Equal(t, standard, cost)
	}
}

func TestPoolStandardRecordUsageChargesCreditsAndLogsSamePrice(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	gid := int64(1)
	ctx := context.WithValue(context.Background(), PoolReservationContextKey{}, "pool-test")
	ctx = context.WithValue(ctx, PoolStandardPricingContextKey{}, "gpt-5.4")
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{Model: "gpt-5.4", RequestID: "pool-standard-record", Usage: OpenAIUsage{InputTokens: 10000, OutputTokens: 2000}},
		APIKey: &APIKey{ID: 1000, GroupID: &gid, Group: &Group{ID: gid, SubscriptionType: "subscription", RateMultiplier: 20}},
		User:   &User{ID: 2000}, Account: &Account{ID: 3000, Type: AccountTypeAPIKey}, Subscription: &UserSubscription{ID: 4000},
	})
	require.NoError(t, err)
	require.NotNil(t, billingRepo.lastCmd)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 0.055, billingRepo.lastCmd.PoolCreditCost, 1e-10)
	require.Zero(t, billingRepo.lastCmd.BalanceCost)
	require.Equal(t, "pool-test", billingRepo.lastCmd.PoolReservationID)
	require.InDelta(t, 0.055, usageRepo.lastLog.ActualCost, 1e-10)
	require.Equal(t, 1.0, usageRepo.lastLog.RateMultiplier)
}

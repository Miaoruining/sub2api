//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoGroupPriceUsesModelPricingNotOnlyGroupMultiplier(t *testing.T) {
	low, high := .000001, .0001
	svc := &GatewayService{resolver: NewModelPricingResolver(nil, newTestBillingServiceForResolver())}
	groups := []Group{
		{ID: 1, RateMultiplier: .1, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, InputPrice: &high, OutputPrice: &high}}},
		{ID: 2, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, InputPrice: &low, OutputPrice: &low}}},
	}
	ordered := svc.RankAutoGroupCandidates(context.Background(), groups, "claude-sonnet-4", 0, "price")
	require.Equal(t, int64(2), ordered[0].ID, "a smaller multiplier can still be more expensive")
}

func TestAutoGroupPriceUsesPerRequestAndIndependentImageRate(t *testing.T) {
	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage} {
		t.Run(string(mode), func(t *testing.T) {
			cheap, expensive := .1, 1.0
			svc := &GatewayService{resolver: NewModelPricingResolver(nil, newTestBillingServiceForResolver())}
			groups := []Group{
				{ID: 1, RateMultiplier: 1, ImageRateIndependent: true, ImageRateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"test-image"}, BillingMode: mode, PerRequestPrice: &expensive}}},
				{ID: 2, RateMultiplier: 1, ImageRateIndependent: true, ImageRateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"test-image"}, BillingMode: mode, PerRequestPrice: &cheap}}},
			}
			ordered := svc.RankAutoGroupCandidates(context.Background(), groups, "test-image", 0, "price")
			require.Equal(t, int64(2), ordered[0].ID)
			if mode == BillingModeImage {
				groups[1].ImageRateMultiplier = 20
				ordered = svc.RankAutoGroupCandidates(context.Background(), groups, "test-image", 0, "price")
				require.Equal(t, int64(1), ordered[0].ID)
			}
		})
	}
}

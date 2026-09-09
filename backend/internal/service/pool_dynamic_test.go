package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDynamicFairAllocationPreservesEarlierUse(t *testing.T) {
	got := DynamicFairAllocation(98, []float64{30, 0})
	require.InDelta(t, 19, got[0], 1e-8)
	require.InDelta(t, 49, got[1], 1e-8)

	got = DynamicFairAllocation(98, []float64{0, 0, 0})
	for _, v := range got {
		require.InDelta(t, 98.0/3, v, 1e-8)
	}
}

func TestDynamicCreditPercentageColdStartAndCalibration(t *testing.T) {
	require.Equal(t, 1.0, DynamicCreditPercentage(0.01))
	require.InDelta(t, 0.25, DynamicCalibratedCreditPercentage(0.1, 2.5, 1), 1e-8)
	require.InDelta(t, 0.0001, DynamicCalibratedCreditPercentage(0.000001, 2.5, 1), 1e-8)
	require.Equal(t, 1.0, DynamicCalibratedCreditPercentage(0.01, 2.5, 0))
}

func TestDynamicModeValidationDoesNotFallbackToLegacyLimits(t *testing.T) {
	now := time.Now()
	p := PoolCreate{PoolCreditConfig: PoolCreditConfig{QuotaMode: "dynamic", PlanType: "plus"}, Title: "动态", Seats: 2, Price: 1, DurationHours: 24, TotalTokens: 0, TotalRequests: 0, Concurrency: 1, JoinDeadline: now.Add(time.Hour)}
	require.NoError(t, p.Validate(now))
	p.TotalTokens = 8192
	require.ErrorIs(t, p.Validate(now), ErrPoolConfig)
}

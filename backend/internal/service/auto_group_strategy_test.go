package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

func TestAutoGroupStrategiesRespectHealthAndPrice(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		want     []int64
	}{
		{"smart", []int64{2, 3, 1, 4}}, {"price", []int64{1, 2, 3, 4}}, {"stable", []int64{3, 2, 1, 4}},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			ranks := []autoRouteRank{
				{group: Group{ID: 1}, price: 1, risk: .7, latency: 1},
				{group: Group{ID: 2}, price: 2, risk: .05, latency: 1},
				{group: Group{ID: 3}, price: 10, risk: 0, latency: 1},
				{group: Group{ID: 4}, price: 0, risk: 0, latency: 1, cooling: true},
			}
			sortAutoRouteRanks(ranks, tc.strategy)
			got := []int64{}
			for _, r := range ranks {
				got = append(got, r.group.ID)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestAutoGroupUnknownPriceIsNotFreeAndDoesNotAddCandidates(t *testing.T) {
	ranks := []autoRouteRank{{group: Group{ID: 1}, price: math.Inf(1)}, {group: Group{ID: 2}, price: 0}}
	sortAutoRouteRanks(ranks, "price")
	require.Equal(t, int64(2), ranks[0].group.ID)
	svc := &GatewayService{}
	groups := []Group{{ID: 2, SortOrder: 100}, {ID: 1, SortOrder: -100}}
	result := svc.RankAutoGroupCandidates(context.Background(), groups, "test", 0, "")
	require.Len(t, result, 2)
	require.Equal(t, int64(2), groups[0].ID, "sorting must not mutate the authorized input")
	require.Equal(t, int64(1), result[0].ID)
	require.Equal(t, "smart", NormalizeRoutingStrategy(""))
	require.False(t, ValidRoutingStrategy("admin"))
}

func TestAutoGroupRecentFailureIsModelScopedAndExpires(t *testing.T) {
	svc := &GatewayService{}
	now := time.Now()
	svc.autoRouteMetrics.record(autoRouteMetricKey{1, "gpt-test"}, true, time.Second, now)
	groups := []Group{{ID: 1}, {ID: 2}}
	require.Equal(t, int64(2), svc.RankAutoGroupCandidates(context.Background(), groups, "gpt-test", 0, "price")[0].ID)
	require.Equal(t, int64(1), svc.RankAutoGroupCandidates(context.Background(), groups, "other", 0, "price")[0].ID)
	svc.autoRouteMetrics.record(autoRouteMetricKey{1, "gpt-test"}, true, time.Second, now.Add(-11*time.Minute))
	// Replace timestamp deterministically; expired data is not a permanent ban.
	v := svc.autoRouteMetrics.values[autoRouteMetricKey{1, "gpt-test"}]
	v.at = now.Add(-11 * time.Minute)
	svc.autoRouteMetrics.values[autoRouteMetricKey{1, "gpt-test"}] = v
	require.Equal(t, int64(1), svc.RankAutoGroupCandidates(context.Background(), groups, "gpt-test", 0, "price")[0].ID)
}

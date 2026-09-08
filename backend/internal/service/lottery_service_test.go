//go:build unit

package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestLotteryProbabilityEveryTicket(t *testing.T) {
	w := []int{9552, 400, 30, 10, 5, 2, 1}
	counts := map[int]int{}
	total := 0
	for ticket := 0; ticket < 10000; ticket++ {
		p, err := LotteryPrizeForTicket(w, ticket, 100)
		require.NoError(t, err)
		counts[p]++
		total += p
	}
	for i, p := range LotteryPrizes() {
		require.Equal(t, w[i], counts[p])
	}
	require.Equal(t, 950, total) // 1000 次基础期望发放 95 额度。
	for ticket := 0; ticket < 10000; ticket++ {
		p, err := LotteryPrizeForTicket(w, ticket, 4)
		require.NoError(t, err)
		require.Contains(t, []int{0, 1}, p)
	}
	p, err := LotteryPrizeForTicket(w, 9999, 99)
	require.NoError(t, err)
	require.Zero(t, p)
}

func TestLotteryRejectsInvalidRulesAndTickets(t *testing.T) {
	for _, w := range [][]int{nil, {10000}, {9553, 400, 30, 10, 5, 2, 1}, {-1, 10001, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0}} {
		require.Error(t, ValidateLotteryWeights(w))
	}
	w := []int{10000, 0, 0, 0, 0, 0, 0}
	for _, ticket := range []int{-1, 10000} {
		_, err := LotteryPrizeForTicket(w, ticket, 100)
		require.Error(t, err)
	}
	for _, budget := range []int{-1, 101} {
		_, err := LotteryPrizeForTicket(w, 0, budget)
		require.Error(t, err)
	}
}

func TestLotteryWindowUsesBeijingIndependentOfServerZone(t *testing.T) {
	for _, test := range []struct {
		utc, date string
		before    bool
	}{
		{"2026-09-08T01:59:59Z", "2026-09-08", true},
		{"2026-09-08T02:00:00Z", "2026-09-08", false},
		{"2026-09-08T15:59:59Z", "2026-09-08", false},
		{"2026-09-08T16:00:00Z", "2026-09-09", true},
	} {
		now, err := time.Parse(time.RFC3339, test.utc)
		require.NoError(t, err)
		date, open, next := LotteryWindow(now.In(time.FixedZone("other", -7*3600)))
		require.Equal(t, test.date, date)
		require.Equal(t, test.before, now.Before(open))
		require.Equal(t, 24*time.Hour, next.Sub(open))
	}
}

func TestLotteryUserDTOHasNoInternalFields(t *testing.T) {
	raw, err := json.Marshal(LotteryStatus{Today: &LotteryDraw{Prize: 100}, History: []LotteryDraw{{Prize: 1}}})
	require.NoError(t, err)
	for _, field := range []string{"weights", "budget", "spent", "ticket", "distribution", "balance_after", "user_id"} {
		require.NotContains(t, string(raw), field)
	}
}

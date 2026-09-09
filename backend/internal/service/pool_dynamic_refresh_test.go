package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolDynamicSnapshotRequiresRealMeasurements(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, raw := range []string{
		`{"limit_window_seconds":18000,"reset_after_seconds":100}`,
		`{"used_percent":null,"limit_window_seconds":18000,"reset_after_seconds":100}`,
		`{"used_percent":0,"reset_after_seconds":100}`,
		`{"used_percent":0,"limit_window_seconds":18000}`,
		`{"used_percent":101,"limit_window_seconds":18000,"reset_after_seconds":100}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var w OpenAIRateLimitWindow
			require.NoError(t, json.Unmarshal([]byte(raw), &w))
			_, err := poolDynamicSnapshotFromUsage(1, &OpenAIQuotaUsage{FetchedAt: now.Unix(), RateLimit: &OpenAIRateLimit{Allowed: true, PrimaryWindow: &w}}, now)
			require.Error(t, err)
		})
	}
	var w OpenAIRateLimitWindow
	require.NoError(t, json.Unmarshal([]byte(`{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":100}`), &w))
	snapshot, err := poolDynamicSnapshotFromUsage(1, &OpenAIQuotaUsage{FetchedAt: now.Unix(), RateLimit: &OpenAIRateLimit{Allowed: true, PrimaryWindow: &w}}, now)
	require.NoError(t, err)
	require.Len(t, snapshot.Windows, 1)
	require.Equal(t, 0.0, snapshot.Windows[0].UsedPercent)
	require.Equal(t, "codex/300", snapshot.Windows[0].Key)
}

func TestPoolDynamicSnapshotDenialDoesNotInventConsumption(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	u := &OpenAIQuotaUsage{PlanType: "pro", FetchedAt: now.Unix(), RateLimit: &OpenAIRateLimit{Allowed: false, LimitReached: true, PrimaryWindow: &OpenAIRateLimitWindow{UsedPercent: 45, LimitWindowSeconds: 604800, ResetAt: now.Add(time.Hour).Unix()}}}
	snapshot, err := poolDynamicSnapshotFromUsage(2, u, now)
	require.NoError(t, err)
	require.True(t, snapshot.Blocked)
	require.Equal(t, 45.0, snapshot.Windows[0].UsedPercent)
	require.Equal(t, "codex/10080", snapshot.Windows[0].Key)
	// Absence of windows is unknown even for a Pro account.
	u.RateLimit.PrimaryWindow = nil
	_, err = poolDynamicSnapshotFromUsage(2, u, now)
	require.Error(t, err)
}

type poolRefreshTestRepo struct {
	PoolDynamicRepository
	claim      bool
	claimErr   error
	applyCount int
}

func (r *poolRefreshTestRepo) ClaimDynamicRefresh(context.Context, int64) (bool, error) {
	return r.claim, r.claimErr
}
func (r *poolRefreshTestRepo) ApplyDynamicSnapshot(context.Context, PoolDynamicSnapshot) error {
	r.applyCount++
	return nil
}

type poolRefreshTestProbe struct {
	calls int
	err   error
}

func (p *poolRefreshTestProbe) QueryPoolDynamicSnapshot(context.Context, int64) (PoolDynamicSnapshot, error) {
	p.calls++
	return PoolDynamicSnapshot{}, p.err
}

func TestPoolDynamicRefreshLeaseAndFailuresNeverPublishFakeBalance(t *testing.T) {
	r := &poolRefreshTestRepo{}
	p := &poolRefreshTestProbe{}
	s := NewPoolDynamicRefreshService(r, p)
	defer s.Stop()
	require.NoError(t, s.Refresh(context.Background(), 1))
	require.Zero(t, p.calls)
	r.claim = true
	p.err = errors.New("upstream unavailable")
	require.Error(t, s.Refresh(context.Background(), 1))
	require.Zero(t, r.applyCount)
	p.err = nil
	require.NoError(t, s.Refresh(context.Background(), 1))
	require.Equal(t, 1, r.applyCount)
}

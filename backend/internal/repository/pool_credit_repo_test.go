//go:build unit

package repository

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPoolCreditWindowUsesUpstreamAndSharedFallback(t *testing.T) {
	anchor := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	now := anchor.Add(6 * time.Hour)
	require.Equal(t, anchor.Add(10*time.Hour), poolCreditReset(now, anchor, "", 5*time.Hour))
	reset := now.Add(time.Hour)
	require.Equal(t, reset, poolCreditReset(now, anchor, reset.Format(time.RFC3339), 5*time.Hour))
	require.Equal(t, now.Add(5*time.Hour), poolCreditReset(now, anchor, now.Format(time.RFC3339), 5*time.Hour))
}

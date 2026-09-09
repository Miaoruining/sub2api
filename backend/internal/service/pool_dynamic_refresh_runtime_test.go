package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type poolDynamicRuntimeRepo struct {
	accountsFn func(context.Context) ([]int64, error)
	accounts   []int64
	claim      bool
	claimErr   error
	applyErr   error
	applyCount int32
}

func (r *poolDynamicRuntimeRepo) DynamicAccounts(ctx context.Context) ([]int64, error) {
	if r.accountsFn != nil {
		return r.accountsFn(ctx)
	}
	return r.accounts, nil
}

func (r *poolDynamicRuntimeRepo) ApplyDynamicSnapshot(context.Context, PoolDynamicSnapshot) error {
	atomic.AddInt32(&r.applyCount, 1)
	return r.applyErr
}

func (r *poolDynamicRuntimeRepo) DynamicUsage(context.Context, int64) ([]PoolDynamicUsage, error) {
	return nil, nil
}

func (r *poolDynamicRuntimeRepo) ClaimDynamicRefresh(context.Context, int64) (bool, error) {
	return r.claim, r.claimErr
}

type poolDynamicRuntimeProbe struct {
	queryFn func(context.Context, int64) (PoolDynamicSnapshot, error)
}

func (p *poolDynamicRuntimeProbe) QueryPoolDynamicSnapshot(ctx context.Context, accountID int64) (PoolDynamicSnapshot, error) {
	return p.queryFn(ctx, accountID)
}

func updateRuntimeMax(max *int32, current int32) {
	for {
		old := atomic.LoadInt32(max)
		if current <= old || atomic.CompareAndSwapInt32(max, old, current) {
			return
		}
	}
}

func TestPoolDynamicRefreshBatchLimitsConcurrencyToFour(t *testing.T) {
	const accountCount = 8
	firstFour := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	defer closeRelease()

	var active int32
	var maxActive int32
	var probeCalls int32
	probe := &poolDynamicRuntimeProbe{queryFn: func(ctx context.Context, accountID int64) (PoolDynamicSnapshot, error) {
		current := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		atomic.AddInt32(&probeCalls, 1)
		updateRuntimeMax(&maxActive, current)
		select {
		case firstFour <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return PoolDynamicSnapshot{AccountID: accountID, ObservedAt: time.Now()}, nil
		case <-ctx.Done():
			return PoolDynamicSnapshot{}, ctx.Err()
		}
	}}

	repo := &poolDynamicRuntimeRepo{accounts: []int64{1, 2, 3, 4, 5, 6, 7, 8}, claim: true}
	service := NewPoolDynamicRefreshService(repo, probe)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		service.refreshBatch(ctx)
		close(done)
	}()

	for i := 0; i < 4; i++ {
		select {
		case <-firstFour:
		case <-ctx.Done():
			t.Fatalf("expected four concurrent probes: %v", ctx.Err())
		}
	}
	closeRelease()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("refresh batch did not finish: %v", ctx.Err())
	}
	require.Equal(t, int32(4), atomic.LoadInt32(&maxActive))
	require.Equal(t, int32(accountCount), atomic.LoadInt32(&probeCalls))
	require.Equal(t, int32(accountCount), atomic.LoadInt32(&repo.applyCount))
}

func TestPoolDynamicRefreshStopCancelsInFlightProbe(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var startedOnce sync.Once
	var cancelledOnce sync.Once
	probe := &poolDynamicRuntimeProbe{queryFn: func(ctx context.Context, accountID int64) (PoolDynamicSnapshot, error) {
		startedOnce.Do(func() { close(started) })
		<-ctx.Done()
		cancelledOnce.Do(func() { close(cancelled) })
		return PoolDynamicSnapshot{}, ctx.Err()
	}}
	repo := &poolDynamicRuntimeRepo{accounts: []int64{1}, claim: true}
	service := NewPoolDynamicRefreshService(repo, probe)
	service.Start()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh worker did not start")
	}
	stopped := make(chan struct{})
	go func() {
		service.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for the in-flight probe")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the in-flight probe")
	}
	service.Stop()
}

func TestPoolDynamicRefreshStartIsIdempotent(t *testing.T) {
	firstScan := make(chan struct{})
	secondScan := make(chan struct{})
	var scans int32
	repo := &poolDynamicRuntimeRepo{claim: true}
	repo.accountsFn = func(ctx context.Context) ([]int64, error) {
		switch atomic.AddInt32(&scans, 1) {
		case 1:
			close(firstScan)
		case 2:
			close(secondScan)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	probe := &poolDynamicRuntimeProbe{queryFn: func(ctx context.Context, accountID int64) (PoolDynamicSnapshot, error) {
		return PoolDynamicSnapshot{}, nil
	}}
	service := NewPoolDynamicRefreshService(repo, probe)
	service.Start()
	service.Start()

	select {
	case <-firstScan:
	case <-time.After(time.Second):
		t.Fatal("refresh worker did not start")
	}
	select {
	case <-secondScan:
		t.Fatal("Start launched a duplicate refresh worker")
	case <-time.After(100 * time.Millisecond):
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&scans))
	service.Stop()
}

func TestPoolDynamicRefreshReportsApplyFailure(t *testing.T) {
	applyErr := errors.New("apply snapshot failed")
	repo := &poolDynamicRuntimeRepo{claim: true, applyErr: applyErr}
	probe := &poolDynamicRuntimeProbe{queryFn: func(context.Context, int64) (PoolDynamicSnapshot, error) {
		return PoolDynamicSnapshot{}, nil
	}}
	service := NewPoolDynamicRefreshService(repo, probe)

	err := service.Refresh(context.Background(), 7)
	require.ErrorIs(t, err, applyErr)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.applyCount))
}

package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

type poolDynamicQuotaProbe interface {
	QueryPoolDynamicSnapshot(context.Context, int64) (PoolDynamicSnapshot, error)
}

// PoolDynamicRefreshService samples only the isolated pool. The database lease
// coalesces refreshes across app instances; no upstream requests run in a SQL tx.
type PoolDynamicRefreshService struct {
	repo   PoolDynamicRepository
	probe  poolDynamicQuotaProbe
	ctx    context.Context
	cancel context.CancelFunc
	start  sync.Once
	stop   sync.Once
	wg     sync.WaitGroup
}

func NewPoolDynamicRefreshService(repo PoolDynamicRepository, probe poolDynamicQuotaProbe) *PoolDynamicRefreshService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PoolDynamicRefreshService{repo: repo, probe: probe, ctx: ctx, cancel: cancel}
}

func (s *PoolDynamicRefreshService) Start() {
	if s == nil || s.repo == nil || s.probe == nil {
		return
	}
	s.start.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				s.refreshBatch(s.ctx)
				select {
				case <-s.ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *PoolDynamicRefreshService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
	s.wg.Wait()
}

func (s *PoolDynamicRefreshService) Refresh(ctx context.Context, accountID int64) error {
	if s == nil || s.repo == nil || s.probe == nil {
		return fmt.Errorf("dynamic quota probe unavailable")
	}
	claimer, ok := s.repo.(PoolDynamicRefreshClaimer)
	if !ok {
		return fmt.Errorf("dynamic quota refresh lease unavailable")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	claimed, err := claimer.ClaimDynamicRefresh(probeCtx, accountID)
	if err != nil || !claimed {
		return err
	}
	snapshot, err := s.probe.QueryPoolDynamicSnapshot(probeCtx, accountID)
	if err != nil {
		return err
	}
	return s.repo.ApplyDynamicSnapshot(probeCtx, snapshot)
}

func (s *PoolDynamicRefreshService) refreshBatch(ctx context.Context) {
	ids, err := s.repo.DynamicAccounts(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("pool dynamic quota account scan failed")
		}
		return
	}
	var workers sync.WaitGroup
	jobs := make(chan int64)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				if err := s.Refresh(ctx, id); err != nil && ctx.Err() == nil {
					// Do not log upstream bodies or credential-bearing errors.
					slog.Warn("pool dynamic quota refresh failed", "account_id", id)
				}
			}
		}()
	}
send:
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	workers.Wait()
}

// QueryPoolDynamicSnapshot deliberately excludes reset-credit lookups and
// redemption. Feature-specific accounts are not interchangeable with Codex.
func (s *OpenAIQuotaService) QueryPoolDynamicSnapshot(ctx context.Context, accountID int64) (PoolDynamicSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return PoolDynamicSnapshot{}, fmt.Errorf("quota service unavailable")
	}
	a, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return PoolDynamicSnapshot{}, err
	}
	if a == nil || a.Platform != "openai" || a.Type != "oauth" || a.IsShadow() {
		return PoolDynamicSnapshot{}, fmt.Errorf("dynamic quota requires a standard Codex OAuth account")
	}
	// Use request start as a conservative observation watermark rather than
	// attributing requests started during network delay to this snapshot.
	observedAt := time.Now()
	u, err := s.queryUsage(ctx, accountID, false)
	if err != nil {
		return PoolDynamicSnapshot{}, err
	}
	return poolDynamicSnapshotFromUsage(accountID, u, observedAt)
}

func poolDynamicSnapshotFromUsage(accountID int64, usage *OpenAIQuotaUsage, observedAt time.Time) (PoolDynamicSnapshot, error) {
	out := PoolDynamicSnapshot{AccountID: accountID, ObservedAt: observedAt}
	if accountID <= 0 || usage == nil || usage.RateLimit == nil || observedAt.IsZero() {
		return out, fmt.Errorf("subscription quota is unknown")
	}
	fetchedAt := time.Unix(usage.FetchedAt, 0)
	if usage.FetchedAt <= 0 {
		fetchedAt = observedAt
	}
	out.Blocked = usage.RateLimit.LimitReached || !usage.RateLimit.Allowed
	seen := map[string]bool{}
	for _, w := range []*OpenAIRateLimitWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if w == nil {
			continue
		}
		if (w.quotaPresenceChecked && !w.quotaWindowValid) || math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent > 100 || w.LimitWindowSeconds <= 0 || w.LimitWindowSeconds > 366*86400 || w.LimitWindowSeconds%60 != 0 {
			return out, fmt.Errorf("invalid subscription quota window")
		}
		reset := time.Unix(w.ResetAt, 0)
		if w.ResetAt <= 0 {
			if w.ResetAfterSeconds <= 0 {
				return out, fmt.Errorf("missing subscription reset time")
			}
			reset = fetchedAt.Add(time.Duration(w.ResetAfterSeconds) * time.Second)
		}
		if !reset.After(fetchedAt) || reset.After(fetchedAt.Add(time.Duration(w.LimitWindowSeconds)*time.Second+time.Minute)) {
			return out, fmt.Errorf("invalid subscription reset time")
		}
		key := fmt.Sprintf("codex/%d", w.LimitWindowSeconds/60)
		if seen[key] {
			return out, fmt.Errorf("duplicate subscription window")
		}
		seen[key] = true
		out.Windows = append(out.Windows, PoolDynamicSnapshotWindow{Key: key, UsedPercent: w.UsedPercent, ResetsAt: reset, WindowSeconds: w.LimitWindowSeconds})
	}
	if len(out.Windows) == 0 {
		return out, fmt.Errorf("subscription quota windows are unknown")
	}
	return out, nil
}

func ProvidePoolDynamicRefreshService(repo PoolRepository, probe *OpenAIQuotaService) (*PoolDynamicRefreshService, error) {
	dynamic, ok := repo.(PoolDynamicRepository)
	if !ok {
		return nil, fmt.Errorf("pool repository does not support dynamic quotas")
	}
	s := NewPoolDynamicRefreshService(dynamic, probe)
	s.Start()
	return s, nil
}

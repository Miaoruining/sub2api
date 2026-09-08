package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const LotteryDailyBudget = 100

var (
	ErrLotteryClosed     = infraerrors.Conflict("LOTTERY_CLOSED", "今日活动暂未开放或已结束")
	ErrLotteryIneligible = infraerrors.Forbidden("LOTTERY_INELIGIBLE", "累计有效人民币实付充值满 10 元后可参与")
	ErrLotteryConfig     = infraerrors.BadRequest("LOTTERY_CONFIG_INVALID", "七档概率权重必须为非负整数且合计 10000")
)

// 固定北京时间，不受操作系统或应用的展示时区设置影响。
var lotteryZone = time.FixedZone("Asia/Shanghai", 8*60*60)

func LotteryWindow(now time.Time) (string, time.Time, time.Time) {
	n := now.In(lotteryZone)
	open := time.Date(n.Year(), n.Month(), n.Day(), 10, 0, 0, 0, lotteryZone)
	return n.Format("2006-01-02"), open, open.AddDate(0, 0, 1)
}

func LotteryPrizes() []int { return []int{0, 1, 5, 10, 20, 50, 100} }

func ValidateLotteryWeights(weights []int) error {
	if len(weights) != 7 {
		return ErrLotteryConfig
	}
	total := 0
	for _, w := range weights {
		if w < 0 || w > 10000 {
			return ErrLotteryConfig
		}
		total += w
	}
	if total != 10000 {
		return ErrLotteryConfig
	}
	return nil
}

// 超过剩余预算的结果变为无奖，不能重新抽取以免扭曲其他奖项权重。
func LotteryPrizeForTicket(weights []int, ticket, remaining int) (int, error) {
	if err := ValidateLotteryWeights(weights); err != nil {
		return 0, err
	}
	if ticket < 0 || ticket >= 10000 || remaining < 0 || remaining > LotteryDailyBudget {
		return 0, ErrLotteryConfig
	}
	for i, w := range weights {
		if ticket < w {
			prize := LotteryPrizes()[i]
			if prize > remaining {
				return 0, nil
			}
			return prize, nil
		}
		ticket -= w
	}
	return 0, ErrLotteryConfig
}

// 用户 DTO 明确白名单，禁止把权重、剩余预算、随机票号和管理员配置透传出去。
type LotteryDraw struct {
	ID           int64     `json:"id"`
	ActivityDate string    `json:"activity_date"`
	Prize        int       `json:"prize"`
	CreatedAt    time.Time `json:"created_at"`
}

type LotteryStatus struct {
	ActivityDate string        `json:"activity_date"`
	State        string        `json:"state"`
	Eligible     bool          `json:"eligible"`
	ServerTime   time.Time     `json:"server_time"`
	OpensAt      time.Time     `json:"opens_at"`
	NextOpensAt  time.Time     `json:"next_opens_at"`
	Prizes       []int         `json:"prizes"`
	Today        *LotteryDraw  `json:"today"`
	History      []LotteryDraw `json:"history"`
}

type LotteryConfigUpdate struct {
	Enabled *bool `json:"enabled"`
	Weights []int `json:"weights,omitempty"`
}

type LotteryAdminDraw struct {
	LotteryDraw
	UserID       int64   `json:"user_id"`
	BalanceAfter float64 `json:"balance_after"`
}

type LotteryAdminStatus struct {
	Enabled           bool               `json:"enabled"`
	ActivityDate      string             `json:"activity_date"`
	DailyBudget       int                `json:"daily_budget"`
	Spent             int                `json:"spent"`
	DrawCount         int                `json:"draw_count"`
	Weights           []int              `json:"weights"`
	NextWeights       []int              `json:"next_weights"`
	NextEffectiveDate string             `json:"next_effective_date"`
	Distribution      []int              `json:"distribution"`
	Records           []LotteryAdminDraw `json:"records"`
}

type LotteryRepository interface {
	Status(context.Context, int64) (*LotteryStatus, error)
	Draw(context.Context, int64, string) (*LotteryDraw, error)
	AdminStatus(context.Context, string) (*LotteryAdminStatus, error)
	UpdateConfig(context.Context, int64, LotteryConfigUpdate) error
}

type LotteryService struct {
	repo         LotteryRepository
	authCache    APIKeyAuthCacheInvalidator
	billingCache *BillingCacheService
}

func NewLotteryService(repo LotteryRepository, authCache APIKeyAuthCacheInvalidator, billingCache *BillingCacheService) *LotteryService {
	return &LotteryService{repo: repo, authCache: authCache, billingCache: billingCache}
}

func (s *LotteryService) Status(ctx context.Context, uid int64) (*LotteryStatus, error) {
	return s.repo.Status(ctx, uid)
}
func (s *LotteryService) AdminStatus(ctx context.Context, date string) (*LotteryAdminStatus, error) {
	return s.repo.AdminStatus(ctx, date)
}
func (s *LotteryService) UpdateConfig(ctx context.Context, uid int64, c LotteryConfigUpdate) error {
	if c.Enabled == nil && c.Weights == nil {
		return ErrLotteryConfig
	}
	if c.Weights != nil {
		if err := ValidateLotteryWeights(c.Weights); err != nil {
			return err
		}
	}
	return s.repo.UpdateConfig(ctx, uid, c)
}
func (s *LotteryService) Draw(ctx context.Context, uid int64, date string) (*LotteryDraw, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, ErrLotteryClosed
	}
	draw, err := s.repo.Draw(ctx, uid, date)
	if err != nil {
		return nil, err
	}
	if draw.Prize > 0 {
		// 提交成功后的缓存清理不依赖客户端是否仍连接。重试也会再次清理。
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if s.authCache != nil {
			s.authCache.InvalidateAuthCacheByUserID(cacheCtx, uid)
		}
		if s.billingCache != nil {
			if err := s.billingCache.InvalidateUserBalance(cacheCtx, uid); err != nil {
				logger.LegacyPrintf("service.lottery", "invalidate balance cache for user %d: %v", uid, err)
			}
		}
	}
	return draw, nil
}

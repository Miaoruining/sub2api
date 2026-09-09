package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPoolPlan               = infraerrors.BadRequest("POOL_PLAN", "账号订阅类型与拼单商品不一致，请选择对应的 Plus / Pro 账号")
	ErrPoolProductChanged     = infraerrors.Conflict("POOL_PRODUCT_CHANGED", "商品已更新或下架，请刷新商品后重新确认")
	ErrPoolConfig             = infraerrors.BadRequest("POOL_CONFIG", "请检查拼单参数；发布时无需绑定账号，发货账号必须来自可用的独立拼单号池")
	ErrPoolClosed             = infraerrors.Conflict("POOL_CLOSED", "拼单已满员、结束或超过截止时间")
	ErrPoolBalance            = infraerrors.Conflict("POOL_BALANCE", "站内余额不足，请先充值")
	ErrPoolAccess             = infraerrors.Forbidden("POOL_ACCESS", "仅限本车成员使用自己的拼单专属 Key")
	ErrPoolLimit              = infraerrors.New(429, "POOL_LIMIT", "个人额度、请求次数或并发额度不足，请查看拼单剩余额度和恢复时间")
	ErrPoolDynamicUnavailable = infraerrors.New(503, "POOL_DYNAMIC_UNAVAILABLE", "拼单动态额度尚未同步，请稍后重试")
	ErrPoolDynamicBlocked     = infraerrors.New(429, "POOL_DYNAMIC_BLOCKED", "上游动态额度暂不可用，请稍后重试")
)

type PoolCreditConfig struct {
	QuotaMode   string  `json:"quota_mode"`
	PlanType    string  `json:"plan_type"`
	TotalCredit float64 `json:"total_credit"`
	Credit5h    float64 `json:"credit_5h"`
	Credit7d    float64 `json:"credit_7d"`
}

func (p PoolCreditConfig) ValidateCredits() bool {
	if p.QuotaMode == "" || p.QuotaMode == "tokens" {
		return true
	}
	if p.QuotaMode == "dynamic" {
		if p.PlanType != "plus" && p.PlanType != "pro" {
			return false
		}
		// Dynamic products use the upstream subscription windows.  The legacy
		// dollar/token fields are deliberately zero so an old cap cannot become
		// an accidental fallback limit.
		return p.TotalCredit == 0 && p.Credit5h == 0 && p.Credit7d == 0
	}
	if p.QuotaMode != "credits" && p.QuotaMode != "dynamic_shadow" || (p.PlanType != "plus" && p.PlanType != "pro") {
		return false
	}
	for _, v := range []float64{p.TotalCredit, p.Credit5h, p.Credit7d} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100000000 {
			return false
		}
	}
	return p.TotalCredit > 0 && ((p.PlanType == "plus" && p.Credit5h > 0 && p.Credit7d >= p.Credit5h && p.TotalCredit >= p.Credit7d) || (p.PlanType == "pro" && p.Credit5h == 0 && p.Credit7d == 0))
}

type PoolCreate struct {
	PoolCreditConfig
	Title         string    `json:"title"`
	GroupID       int64     `json:"group_id"`
	Seats         int       `json:"seats"`
	Price         float64   `json:"price"`
	DurationHours int       `json:"duration_hours"`
	TotalTokens   int64     `json:"total_tokens"`
	TotalRequests int64     `json:"total_requests"`
	Concurrency   int       `json:"concurrency"`
	JoinDeadline  time.Time `json:"join_deadline"`
}

func (p PoolCreate) Validate(now time.Time) error {
	if strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 100 || p.GroupID != 0 || p.Seats < 2 || p.Seats > 50 || math.IsNaN(p.Price) || math.IsInf(p.Price, 0) || p.Price < 0.00000001 || p.Price > 1000000 || p.DurationHours < 1 || p.DurationHours > 8760 || p.TotalRequests > 1000000000 || p.Concurrency < 1 || p.Concurrency > 10 || !p.JoinDeadline.After(now) || p.JoinDeadline.After(now.Add(90*24*time.Hour)) {
		return ErrPoolConfig
	}
	if p.QuotaMode == "dynamic" {
		if p.TotalTokens != 0 || p.TotalRequests != 0 || p.TotalCredit != 0 || p.Credit5h != 0 || p.Credit7d != 0 {
			return ErrPoolConfig
		}
	} else if p.QuotaMode == "dynamic_shadow" {
		if p.TotalTokens < int64(p.Seats)*8192 || p.TotalTokens > 1000000000000 || p.TotalRequests < int64(p.Seats) {
			return ErrPoolConfig
		}
	} else if p.TotalTokens < int64(p.Seats)*8192 || p.TotalTokens > 1000000000000 || p.TotalRequests < int64(p.Seats) {
		return ErrPoolConfig
	}
	if !p.ValidateCredits() {
		return ErrPoolConfig
	}
	return nil
}

// PoolDynamicSnapshot is one immutable observation of an account's upstream
// subscription windows.  A missing window is meaningful and is never filled
// in by this type.
type PoolDynamicSnapshot struct {
	AccountID  int64                       `json:"account_id"`
	ObservedAt time.Time                   `json:"observed_at"`
	Blocked    bool                        `json:"blocked"`
	Windows    []PoolDynamicSnapshotWindow `json:"windows"`
}

type PoolDynamicSnapshotWindow struct {
	Key           string    `json:"key"`
	UsedPercent   float64   `json:"used_percent"`
	ResetsAt      time.Time `json:"resets_at"`
	WindowSeconds int64     `json:"window_seconds"`
}

type PoolDynamicQuota struct {
	Status  string                   `json:"status"`
	Windows []PoolDynamicQuotaWindow `json:"windows"`
	Shadow  bool                     `json:"shadow"`
}

type PoolDynamicQuotaWindow struct {
	Key                     string    `json:"key"`
	AccountRemainingPercent float64   `json:"account_remaining_percent"`
	UsedPercent             float64   `json:"used_percent"`
	ReservedPercent         float64   `json:"reserved_percent"`
	RemainingPercent        float64   `json:"remaining_percent"`
	EntitlementPercent      float64   `json:"entitlement_percent"`
	ResetAt                 time.Time `json:"reset_at"`
	ObservedAt              time.Time `json:"observed_at"`
	Status                  string    `json:"status"`
}

type PoolDynamicUsage struct {
	ID        string                   `json:"id"`
	OrderID   int64                    `json:"order_id"`
	KeyID     int64                    `json:"key_id"`
	Credit    float64                  `json:"credit"`
	Status    string                   `json:"status"`
	CreatedAt time.Time                `json:"created_at"`
	Windows   []PoolDynamicQuotaWindow `json:"windows"`
}

// PoolDynamicRepository is intentionally independent from PoolRepository.
// This lets existing pool mocks and callers keep the old contract while the
// refresh worker and the user usage page opt in to dynamic data.
type PoolDynamicRepository interface {
	DynamicAccounts(context.Context) ([]int64, error)
	ApplyDynamicSnapshot(context.Context, PoolDynamicSnapshot) error
	DynamicUsage(context.Context, int64) ([]PoolDynamicUsage, error)
}

// PoolDynamicRefreshClaimer is an optional companion implemented by the SQL
// dynamic repository.  It is separate so a read-only/test PoolDynamicRepository
// does not need to implement a write-side lease.
type PoolDynamicRefreshClaimer interface {
	ClaimDynamicRefresh(context.Context, int64) (bool, error)
}

type PoolMember struct {
	CreditUsed     float64           `json:"credit_used"`
	ReservedCredit float64           `json:"reserved_credit"`
	Used5h         float64           `json:"credit_used_5h"`
	Used7d         float64           `json:"credit_used_7d"`
	Reset5h        *time.Time        `json:"credit_reset_5h"`
	Reset7d        *time.Time        `json:"credit_reset_7d"`
	ID             int64             `json:"id"`
	Status         string            `json:"status"`
	KeyID          *int64            `json:"key_id"`
	Paid           float64           `json:"paid"`
	Refunded       float64           `json:"refunded"`
	TokensUsed     int64             `json:"tokens_used"`
	RequestsUsed   int64             `json:"requests_used"`
	ReservedTokens int64             `json:"reserved_tokens"`
	Inflight       int               `json:"inflight"`
	DynamicQuota   *PoolDynamicQuota `json:"dynamic_quota,omitempty"`
}
type PoolOrder struct {
	ProductID     *int64        `json:"product_id"`
	DurationDays  float64       `json:"duration_days"`
	SharedAccount *PoolResource `json:"shared_account"`
	ID            int64         `json:"id"`
	PoolCreate
	FormedAt         *time.Time  `json:"formed_at"`
	DeliveryDeadline *time.Time  `json:"delivery_deadline"`
	ResourceID       *int64      `json:"resource_id"`
	Status           string      `json:"status"`
	Joined           int         `json:"joined"`
	StartsAt         *time.Time  `json:"starts_at"`
	ExpiresAt        *time.Time  `json:"expires_at"`
	TokensUsed       int64       `json:"tokens_used"`
	RequestsUsed     int64       `json:"requests_used"`
	ReservedTokens   int64       `json:"reserved_tokens"`
	Mine             *PoolMember `json:"mine"`
}
type PoolGate struct {
	QuotaMode       string
	OrderID         int64
	MemberID        int64
	TokensRemaining int64
	Concurrency     int
}
type PoolReservationContextKey struct{}

type PoolAccountListContextKey struct{}
type PoolAccountCreateContextKey struct{}
type PoolNotification struct {
	ID       int64      `json:"id"`
	OrderID  int64      `json:"order_id"`
	Title    string     `json:"title"`
	Kind     string     `json:"kind"`
	Deadline *time.Time `json:"deadline"`
	Read     bool       `json:"read"`
}
type PoolResourceCreate struct {
	Platform           string          `json:"platform"`
	RateMultiplier     *float64        `json:"rate_multiplier"`
	LoadFactor         *int            `json:"load_factor"`
	ExpiresAt          *int64          `json:"expires_at"`
	AutoPauseOnExpired *bool           `json:"auto_pause_on_expired"`
	Extra              json.RawMessage `json:"extra"`
	ProxyID            *int64          `json:"proxy_id"`
	Notes              *string         `json:"notes"`
	Priority           int             `json:"priority"`
	Name               string          `json:"name"`
	Type               string          `json:"type"`
	Credentials        json.RawMessage `json:"credentials"`
	Concurrency        int             `json:"concurrency"`
}
type PoolResource struct {
	AccountID int64    `json:"account_id"`
	ID        int64    `json:"id"`
	GroupID   int64    `json:"group_id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Used5h    *float64 `json:"used_5h"`
	Used7d    *float64 `json:"used_7d"`
}
type PoolProduct struct {
	PoolCreditConfig
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Seats         int     `json:"seats"`
	Price         float64 `json:"price"`
	DurationDays  int     `json:"duration_days"`
	FormationDays int     `json:"formation_days"`
	TotalTokens   int64   `json:"total_tokens"`
	TotalRequests int64   `json:"total_requests"`
	Concurrency   int     `json:"concurrency"`
	Status        string  `json:"status"`
	Version       int64   `json:"version"`
}

func (p PoolProduct) Validate() error {
	now := time.Now()
	if p.QuotaMode == "credits" || p.QuotaMode == "dynamic_shadow" {
		p.TotalTokens = int64(p.Seats) * 8192
		p.TotalRequests = int64(p.Seats)
	} else if p.QuotaMode == "dynamic" {
		p.TotalTokens = 0
		p.TotalRequests = 0
	}
	if !p.ValidateCredits() {
		return ErrPoolConfig
	}
	if len([]rune(p.Description)) > 2000 || p.DurationDays < 1 || p.DurationDays > 365 || p.FormationDays < 1 || p.FormationDays > 90 || (p.Status != "active" && p.Status != "disabled") {
		return ErrPoolConfig
	}
	return (PoolCreate{PoolCreditConfig: p.PoolCreditConfig, Title: p.Title, Seats: p.Seats, Price: p.Price, DurationHours: p.DurationDays * 24, TotalTokens: p.TotalTokens, TotalRequests: p.TotalRequests, Concurrency: p.Concurrency, JoinDeadline: now.Add(time.Hour)}).Validate(now)
}

type PoolCreditHold struct {
	ID        string    `json:"id"`
	OrderID   int64     `json:"order_id"`
	KeyID     int64     `json:"key_id"`
	Status    string    `json:"status"`
	Credit    float64   `json:"credit"`
	CreatedAt time.Time `json:"created_at"`
}

type PoolRepository interface {
	CreditHolds(context.Context, int64) ([]PoolCreditHold, error)
	Products(context.Context, bool) ([]PoolProduct, error)
	SaveProduct(context.Context, int64, PoolProduct) (int64, error)
	PurchaseProduct(context.Context, int64, int64, string, int64) (int64, error)
	Deliver(context.Context, int64, int64) ([]int64, error)
	Notifications(context.Context, int64, bool) ([]PoolNotification, error)
	ReadNotification(context.Context, int64, int64, bool) error
	Resources(context.Context) ([]PoolResource, error)
	CreateResource(context.Context, PoolResourceCreate) (int64, error)
	SetResourceStatus(context.Context, int64, string) error
	List(context.Context, int64, bool) ([]PoolOrder, error)
	Create(context.Context, PoolCreate) (int64, error)
	Join(context.Context, int64, int64) ([]int64, error)
	Leave(context.Context, int64, int64) ([]int64, error)
	Cancel(context.Context, int64) ([]int64, error)
	Gate(context.Context, int64, int64, int64) (*PoolGate, error)
	Reserve(context.Context, int64, int64, ...float64) (string, error)
	Finish(context.Context, string, bool) error
}

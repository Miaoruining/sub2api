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
	ErrPoolConfig  = infraerrors.BadRequest("POOL_CONFIG", "请检查人数、价格、有效期和额度；资源组必须是独立的 OpenAI 专属订阅组")
	ErrPoolClosed  = infraerrors.Conflict("POOL_CLOSED", "拼单已满员、结束或超过截止时间")
	ErrPoolBalance = infraerrors.Conflict("POOL_BALANCE", "站内余额不足，请先充值")
	ErrPoolAccess  = infraerrors.Forbidden("POOL_ACCESS", "仅限本车成员使用自己的拼单专属 Key")
	ErrPoolLimit   = infraerrors.New(429, "POOL_LIMIT", "个人 Token、请求次数或并发额度不足，请等待当前请求结束或下一周期")
)

type PoolCreate struct {
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
	if strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 100 || p.GroupID <= 0 || p.Seats < 2 || p.Seats > 50 || math.IsNaN(p.Price) || math.IsInf(p.Price, 0) || p.Price < 0.00000001 || p.Price > 1000000 || p.DurationHours < 1 || p.DurationHours > 8760 || p.TotalTokens < int64(p.Seats)*8192 || p.TotalTokens > 1000000000000 || p.TotalRequests < int64(p.Seats) || p.TotalRequests > 1000000000 || p.Concurrency < 1 || p.Concurrency > 10 || !p.JoinDeadline.After(now) || p.JoinDeadline.After(now.Add(90*24*time.Hour)) {
		return ErrPoolConfig
	}
	return nil
}

type PoolMember struct {
	ID             int64   `json:"id"`
	Status         string  `json:"status"`
	KeyID          *int64  `json:"key_id"`
	Paid           float64 `json:"paid"`
	Refunded       float64 `json:"refunded"`
	TokensUsed     int64   `json:"tokens_used"`
	RequestsUsed   int64   `json:"requests_used"`
	ReservedTokens int64   `json:"reserved_tokens"`
	Inflight       int     `json:"inflight"`
}
type PoolOrder struct {
	SharedAccount *PoolResource `json:"shared_account"`
	ID            int64         `json:"id"`
	PoolCreate
	Status         string      `json:"status"`
	Joined         int         `json:"joined"`
	StartsAt       *time.Time  `json:"starts_at"`
	ExpiresAt      *time.Time  `json:"expires_at"`
	TokensUsed     int64       `json:"tokens_used"`
	RequestsUsed   int64       `json:"requests_used"`
	ReservedTokens int64       `json:"reserved_tokens"`
	Mine           *PoolMember `json:"mine"`
}
type PoolGate struct {
	OrderID         int64
	MemberID        int64
	TokensRemaining int64
	Concurrency     int
}
type PoolReservationContextKey struct{}

type PoolResourceCreate struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Credentials json.RawMessage `json:"credentials"`
	Concurrency int             `json:"concurrency"`
}
type PoolResource struct {
	ID      int64    `json:"id"`
	GroupID int64    `json:"group_id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	Used5h  *float64 `json:"used_5h"`
	Used7d  *float64 `json:"used_7d"`
}
type PoolRepository interface {
	Resources(context.Context) ([]PoolResource, error)
	CreateResource(context.Context, PoolResourceCreate) (int64, error)
	SetResourceStatus(context.Context, int64, string) error
	List(context.Context, int64, bool) ([]PoolOrder, error)
	Create(context.Context, PoolCreate) (int64, error)
	Join(context.Context, int64, int64) ([]int64, error)
	Leave(context.Context, int64, int64) ([]int64, error)
	Cancel(context.Context, int64) ([]int64, error)
	Gate(context.Context, int64, int64, int64) (*PoolGate, error)
	Reserve(context.Context, int64, int64) (string, error)
	Finish(context.Context, string, bool) error
}

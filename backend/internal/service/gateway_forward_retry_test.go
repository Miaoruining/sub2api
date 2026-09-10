package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayForwardRetryCountingUpstream struct {
	mu         sync.Mutex
	statuses   []int
	callCount  int
	cancelOnce func()
}

func (u *gatewayForwardRetryCountingUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *gatewayForwardRetryCountingUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.mu.Lock()
	idx := u.callCount
	u.callCount++
	cancel := u.cancelOnce
	u.cancelOnce = nil
	status := http.StatusServiceUnavailable
	if idx < len(u.statuses) {
		status = u.statuses[idx]
	} else if len(u.statuses) > 0 {
		status = u.statuses[len(u.statuses)-1]
	}
	u.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream failure"}}`)),
	}, nil
}

func (u *gatewayForwardRetryCountingUpstream) Calls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.callCount
}

func newGatewayForwardRetryService(upstream HTTPUpstream) *GatewayService {
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	return &GatewayService{
		cfg:                  cfg,
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		tlsFPProfileService:  &TLSFingerprintProfileService{},
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
	}
}

func newGatewayForwardRetryContext(ctx context.Context, path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	return c
}

func newGatewayForwardRetryRequest(t *testing.T) *ParsedRequest {
	t.Helper()
	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	return parsed
}

func newGatewayForwardRetryAPIKeyAccount(pool bool, retryStatusCodes ...int) *Account {
	credentials := map[string]any{
		"api_key":                    "test-anthropic-api-key",
		"base_url":                   "https://api.anthropic.com",
		"custom_error_codes_enabled": true,
		// Exclude the statuses under test so API-key forwarding enters the
		// service-level retry branch. Production accounts with the default error
		// policy retain their existing non-retry behavior.
		"custom_error_codes": []any{float64(http.StatusBadRequest)},
	}
	if pool {
		credentials["pool_mode"] = true
	}
	if retryStatusCodes != nil {
		values := make([]any, 0, len(retryStatusCodes))
		for _, code := range retryStatusCodes {
			values = append(values, float64(code))
		}
		credentials["pool_mode_retry_status_codes"] = values
	}
	return &Account{
		ID:          1,
		Name:        "gateway-forward-retry-test",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: credentials,
		Status:      StatusActive,
		Schedulable: true,
	}
}

func requireGatewayFailover(t *testing.T, err error, status int, sameAccount bool) {
	t.Helper()
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, status, failoverErr.StatusCode)
	require.Equal(t, sameAccount, failoverErr.RetryableOnSameAccount)
}

func TestPoolModeOwnsRetry_OnlyFailoverStatuses(t *testing.T) {
	account := newGatewayForwardRetryAPIKeyAccount(true, http.StatusBadRequest, http.StatusNotFound)
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound} {
		require.False(t, poolModeOwnsRetry(account, status), "状态码 %d 不是 Service 可切换出口，不应跳过既有处理", status)
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, 529, 500} {
		account := newGatewayForwardRetryAPIKeyAccount(true, status)
		require.True(t, poolModeOwnsRetry(account, status), "状态码 %d 应交由外层同账号重试", status)
	}
}

func TestGatewayForward_PoolCustom404KeepsExistingErrorHandling(t *testing.T) {
	upstream := &gatewayForwardRetryCountingUpstream{statuses: []int{
		http.StatusNotFound,
		http.StatusNotFound,
		http.StatusNotFound,
		http.StatusNotFound,
		http.StatusNotFound,
	}}
	svc := newGatewayForwardRetryService(upstream)
	c := newGatewayForwardRetryContext(context.Background(), "/v1/messages")
	account := newGatewayForwardRetryAPIKeyAccount(true, http.StatusNotFound)

	_, err := svc.Forward(c.Request.Context(), c, account, newGatewayForwardRetryRequest(t))

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr, "404 不属于 Service 可切换状态，不能被错误包装为外层 failover")
	require.Equal(t, http.StatusBadGateway, c.Writer.Status())
	require.Equal(t, maxRetryAttempts, upstream.Calls(), "自定义404应保留既有 Service 重试语义")
}

func TestGatewayForward_PoolCustom400KeepsExistingErrorHandling(t *testing.T) {
	upstream := &gatewayForwardRetryCountingUpstream{statuses: []int{http.StatusBadRequest}}
	svc := newGatewayForwardRetryService(upstream)
	svc.settingService = NewSettingService(&gatewayForwardRetrySettingRepo{}, svc.cfg)
	c := newGatewayForwardRetryContext(context.Background(), "/v1/messages")
	account := newGatewayForwardRetryAPIKeyAccount(true, http.StatusBadRequest)

	_, err := svc.Forward(c.Request.Context(), c, account, newGatewayForwardRetryRequest(t))

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr, "400 应保留参数/错误处理出口，不能变成外层 failover")
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
	require.Equal(t, 1, upstream.Calls(), "400 不应进入通用状态重试")
}

func TestGatewayForward_PoolRetryableStatusDelegatesToOuterRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := &gatewayForwardRetryCountingUpstream{statuses: []int{status}}
			svc := newGatewayForwardRetryService(upstream)
			c := newGatewayForwardRetryContext(context.Background(), "/v1/messages")
			account := newGatewayForwardRetryAPIKeyAccount(true, status)

			_, err := svc.Forward(c.Request.Context(), c, account, newGatewayForwardRetryRequest(t))

			requireGatewayFailover(t, err, status, true)
			require.Equal(t, 1, upstream.Calls(), "池模式状态码应由外层重试，不能叠加 5 次 Service 重试")
		})
	}
}

func TestGatewayForward_NonPool503KeepsFiveTotalAttempts(t *testing.T) {
	upstream := &gatewayForwardRetryCountingUpstream{statuses: []int{
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
	}}
	svc := newGatewayForwardRetryService(upstream)
	c := newGatewayForwardRetryContext(context.Background(), "/v1/messages")
	account := newGatewayForwardRetryAPIKeyAccount(false)

	_, err := svc.Forward(c.Request.Context(), c, account, newGatewayForwardRetryRequest(t))

	requireGatewayFailover(t, err, http.StatusServiceUnavailable, false)
	require.Equal(t, maxRetryAttempts, upstream.Calls(), "非池账号仍应保留 Service 层 5 次总尝试")
}

func TestGatewayAnthropicPassthrough_PoolRetryableStatusDelegatesToOuterRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := &gatewayForwardRetryCountingUpstream{statuses: []int{status}}
			svc := newGatewayForwardRetryService(upstream)
			c := newGatewayForwardRetryContext(context.Background(), "/v1/messages")
			account := newGatewayForwardRetryAPIKeyAccount(true, status)
			body := []byte(`{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hello"}]}`)

			_, err := svc.forwardAnthropicAPIKeyPassthrough(
				c.Request.Context(), c, account, body, "claude-3-5-sonnet-latest",
				"claude-3-5-sonnet-latest", false, time.Now(),
			)

			requireGatewayFailover(t, err, status, true)
			require.Equal(t, 1, upstream.Calls(), "透传池模式状态码应跳过 Service 层通用重试")
		})
	}
}

func TestGatewayForward_CancelDuringNonPoolRetryBackoffStopsBeforeNextAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &gatewayForwardRetryCountingUpstream{
		statuses: []int{http.StatusServiceUnavailable},
		cancelOnce: func() {
			cancel()
		},
	}
	svc := newGatewayForwardRetryService(upstream)
	c := newGatewayForwardRetryContext(ctx, "/v1/messages")
	account := newGatewayForwardRetryAPIKeyAccount(false)

	_, err := svc.Forward(ctx, c, account, newGatewayForwardRetryRequest(t))

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "取消应从内部退避直接返回")
	require.Equal(t, 1, upstream.Calls(), "取消后不应发起下一次上游请求")
}

type gatewayForwardRetrySettingRepo struct{}

func (*gatewayForwardRetrySettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (*gatewayForwardRetrySettingRepo) GetValue(context.Context, string) (string, error) {
	return "", ErrSettingNotFound
}

func (*gatewayForwardRetrySettingRepo) Set(context.Context, string, string) error { return nil }

func (*gatewayForwardRetrySettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*gatewayForwardRetrySettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (*gatewayForwardRetrySettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*gatewayForwardRetrySettingRepo) Delete(context.Context, string) error { return nil }

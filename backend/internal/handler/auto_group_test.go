package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type autoRouteKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func TestAutoGroupStreamObservationRequiresProtocolCompletion(t *testing.T) {
	w := newAutoGroupWriter(httptest.NewRecorder())
	w.Header().Set("Content-Type", "text/event-stream")
	_, err := w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"response.completed\"}\n\n"))
	require.NoError(t, err)
	require.False(t, w.streamComplete)
	_, err = w.Write([]byte("data: {\"type\":\"response.com"))
	require.NoError(t, err)
	_, err = w.Write([]byte("pleted\"}\n\n"))
	require.NoError(t, err)
	require.True(t, w.streamComplete)
	_, err = w.Write([]byte("data: {\"type\":\"error\"}\n\n"))
	require.NoError(t, err)
	require.True(t, w.streamFailed)
	require.True(t, w.committed, "observation must not delay successful streaming")
}

func (r autoRouteKeyRepo) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	key := *r.key
	return &key, nil
}
func (r autoRouteKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type autoRouteUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r autoRouteUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	user := *r.user
	return &user, nil
}

type autoRouteGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r autoRouteGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}
func (r autoRouteGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	for _, g := range r.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, service.ErrGroupNotAllowed
}

type autoRouteSubRepo struct {
	service.UserSubscriptionRepository
}

func (r autoRouteSubRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

func autoGroupTestServer(t *testing.T, terminal gin.HandlerFunc) (*gin.Engine, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	user := &service.User{ID: 7, Status: service.StatusActive, Balance: 10, AllowedGroups: []int64{1, 2, 3}, RestrictPublicGroups: true}
	key := &service.APIKey{ID: 5, UserID: 7, Key: "test-key", AutoGroup: true, Status: service.StatusActive, User: user}
	groups := []service.Group{
		{ID: 1, Name: "GPT A", Platform: service.PlatformOpenAI, RateMultiplier: 1, Status: service.StatusActive, Hydrated: true},
		{ID: 2, Name: "GPT B", Platform: service.PlatformOpenAI, RateMultiplier: 3, Status: service.StatusActive, Hydrated: true},
		{ID: 3, Name: "Claude", Platform: service.PlatformAnthropic, RateMultiplier: 2, Status: service.StatusActive, Hydrated: true},
		{ID: 4, Name: "Private", Platform: service.PlatformOpenAI, IsExclusive: true, Status: service.StatusActive},
	}
	cfg := &config.Config{}
	keys := service.NewAPIKeyService(autoRouteKeyRepo{key: key}, autoRouteUserRepo{user: user}, autoRouteGroupRepo{groups: groups}, autoRouteSubRepo{}, nil, nil, cfg)
	accounts := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{}}
	for _, group := range groups {
		model := "gpt-test"
		if group.Platform == service.PlatformAnthropic {
			model = "claude-test"
		}
		if group.ID == 4 {
			model = "gpt-private"
		}
		accounts.byGroup[group.ID] = []service.Account{{Platform: group.Platform, Credentials: map[string]any{"model_mapping": map[string]any{model: model}}}}
	}
	h := newGatewayModelsHandlerForTest(accounts)
	r := gin.New()
	r.Use(middleware.RequestBodyLimit(1<<20), gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)), h.AutoGroupMiddleware(r, keys))
	r.POST("/v1/responses", terminal)
	r.POST("/v1/messages", terminal)
	r.GET("/v1/models", terminal)
	r.GET("/v1/responses", terminal)
	return r, key
}

func TestAutoGroupHTTPFailoverBindsSelectedPriceAndPreservesBody(t *testing.T) {
	var attempted []int64
	var snapshots []*service.APIKey
	r, original := autoGroupTestServer(t, func(c *gin.Context) {
		key, ok := middleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		snapshots = append(snapshots, key)
		attempted = append(attempted, *key.GroupID)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-test","input":"hello"}`, string(body))
		if *key.GroupID == 1 {
			service.MarkAutoGroupRetryable(c.Request.Context())
			c.Header("X-Failed-Attempt", "must-not-leak")
			c.JSON(503, gin.H{"error": "no capacity"})
			return
		}
		c.JSON(200, gin.H{"group_id": *key.GroupID, "rate": key.Group.RateMultiplier})
	})
	w := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	r.ServeHTTP(w, request)
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"group_id":2,"rate":3}`, w.Body.String())
	require.Equal(t, []int64{1, 2}, attempted)
	require.Equal(t, "2", w.Header().Get("X-Sub2API-Group-ID"))
	require.Empty(t, w.Header().Get("X-Failed-Attempt"))
	require.Equal(t, 1.0, snapshots[0].Group.RateMultiplier, "异步结算快照不能被下一次尝试改写")
	require.Nil(t, original.GroupID)
	w = httptest.NewRecorder()
	request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	r.ServeHTTP(w, request)
	require.Equal(t, 200, w.Code)
	require.Equal(t, []int64{1, 2, 2}, attempted, "recently failing group must be demoted on the next request")
	require.Equal(t, "smart", w.Header().Get("X-Sub2API-Routing-Strategy"))
}

func TestAutoGroupHTTPDoesNotReplayClientErrorsOrCommittedStreams(t *testing.T) {
	for _, stream := range []bool{false, true} {
		calls := 0
		r, _ := autoGroupTestServer(t, func(c *gin.Context) {
			calls++
			if stream {
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.Write([]byte("data: first\n\n"))
				c.Writer.Flush()
				service.MarkAutoGroupRetryable(c.Request.Context())
			} else {
				c.JSON(400, gin.H{"error": "bad input"})
			}
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
		req.Header.Set("Authorization", "Bearer test-key")
		r.ServeHTTP(w, req)
		require.Equal(t, 1, calls)
		if stream {
			require.Equal(t, "data: first\n\n", w.Body.String())
		} else {
			require.Equal(t, 400, w.Code)
		}
	}
}

func TestAutoGroupModelListOmitsUnauthorizedModelsAndDeduplicates(t *testing.T) {
	r, _ := autoGroupTestServer(t, func(c *gin.Context) { t.Fatal("模型列表不应进入推理处理器") })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	var models gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &models))
	require.Len(t, models.Data, 2)
	require.Equal(t, "claude-test", models.Data[0].ID)
	require.Equal(t, "gpt-test", models.Data[1].ID)
	require.NotContains(t, w.Body.String(), "private")
}

func TestAutoGroupRejectsUnboundWebSocketAndUnavailableModels(t *testing.T) {
	r, _ := autoGroupTestServer(t, func(c *gin.Context) { t.Fatal("不应调度未知模型或未绑定的长连接") })
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "/v1/responses", nil),
		httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-private"}`)),
	} {
		w := httptest.NewRecorder()
		request.Header.Set("Authorization", "Bearer test-key")
		r.ServeHTTP(w, request)
		require.Contains(t, []int{400, 404}, w.Code)
	}
}

func TestAutoGroupRechecksRevokedAccessBeforeNextGroup(t *testing.T) {
	calls := 0
	var originalKey *service.APIKey
	r, key := autoGroupTestServer(t, func(c *gin.Context) {
		calls++
		key, _ := middleware.GetAPIKeyFromContext(c)
		require.Equal(t, int64(1), *key.GroupID)
		originalKey.User.AllowedGroups = []int64{1}
		service.MarkAutoGroupRetryable(c.Request.Context())
		c.JSON(503, gin.H{"error": "no capacity"})
	})
	// 仓储持有原用户引用；第一组处理后撤销第二组，模拟候选快照过期。
	originalKey = key
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	r.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
	require.Equal(t, 1, calls)
}

func TestAutoGroupCodexManifestContainsOnlyAuthorizedOpenAIModels(t *testing.T) {
	r, _ := autoGroupTestServer(t, func(c *gin.Context) { t.Fatal("不能进入推理") })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models?client_version=test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	var envelope struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Len(t, envelope.Models, 1)
	require.Equal(t, "gpt-test", envelope.Models[0].Slug)
}

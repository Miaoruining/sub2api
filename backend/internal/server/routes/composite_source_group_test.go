package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sourceRouteRepo struct {
	service.CompositeModelRouteRepository
	routes []service.CompositeModelRoute
}

func (r sourceRouteRepo) ListByGroup(context.Context, int64, bool) ([]service.CompositeModelRoute, error) {
	return r.routes, nil
}

type sourceGroupRepo struct {
	service.GroupRepository
	groups map[int64]*service.Group
}

func (r sourceGroupRepo) GetByIDLite(_ context.Context, id int64) (*service.Group, error) {
	return r.groups[id], nil
}

func TestCompositeSourceMiddlewareSelectsAliasPriceAndRejectsUnavailableSource(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "inactive"}[active], func(t *testing.T) {
			entry := &service.Group{ID: 50, Platform: service.PlatformComposite, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}
			source := &service.Group{ID: 19, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: .23}
			if !active {
				source.Status = "disabled"
			}
			resolver := service.NewCompositeRouteResolver(sourceRouteRepo{routes: []service.CompositeModelRoute{{GroupID: 50, SourceGroupID: &source.ID, PublicModel: "gpt-5.6-sol-023", UpstreamModel: "gpt-5.6-sol", TargetPlatform: service.PlatformOpenAI, MatchType: service.CompositeRouteMatchExact, Endpoint: service.CompositeRouteEndpointAny, Enabled: true}}})
			resolver.SetGroupRepository(sourceGroupRepo{groups: map[int64]*service.Group{50: entry, 19: source}})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 77, GroupID: &entry.ID, Group: entry})
				c.Next()
			})
			called := false
			router.POST("/v1/responses", compositeTargetPlatformMiddleware(resolver), func(c *gin.Context) {
				called = true
				effective, _ := middleware.GetAPIKeyFromContext(c)
				require.Equal(t, int64(19), *effective.GroupID)
				require.Equal(t, .23, effective.Group.RateMultiplier)
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"model":"gpt-5.6-sol"`)
				c.Status(200)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol-023","input":"ping"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, active, called)
			if active {
				require.Equal(t, 200, rec.Code)
			} else {
				require.Equal(t, 403, rec.Code)
			}
		})
	}
}

func TestCompositeSourceGroupPreservesKeyAndSourcePricingWithoutMutatingCachedGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	entry := &service.Group{ID: 50, Platform: service.PlatformComposite, Status: service.StatusActive, RateMultiplier: 1, AllowMessagesDispatch: true}
	source := &service.Group{ID: 19, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: .23}
	key := &service.APIKey{ID: 77, UserID: 5, GroupID: &entry.ID, Group: entry}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	decision := service.CompositeRouteDecision{SourceGroupID: &source.ID, SourceGroup: source}
	require.True(t, applyCompositeSourceGroup(c, key, decision))
	effective, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Equal(t, key.ID, effective.ID)
	require.Equal(t, key.UserID, effective.UserID)
	require.Equal(t, source.ID, *effective.GroupID)
	require.Equal(t, .23, effective.Group.RateMultiplier)
	require.True(t, effective.Group.AllowMessagesDispatch)
	require.Same(t, effective.Group, c.Request.Context().Value(ctxkey.Group))
	require.Equal(t, int64(50), *key.GroupID)
	require.Same(t, entry, key.Group)
	require.False(t, source.AllowMessagesDispatch)
	require.NotSame(t, source, effective.Group)
}

func TestCompositeSourceGroupFailsClosedForIneligibleTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*service.Group, *service.Group)
	}{
		{"entry subscription", func(entry, source *service.Group) { entry.SubscriptionType = service.SubscriptionTypeSubscription }},
		{"source subscription", func(entry, source *service.Group) { source.SubscriptionType = service.SubscriptionTypeSubscription }},
		{"exclusive source", func(entry, source *service.Group) { source.IsExclusive = true }},
		{"inactive source", func(entry, source *service.Group) { source.Status = "disabled" }},
		{"self reference", func(entry, source *service.Group) { source.ID = entry.ID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := &service.Group{ID: 50, Platform: service.PlatformComposite, Status: service.StatusActive}
			source := &service.Group{ID: 19, Platform: service.PlatformOpenAI, Status: service.StatusActive}
			tc.change(entry, source)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			key := &service.APIKey{ID: 77, GroupID: &entry.ID, Group: entry}
			require.False(t, applyCompositeSourceGroup(c, key, service.CompositeRouteDecision{SourceGroupID: &source.ID, SourceGroup: source}))
			require.True(t, c.IsAborted())
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Same(t, entry, key.Group)
		})
	}
}

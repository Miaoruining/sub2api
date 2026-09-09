//go:build embed

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTrustedSEOTestServer(t *testing.T, settings any) (*FrontendServer, *mockSettingsProvider, *gin.Engine) {
	t.Helper()
	provider := &mockSettingsProvider{settings: settings}
	server, err := NewFrontendServer(provider)
	require.NoError(t, err)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CSPNonceKey, "seo-test-nonce")
		c.Next()
	})
	router.Use(server.Middleware())
	return server, provider, router
}

func trustedSEOSettings() map[string]any {
	return map[string]any{
		"site_name":            "ModelPort",
		"site_subtitle":        "多平台 AI API 接入与用量管理",
		"home_content":         "",
		"backend_mode_enabled": false,
	}
}

func seoRequest(t *testing.T, router http.Handler, method, target, host, userAgent string) *httptest.ResponseRecorder {
	t.Helper()
	writer := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	req.Header.Set("User-Agent", userAgent)
	router.ServeHTTP(writer, req)
	return writer
}

func TestPublicSEOEndpoints(t *testing.T) {
	_, provider, router := newTrustedSEOTestServer(t, trustedSEOSettings())

	t.Run("robots_is_plain_text_and_uses_fixed_origin", func(t *testing.T) {
		response := seoRequest(t, router, http.MethodGet, "/robots.txt?utm_source=evil", "evil.example", "Mozilla/5.0")

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Header().Get("Content-Type"), "text/plain")
		assert.Contains(t, response.Body.String(), "Disallow: /api/")
		assert.Contains(t, response.Body.String(), "Sitemap: https://modelport.top/sitemap.xml")
		assert.NotContains(t, response.Body.String(), "evil.example")
	})

	t.Run("sitemap_is_xml_and_has_only_root_and_learn_pages", func(t *testing.T) {
		response := seoRequest(t, router, http.MethodGet, "/sitemap.xml?utm_source=evil", "evil.example", "Mozilla/5.0")

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Header().Get("Content-Type"), "application/xml")
		assert.Contains(t, response.Body.String(), `<?xml version="1.0" encoding="UTF-8"?>`)
		assert.Contains(t, response.Body.String(), "https://modelport.top/")
		assert.Contains(t, response.Body.String(), "https://modelport.top/learn")
		assert.Equal(t, 8, strings.Count(response.Body.String(), "<loc>"), "root plus seven editorial pages")
		assert.NotContains(t, response.Body.String(), "evil.example")
		assert.NotContains(t, response.Body.String(), "/home")
		assert.NotContains(t, response.Body.String(), "/model-plaza")
	})

	t.Run("learn_pages_are_same_for_browsers_and_have_canonical_without_query", func(t *testing.T) {
		browser := seoRequest(t, router, http.MethodGet, "/learn/codex?utm_source=browser", "evil.example", "Mozilla/5.0")
		bot := seoRequest(t, router, http.MethodGet, "/learn/codex?utm_source=bot", "evil.example", "Googlebot/2.1")

		assert.Equal(t, http.StatusOK, browser.Code)
		assert.Equal(t, browser.Body.String(), bot.Body.String())
		assert.Contains(t, browser.Body.String(), "https://modelport.top/learn/codex")
		assert.NotContains(t, browser.Body.String(), "utm_source")
		assert.NotContains(t, browser.Body.String(), "evil.example")
		assert.Empty(t, browser.Header().Get("X-Robots-Tag"))
		assert.Empty(t, browser.Header().Get("ETag"))
	})

	t.Run("head_has_headers_without_body", func(t *testing.T) {
		response := seoRequest(t, router, http.MethodHead, "/sitemap.xml?utm_source=head", "evil.example", "Googlebot/2.1")

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Header().Get("Content-Type"), "application/xml")
		assert.NotEmpty(t, response.Header().Get("Content-Length"))
		assert.Empty(t, response.Body.String())
	})

	assert.Equal(t, 1, provider.called, "SEO settings should be reused across endpoint requests")
}

func TestAllPublicSEOPagesRenderTrustedHTMLAndValidJSONLD(t *testing.T) {
	_, _, router := newTrustedSEOTestServer(t, trustedSEOSettings())
	internalLearnHref := regexp.MustCompile(`href="(/learn(?:/[^"?#]*)?)"`)
	knownPages := make(map[string]struct{}, len(publicSEOPages))
	for _, page := range publicSEOPages {
		knownPages[normalizeSEOPath(page.Path)] = struct{}{}
	}

	for _, page := range publicSEOPages {
		page := page
		t.Run(page.Path, func(t *testing.T) {
			response := seoRequest(t, router, http.MethodGet, page.Path+"?utm_source=test", "evil.example", "Googlebot/2.1")
			require.Equal(t, http.StatusOK, response.Code)
			body := response.Body.String()
			assert.Contains(t, body, page.Body, "compile-time body should be rendered as HTML")
			assert.NotContains(t, body, "&lt;p class=", "body must not be HTML-escaped")
			assert.Contains(t, body, "https://modelport.top"+normalizeSEOPath(page.Path))

			scriptStart := strings.Index(body, `<script type="application/ld+json"`)
			require.GreaterOrEqual(t, scriptStart, 0)
			scriptStart = strings.Index(body[scriptStart:], ">") + scriptStart + 1
			scriptEnd := strings.Index(body[scriptStart:], "</script>") + scriptStart
			require.Greater(t, scriptEnd, scriptStart)
			var jsonLD map[string]any
			require.NoError(t, json.Unmarshal([]byte(body[scriptStart:scriptEnd]), &jsonLD))
			assert.Equal(t, page.Title, jsonLD["name"])
			assert.Equal(t, "https://modelport.top"+normalizeSEOPath(page.Path), jsonLD["url"])

			for _, match := range internalLearnHref.FindAllStringSubmatch(body, -1) {
				_, exists := knownPages[normalizeSEOPath(match[1])]
				assert.True(t, exists, "internal learn link must target a published page: %s", match[1])
			}
		})
	}
}

func TestPublicSEOHomeInjectionAndCacheIsolation(t *testing.T) {
	_, _, router := newTrustedSEOTestServer(t, trustedSEOSettings())

	root := seoRequest(t, router, http.MethodGet, "/?utm_source=root", "evil.example", "Mozilla/5.0")
	home := seoRequest(t, router, http.MethodGet, "/home?utm_source=home", "evil.example", "Googlebot/2.1")
	private := seoRequest(t, router, http.MethodGet, "/dashboard?utm_source=private", "evil.example", "Mozilla/5.0")
	rootAgain := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Mozilla/5.0")
	indexHTML := seoRequest(t, router, http.MethodGet, "/index.html?utm_source=index", "evil.example", "Mozilla/5.0")

	for _, response := range []*httptest.ResponseRecorder{root, home, rootAgain} {
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `name="description"`)
		assert.Contains(t, response.Body.String(), `content="https://modelport.top/"`)
		assert.Contains(t, response.Body.String(), `name="robots" content="index,follow"`)
		assert.Contains(t, response.Body.String(), `id="modelport-public-intro"`)
		assert.Contains(t, response.Body.String(), "ModelPort 提供多平台 AI API 接入、模型分组路由和用量计费。")
		assert.NotContains(t, response.Body.String(), "utm_source")
		assert.Empty(t, response.Header().Get("ETag"))
	}
	assert.Contains(t, root.Body.String(), "ModelPort AI API 中转站")
	assert.Less(t, strings.Index(root.Body.String(), `id="app"`), strings.Index(root.Body.String(), `id="modelport-public-intro"`), "intro should remain below the interactive app")
	assert.Equal(t, root.Body.String(), home.Body.String())
	assert.Equal(t, http.StatusPermanentRedirect, indexHTML.Code)
	assert.Equal(t, "/", indexHTML.Header().Get("Location"))
	assert.Empty(t, indexHTML.Body.String())
	assert.NotContains(t, indexHTML.Header().Get("Location"), "utm_source")

	assert.Equal(t, http.StatusOK, private.Code)
	assert.Equal(t, "noindex, nofollow", private.Header().Get("X-Robots-Tag"))
	assert.Contains(t, private.Body.String(), `name="robots" content="noindex,nofollow"`)
	assert.NotContains(t, private.Body.String(), `content="https://modelport.top/"`)
	assert.Empty(t, private.Header().Get("ETag"))

	for _, path := range []string{"/", "/home", "/dashboard"} {
		response := seoRequest(t, router, http.MethodPost, path, "evil.example", "Mozilla/5.0")
		assert.Equal(t, http.StatusMethodNotAllowed, response.Code, path)
		assert.Equal(t, "noindex, nofollow", response.Header().Get("X-Robots-Tag"), path)
	}
	headHome := seoRequest(t, router, http.MethodHead, "/home?utm_source=head", "evil.example", "Googlebot/2.1")
	assert.Equal(t, http.StatusOK, headHome.Code)
	assert.Empty(t, headHome.Body.String())
	assert.Empty(t, headHome.Header().Get("ETag"))
	headIndex := seoRequest(t, router, http.MethodHead, "/index.html?utm_source=head", "evil.example", "Googlebot/2.1")
	assert.Equal(t, http.StatusPermanentRedirect, headIndex.Code)
	assert.Equal(t, "/", headIndex.Header().Get("Location"))
	assert.Empty(t, headIndex.Body.String())
}

func TestPublicSEOUnknownAndPrivateRoutes(t *testing.T) {
	_, _, router := newTrustedSEOTestServer(t, trustedSEOSettings())

	for _, path := range []string{"/learn/not-a-page", "/not-a-route", "/assets/not-found.js"} {
		t.Run(path, func(t *testing.T) {
			response := seoRequest(t, router, http.MethodGet, path, "evil.example", "Googlebot/2.1")

			assert.Equal(t, http.StatusNotFound, response.Code)
			assert.Equal(t, "noindex, nofollow", response.Header().Get("X-Robots-Tag"))
			assert.Contains(t, response.Body.String(), `name="robots" content="noindex,nofollow"`)
		})
	}

	for _, path := range []string{"/dashboard", "/keys", "/custom/user-page", "/admin/settings"} {
		t.Run("private_"+strings.ReplaceAll(strings.Trim(path, "/"), "/", "_"), func(t *testing.T) {
			response := seoRequest(t, router, http.MethodGet, path, "evil.example", "Googlebot/2.1")

			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "noindex, nofollow", response.Header().Get("X-Robots-Tag"))
			assert.Contains(t, response.Body.String(), `name="robots" content="noindex,nofollow"`)
			assert.Empty(t, response.Header().Get("ETag"))
		})
	}
}

func TestPublicSEOGatesFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		settings any
		err      error
	}{
		{name: "non_modelport", settings: map[string]any{"site_name": "Other", "backend_mode_enabled": false}},
		{name: "backend_mode", settings: map[string]any{"site_name": "ModelPort", "backend_mode_enabled": true}},
		{name: "provider_error", err: context.DeadlineExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &mockSettingsProvider{settings: test.settings, err: test.err}
			server, err := NewFrontendServer(provider)
			require.NoError(t, err)
			router := gin.New()
			router.Use(server.Middleware())

			for _, path := range []string{"/robots.txt", "/sitemap.xml", "/learn"} {
				response := seoRequest(t, router, http.MethodGet, path, "evil.example", "Googlebot/2.1")
				assert.Equal(t, http.StatusNotFound, response.Code, path)
				assert.Equal(t, "noindex, nofollow", response.Header().Get("X-Robots-Tag"), path)
			}
			root := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Googlebot/2.1")
			assert.Equal(t, http.StatusOK, root.Code)
			assert.NotContains(t, root.Body.String(), "public-seo-intro")
			assert.NotContains(t, root.Body.String(), "https://modelport.top/")
		})
	}
}

func TestPublicSEORetriesRootAfterExpiredFailureSnapshot(t *testing.T) {
	provider := &mockSettingsProvider{err: context.DeadlineExceeded}
	server, err := NewFrontendServer(provider)
	require.NoError(t, err)
	router := gin.New()
	router.Use(server.Middleware())

	first := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Googlebot/2.1")
	assert.Equal(t, http.StatusOK, first.Code)
	assert.NotContains(t, first.Body.String(), "modelport-public-intro")
	require.NotNil(t, server.currentPublicSEOSnapshot())
	require.True(t, server.currentPublicSEOSnapshot().failure)

	provider.err = nil
	provider.settings = trustedSEOSettings()
	server.currentPublicSEOSnapshot().freshUntil = time.Now().Add(-time.Second)
	second := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Googlebot/2.1")

	assert.Equal(t, http.StatusOK, second.Code)
	assert.Contains(t, second.Body.String(), "modelport-public-intro")
	assert.Contains(t, second.Body.String(), modelPortHomeTitle)
	assert.Equal(t, 2, provider.called)
}

func TestPublicSEOInvalidationClosesSEOWhenBackendModeTurnsOn(t *testing.T) {
	provider := &mockSettingsProvider{settings: trustedSEOSettings()}
	server, err := NewFrontendServer(provider)
	require.NoError(t, err)
	router := gin.New()
	router.Use(server.Middleware())

	initial := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Googlebot/2.1")
	assert.Contains(t, initial.Body.String(), "modelport-public-intro")

	updated := trustedSEOSettings()
	updated["backend_mode_enabled"] = true
	provider.settings = updated
	server.InvalidateCache()

	root := seoRequest(t, router, http.MethodGet, "/", "evil.example", "Googlebot/2.1")
	learn := seoRequest(t, router, http.MethodGet, "/learn", "evil.example", "Googlebot/2.1")
	private := seoRequest(t, router, http.MethodGet, "/dashboard", "evil.example", "Googlebot/2.1")

	assert.Equal(t, http.StatusOK, root.Code)
	assert.NotContains(t, root.Body.String(), "modelport-public-intro")
	assert.NotContains(t, root.Body.String(), "https://modelport.top/")
	assert.Equal(t, http.StatusNotFound, learn.Code)
	assert.Equal(t, "noindex, nofollow", learn.Header().Get("X-Robots-Tag"))
	assert.Equal(t, http.StatusOK, private.Code)
	assert.Equal(t, "noindex, nofollow", private.Header().Get("X-Robots-Tag"))
	assert.NotContains(t, private.Body.String(), "modelport-public-intro")
}

func TestKnownSPARoutesRemainReachable(t *testing.T) {
	_, _, router := newTrustedSEOTestServer(t, trustedSEOSettings())
	for path := range knownSPARouteExact {
		path := path
		t.Run(path, func(t *testing.T) {
			response := seoRequest(t, router, http.MethodGet, path, "evil.example", "Mozilla/5.0")
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Header().Get("Content-Type"), "text/html")
		})
	}
	for _, path := range []string{"/legal/terms", "/guide/codex", "/custom/page-id"} {
		response := seoRequest(t, router, http.MethodGet, path, "evil.example", "Mozilla/5.0")
		assert.Equal(t, http.StatusOK, response.Code, path)
	}
}

func TestRenderPublicSEOPageRejectsNonLearnPath(t *testing.T) {
	_, err := renderPublicSEOPage(publicSEOPage{Path: "/admin", Title: "bad", Description: "bad", Body: "<script>bad</script>"}, "nonce")
	assert.Error(t, err)
}

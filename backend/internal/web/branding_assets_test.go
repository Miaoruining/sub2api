//go:build embed

package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var brandingTestPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x04, 0x00, 0x00, 0x00, 0xb5, 0x1c, 0x0c,
	0x02, 0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41,
	0x54, 0x78, 0xda, 0x63, 0x64, 0x60, 0xf8, 0x0f,
	0x00, 0x05, 0x01, 0x01, 0x27, 0x18, 0xe3, 0x66,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
}

func brandingTestDataURL(body []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(body)
}

func brandingTestServer(provider PublicSettingsProvider) *FrontendServer {
	return &FrontendServer{
		settings:    provider,
		seoSettings: newPublicSEOSettingsCache(),
	}
}

func brandingTestRouter(server *FrontendServer) *gin.Engine {
	router := gin.New()
	router.Use(server.Middleware())
	return router
}

func TestRewriteBrandingLogo(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestPNG)
	settings := []byte(`{"site_name":"ModelPort","site_logo":"` + dataURL + `","keep":{"value":true}}`)

	rewritten, asset := rewriteBrandingLogo(settings)
	require.NotNil(t, asset)
	require.NotEqual(t, settings, rewritten)
	assert.Less(t, len(rewritten), len(settings))
	assert.NotContains(t, string(rewritten), dataURL)
	assert.Contains(t, string(rewritten), brandingAssetURL(asset))

	var original, transformed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(settings, &original))
	require.NoError(t, json.Unmarshal(rewritten, &transformed))
	assert.Equal(t, original["site_name"], transformed["site_name"])
	assert.Equal(t, original["keep"], transformed["keep"])

	var cfg struct {
		SiteLogo string `json:"site_logo"`
	}
	require.NoError(t, json.Unmarshal(rewritten, &cfg))
	assert.Equal(t, brandingAssetURL(asset), cfg.SiteLogo)
}

func TestInjectSettingsBrandingLogoShrinksHTMLAndKeepsFaviconInSync(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestPNG)
	settings := []byte(`{"site_name":"ModelPort","site_logo":"` + dataURL + `"}`)
	server := &FrontendServer{
		baseHTML: []byte(`<!doctype html><html><head><link rel="icon" href="/logo.svg" /><title>Sub2API - AI API Gateway</title></head><body></body></html>`),
	}

	result := server.injectSettings(settings)
	_, asset := rewriteBrandingLogo(settings)
	require.NotNil(t, asset)
	url := brandingAssetURL(asset)
	// The original data URI would appear once in the config and once in the
	// favicon, so compare against that two-copy baseline.
	assert.Less(t, len(result), len(server.baseHTML)+2*len(settings))
	assert.NotContains(t, string(result), dataURL)
	assert.Equal(t, 2, strings.Count(string(result), url))
	assert.Contains(t, string(result), `window.__APP_CONFIG__={`)
	assert.Contains(t, string(result), `"site_logo":"`+url+`"`)
	assert.Contains(t, string(result), `<link rel="icon" href="`+url+`" />`)
}

func TestRewriteBrandingLogoLeavesUnsupportedValuesUnchanged(t *testing.T) {
	for _, value := range []string{
		"https://cdn.example/logo.png",
		"/uploads/logo.png",
		"data:image/svg+xml;base64,PHN2Zy8+",
		"data:image/png;base64,not-base64",
		"data:image/png,plain-text",
	} {
		t.Run(value, func(t *testing.T) {
			settings := []byte(`{"site_name":"ModelPort","site_logo":"` + value + `"}`)
			rewritten, asset := rewriteBrandingLogo(settings)
			assert.Nil(t, asset)
			assert.Equal(t, settings, rewritten)
		})
	}
}

func TestBrandingAssetRequest(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestPNG)
	provider := &mockSettingsProvider{settings: map[string]string{"site_logo": dataURL}}
	server := brandingTestServer(provider)
	router := brandingTestRouter(server)
	asset := parseBrandingDataURL(dataURL)
	require.NotNil(t, asset)
	assetPath := brandingAssetURL(asset)

	t.Run("get_returns_original_bytes_and_immutable_headers", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, assetPath, nil))

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, asset.mime, response.Header().Get("Content-Type"))
		assert.Equal(t, `public, max-age=31536000, immutable`, response.Header().Get("Cache-Control"))
		assert.Equal(t, `"`+asset.hash+`"`, response.Header().Get("ETag"))
		assert.Equal(t, string(asset.body), response.Body.String())
	})

	t.Run("head_has_headers_without_body", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodHead, assetPath, nil))

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Empty(t, response.Body.Bytes())
		assert.Equal(t, int64(len(asset.body)), response.Result().ContentLength)
		assert.Equal(t, `"`+asset.hash+`"`, response.Header().Get("ETag"))
	})

	t.Run("matching_etag_returns_304", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, assetPath, nil)
		request.Header.Set("If-None-Match", `W/"`+asset.hash+`"`)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotModified, response.Code)
		assert.Empty(t, response.Body.Bytes())
		assert.Equal(t, `"`+asset.hash+`"`, response.Header().Get("ETag"))
	})

	t.Run("post_and_unknown_paths_are_rejected", func(t *testing.T) {
		postResponse := httptest.NewRecorder()
		router.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, assetPath, nil))
		assert.Equal(t, http.StatusMethodNotAllowed, postResponse.Code)

		unknownResponse := httptest.NewRecorder()
		router.ServeHTTP(unknownResponse, httptest.NewRequest(http.MethodGet, "/branding/logo/"+strings.Repeat("0", 64)+".png", nil))
		assert.Equal(t, http.StatusNotFound, unknownResponse.Code)

		traversalResponse := httptest.NewRecorder()
		router.ServeHTTP(traversalResponse, httptest.NewRequest(http.MethodGet, "/branding/logo/../secret", nil))
		assert.Equal(t, http.StatusNotFound, traversalResponse.Code)
	})
}

func TestBrandingAssetURLChangesWhenLogoChanges(t *testing.T) {
	firstDataURL := brandingTestDataURL(brandingTestPNG)
	secondBody := append([]byte(nil), brandingTestPNG...)
	secondBody[45] ^= 0x01
	secondDataURL := brandingTestDataURL(secondBody)
	provider := &mockSettingsProvider{settings: map[string]string{"site_logo": firstDataURL}}
	server := brandingTestServer(provider)
	router := brandingTestRouter(server)
	firstAsset := parseBrandingDataURL(firstDataURL)
	secondAsset := parseBrandingDataURL(secondDataURL)
	require.NotNil(t, firstAsset)
	require.NotNil(t, secondAsset)
	require.NotEqual(t, firstAsset.hash, secondAsset.hash)

	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodGet, brandingAssetURL(firstAsset), nil))
	assert.Equal(t, http.StatusOK, firstResponse.Code)

	provider.settings = map[string]string{"site_logo": secondDataURL}
	server.InvalidateCache()

	oldResponse := httptest.NewRecorder()
	router.ServeHTTP(oldResponse, httptest.NewRequest(http.MethodGet, brandingAssetURL(firstAsset), nil))
	assert.Equal(t, http.StatusNotFound, oldResponse.Code)

	newResponse := httptest.NewRecorder()
	router.ServeHTTP(newResponse, httptest.NewRequest(http.MethodGet, brandingAssetURL(secondAsset), nil))
	assert.Equal(t, http.StatusOK, newResponse.Code)
	assert.True(t, bytes.Equal(secondBody, newResponse.Body.Bytes()))
}

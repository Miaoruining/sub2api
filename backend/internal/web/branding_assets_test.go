//go:build embed

package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
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

func brandingTestSizedPNG(width, height int) []byte {
	imageData := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8((x + y) % 256)
			imageData.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x7f, A: alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

func brandingTestServer(provider PublicSettingsProvider) *FrontendServer {
	return &FrontendServer{
		settings:      provider,
		seoSettings:   newPublicSEOSettingsCache(),
		brandingCache: newBrandingVariantCache(),
	}
}

func brandingTestRouter(server *FrontendServer) *gin.Engine {
	router := gin.New()
	router.Use(server.Middleware())
	return router
}

func TestRewriteBrandingLogo(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestSizedPNG(1024, 512))
	settings := []byte(`{"site_name":"ModelPort","site_logo":"` + dataURL + `","keep":{"value":true}}`)

	rewritten, asset := rewriteBrandingLogo(settings)
	require.NotNil(t, asset)
	require.NotEqual(t, settings, rewritten)
	assert.Less(t, len(rewritten), len(settings))
	assert.NotContains(t, string(rewritten), dataURL)
	variant := buildBrandingVariant(asset, brandingAppLogoSize)
	require.NotNil(t, variant)
	assert.Contains(t, string(rewritten), brandingVariantURL(asset, brandingAppLogoSize))

	var original, transformed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(settings, &original))
	require.NoError(t, json.Unmarshal(rewritten, &transformed))
	assert.Equal(t, original["site_name"], transformed["site_name"])
	assert.Equal(t, original["keep"], transformed["keep"])

	var cfg struct {
		SiteLogo string `json:"site_logo"`
	}
	require.NoError(t, json.Unmarshal(rewritten, &cfg))
	assert.Equal(t, brandingVariantURL(asset, brandingAppLogoSize), cfg.SiteLogo)
}

func TestInjectSettingsBrandingLogoShrinksHTMLAndKeepsFaviconInSync(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestSizedPNG(1024, 512))
	settings := []byte(`{"site_name":"ModelPort","site_logo":"` + dataURL + `"}`)
	server := &FrontendServer{
		baseHTML: []byte(`<!doctype html><html><head><link rel="icon" href="/logo.svg" /><title>Sub2API - AI API Gateway</title></head><body></body></html>`),
	}

	result := server.injectSettings(settings)
	_, asset := rewriteBrandingLogo(settings)
	require.NotNil(t, asset)
	appURL := brandingVariantURL(asset, brandingAppLogoSize)
	faviconURL := brandingVariantURL(asset, brandingFaviconSize)
	assert.Less(t, len(result), len(server.baseHTML)+len(settings))
	assert.NotContains(t, string(result), dataURL)
	assert.Equal(t, 2, strings.Count(string(result), appURL))
	assert.Equal(t, 1, strings.Count(string(result), faviconURL))
	assert.Contains(t, string(result), `window.__APP_CONFIG__={`)
	assert.Contains(t, string(result), `"site_logo":"`+appURL+`"`)
	assert.Contains(t, string(result), `<link rel="icon" href="`+faviconURL+`" data-branding-logo="`+appURL+`" />`)
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

func TestBrandingSmallRasterFallsBackToOriginalHashURL(t *testing.T) {
	dataURL := brandingTestDataURL(brandingTestPNG)
	settings := []byte(`{"site_logo":"` + dataURL + `"}`)
	rewritten, asset := rewriteBrandingLogo(settings)
	require.NotNil(t, asset)
	assert.Equal(t, `{"site_logo":"`+brandingAssetURL(asset)+`"}`, string(rewritten))

	server := &FrontendServer{
		baseHTML: []byte(`<!doctype html><html><head><link rel="icon" href="/logo.svg" /></head></html>`),
	}
	result := server.injectSettings(settings)
	assert.NotContains(t, string(result), dataURL)
	assert.Contains(t, string(result), `"site_logo":"`+brandingAssetURL(asset)+`"`)
	assert.Contains(t, string(result), `<link rel="icon" href="`+brandingAssetURL(asset)+`" data-branding-logo="`+brandingAssetURL(asset)+`" />`)
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

func TestBrandingVariantsPreserveDimensionsAndAlpha(t *testing.T) {
	body := brandingTestSizedPNG(1024, 512)
	asset := parseBrandingDataURL(brandingTestDataURL(body))
	require.NotNil(t, asset)

	appVariant := buildBrandingVariant(asset, brandingAppLogoSize)
	require.NotNil(t, appVariant)
	appConfig, err := png.DecodeConfig(bytes.NewReader(appVariant.body))
	require.NoError(t, err)
	assert.Equal(t, 256, appConfig.Width)
	assert.Equal(t, 128, appConfig.Height)
	assert.Less(t, len(appVariant.body), len(body))

	faviconVariant := buildBrandingVariant(asset, brandingFaviconSize)
	require.NotNil(t, faviconVariant)
	faviconConfig, err := png.DecodeConfig(bytes.NewReader(faviconVariant.body))
	require.NoError(t, err)
	assert.Equal(t, 48, faviconConfig.Width)
	assert.Equal(t, 24, faviconConfig.Height)

	decoded, err := png.Decode(bytes.NewReader(faviconVariant.body))
	require.NoError(t, err)
	_, _, _, alpha := decoded.At(0, 0).RGBA()
	assert.Less(t, alpha, uint32(0xffff), "transparent source pixels should remain transparent")
}

func TestBrandingJPEGVariant(t *testing.T) {
	imageData := image.NewRGBA(image.Rect(0, 0, 1024, 512))
	for y := 0; y < imageData.Bounds().Dy(); y++ {
		for x := 0; x < imageData.Bounds().Dx(); x++ {
			imageData.SetRGBA(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 251), B: 0x9f, A: 0xff})
		}
	}
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, imageData, &jpeg.Options{Quality: 90}))
	asset := parseBrandingDataURL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()))
	require.NotNil(t, asset)
	variant := buildBrandingVariant(asset, brandingAppLogoSize)
	require.NotNil(t, variant)
	config, err := png.DecodeConfig(bytes.NewReader(variant.body))
	require.NoError(t, err)
	assert.Equal(t, 256, config.Width)
	assert.Equal(t, 128, config.Height)
}

func TestBrandingVariantRejectsPixelBombBeforeDecode(t *testing.T) {
	// A valid PNG header declaring a huge image is enough to exercise the
	// DecodeConfig guard without allocating the declared pixel buffer.
	bomb := append([]byte(nil), brandingTestPNG...)
	// IHDR width and height are at offsets 16 and 20; the small fixture has a
	// valid CRC which we do not need to preserve because DecodeConfig rejects
	// it from the pixel budget before image.Decode is reached.
	bomb[16] = 0x7f
	bomb[17] = 0xff
	bomb[18] = 0xff
	bomb[19] = 0xff
	bomb[20] = 0x7f
	bomb[21] = 0xff
	bomb[22] = 0xff
	bomb[23] = 0xff
	asset := parseBrandingDataURL(brandingTestDataURL(bomb))
	require.NotNil(t, asset)
	assert.Nil(t, buildBrandingVariant(asset, brandingAppLogoSize))
}

func TestBrandingVariantRejectsAnimatedGIFAndKeepsOriginal(t *testing.T) {
	palette := color.Palette{color.Transparent, color.Black}
	first := image.NewPaletted(image.Rect(0, 0, 512, 256), palette)
	second := image.NewPaletted(image.Rect(0, 0, 512, 256), palette)
	first.SetColorIndex(0, 0, 1)
	second.SetColorIndex(0, 0, 0)
	var encoded bytes.Buffer
	require.NoError(t, gif.EncodeAll(&encoded, &gif.GIF{
		Image: []*image.Paletted{first, second},
		Delay: []int{1, 1},
	}))
	body := encoded.Bytes()
	dataURL := "data:image/gif;base64," + base64.StdEncoding.EncodeToString(body)
	asset := parseBrandingDataURL(dataURL)
	require.NotNil(t, asset)
	assert.Nil(t, buildBrandingVariant(asset, brandingAppLogoSize))
	settings := []byte(`{"site_logo":"` + dataURL + `"}`)
	rewritten, _ := rewriteBrandingLogo(settings)
	assert.Equal(t, `{"site_logo":"`+brandingAssetURL(asset)+`"}`, string(rewritten))
}

func TestBrandingVariantRequest(t *testing.T) {
	body := brandingTestSizedPNG(1024, 512)
	dataURL := brandingTestDataURL(body)
	provider := &mockSettingsProvider{settings: map[string]string{"site_logo": dataURL}}
	server := brandingTestServer(provider)
	router := brandingTestRouter(server)
	asset := parseBrandingDataURL(dataURL)
	require.NotNil(t, asset)
	variant := server.brandingVariant(asset, brandingAppLogoSize)
	require.NotNil(t, variant)
	variantPath := brandingVariantURL(asset, brandingAppLogoSize)

	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, variantPath, nil))
	assert.Equal(t, http.StatusOK, getResponse.Code)
	assert.Equal(t, "image/png", getResponse.Header().Get("Content-Type"))
	assert.Equal(t, `public, max-age=31536000, immutable`, getResponse.Header().Get("Cache-Control"))
	assert.Equal(t, `"`+variant.hash+`"`, getResponse.Header().Get("ETag"))
	assert.Equal(t, "nosniff", getResponse.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, variant.body, getResponse.Body.Bytes())

	headResponse := httptest.NewRecorder()
	router.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, variantPath, nil))
	assert.Equal(t, http.StatusOK, headResponse.Code)
	assert.Empty(t, headResponse.Body.Bytes())
	assert.Equal(t, int64(len(variant.body)), headResponse.Result().ContentLength)

	notModifiedRequest := httptest.NewRequest(http.MethodGet, variantPath, nil)
	notModifiedRequest.Header.Set("If-None-Match", `W/"`+variant.hash+`"`)
	notModifiedResponse := httptest.NewRecorder()
	router.ServeHTTP(notModifiedResponse, notModifiedRequest)
	assert.Equal(t, http.StatusNotModified, notModifiedResponse.Code)
	assert.Empty(t, notModifiedResponse.Body.Bytes())
}

func TestBrandingVariantUpdateInvalidatesOldSource(t *testing.T) {
	firstBody := brandingTestSizedPNG(1024, 512)
	secondBody := brandingTestSizedPNG(1536, 768)
	firstURL := brandingTestDataURL(firstBody)
	secondURL := brandingTestDataURL(secondBody)
	firstAsset := parseBrandingDataURL(firstURL)
	secondAsset := parseBrandingDataURL(secondURL)
	require.NotNil(t, firstAsset)
	require.NotNil(t, secondAsset)
	provider := &mockSettingsProvider{settings: map[string]string{"site_logo": firstURL}}
	server := brandingTestServer(provider)
	router := brandingTestRouter(server)

	firstVariant := server.brandingVariant(firstAsset, brandingAppLogoSize)
	require.NotNil(t, firstVariant)
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodGet, brandingVariantURL(firstAsset, brandingAppLogoSize), nil))
	assert.Equal(t, http.StatusOK, firstResponse.Code)

	provider.settings = map[string]string{"site_logo": secondURL}
	server.InvalidateCache()
	oldResponse := httptest.NewRecorder()
	router.ServeHTTP(oldResponse, httptest.NewRequest(http.MethodGet, brandingVariantURL(firstAsset, brandingAppLogoSize), nil))
	assert.Equal(t, http.StatusNotFound, oldResponse.Code)

	secondVariant := server.brandingVariant(secondAsset, brandingAppLogoSize)
	require.NotNil(t, secondVariant)
	newResponse := httptest.NewRecorder()
	router.ServeHTTP(newResponse, httptest.NewRequest(http.MethodGet, brandingVariantURL(secondAsset, brandingAppLogoSize), nil))
	assert.Equal(t, http.StatusOK, newResponse.Code)
	assert.Equal(t, secondVariant.body, newResponse.Body.Bytes())
}

func TestBrandingVariantCacheIsBoundedAndInvalidatable(t *testing.T) {
	cache := newBrandingVariantCache()
	cache.maxEntries = 1
	firstAsset := parseBrandingDataURL(brandingTestDataURL(brandingTestSizedPNG(1024, 512)))
	secondAsset := parseBrandingDataURL(brandingTestDataURL(brandingTestSizedPNG(1536, 768)))
	require.NotNil(t, firstAsset)
	require.NotNil(t, secondAsset)
	require.NotNil(t, cache.getOrCreate(firstAsset, brandingAppLogoSize))
	require.NotNil(t, cache.getOrCreate(secondAsset, brandingAppLogoSize))
	cache.mu.Lock()
	assert.LessOrEqual(t, cache.order.Len(), 1)
	assert.LessOrEqual(t, cache.bytes, brandingVariantCacheMaxBytes)
	cache.mu.Unlock()
	cache.invalidate()
	cache.mu.Lock()
	assert.Zero(t, cache.order.Len())
	assert.Zero(t, cache.bytes)
	cache.mu.Unlock()
}

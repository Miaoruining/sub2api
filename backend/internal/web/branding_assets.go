//go:build embed

package web

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	xdraw "golang.org/x/image/draw"
)

const maxBrandingAssetBytes = 300 * 1024

const (
	brandingAppLogoSize    = 256
	brandingFaviconSize    = 48
	brandingVariantVersion = "v1"

	// A small compressed image can still declare dimensions large enough to
	// exhaust memory. DecodeConfig is always checked against this budget before
	// image.Decode is allowed to allocate pixels.
	maxBrandingAssetPixels uint64 = 16 * 1024 * 1024

	// A variant is used only when it materially reduces the bytes sent in HTML
	// and by the browser. This also keeps unusually well-compressed source files
	// on the existing original hash URL behavior.
	brandingVariantMaxSizeRatioNumerator   = 9
	brandingVariantMaxSizeRatioDenominator = 10

	brandingVariantCacheMaxEntries = 16
	brandingVariantCacheMaxBytes   = 4 * 1024 * 1024
)

type brandingAsset struct {
	hash      string
	extension string
	mime      string
	body      []byte
}

type brandingVariant struct {
	size      int
	hash      string
	extension string
	mime      string
	body      []byte
}

func brandingVariantURL(asset *brandingAsset, size int) string {
	if asset == nil || !brandingVariantSizeAllowed(size) {
		return ""
	}
	// The source content hash plus a fixed size variant is an immutable,
	// content-addressed version path. The source hash changes whenever the
	// configured logo bytes change, while the size suffix selects the stable
	// encoder variant.
	return "/branding/logo/" + asset.hash + "-" + brandingVariantVersion + "-" + strconv.Itoa(size) + ".png"
}

func brandingVariantSizeAllowed(size int) bool {
	return size == brandingAppLogoSize || size == brandingFaviconSize
}

func brandingVariantURLForVariant(asset *brandingAsset, variant *brandingVariant) string {
	if variant == nil {
		return ""
	}
	return brandingVariantURL(asset, variant.size)
}

// brandingVariantCache is a small per-FrontendServer LRU. Derived bytes are
// immutable after insertion, so handlers can safely reuse the returned value.
// Keeping it on the server avoids a process-wide unbounded cache and lets the
// existing settings invalidation callback discard old logo variants.
type brandingVariantCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	order      *list.List
	bytes      int
	maxEntries int
	maxBytes   int
}

type brandingVariantCacheEntry struct {
	cacheKey string
	variant  *brandingVariant
}

func newBrandingVariantCache() *brandingVariantCache {
	return &brandingVariantCache{
		entries:    make(map[string]*list.Element),
		order:      list.New(),
		maxEntries: brandingVariantCacheMaxEntries,
		maxBytes:   brandingVariantCacheMaxBytes,
	}
}

func (c *brandingVariantCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]*list.Element)
	if c.order == nil {
		c.order = list.New()
	} else {
		c.order.Init()
	}
	c.bytes = 0
	c.mu.Unlock()
}

func brandingVariantCacheKey(asset *brandingAsset, size int) string {
	if asset == nil {
		return ""
	}
	return asset.hash + ":" + strconv.Itoa(size)
}

func (c *brandingVariantCache) getOrCreate(asset *brandingAsset, size int) *brandingVariant {
	if asset == nil || !brandingVariantSizeAllowed(size) {
		return nil
	}
	if c == nil {
		return buildBrandingVariant(asset, size)
	}

	key := brandingVariantCacheKey(asset, size)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*list.Element)
	}
	if c.order == nil {
		c.order = list.New()
	}
	if c.maxEntries <= 0 {
		c.maxEntries = brandingVariantCacheMaxEntries
	}
	if c.maxBytes <= 0 {
		c.maxBytes = brandingVariantCacheMaxBytes
	}
	if element := c.entries[key]; element != nil {
		c.order.MoveToFront(element)
		return element.Value.(*brandingVariantCacheEntry).variant
	}

	// Keep generation under the same lock. It makes concurrent first requests
	// share one decode/scale operation instead of doing duplicate work.
	variant := buildBrandingVariant(asset, size)
	if variant == nil {
		return nil
	}
	if len(variant.body) > c.maxBytes {
		return variant
	}
	for c.order.Len() >= c.maxEntries || c.bytes+len(variant.body) > c.maxBytes {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*brandingVariantCacheEntry)
		delete(c.entries, entry.cacheKey)
		c.bytes -= len(entry.variant.body)
		c.order.Remove(oldest)
	}
	entry := &brandingVariantCacheEntry{cacheKey: key, variant: variant}
	c.entries[key] = c.order.PushFront(entry)
	c.bytes += len(variant.body)
	return variant
}

// rewriteBrandingLogo rewrites only the HTML copy of site_logo. The raw
// public-settings snapshot remains the original data URI so the database and
// public settings API retain their existing semantics. If a source cannot
// produce a useful thumbnail, the HTML copy uses the original hash URL.
func rewriteBrandingLogo(settingsJSON []byte) ([]byte, *brandingAsset) {
	asset := brandingAssetFromSettings(settingsJSON)
	variant := buildBrandingVariant(asset, brandingAppLogoSize)
	return rewriteBrandingLogoWithVariant(settingsJSON, asset, variant), asset
}

func brandingAssetFromSettings(settingsJSON []byte) *brandingAsset {
	var cfg struct {
		SiteLogo string `json:"site_logo"`
	}
	if err := json.Unmarshal(settingsJSON, &cfg); err != nil {
		return nil
	}
	return parseBrandingDataURL(cfg.SiteLogo)
}

func rewriteBrandingLogoWithVariant(settingsJSON []byte, asset *brandingAsset, variant *brandingVariant) []byte {
	if asset == nil {
		return settingsJSON
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(settingsJSON, &fields); err != nil {
		return settingsJSON
	}
	if _, ok := fields["site_logo"]; !ok {
		return settingsJSON
	}
	logoURLValue := brandingAssetURL(asset)
	if variant != nil {
		logoURLValue = brandingVariantURLForVariant(asset, variant)
	}
	logoURL, err := json.Marshal(logoURLValue)
	if err != nil {
		return settingsJSON
	}
	fields["site_logo"] = logoURL
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return settingsJSON
	}
	return rewritten
}

func brandingAssetURL(asset *brandingAsset) string {
	if asset == nil {
		return ""
	}
	return "/branding/logo/" + asset.hash + "." + asset.extension
}

// parseBrandingDataURL accepts the bounded raster formats emitted by the
// settings image uploader. The decoded bytes are checked against the declared
// MIME type before they can be exposed as a static asset.
func parseBrandingDataURL(value string) *brandingAsset {
	value = strings.TrimSpace(value)
	if len(value) < len("data:") || !strings.EqualFold(value[:len("data:")], "data:") {
		return nil
	}
	comma := strings.IndexByte(value, ',')
	if comma <= len("data:") {
		return nil
	}

	metadata := value[len("data:"):comma]
	parts := strings.Split(metadata, ";")
	if len(parts) < 2 || !strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "base64") {
		return nil
	}
	mime, extension, ok := brandingMIMEAndExtension(strings.TrimSpace(parts[0]))
	if !ok {
		return nil
	}

	body, err := decodeBrandingBase64(value[comma+1:])
	if err != nil || len(body) == 0 || len(body) > maxBrandingAssetBytes {
		return nil
	}
	if http.DetectContentType(body) != mime {
		return nil
	}

	digest := sha256.Sum256(body)
	return &brandingAsset{
		hash:      hex.EncodeToString(digest[:]),
		extension: extension,
		mime:      mime,
		body:      body,
	}
}

// buildBrandingVariant validates a source as a static raster image and emits
// a bounded PNG thumbnail. It intentionally returns nil for unsupported
// formats (including GIF/WebP), malformed, oversized, or not materially
// smaller inputs so callers can retain the original hash URL.
func buildBrandingVariant(asset *brandingAsset, size int) *brandingVariant {
	if asset == nil || !brandingVariantSizeAllowed(size) {
		return nil
	}

	if asset.extension != "png" && asset.extension != "jpg" {
		return nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(asset.body))
	if err != nil || config.Width <= 0 || config.Height <= 0 || !brandingPixelsWithinBudget(config.Width, config.Height) {
		return nil
	}

	width, height := brandingVariantDimensions(config.Width, config.Height, size)
	if width >= config.Width && height >= config.Height {
		return nil
	}

	source, _, err := image.Decode(bytes.NewReader(asset.body))
	if err != nil {
		return nil
	}
	thumbnail := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, source.Bounds(), xdraw.Over, nil)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumbnail); err != nil || encoded.Len() == 0 {
		return nil
	}
	if encoded.Len()*brandingVariantMaxSizeRatioDenominator > len(asset.body)*brandingVariantMaxSizeRatioNumerator {
		return nil
	}
	body := append([]byte(nil), encoded.Bytes()...)
	digest := sha256.Sum256(body)
	return &brandingVariant{
		size:      size,
		hash:      hex.EncodeToString(digest[:]),
		extension: "png",
		mime:      "image/png",
		body:      body,
	}
}

func brandingPixelsWithinBudget(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	return uint64(width) <= maxBrandingAssetPixels/uint64(height) && uint64(height) <= maxBrandingAssetPixels/uint64(width)
}

func brandingVariantDimensions(width, height, size int) (int, int) {
	maxDimension := width
	if height > maxDimension {
		maxDimension = height
	}
	// The pixel budget above keeps this multiplication comfortably within int.
	variantWidth := int(math.Round(float64(width) * float64(size) / float64(maxDimension)))
	variantHeight := int(math.Round(float64(height) * float64(size) / float64(maxDimension)))
	if variantWidth < 1 {
		variantWidth = 1
	}
	if variantHeight < 1 {
		variantHeight = 1
	}
	return variantWidth, variantHeight
}

func decodeBrandingBase64(encoded string) ([]byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(encoded))
	// The limit bounds allocation even when a malformed setting contains an
	// unexpectedly large base64 value.
	body, err := io.ReadAll(io.LimitReader(decoder, maxBrandingAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBrandingAssetBytes {
		return nil, io.ErrShortBuffer
	}
	return body, nil
}

func brandingMIMEAndExtension(value string) (string, string, bool) {
	switch strings.ToLower(value) {
	case "image/png":
		return "image/png", "png", true
	case "image/jpeg":
		return "image/jpeg", "jpg", true
	case "image/gif":
		return "image/gif", "gif", true
	case "image/webp":
		return "image/webp", "webp", true
	default:
		return "", "", false
	}
}

func parseBrandingAssetPath(path string) (string, string, bool) {
	const prefix = "/branding/logo/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	tail := strings.TrimPrefix(path, prefix)
	dot := strings.LastIndexByte(tail, '.')
	if dot != sha256.Size*2 || dot == len(tail)-1 {
		return "", "", false
	}
	hash := tail[:dot]
	for _, ch := range hash {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", "", false
		}
	}
	extension, ok := brandingExtension(tail[dot+1:])
	if !ok {
		return "", "", false
	}
	return hash, extension, true
}

func parseBrandingVariantPath(path string) (string, int, bool) {
	const prefix = "/branding/logo/"
	if !strings.HasPrefix(path, prefix) {
		return "", 0, false
	}
	tail := strings.TrimPrefix(path, prefix)
	if !strings.HasSuffix(tail, ".png") {
		return "", 0, false
	}
	tail = strings.TrimSuffix(tail, ".png")
	versionMarker := "-" + brandingVariantVersion + "-"
	marker := strings.Index(tail, versionMarker)
	if marker != sha256.Size*2 || marker+len(versionMarker) == len(tail) {
		return "", 0, false
	}
	hash := tail[:marker]
	for _, ch := range hash {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", 0, false
		}
	}
	size, err := strconv.Atoi(tail[marker+len(versionMarker):])
	if err != nil || !brandingVariantSizeAllowed(size) {
		return "", 0, false
	}
	return hash, size, true
}

func brandingExtension(extension string) (string, bool) {
	switch extension {
	case "png":
		return "png", true
	case "jpg":
		return "jpg", true
	case "gif":
		return "gif", true
	case "webp":
		return "webp", true
	default:
		return "", false
	}
}

func (s *FrontendServer) handleBrandingAssetRequest(c *gin.Context) bool {
	const prefix = "/branding/logo"
	path := c.Request.URL.Path
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return false
	}
	if !allowedSEOHTTPMethod(c.Request.Method) {
		writeSEOMethodNotAllowed(c)
		return true
	}

	hash, extension, ok := parseBrandingAssetPath(path)
	variantSize := 0
	isVariant := false
	if !ok {
		var variantOK bool
		hash, variantSize, variantOK = parseBrandingVariantPath(path)
		if !variantOK {
			writeSEONotFound(c)
			return true
		}
		isVariant = true
		extension = "png"
	}
	if hash == "" {
		writeSEONotFound(c)
		return true
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	seoSnapshot, err := s.loadPublicSEOSnapshot(ctx)
	cancel()
	if err != nil || seoSnapshot == nil {
		writeSEONotFound(c)
		return true
	}
	var cfg struct {
		SiteLogo string `json:"site_logo"`
	}
	if json.Unmarshal(seoSnapshot.raw, &cfg) != nil {
		writeSEONotFound(c)
		return true
	}
	asset := parseBrandingDataURL(cfg.SiteLogo)
	if asset == nil || asset.hash != hash || (!isVariant && asset.extension != extension) {
		writeSEONotFound(c)
		return true
	}

	body := asset.body
	mime := asset.mime
	etag := `"` + asset.hash + `"`
	if isVariant {
		variant := s.brandingVariant(asset, variantSize)
		if variant == nil {
			writeSEONotFound(c)
			return true
		}
		body = variant.body
		mime = variant.mime
		etag = `"` + variant.hash + `"`
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Header("Content-Type", mime)
	c.Header("X-Content-Type-Options", "nosniff")
	if brandingETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		c.Abort()
		return true
	}
	c.Header("Content-Length", strconv.Itoa(len(body)))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		c.Abort()
		return true
	}
	c.Data(http.StatusOK, mime, body)
	c.Abort()
	return true
}

func (s *FrontendServer) brandingVariant(asset *brandingAsset, size int) *brandingVariant {
	if s == nil {
		return buildBrandingVariant(asset, size)
	}
	if s.brandingCache == nil {
		return buildBrandingVariant(asset, size)
	}
	return s.brandingCache.getOrCreate(asset, size)
}

func brandingETagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

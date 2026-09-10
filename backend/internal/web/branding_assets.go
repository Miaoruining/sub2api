//go:build embed

package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxBrandingAssetBytes = 300 * 1024

type brandingAsset struct {
	hash      string
	extension string
	mime      string
	body      []byte
}

// rewriteBrandingLogo rewrites only the HTML copy of site_logo. The raw
// public-settings snapshot remains the original data URI so the database and
// public settings API retain their existing semantics.
func rewriteBrandingLogo(settingsJSON []byte) ([]byte, *brandingAsset) {
	var cfg struct {
		SiteLogo string `json:"site_logo"`
	}
	if err := json.Unmarshal(settingsJSON, &cfg); err != nil {
		return settingsJSON, nil
	}
	asset := parseBrandingDataURL(cfg.SiteLogo)
	if asset == nil {
		return settingsJSON, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(settingsJSON, &fields); err != nil {
		return settingsJSON, nil
	}
	if _, ok := fields["site_logo"]; !ok {
		return settingsJSON, nil
	}
	logoURL, err := json.Marshal(brandingAssetURL(asset))
	if err != nil {
		return settingsJSON, nil
	}
	fields["site_logo"] = logoURL
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return settingsJSON, nil
	}
	return rewritten, asset
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
	if !ok {
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
	if asset == nil || asset.hash != hash || asset.extension != extension {
		writeSEONotFound(c)
		return true
	}

	etag := `"` + asset.hash + `"`
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Header("Content-Type", asset.mime)
	c.Header("X-Content-Type-Options", "nosniff")
	if brandingETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		c.Abort()
		return true
	}
	c.Header("Content-Length", strconv.Itoa(len(asset.body)))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		c.Abort()
		return true
	}
	c.Data(http.StatusOK, asset.mime, asset.body)
	c.Abort()
	return true
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

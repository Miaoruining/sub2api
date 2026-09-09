//go:build embed

package web

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	htmlpkg "html"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// modelPortCanonicalOrigin is deliberately fixed.  In particular, it must
// not be derived from Host or X-Forwarded-Host, otherwise a hostile request
// can poison canonical and sitemap URLs.
const modelPortCanonicalOrigin = "https://modelport.top"

const seoSettingsFailureTTL = 15 * time.Second

const (
	modelPortHomeTitle       = "ModelPort AI API 中转站｜模型接入、计费与使用指南"
	modelPortHomeDescription = "ModelPort 提供多平台 AI API 接入、模型分组路由和用量计费。查看 Codex、Claude Code 接入指南，以及模型倍率、拼团订阅与每日抽奖说明。"
)

// The page and body are compile-time content supplied by seo_content.go.  The
// template is embedded separately so public pages are still served when the
// SPA is not bootstrapped by a browser or crawler.
//
//go:embed seo_public_template.html
var publicSEOTemplateSource string

var publicSEOTemplate = template.Must(template.New("sub2api-public-seo").Parse(publicSEOTemplateSource))

type publicSEOTemplatePage struct {
	Path        string
	Title       string
	Description string
	Body        template.HTML
}

type publicSEOTemplateData struct {
	Page      publicSEOTemplatePage
	Canonical string
	JSONLD    template.JS
	Nonce     string
}

// publicSEOSettings is intentionally a small allowlist of settings.  It is
// not the public settings payload and must never acquire user/account fields.
type publicSEOSettings struct {
	SiteName           string `json:"site_name"`
	SiteSubtitle       string `json:"site_subtitle"`
	HomeContent        string `json:"home_content"`
	BackendModeEnabled bool   `json:"backend_mode_enabled"`
	siteNamePresent    bool
	backendModePresent bool
}

type publicSEOSnapshot struct {
	raw      []byte
	settings publicSEOSettings
	trusted  bool
	failure  bool
	// Failures are briefly cached to avoid turning an unavailable settings DB
	// into a DB request for every crawler/asset request.  Successful settings
	// are invalidated by FrontendServer.InvalidateCache instead.
	freshUntil time.Time
}

type publicSEOSettingsCache struct {
	mu       sync.RWMutex
	snapshot *publicSEOSnapshot
}

func newPublicSEOSettingsCache() *publicSEOSettingsCache {
	return &publicSEOSettingsCache{}
}

func (c *publicSEOSettingsCache) getFresh() *publicSEOSnapshot {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.snapshot == nil {
		return nil
	}
	if c.snapshot.failure && time.Now().After(c.snapshot.freshUntil) {
		return nil
	}
	return c.snapshot
}

func (c *publicSEOSettingsCache) getLatest() *publicSEOSnapshot {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *publicSEOSettingsCache) setSuccess(raw []byte, settings publicSEOSettings) *publicSEOSnapshot {
	if c == nil {
		return &publicSEOSnapshot{raw: raw, settings: settings, trusted: isTrustedPublicSEOSettings(settings)}
	}
	snapshot := &publicSEOSnapshot{
		raw:      append([]byte(nil), raw...),
		settings: settings,
		trusted:  isTrustedPublicSEOSettings(settings),
	}
	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
	return snapshot
}

func (c *publicSEOSettingsCache) setFailure() *publicSEOSnapshot {
	snapshot := &publicSEOSnapshot{failure: true, freshUntil: time.Now().Add(seoSettingsFailureTTL)}
	if c == nil {
		return snapshot
	}
	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
	return snapshot
}

func (c *publicSEOSettingsCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.snapshot = nil
	c.mu.Unlock()
}

func isTrustedPublicSEOSettings(settings publicSEOSettings) bool {
	return settings.siteNamePresent && settings.backendModePresent &&
		strings.TrimSpace(settings.SiteName) == "ModelPort" && !settings.BackendModeEnabled
}

func (s *FrontendServer) currentPublicSEOSnapshot() *publicSEOSnapshot {
	if s == nil || s.seoSettings == nil {
		return nil
	}
	return s.seoSettings.getLatest()
}

// loadPublicSEOSnapshot shares the same settings fetch used by injected SPA
// HTML.  A successful snapshot is invalidated by the existing settings update
// callback; only a short-lived failure snapshot has a time-based expiry.
func (s *FrontendServer) loadPublicSEOSnapshot(ctx context.Context) (*publicSEOSnapshot, error) {
	if s == nil || s.settings == nil {
		return s.markPublicSEOFailure(), errors.New("public settings provider is unavailable")
	}
	if cached := s.seoSettingsFresh(); cached != nil {
		if cached.failure {
			return cached, errors.New("public settings snapshot unavailable")
		}
		return cached, nil
	}

	settings, err := s.settings.GetPublicSettingsForInjection(ctx)
	if err != nil {
		return s.markPublicSEOFailure(), fmt.Errorf("get public settings for seo: %w", err)
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return s.markPublicSEOFailure(), fmt.Errorf("marshal public settings for seo: %w", err)
	}
	var parsed publicSEOSettings
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return s.markPublicSEOFailure(), fmt.Errorf("parse public settings for seo: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return s.markPublicSEOFailure(), fmt.Errorf("inspect public settings for seo: %w", err)
	}
	_, parsed.siteNamePresent = fields["site_name"]
	_, parsed.backendModePresent = fields["backend_mode_enabled"]
	if s.seoSettings == nil {
		return &publicSEOSnapshot{
			raw:      raw,
			settings: parsed,
			trusted:  isTrustedPublicSEOSettings(parsed),
		}, nil
	}
	return s.seoSettings.setSuccess(raw, parsed), nil
}

func (s *FrontendServer) seoSettingsFresh() *publicSEOSnapshot {
	if s == nil || s.seoSettings == nil {
		return nil
	}
	return s.seoSettings.getFresh()
}

func (s *FrontendServer) markPublicSEOFailure() *publicSEOSnapshot {
	if s == nil || s.seoSettings == nil {
		return &publicSEOSnapshot{failure: true, freshUntil: time.Now().Add(seoSettingsFailureTTL)}
	}
	return s.seoSettings.setFailure()
}

func normalizeSEOPath(rawPath string) string {
	parsed, err := url.Parse(rawPath)
	if err != nil {
		rawPath = strings.SplitN(rawPath, "?", 2)[0]
	} else {
		rawPath = parsed.Path
	}
	if rawPath == "" {
		return "/"
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	for len(rawPath) > 1 && strings.HasSuffix(rawPath, "/") {
		rawPath = strings.TrimSuffix(rawPath, "/")
	}
	return rawPath
}

func canonicalSEOURL(path string) string {
	path = normalizeSEOPath(path)
	if path == "/" {
		return modelPortCanonicalOrigin + "/"
	}
	return modelPortCanonicalOrigin + path
}

func publicSEOPageForPath(path string) (*publicSEOPage, bool) {
	path = normalizeSEOPath(path)
	if path != "/learn" && !strings.HasPrefix(path, "/learn/") {
		return nil, false
	}
	for i := range publicSEOPages {
		pagePath := normalizeSEOPath(publicSEOPages[i].Path)
		if pagePath == path {
			return &publicSEOPages[i], true
		}
	}
	return nil, false
}

func isPublicSEOPath(path string) bool {
	path = normalizeSEOPath(path)
	return path == "/learn" || strings.HasPrefix(path, "/learn/")
}

func allowedSEOHTTPMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func writeSEOResponse(c *gin.Context, status int, contentType string, body []byte, noIndex bool) {
	if noIndex {
		c.Header("X-Robots-Tag", "noindex, nofollow")
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Content-Length", strconv.Itoa(len(body)))
	if c.Request != nil && c.Request.Method == http.MethodHead {
		c.Status(status)
		c.Abort()
		return
	}
	c.Data(status, contentType, body)
	c.Abort()
}

func writeSEOHTML(c *gin.Context, status int, body []byte, noIndex bool) {
	writeSEOResponse(c, status, "text/html; charset=utf-8", body, noIndex)
}

func writeSEOMethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "GET, HEAD")
	writeSEOResponse(c, http.StatusMethodNotAllowed, "text/plain; charset=utf-8", []byte("Method Not Allowed"), true)
}

func handleIndexHTMLRedirect(c *gin.Context) bool {
	if normalizeSEOPath(c.Request.URL.Path) != "/index.html" {
		return false
	}
	if !allowedSEOHTTPMethod(c.Request.Method) {
		writeSEOMethodNotAllowed(c)
		return true
	}
	// Keep the canonical URL relative and intentionally omit RawQuery.  The
	// homepage is represented by /; /index.html must not become a second HTML
	// representation with its own metadata.
	c.Header("Location", "/")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusPermanentRedirect)
	c.Abort()
	return true
}

func writeSEONotFound(c *gin.Context) {
	body := []byte("<!doctype html><html lang=\"en\"><head><meta charset=\"UTF-8\"><meta name=\"robots\" content=\"noindex,nofollow\"><title>404 Not Found</title></head><body><h1>404 Not Found</h1></body></html>")
	writeSEOHTML(c, http.StatusNotFound, body, true)
}

func (s *FrontendServer) handlePublicSEORequest(c *gin.Context) bool {
	path := normalizeSEOPath(c.Request.URL.Path)
	isRobots := path == "/robots.txt"
	isSitemap := path == "/sitemap.xml"
	isLearn := isPublicSEOPath(path)
	if !isRobots && !isSitemap && !isLearn {
		return false
	}
	if !allowedSEOHTTPMethod(c.Request.Method) {
		writeSEOMethodNotAllowed(c)
		return true
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	snapshot, err := s.loadPublicSEOSnapshot(ctx)
	if err != nil || snapshot == nil || !snapshot.trusted {
		writeSEONotFound(c)
		return true
	}
	if isRobots {
		writeSEOResponse(c, http.StatusOK, "text/plain; charset=utf-8", []byte(publicRobotsText()), false)
		return true
	}
	if isSitemap {
		body, err := renderPublicSitemap()
		if err != nil {
			writeSEONotFound(c)
			return true
		}
		writeSEOResponse(c, http.StatusOK, "application/xml; charset=utf-8", body, false)
		return true
	}

	page, ok := publicSEOPageForPath(path)
	if !ok {
		writeSEONotFound(c)
		return true
	}
	body, err := renderPublicSEOPage(*page, middleware.GetNonceFromContext(c))
	if err != nil {
		writeSEONotFound(c)
		return true
	}
	writeSEOHTML(c, http.StatusOK, body, false)
	return true
}

func publicRobotsText() string {
	return strings.Join([]string{
		"User-agent: *",
		"Allow: /",
		"Allow: /learn",
		"Allow: /learn/",
		"Disallow: /api/",
		"Disallow: /v1/",
		"Disallow: /v1beta/",
		"Disallow: /backend-api/",
		"Disallow: /antigravity/",
		"Disallow: /responses",
		"Disallow: /models",
		"Disallow: /images/",
		"Disallow: /videos/",
		"Disallow: /admin/",
		"Disallow: /dashboard",
		"Disallow: /keys",
		"Disallow: /usage",
		"Disallow: /profile",
		"Disallow: /subscriptions",
		"Disallow: /purchase",
		"Disallow: /orders",
		"Disallow: /payment/",
		"Disallow: /custom/",
		"Disallow: /auth/",
		"Disallow: /setup",
		"Sitemap: " + canonicalSEOURL("/sitemap.xml"),
		"",
	}, "\n")
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func renderPublicSitemap() ([]byte, error) {
	urls := []sitemapURL{{Loc: canonicalSEOURL("/")}}
	seen := map[string]struct{}{normalizeSEOPath("/"): {}}
	for _, page := range publicSEOPages {
		path := normalizeSEOPath(page.Path)
		if path != "/learn" && !strings.HasPrefix(path, "/learn/") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		urls = append(urls, sitemapURL{Loc: canonicalSEOURL(path)})
	}
	result, err := xml.MarshalIndent(sitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(result, '\n')...), nil
}

func renderPublicSEOPage(page publicSEOPage, nonce string) ([]byte, error) {
	path := normalizeSEOPath(page.Path)
	if path != "/learn" && !strings.HasPrefix(path, "/learn/") {
		return nil, errors.New("invalid public seo page path")
	}
	jsonLD, err := json.Marshal(map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebPage",
		"name":        page.Title,
		"description": page.Description,
		"url":         canonicalSEOURL(path),
		"isPartOf": map[string]string{
			"@type": "WebSite",
			"name":  "ModelPort",
			"url":   canonicalSEOURL("/"),
		},
	})
	if err != nil {
		return nil, err
	}
	data := publicSEOTemplateData{
		Page: publicSEOTemplatePage{
			Path:        path,
			Title:       page.Title,
			Description: page.Description,
			Body:        template.HTML(page.Body), // page body is compile-time trusted HTML
		},
		Canonical: canonicalSEOURL(path),
		JSONLD:    template.JS(jsonLD), // json.Marshal output, not arbitrary input
		Nonce:     nonce,
	}
	var rendered bytes.Buffer
	if err := publicSEOTemplate.Execute(&rendered, data); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}

func replaceHTMLTitle(html []byte, title string) []byte {
	start := bytes.Index(html, []byte("<title>"))
	end := bytes.Index(html, []byte("</title>"))
	if start < 0 || end <= start {
		return html
	}
	titleHTML := []byte("<title>" + htmlpkg.EscapeString(title) + "</title>")
	result := make([]byte, 0, len(html)+len(titleHTML)-(end+len("</title>")-start))
	result = append(result, html[:start]...)
	result = append(result, titleHTML...)
	result = append(result, html[end+len("</title>"):]...)
	return result
}

const seoRouteMarkerStart = "<!-- sub2api-seo-route:start -->"
const seoRouteMarkerEnd = "<!-- sub2api-seo-route:end -->"

func removeSEORouteTags(html []byte) []byte {
	start := bytes.Index(html, []byte(seoRouteMarkerStart))
	if start < 0 {
		return html
	}
	relEnd := bytes.Index(html[start+len(seoRouteMarkerStart):], []byte(seoRouteMarkerEnd))
	if relEnd < 0 {
		return html
	}
	end := start + len(seoRouteMarkerStart) + relEnd + len(seoRouteMarkerEnd)
	result := make([]byte, 0, len(html)-(end-start))
	result = append(result, html[:start]...)
	result = append(result, html[end:]...)
	return result
}

func injectRouteSEOHead(html []byte, title, description, canonical string) []byte {
	html = removeSEORouteTags(html)
	html = replaceHTMLTitle(html, title)
	tags := []byte(seoRouteMarkerStart +
		"<meta name=\"description\" content=\"" + htmlpkg.EscapeString(description) + "\">" +
		"<meta name=\"robots\" content=\"index,follow\">" +
		"<link rel=\"canonical\" href=\"" + htmlpkg.EscapeString(canonical) + "\">" +
		"<meta property=\"og:title\" content=\"" + htmlpkg.EscapeString(title) + "\">" +
		"<meta property=\"og:description\" content=\"" + htmlpkg.EscapeString(description) + "\">" +
		"<meta property=\"og:url\" content=\"" + htmlpkg.EscapeString(canonical) + "\">" +
		seoRouteMarkerEnd)
	close := bytes.Index(html, []byte("</head>"))
	if close < 0 {
		return html
	}
	result := make([]byte, 0, len(html)+len(tags))
	result = append(result, html[:close]...)
	result = append(result, tags...)
	result = append(result, html[close:]...)
	return result
}

func injectNoindexMeta(html []byte) []byte {
	html = removeSEORouteTags(html)
	tags := []byte(seoRouteMarkerStart + `<meta name="robots" content="noindex,nofollow">` + seoRouteMarkerEnd)
	close := bytes.Index(html, []byte("</head>"))
	if close < 0 {
		return html
	}
	result := make([]byte, 0, len(html)+len(tags))
	result = append(result, html[:close]...)
	result = append(result, tags...)
	result = append(result, html[close:]...)
	return result
}

func injectPublicSEOIntro(html []byte) []byte {
	intro := strings.TrimSpace(publicSEOIntro)
	if intro == "" {
		return html
	}
	if bytes.Contains(html, []byte(`id="modelport-public-intro"`)) {
		return html
	}
	// Keep the rich editorial copy below the interactive app.  It is visible
	// HTML (not noscript/UA-specific), but it must not displace the homepage
	// product UI above the fold.
	closeBody := bytes.Index(html, []byte("</body>"))
	if closeBody < 0 {
		return html
	}
	block := []byte("<!-- sub2api-public-seo-intro -->" + string(template.HTML(intro)))
	result := make([]byte, 0, len(html)+len(block))
	result = append(result, html[:closeBody]...)
	result = append(result, block...)
	result = append(result, html[closeBody:]...)
	return result
}

func injectTrustedHomeSEO(html []byte, settings publicSEOSettings) []byte {
	_ = settings // trust was established before this function is called
	title := modelPortHomeTitle
	description := modelPortHomeDescription
	html = injectRouteSEOHead(html, title, description, canonicalSEOURL("/"))
	if strings.TrimSpace(settings.HomeContent) == "" {
		html = injectPublicSEOIntro(html)
	}
	return html
}

func isHomeSEOPath(path string) bool {
	path = normalizeSEOPath(path)
	return path == "/" || path == "/home"
}

// The router's catch-all is intentionally not included here.  Keeping this
// list explicit means a typo/garbage URL gets a real 404 rather than an index
// shell, while all current frontend routes continue to receive the SPA.
var knownSPARouteExact = map[string]struct{}{
	"/": {}, "/home": {}, "/login": {}, "/register": {}, "/email-verify": {},
	"/pool-orders/pricing": {}, "/pool-orders": {}, "/admin/pool-orders": {}, "/admin/pool-resources": {},
	"/auth/callback": {}, "/auth/oauth/callback": {}, "/auth/linuxdo/callback": {},
	"/auth/wechat/callback": {}, "/auth/wechat/payment/callback": {},
	"/auth/dingtalk/callback": {}, "/auth/dingtalk/email-completion": {},
	"/auth/oidc/callback": {}, "/forgot-password": {}, "/reset-password": {},
	"/key-usage": {}, "/model-plaza": {}, "/dashboard": {}, "/keys": {},
	"/batch-image": {}, "/docs/batch-image": {}, "/usage": {}, "/guide": {},
	"/redeem": {}, "/lottery": {}, "/affiliate": {}, "/available-channels": {},
	"/profile": {}, "/subscriptions": {}, "/purchase": {}, "/orders": {},
	"/payment/qrcode": {}, "/payment/result": {}, "/payment/stripe": {},
	"/payment/airwallex": {}, "/payment/stripe-popup": {}, "/admin": {},
	"/admin/dashboard": {}, "/admin/ops": {}, "/admin/audit-logs": {},
	"/admin/users": {}, "/admin/groups": {}, "/admin/channels": {},
	"/admin/channels/pricing": {}, "/admin/channels/monitor": {}, "/monitor": {},
	"/admin/subscriptions": {}, "/admin/accounts": {}, "/admin/plugins": {},
	"/admin/announcements": {}, "/admin/proxies": {}, "/admin/redeem": {},
	"/admin/promo-codes": {}, "/admin/settings": {}, "/admin/risk-control": {},
	"/admin/prompt-audit": {}, "/admin/usage": {}, "/admin/affiliates": {},
	"/admin/affiliates/invites": {}, "/admin/affiliates/rebates": {},
	"/admin/affiliates/transfers": {}, "/admin/orders/dashboard": {},
	"/admin/orders": {}, "/admin/orders/plans": {},
}

func isOneSegmentRoute(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	segment := strings.TrimPrefix(path, prefix)
	if segment == "" || strings.Contains(segment, "/") {
		return false
	}
	if decoded, err := url.PathUnescape(segment); err != nil || strings.Contains(decoded, "/") {
		return false
	}
	return true
}

func isKnownSPARoute(path string) bool {
	path = normalizeSEOPath(path)
	if _, ok := knownSPARouteExact[path]; ok {
		return true
	}
	return isOneSegmentRoute(path, "/legal/") ||
		isOneSegmentRoute(path, "/guide/") ||
		isOneSegmentRoute(path, "/custom/")
}

func isPrivateSPARoute(path string) bool {
	path = normalizeSEOPath(path)
	return isKnownSPARoute(path) && path != "/" && path != "/home"
}

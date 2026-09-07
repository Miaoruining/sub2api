package handler

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const maxAutoGroupAttempts = 8

// AutoGroupMiddleware 在已有组内重试之外增加跨组边界。
// 每组重新走完整鉴权/订阅/限流/计费/协议处理链；不复用上一组的 Gin 上下文。
func (h *GatewayHandler) AutoGroupMiddleware(engine http.Handler, keys *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || key == nil || !key.AutoGroup || service.AutoGroupAttemptFromContext(c.Request.Context()) != nil {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/v1/usage" || path == "/v1/sub2api/billing" {
			c.Next()
			return
		}
		c.Abort()
		groups, err := keys.GetAvailableGroups(c.Request.Context(), key.UserID)
		if err != nil {
			autoGroupError(c, 503, "auto_group_unavailable", "无法读取自动分组权限")
			return
		}
		catalog, err := h.gatewayService.AutoGroupCatalog(c.Request.Context(), groups)
		if err != nil {
			autoGroupError(c, 503, "auto_group_unavailable", "无法读取自动分组模型")
			return
		}
		if c.Request.Method == http.MethodGet && strings.HasSuffix(path, "/models") {
			h.writeAutoGroupModels(c, catalog)
			return
		}
		// 长连接和异步任务需要跨请求的分组绑定，不能当成可重放的普通请求。
		if c.Request.Method != http.MethodPost || !autoGroupSynchronousPath(path) {
			autoGroupError(c, 400, "auto_group_endpoint_unsupported", "此端点暂不支持自动分组，请使用指定分组密钥")
			return
		}
		body, err := io.ReadAll(c.Request.Body) // 外层 RequestBodyLimit 已限制大小
		if err != nil {
			autoGroupError(c, 413, "request_body_error", "无法读取请求体或请求体超过大小限制")
			return
		}
		if !gjson.ValidBytes(body) {
			autoGroupError(c, 400, "invalid_request_error", "自动分组需要有效的 JSON 请求体")
			return
		}
		if gjson.GetBytes(body, "background").Bool() {
			autoGroupError(c, 400, "auto_group_endpoint_unsupported", "后台异步请求需要指定分组密钥")
			return
		}
		model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
		if strings.HasPrefix(path, "/v1beta/models/") {
			model = strings.SplitN(strings.TrimPrefix(path, "/v1beta/models/"), ":", 2)[0]
		}
		if model == "" {
			autoGroupError(c, 400, "invalid_request_error", "model is required")
			return
		}
		candidates, err := service.AutoGroupCandidates(catalog, model)
		if err != nil {
			autoGroupError(c, 400, "ambiguous_model", err.Error())
			return
		}
		if len(candidates) == 0 {
			autoGroupError(c, 404, "model_not_found", "此密钥没有支持该模型的已启用分组")
			return
		}
		candidates = h.gatewayService.RankAutoGroupCandidates(c.Request.Context(), candidates, model, key.UserID, key.RoutingStrategy)
		for i, group := range candidates {
			c.Set("auto_group_dispatched", true)
			if c.Request.Context().Err() != nil {
				return
			}
			attempt := &service.AutoGroupAttempt{APIKeyID: key.ID, GroupID: group.ID, Platform: group.Platform}
			request := c.Request.Clone(service.WithAutoGroupAttempt(c.Request.Context(), attempt))
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
			writer := newAutoGroupWriter(c.Writer)
			writer.Header().Set("X-Sub2API-Group-ID", strconv.FormatInt(group.ID, 10))
			writer.Header().Set("X-Sub2API-Routing-Strategy", service.NormalizeRoutingStrategy(key.RoutingStrategy))
			started := time.Now()
			engine.ServeHTTP(writer, request)
			// 参数/余额/用户限额错误及用户主动取消不污染线路健康。
			isStream := strings.Contains(writer.Header().Get("Content-Type"), "text/event-stream")
			failed := writer.status >= 500 || (writer.status >= 400 && attempt.Retryable()) || writer.streamFailed || (isStream && !writer.streamComplete && !writer.streamUnobservable && writer.status >= 200 && writer.status < 300 && group.Platform != service.PlatformGemini)
			if c.Request.Context().Err() == nil && !writer.downstreamFailed && (failed || (writer.status >= 200 && writer.status < 300 && (!isStream || writer.streamComplete))) && !strings.Contains(path, "count") && !strings.Contains(path, "input_tokens") {
				latency := time.Since(started)
				if !writer.firstByte.IsZero() {
					latency = writer.firstByte.Sub(started)
				}
				h.gatewayService.ObserveAutoRoute(group.ID, model, failed, latency)
			}
			if !writer.committed && writer.status >= 400 && attempt.Retryable() && i+1 < len(candidates) && i+1 < maxAutoGroupAttempts && c.Request.Context().Err() == nil {
				continue
			}
			writer.commit()
			return
		}
	}
}

func autoGroupSynchronousPath(path string) bool {
	if strings.HasPrefix(path, "/v1beta/models/") {
		return strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent") || strings.HasSuffix(path, ":countTokens")
	}
	path = strings.TrimPrefix(path, "/backend-api/codex")
	path = strings.TrimPrefix(path, "/v1")
	switch path {
	case "/messages", "/messages/count_tokens", "/chat/completions", "/responses", "/responses/compact", "/responses/input_tokens", "/embeddings", "/images/generations":
		return true
	}
	return false
}

func autoGroupError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}

func (h *GatewayHandler) writeAutoGroupModels(c *gin.Context, catalog []service.AutoGroupModels) {
	seen := map[string]string{}
	for _, entry := range catalog {
		for _, model := range entry.Models {
			if prior, ok := seen[model]; ok && prior != entry.Group.Platform {
				seen[model] = ""
			} else if !ok {
				seen[model] = entry.Group.Platform
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id, platform := range seen {
		if platform != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if c.Query("client_version") != "" || strings.HasPrefix(c.Request.URL.Path, "/backend-api/codex/") {
		codexIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			if seen[id] == service.PlatformOpenAI {
				codexIDs = append(codexIDs, id)
			}
		}
		body, err := h.gatewayService.BuildAutoGroupCodexManifest(c.Request.Context(), catalog, codexIDs)
		if err != nil {
			autoGroupError(c, 500, "manifest_error", "无法生成模型清单")
			return
		}
		c.Data(200, "application/json", body)
		return
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1beta/") {
		models := make([]gin.H, 0)
		for _, id := range ids {
			if seen[id] == service.PlatformGemini {
				models = append(models, gin.H{"name": "models/" + id, "displayName": id})
			}
		}
		c.JSON(200, gin.H{"models": models})
		return
	}
	models := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		models = append(models, gin.H{"id": id, "object": "model", "created": 0, "owned_by": seen[id]})
	}
	c.JSON(200, gin.H{"object": "list", "data": models})
}

// 只缓冲错误响应，成功响应及 SSE 立即透传。显式 Flush/Hijack 或超过
// 64 KiB 后不可切组，不缓存成功流、不拼接两次响应，也不重试普通 4xx/5xx。
type autoGroupWriter struct {
	target             http.ResponseWriter
	header             http.Header
	status             int
	body               bytes.Buffer
	committed          bool
	firstByte          time.Time
	streamLine         []byte
	streamLineOverflow bool
	streamComplete     bool
	streamFailed       bool
	streamUnobservable bool
	downstreamFailed   bool
}

func newAutoGroupWriter(target http.ResponseWriter) *autoGroupWriter {
	return &autoGroupWriter{target: target, header: target.Header().Clone()}
}
func (w *autoGroupWriter) Header() http.Header { return w.header }
func (w *autoGroupWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status < 400 {
		w.commit()
	}
}
func (w *autoGroupWriter) Write(p []byte) (int, error) {
	if len(p) > 0 && w.firstByte.IsZero() {
		w.firstByte = time.Now()
	}
	if strings.Contains(w.header.Get("Content-Type"), "text/event-stream") {
		w.observeStream(p)
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if !w.committed && w.body.Len()+len(p) > 64<<10 {
		w.commit()
	}
	if w.committed {
		n, err := w.target.Write(p)
		if err != nil {
			w.downstreamFailed = true
		}
		return n, err
	}
	return w.body.Write(p)
}

// 旁路观察终止事件，不缓冲/改写输出；跨 Write 的残留行最多 64 KiB。
// 仅解析协议顶层 type，不把模型正文中的 response.completed 字样当成功。
func (w *autoGroupWriter) observeStream(p []byte) {
	for _, b := range p {
		if b != '\n' {
			if !w.streamLineOverflow {
				if len(w.streamLine) < 64<<10 {
					w.streamLine = append(w.streamLine, b)
				} else {
					w.streamLine = nil
					w.streamLineOverflow = true
					w.streamUnobservable = true
				}
			}
			continue
		}
		if !w.streamLineOverflow {
			line := strings.TrimSpace(string(w.streamLine))
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				kind := gjson.Get(data, "type").String()
				if data == "[DONE]" || kind == "message_stop" || kind == "response.completed" {
					w.streamComplete = true
				}
				if kind == "error" || kind == "response.failed" || kind == "response.incomplete" || gjson.Get(data, "error").Exists() {
					w.streamFailed = true
				}
			}
		}
		w.streamLine = w.streamLine[:0]
		w.streamLineOverflow = false
	}
}
func (w *autoGroupWriter) commit() {
	if w.committed {
		return
	}
	w.committed = true
	for k := range w.target.Header() {
		w.target.Header().Del(k)
	}
	for k, values := range w.header {
		w.target.Header()[k] = append([]string(nil), values...)
	}
	if w.status == 0 {
		w.status = 200
	}
	w.target.WriteHeader(w.status)
	_, _ = w.body.WriteTo(w.target)
}
func (w *autoGroupWriter) Flush() {
	w.commit()
	if f, ok := w.target.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *autoGroupWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.committed = true
	if h, ok := w.target.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack unsupported")
}
func (w *autoGroupWriter) Unwrap() http.ResponseWriter { return w.target }

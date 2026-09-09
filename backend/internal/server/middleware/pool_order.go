package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// PoolQuota 对全部协议入口执行。拼单只开放可预占上界的无状态 OpenAI 文本 HTTP 请求。
func PoolQuota(repo service.PoolRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		if repo == nil {
			c.Next()
			return
		}
		key, ok := GetAPIKeyFromContext(c)
		if !ok || key == nil {
			c.Next()
			return
		}
		var gid int64
		if key.GroupID != nil {
			gid = *key.GroupID
		}
		gate, err := repo.Gate(c.Request.Context(), key.ID, key.UserID, gid)
		if err != nil {
			poolAbort(c, err)
			return
		}
		if gate == nil {
			c.Next()
			return
		}
		path := strings.TrimRight(c.Request.URL.Path, "/")
		if c.Request.Method == http.MethodGet && (strings.HasSuffix(path, "/models") || path == "/v1/usage" || path == "/v1/sub2api/billing") {
			c.Next()
			return
		}
		if c.Request.Method != http.MethodPost || !(strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/messages")) || strings.Contains(path, "antigravity") {
			AbortWithError(c, 400, "POOL_ENDPOINT", "拼单 Key 支持 HTTP 文本 Responses、Chat Completions 和 Messages；请关闭 WebSocket/实时或多媒体模式")
			return
		}
		if _, ok := GetSubscriptionFromContext(c); !ok {
			AbortWithError(c, 403, "POOL_SUBSCRIPTION_REQUIRED", "拼单需要标准模式和有效订阅")
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1024*1024+1))
		if err != nil || len(body) > 1024*1024 {
			AbortWithError(c, 400, "POOL_BODY", "文本请求不能超过 1 MiB")
			return
		}
		rewritten, reserved, err := preparePoolText(body, path, gate.TokensRemaining)
		if err != nil {
			if errors.Is(err, service.ErrPoolLimit) {
				poolAbort(c, service.ErrPoolLimit)
			} else {
				AbortWithError(c, 400, "POOL_TEXT_BUDGET", err.Error())
			}
			return
		}
		id, err := repo.Reserve(c.Request.Context(), gate.MemberID, reserved)
		if err != nil {
			poolAbort(c, err)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
		c.Request.ContentLength = int64(len(rewritten))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), service.PoolReservationContextKey{}, id))
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = repo.Finish(ctx, id, c.Writer.Status() < 400)
		}()
		c.Next()
	}
}
func poolAbort(c *gin.Context, err error) {
	switch err {
	case service.ErrPoolLimit:
		AbortWithError(c, 429, "POOL_LIMIT", err.Error())
	case service.ErrPoolAccess:
		AbortWithError(c, 403, "POOL_ACCESS", err.Error())
	default:
		AbortWithError(c, 503, "POOL_UNAVAILABLE", "拼单额度检查暂不可用，请稍后重试")
	}
}

func preparePoolText(body []byte, path string, remaining int64) ([]byte, int64, error) {
	invalid := func() ([]byte, int64, error) {
		return nil, 0, errors.New("拼单仅支持无状态纯文本请求；请发送完整历史，关闭存储上下文、联网工具和多媒体输入，并设置有效的输出 Token 上限")
	}
	var req map[string]json.RawMessage
	if json.Unmarshal(body, &req) != nil || req == nil {
		return invalid()
	}
	for _, name := range []string{"previous_response_id", "conversation", "background", "include", "truncation"} {
		if v, ok := req[name]; ok && string(v) != "null" && string(v) != "false" {
			return invalid()
		}
	}
	if raw, ok := req["tools"]; ok {
		var ts []map[string]any
		if json.Unmarshal(raw, &ts) != nil {
			return invalid()
		}
		for _, t := range ts {
			if typ, ok := t["type"]; ok && typ != "function" {
				return invalid()
			}
		}
	}
	// 使用 JSON 结构检查多媒体和服务端工具，避免将文本里的单词误判成请求参数。
	var tree any
	if json.Unmarshal(body, &tree) != nil || !poolTextOnly(tree) {
		return invalid()
	}
	if v, ok := req["n"]; ok && string(v) != "1" {
		return invalid()
	}
	// UTF-8 字节数按 1 byte/token 预占，另留协议包装开销；不读取客户端自报 usage。
	input := int64(len(body)) + 4096
	output := int64(4096)
	field := "max_tokens"
	if strings.HasSuffix(path, "/responses") {
		field = "max_output_tokens"
	} else if strings.HasSuffix(path, "/chat/completions") {
		field = "max_completion_tokens"
	}
	for _, name := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if v, ok := req[name]; ok {
			var n int64
			if json.Unmarshal(v, &n) != nil || n < 1 || n > 1000000 {
				return invalid()
			}
			output = n
			delete(req, name)
		}
	}
	if input+output > remaining {
		return nil, 0, fmt.Errorf("%w: 本次输入与最大输出的预占量超过个人剩余额度，请缩短历史或降低最大输出 Token", service.ErrPoolLimit)
	}
	req[field] = json.RawMessage([]byte(strconv.FormatInt(output, 10)))
	if strings.HasSuffix(path, "/responses") {
		req["store"] = json.RawMessage("false")
	}
	out, err := json.Marshal(req)
	return out, input + output, err
}
func poolTextOnly(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "type" {
				if s, ok := v.(string); ok {
					switch s {
					case "input_image", "image", "image_url", "input_audio", "audio", "input_file", "file", "video", "item_reference", "web_search", "web_search_preview", "computer_use_preview", "file_search", "code_interpreter", "mcp", "image_generation":
						return false
					}
				}
			}
			switch k {
			case "image_url", "image", "file_id", "file_data", "audio", "video", "encrypted_content":
				return false
			}
			if !poolTextOnly(v) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !poolTextOnly(v) {
				return false
			}
		}
	}
	return true
}

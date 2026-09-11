package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiResponsesCompatibilityTextAndToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "json"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			upstream := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read_file","args":{"path":"main.go"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":4}}`
			contentType := "application/json"
			if stream {
				upstream = "data: " + upstream + "\n\n"
				contentType = "text/event-stream"
			}
			stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{{status: 200, header: http.Header{"Content-Type": []string{contentType}}, body: upstream}}}
			svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := `{"model":"gemini-3.8-flash","input":"Read main.go","tools":[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}`
			if stream {
				body = strings.TrimSuffix(body, "}") + `,"stream":true}`
			}
			result, err := svc.ForwardAsResponses(context.Background(), c, geminiAnthropicIntegrationAccount(AccountTypeAPIKey), []byte(body))
			require.NoError(t, err)
			require.Equal(t, "gemini-3.8-flash", result.Model)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 4, result.Usage.OutputTokens)
			require.Len(t, stub.requests, 1)
			require.Contains(t, stub.requests[0].URL.Path, "gemini-3.8-flash:")
			payload, err := io.ReadAll(stub.requests[0].Body)
			require.NoError(t, err)
			require.Contains(t, string(payload), "functionDeclarations")
			require.Contains(t, string(payload), "read_file")
			if stream {
				require.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
				require.Contains(t, w.Body.String(), "event: response.created")
				require.Contains(t, w.Body.String(), "event: response.completed")
				require.Contains(t, w.Body.String(), `"type":"function_call"`)
			} else {
				require.Contains(t, w.Header().Get("Content-Type"), "application/json")
				require.Equal(t, "response", gjson.Get(w.Body.String(), "object").String())
				require.Equal(t, "read_file", gjson.Get(w.Body.String(), "output.0.name").String())
				require.Equal(t, "main.go", gjson.Get(gjson.Get(w.Body.String(), "output.0.arguments").String(), "path").String())
			}
		})
	}

	stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{{status: 200, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"text":"File reviewed"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":2}}`}}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.ForwardAsResponses(context.Background(), c, geminiAnthropicIntegrationAccount(AccountTypeAPIKey), []byte(`{"model":"gemini-3.8-flash","input":[{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"main.go\"}"},{"type":"function_call_output","call_id":"call_1","output":"package main"}]}`))
	require.NoError(t, err)
	require.Equal(t, "File reviewed", gjson.Get(w.Body.String(), "output.0.content.0.text").String())
	body, err := io.ReadAll(stub.requests[0].Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "functionResponse")
	require.Contains(t, string(body), "package main")
}

func TestGeminiResponsesCompatibilityUpstreamErrorDoesNotComplete(t *testing.T) {
	for _, stream := range []bool{false, true} {
		stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{{status: 400, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`}}}
		svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		body := `{"model":"gemini-3.8-flash","input":"ping","stream":false}`
		if stream {
			body = strings.Replace(body, "false", "true", 1)
		}
		result, err := svc.ForwardAsResponses(context.Background(), c, geminiAnthropicIntegrationAccount(AccountTypeAPIKey), []byte(body))
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 400, w.Code)
		require.True(t, gjson.Get(w.Body.String(), "error").Exists())
		require.NotContains(t, w.Body.String(), "response.completed")
		require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type queuedGeminiResponse struct {
	status int
	header http.Header
	body   string
}

type queuedGeminiHTTPStub struct {
	responses []queuedGeminiResponse
	requests  []*http.Request
}

func (s *queuedGeminiHTTPStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if len(s.responses) == 0 {
		return nil, errors.New("unexpected upstream request")
	}
	next := s.responses[0]
	s.responses = s.responses[1:]
	return &http.Response{
		StatusCode: next.status,
		Header:     next.header.Clone(),
		Body:       io.NopCloser(strings.NewReader(next.body)),
	}, nil
}

func (s *queuedGeminiHTTPStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

type staticGeminiTokenCache struct{ token string }

func (s staticGeminiTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return s.token, nil
}
func (s staticGeminiTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}
func (s staticGeminiTokenCache) DeleteAccessToken(context.Context, string) error { return nil }
func (s staticGeminiTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (s staticGeminiTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

func geminiAnthropicIntegrationAccount(accountType string) *Account {
	credentials := map[string]any{"api_key": "test-api-key", "access_token": "ya29.test"}
	if accountType == AccountTypeServiceAccount {
		credentials = map[string]any{"service_account_json": map[string]any{
			"project_id": "vertex-project", "private_key_id": "kid-1",
			"private_key":  "-----BEGIN PRIVATE KEY-----\ntest-only\n-----END PRIVATE KEY-----\n",
			"client_email": "fixture@vertex-project.iam.gserviceaccount.com",
		}}
	}
	return &Account{ID: 10, Platform: PlatformGemini, Type: accountType, Concurrency: 1, Credentials: credentials}
}

func TestGeminiAnthropicCompatibility(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeServiceAccount} {
		t.Run(accountType, func(t *testing.T) {
			stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"text":"pong"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`},
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"text/event-stream"}}, body: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pong\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":1}}\n\n"},
				{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"totalTokens":17}`},
			}}
			tokenProvider := NewGeminiTokenProvider(nil, staticGeminiTokenCache{token: "static-token"}, nil)
			svc := &GeminiMessagesCompatService{httpUpstream: stub, tokenProvider: tokenProvider, cfg: &config.Config{}}
			account := geminiAnthropicIntegrationAccount(accountType)

			messageRec := httptest.NewRecorder()
			messageContext, _ := gin.CreateTestContext(messageRec)
			messageContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			messageResult, err := svc.ForwardAnthropic(context.Background(), messageContext, account,
				[]byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"ping"}]}`),
				"claude-sonnet-4-6", "gemini-2.5-pro")
			require.NoError(t, err)
			require.Equal(t, "claude-sonnet-4-6", gjson.Get(messageRec.Body.String(), "model").String())
			require.Equal(t, "gemini-2.5-pro", messageResult.UpstreamModel)

			streamRec := httptest.NewRecorder()
			streamContext, _ := gin.CreateTestContext(streamRec)
			streamContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			_, err = svc.ForwardAnthropic(context.Background(), streamContext, account,
				[]byte(`{"model":"claude-haiku-4-5","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"ping"}]}`),
				"claude-haiku-4-5", "gemini-2.5-flash")
			require.NoError(t, err)
			events := streamRec.Body.String()
			previous := -1
			for _, event := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: content_block_stop", "event: message_delta", "event: message_stop"} {
				index := strings.Index(events, event)
				require.Greater(t, index, previous, event)
				previous = index
			}

			countRec := httptest.NewRecorder()
			countContext, _ := gin.CreateTestContext(countRec)
			countContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
			_, err = svc.CountAnthropicTokens(context.Background(), countContext, account,
				"claude-sonnet-4-6", "gemini-2.5-pro",
				[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"ping"}]}`))
			require.NoError(t, err)
			require.Equal(t, int64(17), gjson.Get(countRec.Body.String(), "input_tokens").Int())
		})
	}
}

func TestGeminiAnthropicCompatibility_ToolRoundTrip(t *testing.T) {
	stub := &queuedGeminiHTTPStub{responses: []queuedGeminiResponse{
		{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"weather"}}}]} ,"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`},
		{status: http.StatusOK, header: http.Header{"Content-Type": []string{"application/json"}}, body: `{"candidates":[{"content":{"parts":[{"text":"sunny"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2}}`},
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	account := geminiAnthropicIntegrationAccount(AccountTypeAPIKey)

	firstRec := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(firstRec)
	firstContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := svc.ForwardAnthropic(context.Background(), firstContext, account,
		[]byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"weather"}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`),
		"claude-sonnet-4-6", "gemini-2.5-pro")
	require.NoError(t, err)
	toolID := gjson.Get(firstRec.Body.String(), "content.0.id").String()
	require.Equal(t, "tool_use", gjson.Get(firstRec.Body.String(), "content.0.type").String())
	require.NotEmpty(t, toolID)

	secondBody := fmt.Sprintf(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"lookup","input":{"q":"weather"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"sunny"}]}]}`, toolID, toolID)
	secondRec := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRec)
	secondContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err = svc.ForwardAnthropic(context.Background(), secondContext, account, []byte(secondBody),
		"claude-sonnet-4-6", "gemini-2.5-pro")
	require.NoError(t, err)
	require.Contains(t, secondRec.Body.String(), "sunny")
	sent, readErr := io.ReadAll(stub.requests[1].Body)
	require.NoError(t, readErr)
	require.Equal(t, "lookup", gjson.GetBytes(sent, "contents.1.parts.0.functionResponse.name").String())
}

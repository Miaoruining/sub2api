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

func newGeminiCountTokensTestContext(status int, responseBody string) (*GeminiMessagesCompatService, *Account, *gin.Context, *httptest.ResponseRecorder) {
	httpStub := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}}
	svc := &GeminiMessagesCompatService{
		tokenProvider: &GeminiTokenProvider{},
		httpUpstream:  httpStub,
		cfg:           &config.Config{},
	}
	account := &Account{
		ID:       1,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "ya29.test",
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	return svc, account, c, recorder
}

func TestCountAnthropicTokens_ConvertsGeminiTotalTokens(t *testing.T) {
	svc, account, c, recorder := newGeminiCountTokensTestContext(http.StatusOK, `{"totalTokens":123}`)

	result, err := svc.CountAnthropicTokens(
		context.Background(), c, account,
		"claude-sonnet-4-6", "gemini-2.5-pro",
		[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`),
	)

	require.NoError(t, err)
	require.Equal(t, int64(123), gjson.Get(recorder.Body.String(), "input_tokens").Int())
	require.Empty(t, recorder.Header().Get("x-modelport-token-count-estimated"))
	require.Equal(t, "gemini-2.5-pro", result.UpstreamModel)
	require.Equal(t, "claude-sonnet-4-6", result.Model)
}

func TestCountAnthropicTokens_OAuthScopeFallbackIsMarked(t *testing.T) {
	svc, account, c, recorder := newGeminiCountTokensTestContext(
		http.StatusForbidden,
		`{"error":{"status":"PERMISSION_DENIED","message":"insufficient authentication scopes"}}`,
	)

	_, err := svc.CountAnthropicTokens(
		context.Background(), c, account,
		"claude-haiku-4-5", "gemini-2.5-flash",
		[]byte(`{"model":"claude-haiku-4-5","system":"rules","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`),
	)

	require.NoError(t, err)
	require.Equal(t, "true", recorder.Header().Get("x-modelport-token-count-estimated"))
	require.Positive(t, gjson.Get(recorder.Body.String(), "input_tokens").Int())
}

func TestCountAnthropicTokens_ImageCannotUseLocalFallback(t *testing.T) {
	svc, account, c, recorder := newGeminiCountTokensTestContext(
		http.StatusForbidden,
		`{"error":{"status":"PERMISSION_DENIED","message":"insufficient authentication scopes"}}`,
	)

	_, err := svc.CountAnthropicTokens(
		context.Background(), c, account,
		"claude-sonnet-4-6", "gemini-2.5-pro",
		[]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`),
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.type").String())
}

func TestForwardNativeCountTokens_MarksEstimatedFallback(t *testing.T) {
	svc, account, c, recorder := newGeminiCountTokensTestContext(
		http.StatusForbidden,
		`{"error":{"status":"PERMISSION_DENIED","message":"insufficient authentication scopes"}}`,
	)

	result, err := svc.ForwardNative(
		context.Background(), c, account,
		"gemini-2.5-flash", "countTokens", false,
		[]byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "true", recorder.Header().Get("x-modelport-token-count-estimated"))
	require.Positive(t, gjson.Get(recorder.Body.String(), "totalTokens").Int())
}

func TestEstimateGeminiCountTokens_IncludesFunctionDeclarations(t *testing.T) {
	withoutTools := estimateGeminiCountTokens([]byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
	withTools := estimateGeminiCountTokens([]byte(`{"contents":[{"parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"lookup_weather","description":"Look up weather for a city","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}]}`))

	require.Greater(t, withTools, withoutTools)
}

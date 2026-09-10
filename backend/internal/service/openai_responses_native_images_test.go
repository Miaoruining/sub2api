//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A real, tiny PNG keeps edit-input validation meaningful. In particular,
// tests must not use arbitrary base64 text because the production bridge
// deliberately rejects non-image bytes before constructing multipart edits.
var nativeImagesTestPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
)

func nativeImagesTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func nativeImagesTestAccount() *Account {
	account := rawChatCompletionsTestAccount()
	account.Credentials["base_url"] = "http://upstream.example"
	return account
}

func nativeImagesTestJSONResponse() *http.Response {
	encoded := base64.StdEncoding.EncodeToString(nativeImagesTestPNG)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"native-images-upstream-request"},
		},
		Body: io.NopCloser(strings.NewReader(`{"created":1726000000,"data":[{"b64_json":"` + encoded + `","revised_prompt":"draw a cat"}]}`)),
	}
}

func TestForwardResponsesViaNativeImages_GenerationPreservesImageResultAndMetrics(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"draw a cat"}],"stream":false}`)
	c, recorder := nativeImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: nativeImagesTestJSONResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, openAIImagesGenerationsEndpoint, result.UpstreamEndpoint)
	require.Equal(t, "gpt-image-2", result.Model)
	require.Equal(t, "native-images-upstream-request", result.RequestID)
	require.NotEmpty(t, result.ResponseID)
	require.Equal(t, 1, result.ImageCount)
	require.False(t, result.Stream)
	require.Equal(t, http.StatusOK, recorder.Code)

	response := gjson.ParseBytes(recorder.Body.Bytes())
	require.Equal(t, "completed", response.Get("status").String())
	require.Equal(t, "image_generation_call", response.Get("output.0.type").String())
	require.Equal(t, base64.StdEncoding.EncodeToString(nativeImagesTestPNG), response.Get("output.0.result").String())
	require.Equal(t, "gpt-image-2", response.Get("model").String())

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, openAIImagesGenerationsEndpoint, upstream.lastReq.URL.Path)
	require.Equal(t, "gpt-image-2", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(upstream.lastBody, "prompt").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool(), "native upstream request must be non-streaming")
}

func TestForwardResponsesViaNativeImages_EditRetainsInputImageAsMultipart(t *testing.T) {
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(nativeImagesTestPNG)
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_image","image_url":"` + dataURL + `"},{"type":"input_text","text":"replace the background"}],"stream":false}`)
	c, recorder := nativeImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: nativeImagesTestJSONResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, openAIImagesEditsEndpoint, result.UpstreamEndpoint)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, openAIImagesEditsEndpoint, upstream.lastReq.URL.Path)

	mediaType, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
	var foundImage, foundPrompt, foundModel bool
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		partBytes, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		switch part.FormName() {
		case "image", "image[]":
			foundImage = true
			require.Equal(t, nativeImagesTestPNG, partBytes)
		case "prompt":
			foundPrompt = true
			require.Equal(t, "replace the background", string(partBytes))
		case "model":
			foundModel = true
			require.Equal(t, "gpt-image-2", string(partBytes))
		}
	}
	require.True(t, foundImage, "input_image must be retained as an image part")
	require.True(t, foundPrompt)
	require.True(t, foundModel)
}

func TestForwardResponsesViaNativeImages_StreamHasTerminalResponsesEvent(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"draw a cat"}],"stream":true}`)
	c, recorder := nativeImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: nativeImagesTestJSONResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", true)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	streamBody := recorder.Body.String()
	require.Contains(t, streamBody, "event: response.created\n")
	require.Contains(t, streamBody, "event: response.output_item.added\n")
	require.Contains(t, streamBody, "event: response.output_item.done\n")
	require.Contains(t, streamBody, "event: response.completed\n")
	require.Contains(t, streamBody, base64.StdEncoding.EncodeToString(nativeImagesTestPNG))
	require.NotContains(t, streamBody, "response.failed")
	require.True(t, strings.HasSuffix(streamBody, "\n\n"), "SSE must end on an event boundary")
	require.Equal(t, "text/event-stream", strings.TrimSpace(strings.Split(recorder.Header().Get("Content-Type"), ";")[0]))
}

func TestForwardResponsesViaNativeImages_UpstreamFailureLeavesResponseForFailover(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"draw a cat"}],"stream":false}`)
	c, recorder := nativeImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusNotFound,
		Header: http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"native-images-404"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"type":"model_not_found","message":"image model unavailable"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)

	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusNotFound, failoverErr.StatusCode)
	require.Equal(t, http.StatusOK, recorder.Code, "bridge must not commit an upstream failure before failover")
	require.Empty(t, recorder.Body.Bytes())
}

func TestForwardResponsesViaNativeImages_EmptyDataIsNotSuccess(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"draw a cat"}],"stream":false}`)
	c, recorder := nativeImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"created":1726000000,"data":[]}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.Bytes(), "zero images must not be reported as a successful Responses response")
}

func TestForwardResponsesViaNativeImages_UnsupportedFieldIsExplicitBadRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"draw a cat"}],"previous_response_id":"resp_old"}`)
	c, recorder := nativeImagesTestContext(t, body)
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}

	result, err := svc.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "previous_response_id")
}

func TestNativeImagesResponsesResultIsValidJSON(t *testing.T) {
	// Keep a small direct assertion around the fixture so future edits do not
	// accidentally turn it into a non-image payload while changing tests.
	var response map[string]any
	err := json.Unmarshal(nativeImagesTestJSONResponseBody(), &response)
	require.NoError(t, err)
	require.Equal(t, 1, len(response["data"].([]any)))
}

func nativeImagesTestJSONResponseBody() []byte {
	encoded := base64.StdEncoding.EncodeToString(nativeImagesTestPNG)
	return []byte(`{"created":1726000000,"data":[{"b64_json":"` + encoded + `"}]}`)
}

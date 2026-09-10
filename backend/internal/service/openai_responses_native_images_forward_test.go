//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardNativeImagesOptInAvoidsTextDriver(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"a blue circle"}]}],"tools":[{"type":"image_generation","model":"gpt-image-2","quality":"high"}]}`)
	c, rec := nativeImagesTestContext(t, body)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	a := nativeImagesTestAccount()
	a.Extra = map[string]any{"openai_responses_image_transport": "images", "openai_responses_supported": true}
	u := &httpUpstreamRecorder{resp: nativeImagesTestJSONResponse()}
	s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u}
	result, err := s.Forward(context.Background(), c, a, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/v1/images/generations", u.lastReq.URL.Path)
	require.Equal(t, "gpt-image-2", gjson.GetBytes(u.lastBody, "model").String())
	require.Equal(t, "high", gjson.GetBytes(u.lastBody, "quality").String())
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", result.BillingModel)
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestNativeImagesBridgeRejectsPrivateImageURL(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":[{"type":"input_text","text":"edit this"},{"type":"input_image","image_url":"http://127.0.0.1/private"}]}`)
	c, rec := nativeImagesTestContext(t, body)
	u := &httpUpstreamRecorder{resp: nativeImagesTestJSONResponse()}
	s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u}
	result, err := s.forwardResponsesViaNativeImages(context.Background(), c, nativeImagesTestAccount(), body, "gpt-image-2", false)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, u.lastReq)
}

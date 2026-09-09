package middleware

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPoolTextReservationRejectsBypasses(t *testing.T) {
	for _, body := range []string{`{"previous_response_id":"resp_old"}`, `{"background":true}`, `{"input":[{"type":"input_image","image_url":"https://example.test/a"}]}`, `{"tools":[{"type":"web_search"}]}`, `{"input":[{"type":"item_reference","id":"old"}]}`, `{"max_output_tokens":-1}`, `{"max_output_tokens":100000000000000000000}`, `{"n":2}`, `null`} {
		_, _, err := preparePoolText([]byte(body), "/v1/responses", 100000)
		require.Error(t, err, body)
	}
}
func TestPoolTextReservationUsesBytesAndEnforcesOutputLimit(t *testing.T) {
	body := []byte(`{"model":"gpt-test","input":"你好","max_output_tokens":100}`)
	out, reserved, err := preparePoolText(body, "/v1/responses", 50000)
	require.NoError(t, err)
	require.EqualValues(t, len(body)+4096+100, reserved)
	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	require.EqualValues(t, 100, req["max_output_tokens"])
	require.Equal(t, false, req["store"])
	_, _, err = preparePoolText(body, "/v1/responses", reserved-1)
	require.Error(t, err)
	// 自定义函数的 JSON Schema 不应被当成多媒体。
	_, _, err = preparePoolText([]byte(`{"input":"test","tools":[{"type":"function","name":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}`), "/responses", 50000)
	require.NoError(t, err)
}

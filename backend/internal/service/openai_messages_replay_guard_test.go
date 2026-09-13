package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestApplyAnthropicCompatFullReplayGuard_PreservesFullConversation(t *testing.T) {
	t.Parallel()

	req := &apicompat.AnthropicRequest{Messages: make([]apicompat.AnthropicMessage, 0, 15)}
	for i := 0; i < 15; i++ {
		req.Messages = append(req.Messages, apicompat.AnthropicMessage{
			Role:    "user",
			Content: json.RawMessage(`"message"`),
		})
	}
	req.Messages[0].Content = json.RawMessage(`"initial task context"`)
	before, err := json.Marshal(req.Messages)
	require.NoError(t, err)

	trimmed := applyAnthropicCompatFullReplayGuard(req)
	after, err := json.Marshal(req.Messages)
	require.NoError(t, err)

	require.False(t, trimmed)
	require.JSONEq(t, string(before), string(after))
	require.JSONEq(t, `"initial task context"`, string(req.Messages[0].Content))
	require.Len(t, req.Messages, 15)
}

func TestApplyAnthropicCompatFullReplayGuard_PreservesToolCallResultSequence(t *testing.T) {
	t.Parallel()

	req := &apicompat.AnthropicRequest{Messages: make([]apicompat.AnthropicMessage, 0, 15)}
	for i := 0; i < 15; i++ {
		role := "user"
		content := json.RawMessage(`"message"`)
		if i == 1 {
			role = "assistant"
			content = json.RawMessage(`[{"type":"tool_use","id":"toolu_keep","name":"Read","input":{"file_path":"main.go"}}]`)
		}
		if i == 3 {
			content = json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_keep","content":"ok"}]`)
		}
		req.Messages = append(req.Messages, apicompat.AnthropicMessage{
			Role:    role,
			Content: content,
		})
	}
	before, err := json.Marshal(req.Messages)
	require.NoError(t, err)

	trimmed := applyAnthropicCompatFullReplayGuard(req)
	after, err := json.Marshal(req.Messages)
	require.NoError(t, err)

	require.False(t, trimmed)
	require.JSONEq(t, string(before), string(after))
	require.Len(t, req.Messages, 15)
	require.Equal(t, "user", req.Messages[0].Role)
	require.Equal(t, "assistant", req.Messages[1].Role)
	require.Contains(t, string(req.Messages[1].Content), `"toolu_keep"`)
	require.Equal(t, "user", req.Messages[2].Role)
	require.Equal(t, "user", req.Messages[3].Role)
	require.Contains(t, string(req.Messages[3].Content), `"tool_result"`)
}

func TestApplyAnthropicCompatFullReplayGuard_NilAndShortHistoryUnchanged(t *testing.T) {
	t.Parallel()

	require.False(t, applyAnthropicCompatFullReplayGuard(nil))

	req := &apicompat.AnthropicRequest{Messages: []apicompat.AnthropicMessage{
		{Role: "user", Content: json.RawMessage(`"initial task"`)},
		{Role: "assistant", Content: json.RawMessage(`"answer"`)},
	}}
	before, err := json.Marshal(req.Messages)
	require.NoError(t, err)

	require.False(t, applyAnthropicCompatFullReplayGuard(req))
	after, err := json.Marshal(req.Messages)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

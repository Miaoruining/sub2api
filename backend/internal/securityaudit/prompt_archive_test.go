package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchivePersistsRequestContextWithoutPayloadOrGuard(t *testing.T) {
	repo := &fakeJobRepository{}
	// Archive has no dependency on the Prompt Guard config, Redis payload, or
	// scanner. A nil config/payload is therefore valid for this direct path.
	enqueuer := NewEnqueuer(nil, repo, nil, NewAtomicMetrics())

	err := enqueuer.Archive(context.Background(), Request{
		RequestID: "req-archive", UserID: 42, Provider: "openai", Endpoint: "/v1/chat/completions",
		Protocol: "openai_chat_completions", Model: "gpt-test",
		Body: []byte(`{"messages":[{"role":"user","content":"current request"},{"role":"assistant","content":"historical output"}]}`),
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.recordArchiveCalls)
	require.Equal(t, []RequestContextMessage{
		{Role: "user", Content: "current request"},
		{Role: "assistant", Content: "historical output"},
	}, repo.recordArchive.ContextPayload)
	require.Equal(t, 2, repo.recordArchive.MessageCount)
	require.NotEmpty(t, repo.recordArchive.ContentHash)
}

func TestExtractRequestContextArchivePreservesOrderedAllowedMessages(t *testing.T) {
	archive, err := ExtractRequestContextArchive(Request{
		RequestID: "ordered", Protocol: "anthropic_messages", Model: "claude-test", Stage: "http",
		Body: []byte(`{"system":"system context","messages":[` +
			`{"role":"developer","content":"developer context"},` +
			`{"role":"user","content":[{"type":"text","text":"user context"}]},` +
			`{"role":"assistant","content":"historical answer"},` +
			`{"role":"tool","content":"tool result"},` +
			`{"role":"model","content":"model history"},` +
			`{"role":"unknown","content":"must not be archived"}]}`),
	})

	require.NoError(t, err)
	require.Equal(t, []RequestContextMessage{
		{Role: "system", Content: "system context"},
		{Role: "developer", Content: "developer context"},
		{Role: "user", Content: "user context"},
		{Role: "assistant", Content: "historical answer"},
		{Role: "tool", Content: "tool result"},
		{Role: "model", Content: "model history"},
	}, archive.ContextPayload)
	require.Equal(t, archive.MessageCount, len(archive.ContextPayload))
	require.NotContains(t, string(mustArchiveJSON(t, archive.ContextPayload)), "must not be archived")
}

func TestExtractRequestContextArchiveDoesNotTruncateContent(t *testing.T) {
	longContent := strings.Repeat("长", 70000)
	archive, err := ExtractRequestContextArchive(Request{
		RequestID: "long", Protocol: "openai_chat_completions", Model: "gpt-test",
		Body: []byte(`{"messages":[{"role":"user","content":"` + longContent + `"}]}`),
	})

	require.NoError(t, err)
	require.Len(t, archive.ContextPayload, 1)
	require.Equal(t, longContent, archive.ContextPayload[0].Content)
}

func mustArchiveJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func TestArchiveSkipsKnownHealthCheck(t *testing.T) {
	repo := &fakeJobRepository{}
	config := &fakeConfigStore{active: true}
	enqueuer := NewEnqueuer(config, repo, nil)

	err := enqueuer.Archive(context.Background(), Request{
		Protocol: "anthropic_messages",
		Body:     []byte(`{"model":"claude-3-haiku","max_tokens":1,"messages":[{"role":"user","content":"probe"}]}`),
	})

	require.NoError(t, err)
	require.Zero(t, repo.recordArchiveCalls)
}

func TestArchiveWriteFailureIsReturnedToBestEffortCaller(t *testing.T) {
	repo := &fakeJobRepository{recordArchiveErr: errors.New("database unavailable")}
	config := &fakeConfigStore{active: true}
	enqueuer := NewEnqueuer(config, repo, nil)

	err := enqueuer.Archive(context.Background(), Request{
		Protocol: "openai_chat_completions",
		Body:     []byte(`{"messages":[{"role":"user","content":"request"}]}`),
	})

	require.Error(t, err)
	require.Equal(t, 1, repo.recordArchiveCalls)
}

func TestArchiveLogFieldsContainOnlyOperationalContext(t *testing.T) {
	groupID := int64(23)
	fields := archiveLogFields(Request{
		RequestID:  "archive-request",
		UserID:     42,
		Username:   "private-user",
		UserEmail:  "private@example.com",
		APIKeyID:   7,
		APIKeyName: "private-key",
		GroupID:    &groupID,
		Provider:   "openai",
		Endpoint:   "/v1/chat/completions",
		Protocol:   "openai_chat_completions",
		Model:      "gpt-test",
		Body:       []byte(`{"messages":[{"role":"user","content":"private prompt"}]}`),
		Stage:      "subsequent_turn",
	})

	require.Equal(t, map[string]any{
		"request_id": "archive-request",
		"protocol":   "openai_chat_completions",
		"model":      "gpt-test",
		"stage":      "subsequent_turn",
	}, fields)
}

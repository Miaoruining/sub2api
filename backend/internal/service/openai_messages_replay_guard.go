package service

import "github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"

// Retained for compatibility with existing service test fixtures. Full replay
// no longer uses a tail-message limit.
const openAICompatAnthropicReplayMaxTailMessages = 12

// applyAnthropicCompatFullReplayGuard is kept as the compatibility hook used
// by the Messages gateway when no upstream response history is available.
//
// In that mode the request is the only complete source of conversation state.
// A tail-only replay can silently remove the initial task (and, separately,
// split tool_use/tool_result pairs), so preserving the entire sequence is the
// only replay-safe behavior. The bool remains false because this function no
// longer mutates the request.
func applyAnthropicCompatFullReplayGuard(req *apicompat.AnthropicRequest) bool {
	return false
}

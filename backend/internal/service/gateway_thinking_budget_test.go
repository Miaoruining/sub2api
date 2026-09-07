package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestIsThinkingBudgetConstraintError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "minimum budget validation",
			message: "thinking.budget_tokens: Input should be greater than or equal to 1024",
			want:    true,
		},
		{
			name:    "opus 5 relationship wording",
			message: "Claude thinking budget_tokens must be less than max_tokens",
			want:    true,
		},
		{
			name:    "relationship wording without thinking prefix",
			message: "budget_tokens must be less than max_tokens",
			want:    true,
		},
		{
			name:    "reversed relationship wording",
			message: "max_tokens must be greater than thinking.budget_tokens",
			want:    true,
		},
		{
			name:    "unrelated max tokens error",
			message: "max_tokens must be greater than zero",
			want:    false,
		},
		{
			name:    "unrelated thinking error",
			message: "thinking signature is invalid",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isThinkingBudgetConstraintError(tt.message))
		})
	}
}

func TestNormalizeClaudeThinkingBudget(t *testing.T) {
	t.Run("rectifies budget not less than max tokens", func(t *testing.T) {
		body := []byte(`{"max_tokens":16000,"thinking":{"type":"enabled","budget_tokens":32000}}`)
		got, changed := NormalizeClaudeThinkingBudget(body)

		require.True(t, changed)
		assert.Equal(t, int64(BudgetRectifyBudgetTokens), gjson.GetBytes(got, "thinking.budget_tokens").Int())
		assert.Equal(t, int64(BudgetRectifyMaxTokens), gjson.GetBytes(got, "max_tokens").Int())
	})

	t.Run("rectifies missing enabled budget", func(t *testing.T) {
		body := []byte(`{"max_tokens":16000,"thinking":{"type":"enabled"}}`)
		got, changed := NormalizeClaudeThinkingBudget(body)

		require.True(t, changed)
		assert.Equal(t, int64(BudgetRectifyBudgetTokens), gjson.GetBytes(got, "thinking.budget_tokens").Int())
		assert.Equal(t, int64(BudgetRectifyMaxTokens), gjson.GetBytes(got, "max_tokens").Int())
	})

	t.Run("keeps a valid enabled budget", func(t *testing.T) {
		body := []byte(`{"max_tokens":20000,"thinking":{"type":"enabled","budget_tokens":10000}}`)
		got, changed := NormalizeClaudeThinkingBudget(body)

		assert.False(t, changed)
		assert.JSONEq(t, string(body), string(got))
	})

	t.Run("removes manual budget from adaptive thinking", func(t *testing.T) {
		body := []byte(`{"max_tokens":20000,"thinking":{"type":"adaptive","budget_tokens":10000}}`)
		got, changed := NormalizeClaudeThinkingBudget(body)

		require.True(t, changed)
		assert.Equal(t, "adaptive", gjson.GetBytes(got, "thinking.type").String())
		assert.False(t, gjson.GetBytes(got, "thinking.budget_tokens").Exists())
	})

	t.Run("leaves disabled thinking untouched", func(t *testing.T) {
		body := []byte(`{"max_tokens":20000,"thinking":{"type":"disabled","budget_tokens":10000}}`)
		got, changed := NormalizeClaudeThinkingBudget(body)

		assert.False(t, changed)
		assert.JSONEq(t, string(body), string(got))
	})
}

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIMessagesDispatchModelConfig(t *testing.T) {
	t.Parallel()

	cfg := normalizeOpenAIMessagesDispatchModelConfig(OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   " gpt-5.4-high ",
		SonnetMappedModel: "gpt-5.3-codex",
		HaikuMappedModel:  " gpt-5.4-mini-medium ",
		ExactModelMappings: map[string]string{
			" claude-sonnet-4-5-20250929 ": " gpt-5.2-high ",
			"":                             "gpt-5.4",
			"claude-opus-4-6":              " ",
		},
	})

	require.Equal(t, "gpt-5.4", cfg.OpusMappedModel)
	require.Equal(t, "gpt-5.3-codex", cfg.SonnetMappedModel)
	require.Equal(t, "gpt-5.4-mini", cfg.HaikuMappedModel)
	require.Equal(t, map[string]string{
		"claude-sonnet-4-5-20250929": "gpt-5.2",
	}, cfg.ExactModelMappings)
}

func TestGroupResolveMessagesDispatchModel_GrokRequiresCrossClientMapping(t *testing.T) {
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })
	group := &Group{Platform: PlatformGrok}

	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{})
	require.Empty(t, group.ResolveMessagesDispatchModel("claude-sonnet-4-5"))

	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{
		DefaultText:          "grok-build-0.1",
		EnableCrossClientMap: true,
	})
	require.Equal(t, "grok-build-0.1", group.ResolveMessagesDispatchModel("claude-sonnet-4-5"))
	require.Equal(t, "grok-build-0.1", group.ResolveMessagesDispatchModel("claude-opus-4-6"))
	require.Equal(t, "grok-build-0.1", group.ResolveMessagesDispatchModel("claude-haiku-4-5"))
	require.Empty(t, group.ResolveMessagesDispatchModel("grok"))
	require.Empty(t, group.ResolveMessagesDispatchModel("gpt-5.3-codex"))
}

func TestSanitizeGroupMessagesDispatchFields_ClearsNonOpenAIPlatform(t *testing.T) {
	t.Parallel()

	group := &Group{
		Platform:              PlatformAnthropic,
		AllowMessagesDispatch: true,
		DefaultMappedModel:    "gpt-5.6-sol",
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			SonnetMappedModel: "gpt-5.3-codex",
			ExactModelMappings: map[string]string{
				"claude-fable-5": "gpt-5.6-sol",
			},
		},
	}

	sanitizeGroupMessagesDispatchFields(group)

	require.False(t, group.AllowMessagesDispatch)
	require.Empty(t, group.DefaultMappedModel)
	require.Equal(t, OpenAIMessagesDispatchModelConfig{}, group.MessagesDispatchModelConfig)
}

func TestSanitizeGroupMessagesDispatchFields_PreservesCompositeDispatchToggle(t *testing.T) {
	t.Parallel()

	group := &Group{
		Platform:              PlatformComposite,
		AllowMessagesDispatch: true,
		DefaultMappedModel:    "gpt-5.6-sol",
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			SonnetMappedModel: "gpt-5.3-codex",
			ExactModelMappings: map[string]string{
				"claude-fable-5": "gpt-5.6-sol",
			},
		},
	}

	sanitizeGroupMessagesDispatchFields(group)

	require.True(t, group.AllowMessagesDispatch)
	require.Empty(t, group.DefaultMappedModel)
	require.Equal(t, OpenAIMessagesDispatchModelConfig{}, group.MessagesDispatchModelConfig)
}

func TestResolveGeminiAnthropicModel(t *testing.T) {
	t.Parallel()

	group := &Group{
		Platform:              PlatformGemini,
		AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			OpusMappedModel:   "gemini-2.5-pro",
			SonnetMappedModel: "gemini-2.5-pro",
			HaikuMappedModel:  "gemini-2.5-flash",
			ExactModelMappings: map[string]string{
				"claude-sonnet-*":   "gemini-2.5-flash",
				"claude-sonnet-4-*": "gemini-2.5-pro",
				"claude-sonnet-4-6": "gemini-2.0-flash",
			},
		},
	}

	exact, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-6")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.0-flash", exact.TargetModel)
	require.Equal(t, "claude-sonnet-4-6", exact.MappingRule)

	longest, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-pro", longest.TargetModel)
	require.Equal(t, "claude-sonnet-4-*", longest.MappingRule)

	direct, err := ResolveGeminiAnthropicModel(group, "gemini-2.5-flash")
	require.NoError(t, err)
	require.False(t, direct.Mapped)
	require.Equal(t, "gemini-2.5-flash", direct.TargetModel)
}

func TestResolveGeminiAnthropicModel_DisabledAliasDoesNotAffectDirectGemini(t *testing.T) {
	t.Parallel()

	group := &Group{Platform: PlatformGemini, AllowMessagesDispatch: false}

	_, err := ResolveGeminiAnthropicModel(group, "claude-sonnet-4-6")
	require.ErrorIs(t, err, ErrMessagesDispatchModelNotFound)

	direct, err := ResolveGeminiAnthropicModel(group, "gemini-2.5-pro")
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-pro", direct.TargetModel)
}

func TestSanitizeGroupMessagesDispatchFields_PreservesGeminiConfig(t *testing.T) {
	t.Parallel()

	group := &Group{
		Platform:              PlatformGemini,
		AllowMessagesDispatch: true,
		DefaultMappedModel:    "must-be-cleared",
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			SonnetMappedModel: "gemini-2.5-pro",
		},
	}

	sanitizeGroupMessagesDispatchFields(group)

	require.True(t, group.AllowMessagesDispatch)
	require.Empty(t, group.DefaultMappedModel)
	require.Equal(t, "gemini-2.5-pro", group.MessagesDispatchModelConfig.SonnetMappedModel)
}

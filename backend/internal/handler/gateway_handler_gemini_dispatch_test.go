package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveGatewayMessagesDispatchModels_GeminiAlias(t *testing.T) {
	group := &service.Group{
		Platform:              service.PlatformGemini,
		AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{
			SonnetMappedModel: "gemini-2.5-pro",
		},
	}

	publicModel, dispatchModel, err := resolveGatewayMessagesDispatchModels(
		group,
		service.PlatformGemini,
		"claude-sonnet-4-6",
	)

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-6", publicModel)
	require.Equal(t, "gemini-2.5-pro", dispatchModel)
}

func TestResolveGatewayMessagesDispatchModels_NonGeminiPassesThrough(t *testing.T) {
	publicModel, dispatchModel, err := resolveGatewayMessagesDispatchModels(
		&service.Group{Platform: service.PlatformAnthropic},
		service.PlatformAnthropic,
		"claude-sonnet-4-6",
	)

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-6", publicModel)
	require.Equal(t, publicModel, dispatchModel)
}

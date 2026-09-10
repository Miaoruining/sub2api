package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestUseOpenAIHTTP1TransportIsExactAndAccountScoped(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{
			name: "enabled OpenAI API key",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
			},
			want: true,
		},
		{
			name: "missing value",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
			},
		},
		{
			name: "unknown value",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: "h1"},
			},
		},
		{
			name: "whitespace is not exact",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: "http1 "},
			},
		},
		{
			name: "non string is not enabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: true},
			},
		},
		{
			name: "OAuth is unchanged",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
			},
		},
		{
			name: "other platform is unchanged",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
			},
		},
		{name: "nil account", account: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, useOpenAIHTTP1Transport(tt.account))
		})
	}
}

func TestWithOpenAIAccountHTTPTransportOnlyReprofilesEnabledAccount(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	got := withOpenAIAccountHTTPTransport(request, &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
	})
	require.NotSame(t, request, got)
	require.Equal(t, HTTPUpstreamProfileOpenAIHTTP1, HTTPUpstreamProfileFromContext(got.Context()))

	unchanged := withOpenAIAccountHTTPTransport(request, &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
	})
	require.Same(t, request, unchanged)
}

type accountHTTPTransportProfileUpstream struct {
	request *http.Request
}

func (u *accountHTTPTransportProfileUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = request
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}

func (u *accountHTTPTransportProfileUpstream) DoWithTLS(request *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxyURL, accountID, concurrency)
}

func TestOpenAIGatewayHTTP1RolloutIsAppliedOnlyToEligibleAccount(t *testing.T) {
	upstream := &accountHTTPTransportProfileUpstream{}
	service := &OpenAIGatewayService{pluginManager: &PluginManager{}, httpUpstream: upstream}

	request, err := http.NewRequestWithContext(
		WithHTTPUpstreamProfile(context.Background(), HTTPUpstreamProfileOpenAI),
		http.MethodPost,
		"https://api.openai.com/v1/responses",
		nil,
	)
	require.NoError(t, err)
	response, err := service.doOpenAIUpstream(request, "", &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Extra:       map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
	})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, HTTPUpstreamProfileOpenAIHTTP1, HTTPUpstreamProfileFromContext(upstream.request.Context()))

	request, err = http.NewRequestWithContext(
		WithHTTPUpstreamProfile(context.Background(), HTTPUpstreamProfileOpenAI),
		http.MethodPost,
		"https://api.openai.com/v1/responses",
		nil,
	)
	require.NoError(t, err)
	response, err = service.doOpenAIUpstream(request, "", &Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
	})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.request.Context()))

	request, err = http.NewRequestWithContext(
		WithHTTPUpstreamProfile(context.Background(), HTTPUpstreamProfileOpenAI),
		http.MethodPost,
		"https://api.openai.com/v1/responses",
		nil,
	)
	require.NoError(t, err)
	response, err = service.doOpenAIUpstream(request, "", &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Extra:       map[string]any{openAIHTTPTransportExtraKey: openAIHTTPTransportHTTP1},
	})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.request.Context()))
}

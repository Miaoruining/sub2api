package service

import "testing"

func TestUsesNativeImagesForResponsesIsExplicitAndAPIKeyOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *Account
		want    bool
	}{
		{"nil", nil, false},
		{"default", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, false},
		{"opt-in", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_image_transport": "images"}}, true},
		{"oauth", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_responses_image_transport": "images"}}, false},
		{"other-platform", &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_image_transport": "images"}}, false},
		{"wrong-value", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_responses_image_transport": true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.account.UsesNativeImagesForResponses(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNativeImagesOptInDoesNotEnableTextResponses(t *testing.T) {
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{
		"openai_responses_image_transport": "images", "openai_responses_supported": false,
	}}
	if a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses) {
		t.Fatal("native image opt-in must not enable text Responses or compaction")
	}
}

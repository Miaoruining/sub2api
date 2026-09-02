package domain

// OpenAIMessagesDispatchModelConfig controls how Anthropic /v1/messages
// requests are mapped onto an OpenAI/Codex or Gemini group's upstream models.
//
// The persisted JSON field name is intentionally kept for backward compatibility.
type OpenAIMessagesDispatchModelConfig struct {
	OpusMappedModel    string            `json:"opus_mapped_model,omitempty"`
	SonnetMappedModel  string            `json:"sonnet_mapped_model,omitempty"`
	HaikuMappedModel   string            `json:"haiku_mapped_model,omitempty"`
	ExactModelMappings map[string]string `json:"exact_model_mappings,omitempty"`
}

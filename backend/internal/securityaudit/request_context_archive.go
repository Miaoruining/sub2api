package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// RequestContextMessage is the only content retained by request-context
// archival. It deliberately has no fields for request options, credentials,
// identities, or model output.
type RequestContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RequestContextArchive is the storage-ready representation of one request's
// client-supplied conversation context. The surrounding request metadata is
// stored as dedicated columns; ContextPayload contains messages only.
type RequestContextArchive struct {
	RequestID      string
	Protocol       string
	Model          string
	Stage          string
	ContextPayload []RequestContextMessage
	ContentHash    string
	MessageCount   int
}

// ExtractRequestContextArchive extracts textual messages in their original
// request order. Unlike PromptSnapshot, it does not prioritize/reorder,
// redact, or truncate content, and it never includes arbitrary request
// parameters.
func ExtractRequestContextArchive(req Request) (RequestContextArchive, error) {
	var document any
	if err := json.Unmarshal(req.Body, &document); err != nil {
		return RequestContextArchive{}, errors.New("request context archive JSON is invalid")
	}
	root, _ := document.(map[string]any)
	messages := extractRequestContextMessages(strings.ToLower(strings.TrimSpace(req.Protocol)), root)
	if len(messages) == 0 {
		return RequestContextArchive{}, ErrNoPromptText
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return RequestContextArchive{}, errors.New("request context archive payload is invalid")
	}
	digest := sha256.Sum256(encoded)
	return RequestContextArchive{
		RequestID:      req.RequestID,
		Protocol:       strings.TrimSpace(req.Protocol),
		Model:          strings.TrimSpace(req.Model),
		Stage:          normalizeStage(req.Stage),
		ContextPayload: messages,
		ContentHash:    hex.EncodeToString(digest[:]),
		MessageCount:   len(messages),
	}, nil
}

var requestContextArchiveRoles = map[string]struct{}{
	"system":    {},
	"developer": {},
	"user":      {},
	"assistant": {},
	"tool":      {},
	"model":     {},
}

func extractRequestContextMessages(protocol string, root map[string]any) []RequestContextMessage {
	if root == nil {
		return nil
	}
	result := make([]RequestContextMessage, 0, 8)
	appendValue := func(role string, value any) {
		result = appendRequestContextValue(result, role, value)
	}
	appendMessages := func(value any) {
		result = appendRequestContextMessages(result, value)
	}

	switch protocol {
	case "anthropic_messages", "claude_messages", "messages":
		appendValue("system", root["system"])
		appendMessages(root["messages"])
	case "gemini", "gemini_generate_content":
		result = appendRequestContextGeminiRoot(result, root)
	case "openai_responses", "responses", "responses_websocket":
		if response, ok := root["response"].(map[string]any); ok {
			appendValue("system", response["instructions"])
			result = appendRequestContextResponses(result, response["input"])
		} else {
			appendValue("system", root["instructions"])
			result = appendRequestContextResponses(result, root["input"])
		}
	case "openai_images", "grok_media", "media", "images":
		for _, prompt := range extractMediaPrompts(root) {
			result = appendRequestContextMessage(result, "user", prompt)
		}
	default:
		appendValue("system", root["system"])
		appendMessages(root["messages"])
		if len(result) == 0 {
			appendValue("system", root["instructions"])
			result = appendRequestContextResponses(result, root["input"])
		}
		if len(result) == 0 {
			result = appendRequestContextGeminiRoot(result, root)
		}
		if len(result) == 0 {
			for _, prompt := range extractMediaPrompts(root) {
				result = appendRequestContextMessage(result, "user", prompt)
			}
		}
	}
	return result
}

func appendRequestContextMessages(result []RequestContextMessage, value any) []RequestContextMessage {
	items, ok := value.([]any)
	if !ok {
		return result
	}
	for _, item := range items {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringValue(message["role"])))
		if _, allowed := requestContextArchiveRoles[role]; !allowed {
			continue
		}
		before := len(result)
		result = appendRequestContextValue(result, role, message["content"])
		if len(result) == before {
			// A text-less message is not context payload and may have been skipped.
			if text := requestContextText(message["text"]); text != "" {
				result = appendRequestContextMessage(result, role, text)
			}
		}
	}
	return result
}

func appendRequestContextResponses(result []RequestContextMessage, value any) []RequestContextMessage {
	switch typed := value.(type) {
	case string:
		return appendRequestContextMessage(result, "user", typed)
	case map[string]any:
		return appendRequestContextResponseItem(result, typed)
	case []any:
		for _, item := range typed {
			if message, ok := item.(map[string]any); ok {
				result = appendRequestContextResponseItem(result, message)
			} else if text := requestContextText(item); text != "" {
				result = appendRequestContextMessage(result, "user", text)
			}
		}
	}
	return result
}

func appendRequestContextResponseItem(result []RequestContextMessage, item map[string]any) []RequestContextMessage {
	role := strings.ToLower(strings.TrimSpace(stringValue(item["role"])))
	if role == "" {
		switch strings.ToLower(strings.TrimSpace(stringValue(item["type"]))) {
		case "function_call_output":
			role = "tool"
		default:
			role = "user"
		}
	}
	if _, allowed := requestContextArchiveRoles[role]; !allowed {
		return result
	}
	before := len(result)
	result = appendRequestContextValue(result, role, item["content"])
	if len(result) == before {
		value := item["text"]
		if role == "tool" {
			value = item["output"]
		}
		if text := requestContextText(value); text != "" {
			result = appendRequestContextMessage(result, role, text)
		} else {
			result = appendRequestContextValue(result, role, value)
		}
	}
	return result
}

func appendRequestContextGeminiRoot(result []RequestContextMessage, root map[string]any) []RequestContextMessage {
	result = appendRequestContextValue(result, "system", root["systemInstruction"])
	result = appendRequestContextValue(result, "system", root["system_instruction"])
	result = appendRequestContextGemini(result, root["contents"])
	result = appendRequestContextGemini(result, root["content"])
	result = appendRequestContextGeminiInstances(result, root["instances"])
	if requests, ok := root["requests"].([]any); ok {
		for _, item := range requests {
			request, ok := item.(map[string]any)
			if !ok {
				continue
			}
			result = appendRequestContextValue(result, "system", request["systemInstruction"])
			result = appendRequestContextValue(result, "system", request["system_instruction"])
			result = appendRequestContextGemini(result, request["contents"])
			result = appendRequestContextGemini(result, request["content"])
			result = appendRequestContextGeminiInstances(result, request["instances"])
		}
	}
	return result
}

func appendRequestContextGemini(result []RequestContextMessage, value any) []RequestContextMessage {
	items, ok := value.([]any)
	if !ok {
		if value != nil {
			items = []any{value}
		} else {
			return result
		}
	}
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringValue(content["role"])))
		if role == "" {
			role = "user"
		}
		if _, allowed := requestContextArchiveRoles[role]; !allowed {
			continue
		}
		result = appendRequestContextValue(result, role, content["parts"])
	}
	return result
}

func appendRequestContextGeminiInstances(result []RequestContextMessage, value any) []RequestContextMessage {
	items, ok := value.([]any)
	if !ok {
		return result
	}
	for _, item := range items {
		if instance, ok := item.(map[string]any); ok {
			result = appendRequestContextValue(result, "user", instance["prompt"])
		}
	}
	return result
}

func appendRequestContextValue(result []RequestContextMessage, role string, value any) []RequestContextMessage {
	role = strings.ToLower(strings.TrimSpace(role))
	if _, allowed := requestContextArchiveRoles[role]; !allowed {
		return result
	}
	texts := requestContextTexts(value)
	if len(texts) == 0 {
		return result
	}
	return appendRequestContextMessage(result, role, strings.Join(texts, "\n"))
}

func appendRequestContextMessage(result []RequestContextMessage, role, content string) []RequestContextMessage {
	content = strings.ReplaceAll(content, "\x00", "")
	if strings.TrimSpace(content) == "" {
		return result
	}
	return append(result, RequestContextMessage{Role: role, Content: content})
}

func requestContextTexts(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				if text := requestContextText(object["text"]); text != "" {
					result = append(result, text)
				} else if nested := requestContextTexts(object["content"]); len(nested) > 0 {
					result = append(result, nested...)
				}
			}
		}
		return result
	case map[string]any:
		if text := requestContextText(typed["text"]); text != "" {
			return []string{text}
		}
		return requestContextTexts(typed["content"])
	}
	return nil
}

func requestContextText(value any) string {
	text, _ := value.(string)
	return strings.ReplaceAll(text, "\x00", "")
}

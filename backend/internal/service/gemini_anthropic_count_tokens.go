package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const modelPortTokenCountEstimatedHeader = "x-modelport-token-count-estimated"

type geminiTokenCountResult struct {
	Total     int
	Estimated bool
	RequestID string
	Model     string
}

type geminiTokenCountError struct {
	StatusCode int
	ErrorType  string
	Message    string
}

func (e *geminiTokenCountError) Error() string {
	return fmt.Sprintf("Gemini countTokens failed: status=%d type=%s message=%s", e.StatusCode, e.ErrorType, e.Message)
}

func newGeminiTokenCountError(statusCode int, errorType, message string) error {
	return &geminiTokenCountError{StatusCode: statusCode, ErrorType: errorType, Message: message}
}

func anthropicRequestContainsBase64Image(body []byte) bool {
	var request struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return false
	}
	for _, message := range request.Messages {
		blocks, ok := message.Content.([]any)
		if !ok {
			continue
		}
		for _, rawBlock := range blocks {
			block, ok := rawBlock.(map[string]any)
			if !ok || block["type"] != "image" {
				continue
			}
			source, _ := block["source"].(map[string]any)
			if source["type"] == "base64" {
				return true
			}
		}
	}
	return false
}

func (s *GeminiMessagesCompatService) CountAnthropicTokens(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	publicModel string,
	dispatchModel string,
	claudeBody []byte,
) (*ForwardResult, error) {
	startTime := time.Now()
	publicModel = strings.TrimSpace(publicModel)
	dispatchModel = strings.TrimSpace(dispatchModel)
	if publicModel == "" || dispatchModel == "" {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	geminiBody, err := convertClaudeMessagesToGeminiGenerateContent(claudeBody)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	count, err := s.countGeminiTokens(ctx, c, account, dispatchModel, geminiBody)
	if err != nil {
		var countErr *geminiTokenCountError
		if errors.As(err, &countErr) {
			return nil, s.writeClaudeError(c, countErr.StatusCode, countErr.ErrorType, countErr.Message)
		}
		return nil, err
	}
	if count.Estimated && anthropicRequestContainsBase64Image(claudeBody) {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", "Image token count cannot be estimated reliably for this Gemini account")
	}
	if count.Estimated {
		c.Header(modelPortTokenCountEstimatedHeader, "true")
	}
	c.JSON(http.StatusOK, gin.H{"input_tokens": count.Total})
	return &ForwardResult{
		RequestID:     count.RequestID,
		Model:         publicModel,
		UpstreamModel: count.Model,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

func (s *GeminiMessagesCompatService) countGeminiTokens(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	dispatchModel string,
	geminiBody []byte,
) (*geminiTokenCountResult, error) {
	if account == nil {
		return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini account is unavailable")
	}
	mappedModel := account.GetMappedModel(strings.TrimSpace(dispatchModel))
	if mappedModel == "" {
		return nil, newGeminiTokenCountError(http.StatusBadRequest, "invalid_request_error", "model is required")
	}
	if filteredBody, err := filterEmptyPartsFromGeminiRequest(geminiBody); err == nil {
		geminiBody = filteredBody
	}
	geminiBody = ensureGeminiFunctionCallThoughtSignatures(geminiBody)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	var request *http.Request
	var err error
	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini API key is not configured")
		}
		baseURL, validateErr := s.validateUpstreamBaseURL(account.GetGeminiBaseURL(geminicli.AIStudioBaseURL))
		if validateErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", validateErr.Error())
		}
		fullURL, buildErr := buildGeminiAIStudioModelActionURL(baseURL, mappedModel, "countTokens", false)
		if buildErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", buildErr.Error())
		}
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(geminiBody))
		if err == nil {
			request.Header.Set("x-goog-api-key", apiKey)
		}

	case AccountTypeOAuth:
		if s.tokenProvider == nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini token provider is not configured")
		}
		accessToken, tokenErr := s.tokenProvider.GetAccessToken(ctx, account)
		if tokenErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", sanitizeUpstreamErrorMessage(tokenErr.Error()))
		}
		baseURL, validateErr := s.validateUpstreamBaseURL(account.GetGeminiBaseURL(geminicli.AIStudioBaseURL))
		if validateErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", validateErr.Error())
		}
		fullURL, buildErr := buildGeminiAIStudioModelActionURL(baseURL, mappedModel, "countTokens", false)
		if buildErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", buildErr.Error())
		}
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(geminiBody))
		if err == nil {
			request.Header.Set("Authorization", "Bearer "+accessToken)
		}

	case AccountTypeServiceAccount:
		if s.tokenProvider == nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini token provider is not configured")
		}
		accessToken, tokenErr := s.tokenProvider.GetAccessToken(ctx, account)
		if tokenErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", sanitizeUpstreamErrorMessage(tokenErr.Error()))
		}
		fullURL, buildErr := buildVertexGeminiURL(
			account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, "countTokens", false,
		)
		if buildErr != nil {
			return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", buildErr.Error())
		}
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(geminiBody))
		if err == nil {
			request.Header.Set("Authorization", "Bearer "+accessToken)
		}

	default:
		return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Unsupported Gemini account type")
	}
	if err != nil {
		return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", err.Error())
	}
	request.Header.Set("Content-Type", "application/json")

	response, requestErr := s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	if requestErr != nil {
		return &geminiTokenCountResult{
			Total:     estimateGeminiCountTokens(geminiBody),
			Estimated: true,
			Model:     mappedModel,
		}, nil
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		return &geminiTokenCountResult{
			Total:     estimateGeminiCountTokens(geminiBody),
			Estimated: true,
			Model:     mappedModel,
		}, nil
	}
	requestID := strings.TrimSpace(response.Header.Get("x-request-id"))
	if requestID == "" {
		requestID = strings.TrimSpace(response.Header.Get("x-goog-request-id"))
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	if response.StatusCode >= http.StatusBadRequest {
		if account.Type == AccountTypeOAuth && isGeminiInsufficientScope(response.Header, responseBody) {
			return &geminiTokenCountResult{
				Total:     estimateGeminiCountTokens(geminiBody),
				Estimated: true,
				RequestID: requestID,
				Model:     mappedModel,
			}, nil
		}
		message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		errorType := "api_error"
		switch response.StatusCode {
		case http.StatusBadRequest:
			errorType = "invalid_request_error"
		case http.StatusUnauthorized:
			errorType = "authentication_error"
		case http.StatusForbidden:
			errorType = "permission_error"
		case http.StatusNotFound:
			errorType = "not_found_error"
		case http.StatusTooManyRequests:
			errorType = "rate_limit_error"
		}
		return nil, newGeminiTokenCountError(response.StatusCode, errorType, message)
	}

	total := gjson.GetBytes(responseBody, "totalTokens")
	if !total.Exists() {
		return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini countTokens response did not include totalTokens")
	}
	if total.Int() < 0 {
		return nil, newGeminiTokenCountError(http.StatusBadGateway, "api_error", "Gemini countTokens returned an invalid totalTokens value")
	}
	return &geminiTokenCountResult{
		Total:     int(total.Int()),
		Estimated: false,
		RequestID: requestID,
		Model:     mappedModel,
	}, nil
}

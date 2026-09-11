package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ForwardAsResponses reuses Gemini's native Messages transport and converts
// its output incrementally. Scheduling, billing and retries remain owned by
// the Responses handler; this adapter never makes a second gateway request.
func (s *GeminiMessagesCompatService) ForwardAsResponses(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	if normalized, changed, err := normalizeOpenAIResponsesLegacyIngress(body); err != nil {
		return nil, err
	} else if changed {
		body = normalized
	}
	adapted, mapping, err := adaptResponsesClientToolsForAnthropic(body)
	if err != nil {
		return nil, err
	}
	var request apicompat.ResponsesRequest
	if err := json.Unmarshal(adapted, &request); err != nil {
		return nil, err
	}
	converted, err := apicompat.ResponsesToAnthropicRequest(&request)
	if err != nil {
		return nil, err
	}
	converted.Stream = request.Stream
	claudeBody, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	originalWriter := c.Writer
	w := newGeminiResponsesWriter(originalWriter, request.Model, request.Stream, mapping)
	c.Writer = w
	defer func() { c.Writer = originalWriter }()
	result, forwardErr := s.Forward(ctx, c, account, claudeBody)
	if forwardErr == nil || w.status >= http.StatusBadRequest {
		if finishErr := w.finish(); finishErr != nil && forwardErr == nil {
			forwardErr = finishErr
		}
	}
	return result, forwardErr
}

// Gemini writes Anthropic JSON/SSE to this writer. Only complete translated
// events reach the client, including function calls and their original names.
type geminiResponsesWriter struct {
	gin.ResponseWriter
	stream   bool
	status   int
	model    string
	buffer   bytes.Buffer
	state    *apicompat.AnthropicEventToResponsesState
	restorer *apicompat.ResponsesClientToolStreamRestorer
	mapping  apicompat.ResponsesClientToolMapping
	err      error
}

func newGeminiResponsesWriter(writer gin.ResponseWriter, model string, stream bool, mapping apicompat.ResponsesClientToolMapping) *geminiResponsesWriter {
	state := apicompat.NewAnthropicEventToResponsesState()
	state.Model = model
	return &geminiResponsesWriter{ResponseWriter: writer, stream: stream, status: http.StatusOK,
		model: model, state: state, mapping: mapping, restorer: apicompat.NewResponsesClientToolStreamRestorer(mapping)}
}

func (w *geminiResponsesWriter) WriteHeader(status int) { w.status = status }
func (w *geminiResponsesWriter) Status() int            { return w.status }
func (w *geminiResponsesWriter) WriteHeaderNow() {
	if w.stream && w.status < http.StatusBadRequest {
		w.ResponseWriter.Header().Del("Content-Length")
		w.ResponseWriter.Header().Set("Content-Type", "text/event-stream")
		w.ResponseWriter.WriteHeader(w.status)
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *geminiResponsesWriter) WriteString(value string) (int, error) { return w.Write([]byte(value)) }
func (w *geminiResponsesWriter) Flush() {
	if w.stream && w.ResponseWriter.Written() {
		w.ResponseWriter.Flush()
	}
}

func (w *geminiResponsesWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.buffer.Len()+len(data) > 16<<20 {
		return 0, fmt.Errorf("Gemini Responses conversion buffer exceeded")
	}
	w.buffer.Write(data)
	if !w.stream || w.status >= http.StatusBadRequest {
		return len(data), nil
	}
	for {
		index := bytes.IndexByte(w.buffer.Bytes(), '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buffer.Next(index + 1)))
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			w.err = err
			return 0, err
		}
		for _, output := range apicompat.AnthropicEventToResponsesEvents(&event, w.state) {
			encoded, err := json.Marshal(output)
			if err == nil {
				err = w.writeEvent(encoded)
			}
			if err != nil {
				w.err = err
				return 0, err
			}
		}
	}
	return len(data), nil
}

func (w *geminiResponsesWriter) writeEvent(payload []byte) error {
	outputs, _, err := w.restorer.RestoreEvent(payload)
	if err != nil {
		return err
	}
	for _, output := range outputs {
		w.WriteHeaderNow()
		if _, err := fmt.Fprintf(w.ResponseWriter, "event: %s\ndata: %s\n\n", gjson.GetBytes(output, "type").String(), output); err != nil {
			return err
		}
	}
	return nil
}

func (w *geminiResponsesWriter) finish() error {
	if w.err != nil {
		return w.err
	}
	if w.stream && w.status < http.StatusBadRequest {
		if w.buffer.Len() > 0 {
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
		}
		for _, event := range apicompat.FinalizeAnthropicResponsesStream(w.state) {
			payload, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if err := w.writeEvent(payload); err != nil {
				return err
			}
		}
		w.Flush()
		return nil
	}
	payload := append([]byte(nil), w.buffer.Bytes()...)
	if w.status < http.StatusBadRequest {
		var response apicompat.AnthropicResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return fmt.Errorf("decode Gemini Messages response: %w", err)
		}
		response.Model = w.model
		var err error
		payload, err = json.Marshal(apicompat.AnthropicToResponsesResponse(&response))
		if err != nil {
			return err
		}
		payload, _, err = apicompat.RestoreResponsesClientToolPayload(payload, w.mapping)
		if err != nil {
			return err
		}
	}
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.ResponseWriter.WriteHeader(w.status)
	_, err := w.ResponseWriter.Write(payload)
	return err
}

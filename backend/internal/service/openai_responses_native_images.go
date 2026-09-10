package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const nativeImagesBridgeMaxBytes = 32 << 20

// Buffer only the bounded native JSON reply. Nothing reaches the client before
// validation, so an upstream failure can still use the existing failover path.
type nativeImagesResponseWriter struct {
	gin.ResponseWriter
	header http.Header
	body   bytes.Buffer
	status int
	err    error
}

func (w *nativeImagesResponseWriter) Header() http.Header               { return w.header }
func (w *nativeImagesResponseWriter) WriteHeader(code int)              { w.status = code }
func (w *nativeImagesResponseWriter) WriteHeaderNow()                   {}
func (w *nativeImagesResponseWriter) Flush()                            {}
func (w *nativeImagesResponseWriter) Status() int                       { return w.status }
func (w *nativeImagesResponseWriter) Size() int                         { return w.body.Len() }
func (w *nativeImagesResponseWriter) Written() bool                     { return w.body.Len() > 0 }
func (w *nativeImagesResponseWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *nativeImagesResponseWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(b) > nativeImagesBridgeMaxBytes-w.body.Len() {
		w.err = errors.New("native image response exceeds bridge size limit")
		return 0, w.err
	}
	return w.body.Write(b)
}

func (s *OpenAIGatewayService) forwardResponsesViaNativeImages(ctx context.Context, c *gin.Context, account *Account, body []byte, model string, stream bool) (*OpenAIForwardResult, error) {
	started := time.Now()
	nativeBody, contentType, endpoint, err := s.buildNativeImagesFromResponses(ctx, account, body, model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return nil, err
	}
	// Reuse the native parser without changing the real inbound request metadata.
	originalRequest := c.Request
	parseRequest := originalRequest.Clone(ctx)
	parseRequest.URL.Path = endpoint
	parseRequest.Header.Set("Content-Type", contentType)
	c.Request = parseRequest
	parsed, err := s.ParseOpenAIImagesRequest(c, nativeBody)
	c.Request = originalRequest
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	originalWriter := c.Writer
	capture := &nativeImagesResponseWriter{ResponseWriter: originalWriter, header: make(http.Header), status: http.StatusOK}
	c.Writer = capture
	// Always ask the native endpoint for JSON; downstream SSE is produced below.
	result, forwardErr := func() (*OpenAIForwardResult, error) {
		defer func() { c.Writer = originalWriter }()
		return s.ForwardImages(ctx, c, account, nativeBody, parsed, "")
	}()
	if forwardErr != nil {
		var nativeErr *OpenAIImagesUpstreamError
		if errors.As(forwardErr, &nativeErr) && nativeErr.StatusCode == http.StatusNotFound {
			// A missing model/Images endpoint belongs to this provider, not to
			// the valid inbound Responses URL. No bytes have been committed.
			return nil, &UpstreamFailoverError{StatusCode: nativeErr.StatusCode, ResponseBody: append([]byte(nil), capture.body.Bytes()...)}
		}
		if capture.status >= 400 && capture.body.Len() > 0 {
			c.Data(capture.status, "application/json", capture.body.Bytes())
		}
		return nil, forwardErr
	}
	if capture.err != nil {
		return nil, capture.err
	}
	if result == nil {
		return nil, errors.New("native image upstream returned no result")
	}
	var payload struct {
		Data []struct {
			B64           string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(capture.body.Bytes(), &payload); err != nil || len(payload.Data) == 0 || len(payload.Data) > 4 {
		return nil, errors.New("native image upstream returned no usable images")
	}
	items := make([]map[string]any, 0, len(payload.Data))
	var total int
	for _, image := range payload.Data {
		encoded := image.B64
		if encoded == "" && image.URL != "" {
			encoded, err = s.fetchOpenAIImageURLBase64(ctx, account, image.URL)
			if err != nil {
				return nil, errors.New("could not retrieve native image result")
			}
		}
		data, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil || len(data) == 0 || !isBackfillImageContent(data) {
			return nil, errors.New("native image upstream returned invalid image data")
		}
		total += len(data)
		if total > nativeImagesBridgeMaxBytes {
			return nil, errors.New("native image results exceed size limit")
		}
		itemID, err := nativeImageResponseID("ig_")
		if err != nil {
			return nil, err
		}
		item := map[string]any{"id": itemID, "type": "image_generation_call", "status": "completed", "result": encoded}
		if parsed.OutputFormat != "" {
			item["output_format"] = parsed.OutputFormat
		}
		if parsed.Size != "" {
			item["size"] = parsed.Size
		}
		if parsed.Quality != "" {
			item["quality"] = parsed.Quality
		}
		if image.RevisedPrompt != "" {
			item["revised_prompt"] = image.RevisedPrompt
		}
		items = append(items, item)
	}
	responseID, err := nativeImageResponseID("resp_")
	if err != nil {
		return nil, err
	}
	usage := map[string]any{"input_tokens": result.Usage.InputTokens, "output_tokens": result.Usage.OutputTokens, "total_tokens": result.Usage.InputTokens + result.Usage.OutputTokens}
	response := map[string]any{"id": responseID, "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": model, "output": items, "usage": usage, "error": nil}
	result.Model = model
	result.BillingModel = parsed.Model
	result.ResponseID = responseID
	result.UpstreamEndpoint = endpoint
	result.Stream = stream
	result.ImageCount = len(items)
	result.Duration = time.Since(started)
	ttft := int(result.Duration.Milliseconds())
	result.FirstTokenMs = &ttft
	if requestID := capture.header.Get("X-Request-Id"); requestID != "" {
		c.Header("X-Request-Id", requestID)
	}
	if !stream {
		c.JSON(http.StatusOK, response)
		return result, nil
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	sequence := 0
	emit := func(kind string, event map[string]any) error {
		event["type"], event["sequence_number"] = kind, sequence
		sequence++
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", kind, encoded)
		if err == nil {
			c.Writer.Flush()
		}
		return err
	}
	initial := map[string]any{"id": responseID, "object": "response", "created_at": response["created_at"], "status": "in_progress", "model": model, "output": []any{}}
	if err := emit("response.created", map[string]any{"response": initial}); err != nil {
		return result, err
	}
	for index, item := range items {
		pending := map[string]any{"id": item["id"], "type": "image_generation_call", "status": "in_progress"}
		if err := emit("response.output_item.added", map[string]any{"output_index": index, "item": pending}); err != nil {
			return result, err
		}
		if err := emit("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return result, err
		}
	}
	if err := emit("response.completed", map[string]any{"response": response}); err != nil {
		return result, err
	}
	return result, nil
}

func nativeImageResponseID(prefix string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(id[:]), nil
}

func (s *OpenAIGatewayService) buildNativeImagesFromResponses(ctx context.Context, account *Account, body []byte, model string) ([]byte, string, string, error) {
	fail := func(message string) ([]byte, string, string, error) { return nil, "", "", errors.New(message) }
	if len(body) > nativeImagesBridgeMaxBytes {
		return fail("image request exceeds 32 MiB limit")
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return fail("invalid Responses JSON")
	}
	if previous, _ := request["previous_response_id"].(string); previous != "" {
		return fail("native image editing requires image data in input, not previous_response_id")
	}
	if conversation := request["conversation"]; conversation != nil {
		return fail("native image editing requires full input, not a stored conversation")
	}
	fields := map[string]any{"model": model, "n": 1, "response_format": "b64_json"}
	keys := []string{"n", "size", "quality", "background", "output_format", "output_compression", "moderation", "input_fidelity", "style"}
	for _, key := range keys {
		if value, ok := request[key]; ok && value != nil {
			fields[key] = value
		}
	}
	var mask string
	if rawMask, exists := request["mask"]; exists && rawMask != nil {
		var ok bool
		mask, ok = rawMask.(string)
		if !ok {
			return fail("mask must be an image data URL or public URL")
		}
	}
	var action string
	if tools, ok := request["tools"].([]any); ok {
		for _, value := range tools {
			tool, ok := value.(map[string]any)
			if !ok || tool["type"] != "image_generation" {
				continue
			}
			if tm, _ := tool["model"].(string); tm != "" && tm != model {
				return fail("image tool model must match the dedicated image model")
			}
			if value, _ := tool["action"].(string); value != "" {
				if value != "auto" && value != "edit" && value != "generate" {
					return fail("unsupported image action")
				}
				action = value
			}
			for _, key := range keys {
				if value, ok := tool[key]; ok && value != nil {
					fields[key] = value
				}
			}
			if m, ok := tool["input_image_mask"].(map[string]any); ok {
				if m["file_id"] != nil {
					return fail("mask file_id is not supported; provide image_url")
				}
				mask, _ = m["image_url"].(string)
				if mask == "" {
					return fail("mask image_url is required")
				}
			}
		}
	}
	if n, ok := fields["n"].(float64); ok {
		if n < 1 || n > 4 || math.Trunc(n) != n {
			return fail("n must be an integer between 1 and 4")
		}
	} else if n, ok := fields["n"].(int); !ok || n != 1 {
		return fail("n must be an integer between 1 and 4")
	}
	var prompts, urls []string
	var visit func(any, bool) error
	visit = func(value any, user bool) error {
		switch v := value.(type) {
		case string:
			if user && strings.TrimSpace(v) != "" {
				prompts = append(prompts, v)
			}
		case []any:
			for _, child := range v {
				if err := visit(child, user); err != nil {
					return err
				}
			}
		case map[string]any:
			typ, _ := v["type"].(string)
			switch typ {
			case "input_image":
				url, _ := v["image_url"].(string)
				if url == "" {
					return errors.New("input images require image_url/data URL; file_id is not supported")
				}
				urls = append(urls, url)
			case "image_generation_call":
				if result, _ := v["result"].(string); result != "" {
					urls = append(urls, "data:image/png;base64,"+result)
				}
			case "input_text", "text":
				if user {
					if text, _ := v["text"].(string); strings.TrimSpace(text) != "" {
						prompts = append(prompts, text)
					}
				}
			case "input_file":
				return errors.New("native image requests do not support input_file")
			default:
				if content, ok := v["content"]; ok {
					role, _ := v["role"].(string)
					return visit(content, role == "user" || role == "")
				}
			}
		}
		return nil
	}
	if err := visit(request["input"], true); err != nil {
		return fail(err.Error())
	}
	if len(prompts) == 0 {
		if p, _ := request["prompt"].(string); strings.TrimSpace(p) != "" {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		return fail("image prompt is required")
	}
	fields["prompt"] = strings.Join(prompts, "\n")
	if len(urls) == 0 {
		if action == "edit" {
			return fail("image edit action requires an input image")
		}
		if mask != "" {
			return fail("mask requires an input image")
		}
		encoded, err := json.Marshal(fields)
		return encoded, "application/json", openAIImagesGenerationsEndpoint, err
	}
	if len(urls) > 4 {
		return fail("at most four input images are supported")
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range fields {
		if err := writer.WriteField(key, fmt.Sprint(value)); err != nil {
			return fail("could not encode image options")
		}
	}
	writeImage := func(field, rawURL string, index int) error {
		if len(rawURL) > nativeImagesBridgeMaxBytes {
			return errors.New("input image exceeds size limit")
		}
		encoded, err := s.fetchOpenAIImageURLBase64(ctx, account, rawURL)
		if err != nil {
			return errors.New("could not load a public input image; use a valid image data URL")
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > openAIImageMaxDownloadBytes || !isBackfillImageContent(data) {
			return errors.New("invalid or oversized input image")
		}
		if len(data) > nativeImagesBridgeMaxBytes-buffer.Len() {
			return errors.New("total input images exceed size limit")
		}
		contentType := http.DetectContentType(data)
		ext := "png"
		if strings.Contains(contentType, "jpeg") {
			ext = "jpg"
		} else if strings.Contains(contentType, "webp") {
			ext = "webp"
		} else if strings.Contains(contentType, "gif") {
			ext = "gif"
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="image-%d.%s"`, field, index, ext))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		_, err = part.Write(data)
		return err
	}
	for index, url := range urls {
		if err := writeImage("image[]", url, index); err != nil {
			return fail(err.Error())
		}
	}
	if mask != "" {
		if err := writeImage("mask", mask, 0); err != nil {
			return fail(err.Error())
		}
	}
	if err := writer.Close(); err != nil {
		return fail("could not finalize image upload")
	}
	return buffer.Bytes(), writer.FormDataContentType(), openAIImagesEditsEndpoint, nil
}

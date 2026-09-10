package repository

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	httpUpstreamTraceSlow      = 5 * time.Second
	httpUpstreamTraceMaxEvents = 64
)

// Only fixed stage names, timings and flags are retained, never hook arguments
// containing addresses, headers, request data or error messages.
type httpUpstreamTraceEvent struct {
	Stage   string `json:"stage"`
	AtMs    int64  `json:"at_ms"`
	HasErr  bool   `json:"has_error,omitempty"`
	Reused  *bool  `json:"reused,omitempty"`
	WasIdle *bool  `json:"was_idle,omitempty"`
	IdleMs  int64  `json:"idle_ms,omitempty"`
}

type httpUpstreamTrace struct {
	mu        sync.Mutex
	ctx       context.Context
	startedAt time.Time
	fields    map[string]any
	events    []httpUpstreamTraceEvent
	truncated bool
	finished  bool
	firstBody sync.Once
	bodyError string
}

func newHTTPUpstreamTrace(req *http.Request, accountID int64, startedAt time.Time) *httpUpstreamTrace {
	ctx := context.Background()
	host, scheme := "", ""
	length := int64(-1)
	if req != nil {
		ctx = req.Context()
		length = req.ContentLength
		if length < 0 || (length == 0 && req.Body != nil && req.Body != http.NoBody) {
			length = -1
		}
		if req.URL != nil {
			host = strings.ToLower(req.URL.Hostname())
			scheme = strings.ToLower(req.URL.Scheme)
		}
	}
	return &httpUpstreamTrace{ctx: ctx, startedAt: startedAt,
		fields: map[string]any{"account_id": accountID, "host": host, "scheme": scheme, "request_content_length": length},
		events: make([]httpUpstreamTraceEvent, 0, 12)}
}

func (t *httpUpstreamTrace) elapsed(at time.Time) int64 {
	return max(int64(0), at.Sub(t.startedAt).Milliseconds())
}

func (t *httpUpstreamTrace) record(event httpUpstreamTraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	event.AtMs = t.elapsed(time.Now())
	if event.Stage == "got_first_response_byte" {
		if _, ok := t.fields["got_first_response_byte_ms"]; !ok {
			t.fields["got_first_response_byte_ms"] = event.AtMs
		}
	}
	if len(t.events) < httpUpstreamTraceMaxEvents {
		t.events = append(t.events, event)
	} else {
		t.truncated = true
	}
}

func (t *httpUpstreamTrace) requestWithTrace(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) { t.record(httpUpstreamTraceEvent{Stage: "get_conn"}) },
		GotConn: func(i httptrace.GotConnInfo) {
			t.record(httpUpstreamTraceEvent{Stage: "got_conn", Reused: &i.Reused, WasIdle: &i.WasIdle, IdleMs: i.IdleTime.Milliseconds()})
		},
		DNSStart: func(httptrace.DNSStartInfo) { t.record(httpUpstreamTraceEvent{Stage: "dns_start"}) },
		DNSDone: func(i httptrace.DNSDoneInfo) {
			t.record(httpUpstreamTraceEvent{Stage: "dns_done", HasErr: i.Err != nil})
		},
		ConnectStart: func(string, string) { t.record(httpUpstreamTraceEvent{Stage: "connect_start"}) },
		ConnectDone: func(_, _ string, err error) {
			t.record(httpUpstreamTraceEvent{Stage: "connect_done", HasErr: err != nil})
		},
		TLSHandshakeStart: func() { t.record(httpUpstreamTraceEvent{Stage: "tls_handshake_start"}) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			t.record(httpUpstreamTraceEvent{Stage: "tls_handshake_done", HasErr: err != nil})
		},
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			t.record(httpUpstreamTraceEvent{Stage: "wrote_request", HasErr: i.Err != nil})
		},
		GotFirstResponseByte: func() { t.record(httpUpstreamTraceEvent{Stage: "got_first_response_byte"}) },
	}
	// WithClientTrace composes existing hooks rather than replacing them.
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

func (t *httpUpstreamTrace) markClientAcquired(at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fields["client_acquire_ms"] = t.elapsed(at)
}

func (t *httpUpstreamTrace) markHeaders(resp *http.Response, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fields["client_do_return_ms"] = t.elapsed(at)
	if resp == nil {
		return
	}
	t.fields["response_headers_ms"] = t.elapsed(at)
	t.fields["status_code"] = resp.StatusCode
	t.fields["http_protocol"] = resp.Proto
	// Validated correlation metadata only; no arbitrary response header logging.
	if raw := resp.Header.Get("X-Request-ID"); len(raw) <= 64 {
		if id, err := uuid.Parse(raw); err == nil {
			t.fields["upstream_request_id"] = id.String()
		}
	}
}

func classifyHTTPUpstreamTraceError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "io"
}

// finish emits once after body consumption, so a fast header followed by a slow
// stream is not missed. This time includes caller processing and backpressure.
func (t *httpUpstreamTrace) finish(err error, at time.Time) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished = true
	errorKind := classifyHTTPUpstreamTraceError(err)
	if errorKind == "" {
		errorKind = t.bodyError
	}
	if status, ok := t.fields["status_code"].(int); errorKind == "" && ok && status >= http.StatusBadRequest {
		errorKind = "http_status"
	}
	total := t.elapsed(at)
	if errorKind == "" && total < httpUpstreamTraceSlow.Milliseconds() {
		t.mu.Unlock()
		return
	}
	fields := make([]zap.Field, 0, len(t.fields)+5)
	for k, v := range t.fields {
		fields = append(fields, zap.Any(k, v))
	}
	fields = append(fields, zap.Int64("total_ms", total), zap.Bool("events_truncated", t.truncated),
		zap.Any("events", append([]httpUpstreamTraceEvent(nil), t.events...)))
	if errorKind != "" {
		fields = append(fields, zap.String("error_kind", errorKind))
	}
	t.mu.Unlock()
	logger.FromContext(t.ctx).Info("http_upstream_trace", fields...)
}

type httpUpstreamTraceBody struct {
	io.ReadCloser
	trace *httpUpstreamTrace
}

func (b *httpUpstreamTraceBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.trace.firstBody.Do(func() {
			b.trace.mu.Lock()
			b.trace.fields["first_body_read_ms"] = b.trace.elapsed(time.Now())
			b.trace.mu.Unlock()
		})
	}
	if err != nil && !errors.Is(err, io.EOF) {
		b.noteError(err)
	}
	return n, err
}

func (b *httpUpstreamTraceBody) noteError(err error) {
	kind := classifyHTTPUpstreamTraceError(err)
	b.trace.mu.Lock()
	if b.trace.bodyError == "" {
		b.trace.bodyError = kind
	}
	b.trace.mu.Unlock()
}

func (b *httpUpstreamTraceBody) Close() error {
	err := b.ReadCloser.Close()
	if err != nil {
		b.noteError(err)
	}
	return err
}

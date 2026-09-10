package repository

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func traceFixture(t *testing.T, elapsed time.Duration) (*httpUpstreamTrace, *http.Request, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "test-local-id")))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://private.example/secret/path?token=do-not-log", strings.NewReader("secret-body"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret-key")
	return newHTTPUpstreamTrace(req, 7, time.Now().Add(-elapsed)), req, logs
}

func TestHTTPUpstreamTraceEventsComposeAndPreservePrivacy(t *testing.T) {
	tr, req, logs := traceFixture(t, 6*time.Second)
	called := false
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { called = true },
	}))
	hooks := httptrace.ContextClientTrace(tr.requestWithTrace(req).Context())
	hooks.GetConn("secret-address:443")
	hooks.GotConn(httptrace.GotConnInfo{Reused: true, WasIdle: true, IdleTime: 25 * time.Millisecond})
	hooks.DNSStart(httptrace.DNSStartInfo{Host: "secret-dns"})
	hooks.DNSDone(httptrace.DNSDoneInfo{})
	hooks.ConnectStart("tcp", "secret-ip:443")
	hooks.ConnectDone("tcp", "secret-ip:443", errors.New("secret-error"))
	hooks.TLSHandshakeStart()
	hooks.TLSHandshakeDone(tls.ConnectionState{}, nil)
	hooks.WroteRequest(httptrace.WroteRequestInfo{})
	hooks.GotFirstResponseByte()
	tr.markClientAcquired(time.Now())
	tr.markHeaders(&http.Response{StatusCode: 200, Proto: "HTTP/2.0", Header: http.Header{"X-Request-Id": {"c201db81-e37f-4f82-a6b5-0605a0ba53c9"}}}, time.Now())
	tr.finish(nil, time.Now())
	tr.finish(nil, time.Now())
	require.True(t, called)
	require.Len(t, logs.All(), 1)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "test-local-id", fields["request_id"])
	require.Equal(t, "HTTP/2.0", fields["http_protocol"])
	require.Equal(t, "c201db81-e37f-4f82-a6b5-0605a0ba53c9", fields["upstream_request_id"])
	require.Len(t, fields["events"], 10)
	require.True(t, *tr.events[1].Reused)
	require.GreaterOrEqual(t, tr.events[0].AtMs, int64(6000))
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	for _, secret := range []string{"secret/path", "do-not-log", "secret-body", "secret-key", "secret-address", "secret-dns", "secret-ip", "secret-error"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestHTTPUpstreamTraceMissingHooksAreNotZero(t *testing.T) {
	tr, _, logs := traceFixture(t, 0)
	tr.finish(errors.New("hidden-error"), time.Now())
	fields := logs.All()[0].ContextMap()
	for _, field := range []string{"response_headers_ms", "got_first_response_byte_ms", "first_body_read_ms", "client_acquire_ms"} {
		require.NotContains(t, fields, field)
	}
	require.Equal(t, "io", fields["error_kind"])
}

func TestHTTPUpstreamTraceFastHeadersSlowBody(t *testing.T) {
	tr, _, logs := traceFixture(t, 6*time.Second)
	tr.markHeaders(&http.Response{StatusCode: 200}, tr.startedAt.Add(time.Millisecond))
	require.Zero(t, logs.Len())
	body := &httpUpstreamTraceBody{ReadCloser: io.NopCloser(strings.NewReader("data: example\n\n")), trace: tr}
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "data: example\n\n", string(content))
	require.NoError(t, body.Close())
	tr.finish(nil, time.Now())
	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	require.Equal(t, int64(1), fields["response_headers_ms"])
	require.GreaterOrEqual(t, fields["first_body_read_ms"].(int64), int64(6000))
	require.NotContains(t, fields, "error_kind", "EOF is not an error")
}

func TestHTTPUpstreamTraceFastSuccessSilentAndMalformedIDSkipped(t *testing.T) {
	tr, _, logs := traceFixture(t, 0)
	tr.markHeaders(&http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": {"Bearer secret"}}}, time.Now())
	require.NotContains(t, tr.fields, "upstream_request_id")
	tr.finish(nil, time.Now())
	require.Zero(t, logs.Len())
}

func TestHTTPUpstreamTraceCallbacksConcurrentWithFinish(t *testing.T) {
	tr, req, logs := traceFixture(t, 6*time.Second)
	hooks := httptrace.ContextClientTrace(tr.requestWithTrace(req).Context())
	// Force truncation deterministically before racing callbacks with completion.
	for i := 0; i < httpUpstreamTraceMaxEvents+1; i++ {
		hooks.GetConn("ignored")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				hooks.GotConn(httptrace.GotConnInfo{Reused: true})
				hooks.GotFirstResponseByte()
			}
			tr.finish(nil, time.Now())
		}()
	}
	wg.Wait()
	require.Len(t, logs.All(), 1)
	require.Equal(t, true, logs.All()[0].ContextMap()["events_truncated"])
	require.Len(t, logs.All()[0].ContextMap()["events"], httpUpstreamTraceMaxEvents)
}

func TestHTTPUpstreamTraceContentLengthUnknown(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test", io.NopCloser(strings.NewReader("unknown")))
	require.NoError(t, err)
	require.Equal(t, int64(-1), newHTTPUpstreamTrace(req, 1, time.Now()).fields["request_content_length"])
}

type traceFailingBody struct{ readErr, closeErr error }

func (b *traceFailingBody) Read([]byte) (int, error) { return 0, b.readErr }
func (b *traceFailingBody) Close() error             { return b.closeErr }

func TestHTTPUpstreamTraceBodyPreservesErrorsAndEmitsOnce(t *testing.T) {
	tr, _, logs := traceFixture(t, 0)
	readErr, closeErr := errors.New("secret-read"), errors.New("secret-close")
	raw := &httpUpstreamTraceBody{ReadCloser: &traceFailingBody{readErr, closeErr}, trace: tr}
	closed := 0
	body := wrapTrackedBody(raw, func() { closed++; tr.finish(nil, time.Now()) })
	_, err := body.Read(make([]byte, 1))
	require.ErrorIs(t, err, readErr)
	require.ErrorIs(t, body.Close(), closeErr)
	require.ErrorIs(t, body.Close(), closeErr)
	require.Equal(t, 1, closed)
	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "io", fields["error_kind"])
	require.NotContains(t, fields, "first_body_read_ms")
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-read")
	require.NotContains(t, string(encoded), "secret-close")
}

func TestHTTPUpstreamTraceDoDoesNotPreReadStream(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: test\n\n")
	}))
	defer server.Close()
	defer unblock()
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() { resp, err := NewHTTPUpstream(nil).Do(req, "", 7, 1); done <- result{resp, err} }()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		defer got.resp.Body.Close()
		unblock()
		data, err := io.ReadAll(got.resp.Body)
		require.NoError(t, err)
		require.Equal(t, "data: test\n\n", string(data))
	case <-time.After(3 * time.Second):
		t.Fatal("Do waited for stream data after headers")
	}
}

func TestHTTPUpstreamTraceFastHTTPErrorIsRecorded(t *testing.T) {
	tr, _, logs := traceFixture(t, 0)
	tr.markHeaders(&http.Response{StatusCode: http.StatusTooManyRequests}, time.Now())
	tr.finish(nil, time.Now())
	require.Equal(t, 1, logs.Len())
	require.Equal(t, "http_status", logs.All()[0].ContextMap()["error_kind"])
}

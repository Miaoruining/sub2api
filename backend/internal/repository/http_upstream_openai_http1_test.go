package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func (s *HTTPUpstreamSuite) TestOpenAIHTTP1ProfileRetainsOpenAITimeoutAndSharesH1Mode() {
	s.cfg.Gateway = config.GatewayConfig{
		ResponseHeaderTimeout:       600,
		OpenAIResponseHeaderTimeout: 1800,
		OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{
			Enabled: true,
		},
	}
	svc := s.newService()

	h1Entry, err := svc.getClientEntry("", 1, 1, service.HTTPUpstreamProfileOpenAIHTTP1, false, false)
	require.NoError(s.T(), err)
	h1Transport, ok := h1Entry.client.Transport.(*http.Transport)
	require.True(s.T(), ok, "expected *http.Transport")
	require.Equal(s.T(), 1800*time.Second, h1Transport.ResponseHeaderTimeout)
	require.False(s.T(), h1Transport.ForceAttemptHTTP2)
	require.Equal(s.T(), upstreamProtocolModeOpenAIH1, h1Entry.protocolMode)

	h2Entry, err := svc.getClientEntry("", 1, 1, service.HTTPUpstreamProfileOpenAI, false, false)
	require.NoError(s.T(), err)
	h2Transport, ok := h2Entry.client.Transport.(*http.Transport)
	require.True(s.T(), ok, "expected *http.Transport")
	require.True(s.T(), h2Transport.ForceAttemptHTTP2)
	require.Equal(s.T(), upstreamProtocolModeOpenAIH2, h2Entry.protocolMode)
	require.NotSame(s.T(), h1Entry, h2Entry, "HTTP/1.1 and HTTP/2 profiles must not share a cached client")

	h1Transport.CloseIdleConnections()
	h2Transport.CloseIdleConnections()
}

func TestBuildUpstreamTransportOpenAIHTTP1NegotiatesHTTP11OverTLS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 || r.ProtoMinor != 1 {
			t.Errorf("server received %s, want HTTP/1.1", r.Proto)
		}
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH1)
	require.NoError(t, err)
	defer transport.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, response.ProtoMajor)
	require.Equal(t, 1, response.ProtoMinor)
}

func TestBuildUpstreamTransportOpenAIHTTP1DeliversFirstStreamChunkBeforeEOF(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseServer := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server does not support flushing")
			return
		}
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher.Flush()
		<-release
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	// 释放服务端阻塞的流，再关闭测试服务器，避免断言失败时泄漏 goroutine。
	defer releaseServer()

	transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH1)
	require.NoError(t, err)
	defer transport.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	responseCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		response, roundTripErr := transport.RoundTrip(request)
		if roundTripErr != nil {
			errCh <- roundTripErr
			return
		}
		responseCh <- response
	}()

	var response *http.Response
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case response = <-responseCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "HTTP/1.1 response headers were not delivered")
	}
	if response == nil {
		return
	}
	defer response.Body.Close()

	firstChunk := make([]byte, len("data: first\n\n"))
	readCh := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(response.Body, firstChunk)
		readCh <- err
	}()
	select {
	case err := <-readCh:
		require.NoError(t, err)
		require.Equal(t, "data: first\n\n", string(firstChunk))
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first stream chunk was not delivered before EOF")
	}
}

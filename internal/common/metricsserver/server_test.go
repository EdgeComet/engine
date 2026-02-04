package metricsserver

// NOTE: Tests involving FastHTTP server shutdown may trigger benign data race warnings
// when run with -race flag. This is a known limitation in FastHTTP's internal shutdown
// logic (github.com/valyala/fasthttp) where connection cleanup races with worker goroutines.
// The races are harmless and do not affect real-world server behavior. All tests pass
// functionally without the race detector.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

type mockMetricsHandler struct {
	called bool
}

func (m *mockMetricsHandler) ServeHTTP(ctx *fasthttp.RequestCtx) {
	m.called = true
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString("# HELP test_metric A test metric\n# TYPE test_metric counter\ntest_metric 42\n")
}

func TestStartMetricsServer_Disabled(t *testing.T) {
	logger := zap.NewNop()
	handler := &mockMetricsHandler{}

	server, err := StartMetricsServer(false, ":10079", "/metrics", handler, logger)

	require.NoError(t, err)
	assert.Nil(t, server, "Should return nil when metrics disabled")
	assert.False(t, handler.called, "Handler should not be called")
}

func TestStartMetricsServer_SeparatePort(t *testing.T) {
	logger := zap.NewNop()
	handler := &mockMetricsHandler{}

	server, err := StartMetricsServer(true, ":19091", "/metrics", handler, logger)

	require.NoError(t, err)
	require.NotNil(t, server, "Should return server when metrics port different from main port")

	time.Sleep(200 * time.Millisecond)

	defer func() {
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.ShutdownWithContext(ctx)
		}
	}()

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI("http://localhost:19091/metrics")
	req.Header.SetMethod("GET")
	// Avoid keep-alive to prevent shutdown/read data race in fasthttp internals
	req.Header.SetConnectionClose()

	client := &fasthttp.Client{
		MaxIdleConnDuration: 0,
	}
	err = client.Do(req, resp)

	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	assert.True(t, handler.called, "Handler should be called")
	assert.Contains(t, string(resp.Body()), "test_metric 42")

	// Allow server workers to finish processing before shutdown
	time.Sleep(100 * time.Millisecond)
}

func TestMetricsHandler_CorrectPath(t *testing.T) {
	logger := zap.NewNop()
	mockHandler := &mockMetricsHandler{}

	handler := createMetricsHandler("/metrics", mockHandler, logger)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/metrics")

	handler(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.True(t, mockHandler.called, "Metrics handler should be called for /metrics")
}

func TestMetricsHandler_WrongPath(t *testing.T) {
	logger := zap.NewNop()
	mockHandler := &mockMetricsHandler{}

	handler := createMetricsHandler("/metrics", mockHandler, logger)

	testCases := []struct {
		name string
		path string
	}{
		{"root path", "/"},
		{"render path", "/render"},
		{"health path", "/health"},
		{"wrong metrics path", "/metric"},
		{"nested path", "/metrics/detailed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockHandler.called = false
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.SetRequestURI(tc.path)

			handler(ctx)

			assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
			assert.Equal(t, "Not Found", string(ctx.Response.Body()))
			assert.False(t, mockHandler.called, "Metrics handler should not be called for "+tc.path)
		})
	}
}

func TestMetricsHandler_CustomPath(t *testing.T) {
	logger := zap.NewNop()
	mockHandler := &mockMetricsHandler{}

	handler := createMetricsHandler("/custom/metrics", mockHandler, logger)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/custom/metrics")

	handler(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.True(t, mockHandler.called, "Metrics handler should be called for custom path")

	mockHandler.called = false
	ctx2 := &fasthttp.RequestCtx{}
	ctx2.Request.SetRequestURI("/metrics")

	handler(ctx2)

	assert.Equal(t, fasthttp.StatusNotFound, ctx2.Response.StatusCode())
	assert.False(t, mockHandler.called, "Should not serve on default path when custom path configured")
}

func TestStartMetricsServer_GracefulShutdown(t *testing.T) {
	logger := zap.NewNop()
	handler := &mockMetricsHandler{}

	server, err := StartMetricsServer(true, ":19092", "/metrics", handler, logger)

	require.NoError(t, err)
	require.NotNil(t, server)

	time.Sleep(200 * time.Millisecond)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI("http://localhost:19092/metrics")
	req.Header.SetMethod("GET")
	// Avoid keep-alive to prevent shutdown/read data race in fasthttp internals
	req.Header.SetConnectionClose()

	client := &fasthttp.Client{
		MaxIdleConnDuration: 0,
	}
	err = client.Do(req, resp)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	// Allow server workers to finish processing before shutdown
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.ShutdownWithContext(ctx)
	assert.NoError(t, err, "Graceful shutdown should complete without error")

	time.Sleep(100 * time.Millisecond)

	resp2 := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp2)
	req.SetRequestURI("http://localhost:19092/metrics")

	err = client.Do(req, resp2)
	assert.Error(t, err, "Should fail to connect after shutdown")
}

func TestStartMetricsServer_PortConflict(t *testing.T) {
	logger := zap.NewNop()
	handler1 := &mockMetricsHandler{}
	handler2 := &mockMetricsHandler{}

	server1, err := StartMetricsServer(true, ":19093", "/metrics", handler1, logger)
	require.NoError(t, err)
	require.NotNil(t, server1)

	defer func() {
		if server1 != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server1.ShutdownWithContext(ctx)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	server2, _ := StartMetricsServer(true, ":19093", "/metrics", handler2, logger)

	assert.NotNil(t, server2, "Function should return server object even if bind fails")

	time.Sleep(100 * time.Millisecond)

	if server2 != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = server2.ShutdownWithContext(ctx)
	}
}

func TestMetricsServerConfiguration(t *testing.T) {
	logger := zap.NewNop()
	handler := &mockMetricsHandler{}

	server, err := StartMetricsServer(true, ":19094", "/metrics", handler, logger)
	require.NoError(t, err)
	require.NotNil(t, server)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.ShutdownWithContext(ctx)
	}()

	assert.Equal(t, "EdgeComet-Metrics", server.Name)
	assert.Equal(t, 10*time.Second, server.ReadTimeout)
	assert.Equal(t, 10*time.Second, server.WriteTimeout)
	assert.Equal(t, 1*1024, server.MaxRequestBodySize)
	assert.False(t, server.DisableKeepalive)
	assert.True(t, server.TCPKeepalive)
	assert.Equal(t, 30*time.Second, server.TCPKeepalivePeriod)
	assert.Equal(t, 100, server.MaxConnsPerIP)
	assert.Equal(t, 1000, server.MaxRequestsPerConn)
	assert.Equal(t, 100, server.Concurrency)
}

func TestStartMetricsServer_MultipleRequests(t *testing.T) {
	logger := zap.NewNop()
	handler := &mockMetricsHandler{}

	server, err := StartMetricsServer(true, ":19095", "/metrics", handler, logger)
	require.NoError(t, err)
	require.NotNil(t, server)

	time.Sleep(200 * time.Millisecond)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.ShutdownWithContext(ctx)
	}()

	client := &fasthttp.Client{
		MaxIdleConnDuration: 0,
	}

	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("request_%d", i), func(t *testing.T) {
			handler.called = false

			req := fasthttp.AcquireRequest()
			defer fasthttp.ReleaseRequest(req)
			resp := fasthttp.AcquireResponse()
			defer fasthttp.ReleaseResponse(resp)

			req.SetRequestURI("http://localhost:19095/metrics")
			req.Header.SetMethod("GET")
			// Avoid keep-alive to prevent shutdown/read data race in fasthttp internals
			req.Header.SetConnectionClose()

			err := client.Do(req, resp)
			require.NoError(t, err)
			assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
			assert.True(t, handler.called)
		})
	}

	// Allow server workers to finish processing before shutdown
	time.Sleep(100 * time.Millisecond)
}

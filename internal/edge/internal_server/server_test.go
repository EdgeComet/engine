package internal_server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

func TestNewInternalServer(t *testing.T) {
	logger := zap.NewNop()

	server := NewInternalServer("test-key", logger)

	assert.NotNil(t, server)
	assert.Equal(t, "test-key", server.authKey)
	assert.NotNil(t, server.routes)
}

func TestRegisterHandler(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	called := false
	handler := func(ctx *fasthttp.RequestCtx) {
		called = true
	}

	server.RegisterHandler("GET", "/test", handler)

	assert.NotNil(t, server.routes["GET"]["/test"])

	// Test the handler is callable
	ctx := &fasthttp.RequestCtx{}
	server.routes["GET"]["/test"](ctx)
	assert.True(t, called)
}

func TestAuthentication_MissingHeader(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/test", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.SetMethod("GET")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusUnauthorized, ctx.Response.StatusCode())
}

func TestAuthentication_InvalidHeader(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/test", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("X-Internal-Auth", "wrong-key")
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.SetMethod("GET")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusUnauthorized, ctx.Response.StatusCode())
}

func TestAuthentication_ValidHeader(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/test", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString("success")
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("X-Internal-Auth", "test-key")
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.SetMethod("GET")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, "success", string(ctx.Response.Body()))
}

func TestRouting_NotFound(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/test", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("X-Internal-Auth", "test-key")
	ctx.Request.SetRequestURI("/nonexistent")
	ctx.Request.Header.SetMethod("GET")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
}

func TestRouting_MethodNotAllowed(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/test", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("X-Internal-Auth", "test-key")
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.SetMethod("POST")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusMethodNotAllowed, ctx.Response.StatusCode())
}

func TestRouting_PrefixMatch(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	var capturedPath string
	server.RegisterHandler("GET", "/debug/har", func(ctx *fasthttp.RequestCtx) {
		capturedPath = string(ctx.Path())
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("X-Internal-Auth", "test-key")
	ctx.Request.SetRequestURI("/debug/har/1/request-123")
	ctx.Request.Header.SetMethod("GET")

	handler := server.Handler()
	handler(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, "/debug/har/1/request-123", capturedPath)
}

func TestRouting_MultipleHandlers(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	server.RegisterHandler("GET", "/pull", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyString("pull")
	})
	server.RegisterHandler("POST", "/push", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyString("push")
	})
	server.RegisterHandler("GET", "/status", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyString("status")
	})

	tests := []struct {
		method   string
		path     string
		expected string
	}{
		{"GET", "/pull", "pull"},
		{"POST", "/push", "push"},
		{"GET", "/status", "status"},
	}

	for _, tt := range tests {
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.Header.Set("X-Internal-Auth", "test-key")
		ctx.Request.SetRequestURI(tt.path)
		ctx.Request.Header.SetMethod(tt.method)

		handler := server.Handler()
		handler(ctx)

		assert.Equal(t, tt.expected, string(ctx.Response.Body()), "path: %s", tt.path)
	}
}

func TestGetStartTime(t *testing.T) {
	logger := zap.NewNop()
	server := NewInternalServer("test-key", logger)

	startTime := server.GetStartTime()
	assert.False(t, startTime.IsZero())
}

func TestPathConstants(t *testing.T) {
	assert.Equal(t, "/internal/cache/pull", PathCachePull)
	assert.Equal(t, "/internal/cache/push", PathCachePush)
	assert.Equal(t, "/internal/cache/status", PathCacheStatus)
	assert.Equal(t, "/internal/cache/recache", PathCacheRecache)
	assert.Equal(t, "/debug/har", PathDebugHAR)
}

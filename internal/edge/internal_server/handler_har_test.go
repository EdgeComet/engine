package internal_server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// mockHARStore implements HARStore for testing
type mockHARStore struct {
	data map[string][]byte
	err  error
}

func (m *mockHARStore) GetHAR(ctx context.Context, hostID, requestID string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := hostID + ":" + requestID
	return m.data[key], nil
}

func TestHandleHAR_Success(t *testing.T) {
	harData := []byte(`{"log":{"version":"1.2"}}`)
	store := &mockHARStore{
		data: map[string][]byte{
			"1:test-request-123": harData,
		},
	}

	handler := NewHARHandler(store, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/1/test-request-123")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHAR(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, "application/json", string(ctx.Response.Header.ContentType()))
	assert.Equal(t, "gzip", string(ctx.Response.Header.Peek("Content-Encoding")))
	assert.Equal(t, harData, ctx.Response.Body())
}

func TestHandleHAR_NotFound(t *testing.T) {
	store := &mockHARStore{
		data: map[string][]byte{},
	}

	handler := NewHARHandler(store, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/1/nonexistent")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHAR(ctx)

	assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
}

func TestHandleHAR_InvalidPath(t *testing.T) {
	store := &mockHARStore{}
	handler := NewHARHandler(store, zap.NewNop())

	testCases := []struct {
		name string
		path string
	}{
		{"missing request ID", "/debug/har/1"},
		{"missing host ID", "/debug/har//test-123"},
		{"empty path", "/debug/har/"},
		{"too many segments", "/debug/har/1/2/3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.SetRequestURI(tc.path)
			ctx.Request.Header.SetMethod("GET")

			handler.handleHAR(ctx)

			assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
		})
	}
}

func TestHandleHAR_StorageError(t *testing.T) {
	store := &mockHARStore{
		err: errors.New("redis connection failed"),
	}

	handler := NewHARHandler(store, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/1/test-request-123")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHAR(ctx)

	assert.Equal(t, fasthttp.StatusInternalServerError, ctx.Response.StatusCode())
}

func TestHARHandler_RegisterEndpoints(t *testing.T) {
	store := &mockHARStore{}
	handler := NewHARHandler(store, zap.NewNop())
	server := NewInternalServer("test-key", zap.NewNop())

	handler.RegisterEndpoints(server)

	// Verify that the handler was registered
	assert.NotNil(t, server.routes["GET"][PathDebugHAR])
}

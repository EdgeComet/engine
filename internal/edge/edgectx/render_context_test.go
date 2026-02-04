package edgectx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

func TestRenderContext_Creation(t *testing.T) {
	requestID := "test-request-123"
	logger := zap.NewNop()
	ctx := &fasthttp.RequestCtx{}

	renderCtx := NewRenderContext(requestID, ctx, logger, 30*time.Second)

	assert.Equal(t, requestID, renderCtx.RequestID)
	assert.Equal(t, ctx, renderCtx.HTTPCtx)
	assert.NotNil(t, renderCtx.Logger)
}

func TestRenderContext_Enrichment(t *testing.T) {
	requestID := "test-request-123"
	logger := zap.NewNop()
	ctx := &fasthttp.RequestCtx{}

	renderCtx := NewRenderContext(requestID, ctx, logger, 30*time.Second)

	// Test URL enrichment
	targetURL := "https://example.com/test"
	renderCtx.WithTargetURL(targetURL)
	assert.Equal(t, targetURL, renderCtx.TargetURL)

	// Test host enrichment
	host := &types.Host{
		ID:     1,
		Domain: "example.com",
	}
	renderCtx.WithHost(host)
	assert.Equal(t, host, renderCtx.Host)

	// Test dimension enrichment
	dimension := "mobile"
	renderCtx.WithDimension(dimension)
	assert.Equal(t, dimension, renderCtx.Dimension)

	// Test cache key enrichment
	cacheKey := &types.CacheKey{
		HostID:      1,
		DimensionID: 2,
		URLHash:     "abc123",
	}
	renderCtx.WithCacheKey(cacheKey)
	assert.Equal(t, cacheKey, renderCtx.CacheKey)
}

func TestRenderContext_FluentInterface(t *testing.T) {
	requestID := "test-request-123"
	logger := zap.NewNop()
	ctx := &fasthttp.RequestCtx{}

	host := &types.Host{ID: 1, Domain: "example.com"}
	cacheKey := &types.CacheKey{HostID: 1, DimensionID: 2, URLHash: "abc123"}

	// Test fluent interface
	renderCtx := NewRenderContext(requestID, ctx, logger, 30*time.Second).
		WithTargetURL("https://example.com/test").
		WithHost(host).
		WithDimension("mobile").
		WithCacheKey(cacheKey)

	assert.Equal(t, "https://example.com/test", renderCtx.TargetURL)
	assert.Equal(t, host, renderCtx.Host)
	assert.Equal(t, "mobile", renderCtx.Dimension)
	assert.Equal(t, cacheKey, renderCtx.CacheKey)
}

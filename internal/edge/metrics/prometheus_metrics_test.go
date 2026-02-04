package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

func TestPrometheusMetrics_Recording(t *testing.T) {
	logger := zap.NewNop()
	registry := prometheus.NewRegistry()
	pm := NewPrometheusMetricsWithRegistry("edgecomet", registry, logger)

	// Test request metrics
	pm.RecordRequest("example.com", "desktop", "success", time.Millisecond*150)
	pm.RecordRequest("example.com", "mobile", "cache_hit", time.Millisecond*50)

	// Test cache metrics
	pm.RecordCacheHit("example.com", "desktop")
	pm.RecordCacheMiss("example.com", "mobile")

	// Test service metrics
	pm.RecordRenderDuration("example.com", "desktop", "service-1", time.Second*2)
	pm.RecordBypass("example.com", "no_services")

	// Test error metrics
	pm.RecordError("auth_failed", "example.com")

	// Test active requests
	pm.IncActiveRequests()
	pm.IncActiveRequests()
	pm.DecActiveRequests()

	// Test cache size
	pm.UpdateCacheSize(1024 * 1024 * 100) // 100MB

	// If we got here without panicking, metrics recording works
	assert.NotNil(t, pm)
}

func TestPrometheusMetrics_HTTPEndpoint(t *testing.T) {
	logger := zap.NewNop()
	registry := prometheus.NewRegistry()
	pm := NewPrometheusMetricsWithRegistry("edgecomet", registry, logger)

	// Record some test metrics
	pm.RecordRequest("test.com", "desktop", "success", time.Millisecond*100)
	pm.RecordCacheHit("test.com", "desktop")

	// Create a test HTTP context
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/metrics")
	ctx.Request.Header.SetMethod("GET")

	// Serve metrics
	pm.ServeHTTP(ctx)

	// Check response
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Header.Peek("Content-Type")), "text/plain")

	body := string(ctx.Response.Body())
	assert.Contains(t, body, "edgecomet_eg_requests_total")
	assert.Contains(t, body, "edgecomet_eg_cache_hits_total")
	assert.Contains(t, body, "# HELP")
	assert.Contains(t, body, "# TYPE")
}

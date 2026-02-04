package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

func TestPrometheusMetrics(t *testing.T) {
	// Use the shared test server to avoid Prometheus registry conflicts
	server := setupTestServer(t)
	metricsCollector := server.metricsCollector

	// Test basic metrics recording
	metricsCollector.RecordRequest("example.com", "desktop", "cache_hit", time.Millisecond*100)
	metricsCollector.RecordCacheHit("example.com", "desktop")
	metricsCollector.RecordCacheMiss("example.com", "desktop")
	metricsCollector.RecordBypass("example.com", "no_services")

	// Test active request tracking
	metricsCollector.IncActiveRequests()
	metricsCollector.DecActiveRequests()

	// Test error recording
	metricsCollector.RecordError("auth_failed", "example.com")

	// If we got here without panicking, the basic functionality works
	assert.NotNil(t, metricsCollector)
}

func TestResolveClientIPHeaders(t *testing.T) {
	tests := []struct {
		name            string
		hostClientIP    *types.ClientIPConfig
		globalClientIP  *types.ClientIPConfig
		expectedHeaders []string
	}{
		{
			name:            "Host-level override returns host headers",
			hostClientIP:    &types.ClientIPConfig{Headers: []string{"X-Real-IP"}},
			globalClientIP:  &types.ClientIPConfig{Headers: []string{"X-Forwarded-For"}},
			expectedHeaders: []string{"X-Real-IP"},
		},
		{
			name:            "Global fallback when host has no ClientIP",
			hostClientIP:    nil,
			globalClientIP:  &types.ClientIPConfig{Headers: []string{"X-Forwarded-For"}},
			expectedHeaders: []string{"X-Forwarded-For"},
		},
		{
			name:            "Both nil returns nil",
			hostClientIP:    nil,
			globalClientIP:  nil,
			expectedHeaders: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManager{
				config: &configtypes.EgConfig{
					ClientIP: tt.globalClientIP,
				},
			}
			s := &Server{configManager: cm}

			host := getTestHost()
			host.ClientIP = tt.hostClientIP

			result := s.resolveClientIPHeaders(host)
			assert.Equal(t, tt.expectedHeaders, result)
		})
	}
}

func TestHandleRequestErrorClientIPFallback(t *testing.T) {
	baseServer := setupTestServer(t)

	tests := []struct {
		name           string
		clientIP       string
		globalClientIP *types.ClientIPConfig
		headerName     string
		headerValue    string
		expectedIP     string
	}{
		{
			name:           "ClientIP already set - no fallback",
			clientIP:       "10.0.0.1",
			globalClientIP: &types.ClientIPConfig{Headers: []string{"X-Forwarded-For"}},
			headerName:     "X-Forwarded-For",
			headerValue:    "203.0.113.50",
			expectedIP:     "10.0.0.1",
		},
		{
			name:           "ClientIP empty - extracts from global config headers",
			clientIP:       "",
			globalClientIP: &types.ClientIPConfig{Headers: []string{"X-Forwarded-For"}},
			headerName:     "X-Forwarded-For",
			headerValue:    "203.0.113.50",
			expectedIP:     "203.0.113.50",
		},
		{
			name:           "ClientIP empty and no config - falls back to RemoteAddr",
			clientIP:       "",
			globalClientIP: nil,
			expectedIP:     "0.0.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &mockEventEmitter{}
			cm := &mockConfigManager{
				config: &configtypes.EgConfig{
					ClientIP: tt.globalClientIP,
				},
			}

			s := &Server{
				configManager:    cm,
				eventEmitter:     emitter,
				metricsCollector: baseServer.metricsCollector,
				logger:           zap.NewNop(),
			}

			ctx := &fasthttp.RequestCtx{}
			if tt.headerName != "" {
				ctx.Request.Header.Set(tt.headerName, tt.headerValue)
			}

			logger := zap.NewNop()
			renderCtx := edgectx.NewRenderContext("test-req", ctx, logger, 30*time.Second)
			renderCtx.WithHost(getTestHost())
			if tt.clientIP != "" {
				renderCtx.ClientIP = tt.clientIP
			}

			reqErr := &requestError{
				statusCode: fasthttp.StatusBadRequest,
				message:    "test error",
				category:   "test_error",
			}

			s.handleRequestError(ctx, renderCtx, fmt.Errorf("test"), reqErr, 100*time.Millisecond)

			require.Len(t, emitter.emittedEvents, 1)
			assert.Equal(t, tt.expectedIP, emitter.emittedEvents[0].ClientIP)
		})
	}
}

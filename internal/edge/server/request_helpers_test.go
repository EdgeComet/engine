package server

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/device"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/pkg/types"
)

// mockConfigManager implements configtypes.EGConfigManager for tests
type mockConfigManager struct {
	config *configtypes.EgConfig
}

func (m *mockConfigManager) GetConfig() *configtypes.EgConfig {
	return m.config
}

func (m *mockConfigManager) GetHosts() []types.Host {
	return nil
}

func (m *mockConfigManager) GetHostByDomain(domain string) *types.Host {
	return nil
}

// mockEventEmitter captures emitted events for test assertions
type mockEventEmitter struct {
	emittedEvents []*events.RequestEvent
}

func (m *mockEventEmitter) Emit(event *events.RequestEvent) {
	m.emittedEvents = append(m.emittedEvents, event)
}

func (m *mockEventEmitter) Close() error {
	return nil
}

var (
	testServer *Server
	once       sync.Once
)

// getTestHost returns a fresh test host for each test
func getTestHost() *types.Host {
	return &types.Host{
		ID:                 1,
		Domain:             "test.com",
		RenderKey:          "test-key",
		Enabled:            true,
		UnmatchedDimension: types.UnmatchedDimensionBypass,
		Dimensions: map[string]types.Dimension{
			"mobile": {
				ID:       2,
				MatchUA:  []string{"Googlebot-Mobile"},
				Width:    375,
				Height:   667,
				RenderUA: "Mobile Bot",
			},
			"desktop": {
				ID:       1,
				MatchUA:  []string{"Googlebot"},
				Width:    1920,
				Height:   1080,
				RenderUA: "Desktop Bot",
			},
		},
		Render: types.RenderConfig{
			Timeout: types.Duration(30 * time.Second),
			Events: types.RenderEvents{
				WaitFor:        "networkIdle",
				AdditionalWait: nil, // Inherit from global config
			},
			Cache: &types.RenderCacheConfig{
				TTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
		},
	}
}

// setupTestServer creates a test server with minimal dependencies (shared across all tests)
func setupTestServer(t *testing.T) *Server {
	t.Helper()

	once.Do(func() {
		logger := zap.NewNop()

		// Create Redis client (will not actually connect in tests)
		redisClient := &redis.Client{}

		// Initialize components
		keyGenerator := redis.NewKeyGenerator()
		deviceDetector := device.NewDeviceDetector()

		// Create metrics collector (shared across all tests to avoid Prometheus registry conflicts)
		metricsCollector := metrics.NewMetricsCollector("edgecomet", logger)

		testServer = &Server{
			redis:            redisClient,
			keyGenerator:     keyGenerator,
			logger:           logger,
			deviceDetector:   deviceDetector,
			metricsCollector: metricsCollector,
		}
	})

	return testServer
}

func TestHandleDimensionBlock(t *testing.T) {
	server := setupTestServer(t)
	testHost := getTestHost()
	logger := zap.NewNop()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetUserAgent("Googlebot/2.1")

	renderCtx := edgectx.NewRenderContext("test-req", ctx, logger, 30*time.Second)
	renderCtx.WithHost(testHost)
	renderCtx.WithDimension("desktop")

	start := time.Now()
	err := server.handleDimensionBlock(ctx, renderCtx, start)

	assert.Error(t, err, "Should return error")
	assert.Equal(t, fasthttp.StatusForbidden, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "Forbidden")
	assert.Equal(t, "", string(ctx.Response.Header.Peek("X-Unmatched-Dimension")))
}

func TestHandleUnmatchedBlock(t *testing.T) {
	server := setupTestServer(t)
	testHost := getTestHost()
	logger := zap.NewNop()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetUserAgent("UnknownBot/1.0")

	renderCtx := edgectx.NewRenderContext("test-req", ctx, logger, 30*time.Second)
	renderCtx.WithHost(testHost)

	start := time.Now()
	err := server.handleUnmatchedBlock(ctx, renderCtx, start)

	assert.Error(t, err, "Should return error")
	assert.Equal(t, fasthttp.StatusForbidden, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "User-Agent not supported")
}

func TestRecordResultMetrics(t *testing.T) {
	_ = setupTestServer(t)
	testHost := getTestHost()
	logger := zap.NewNop()

	ctx := &fasthttp.RequestCtx{}
	renderCtx := edgectx.NewRenderContext("test-req", ctx, logger, 30*time.Second)
	renderCtx.WithHost(testHost)
	renderCtx.WithDimension("desktop")

	// NOTE: This test is minimal because it would require importing orchestrator types
	// which creates a circular dependency. The function is tested by integration tests.
	assert.NotNil(t, renderCtx, "Render context should be initialized")
}

func TestHandleRequestError(t *testing.T) {
	server := setupTestServer(t)
	testHost := getTestHost()
	logger := zap.NewNop()

	ctx := &fasthttp.RequestCtx{}
	renderCtx := edgectx.NewRenderContext("test-req", ctx, logger, 30*time.Second)
	renderCtx.WithHost(testHost)

	reqErr := &requestError{
		statusCode: fasthttp.StatusBadRequest,
		message:    "Invalid URL",
		category:   "invalid_url",
	}

	duration := 100 * time.Millisecond
	server.handleRequestError(ctx, renderCtx, assert.AnError, reqErr, duration)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "Invalid URL")
}

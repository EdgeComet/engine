package orchestrator

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	stampRenderKey    = "sk_test_123"
	stampPartnerToken = "X-Partner-Token"
	stampPartnerValue = "partner-secret"
	stampRedacted     = "(redacted)"
	stampEdgeRender   = "edge-gateway"
	stampDimension    = "desktop"
	stampRequestTTL   = 30 * time.Second
)

// MustRegister panics on a repeated namespace, so the whole package shares one collector.
var stampMetrics = metrics.NewMetricsCollector("origin_headers_stamp_test", zap.NewNop())

// stampConfigManager supplies only the tab-selection strategy, which is all the render path
// reads off the manager.
type stampConfigManager struct{}

func (stampConfigManager) GetConfig() *configtypes.EgConfig {
	return &configtypes.EgConfig{Registry: configtypes.EdgeRegistryConfig{SelectionStrategy: "least_loaded"}}
}

func (stampConfigManager) GetHosts() []types.Host { return nil }

func (stampConfigManager) GetHostByDomain(string) *types.Host { return nil }

type stampHarness struct {
	ro       *RenderOrchestrator
	metadata *cache.MetadataStore
	mr       *miniredis.Miniredis
	redis    *customredis.Client
}

func newStampHarness(t *testing.T) *stampHarness {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)

	metadataStore := cache.NewMetadataStore(redisClient, customredis.NewKeyGenerator(), t.TempDir(), logger)
	fsCache := cache.NewFilesystemCache(logger)
	cacheService := cache.NewCacheService(metadataStore, fsCache, logger)

	ssrfOff := false
	bypassSvc := bypass.NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, logger)

	return &stampHarness{
		ro: &RenderOrchestrator{
			cacheCoord:       NewCacheCoordinator(metadataStore, fsCache, cacheService, &stubShardingManager{egID: "eg-test"}, stampMetrics, logger),
			lockCoord:        NewLockCoordinator(metadataStore, logger),
			responseWriter:   NewResponseWriter(),
			bypassSvc:        bypassSvc,
			metricsCollector: stampMetrics,
			tabSelector:      registry.NewTabSelector(redisClient, logger),
			rsClient:         rsclient.NewRSClient(logger),
			configManager:    stampConfigManager{},
			logger:           logger,
		},
		metadata: metadataStore,
		mr:       mr,
		redis:    redisClient,
	}
}

// seedRenderService registers one render service with a single free tab. The tabs hash is the
// render service's own registration side, which no exported helper writes, so the test writes the
// registry's wire format directly: key "tabs:<service id>", field per tab, empty value = free.
func (h *stampHarness) seedRenderService(t *testing.T, serviceURL string) {
	t.Helper()

	const serviceID = "rs-stamp-test"
	address, portNum := splitServerURL(t, serviceURL)
	require.NoError(t, registry.NewServiceRegistry(h.redis, zap.NewNop()).RegisterService(
		context.Background(), &registry.ServiceInfo{
			ID:       serviceID,
			Address:  address,
			Port:     portNum,
			Capacity: 1,
			Load:     0,
		}))

	h.mr.HSet("tabs:"+serviceID, "0", "")
	h.mr.SetTTL("tabs:"+serviceID, registry.RegistryTTL)
}

// storeStaleRenderEntry writes an already-expired, still-servable metadata-only render entry.
func (h *stampHarness) storeStaleRenderEntry(t *testing.T, renderCtx *edgectx.RenderContext, statusCode int) *cache.CacheMetadata {
	t.Helper()

	staleTTL := types.Duration(time.Hour)
	renderCtx.ResolvedConfig.Cache = config.ResolvedCacheConfig{
		TTL:         time.Minute,
		StatusCodes: []int{statusCode},
		Expired: types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: &staleTTL,
		},
	}

	entry := &cache.CacheMetadata{
		Key:        renderCtx.CacheKey.String(),
		URL:        renderCtx.TargetURL,
		HostID:     renderCtx.Host.ID,
		Dimension:  renderCtx.Dimension,
		CreatedAt:  time.Now().UTC().Add(-2 * time.Minute),
		ExpiresAt:  time.Now().UTC().Add(-time.Minute),
		Source:     cache.SourceRender,
		StatusCode: statusCode,
		Headers:    map[string][]string{"Location": {"https://example.com/stale"}},
		EgIDs:      []string{"eg-test"},
	}

	require.NoError(t, h.metadata.StoreMetadata(context.Background(), entry, renderCtx.CacheKey, time.Hour))
	return entry
}

func stampRenderContext(t *testing.T, targetURL string) *edgectx.RenderContext {
	t.Helper()

	httpCtx := &fasthttp.RequestCtx{}
	httpCtx.Request.Header.SetMethod("GET")
	httpCtx.Request.SetRequestURI("/page")

	renderCtx := edgectx.NewRenderContext("req-stamp", httpCtx, zap.NewNop(), stampRequestTTL)
	renderCtx.TargetURL = targetURL
	renderCtx.URLHash = 0xabc123
	renderCtx.Dimension = stampDimension
	renderCtx.Host = &types.Host{
		ID:        1,
		Domain:    "example.com",
		RenderKey: stampRenderKey,
		Dimensions: map[string]types.Dimension{
			stampDimension: {Width: 1920, Height: 1080, RenderUA: "EdgeCometTest/1.0"},
		},
	}
	renderCtx.CacheKey = &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0xabc123}
	renderCtx.ResolvedConfig = &config.ResolvedConfig{
		Compression: "none",
		Render:      config.ResolvedRenderConfig{Timeout: stampRequestTTL},
	}

	return renderCtx
}

// storeStaleBypassEntry writes a metadata-only bypass entry that is already expired but still
// inside its stale window, and configures the context to serve it.
func (h *stampHarness) storeStaleBypassEntry(t *testing.T, renderCtx *edgectx.RenderContext, statusCode int) {
	t.Helper()

	staleTTL := types.Duration(time.Hour)
	renderCtx.ResolvedConfig.Bypass.Cache = config.ResolvedBypassCacheConfig{
		Enabled:     true,
		TTL:         time.Minute,
		StatusCodes: []int{statusCode},
		Expired: types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: &staleTTL,
		},
	}

	entry := &cache.CacheMetadata{
		Key:        renderCtx.CacheKey.String(),
		URL:        renderCtx.TargetURL,
		HostID:     renderCtx.Host.ID,
		Dimension:  renderCtx.Dimension,
		CreatedAt:  time.Now().UTC().Add(-2 * time.Minute),
		ExpiresAt:  time.Now().UTC().Add(-time.Minute),
		Source:     cache.SourceBypass,
		StatusCode: statusCode,
		Headers:    map[string][]string{"Location": {"https://example.com/stale"}},
		EgIDs:      []string{"eg-test"},
	}

	require.NoError(t, h.metadata.StoreMetadata(context.Background(), entry, renderCtx.CacheKey, time.Hour))
}

// stampOrigin starts a listener that records what it received and answers with the given status
// and a marker header.
func stampOrigin(t *testing.T, statusCode int) (url string, received func() http.Header) {
	t.Helper()

	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("X-Origin-Marker", "origin-"+strconv.Itoa(statusCode))
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte("<html><body>origin</body></html>"))
	}))
	t.Cleanup(server.Close)

	return server.URL, func() http.Header { return got }
}

// stampRenderService answers /render with the supplied response.
func stampRenderService(t *testing.T, response types.RenderResponse) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func reservationFor(t *testing.T, serverURL string) *registry.TabReservation {
	t.Helper()

	address, portNum := splitServerURL(t, serverURL)
	return &registry.TabReservation{ServiceID: "rs-test", TabID: 1, Address: address, Port: portNum}
}

func splitServerURL(t *testing.T, serverURL string) (address string, port int) {
	t.Helper()

	host, portStr, err := net.SplitHostPort(serverURL[len("http://"):])
	require.NoError(t, err)
	portNum, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return host, portNum
}

func TestServeBypassStampsBothOriginHops(t *testing.T) {
	h := newStampHarness(t)
	originURL, received := stampOrigin(t, http.StatusOK)
	renderCtx := stampRenderContext(t, originURL)
	renderCtx.ClientHeaders = map[string][]string{"Authorization": {"Bearer token"}}

	result, err := h.ro.serveBypass(renderCtx, "test")
	require.NoError(t, err)
	require.Equal(t, ServedFromBypass, result.Source)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"Bearer token"}, renderCtx.OriginRequestHeaders["Authorization"])
	assert.Equal(t, []string{"origin-200"}, renderCtx.OriginResponseHeaders["X-Origin-Marker"])
	assert.Equal(t, stampRenderKey, received().Get(types.HeaderRenderKey))
}

// The origin was never reached, so there is no response to record - but the request field still
// says what the EG had prepared.
func TestServeBypassStampsRequestHeadersOnTransportFailure(t *testing.T) {
	h := newStampHarness(t)
	// The default dialer refuses this port, which is a transport failure without depending on
	// network reachability.
	renderCtx := stampRenderContext(t, "http://127.0.0.1:1/page")

	result, err := h.ro.serveBypass(renderCtx, "test")
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, result.StatusCode)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Empty(t, renderCtx.OriginResponseHeaders)
}

// A row served from stale bypass cache keeps the refetch that made it fall back: that is the
// evidence of why stale was served.
func TestServeBypassStaleFallbackKeepsFailedRefetchHops(t *testing.T) {
	h := newStampHarness(t)
	originURL, _ := stampOrigin(t, http.StatusInternalServerError)
	renderCtx := stampRenderContext(t, originURL)
	h.storeStaleBypassEntry(t, renderCtx, http.StatusMovedPermanently)

	result, err := h.ro.serveBypass(renderCtx, "test")
	require.NoError(t, err)
	require.Equal(t, ServedFromBypassCache, result.Source, "the stale entry must have been served")
	require.Equal(t, http.StatusMovedPermanently, result.StatusCode)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-500"}, renderCtx.OriginResponseHeaders["X-Origin-Marker"],
		"the response headers describe the failed refetch, not the cache entry")
}

// The configured value is a credential. It has to reach the origin and must not reach the event.
func TestServeBypassRedactsConfiguredHeaderValues(t *testing.T) {
	h := newStampHarness(t)
	originURL, received := stampOrigin(t, http.StatusOK)
	renderCtx := stampRenderContext(t, originURL)
	renderCtx.ResolvedConfig.RequestHeadersSet = map[string]string{stampPartnerToken: stampPartnerValue}
	renderCtx.ClientHeaders = renderCtx.ResolvedConfig.ApplyRequestHeaders(
		map[string][]string{"Authorization": {"Bearer token"}})

	_, err := h.ro.serveBypass(renderCtx, "test")
	require.NoError(t, err)

	assert.Equal(t, stampPartnerValue, received().Get(stampPartnerToken), "the origin must get the real value")
	assert.Equal(t, []string{stampRedacted}, renderCtx.OriginRequestHeaders[stampPartnerToken])
	assert.Equal(t, []string{"Bearer token"}, renderCtx.OriginRequestHeaders["Authorization"])
	assert.Equal(t, []string{stampPartnerValue}, renderCtx.ClientHeaders[stampPartnerToken],
		"redaction must not reach the map the request is built from")
}

func TestPerformActualRenderWithTabStampsBothOriginHops(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	renderCtx.ClientHeaders = map[string][]string{"Authorization": {"Bearer token"}}
	reservation := reservationFor(t, stampRenderService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><body>rendered</body></html>",
		Headers: map[string][]string{"content-type": {"text/html"}},
		Metrics: types.PageMetrics{StatusCode: 200},
	}))

	result, err := h.ro.performActualRenderWithTab(renderCtx, reservation)
	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"Bearer token"}, renderCtx.OriginRequestHeaders["Authorization"])
	assert.Equal(t, []string{"text/html"}, renderCtx.OriginResponseHeaders["content-type"])
}

func TestPerformActualRenderWithTabRedactsConfiguredHeaderValues(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	renderCtx.ResolvedConfig.RequestHeadersSet = map[string]string{stampPartnerToken: stampPartnerValue}
	renderCtx.ClientHeaders = renderCtx.ResolvedConfig.ApplyRequestHeaders(nil)
	reservation := reservationFor(t, stampRenderService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><body>rendered</body></html>",
		Metrics: types.PageMetrics{StatusCode: 200},
	}))

	_, err := h.ro.performActualRenderWithTab(renderCtx, reservation)
	require.NoError(t, err)

	assert.Equal(t, []string{stampRedacted}, renderCtx.OriginRequestHeaders[stampPartnerToken])
	assert.Equal(t, []string{stampPartnerValue}, renderCtx.ClientHeaders[stampPartnerToken],
		"redaction must not reach the map the render request is built from")
}

// Chrome may or may not have reached the origin; the maps cannot know. The request field says
// what was attempted, the response field stays empty.
func TestPerformActualRenderWithTabStampsRequestHeadersOnRPCFailure(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	renderCtx.OriginResponseHeaders = map[string][]string{"stale": {"from an earlier attempt"}}

	// A listener that is closed before the call: the RPC fails at the transport.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	reservation := reservationFor(t, dead.URL)
	dead.Close()

	_, err := h.ro.performActualRenderWithTab(renderCtx, reservation)
	require.Error(t, err)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Nil(t, renderCtx.OriginResponseHeaders, "the pre-call stamp must clear an earlier attempt")
}

// Stamped before validation, so a response the validator rejects still leaves its evidence for
// the stale-cache row that follows.
func TestPerformActualRenderWithTabStampsResponseHeadersBeforeValidation(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	reservation := reservationFor(t, stampRenderService(t, types.RenderResponse{
		Success:   false,
		Error:     "navigation failed",
		ErrorType: types.ErrorTypeHardTimeout,
		Headers:   map[string][]string{"x-origin-marker": {"origin-500"}},
		Metrics:   types.PageMetrics{StatusCode: 500},
	}))

	_, err := h.ro.performActualRenderWithTab(renderCtx, reservation)
	require.Error(t, err)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-500"}, renderCtx.OriginResponseHeaders["x-origin-marker"])
}

// The fallback chain writes both fields at each attempt, so the row describes the bypass alone
// and never a mix of the two.
func TestRenderFailureFollowedByBypassKeepsOnlyBypassHops(t *testing.T) {
	h := newStampHarness(t)
	originURL, _ := stampOrigin(t, http.StatusOK)
	renderCtx := stampRenderContext(t, originURL)
	reservation := reservationFor(t, stampRenderService(t, types.RenderResponse{
		Success:   false,
		Error:     "navigation failed",
		ErrorType: types.ErrorTypeHardTimeout,
		Headers:   map[string][]string{"x-render-attempt": {"1"}},
		Metrics:   types.PageMetrics{StatusCode: 500},
	}))

	_, err := h.ro.performActualRenderWithTab(renderCtx, reservation)
	require.Error(t, err)
	require.Contains(t, renderCtx.OriginResponseHeaders, "x-render-attempt")

	result, err := h.ro.serveBypass(renderCtx, "render_failed")
	require.NoError(t, err)
	require.Equal(t, ServedFromBypass, result.Source)

	assert.Equal(t, []string{"origin-200"}, renderCtx.OriginResponseHeaders["X-Origin-Marker"])
	assert.NotContains(t, renderCtx.OriginResponseHeaders, "x-render-attempt")
	// X-Edge-Render is set by the bypass fetch only, never by the render injection.
	assert.Equal(t, []string{stampEdgeRender}, renderCtx.OriginRequestHeaders[types.HeaderEdgeRender])
	assert.Equal(t, []string{"EdgeCometTest/1.0"}, renderCtx.OriginRequestHeaders["User-Agent"])
}

// A status action serves from configuration without an upstream attempt.
func TestServeStatusActionRecordsNoOriginHops(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	renderCtx.ResolvedConfig.Action = types.ActionStatus
	renderCtx.ResolvedConfig.Status = config.ResolvedStatusConfig{
		Code:    http.StatusMovedPermanently,
		Headers: map[string]string{"Location": "https://example.com/new"},
	}

	result, err := h.ro.ServeStatusAction(renderCtx)
	require.NoError(t, err)
	require.Equal(t, ServedFromBypass, result.Source)

	assert.Nil(t, renderCtx.OriginRequestHeaders)
	assert.Nil(t, renderCtx.OriginResponseHeaders)
	assert.Equal(t, "https://example.com/new", string(renderCtx.HTTPCtx.Response.Header.Peek("Location")))
}

// A render that completes with 5xx falls back to stale render cache. That cache_hit row keeps the
// failed render's hops: evidence of why stale was served.
func TestRenderStaleFallbackKeepsFailedRenderHops(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	staleCache := h.storeStaleRenderEntry(t, renderCtx, http.StatusMovedPermanently)
	h.seedRenderService(t, stampRenderService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><body>error page</body></html>",
		Headers: map[string][]string{"x-origin-marker": {"origin-503"}},
		Metrics: types.PageMetrics{StatusCode: http.StatusServiceUnavailable},
	}))

	result, err := h.ro.executeRenderWithExplicitServing(renderCtx, staleCache)
	require.NoError(t, err)
	require.Equal(t, ServedFromCache, result.Source)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-503"}, renderCtx.OriginResponseHeaders["x-origin-marker"])
}

// A render RPC that fails outright records what was attempted; nothing came back to record.
func TestRenderRPCFailureWithStaleCacheKeepsRequestHeadersOnly(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	staleCache := h.storeStaleRenderEntry(t, renderCtx, http.StatusMovedPermanently)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	h.seedRenderService(t, deadURL)

	result, err := h.ro.executeRenderWithExplicitServing(renderCtx, staleCache)
	require.NoError(t, err)
	require.Equal(t, ServedFromCache, result.Source)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Nil(t, renderCtx.OriginResponseHeaders)
}

// The hook answers before a tab is reserved, so the render row names no service and records no
// upstream hops.
func TestHookAnsweredRenderRecordsNoOriginHops(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/page")
	renderCtx.ResolvedConfig.Cache = config.ResolvedCacheConfig{TTL: time.Hour, StatusCodes: []int{404}}
	h.ro.preRenderHook = func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
		return &PreRenderDecision{Handled: true, StatusCode: http.StatusNotFound}, nil
	}
	h.seedRenderService(t, stampRenderService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><body>rendered</body></html>",
		Metrics: types.PageMetrics{StatusCode: 200},
	}))

	result, err := h.ro.executeRenderWithExplicitServing(renderCtx, nil)
	require.NoError(t, err)

	assert.Equal(t, ServedFromRender, result.Source)
	assert.Empty(t, result.ServiceID, "a hook-answered row names no render service")
	assert.Nil(t, renderCtx.OriginRequestHeaders)
	assert.Nil(t, renderCtx.OriginResponseHeaders)
}

// The redirect headers captured by the renderer reach the cache writer, where they collide with
// the Location the render resolved. One spelling has to win before storage: two keys differing
// only in case make every case-insensitive reader of the entry pick at random, and the origin's
// value is frequently relative where the resolved one is absolute.
func TestRenderedRedirectStoresSingleAbsoluteLocation(t *testing.T) {
	h := newStampHarness(t)
	renderCtx := stampRenderContext(t, "https://example.com/old")
	renderCtx.ResolvedConfig.Cache = config.ResolvedCacheConfig{
		TTL:         time.Hour,
		StatusCodes: []int{http.StatusFound},
	}
	renderCtx.ResolvedConfig.SafeResponseHeaders = []string{"Location", "Content-Type"}
	h.seedRenderService(t, stampRenderService(t, types.RenderResponse{
		Success: true,
		Headers: map[string][]string{"location": {"/new"}},
		Metrics: types.PageMetrics{
			StatusCode: http.StatusFound,
			FinalURL:   "https://example.com/new",
		},
	}))

	result, err := h.ro.executeRenderWithExplicitServing(renderCtx, nil)
	require.NoError(t, err)
	require.Equal(t, ServedFromRender, result.Source)

	assert.Equal(t, []string{"/new"}, renderCtx.OriginResponseHeaders["location"],
		"the event records the redirect exactly as the origin sent it")

	entry, err := h.metadata.GetCacheEntry(context.Background(), renderCtx.CacheKey)
	require.NoError(t, err)

	var spellings []string
	for name := range entry.Headers {
		if strings.EqualFold(name, "Location") {
			spellings = append(spellings, name)
		}
	}
	require.Len(t, spellings, 1, "stored entry must hold one Location, got %v", entry.Headers)
	assert.Equal(t, []string{"https://example.com/new"}, entry.Headers[spellings[0]])
}

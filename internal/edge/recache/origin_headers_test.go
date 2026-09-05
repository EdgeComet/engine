package recache

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	stampRenderKey     = "sk_test_123"
	stampPartnerToken  = "X-Partner-Token"
	stampPartnerValue  = "partner-secret"
	stampRedacted      = "(redacted)"
	stampDimension     = "desktop"
	stampOriginMarker  = "X-Origin-Marker"
	stampRenderTimeout = 30 * time.Second
)

// stampOrigin answers with a marker header so captured response headers are attributable to this fetch.
func stampOrigin(t *testing.T, statusCode int) (url string, received func() http.Header) {
	t.Helper()

	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set(stampOriginMarker, "origin-"+strconv.Itoa(statusCode))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte("<html><head><title>Page</title></head><body>origin</body></html>"))
	}))
	t.Cleanup(server.Close)

	return server.URL, func() http.Header { return got }
}

func stampBypassService() *bypass.BypassService {
	// httptest listens on loopback, which the SSRF-safe dialer refuses by design.
	ssrfOff := false
	return bypass.NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())
}

func TestProcessBypassRecacheStampsBothOriginHops(t *testing.T) {
	originURL, received := stampOrigin(t, http.StatusOK)
	rs, emitter := successPathService(t)
	rs.bypassSvc = stampBypassService()

	renderCtx := cacheableNon200Context(originURL + "/page")
	renderCtx.ResolvedConfig.Action = types.ActionBypass
	renderCtx.Host.RenderKey = stampRenderKey
	renderCtx.ClientHeaders = renderCtx.ResolvedConfig.ApplyRequestHeaders(nil)

	require.NoError(t, rs.processBypassRecache(context.Background(), renderCtx.TargetURL, renderCtx, time.Now()))

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-200"}, renderCtx.OriginResponseHeaders[stampOriginMarker])
	assert.Equal(t, stampRenderKey, received().Get(types.HeaderRenderKey))

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, []string{stampRenderKey}, event.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-200"}, event.OriginResponseHeaders[stampOriginMarker])
	assert.Nil(t, event.RequestHeaders, "precache has no client request")
}

// The failure row is built from the attempt's context, so an unreachable origin still files what
// the daemon prepared.
func TestProcessBypassRecacheStampsRequestHeadersOnTransportFailure(t *testing.T) {
	rs, emitter := successPathService(t)
	rs.bypassSvc = stampBypassService()

	renderCtx := cacheableNon200Context("http://127.0.0.1:1/page")
	renderCtx.ResolvedConfig.Action = types.ActionBypass
	renderCtx.Host.RenderKey = stampRenderKey

	err := rs.processBypassRecache(context.Background(), renderCtx.TargetURL, renderCtx, time.Now())
	require.Error(t, err)

	assert.Equal(t, []string{stampRenderKey}, renderCtx.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Empty(t, renderCtx.OriginResponseHeaders)

	// ProcessRecache owns the failure emission; drive it the way the deferred emitter does.
	rs.emitPrecacheFailure(&precacheAttempt{
		url:       renderCtx.TargetURL,
		host:      renderCtx.Host,
		dimension: renderCtx.Dimension,
		startTime: time.Now(),
		renderCtx: renderCtx,
	}, classifiedFailure(err))

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, []string{stampRenderKey}, event.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Empty(t, event.OriginResponseHeaders)
}

func TestProcessBypassRecacheRedactsConfiguredHeaderValues(t *testing.T) {
	originURL, received := stampOrigin(t, http.StatusOK)
	rs, _ := successPathService(t)
	rs.bypassSvc = stampBypassService()

	renderCtx := cacheableNon200Context(originURL + "/page")
	renderCtx.ResolvedConfig.Action = types.ActionBypass
	renderCtx.ResolvedConfig.RequestHeadersSet = map[string]string{stampPartnerToken: stampPartnerValue}
	renderCtx.ClientHeaders = renderCtx.ResolvedConfig.ApplyRequestHeaders(nil)

	require.NoError(t, rs.processBypassRecache(context.Background(), renderCtx.TargetURL, renderCtx, time.Now()))

	assert.Equal(t, stampPartnerValue, received().Get(stampPartnerToken))
	assert.Equal(t, []string{stampRedacted}, renderCtx.OriginRequestHeaders[stampPartnerToken])
	assert.Equal(t, []string{stampPartnerValue}, renderCtx.ClientHeaders[stampPartnerToken])
}

// stampConfigManager resolves a host that renders and caches, which the empty EgConfig the other
// recache tests use cannot do.
type stampConfigManager struct {
	hosts []types.Host
}

func (m *stampConfigManager) GetConfig() *configtypes.EgConfig {
	return &configtypes.EgConfig{
		Render:   configtypes.GlobalRenderConfig{Cache: types.RenderCacheConfig{TTL: ptrDuration(time.Hour)}},
		Bypass:   configtypes.GlobalBypassConfig{Timeout: ptrDuration(stampRenderTimeout)},
		Registry: configtypes.EdgeRegistryConfig{SelectionStrategy: "least_loaded"},
	}
}

func (m *stampConfigManager) GetHosts() []types.Host { return m.hosts }

func (m *stampConfigManager) GetHostByDomain(string) *types.Host { return nil }

func ptrDuration(d time.Duration) *types.Duration {
	td := types.Duration(d)
	return &td
}

// seedRenderService registers one render service with a single free tab. The tabs hash is the
// render service's own registration side, which no exported helper writes, so the test writes the
// registry's wire format directly: key "tabs:<service id>", field per tab, empty value = free.
func seedRenderService(t *testing.T, mr *miniredis.Miniredis, redisClient *customredis.Client, serviceURL string) {
	t.Helper()

	host, port, err := net.SplitHostPort(serviceURL[len("http://"):])
	require.NoError(t, err)
	portNum, err := strconv.Atoi(port)
	require.NoError(t, err)

	const serviceID = "rs-stamp-test"
	require.NoError(t, registry.NewServiceRegistry(redisClient, zap.NewNop()).RegisterService(
		context.Background(), &registry.ServiceInfo{
			ID:       serviceID,
			Address:  host,
			Port:     portNum,
			Capacity: 1,
			Load:     0,
		}))

	mr.HSet("tabs:"+serviceID, "0", "")
	mr.SetTTL("tabs:"+serviceID, registry.RegistryTTL)
}

// stampRenderRecacheService wires the render half of a recache over miniredis, a seeded registry
// and a fake render service.
func stampRenderRecacheService(t *testing.T, response types.RenderResponse) (*RecacheService, *captureEmitter, func() types.RenderRequest) {
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

	var renderReq types.RenderRequest
	renderService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&renderReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(renderService.Close)

	seedRenderService(t, mr, redisClient, renderService.URL)

	emitter := &captureEmitter{}
	rs := &RecacheService{
		configManager: &stampConfigManager{hosts: []types.Host{{
			ID:         1,
			Domain:     "example.com",
			Domains:    []string{"example.com"},
			RenderKey:  stampRenderKey,
			Render:     types.RenderConfig{Timeout: types.Duration(stampRenderTimeout)},
			Dimensions: map[string]types.Dimension{stampDimension: {ID: 0, Width: 1920, Height: 1080}},
		}}},
		cacheCoord:    orchestrator.NewCacheCoordinator(metadataStore, fsCache, cacheService, localShardingManager{}, nil, logger),
		bypassSvc:     stampBypassService(),
		tabSelector:   registry.NewTabSelector(redisClient, logger),
		rsClient:      rsclient.NewRSClient(logger),
		metadataStore: metadataStore,
		eventEmitter:  emitter,
		instanceID:    testEGInstanceID,
		logger:        logger,
	}

	return rs, emitter, func() types.RenderRequest { return renderReq }
}

func TestProcessRecacheRenderStampsBothOriginHops(t *testing.T) {
	rs, emitter, sentRequest := stampRenderRecacheService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><head><title>Page</title></head><body>rendered</body></html>",
		Headers: map[string][]string{"x-origin-marker": {"origin-200"}},
		Metrics: types.PageMetrics{StatusCode: 200},
	})

	err := rs.ProcessRecache(context.Background(), "https://example.com/page", 1, 0, types.RecacheModeRender)
	require.NoError(t, err)

	require.Equal(t, stampRenderKey, sentRequest().RenderKey, "the render service was actually called")
	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, []string{stampRenderKey}, event.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{"origin-200"}, event.OriginResponseHeaders["x-origin-marker"])
	assert.Nil(t, event.RequestHeaders, "precache has no client request")
}

// A render RPC that never answers records what was attempted and nothing else.
func TestProcessRecacheRenderStampsRequestHeadersOnRPCFailure(t *testing.T) {
	rs, emitter, _ := stampRenderRecacheService(t, types.RenderResponse{})
	rs.rsClient = rsclient.NewRSClient(zap.NewNop())

	// Point the reservation at a service that is registered but not listening.
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, zap.NewNop())
	require.NoError(t, err)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	seedRenderService(t, mr, redisClient, deadURL)
	rs.tabSelector = registry.NewTabSelector(redisClient, zap.NewNop())

	err = rs.ProcessRecache(context.Background(), "https://example.com/page", 1, 0, types.RecacheModeRender)
	require.Error(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, []string{stampRenderKey}, event.OriginRequestHeaders[types.HeaderRenderKey])
	assert.Empty(t, event.OriginResponseHeaders)
}

// A tab reservation that never succeeds means the render call was never prepared, so the row
// records nothing on either hop.
func TestProcessRecacheRenderUnavailableRecordsNoOriginHops(t *testing.T) {
	rs, emitter, _ := stampRenderRecacheService(t, types.RenderResponse{})

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, zap.NewNop())
	require.NoError(t, err)
	rs.tabSelector = registry.NewTabSelector(redisClient, zap.NewNop())

	err = rs.ProcessRecache(context.Background(), "https://example.com/page", 1, 0, types.RecacheModeRender)
	require.Error(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, types.ErrorTypeRenderUnavailable, event.ErrorType)
	assert.Nil(t, event.OriginRequestHeaders)
	assert.Nil(t, event.OriginResponseHeaders)
}

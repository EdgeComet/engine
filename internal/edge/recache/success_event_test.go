package recache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/sharding"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	testEGInstanceID = "eg-test-1"
	testRenderSvcID  = "rs-test-1"
	testCacheableTTL = time.Hour
)

// localShardingManager stands in for the cluster. Sharding is off, so nothing is pushed, but
// SaveCache still stamps the writing EG onto the metadata and needs an ID to stamp.
type localShardingManager struct{}

func (localShardingManager) IsEnabled() bool { return false }

func (localShardingManager) ComputeTargets(context.Context, *types.CacheKey) ([]string, error) {
	return nil, nil
}

func (localShardingManager) IsTargetForCache(context.Context, *types.CacheKey) (bool, error) {
	return true, nil
}

func (localShardingManager) PushToTargets(context.Context, *types.CacheKey, []byte, *cache.CacheMetadata, []string, string) ([]string, error) {
	return nil, nil
}

func (localShardingManager) PullFromRemote(context.Context, *types.CacheKey, []string) ([]byte, error) {
	return nil, nil
}

func (localShardingManager) GetEgID() string { return testEGInstanceID }

func (localShardingManager) GetReplicationFactor() int { return 1 }

func (localShardingManager) GetInterEgTimeout() time.Duration { return time.Second }

func (localShardingManager) GetHealthyEGs(context.Context) ([]sharding.EGInfo, error) {
	return nil, nil
}

// successPathService wires the real cache write path over miniredis and a temp cache dir, so the
// success rows under test are the ones the production code emits rather than ones a mock invents.
func successPathService(t *testing.T) (*RecacheService, *captureEmitter) {
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

	emitter := &captureEmitter{}
	rs := &RecacheService{
		cacheCoord:    orchestrator.NewCacheCoordinator(metadataStore, fsCache, cacheService, localShardingManager{}, nil, logger),
		metadataStore: metadataStore,
		eventEmitter:  emitter,
		instanceID:    testEGInstanceID,
		logger:        logger,
	}

	return rs, emitter
}

// cacheableNon200Context resolves a host that caches 404s, the configuration whose refreshes the
// old flat non-200 check made impossible.
func cacheableNon200Context(targetURL string) *edgectx.RenderContext {
	host := &types.Host{ID: 1, Domain: "example.com", Domains: []string{"example.com"}}

	return &edgectx.RenderContext{
		TargetURL:   targetURL,
		OriginalURL: targetURL,
		URLHash:     4242,
		Host:        host,
		Dimension:   "desktop",
		CacheKey:    &types.CacheKey{HostID: host.ID, DimensionID: 0, URLHash: 4242},
		RequestID:   "recache-1-0-test",
		Logger:      zap.NewNop(),
		IsPrecache:  true,
		ResolvedConfig: &config.ResolvedConfig{
			Compression: types.CompressionNone,
			Cache: config.ResolvedCacheConfig{
				TTL:         testCacheableTTL,
				StatusCodes: []int{200, 404},
			},
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Enabled:     true,
					TTL:         testCacheableTTL,
					StatusCodes: []int{200, 404},
				},
			},
		},
	}
}

// A configured cacheable 404 is a refresh, and an empty error_type is exactly what a dashboard
// counts as one. buildRenderResult now carries the render service's error type, so the success row
// has to drop it: copying it through would file every refreshed 404 as a failed attempt and break
// the counting rule for every consumer at once.
func TestSaveToCache_CacheableNon200EmitsSuccessRow(t *testing.T) {
	rs, emitter := successPathService(t)
	renderCtx := cacheableNon200Context("https://example.com/gone")

	renderResult := &orchestrator.RenderServiceResult{
		HTML:         []byte("<html><head><title>Gone</title></head><body>gone</body></html>"),
		StatusCode:   404,
		ChromeID:     "chrome-1",
		ErrorType:    types.ErrorTypeOrigin4xx,
		ErrorMessage: "Origin returned 404",
	}

	err := rs.saveToCache(context.Background(), renderCtx, renderResult, nil, nil, nil, nil, testRenderSvcID, time.Second)
	require.NoError(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, events.EventTypePrecache, event.EventType)
	assert.Equal(t, events.SourceRender, event.Source)
	assert.Empty(t, event.ErrorType, "error_type is the success discriminator and must stay empty on a refresh")
	assert.Empty(t, event.ErrorMessage)
	assert.Equal(t, 404, event.StatusCode, "the status the page was cached with belongs on the row")
	assert.Equal(t, 1, event.HostID)
	assert.Equal(t, testRenderSvcID, event.RenderServiceID)
}

// Same rule on the bypass half, driven through the real origin fetch: a cacheable 404 fetched from
// origin is a refresh, not an origin_4xx.
func TestProcessBypassRecache_CacheableNon200EmitsSuccessRow(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><head><title>Not found</title></head><body>missing</body></html>"))
	}))
	defer origin.Close()

	rs, emitter := successPathService(t)

	// httptest listens on loopback, which the SSRF-safe dialer refuses by design.
	ssrfProtection := false
	rs.bypassSvc = bypass.NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfProtection,
	}, zap.NewNop())

	targetURL := origin.URL + "/gone"
	renderCtx := cacheableNon200Context(targetURL)
	renderCtx.ResolvedConfig.Action = types.ActionBypass

	err := rs.processBypassRecache(context.Background(), targetURL, renderCtx, time.Now())
	require.NoError(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, events.EventTypePrecache, event.EventType)
	assert.Equal(t, events.SourceBypass, event.Source)
	assert.Empty(t, event.ErrorType, "a cacheable origin status is a refresh, not an origin failure")
	assert.Empty(t, event.ErrorMessage)
	assert.Equal(t, 404, event.StatusCode)
	assert.Equal(t, 1, event.HostID)
}

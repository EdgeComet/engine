package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

const redirectCacheTarget = "https://example.com/new-page"

// redirectOrigin answers every request with a 301 carrying nginx's default body and counts the
// requests it receives.
func redirectOrigin(t *testing.T) (url string, hits *atomic.Int32) {
	t.Helper()

	hits = &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Location", redirectCacheTarget)
		w.WriteHeader(http.StatusMovedPermanently)
		_, _ = w.Write([]byte(nginxMovedPermanentlyBody))
	}))
	t.Cleanup(server.Close)

	return server.URL, hits
}

// redirectCacheContext is a fresh request for the same cache key, with bypass caching on for 301.
func redirectCacheContext(t *testing.T, originURL string) *edgectx.RenderContext {
	t.Helper()

	staleTTL := types.Duration(time.Hour)
	renderCtx := stampRenderContext(t, originURL)
	renderCtx.ResolvedConfig.Bypass.Cache = config.ResolvedBypassCacheConfig{
		Enabled:     true,
		TTL:         time.Hour,
		StatusCodes: []int{http.StatusMovedPermanently},
		Expired: types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: &staleTTL,
		},
	}
	return renderCtx
}

func TestServeBypassServesBodiedRedirectFromCache(t *testing.T) {
	h := newStampHarness(t)
	originURL, hits := redirectOrigin(t)

	first, err := h.ro.serveBypass(redirectCacheContext(t, originURL), "test")
	require.NoError(t, err)
	require.Equal(t, ServedFromBypass, first.Source)

	renderCtx := redirectCacheContext(t, originURL)
	second, err := h.ro.serveBypass(renderCtx, "test")
	require.NoError(t, err)

	assert.Equal(t, ServedFromBypassCache, second.Source)
	assert.Equal(t, int32(1), hits.Load(), "the cached redirect must not be refetched")
	assert.Equal(t, http.StatusMovedPermanently, renderCtx.HTTPCtx.Response.StatusCode())
	assert.Equal(t, redirectCacheTarget, string(renderCtx.HTTPCtx.Response.Header.Peek("Location")))
	assert.Empty(t, renderCtx.HTTPCtx.Response.Body())
}

// The stale fallback is what an origin outage leaves; it must be able to serve the entry the
// save path wrote.
func TestServeStaleBypassCacheServesBodiedRedirect(t *testing.T) {
	h := newStampHarness(t)
	originURL, _ := redirectOrigin(t)

	_, err := h.ro.serveBypass(redirectCacheContext(t, originURL), "test")
	require.NoError(t, err)

	renderCtx := redirectCacheContext(t, originURL)
	entry, exists := h.ro.cacheCoord.LookupCacheRaw(renderCtx)
	require.True(t, exists)

	result, err := h.ro.serveStaleBypassCache(renderCtx, entry, "origin_error")
	require.NoError(t, err)
	assert.Equal(t, ServedFromBypassCache, result.Source)
	assert.Equal(t, http.StatusMovedPermanently, result.StatusCode)
	assert.Equal(t, redirectCacheTarget, string(renderCtx.HTTPCtx.Response.Header.Peek("Location")))
}

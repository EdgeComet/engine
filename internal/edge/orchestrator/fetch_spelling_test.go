package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	spellingEncoded  = "https://example.com/a%2Cb/53"
	spellingComma    = "https://example.com/a,b/53"
	spellingLower    = "https://example.com/a%2cb/53"
	spellingWriterID = "writer-req"
	spellingDomain   = "example.com"
	spellingMetric   = `fetch_spelling_test_eg_cached_redirect_ignored_total{host="example.com"} 1`
)

var spellingMetrics = metrics.NewMetricsCollector("fetch_spelling_test", zap.NewNop())

// spellingRequestCtx is a request for fetchURL as server.go builds it.
func spellingRequestCtx(t *testing.T, fetchURL string, exact, strip bool) *edgectx.RenderContext {
	t.Helper()

	var trackingParams *config.ResolvedTrackingParams
	if strip {
		patterns, err := config.CompileStripPatterns([]string{"utm_*"})
		require.NoError(t, err)
		trackingParams = &config.ResolvedTrackingParams{Enabled: true, CompiledPatterns: patterns}
	}

	normalizer := hash.NewURLNormalizer()
	result, err := normalizer.Normalize(fetchURL, trackingParams.StripPatterns())
	require.NoError(t, err)
	urlHash := normalizer.Hash(result.NormalizedURL)

	renderCtx := overrideRenderCtx(&types.CacheKey{HostID: 1, DimensionID: 1, URLHash: urlHash})
	renderCtx.TargetURL = result.NormalizedURL
	renderCtx.URLHash = urlHash
	renderCtx.Host.Domain = spellingDomain
	renderCtx.ResolvedConfig.TrackingParams = trackingParams
	renderCtx.WithFetchURL(fetchURL, exact)
	return renderCtx
}

func spellingEntry(status int, location, fetchURL, fetchURLRequestID string) *cache.CacheMetadata {
	entry := &cache.CacheMetadata{
		RequestID:         spellingWriterID,
		StatusCode:        status,
		FetchURL:          fetchURL,
		FetchURLRequestID: fetchURLRequestID,
	}
	if location != "" {
		entry.Headers = map[string][]string{headerLocation: {location}}
	}
	return entry
}

// spellingSavedEntry is a redirect to spellingEncoded saved by a request for entryURL.
func spellingSavedEntry(entryURL, fetchURLRequestID string) *cache.CacheMetadata {
	entry := spellingEntry(http.StatusMovedPermanently, spellingEncoded, "", fetchURLRequestID)
	entry.URL = entryURL
	return entry
}

func TestIgnoreCachedRedirect(t *testing.T) {
	tests := []struct {
		name       string
		requestURL string
		legacy     bool
		strip      bool
		entry      *cache.CacheMetadata
		want       bool
	}{
		{name: "absolute Location to the own key", entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, "", ""), want: true},
		{name: "comma Location to the own key", entry: spellingEntry(http.StatusMovedPermanently, spellingComma, "", ""), want: true},
		{name: "relative path Location", entry: spellingEntry(http.StatusFound, "/a%2Cb/53", "", ""), want: true},
		{name: "relative segment Location", entry: spellingEntry(http.StatusTemporaryRedirect, "53", "", ""), want: true},
		{name: "fragment is ignored", entry: spellingEntry(http.StatusPermanentRedirect, spellingEncoded+"#top", "", ""), want: true},
		{name: "Location to another path", entry: spellingEntry(http.StatusMovedPermanently, "/a%2Cb/54", "", "")},
		{name: "Location to another host", entry: spellingEntry(http.StatusMovedPermanently, "https://other.com/a%2Cb/53", "", "")},
		{
			name: "http to https is another key", requestURL: "http://example.com/a%2Cb/53",
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, "", ""),
		},
		{
			name: "tracking-param-only difference with stripping", strip: true,
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded+"?utm_source=x", "", ""), want: true,
		},
		{
			name:  "tracking-param-only difference without stripping",
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded+"?utm_source=x", "", ""),
		},
		{
			name:  "hex-case-only difference",
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, spellingLower, spellingWriterID), want: true,
		},
		{name: "legacy request", legacy: true, entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, "", "")},
		{
			name:  "equal spelling is a genuine loop",
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, spellingEncoded, spellingWriterID),
		},
		{
			name: "exact save equal to the entry URL is a genuine loop", requestURL: spellingComma,
			entry: spellingSavedEntry(spellingComma, spellingWriterID),
		},
		{
			name: "legacy save counts as unknown", requestURL: spellingComma,
			entry: spellingSavedEntry(spellingComma, ""), want: true,
		},
		{
			name:  "invalidated spelling counts as unknown",
			entry: spellingEntry(http.StatusMovedPermanently, spellingEncoded, spellingEncoded, "older-writer"), want: true,
		},
		{name: "unparsable Location", entry: spellingEntry(http.StatusMovedPermanently, "https://example.com/a%zz", "", "")},
		{name: "Location that does not normalize", entry: spellingEntry(http.StatusMovedPermanently, "http:///a%2Cb/53", "", "")},
		{name: "non-redirect status", entry: spellingEntry(http.StatusOK, spellingEncoded, "", "")},
		{name: "304 is not a cached redirect", entry: spellingEntry(http.StatusNotModified, spellingEncoded, "", "")},
		{name: "redirect without Location", entry: spellingEntry(http.StatusMovedPermanently, "", "", "")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestURL := tt.requestURL
			if requestURL == "" {
				requestURL = spellingEncoded
			}
			renderCtx := spellingRequestCtx(t, requestURL, !tt.legacy, tt.strip)

			assert.Equal(t, tt.want, ignoreCachedRedirect(tt.entry, renderCtx))
		})
	}
}

func TestLookupCache_IgnoresSameKeyRedirectOfAnotherSpelling(t *testing.T) {
	seed := func(t *testing.T, cc *CacheCoordinator, renderCtx *edgectx.RenderContext) {
		t.Helper()
		now := time.Now().UTC()
		entry := spellingEntry(http.StatusMovedPermanently, spellingEncoded, "", "")
		entry.Key = renderCtx.CacheKey.String()
		entry.HostID = renderCtx.Host.ID
		entry.CreatedAt = now
		entry.ExpiresAt = now.Add(time.Hour)
		entry.LastAccess = now
		entry.Source = cache.SourceBypass
		require.NoError(t, cc.metadata.StoreMetadata(context.Background(), entry, renderCtx.CacheKey, 0))
	}

	t.Run("exact request reads the entry as absent, logging and counting once", func(t *testing.T) {
		cc, _ := newOverrideCacheCoordinator(t)
		cc.metrics = spellingMetrics
		renderCtx := spellingRequestCtx(t, spellingEncoded, true, false)
		core, logs := observer.New(zapcore.InfoLevel)
		renderCtx.Logger = zap.New(core)
		seed(t, cc, renderCtx)

		for range 3 {
			entry, exists := cc.LookupCache(renderCtx)
			assert.False(t, exists)
			assert.Nil(t, entry)
		}

		ignored := logs.FilterMessage("Cached redirect ignored for different spelling").All()
		require.Len(t, ignored, 1)
		fields := ignored[0].ContextMap()
		assert.Equal(t, renderCtx.CacheKey.String(), fields["cache_key"])
		assert.Equal(t, spellingEncoded, fields["location"])
		assert.Equal(t, "", fields["entry_spelling"])
		assert.Equal(t, spellingEncoded, fields["request_spelling"])

		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetRequestURI("/metrics")
		spellingMetrics.ServeHTTP(ctx)
		assert.Contains(t, string(ctx.Response.Body()), spellingMetric)

		raw, exists := cc.LookupCacheRaw(renderCtx)
		require.True(t, exists, "non-serving reads still see the entry")
		assert.Equal(t, http.StatusMovedPermanently, raw.StatusCode)
	})

	t.Run("legacy request is served the entry", func(t *testing.T) {
		cc, _ := newOverrideCacheCoordinator(t)
		renderCtx := spellingRequestCtx(t, spellingComma, false, false)
		seed(t, cc, renderCtx)

		entry, exists := cc.LookupCache(renderCtx)
		require.True(t, exists)
		assert.Equal(t, http.StatusMovedPermanently, entry.StatusCode)
		assert.False(t, renderCtx.CachedRedirectIgnored)
	})

	t.Run("bypass save is allowed over an ignored bypass redirect", func(t *testing.T) {
		cc, _ := newOverrideCacheCoordinator(t)
		renderCtx := spellingRequestCtx(t, spellingEncoded, true, false)
		renderCtx.ResolvedConfig.Bypass.Cache = config.ResolvedBypassCacheConfig{Enabled: true, TTL: time.Hour, StatusCodes: []int{200}}
		seed(t, cc, renderCtx)

		canSave, reason := cc.CanSaveBypassCache(renderCtx, http.StatusOK, "text/html")
		assert.True(t, canSave, reason)
	})
}

func TestSaveCache_RecordsFetchSpelling(t *testing.T) {
	cacheKey := &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0x53}
	metaKey := "meta:" + cacheKey.String()
	override := &ResponseOverride{StatusCode: 301, Location: spellingEncoded}

	t.Run("exact request records its spelling and request ID", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)
		renderCtx := overrideRenderCtx(cacheKey)
		renderCtx.WithFetchURL(spellingEncoded, true)

		require.NoError(t, cc.SaveOverrideCache(renderCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		assert.Equal(t, spellingEncoded, mr.HGet(metaKey, "fetch_url"))
		assert.Equal(t, renderCtx.RequestID, mr.HGet(metaKey, "fetch_url_rid"))
	})

	t.Run("exact request equal to the entry URL records only its request ID", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		encodedCtx := overrideRenderCtx(cacheKey)
		encodedCtx.TargetURL = spellingComma
		encodedCtx.WithFetchURL(spellingEncoded, true)
		require.NoError(t, cc.SaveOverrideCache(encodedCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		renderCtx := overrideRenderCtx(cacheKey)
		renderCtx.RequestID = "comma-req"
		renderCtx.TargetURL = spellingComma
		renderCtx.WithFetchURL(spellingComma, true)
		require.NoError(t, cc.SaveOverrideCache(renderCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		assert.Equal(t, "", mr.HGet(metaKey, "fetch_url"))
		assert.Equal(t, "comma-req", mr.HGet(metaKey, "fetch_url_rid"))
		entry, exists := cc.LookupCacheRaw(renderCtx)
		require.True(t, exists)
		assert.Equal(t, spellingComma, entry.EffectiveFetchURL())
	})

	t.Run("legacy request records no spelling", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)
		renderCtx := overrideRenderCtx(cacheKey)
		renderCtx.WithFetchURL(spellingComma, false)

		require.NoError(t, cc.SaveOverrideCache(renderCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		assert.True(t, mr.Exists(metaKey))
		assert.Equal(t, "", mr.HGet(metaKey, "fetch_url"))
		assert.Equal(t, "", mr.HGet(metaKey, "fetch_url_rid"))
	})

	t.Run("legacy save clears the spelling an exact save left", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		exactCtx := overrideRenderCtx(cacheKey)
		exactCtx.WithFetchURL(spellingEncoded, true)
		require.NoError(t, cc.SaveOverrideCache(exactCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		legacyCtx := overrideRenderCtx(cacheKey)
		legacyCtx.RequestID = "legacy-req"
		legacyCtx.WithFetchURL(spellingComma, false)
		require.NoError(t, cc.SaveOverrideCache(legacyCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{}))

		assert.Equal(t, "", mr.HGet(metaKey, "fetch_url"))
		assert.Equal(t, "", mr.HGet(metaKey, "fetch_url_rid"))
	})
}

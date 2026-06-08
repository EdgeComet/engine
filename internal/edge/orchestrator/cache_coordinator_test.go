package orchestrator

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

func TestFilterSafeHeaders(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string][]string
		safeHeaders []string
		statusCode  int
		expected    map[string][]string
	}{
		{
			name: "case-insensitive matching - lowercase input, Title-Case config",
			headers: map[string][]string{
				"content-type":  {"text/html"},
				"cache-control": {"max-age=3600"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control"},
			statusCode:  200,
			expected: map[string][]string{
				"content-type":  {"text/html"},
				"cache-control": {"max-age=3600"},
			},
		},
		{
			name: "case-insensitive matching - Title-Case input, lowercase config",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
			},
			safeHeaders: []string{"content-type", "cache-control"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
			},
		},
		{
			name: "mixed case variations - multiple representations of same header",
			headers: map[string][]string{
				"CONTENT-TYPE": {"text/html"},
				"content-type": {"application/json"}, // Note: This will overwrite in real scenario
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected: map[string][]string{
				"CONTENT-TYPE": {"text/html"},
				"content-type": {"application/json"},
			},
		},
		{
			name: "empty safe_headers list returns nil",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
			safeHeaders: []string{},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "nil safe_headers returns nil",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
			safeHeaders: nil,
			statusCode:  200,
			expected:    nil,
		},
		{
			name:        "empty headers map returns nil",
			headers:     map[string][]string{},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name:        "nil headers map returns nil",
			headers:     nil,
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "headers not in safe list are filtered out",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"X-Custom":     {"value"},
				"Server":       {"nginx"},
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "preserves original header case from response",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"ETag":         {`"abc123"`},
			},
			safeHeaders: []string{"content-type", "etag"}, // lowercase config
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"}, // preserves Title-Case from response
				"ETag":         {`"abc123"`},  // preserves ETag case
			},
		},
		{
			name: "multiple headers match all returned",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
				"ETag":          {`"abc123"`},
				"Expires":       {"Wed, 21 Oct 2025 07:28:00 GMT"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control", "ETag", "Expires"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
				"ETag":          {`"abc123"`},
				"Expires":       {"Wed, 21 Oct 2025 07:28:00 GMT"},
			},
		},
		{
			name: "no matching headers returns nil",
			headers: map[string][]string{
				"X-Custom":       {"value"},
				"X-Another":      {"value2"},
				"X-Third-Header": {"value3"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "partial matches with some filtered",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"Server":       {"nginx"},
				"ETag":         {`"abc123"`},
				"X-Custom":     {"value"},
			},
			safeHeaders: []string{"Content-Type", "ETag"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
				"ETag":         {`"abc123"`},
			},
		},
		{
			name: "multi-value headers preserved",
			headers: map[string][]string{
				"X-Custom-Multi": {"value1", "value2"},
				"Vary":           {"Accept-Encoding", "Accept-Language"},
			},
			safeHeaders: []string{"X-Custom-Multi", "Vary"},
			statusCode:  200,
			expected: map[string][]string{
				"X-Custom-Multi": {"value1", "value2"},
				"Vary":           {"Accept-Encoding", "Accept-Language"},
			},
		},
		{
			name: "security headers blocked even if in safe list",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"Set-Cookie":   {"session=abc123"},
				"X-Auth-Token": {"secret-token"},
			},
			safeHeaders: []string{"Content-Type", "Set-Cookie", "X-Auth-Token"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "security headers case-insensitive blocking",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"SET-COOKIE":    {"session=abc123"},
				"Authorization": {"Bearer xyz"},
			},
			safeHeaders: []string{"Content-Type", "SET-COOKIE", "Authorization"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "all security headers in deny list are blocked",
			headers: map[string][]string{
				"Content-Type":        {"text/html"},
				"Set-Cookie":          {"session=abc"},
				"Authorization":       {"Bearer token"},
				"WWW-Authenticate":    {"Basic"},
				"Proxy-Authenticate":  {"Basic"},
				"Proxy-Authorization": {"Basic creds"},
				"X-Auth-Token":        {"token123"},
				"X-Access-Token":      {"access123"},
				"X-Refresh-Token":     {"refresh123"},
				"X-API-Key":           {"apikey123"},
				"X-CSRF-Token":        {"csrf123"},
				"X-XSRF-Token":        {"xsrf123"},
			},
			safeHeaders: []string{
				"Content-Type", "Set-Cookie", "Authorization", "WWW-Authenticate",
				"Proxy-Authenticate", "Proxy-Authorization", "X-Auth-Token",
				"X-Access-Token", "X-Refresh-Token", "X-API-Key", "X-CSRF-Token", "X-XSRF-Token",
			},
			statusCode: 200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		// Redirect Location header tests
		{
			name: "lowercase location header with redirect status",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"location":     {"https://example.com/new-path"}, // lowercase
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  301, // redirect status
			expected: map[string][]string{
				"Content-Type": {"text/html"},
				"Location":     {"https://example.com/new-path"}, // normalized to canonical "Location"
			},
		},
		{
			name: "redirect auto-includes Location even without safe_headers",
			headers: map[string][]string{
				"Location": {"https://example.com/redirect"},
				"Server":   {"nginx"},
			},
			safeHeaders: []string{}, // empty safe_headers
			statusCode:  302,
			expected: map[string][]string{
				"Location": {"https://example.com/redirect"},
			},
		},
		{
			name: "non-redirect status does not auto-include Location",
			headers: map[string][]string{
				"Location": {"https://example.com/not-redirect"},
			},
			safeHeaders: []string{},
			statusCode:  200, // not a redirect
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterHeaders(tt.headers, tt.safeHeaders, tt.statusCode, true)

			if tt.expected == nil {
				require.Nil(t, result, "Expected nil but got: %v", result)
			} else {
				require.NotNil(t, result, "Expected non-nil result")
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFilterHeaders_SetCookieServing(t *testing.T) {
	// Set-Cookie should pass through when forCache=false and explicitly in safe list
	headers := map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123; Path=/; HttpOnly"},
	}
	safeHeaders := []string{"Content-Type", "Set-Cookie"}

	// forCache=false: Set-Cookie allowed (for serving to client)
	result := FilterHeaders(headers, safeHeaders, 200, false)
	require.NotNil(t, result)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123; Path=/; HttpOnly"},
	}, result)

	// forCache=true: Set-Cookie blocked (for caching)
	resultCache := FilterHeaders(headers, safeHeaders, 200, true)
	require.NotNil(t, resultCache)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
	}, resultCache)
}

func TestFilterHeaders_SetCookieNotInSafeList(t *testing.T) {
	// Set-Cookie should NOT pass through if not in safe list, even with forCache=false
	headers := map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123"},
	}
	safeHeaders := []string{"Content-Type"} // Set-Cookie not in list

	result := FilterHeaders(headers, safeHeaders, 200, false)
	require.NotNil(t, result)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
	}, result)
}

func TestFilterSafeHeaders_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string][]string
		safeHeaders []string
		expected    map[string][]string
	}{
		{
			name: "typical CDN response headers",
			headers: map[string][]string{
				"Content-Type":  {"text/html; charset=utf-8"},
				"Cache-Control": {"public, max-age=3600"},
				"ETag":          {`"5f8-5e5c0b5f5a5a"`},
				"Vary":          {"Accept-Encoding"},
				"Server":        {"cloudflare"},
				"CF-RAY":        {"8a1b2c3d4e5f6-SJC"},
				"X-Powered-By":  {"Express"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control", "ETag", "Vary"},
			expected: map[string][]string{
				"Content-Type":  {"text/html; charset=utf-8"},
				"Cache-Control": {"public, max-age=3600"},
				"ETag":          {`"5f8-5e5c0b5f5a5a"`},
				"Vary":          {"Accept-Encoding"},
			},
		},
		{
			name: "redirect response with Location",
			headers: map[string][]string{
				"Location":      {"https://example.com/new-path"},
				"Cache-Control": {"no-cache"},
				"Server":        {"nginx"},
			},
			safeHeaders: []string{"Location", "Cache-Control"},
			expected: map[string][]string{
				"Location":      {"https://example.com/new-path"},
				"Cache-Control": {"no-cache"},
			},
		},
		{
			name: "API response with custom headers",
			headers: map[string][]string{
				"Content-Type":          {"application/json"},
				"X-RateLimit-Limit":     {"1000"},
				"X-RateLimit-Remaining": {"999"},
				"EC-Request-ID":         {"abc-123-def"},
			},
			safeHeaders: []string{"Content-Type", "X-RateLimit-Limit", "X-RateLimit-Remaining"},
			expected: map[string][]string{
				"Content-Type":          {"application/json"},
				"X-RateLimit-Limit":     {"1000"},
				"X-RateLimit-Remaining": {"999"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterHeaders(tt.headers, tt.safeHeaders, 200, true)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_isCacheStaleServable(t *testing.T) {
	ptrDuration := func(d time.Duration) *types.Duration {
		td := types.Duration(d)
		return &td
	}

	defaultStatusCodes := []int{200}
	staleTTL := 2 * time.Hour

	t.Run("fresh cache returns false", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(1 * time.Hour),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.False(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("delete strategy returns false", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-10 * time.Minute),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyDelete,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.False(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("serve_stale with nil stale_ttl returns false", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-10 * time.Minute),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: nil,
		}
		assert.False(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("expired within stale window returns true", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-10 * time.Minute),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.True(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("expired beyond stale window returns false", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-3 * time.Hour),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.False(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("status code not in cacheable list returns false", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 404,
			ExpiresAt:  time.Now().UTC().Add(-10 * time.Minute),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.False(t, isCacheStaleServable(cached, expired, defaultStatusCodes))
	})

	t.Run("status code in cacheable list within stale window returns true", func(t *testing.T) {
		cached := &cache.CacheMetadata{
			StatusCode: 404,
			ExpiresAt:  time.Now().UTC().Add(-10 * time.Minute),
		}
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(staleTTL),
		}
		assert.True(t, isCacheStaleServable(cached, expired, []int{200, 404}))
	})
}

func Test_getStaleTTL(t *testing.T) {
	ptrDuration := func(d time.Duration) *types.Duration {
		td := types.Duration(d)
		return &td
	}

	tests := []struct {
		name     string
		expired  types.CacheExpiredConfig
		expected time.Duration
	}{
		{
			name: "serve_stale strategy with stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: ptrDuration(2 * time.Hour),
			},
			expected: 2 * time.Hour,
		},
		{
			name: "delete strategy with stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
				StaleTTL: ptrDuration(2 * time.Hour),
			},
			expected: 0,
		},
		{
			name: "serve_stale strategy without stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: nil,
			},
			expected: 0,
		},
		{
			name: "delete strategy without stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
				StaleTTL: nil,
			},
			expected: 0,
		},
		{
			name: "empty strategy",
			expired: types.CacheExpiredConfig{
				Strategy: "",
				StaleTTL: ptrDuration(1 * time.Hour),
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStaleTTL(tt.expired)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func newOverrideCacheCoordinator(t *testing.T) (*CacheCoordinator, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)

	keyGen := customredis.NewKeyGenerator()
	cacheDir := t.TempDir()
	metadataStore := cache.NewMetadataStore(redisClient, keyGen, cacheDir, logger)
	fsCache := cache.NewFilesystemCache(logger)
	cacheService := cache.NewCacheService(metadataStore, fsCache, logger)

	cc := NewCacheCoordinator(metadataStore, fsCache, cacheService, nil, nil, logger)
	return cc, mr
}

func overrideRenderCtx(cacheKey *types.CacheKey) *edgectx.RenderContext {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("GET")
	ctx.Request.SetRequestURI("/test")
	renderCtx := edgectx.NewRenderContext("test-req", ctx, zap.NewNop(), 120*time.Second)
	renderCtx.TargetURL = "https://example.com/page"
	renderCtx.URLHash = 0xabc123
	renderCtx.Host = &types.Host{ID: 1}
	renderCtx.Dimension = "desktop"
	renderCtx.CacheKey = cacheKey
	renderCtx.ResolvedConfig = &config.ResolvedConfig{Compression: "none"}
	return renderCtx
}

func TestSaveOverrideCache(t *testing.T) {
	t.Run("301 redirect creates metadata-only entry with Location header", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		cacheKey := &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0x301}
		renderCtx := overrideRenderCtx(cacheKey)

		override := &ResponseOverride{StatusCode: 301, Location: "https://example.com/new-page"}
		err := cc.SaveOverrideCache(renderCtx, override, cache.SourceRender, time.Hour, types.CacheExpiredConfig{})
		require.NoError(t, err)

		metaKey := "meta:" + cacheKey.String()
		assert.Equal(t, "301", mr.HGet(metaKey, "status_code"))
		assert.Equal(t, "0", mr.HGet(metaKey, "size"))
		assert.Equal(t, "0", mr.HGet(metaKey, "disk_size"))
		assert.Contains(t, mr.HGet(metaKey, "headers"), "https://example.com/new-page")
		assert.Equal(t, cache.SourceRender, mr.HGet(metaKey, "source"))
	})

	t.Run("404 override creates metadata-only entry without Location", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		cacheKey := &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0x404}
		renderCtx := overrideRenderCtx(cacheKey)

		override := &ResponseOverride{StatusCode: 404}
		err := cc.SaveOverrideCache(renderCtx, override, cache.SourceRender, time.Hour, types.CacheExpiredConfig{})
		require.NoError(t, err)

		metaKey := "meta:" + cacheKey.String()
		assert.Equal(t, "404", mr.HGet(metaKey, "status_code"))
		assert.Equal(t, "0", mr.HGet(metaKey, "size"))
		assert.Empty(t, mr.HGet(metaKey, "headers"))
	})

	t.Run("bypass source stored correctly", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		cacheKey := &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0x410}
		renderCtx := overrideRenderCtx(cacheKey)

		override := &ResponseOverride{StatusCode: 410}
		err := cc.SaveOverrideCache(renderCtx, override, cache.SourceBypass, time.Hour, types.CacheExpiredConfig{})
		require.NoError(t, err)

		metaKey := "meta:" + cacheKey.String()
		assert.Equal(t, cache.SourceBypass, mr.HGet(metaKey, "source"))
		assert.Equal(t, "410", mr.HGet(metaKey, "status_code"))
	})

	t.Run("stale TTL extends Redis key expiration", func(t *testing.T) {
		cc, mr := newOverrideCacheCoordinator(t)

		cacheKey := &types.CacheKey{HostID: 1, DimensionID: 1, URLHash: 0x5301}
		renderCtx := overrideRenderCtx(cacheKey)

		staleTTLValue := types.Duration(30 * time.Minute)
		expired := types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: &staleTTLValue,
		}
		override := &ResponseOverride{StatusCode: 301, Location: "https://example.com/target"}
		err := cc.SaveOverrideCache(renderCtx, override, cache.SourceRender, time.Hour, expired)
		require.NoError(t, err)

		metaKey := "meta:" + cacheKey.String()
		ttl := mr.TTL(metaKey)
		// TTL should be base (1h) + stale (30m) = 90m
		assert.Greater(t, ttl, 89*time.Minute)
		assert.LessOrEqual(t, ttl, 91*time.Minute)
	})
}

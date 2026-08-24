package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

func ptrDuration(d time.Duration) *types.Duration {
	td := types.Duration(d)
	return &td
}

func newTestRenderContext(resolvedCfg *config.ResolvedConfig) *edgectx.RenderContext {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("GET")
	ctx.Request.SetRequestURI("/test")
	renderCtx := edgectx.NewRenderContext("test-request", ctx, zap.NewNop(), 30*time.Second)
	renderCtx.ResolvedConfig = resolvedCfg
	return renderCtx
}

func TestWriteCacheResponse_StaleDetection(t *testing.T) {
	rw := NewResponseWriter()

	t.Run("render cache entry uses render stale TTL", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: ptrDuration(2 * time.Hour),
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
		}
		resp := &cache.CacheResponse{
			Content:  []byte("<html>test</html>"),
			CacheAge: 2 * time.Hour,
		}

		err := rw.WriteCacheResponse(renderCtx, entry, resp)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceRenderStale, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("bypass cache entry uses bypass stale TTL", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: nil,
				},
			},
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Expired: types.CacheExpiredConfig{
						StaleTTL: ptrDuration(2 * time.Hour),
					},
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
		}
		resp := &cache.CacheResponse{
			Content:  []byte("<html>test</html>"),
			CacheAge: 2 * time.Hour,
		}

		err := rw.WriteCacheResponse(renderCtx, entry, resp)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceBypassStale, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("bypass cache entry without bypass stale TTL is fresh", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: ptrDuration(2 * time.Hour),
				},
			},
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Expired: types.CacheExpiredConfig{
						StaleTTL: nil,
					},
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
		}
		resp := &cache.CacheResponse{
			Content:  []byte("<html>test</html>"),
			CacheAge: 2 * time.Hour,
		}

		err := rw.WriteCacheResponse(renderCtx, entry, resp)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceBypassCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("bypass cache entry expired beyond stale TTL is fresh", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Expired: types.CacheExpiredConfig{
						StaleTTL: ptrDuration(1 * time.Hour),
					},
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(-2 * time.Hour),
			CreatedAt:  time.Now().UTC().Add(-4 * time.Hour),
		}
		resp := &cache.CacheResponse{
			Content:  []byte("<html>test</html>"),
			CacheAge: 4 * time.Hour,
		}

		err := rw.WriteCacheResponse(renderCtx, entry, resp)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceBypassCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})
}

func TestWriteCachedMetadataResponse_StaleDetection(t *testing.T) {
	rw := NewResponseWriter()

	t.Run("render redirect uses render stale TTL", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: ptrDuration(2 * time.Hour),
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 301,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
			Headers: map[string][]string{
				"Location": {"https://example.com/new"},
			},
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceRenderStale, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("bypass redirect uses bypass stale TTL", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: nil,
				},
			},
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Expired: types.CacheExpiredConfig{
						StaleTTL: ptrDuration(2 * time.Hour),
					},
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 302,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
			Headers: map[string][]string{
				"Location": {"https://example.com/other"},
			},
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceBypassStale, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("bypass redirect without bypass stale TTL is fresh", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{
					StaleTTL: ptrDuration(2 * time.Hour),
				},
			},
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Expired: types.CacheExpiredConfig{
						StaleTTL: nil,
					},
				},
			},
		})

		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 301,
			ExpiresAt:  time.Now().UTC().Add(-30 * time.Minute),
			CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
			Headers: map[string][]string{
				"Location": {"https://example.com/new"},
			},
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, types.SourceBypassCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})
}

func TestWriteCachedMetadataResponse_StatusOverrides(t *testing.T) {
	rw := NewResponseWriter()

	t.Run("404 override from render cache has correct headers", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 404,
			DiskSize:   0,
			CreatedAt:  time.Now().UTC().Add(-5 * time.Minute),
			ExpiresAt:  time.Now().UTC().Add(55 * time.Minute),
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, 404, renderCtx.HTTPCtx.Response.StatusCode())
		assert.Equal(t, types.SourceRenderCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
		assert.NotEmpty(t, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderCacheAge)))
		assert.Contains(t, string(renderCtx.HTTPCtx.Response.Body()), "Not Found")
		assert.Equal(t, "text/plain; charset=utf-8", string(renderCtx.HTTPCtx.Response.Header.Peek("Content-Type")))
	})

	t.Run("410 override from render cache", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 410,
			DiskSize:   0,
			CreatedAt:  time.Now().UTC().Add(-5 * time.Minute),
			ExpiresAt:  time.Now().UTC().Add(55 * time.Minute),
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, 410, renderCtx.HTTPCtx.Response.StatusCode())
		assert.Equal(t, types.SourceRenderCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
		assert.Contains(t, string(renderCtx.HTTPCtx.Response.Body()), "Gone")
	})

	t.Run("404 override from bypass cache", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceBypass,
			StatusCode: 404,
			DiskSize:   0,
			CreatedAt:  time.Now().UTC().Add(-5 * time.Minute),
			ExpiresAt:  time.Now().UTC().Add(55 * time.Minute),
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, 404, renderCtx.HTTPCtx.Response.StatusCode())
		assert.Equal(t, types.SourceBypassCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
	})

	t.Run("301 redirect still has Location and empty body", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 301,
			DiskSize:   0,
			CreatedAt:  time.Now().UTC().Add(-5 * time.Minute),
			ExpiresAt:  time.Now().UTC().Add(55 * time.Minute),
			Headers: map[string][]string{
				"Location": {"https://example.com/new"},
			},
		}

		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, 301, renderCtx.HTTPCtx.Response.StatusCode())
		assert.Equal(t, "https://example.com/new", string(renderCtx.HTTPCtx.Response.Header.Peek("Location")))
		assert.Equal(t, types.SourceRenderCache, string(renderCtx.HTTPCtx.Response.Header.Peek(types.HeaderSource)))
		assert.Empty(t, string(renderCtx.HTTPCtx.Response.Body()))
	})
}

// TestResponseWriters_KeepAliveEnabled guards against re-introducing the forced
// Connection: close that previously prevented client/proxy connection reuse.
// Every write path sets a definite body length, so keep-alive is safe.
func TestResponseWriters_KeepAliveEnabled(t *testing.T) {
	rw := NewResponseWriter()

	t.Run("rendered response", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		err := rw.WriteRenderedResponse(renderCtx, []byte("<html>ok</html>"), 200, "", "rs-1", nil)
		assert.NoError(t, err)
		assert.False(t, renderCtx.HTTPCtx.Response.ConnectionClose(), "rendered response must not force Connection: close")
	})

	t.Run("bypass response", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		err := rw.WriteBypassResponse(renderCtx, &bypass.BypassResponse{
			StatusCode:  200,
			Body:        []byte("<html>ok</html>"),
			ContentType: "text/html; charset=utf-8",
			Headers:     map[string][]string{},
		})
		assert.NoError(t, err)
		assert.False(t, renderCtx.HTTPCtx.Response.ConnectionClose(), "bypass response must not force Connection: close")
	})

	t.Run("cache response", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 200,
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			CreatedAt:  time.Now().UTC(),
		}
		err := rw.WriteCacheResponse(renderCtx, entry, &cache.CacheResponse{Content: []byte("<html>ok</html>")})
		assert.NoError(t, err)
		assert.False(t, renderCtx.HTTPCtx.Response.ConnectionClose(), "cache response must not force Connection: close")
	})

	t.Run("cached metadata response", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 404,
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			CreatedAt:  time.Now().UTC(),
		}
		err := rw.WriteCachedMetadataResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.False(t, renderCtx.HTTPCtx.Response.ConnectionClose(), "cached metadata response must not force Connection: close")
	})

	t.Run("status response", func(t *testing.T) {
		renderCtx := newTestRenderContext(&config.ResolvedConfig{})
		err := rw.WriteStatusResponse(renderCtx, config.ResolvedStatusConfig{Code: 404, Reason: "gone"})
		assert.NoError(t, err)
		assert.False(t, renderCtx.HTTPCtx.Response.ConnectionClose(), "status response must not force Connection: close")
	})
}

func TestWriteCacheResponse_LocationGatedByStatus(t *testing.T) {
	rw := NewResponseWriter()

	newCtx := func() *edgectx.RenderContext {
		return newTestRenderContext(&config.ResolvedConfig{
			SafeResponseHeaders: []string{"Content-Type", "Location"},
			Cache: config.ResolvedCacheConfig{
				Expired: types.CacheExpiredConfig{StaleTTL: ptrDuration(time.Hour)},
			},
		})
	}

	// Entries written before the save-side gate still carry a Location on a 200.
	t.Run("200 entry drops a stored Location", func(t *testing.T) {
		renderCtx := newCtx()
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 200,
			Headers:    map[string][]string{"Location": {"https://example.com/final"}},
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			CreatedAt:  time.Now().UTC(),
		}
		resp := &cache.CacheResponse{Content: []byte("<html>page</html>")}

		assert.NoError(t, rw.WriteCacheResponse(renderCtx, entry, resp))
		assert.Empty(t, renderCtx.HTTPCtx.Response.Header.Peek("Location"))
	})

	// Redirects normally take the metadata-only path; the guard here keeps Location intact
	// if a 3xx ever arrives with a body.
	t.Run("301 entry keeps a stored Location", func(t *testing.T) {
		renderCtx := newCtx()
		entry := &cache.CacheMetadata{
			Source:     cache.SourceRender,
			StatusCode: 301,
			Headers:    map[string][]string{"Location": {"https://example.com/new-page"}},
			ExpiresAt:  time.Now().UTC().Add(time.Hour),
			CreatedAt:  time.Now().UTC(),
		}
		resp := &cache.CacheResponse{Content: []byte("<html>moved</html>")}

		assert.NoError(t, rw.WriteCacheResponse(renderCtx, entry, resp))
		assert.Equal(t, "https://example.com/new-page", string(renderCtx.HTTPCtx.Response.Header.Peek("Location")))
	})
}

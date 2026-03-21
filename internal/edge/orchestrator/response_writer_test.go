package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
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
		assert.Equal(t, "stale", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
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
		assert.Equal(t, "stale", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
	})

	t.Run("bypass cache entry without bypass stale TTL returns hit", func(t *testing.T) {
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
		assert.Equal(t, "hit", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
	})

	t.Run("bypass cache entry expired beyond stale TTL returns hit", func(t *testing.T) {
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
		assert.Equal(t, "hit", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
	})
}

func TestWriteCachedRedirectResponse_StaleDetection(t *testing.T) {
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

		err := rw.WriteCachedRedirectResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, "stale", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
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

		err := rw.WriteCachedRedirectResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, "stale", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
	})

	t.Run("bypass redirect without bypass stale TTL returns hit", func(t *testing.T) {
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

		err := rw.WriteCachedRedirectResponse(renderCtx, entry)
		assert.NoError(t, err)
		assert.Equal(t, "hit", string(renderCtx.HTTPCtx.Response.Header.Peek("X-Render-Cache")))
	})
}

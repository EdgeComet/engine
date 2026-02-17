package cachedaemon

import (
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

type mockConfigManager struct {
	hosts []types.Host
}

func (m *mockConfigManager) GetConfig() *configtypes.EgConfig {
	return &configtypes.EgConfig{}
}

func (m *mockConfigManager) GetHosts() []types.Host {
	return m.hosts
}

func (m *mockConfigManager) GetHostByDomain(domain string) *types.Host {
	for i := range m.hosts {
		if m.hosts[i].Domain == domain {
			return &m.hosts[i]
		}
	}
	return nil
}

func setupTestDaemon(t *testing.T) (*CacheDaemon, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{
		Addr: mr.Addr(),
	}, logger)
	require.NoError(t, err)

	keyGen := redis.NewKeyGenerator()
	iq := NewInternalQueue(100)

	configMgr := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:     1,
				Domain: "example.com",
				Render: types.RenderConfig{
					Dimensions: map[string]types.Dimension{
						"mobile":  {ID: 1},
						"desktop": {ID: 2},
					},
					Cache: &types.RenderCacheConfig{},
				},
			},
			{
				ID:     2,
				Domain: "nocache.com",
				Render: types.RenderConfig{
					Dimensions: map[string]types.Dimension{
						"mobile": {ID: 1},
					},
				},
			},
		},
	}

	daemon := &CacheDaemon{
		redis:           redisClient,
		logger:          logger,
		keyGenerator:    keyGen,
		internalQueue:   iq,
		internalAuthKey: "test-auth-key",
		configManager:   configMgr,
		cacheReader:     NewCacheReader(redisClient, keyGen, logger),
		queueReader:     NewQueueReader(redisClient, keyGen, iq, logger),
	}

	return daemon, mr
}

func makeTestRequest(daemon *CacheDaemon, method, path string) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(method)
	ctx.Request.SetRequestURI(path)
	ctx.Request.Header.Set("X-Internal-Auth", "test-auth-key")
	daemon.ServeHTTP(ctx)
	return ctx
}

func TestHandlerValidation(t *testing.T) {
	t.Run("missing host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=abc")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("unknown host_id returns 404", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=999")
		assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	})

	t.Run("invalid limit returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&limit=0")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())

		ctx = makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&limit=101")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid status filter returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&status=invalid")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid dimension returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&dimension=tablet")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("dimension filter with spaces is trimmed", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)
		populateMetadataHash(mr, 1, 1, "mob1", map[string]string{
			"url": "https://example.com/m1", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
		populateMetadataHash(mr, 1, 2, "desk1", map[string]string{
			"url": "https://example.com/d1", "dimension": "desktop",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&dimension=mobile,+desktop&limit=100")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		body := string(ctx.Response.Body())
		assert.Contains(t, body, "example.com/m1")
		assert.Contains(t, body, "example.com/d1")
	})

	t.Run("urlContains too long returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		longStr := strings.Repeat("a", 201)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&urlContains="+longStr)
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("sizeMax < sizeMin returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&sizeMin=100&sizeMax=50")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("cacheAgeMax < cacheAgeMin returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&cacheAgeMin=100&cacheAgeMax=50")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid source returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&source=cached")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid priority returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/queue?host_id=1&priority=urgent")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("invalid statusCode returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&statusCode=abc")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("statusCode filter with spaces is trimmed", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)
		populateMetadataHash(mr, 1, 1, "ok1", map[string]string{
			"url": "https://example.com/ok", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999",
			"source": "render", "status_code": "200",
		})
		populateMetadataHash(mr, 1, 1, "nf1", map[string]string{
			"url": "https://example.com/nf", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999",
			"source": "render", "status_code": "404",
		})
		populateMetadataHash(mr, 1, 1, "err1", map[string]string{
			"url": "https://example.com/err", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999",
			"source": "render", "status_code": "500",
		})
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&statusCode=200,+404&limit=100")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		body := string(ctx.Response.Body())
		assert.Contains(t, body, "example.com/ok")
		assert.Contains(t, body, "example.com/nf")
		assert.NotContains(t, body, "example.com/err")
	})

	t.Run("invalid indexStatus returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1&indexStatus=5")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("valid request returns 200", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/urls?host_id=1")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Contains(t, string(ctx.Response.Body()), `"success":true`)
		assert.Contains(t, string(ctx.Response.Body()), `"items"`)
	})

	t.Run("summary returns 200", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/summary?host_id=1")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Contains(t, string(ctx.Response.Body()), `"success":true`)
		assert.Contains(t, string(ctx.Response.Body()), `"totalUrls"`)
	})

	t.Run("queue returns 200", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/queue?host_id=1")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Contains(t, string(ctx.Response.Body()), `"success":true`)
		assert.Contains(t, string(ctx.Response.Body()), `"items"`)
	})

	t.Run("queue summary returns 200", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/queue/summary?host_id=1")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Contains(t, string(ctx.Response.Body()), `"success":true`)
		assert.Contains(t, string(ctx.Response.Body()), `"pending"`)
	})

	t.Run("host with nil Cache config does not panic", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/summary?host_id=2")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	})

	t.Run("unauthorized returns 401", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := &fasthttp.RequestCtx{}
		ctx.Request.Header.SetMethod("GET")
		ctx.Request.SetRequestURI("/internal/cache/urls?host_id=1")
		daemon.ServeHTTP(ctx)
		assert.Equal(t, fasthttp.StatusUnauthorized, ctx.Response.StatusCode())
	})
}

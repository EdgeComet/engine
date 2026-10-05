package orchestrator

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/internal/common/config"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

const lockTestKey = "lock:test"

// lockTestHost has a 15s host render timeout and a URL rule raising it to 60s for /slow/*.
func lockTestHost() *types.Host {
	ruleTimeout := types.Duration(60 * time.Second)
	return &types.Host{
		ID:     1,
		Domain: "example.com",
		Render: types.RenderConfig{Timeout: types.Duration(15 * time.Second)},
		URLRules: []types.URLRule{{
			Match:  "/slow/*",
			Action: types.ActionRender,
			Render: &types.RenderRuleConfig{Timeout: &ruleTimeout},
		}},
	}
}

func lockTestContext(t *testing.T, host *types.Host, targetURL string, logger *zap.Logger, requestTimeout time.Duration) *edgectx.RenderContext {
	t.Helper()
	httpCtx := &fasthttp.RequestCtx{}
	httpCtx.Request.Header.SetMethod("GET")
	httpCtx.Request.SetRequestURI("/render")
	renderCtx := edgectx.NewRenderContext("lock-test", httpCtx, logger, requestTimeout)
	renderCtx.WithHost(host)
	renderCtx.WithLockKey(lockTestKey)
	renderCtx.ResolvedConfig = config.NewConfigResolver(&config.GlobalRenderConfig{}, &config.GlobalBypassConfig{}, nil, nil, nil, nil, "", host).
		ResolveForURL(targetURL)
	return renderCtx
}

func newLockTestCoordinator(t *testing.T) (*LockCoordinator, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)
	return NewLockCoordinator(cache.NewMetadataStore(redisClient, customredis.NewKeyGenerator(), t.TempDir(), logger), logger), mr
}

// Regression: the lock was sized from the host's render timeout, so a URL rule that raised it let
// the lock expire mid-render and a second request start a duplicate render.
func TestAcquireLock_SizedFromResolvedRenderTimeout(t *testing.T) {
	lc, mr := newLockTestCoordinator(t)
	host := lockTestHost()

	first := lockTestContext(t, host, "https://example.com/slow/page", zap.NewNop(), 30*time.Second)
	require.Equal(t, 60*time.Second, first.ResolvedConfig.Render.Timeout, "the rule's timeout is what the render runs with")

	acquired, err := lc.AcquireLock(first)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, 63*time.Second, mr.TTL(lockTestKey), "rule timeout + buffer")

	// Past the old host-derived lock (30s floor) but inside the 60s render budget.
	mr.FastForward(31 * time.Second)
	second := lockTestContext(t, host, "https://example.com/slow/page", zap.NewNop(), 30*time.Second)
	acquired, err = lc.AcquireLock(second)
	require.NoError(t, err)
	assert.False(t, acquired, "the first render still holds the lock")
}

// Without an override the host's timeout applies, and the 30s floor still holds.
func TestAcquireLock_HostTimeoutKeepsFloor(t *testing.T) {
	lc, mr := newLockTestCoordinator(t)

	renderCtx := lockTestContext(t, lockTestHost(), "https://example.com/other", zap.NewNop(), 30*time.Second)
	require.Equal(t, 15*time.Second, renderCtx.ResolvedConfig.Render.Timeout)

	acquired, err := lc.AcquireLock(renderCtx)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, minLockTTL, mr.TTL(lockTestKey))
}

// The concurrent wait is 80% of the resolved render timeout. The request context is already past
// its own deadline, so the wait logs its budget and returns on the first poll.
func TestWaitForConcurrentRender_SizedFromResolvedRenderTimeout(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantWait time.Duration
	}{
		{name: "rule override", url: "https://example.com/slow/page", wantWait: 48 * time.Second},
		{name: "host timeout", url: "https://example.com/other", wantWait: 12 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc, _ := newLockTestCoordinator(t)
			core, logs := observer.New(zapcore.InfoLevel)
			renderCtx := lockTestContext(t, lockTestHost(), tt.url, zap.New(core), time.Nanosecond)
			time.Sleep(time.Millisecond)

			result, err := lc.WaitForConcurrentRender(renderCtx, nil, stampMetrics)
			require.NoError(t, err)
			require.Equal(t, WaitRequestTimeout, result)

			entries := logs.FilterMessage("Lock not acquired, waiting for concurrent render to complete").All()
			require.Len(t, entries, 1)
			assert.Equal(t, tt.wantWait, entries[0].ContextMap()["wait_timeout"])
		})
	}
}

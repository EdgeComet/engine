package recache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

// bypassRecacheContext builds the minimal context processBypassRecache reads before it reaches
// the bypass fetch: the skip checks run first, so no live dependencies are needed.
func bypassRecacheContext(enabled bool, ttl time.Duration) *edgectx.RenderContext {
	return &edgectx.RenderContext{
		CacheKey: &types.CacheKey{HostID: 1, DimensionID: 0, URLHash: 42},
		Logger:   zap.NewNop(),
		ResolvedConfig: &config.ResolvedConfig{
			Bypass: config.ResolvedBypassConfig{
				Cache: config.ResolvedBypassCacheConfig{
					Enabled: enabled,
					TTL:     ttl,
				},
			},
		},
	}
}

// The daemon retries any non-200 up to MaxRetries and reports the exhaustion at error level, so a
// configuration-declined recache must be distinguishable from a genuine failure. Guards the
// classification, not the wording.
func TestProcessBypassRecache_ConfigDeclinedIsSkipped(t *testing.T) {
	rs := &RecacheService{logger: zap.NewNop()}

	tests := []struct {
		name    string
		enabled bool
		ttl     time.Duration
	}{
		{name: "bypass cache disabled", enabled: false, ttl: time.Minute},
		{name: "bypass cache ttl zero", enabled: true, ttl: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rs.processBypassRecache(
				context.Background(),
				"https://example.com/page",
				bypassRecacheContext(tt.enabled, tt.ttl),
				time.Now(),
			)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrRecacheSkipped),
				"config-declined recache must wrap ErrRecacheSkipped so the handler answers 200 and logs below error level, got: %v", err)
		})
	}
}

// The skip set is now exactly the config declines. An uncacheable origin status used to fold
// into ErrRecacheSkipped through CanSaveBypassCache, which is how origin outages reported a
// 100% success rate to the daemon.
func TestClassifyStatus_UncacheableStatusIsNotSkipped(t *testing.T) {
	rs := &RecacheService{cacheCoord: &orchestrator.CacheCoordinator{}}

	failure := rs.classifyStatus(502, []int{200})

	require.NotNil(t, failure)
	assert.False(t, errors.Is(error(failure), ErrRecacheSkipped),
		"an origin failure is a failure, not a configuration decline")
	assert.Equal(t, types.ErrorTypeOrigin5xx, failure.errorType)
}

// An unreachable origin returns a synthetic 502 with a nil error, so recache used to log
// "Bypass fetch completed successfully" and answer 200. The transport marker makes it a
// retryable network_error carrying no status code - the 502 was never sent by the origin.
func TestProcessBypassRecache_UnreachableOriginIsNetworkError(t *testing.T) {
	rs := &RecacheService{
		logger: zap.NewNop(),
		bypassSvc: bypass.NewBypassService(&config.GlobalBypassConfig{
			UserAgent: "EdgeCometTest/1.0",
		}, zap.NewNop()),
	}

	renderCtx := bypassRecacheContext(true, time.Minute)
	renderCtx.Host = &types.Host{ID: 1, Domain: "example.com"}

	// Loopback is rejected by the SSRF-safe dialer, so the fetch fails without touching the network.
	err := rs.processBypassRecache(context.Background(), "http://127.0.0.1:1/page", renderCtx, time.Now())

	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrRecacheSkipped), "an unreachable origin is a failure, not a config decline")

	var classified *recacheError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, types.ErrorTypeNetworkError, classified.errorType)
	assert.Equal(t, noOriginStatus, classified.statusCode, "the synthetic 502 must never be reported as an origin status")
	assert.False(t, classified.permanent)
}

// The live path writes render cache only when a TTL is configured. A URL answered by a status
// url_rule resolves no cache section at all, so it has no TTL - classifying that as an origin
// problem would blame the site for a configuration decision, and caching it with a zero TTL
// (the pre-classification behaviour) wrote entries the live path would never create.
// statusRuleService serves a host whose /blocked URL is answered by a status url_rule, so the
// resolved config carries no cache section and therefore no TTL.
func statusRuleService() *RecacheService {
	return &RecacheService{
		logger: zap.NewNop(),
		configManager: &mockEGConfigManager{
			hosts: []types.Host{
				{
					ID:         1,
					Domain:     "example.com",
					Domains:    []string{"example.com"},
					Dimensions: map[string]types.Dimension{"desktop": {ID: 0}},
					URLRules:   []types.URLRule{{Match: "/blocked", Action: types.ActionStatus403}},
				},
			},
		},
	}
}

func TestProcessRecache_RenderWithoutCacheTTLIsSkipped(t *testing.T) {
	rs := statusRuleService()

	err := rs.ProcessRecache(context.Background(), "https://example.com/blocked", 1, 0, "")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRecacheSkipped),
		"a render with no cache TTL is a configuration decline, not a failure, got: %v", err)

	var classified *recacheError
	assert.False(t, errors.As(err, &classified), "a config decline must not be counted as a failed attempt")
}

// A skip must not be mistaken for success: the handler still needs an error to report and log.
func TestErrRecacheSkipped_IsNotNil(t *testing.T) {
	assert.Error(t, ErrRecacheSkipped)
}

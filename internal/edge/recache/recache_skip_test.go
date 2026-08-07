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
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

// bypassRecacheContext builds the minimal context processBypassRecache reads before it reaches
// the bypass fetch: the skip checks run first, so no live dependencies are needed.
func bypassRecacheContext(enabled bool, ttl time.Duration) *edgectx.RenderContext {
	return &edgectx.RenderContext{
		CacheKey: &types.CacheKey{HostID: 1, DimensionID: 0, URLHash: 42},
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

// A skip must not be mistaken for success: the handler still needs an error to report and log.
func TestErrRecacheSkipped_IsNotNil(t *testing.T) {
	assert.Error(t, ErrRecacheSkipped)
}

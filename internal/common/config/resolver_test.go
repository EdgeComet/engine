package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// Helper functions to build test configurations

func buildTestGlobalRender() *GlobalRenderConfig {
	return &GlobalRenderConfig{
		Cache: types.RenderCacheConfig{
			TTL: ptrDuration(1 * time.Hour),
			Expired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: ptrDuration(1 * time.Hour),
			},
		},
	}
}

func buildTestGlobalBypass() *GlobalBypassConfig {
	return &GlobalBypassConfig{
		Timeout:   ptrDuration(30 * time.Second),
		UserAgent: "Mozilla/5.0 (compatible; EdgeComet/1.0)",
	}
}

func buildTestHost() *types.Host {
	return &types.Host{
		ID:     1,
		Domain: "example.com",
		Render: types.RenderConfig{
			Timeout: types.Duration(30 * time.Second),
			Cache: &types.RenderCacheConfig{
				TTL: ptrDuration(30 * time.Minute), // Override global
			},
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
				"mobile":  {ID: 2, Width: 375, Height: 812},
			},
			Events: types.RenderEvents{
				WaitFor:        "networkIdle",
				AdditionalWait: ptrDuration(1 * time.Second),
			},
		},
		URLRules: []types.URLRule{},
	}
}

// TestResolver_ActionResolution tests action resolution for URLs
func TestResolver_ActionResolution(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	tests := []struct {
		name           string
		urlRules       []types.URLRule
		url            string
		expectedAction types.URLRuleAction
		expectedReason string
	}{
		{
			name:           "no rules - default to render",
			urlRules:       []types.URLRule{},
			url:            "https://example.com/page",
			expectedAction: types.ActionRender,
			expectedReason: "",
		},
		{
			name: "render action",
			urlRules: []types.URLRule{
				{Match: "/blog/*", Action: types.ActionRender},
			},
			url:            "https://example.com/blog/post",
			expectedAction: types.ActionRender,
			expectedReason: "",
		},
		{
			name: "bypass action",
			urlRules: []types.URLRule{
				{Match: "/account/*", Action: types.ActionBypass},
			},
			url:            "https://example.com/account/profile",
			expectedAction: types.ActionBypass,
			expectedReason: "",
		},
		{
			name: "block action with reason",
			urlRules: []types.URLRule{
				{Match: "/admin/*", Action: types.ActionBlock, Status: &types.StatusRuleConfig{Reason: "Admin area restricted"}},
			},
			url:            "https://example.com/admin/users",
			expectedAction: types.ActionBlock,
			expectedReason: "Admin area restricted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host.URLRules = tt.urlRules
			resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

			resolved := resolver.ResolveForURL(tt.url)
			assert.Equal(t, tt.expectedAction, resolved.Action)
			assert.Equal(t, tt.expectedReason, resolved.Status.Reason)
		})
	}
}

// TestResolver_CacheTTLResolution tests cache TTL resolution with overrides
func TestResolver_CacheTTLResolution(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	tests := []struct {
		name        string
		urlRules    []types.URLRule
		url         string
		expectedTTL time.Duration
	}{
		{
			name:        "no override - use host default",
			urlRules:    []types.URLRule{},
			url:         "https://example.com/page",
			expectedTTL: 30 * time.Minute, // Host default
		},
		{
			name: "pattern override TTL",
			urlRules: []types.URLRule{
				{
					Match:  "/homepage",
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{
						Cache: &types.RenderCacheOverride{
							TTL: ptrDuration(15 * time.Minute),
						},
					},
				},
			},
			url:         "https://example.com/homepage",
			expectedTTL: 15 * time.Minute,
		},
		{
			name: "pattern TTL=0 (no cache)",
			urlRules: []types.URLRule{
				{
					Match:  "/search*",
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{
						Cache: &types.RenderCacheOverride{
							TTL: ptrDuration(0),
						},
					},
				},
			},
			url:         "https://example.com/search?q=test",
			expectedTTL: 0,
		},
		{
			name: "no pattern match - use host default",
			urlRules: []types.URLRule{
				{
					Match:  "/special",
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{
						Cache: &types.RenderCacheOverride{
							TTL: ptrDuration(5 * time.Minute),
						},
					},
				},
			},
			url:         "https://example.com/other",
			expectedTTL: 30 * time.Minute, // Host default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host.URLRules = tt.urlRules
			resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

			resolved := resolver.ResolveForURL(tt.url)
			assert.Equal(t, tt.expectedTTL, resolved.Cache.TTL)
		})
	}
}

// TestResolver_CacheExpiredMerge tests deep merge of cache expiration config
func TestResolver_CacheExpiredMerge(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("global defaults", func(t *testing.T) {
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		// Should use global defaults from buildTestGlobalRender()
		assert.Equal(t, types.ExpirationStrategyServeStale, resolved.Cache.Expired.Strategy)
		assert.Equal(t, 1*time.Hour, time.Duration(*resolved.Cache.Expired.StaleTTL))
	})

	t.Run("host atomic override - full config", func(t *testing.T) {
		host.Render.Cache.Expired = &types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyDelete,
			StaleTTL: ptrDuration(2 * time.Hour),
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		// Host completely replaces global
		assert.Equal(t, types.ExpirationStrategyDelete, resolved.Cache.Expired.Strategy)
		assert.Equal(t, 2*time.Hour, time.Duration(*resolved.Cache.Expired.StaleTTL))
	})

	t.Run("host atomic override - partial config replaces entirely", func(t *testing.T) {
		// Global has both fields set (from buildTestGlobalRender)
		// Host only sets strategy (atomic replacement means StaleTTL will be nil)
		host.Render.Cache.Expired = &types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyDelete,
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		// Atomic replacement: host config completely replaces global
		assert.Equal(t, types.ExpirationStrategyDelete, resolved.Cache.Expired.Strategy)
		assert.Nil(t, resolved.Cache.Expired.StaleTTL, "StaleTTL should be nil (not inherited from global)")
	})

	t.Run("pattern atomic override - replaces host and global", func(t *testing.T) {
		// Host config
		host.Render.Cache.Expired = &types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(30 * time.Minute),
		}
		// Pattern overrides with different config
		host.URLRules = []types.URLRule{
			{
				Match:  "/blog/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Cache: &types.RenderCacheOverride{
						Expired: &types.CacheExpiredConfig{
							Strategy: types.ExpirationStrategyServeStale,
							StaleTTL: ptrDuration(4 * time.Hour),
						},
					},
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/blog/post")

		// Pattern completely replaces host (and global)
		assert.Equal(t, types.ExpirationStrategyServeStale, resolved.Cache.Expired.Strategy)
		assert.Equal(t, 4*time.Hour, time.Duration(*resolved.Cache.Expired.StaleTTL))
	})

	t.Run("pattern with nil expired uses host config", func(t *testing.T) {
		// Host config
		host.Render.Cache.Expired = &types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyServeStale,
			StaleTTL: ptrDuration(1 * time.Hour),
		}
		// Pattern doesn't override expired config
		host.URLRules = []types.URLRule{
			{
				Match:  "/news/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Cache: &types.RenderCacheOverride{
						TTL: ptrDuration(10 * time.Minute), // Only override TTL
					},
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/news/article")

		// No pattern override, so use host config
		assert.Equal(t, types.ExpirationStrategyServeStale, resolved.Cache.Expired.Strategy)
		assert.Equal(t, 1*time.Hour, time.Duration(*resolved.Cache.Expired.StaleTTL))
	})
}

// TestResolver_RenderConfigMerge tests render configuration resolution
func TestResolver_RenderConfigMerge(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("host defaults", func(t *testing.T) {
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, 30*time.Second, resolved.Render.Timeout)
		assert.Equal(t, "", resolved.Render.Dimension) // Empty = use detected
		assert.Equal(t, "networkIdle", resolved.Render.Events.WaitFor)
		assert.Equal(t, 1*time.Second, time.Duration(*resolved.Render.Events.AdditionalWait))
	})

	t.Run("pattern override timeout", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/slow/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Timeout: ptrDuration(60 * time.Second),
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/slow/page")

		assert.Equal(t, 60*time.Second, resolved.Render.Timeout)
		assert.Equal(t, "", resolved.Render.Dimension) // Still empty
	})

	t.Run("pattern force dimension", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/mobile-only/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Dimension: "mobile",
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/mobile-only/app")

		assert.Equal(t, "mobile", resolved.Render.Dimension)
		assert.Equal(t, 30*time.Second, resolved.Render.Timeout) // Host default
	})

	t.Run("pattern override events", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/fast/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Events: &types.RenderEvents{
						WaitFor:        "load",
						AdditionalWait: ptrDuration(500 * time.Millisecond),
					},
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/fast/page")

		assert.Equal(t, "load", resolved.Render.Events.WaitFor)
		assert.Equal(t, 500*time.Millisecond, time.Duration(*resolved.Render.Events.AdditionalWait))
	})

	t.Run("pattern partial events override", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/wait/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Events: &types.RenderEvents{
						AdditionalWait: ptrDuration(3 * time.Second), // Only override additional wait
					},
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/wait/page")

		assert.Equal(t, "networkIdle", resolved.Render.Events.WaitFor) // From host
		assert.Equal(t, 3*time.Second, time.Duration(*resolved.Render.Events.AdditionalWait))
	})
}

// TestResolver_BypassConfigMerge tests bypass configuration resolution
func TestResolver_BypassConfigMerge(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("global defaults", func(t *testing.T) {
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, 30*time.Second, resolved.Bypass.Timeout)
		assert.Equal(t, "Mozilla/5.0 (compatible; EdgeComet/1.0)", resolved.Bypass.UserAgent)
	})

	t.Run("host timeout override", func(t *testing.T) {
		host.Bypass = &types.BypassConfig{
			Timeout: ptrDuration(45 * time.Second),
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, 45*time.Second, resolved.Bypass.Timeout)
	})

	t.Run("host user agent override", func(t *testing.T) {
		host.Bypass = &types.BypassConfig{
			UserAgent: "CustomBypass/2.0",
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, "CustomBypass/2.0", resolved.Bypass.UserAgent)
		assert.Equal(t, 30*time.Second, resolved.Bypass.Timeout) // From global
	})

	t.Run("pattern timeout override for bypass action", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/external/*",
				Action: types.ActionBypass,
				Bypass: &types.BypassRuleConfig{
					Timeout: ptrDuration(60 * time.Second),
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/external/api")

		assert.Equal(t, 60*time.Second, resolved.Bypass.Timeout)
	})
}

// TestResolver_NilPointerSafety tests nil pointer handling
func TestResolver_NilPointerSafety(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("nil host cache expired", func(t *testing.T) {
		host.Render.Cache.Expired = nil
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

		// Should not panic
		resolved := resolver.ResolveForURL("https://example.com/page")
		assert.NotNil(t, resolved)
		// Should use global defaults
		assert.Equal(t, types.ExpirationStrategyServeStale, resolved.Cache.Expired.Strategy)
	})

	t.Run("nil pattern cache config", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/test",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Cache: nil, // Nil cache config
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

		resolved := resolver.ResolveForURL("https://example.com/test")
		assert.NotNil(t, resolved)
		// Should use host/global defaults
		assert.Equal(t, 30*time.Minute, resolved.Cache.TTL)
	})

	t.Run("nil pattern render config", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/test",
				Action: types.ActionRender,
				Render: nil,
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

		resolved := resolver.ResolveForURL("https://example.com/test")
		assert.NotNil(t, resolved)
		assert.Equal(t, 30*time.Second, resolved.Render.Timeout) // Host default
	})

	t.Run("nil host bypass config", func(t *testing.T) {
		host.Bypass = nil
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)

		resolved := resolver.ResolveForURL("https://example.com/page")
		assert.NotNil(t, resolved)
		// Should use global defaults
		assert.Equal(t, 30*time.Second, resolved.Bypass.Timeout)
	})
}

// TestResolver_ZeroValueHandling tests handling of zero values
func TestResolver_ZeroValueHandling(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("zero TTL means no cache", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/nocache",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					Cache: &types.RenderCacheOverride{
						TTL: ptrDuration(0),
					},
				},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/nocache")

		assert.Equal(t, time.Duration(0), resolved.Cache.TTL)
	})

	t.Run("nil host TTL uses global", func(t *testing.T) {
		host.Render.Cache.TTL = nil // Not specified, use global
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		// Should use global default
		assert.Equal(t, 1*time.Hour, resolved.Cache.TTL)
	})

	t.Run("zero host TTL disables caching", func(t *testing.T) {
		zeroDuration := types.Duration(0)
		host.Render.Cache.TTL = &zeroDuration // Explicitly set to 0
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		// Should disable caching
		assert.Equal(t, time.Duration(0), resolved.Cache.TTL)
	})
}

// TestResolver_ComplexLayering tests complex multi-layer overrides
func TestResolver_ComplexLayering(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	// Host overrides some global settings
	host.Render.Cache.Expired = &types.CacheExpiredConfig{
		Strategy: types.ExpirationStrategyDelete,
		StaleTTL: ptrDuration(10 * time.Minute),
	}
	host.Bypass = &types.BypassConfig{
		UserAgent: "HostBypass/1.0",
	}

	// Pattern overrides some host settings
	host.URLRules = []types.URLRule{
		{
			Match:  "/content/*",
			Action: types.ActionRender,
			Render: &types.RenderRuleConfig{
				Timeout:   ptrDuration(45 * time.Second),
				Dimension: "desktop",
				Cache: &types.RenderCacheOverride{
					TTL: ptrDuration(2 * time.Hour),
					Expired: &types.CacheExpiredConfig{
						Strategy: types.ExpirationStrategyServeStale, // Back to serve_stale
						StaleTTL: ptrDuration(12 * time.Hour),
					},
				},
			},
		},
	}

	resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
	resolved := resolver.ResolveForURL("https://example.com/content/article")

	// Verify final resolved config comes from correct layers
	assert.Equal(t, 2*time.Hour, resolved.Cache.TTL) // Pattern

	// Expired config: strategy and StaleTTL from pattern
	assert.Equal(t, types.ExpirationStrategyServeStale, resolved.Cache.Expired.Strategy) // Pattern
	assert.Equal(t, 12*time.Hour, time.Duration(*resolved.Cache.Expired.StaleTTL))       // Pattern

	// Render config: timeout and dimension from pattern, events from host
	assert.Equal(t, 45*time.Second, resolved.Render.Timeout)                              // Pattern
	assert.Equal(t, "desktop", resolved.Render.Dimension)                                 // Pattern
	assert.Equal(t, "networkIdle", resolved.Render.Events.WaitFor)                        // Host
	assert.Equal(t, 1*time.Second, time.Duration(*resolved.Render.Events.AdditionalWait)) // Host

	// Bypass config: user agent from host, timeout from global
	assert.Equal(t, "HostBypass/1.0", resolved.Bypass.UserAgent) // Host
	assert.Equal(t, 30*time.Second, resolved.Bypass.Timeout)     // Global
}

// TestResolver_ActionSpecificConfigs tests that configs are only resolved for relevant actions
func TestResolver_ActionSpecificConfigs(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()
	host := buildTestHost()

	t.Run("bypass action still resolves bypass config", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/account/*",
				Action: types.ActionBypass,
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/account/profile")

		assert.Equal(t, types.ActionBypass, resolved.Action)
		// Bypass config should still be resolved (for potential fallback use)
		assert.Equal(t, 30*time.Second, resolved.Bypass.Timeout)
	})

	t.Run("block action", func(t *testing.T) {
		host.URLRules = []types.URLRule{
			{
				Match:  "/admin/*",
				Action: types.ActionBlock,
				Status: &types.StatusRuleConfig{Reason: "Restricted"},
			},
		}
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/admin/users")

		assert.Equal(t, types.ActionBlock, resolved.Action)
		assert.Equal(t, "Restricted", resolved.Status.Reason)
		// Bypass config still resolved for consistency
		assert.NotZero(t, resolved.Bypass.Timeout)
	})
}

// TestResolver_TrackingParamsConfigMerge tests tracking parameter configuration resolution
func TestResolver_TrackingParamsConfigMerge(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()

	t.Run("global params replaces defaults entirely", func(t *testing.T) {
		// Setup: Global config with params (replaces defaults)
		globalTracking := &types.TrackingParamsConfig{
			Strip:  ptrBool(true),
			Params: []string{"custom_only"},
		}

		host := buildTestHost()
		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Should contain ONLY custom_only (NOT built-in defaults)
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 1)
		assert.Equal(t, "custom_only", resolved.TrackingParams.CompiledPatterns[0].Compiled.Original)
	})

	t.Run("global params_add extends built-in defaults", func(t *testing.T) {
		// Setup: Global config with params_add (extends defaults)
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"custom_global"},
		}

		host := buildTestHost()
		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Should contain built-in defaults (15) + custom_global (1) = 16 total
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 16)

		// Verify built-in defaults are present
		patterns := extractPatternStrings(resolved.TrackingParams.CompiledPatterns)
		assert.Contains(t, patterns, "utm_source")
		assert.Contains(t, patterns, "gclid")
		assert.Contains(t, patterns, "custom_global")
	})

	t.Run("host params replaces all parent params", func(t *testing.T) {
		// Setup: Global with params_add, Host with params (replaces)
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"global_param"},
		}

		host := buildTestHost()
		host.TrackingParams = &types.TrackingParamsConfig{
			Params: []string{"host_only"},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Host params should REPLACE all previous params
		// Should contain ONLY host_only (NOT built-ins, NOT global_param)
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 1)
		assert.Equal(t, "host_only", resolved.TrackingParams.CompiledPatterns[0].Compiled.Original)
	})

	t.Run("pattern params replaces all parent params", func(t *testing.T) {
		// Setup: Global + Host with params_add, Pattern with params (replaces)
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"global_param"},
		}

		host := buildTestHost()
		host.TrackingParams = &types.TrackingParamsConfig{
			ParamsAdd: []string{"host_param"},
		}

		host.URLRules = []types.URLRule{
			{
				Match:  "/special/*",
				Action: types.ActionRender,
				TrackingParams: &types.TrackingParamsConfig{
					Params: []string{"pattern_only"},
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/special/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Pattern params should REPLACE all previous params
		// Should contain ONLY pattern_only (NOT built-ins, global, or host)
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 1)
		assert.Equal(t, "pattern_only", resolved.TrackingParams.CompiledPatterns[0].Compiled.Original)
	})

	t.Run("three-level merge with all params_add", func(t *testing.T) {
		// Setup: All layers use params_add to extend
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"global_param"},
		}

		host := buildTestHost()
		host.TrackingParams = &types.TrackingParamsConfig{
			ParamsAdd: []string{"host_param"},
		}

		host.URLRules = []types.URLRule{
			{
				Match:  "/merge/*",
				Action: types.ActionRender,
				TrackingParams: &types.TrackingParamsConfig{
					ParamsAdd: []string{"pattern_param"},
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/merge/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Should contain built-ins + global + host + pattern = 15 + 3 = 18
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 18)

		patterns := extractPatternStrings(resolved.TrackingParams.CompiledPatterns)
		assert.Contains(t, patterns, "utm_source") // Built-in
		assert.Contains(t, patterns, "global_param")
		assert.Contains(t, patterns, "host_param")
		assert.Contains(t, patterns, "pattern_param")
	})

	t.Run("strip disabled at host level", func(t *testing.T) {
		globalTracking := &types.TrackingParamsConfig{
			Strip:  ptrBool(true),
			Params: []string{"global_param"},
		}

		host := buildTestHost()
		host.TrackingParams = &types.TrackingParamsConfig{
			Strip: ptrBool(false), // Disable stripping
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.False(t, resolved.TrackingParams.Enabled) // Stripping disabled
		assert.Nil(t, resolved.TrackingParams.CompiledPatterns)
	})

	t.Run("no global config uses built-in defaults", func(t *testing.T) {
		// Setup: No global tracking config (nil)
		host := buildTestHost()

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.True(t, resolved.TrackingParams.Enabled)

		// Should contain only built-in defaults (15 params)
		assert.Len(t, resolved.TrackingParams.CompiledPatterns, 15)

		patterns := extractPatternStrings(resolved.TrackingParams.CompiledPatterns)
		assert.Contains(t, patterns, "utm_source")
		assert.Contains(t, patterns, "gclid")
		assert.Contains(t, patterns, "fbclid")
	})

	t.Run("empty params list auto-disables", func(t *testing.T) {
		// Setup: Global with empty params (replaces defaults with nothing)
		globalTracking := &types.TrackingParamsConfig{
			Params: []string{},
		}

		host := buildTestHost()
		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.False(t, resolved.TrackingParams.Enabled, "empty pattern list should auto-disable tracking params")
		assert.Nil(t, resolved.TrackingParams.CompiledPatterns)
	})

	t.Run("host empty params replaces and auto-disables", func(t *testing.T) {
		// Setup: Global has params_add, but host replaces with empty params
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"global_param"},
		}

		host := buildTestHost()
		host.TrackingParams = &types.TrackingParamsConfig{
			Params: []string{}, // Empty - replaces all parent params
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.False(t, resolved.TrackingParams.Enabled, "empty pattern list should auto-disable tracking params")
		assert.Nil(t, resolved.TrackingParams.CompiledPatterns)
	})

	t.Run("pattern empty params replaces and auto-disables", func(t *testing.T) {
		// Setup: Global has params_add, but pattern replaces with empty
		globalTracking := &types.TrackingParamsConfig{
			Strip:     ptrBool(true),
			ParamsAdd: []string{"global_param"},
		}

		host := buildTestHost()
		host.URLRules = []types.URLRule{
			{
				Match:  "/empty/*",
				Action: types.ActionRender,
				TrackingParams: &types.TrackingParamsConfig{
					Params: []string{}, // Clear all params
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, globalTracking, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/empty/page")

		require.NotNil(t, resolved.TrackingParams)
		assert.False(t, resolved.TrackingParams.Enabled, "empty pattern list should auto-disable tracking params")
		assert.Nil(t, resolved.TrackingParams.CompiledPatterns)
	})
}

// Helper to extract pattern strings from compiled patterns
func extractPatternStrings(patterns []CompiledStripPattern) []string {
	result := make([]string, len(patterns))
	for i, p := range patterns {
		if p.Compiled != nil {
			result[i] = p.Compiled.Original
		}
	}
	return result
}

// Helper for bool pointers
func ptrBool(b bool) *bool {
	return &b
}

// Note: ptrDuration helper is defined in config_test.go to avoid duplication

// TestCacheShardingResolution tests pattern-level cache sharding configuration resolution

// TestCacheShardingResolution tests pattern-level cache sharding configuration resolution
func TestCacheShardingResolution(t *testing.T) {
	t.Run("pattern override push_on_render to false", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalBypass := buildTestGlobalBypass()
		globalSharding := &types.CacheShardingConfig{
			Enabled:         ptrBool(true),
			PushOnRender:    ptrBool(true),
			ReplicateOnPull: ptrBool(true),
		}

		host := buildTestHost()
		host.URLRules = []types.URLRule{
			{
				Match:  "/static/pull-only.html",
				Action: types.ActionRender,
				CacheSharding: &types.CacheShardingBehaviorConfig{
					PushOnRender:    ptrBool(false),
					ReplicateOnPull: ptrBool(true),
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, globalSharding, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/static/pull-only.html")

		assert.True(t, resolved.Sharding.Enabled, "Sharding should be enabled")
		assert.False(t, resolved.Sharding.PushOnRender, "PushOnRender should be false (overridden)")
		assert.True(t, resolved.Sharding.ReplicateOnPull, "ReplicateOnPull should be true")
	})

	t.Run("pattern override push_on_render to true", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalBypass := buildTestGlobalBypass()
		globalSharding := &types.CacheShardingConfig{
			Enabled:         ptrBool(true),
			PushOnRender:    ptrBool(false),
			ReplicateOnPull: ptrBool(true),
		}

		host := buildTestHost()
		host.URLRules = []types.URLRule{
			{
				Match:  "/static/test.html",
				Action: types.ActionRender,
				CacheSharding: &types.CacheShardingBehaviorConfig{
					PushOnRender:    ptrBool(true),
					ReplicateOnPull: ptrBool(true),
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, globalSharding, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/static/test.html")

		assert.True(t, resolved.Sharding.Enabled, "Sharding should be enabled")
		assert.True(t, resolved.Sharding.PushOnRender, "PushOnRender should be true (overridden)")
		assert.True(t, resolved.Sharding.ReplicateOnPull, "ReplicateOnPull should be true")
	})
}

// TestResolver_HeadersResolution tests headers configuration resolution with replace/add semantics
func TestResolver_HeadersResolution(t *testing.T) {
	globalRender := buildTestGlobalRender()
	globalBypass := buildTestGlobalBypass()

	tests := []struct {
		name                    string
		globalHeaders           *types.HeadersConfig
		hostHeaders             *types.HeadersConfig
		patternHeaders          *types.HeadersConfig
		url                     string
		expectedRequestHeaders  []string
		expectedResponseHeaders []string
	}{
		{
			name:                    "default response headers when no config",
			globalHeaders:           nil,
			hostHeaders:             nil,
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  nil,
			expectedResponseHeaders: []string{"Content-Type", "Cache-Control", "Expires", "Last-Modified", "ETag", "Location"},
		},
		{
			name: "global safe_response replaces defaults",
			globalHeaders: &types.HeadersConfig{
				SafeResponse: []string{"Content-Type", "Cache-Control"},
			},
			hostHeaders:             nil,
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  nil,
			expectedResponseHeaders: []string{"Content-Type", "Cache-Control"},
		},
		{
			name: "host safe_response replaces global",
			globalHeaders: &types.HeadersConfig{
				SafeResponse: []string{"Content-Type", "Cache-Control"},
			},
			hostHeaders: &types.HeadersConfig{
				SafeResponse: []string{"ETag", "Expires"},
			},
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  nil,
			expectedResponseHeaders: []string{"ETag", "Expires"},
		},
		{
			name: "host safe_response_add adds to global",
			globalHeaders: &types.HeadersConfig{
				SafeResponse: []string{"Content-Type"},
			},
			hostHeaders: &types.HeadersConfig{
				SafeResponseAdd: []string{"X-Custom-Header"},
			},
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  nil,
			expectedResponseHeaders: []string{"Content-Type", "X-Custom-Header"},
		},
		{
			name: "pattern safe_response replaces host",
			globalHeaders: &types.HeadersConfig{
				SafeResponse: []string{"Content-Type"},
			},
			hostHeaders: &types.HeadersConfig{
				SafeResponse: []string{"Cache-Control"},
			},
			patternHeaders: &types.HeadersConfig{
				SafeResponse: []string{"ETag"},
			},
			url:                     "https://example.com/special/page",
			expectedRequestHeaders:  nil,
			expectedResponseHeaders: []string{"ETag"},
		},
		{
			name: "request headers opt-in at global level",
			globalHeaders: &types.HeadersConfig{
				SafeRequest: []string{"Authorization"},
			},
			hostHeaders:             nil,
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  []string{"Authorization"},
			expectedResponseHeaders: []string{"Content-Type", "Cache-Control", "Expires", "Last-Modified", "ETag", "Location"},
		},
		{
			name: "request headers add at host level",
			globalHeaders: &types.HeadersConfig{
				SafeRequest: []string{"Authorization"},
			},
			hostHeaders: &types.HeadersConfig{
				SafeRequestAdd: []string{"X-Tenant-ID"},
			},
			patternHeaders:          nil,
			url:                     "https://example.com/page",
			expectedRequestHeaders:  []string{"Authorization", "X-Tenant-ID"},
			expectedResponseHeaders: []string{"Content-Type", "Cache-Control", "Expires", "Last-Modified", "ETag", "Location"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := buildTestHost()
			host.Headers = tt.hostHeaders

			// Add pattern rule if needed
			if tt.patternHeaders != nil {
				host.URLRules = []types.URLRule{
					{
						Match:   "/special/*",
						Action:  types.ActionRender,
						Headers: tt.patternHeaders,
					},
				}
			}

			resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, tt.globalHeaders, types.CompressionSnappy, host)
			resolved := resolver.ResolveForURL(tt.url)

			assert.Equal(t, tt.expectedRequestHeaders, resolved.SafeRequestHeaders)
			assert.Equal(t, tt.expectedResponseHeaders, resolved.SafeResponseHeaders)
		})
	}
}

// TestResolver_UnmatchedDimensionResolution tests unmatched_dimension configuration resolution
func TestResolver_UnmatchedDimensionResolution(t *testing.T) {
	tests := []struct {
		name              string
		globalUnmatched   string
		hostUnmatched     string
		patternUnmatched  string
		matchURL          string
		expectedUnmatched string
		description       string
	}{
		{
			name:              "host inherits global default",
			globalUnmatched:   "bypass",
			hostUnmatched:     "",
			patternUnmatched:  "",
			matchURL:          "https://example.com/page",
			expectedUnmatched: "bypass",
			description:       "When host doesn't specify, use global value",
		},
		{
			name:              "host overrides global bypass to block",
			globalUnmatched:   "bypass",
			hostUnmatched:     "block",
			patternUnmatched:  "",
			matchURL:          "https://example.com/page",
			expectedUnmatched: "block",
			description:       "Host-level value replaces global",
		},
		{
			name:              "host overrides global to dimension name",
			globalUnmatched:   "bypass",
			hostUnmatched:     "desktop",
			patternUnmatched:  "",
			matchURL:          "https://example.com/page",
			expectedUnmatched: "desktop",
			description:       "Host can specify dimension name fallback",
		},
		{
			name:              "pattern overrides host and global",
			globalUnmatched:   "bypass",
			hostUnmatched:     "block",
			patternUnmatched:  "desktop",
			matchURL:          "https://example.com/api/docs",
			expectedUnmatched: "desktop",
			description:       "Pattern-level has highest priority",
		},
		{
			name:              "pattern uses bypass constant",
			globalUnmatched:   "desktop",
			hostUnmatched:     "block",
			patternUnmatched:  "bypass",
			matchURL:          "https://example.com/public/page",
			expectedUnmatched: "bypass",
			description:       "Pattern can override to bypass constant",
		},
		{
			name:              "pattern uses block constant",
			globalUnmatched:   "bypass",
			hostUnmatched:     "desktop",
			patternUnmatched:  "block",
			matchURL:          "https://example.com/admin/page",
			expectedUnmatched: "block",
			description:       "Pattern can override to block constant",
		},
		{
			name:              "pattern uses dimension name",
			globalUnmatched:   "bypass",
			hostUnmatched:     "block",
			patternUnmatched:  "mobile",
			matchURL:          "https://example.com/mobile-app",
			expectedUnmatched: "mobile",
			description:       "Pattern can specify dimension name",
		},
		{
			name:              "empty host inherits global",
			globalUnmatched:   "block",
			hostUnmatched:     "",
			patternUnmatched:  "",
			matchURL:          "https://example.com/page",
			expectedUnmatched: "block",
			description:       "Empty string at host level inherits global",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalRender := &GlobalRenderConfig{
				UnmatchedDimension: tt.globalUnmatched,
				Dimensions: map[string]types.Dimension{
					"desktop": {ID: 1, Width: 1920, Height: 1080},
					"mobile":  {ID: 2, Width: 375, Height: 812},
				},
			}

			host := buildTestHost()
			host.Render.UnmatchedDimension = tt.hostUnmatched

			if tt.patternUnmatched != "" {
				host.URLRules = []types.URLRule{
					{
						Match:  []string{"*"},
						Action: types.ActionRender,
						Render: &types.RenderRuleConfig{
							UnmatchedDimension: tt.patternUnmatched,
						},
					},
				}
			}

			globalBypass := buildTestGlobalBypass()
			resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
			resolved := resolver.ResolveForURL(tt.matchURL)

			assert.Equal(t, tt.expectedUnmatched, resolved.Render.UnmatchedDimension,
				"UnmatchedDimension resolution failed: %s", tt.description)
		})
	}
}

// TestResolver_UnmatchedDimensionDefaults tests default value handling for unmatched_dimension
func TestResolver_UnmatchedDimensionDefaults(t *testing.T) {
	t.Run("global bypass value propagates through resolution", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "bypass",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
			},
		}

		host := buildTestHost()
		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, "bypass", resolved.Render.UnmatchedDimension,
			"Global bypass value should propagate through resolution when host is empty")
	})

	t.Run("empty host inherits global", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "desktop",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = ""
		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.Equal(t, "desktop", resolved.Render.UnmatchedDimension,
			"Empty host UnmatchedDimension should inherit global value")
	})

	t.Run("empty pattern inherits host", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "bypass",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
				"mobile":  {ID: 2, Width: 375, Height: 812},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = "mobile"
		host.URLRules = []types.URLRule{
			{
				Match:  []string{"/test/*"},
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					UnmatchedDimension: "",
				},
			},
		}

		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/test/page")

		assert.Equal(t, "mobile", resolved.Render.UnmatchedDimension,
			"Empty pattern UnmatchedDimension should inherit host value")
	})
}

// TestResolver_UnmatchedDimensionPriority tests priority hierarchy validation
func TestResolver_UnmatchedDimensionPriority(t *testing.T) {
	t.Run("pattern > host > global (all specified)", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "bypass",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
				"mobile":  {ID: 2, Width: 375, Height: 812},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = "block"
		host.URLRules = []types.URLRule{
			{
				Match:  []string{"/special/*"},
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					UnmatchedDimension: "desktop",
				},
			},
		}

		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/special/page")

		assert.Equal(t, "desktop", resolved.Render.UnmatchedDimension,
			"Pattern value should win when all levels are specified")
	})

	t.Run("pattern > host (global empty)", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
				"mobile":  {ID: 2, Width: 375, Height: 812},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = "mobile"
		host.URLRules = []types.URLRule{
			{
				Match:  []string{"/override/*"},
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					UnmatchedDimension: "desktop",
				},
			},
		}

		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/override/page")

		assert.Equal(t, "desktop", resolved.Render.UnmatchedDimension,
			"Pattern should override host even when global is empty")
	})

	t.Run("pattern > global (host empty)", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "block",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = ""
		host.URLRules = []types.URLRule{
			{
				Match:  []string{"/test/*"},
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					UnmatchedDimension: "desktop",
				},
			},
		}

		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/test/page")

		assert.Equal(t, "desktop", resolved.Render.UnmatchedDimension,
			"Pattern should override global even when host is empty")
	})

	t.Run("host > global (pattern empty)", func(t *testing.T) {
		globalRender := &GlobalRenderConfig{
			UnmatchedDimension: "bypass",
			Dimensions: map[string]types.Dimension{
				"desktop": {ID: 1, Width: 1920, Height: 1080},
				"mobile":  {ID: 2, Width: 375, Height: 812},
			},
		}

		host := buildTestHost()
		host.Render.UnmatchedDimension = "mobile"
		host.URLRules = []types.URLRule{
			{
				Match:  []string{"/no-override/*"},
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					UnmatchedDimension: "",
				},
			},
		}

		globalBypass := buildTestGlobalBypass()
		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/no-override/page")

		assert.Equal(t, "mobile", resolved.Render.UnmatchedDimension,
			"Host should override global when pattern is empty")
	})
}

// TestResolver_StripScriptsResolution tests strip_scripts configuration resolution
func TestResolver_StripScriptsResolution(t *testing.T) {
	globalBypass := buildTestGlobalBypass()

	t.Run("default value is true", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.True(t, resolved.Render.StripScripts, "Default StripScripts should be true")
	})

	t.Run("global override to false", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalRender.StripScripts = ptrBool(false)
		host := buildTestHost()

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.False(t, resolved.Render.StripScripts, "Global StripScripts=false should be applied")
	})

	t.Run("host override takes precedence over global", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalRender.StripScripts = ptrBool(true)
		host := buildTestHost()
		host.Render.StripScripts = ptrBool(false)

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.False(t, resolved.Render.StripScripts, "Host StripScripts should override global")
	})

	t.Run("pattern override takes precedence over host", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalRender.StripScripts = ptrBool(true)
		host := buildTestHost()
		host.Render.StripScripts = ptrBool(true)
		host.URLRules = []types.URLRule{
			{
				Match:  "/app/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					StripScripts: ptrBool(false),
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/app/page")

		assert.False(t, resolved.Render.StripScripts, "Pattern StripScripts should override host")
	})

	t.Run("pattern can enable when host disables", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.StripScripts = ptrBool(false)
		host.URLRules = []types.URLRule{
			{
				Match:  "/secure/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					StripScripts: ptrBool(true),
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/secure/page")

		assert.True(t, resolved.Render.StripScripts, "Pattern should be able to enable StripScripts")
	})

	t.Run("unmatched URL uses host default", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.StripScripts = ptrBool(false)
		host.URLRules = []types.URLRule{
			{
				Match:  "/special/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{
					StripScripts: ptrBool(true),
				},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/other/page")

		assert.False(t, resolved.Render.StripScripts, "Unmatched URL should use host default")
	})

	t.Run("nil pattern render uses host default", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.StripScripts = ptrBool(false)
		host.URLRules = []types.URLRule{
			{
				Match:  "/test/*",
				Action: types.ActionRender,
				Render: nil, // nil render config
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/test/page")

		assert.False(t, resolved.Render.StripScripts, "Nil pattern render should use host default")
	})

	t.Run("inheritance chain: global -> host -> pattern", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalRender.StripScripts = ptrBool(false)
		host := buildTestHost()
		// host.Render.StripScripts is nil, inherits from global

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/page")

		assert.False(t, resolved.Render.StripScripts, "Should inherit from global when host is nil")
	})
}

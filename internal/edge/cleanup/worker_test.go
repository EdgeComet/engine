package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func TestParseDirectoryTimestamp(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}
	worker := &FilesystemCleanupWorker{
		basePath: "/cache",
		logger:   logger,
		metrics:  metrics,
	}

	tests := []struct {
		name          string
		path          string
		hostDir       string
		expectedTime  time.Time
		expectError   bool
		errorContains string
	}{
		{
			name:         "valid timestamp",
			path:         "/cache/1/2025/10/17/14/30",
			hostDir:      "/cache/1",
			expectedTime: time.Date(2025, 10, 17, 14, 30, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:         "valid timestamp with file",
			path:         "/cache/1/2025/10/17/14/30/file.html",
			hostDir:      "/cache/1",
			expectedTime: time.Date(2025, 10, 17, 14, 30, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:          "path too short",
			path:          "/cache/1/2025/10",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "path too short",
		},
		{
			name:          "invalid year",
			path:          "/cache/1/invalid/10/17/14/30",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "invalid year",
		},
		{
			name:          "invalid month",
			path:          "/cache/1/2025/abc/17/14/30",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "invalid month",
		},
		{
			name:          "out of range month",
			path:          "/cache/1/2025/13/17/14/30",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "invalid timestamp values",
		},
		{
			name:          "out of range hour",
			path:          "/cache/1/2025/10/17/25/30",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "invalid timestamp values",
		},
		{
			name:          "out of range minute",
			path:          "/cache/1/2025/10/17/14/65",
			hostDir:       "/cache/1",
			expectError:   true,
			errorContains: "invalid timestamp values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timestamp, err := worker.parseDirectoryTimestamp(tt.path, tt.hostDir)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedTime, timestamp)
			}
		})
	}
}

func TestNewFilesystemCleanupWorker(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}
	cfg := &configtypes.CleanupConfig{
		Enabled:      true,
		Interval:     types.Duration(1 * time.Hour),
		SafetyMargin: types.Duration(2 * time.Hour),
	}

	mockConfig := &config.EGConfigManager{}

	worker := NewFilesystemCleanupWorker(cfg, "/cache", mockConfig, logger, metrics)

	assert.NotNil(t, worker)
	assert.Equal(t, cfg, worker.config)
	assert.Equal(t, "/cache", worker.basePath)
	assert.NotNil(t, worker.ctx)
	assert.NotNil(t, worker.cancel)
}

func TestDeleteOldDirectories(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}
	worker := &FilesystemCleanupWorker{
		basePath: "/cache",
		logger:   logger,
		metrics:  metrics,
	}

	tempDir := t.TempDir()

	now := time.Now().UTC()
	threshold := now.Add(-26 * time.Hour)

	oldTime := now.Add(-30 * time.Hour)
	recentTime := now.Add(-1 * time.Hour)

	oldDir := filepath.Join(tempDir,
		oldTime.Format("2006"),
		oldTime.Format("01"),
		oldTime.Format("02"),
		oldTime.Format("15"),
		oldTime.Format("04"))

	recentDir := filepath.Join(tempDir,
		recentTime.Format("2006"),
		recentTime.Format("01"),
		recentTime.Format("02"),
		recentTime.Format("15"),
		recentTime.Format("04"))

	require.NoError(t, os.MkdirAll(oldDir, 0755))
	require.NoError(t, os.MkdirAll(recentDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "file1.html"), []byte("old"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(recentDir, "file2.html"), []byte("recent"), 0644))

	deleted, err := worker.deleteOldDirectories(tempDir, threshold)

	require.NoError(t, err)
	assert.Greater(t, deleted, 0, "should have deleted at least one directory")

	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err), "old directory should be deleted")

	_, err = os.Stat(recentDir)
	assert.NoError(t, err, "recent directory should still exist")
}

func TestCleanupWorkerLifecycle(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}
	cfg := &configtypes.CleanupConfig{
		Enabled:      false,
		Interval:     types.Duration(100 * time.Millisecond),
		SafetyMargin: types.Duration(1 * time.Hour),
	}

	mockConfig := &config.EGConfigManager{}
	worker := NewFilesystemCleanupWorker(cfg, t.TempDir(), mockConfig, logger, metrics)

	worker.Start()
	time.Sleep(50 * time.Millisecond)
	worker.Shutdown()
}

func TestGetMaxRetentionTime(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}

	tests := []struct {
		name              string
		globalExpired     *types.CacheExpiredConfig
		hostExpired       *types.CacheExpiredConfig
		hostHasCache      bool // Host has cache config (TTL) but no expired section
		patternExpired    *types.CacheExpiredConfig
		expectedRetention time.Duration
		expectedSource    string
	}{
		{
			name: "global serve_stale only",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
			hostExpired:       nil,
			patternExpired:    nil,
			expectedRetention: 24 * time.Hour,
			expectedSource:    "global(serve_stale)",
		},
		{
			name: "global delete strategy",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
			},
			hostExpired:       nil,
			patternExpired:    nil,
			expectedRetention: 0,
			expectedSource:    "global(delete)",
		},
		{
			name: "global and host serve_stale - host should override",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
			hostExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(48 * time.Hour); return &d }(),
			},
			patternExpired:    nil,
			expectedRetention: 48 * time.Hour,
			expectedSource:    "host(serve_stale)",
		},
		{
			name: "global serve_stale and host delete - host should take precedence",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
			hostExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
			},
			patternExpired:    nil,
			expectedRetention: 0,
			expectedSource:    "host(delete)",
		},
		{
			name: "global, host and pattern - pattern should have highest retention",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(12 * time.Hour); return &d }(),
			},
			hostExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
			patternExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(72 * time.Hour); return &d }(),
			},
			expectedRetention: 72 * time.Hour,
			expectedSource:    "pattern[0](serve_stale)",
		},
		{
			name: "host with cache but no expired section - should inherit global",
			globalExpired: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: func() *types.Duration { d := types.Duration(24 * time.Hour); return &d }(),
			},
			hostExpired:       nil,
			hostHasCache:      true, // Host has cache.ttl but no expired section
			patternExpired:    nil,
			expectedRetention: 24 * time.Hour,
			expectedSource:    "global(serve_stale)",
		},
		{
			name:              "no config at any level",
			globalExpired:     nil,
			hostExpired:       nil,
			patternExpired:    nil,
			expectedRetention: 0,
			expectedSource:    "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			egConfig := &configtypes.EgConfig{
				Render: configtypes.GlobalRenderConfig{
					Cache: types.RenderCacheConfig{
						Expired: tt.globalExpired,
					},
				},
			}

			configManager := &config.EGConfigManager{}
			configManager.SetConfig(egConfig)

			host := &types.Host{
				ID:     1,
				Domain: "test.com",
			}

			if tt.hostExpired != nil {
				host.Render.Cache = &types.RenderCacheConfig{
					Expired: tt.hostExpired,
				}
			} else if tt.hostHasCache {
				// Host has cache config (e.g., TTL) but no expired section
				// This tests the inheritance scenario
				ttl := types.Duration(1 * time.Hour)
				host.Render.Cache = &types.RenderCacheConfig{
					TTL: &ttl,
					// Expired is nil - should inherit from global
				}
			}

			if tt.patternExpired != nil {
				host.URLRules = []types.URLRule{
					{
						Action: types.ActionRender,
						Render: &types.RenderRuleConfig{
							Cache: &types.RenderCacheOverride{
								Expired: tt.patternExpired,
							},
						},
					},
				}
			}

			worker := &FilesystemCleanupWorker{
				configManager: configManager,
				logger:        logger,
				metrics:       metrics,
			}

			retention, source := worker.getMaxRetentionTime(host)

			assert.Equal(t, tt.expectedRetention, retention, "retention time should match")
			assert.Equal(t, tt.expectedSource, source, "source should match")
		})
	}
}

func TestGetMaxRetentionTimeBypass(t *testing.T) {
	logger := zap.NewNop()
	metrics := &CleanupMetrics{logger: logger}

	staleTTL := func(d time.Duration) *types.Duration {
		v := types.Duration(d)
		return &v
	}

	tests := []struct {
		name              string
		globalRender      *types.CacheExpiredConfig
		globalBypass      *types.CacheExpiredConfig
		hostRender        *types.CacheExpiredConfig
		hostBypass        *types.CacheExpiredConfig
		urlRules          []types.URLRule
		expectedRetention time.Duration
		expectedSource    string
	}{
		{
			name: "global bypass stale TTL larger than render stale TTL",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(1 * time.Hour),
			},
			globalBypass: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(2 * time.Hour),
			},
			expectedRetention: 2 * time.Hour,
			expectedSource:    "global_bypass(serve_stale)",
		},
		{
			name: "global render stale TTL larger than bypass stale TTL",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(3 * time.Hour),
			},
			globalBypass: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(1 * time.Hour),
			},
			expectedRetention: 3 * time.Hour,
			expectedSource:    "global(serve_stale)",
		},
		{
			name: "host-level bypass stale TTL overrides global",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(1 * time.Hour),
			},
			hostBypass: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(4 * time.Hour),
			},
			expectedRetention: 4 * time.Hour,
			expectedSource:    "host_bypass(serve_stale)",
		},
		{
			name: "pattern-level bypass stale TTL included in MAX calculation",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(1 * time.Hour),
			},
			urlRules: []types.URLRule{
				{
					Action: types.ActionBypass,
					Bypass: &types.BypassRuleConfig{
						Cache: &types.BypassCacheConfig{
							Expired: &types.CacheExpiredConfig{
								Strategy: types.ExpirationStrategyServeStale,
								StaleTTL: staleTTL(5 * time.Hour),
							},
						},
					},
				},
			},
			expectedRetention: 5 * time.Hour,
			expectedSource:    "pattern[0]_bypass(serve_stale)",
		},
		{
			name: "no bypass expired config - retention unchanged from render",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(2 * time.Hour),
			},
			expectedRetention: 2 * time.Hour,
			expectedSource:    "global(serve_stale)",
		},
		{
			name: "bypass strategy delete - no retention extension",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: staleTTL(1 * time.Hour),
			},
			globalBypass: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
			},
			expectedRetention: 1 * time.Hour,
			expectedSource:    "global(serve_stale)",
		},
		{
			name: "mixed patterns - render 1h stale bypass 2h stale",
			globalRender: &types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
			},
			urlRules: []types.URLRule{
				{
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{
						Cache: &types.RenderCacheOverride{
							Expired: &types.CacheExpiredConfig{
								Strategy: types.ExpirationStrategyServeStale,
								StaleTTL: staleTTL(1 * time.Hour),
							},
						},
					},
				},
				{
					Action: types.ActionBypass,
					Bypass: &types.BypassRuleConfig{
						Cache: &types.BypassCacheConfig{
							Expired: &types.CacheExpiredConfig{
								Strategy: types.ExpirationStrategyServeStale,
								StaleTTL: staleTTL(2 * time.Hour),
							},
						},
					},
				},
			},
			expectedRetention: 2 * time.Hour,
			expectedSource:    "pattern[1]_bypass(serve_stale)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			egConfig := &configtypes.EgConfig{
				Render: configtypes.GlobalRenderConfig{
					Cache: types.RenderCacheConfig{
						Expired: tt.globalRender,
					},
				},
			}

			if tt.globalBypass != nil {
				egConfig.Bypass.Cache.Expired = tt.globalBypass
			}

			configManager := &config.EGConfigManager{}
			configManager.SetConfig(egConfig)

			host := &types.Host{
				ID:     1,
				Domain: "test.com",
			}

			if tt.hostRender != nil {
				host.Render.Cache = &types.RenderCacheConfig{
					Expired: tt.hostRender,
				}
			}

			if tt.hostBypass != nil {
				host.Bypass = &types.BypassConfig{
					Cache: &types.BypassCacheConfig{
						Expired: tt.hostBypass,
					},
				}
			}

			if tt.urlRules != nil {
				host.URLRules = tt.urlRules
			}

			worker := &FilesystemCleanupWorker{
				configManager: configManager,
				logger:        logger,
				metrics:       metrics,
			}

			retention, source := worker.getMaxRetentionTime(host)

			assert.Equal(t, tt.expectedRetention, retention, "retention time should match")
			assert.Equal(t, tt.expectedSource, source, "source should match")
		})
	}
}

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

	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.MkdirAll(recentDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "file1.html"), []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(recentDir, "file2.html"), []byte("recent"), 0o644))

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

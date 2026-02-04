package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func TestLoadRSConfig(t *testing.T) {
	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "render-service.yaml")

	configYAML := `
server:
  id: "rs-test-1"
  listen: "0.0.0.0:8081"

redis:
  addr: "localhost:6380"
  password: "test123"
  db: 1

chrome:
  pool_size: "20"
  warmup:
    url: "https://test.com/"
    timeout: 15s
  restart:
    after_count: 100
    after_time: 1h

  render:
    max_timeout: 25s

log:
  level: "debug"
  console:
    enabled: true
    format: "console"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"
`

	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0644))

	// Load config
	cfg, err := LoadRSConfig(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify server config
	assert.Equal(t, "rs-test-1", cfg.Server.ID)
	assert.Equal(t, "0.0.0.0:8081", cfg.Server.Listen)

	// Verify Redis config
	assert.Equal(t, "localhost:6380", cfg.Redis.Addr)
	assert.Equal(t, "test123", cfg.Redis.Password)
	assert.Equal(t, 1, cfg.Redis.DB)

	// Verify Chrome config
	assert.Equal(t, "20", cfg.Chrome.PoolSize)
	assert.Equal(t, "https://test.com/", cfg.Chrome.Warmup.URL)
	assert.Equal(t, types.Duration(15*time.Second), cfg.Chrome.Warmup.Timeout)
	assert.Equal(t, 100, cfg.Chrome.Restart.AfterCount)
	assert.Equal(t, types.Duration(1*time.Hour), cfg.Chrome.Restart.AfterTime)

	// Verify Render config
	assert.Equal(t, types.Duration(25*time.Second), cfg.Chrome.Render.MaxTimeout)

	// Verify log config
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, true, cfg.Log.Console.Enabled)
	assert.Equal(t, "console", cfg.Log.Console.Format)
}

func TestLoadRSConfigWithAuto(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "render-service.yaml")

	configYAML := `
server:
  id: "rs-auto"
  listen: ":8080"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

chrome:
  pool_size: "auto"
  warmup:
    url: "https://example.com/"
    timeout: 10s
  restart:
    after_count: 50
    after_time: 30m

  render:
    max_timeout: 50s

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"
`

	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0644))

	cfg, err := LoadRSConfig(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "auto", cfg.Chrome.PoolSize)
}

func TestRSConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      RSConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: RSConfig{
				Server: RSServerConfig{
					ID:     "rs-1",
					Listen: "0.0.0.0:8080",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				Chrome: ChromeYAMLConfig{
					PoolSize: "auto",
					Warmup: WarmupConfig{
						URL:     "https://example.com/",
						Timeout: types.Duration(10 * time.Second),
					},
					Restart: RestartConfig{
						AfterCount: 50,
						AfterTime:  types.Duration(30 * time.Minute),
					},
					Render: RSRenderConfig{
						MaxTimeout: types.Duration(50 * time.Second),
					},
				},
				Log: LogConfig{
					Level: "info",
					Console: configtypes.ConsoleLogConfig{
						Enabled: true,
						Format:  "json",
					},
					File: configtypes.FileLogConfig{
						Enabled: false,
					},
				},
				Metrics: configtypes.MetricsConfig{
					Enabled:   true,
					Listen:    ":9090",
					Path:      "/metrics",
					Namespace: "edgecomet",
				},
			},
			expectError: false,
		},
		{
			name: "missing server ID",
			config: RSConfig{
				Server: RSServerConfig{
					Listen: "0.0.0.0:8080",
				},
			},
			expectError: true,
			errorMsg:    "server.id is required",
		},
		{
			name: "invalid port",
			config: RSConfig{
				Server: RSServerConfig{
					ID:     "rs-1",
					Listen: "0.0.0.0:99999",
				},
			},
			expectError: true,
			errorMsg:    "invalid server.listen",
		},
		{
			name: "invalid pool size",
			config: RSConfig{
				Server: RSServerConfig{
					ID:     "rs-1",
					Listen: "0.0.0.0:8080",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				Chrome: ChromeYAMLConfig{
					PoolSize: "invalid",
					Warmup: WarmupConfig{
						URL:     "https://example.com/",
						Timeout: types.Duration(10 * time.Second),
					},
					Restart: RestartConfig{
						AfterCount: 50,
						AfterTime:  types.Duration(30 * time.Minute),
					},
					Render: RSRenderConfig{
						MaxTimeout: types.Duration(50 * time.Second),
					},
				},
				Log: LogConfig{
					Level: "info",
					Console: configtypes.ConsoleLogConfig{
						Enabled: true,
						Format:  "json",
					},
					File: configtypes.FileLogConfig{
						Enabled: false,
					},
				},
			},
			expectError: true,
			errorMsg:    "chrome.pool_size must be 'auto' or positive integer",
		},
		{
			name: "invalid log level",
			config: RSConfig{
				Server: RSServerConfig{
					ID:     "rs-1",
					Listen: "0.0.0.0:8080",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				Chrome: ChromeYAMLConfig{
					PoolSize: "auto",
					Warmup: WarmupConfig{
						URL:     "https://example.com/",
						Timeout: types.Duration(10 * time.Second),
					},
					Restart: RestartConfig{
						AfterCount: 50,
						AfterTime:  types.Duration(30 * time.Minute),
					},
					Render: RSRenderConfig{
						MaxTimeout: types.Duration(50 * time.Second),
					},
				},
				Log: LogConfig{
					Level: "invalid",
					Console: configtypes.ConsoleLogConfig{
						Enabled: true,
						Format:  "json",
					},
					File: configtypes.FileLogConfig{
						Enabled: false,
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid log.level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRSConfigManager(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "render-service.yaml")

	initialConfig := `
server:
  id: "rs-1"
  listen: ":8080"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

chrome:
  pool_size: "auto"
  warmup:
    url: "https://example.com/"
    timeout: 10s
  restart:
    after_count: 50
    after_time: 30m

  render:
    max_timeout: 50s

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"
`

	require.NoError(t, os.WriteFile(configPath, []byte(initialConfig), 0644))

	logger := zaptest.NewLogger(t)
	configMgr, err := NewRSConfigManager(configPath, logger)
	require.NoError(t, err)
	require.NotNil(t, configMgr)

	// Test initial config
	cfg := configMgr.GetConfig()
	assert.Equal(t, "rs-1", cfg.Server.ID)
	assert.Equal(t, "auto", cfg.Chrome.PoolSize)
}

func TestGetConfigPath(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.yaml")

	// Create test file
	require.NoError(t, os.WriteFile(configPath, []byte("test: value"), 0644))

	// Test with valid path
	absPath, err := GetConfigPath(configPath)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(absPath))

	// Test with empty path
	_, err = GetConfigPath("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config path cannot be empty")

	// Test with non-existent file
	_, err = GetConfigPath(filepath.Join(tempDir, "nonexistent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file does not exist")
}

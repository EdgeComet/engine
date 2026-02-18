package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

// ConfigBuilder builds typed daemon configs from TestEnvironmentConfig
type ConfigBuilder struct {
	testConfig *TestEnvironmentConfig
	redisAddr  string
}

// NewConfigBuilder creates a new config builder
func NewConfigBuilder(testConfig *TestEnvironmentConfig, redisAddr string) *ConfigBuilder {
	return &ConfigBuilder{
		testConfig: testConfig,
		redisAddr:  redisAddr,
	}
}

// BuildEGConfig builds Edge Gateway configuration
func (b *ConfigBuilder) BuildEGConfig() *config.EgConfig {
	// Convert cache base path to absolute path
	// Tests run from tests/acceptance/basic/, so compute path from test directory
	cacheBasePath := b.testConfig.EdgeGateway.Storage.BasePath
	if !filepath.IsAbs(cacheBasePath) {
		// Get absolute path from current working directory (test directory)
		absCachePath, err := filepath.Abs(cacheBasePath)
		if err == nil {
			cacheBasePath = absCachePath
		}
	}

	return &config.EgConfig{
		Internal: configtypes.InternalConfig{
			Listen:  "0.0.0.0:10071",
			AuthKey: "test-auth-key-12345",
		},
		Server: config.ServerConfig{
			Listen:  fmt.Sprintf(":%d", b.testConfig.EdgeGateway.Port),
			Timeout: types.Duration(b.parseDuration(b.testConfig.EdgeGateway.Timeout)),
		},
		Redis: config.RedisConfig{
			Addr:     b.redisAddr,
			Password: b.testConfig.Redis.Password,
			DB:       b.testConfig.Redis.DB,
		},
		Storage: config.GlobalStorageConfig{
			BasePath:    cacheBasePath,
			Compression: types.CompressionSnappy, // Explicitly set compression for tests
		},
		Render: config.GlobalRenderConfig{
			Cache: types.RenderCacheConfig{
				TTL: ptrDuration(1 * time.Hour),
				Expired: &types.CacheExpiredConfig{
					Strategy: types.ExpirationStrategyServeStale,
					StaleTTL: ptrDuration(1 * time.Hour),
				},
			},
		},
		Bypass: config.GlobalBypassConfig{
			Timeout:        ptrDuration(b.parseDuration(b.testConfig.EdgeGateway.Bypass.Timeout)),
			UserAgent:      b.testConfig.EdgeGateway.Bypass.UserAgent,
			SSRFProtection: b.testConfig.EdgeGateway.Bypass.SSRFProtection,
			Cache: types.BypassCacheConfig{
				Enabled:     ptrBool(false),                // Global default: disabled
				TTL:         ptrDuration(30 * time.Minute), // Default: 30m
				StatusCodes: []int{200},                    // Default: cache only 200s
			},
		},
		Registry: config.EdgeRegistryConfig{
			SelectionStrategy: b.testConfig.EdgeGateway.Registry.SelectionStrategy,
		},
		Log: config.LogConfig{
			Level: b.testConfig.EdgeGateway.Log.Level,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  b.testConfig.EdgeGateway.Log.Format,
			},
		},
		Hosts: config.HostsIncludeConfig{
			Include: "hosts.d/",
		},
	}
}

// BuildRSConfig builds Render Service configuration
func (b *ConfigBuilder) BuildRSConfig() *config.RSConfig {
	return &config.RSConfig{
		Server: config.RSServerConfig{
			ID:     b.testConfig.RenderService.ID,
			Listen: b.testConfig.RenderService.Address,
		},
		Redis: config.RedisConfig{
			Addr:     b.redisAddr,
			Password: b.testConfig.Redis.Password,
			DB:       b.testConfig.Redis.DB,
		},
		Chrome: config.ChromeYAMLConfig{
			PoolSize: fmt.Sprintf("%d", b.testConfig.RenderService.Chrome.PoolSize),
			Warmup: config.WarmupConfig{
				URL:     b.testConfig.RenderService.Chrome.Warmup.URL,
				Timeout: types.Duration(b.parseDuration(b.testConfig.RenderService.Chrome.Warmup.Timeout)),
			},
			Restart: config.RestartConfig{
				AfterCount: b.testConfig.RenderService.Chrome.Restart.AfterCount,
				AfterTime:  types.Duration(b.parseDuration(b.testConfig.RenderService.Chrome.Restart.AfterTime)),
			},
			Render: config.RSRenderConfig{
				MaxTimeout: types.Duration(b.parseDuration(b.testConfig.RenderService.Chrome.Render.MaxTimeout)),
			},
		},
		Log: config.LogConfig{
			Level: b.testConfig.RenderService.Log.Level,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  b.testConfig.RenderService.Log.Format,
			},
		},
	}
}

// WriteTestConfigs writes Edge Gateway and Render Service configs to temp directory
// Also copies hosts.d/ directory from fixtures
func (b *ConfigBuilder) WriteTestConfigs(tempDir string) error {
	// Build configs
	egConfig := b.BuildEGConfig()
	rsConfig := b.BuildRSConfig()

	// Marshal to YAML
	egData, err := yaml.Marshal(egConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal EG config: %w", err)
	}

	rsData, err := yaml.Marshal(rsConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal RS config: %w", err)
	}

	// Write EG config
	egPath := filepath.Join(tempDir, "edge-gateway.yaml")
	if err := os.WriteFile(egPath, egData, 0644); err != nil {
		return fmt.Errorf("failed to write EG config: %w", err)
	}

	// Write RS config
	rsPath := filepath.Join(tempDir, "render-service.yaml")
	if err := os.WriteFile(rsPath, rsData, 0644); err != nil {
		return fmt.Errorf("failed to write RS config: %w", err)
	}

	// Copy hosts.d/ directory from fixtures
	hostsFixtureDir := filepath.Join("fixtures", "configs-local", "hosts.d")
	hostsDestDir := filepath.Join(tempDir, "hosts.d")

	// Create destination directory
	if err := os.MkdirAll(hostsDestDir, 0755); err != nil {
		return fmt.Errorf("failed to create hosts.d directory: %w", err)
	}

	// Read all host files from fixture directory
	hostFiles, err := filepath.Glob(filepath.Join(hostsFixtureDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to glob host files: %w", err)
	}

	// Copy each host file
	for _, srcFile := range hostFiles {
		data, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("failed to read host file %s: %w", srcFile, err)
		}

		destFile := filepath.Join(hostsDestDir, filepath.Base(srcFile))
		if err := os.WriteFile(destFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write host file %s: %w", destFile, err)
		}
	}

	return nil
}

// LoadHostsConfig loads hosts from hosts.d/ directory in fixtures for test validation
func LoadHostsConfig() (*config.HostsConfig, error) {
	hostsDir := filepath.Join("fixtures", "configs-local", "hosts.d")

	// Glob for all host files
	hostFiles, err := filepath.Glob(filepath.Join(hostsDir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob host files: %w", err)
	}

	if len(hostFiles) == 0 {
		return nil, fmt.Errorf("no host files found in %s", hostsDir)
	}

	// Load and merge all host files
	var allHosts []types.Host
	for _, file := range hostFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read host file %s: %w", file, err)
		}

		var hostsConfig config.HostsConfig
		if err := yaml.Unmarshal(data, &hostsConfig); err != nil {
			return nil, fmt.Errorf("failed to parse host file %s: %w", file, err)
		}

		allHosts = append(allHosts, hostsConfig.Hosts...)
	}

	return &config.HostsConfig{Hosts: allHosts}, nil
}

// GetHost gets a host by ID from hosts config
func GetHost(hostsConfig *config.HostsConfig, hostID int) (*types.Host, error) {
	for i := range hostsConfig.Hosts {
		if hostsConfig.Hosts[i].ID == hostID {
			return &hostsConfig.Hosts[i], nil
		}
	}
	return nil, fmt.Errorf("host with ID %d not found", hostID)
}

// parseDuration is a helper that safely parses duration strings
func (b *ConfigBuilder) parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		// Return 0 for invalid durations - config validation will catch this
		return 0
	}
	return d
}

// ptrDuration returns a pointer to a types.Duration value
func ptrDuration(d time.Duration) *types.Duration {
	v := types.Duration(d)
	return &v
}

// ptrInt returns a pointer to an int value
func ptrInt(i int) *int {
	return &i
}

// ptrBool returns a pointer to a bool value
func ptrBool(b bool) *bool {
	return &b
}

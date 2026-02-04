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

// buildEGConfigBase builds base Edge Gateway configuration with sharding support
func (b *ConfigBuilder) buildEGConfigBase(egConfig EdgeGatewayConfig) *config.EgConfig {
	// Convert cache base path to absolute path
	// Tests run from tests/acceptance/sharding/, so compute path from test directory
	cacheBasePath := egConfig.Storage.BasePath
	if !filepath.IsAbs(cacheBasePath) {
		// Get absolute path from current working directory (test directory)
		absCachePath, err := filepath.Abs(cacheBasePath)
		if err == nil {
			cacheBasePath = absCachePath
		}
	}

	cfg := &config.EgConfig{
		EgID: egConfig.EgID,
		Internal: configtypes.InternalConfig{
			Listen:  egConfig.Internal.Listen,
			AuthKey: "test-shared-secret-key-12345678",
		},
		Server: config.ServerConfig{
			Listen:  fmt.Sprintf(":%d", egConfig.Port),
			Timeout: types.Duration(b.parseDuration(egConfig.Timeout)),
		},
		Redis: config.RedisConfig{
			Addr:     b.redisAddr,
			Password: b.testConfig.Redis.Password,
			DB:       b.testConfig.Redis.DB,
		},
		Storage: config.GlobalStorageConfig{
			BasePath:    cacheBasePath,
			Compression: types.CompressionSnappy, // Enable compression for sharding tests
		},
		CacheSharding: &types.CacheShardingConfig{
			Enabled:              ptrBool(true),
			ReplicationFactor:    ptrInt(2),
			DistributionStrategy: "hash_modulo",
			PushOnRender:         ptrBool(true),
			ReplicateOnPull:      ptrBool(true),
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
			Timeout:   ptrDuration(b.parseDuration(egConfig.Bypass.Timeout)),
			UserAgent: egConfig.Bypass.UserAgent,
			Cache: types.BypassCacheConfig{
				Enabled:     ptrBool(false),                // Global default: disabled
				TTL:         ptrDuration(30 * time.Minute), // Default: 30m
				StatusCodes: []int{200},                    // Default: cache only 200s
			},
		},
		Registry: config.EdgeRegistryConfig{
			SelectionStrategy: egConfig.Registry.SelectionStrategy,
		},
		Log: config.LogConfig{
			Level: egConfig.Log.Level,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  egConfig.Log.Format,
			},
		},
		Hosts: config.HostsIncludeConfig{
			Include: "hosts.d/",
		},
	}
	return cfg
}

// BuildEG1Config builds Edge Gateway 1 configuration with sharding enabled
func (b *ConfigBuilder) BuildEG1Config() *config.EgConfig {
	return b.buildEGConfigBase(b.testConfig.EdgeGateway1)
}

// BuildEG2Config builds Edge Gateway 2 configuration with sharding enabled
func (b *ConfigBuilder) BuildEG2Config() *config.EgConfig {
	return b.buildEGConfigBase(b.testConfig.EdgeGateway2)
}

// BuildEG3Config builds Edge Gateway 3 configuration with sharding enabled
func (b *ConfigBuilder) BuildEG3Config() *config.EgConfig {
	return b.buildEGConfigBase(b.testConfig.EdgeGateway3)
}

// BuildEGConfig builds Edge Gateway 1 configuration (for backward compatibility)
func (b *ConfigBuilder) BuildEGConfig() *config.EgConfig {
	return b.BuildEG1Config()
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
// Creates separate config directories for each EG
func (b *ConfigBuilder) WriteTestConfigs(tempDir string) error {
	// Build configs
	eg1Config := b.BuildEG1Config()
	eg2Config := b.BuildEG2Config()
	eg3Config := b.BuildEG3Config()
	rsConfig := b.BuildRSConfig()

	// Marshal to YAML
	eg1Data, err := yaml.Marshal(eg1Config)
	if err != nil {
		return fmt.Errorf("failed to marshal EG1 config: %w", err)
	}

	eg2Data, err := yaml.Marshal(eg2Config)
	if err != nil {
		return fmt.Errorf("failed to marshal EG2 config: %w", err)
	}

	eg3Data, err := yaml.Marshal(eg3Config)
	if err != nil {
		return fmt.Errorf("failed to marshal EG3 config: %w", err)
	}

	rsData, err := yaml.Marshal(rsConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal RS config: %w", err)
	}

	// Create EG1 config directory
	eg1Dir := filepath.Join(tempDir, "eg1")
	if err := os.MkdirAll(eg1Dir, 0755); err != nil {
		return fmt.Errorf("failed to create EG1 directory: %w", err)
	}
	eg1Path := filepath.Join(eg1Dir, "edge-gateway.yaml")
	if err := os.WriteFile(eg1Path, eg1Data, 0644); err != nil {
		return fmt.Errorf("failed to write EG1 config: %w", err)
	}

	// Create EG2 config directory
	eg2Dir := filepath.Join(tempDir, "eg2")
	if err := os.MkdirAll(eg2Dir, 0755); err != nil {
		return fmt.Errorf("failed to create EG2 directory: %w", err)
	}
	eg2Path := filepath.Join(eg2Dir, "edge-gateway.yaml")
	if err := os.WriteFile(eg2Path, eg2Data, 0644); err != nil {
		return fmt.Errorf("failed to write EG2 config: %w", err)
	}

	// Create EG3 config directory
	eg3Dir := filepath.Join(tempDir, "eg3")
	if err := os.MkdirAll(eg3Dir, 0755); err != nil {
		return fmt.Errorf("failed to create EG3 directory: %w", err)
	}
	eg3Path := filepath.Join(eg3Dir, "edge-gateway.yaml")
	if err := os.WriteFile(eg3Path, eg3Data, 0644); err != nil {
		return fmt.Errorf("failed to write EG3 config: %w", err)
	}

	// Write RS config in root temp dir
	rsPath := filepath.Join(tempDir, "render-service.yaml")
	if err := os.WriteFile(rsPath, rsData, 0644); err != nil {
		return fmt.Errorf("failed to write RS config: %w", err)
	}

	// Copy hosts.d/ directory to each EG config dir
	hostsFixtureDir := filepath.Join("fixtures", "configs-local", "hosts.d")

	// Copy to EG1
	eg1HostsDir := filepath.Join(eg1Dir, "hosts.d")
	if err := b.copyHostsDir(hostsFixtureDir, eg1HostsDir); err != nil {
		return fmt.Errorf("failed to copy hosts.d to EG1: %w", err)
	}

	// Copy to EG2
	eg2HostsDir := filepath.Join(eg2Dir, "hosts.d")
	if err := b.copyHostsDir(hostsFixtureDir, eg2HostsDir); err != nil {
		return fmt.Errorf("failed to copy hosts.d to EG2: %w", err)
	}

	// Copy to EG3
	eg3HostsDir := filepath.Join(eg3Dir, "hosts.d")
	if err := b.copyHostsDir(hostsFixtureDir, eg3HostsDir); err != nil {
		return fmt.Errorf("failed to copy hosts.d to EG3: %w", err)
	}

	return nil
}

// copyHostsDir copies hosts.d directory
func (b *ConfigBuilder) copyHostsDir(srcDir, destDir string) error {
	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create hosts.d directory: %w", err)
	}

	// Read all host files from source directory
	hostFiles, err := filepath.Glob(filepath.Join(srcDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to glob host files: %w", err)
	}

	// Copy each host file
	for _, srcFile := range hostFiles {
		data, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("failed to read host file %s: %w", srcFile, err)
		}

		destFile := filepath.Join(destDir, filepath.Base(srcFile))
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

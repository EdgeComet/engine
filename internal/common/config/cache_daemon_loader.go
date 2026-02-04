package config

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/yamlutil"
)

// applyDaemonDefaults applies default values to daemon configuration
func applyDaemonDefaults(config *configtypes.CacheDaemonConfig) {
	// Apply log configuration defaults
	// If both outputs are disabled (zero values), enable console by default
	if !config.Logging.Console.Enabled && !config.Logging.File.Enabled {
		config.Logging.Console.Enabled = true
	}

	// Set format defaults if not specified
	if config.Logging.Console.Format == "" {
		config.Logging.Console.Format = configtypes.LogFormatConsole
	}

	if config.Logging.File.Format == "" {
		config.Logging.File.Format = configtypes.LogFormatText
	}
}

// LoadCacheDaemonConfig loads cache-daemon configuration from YAML file
func LoadCacheDaemonConfig(path string, logger *zap.Logger) (*configtypes.CacheDaemonConfig, error) {
	logger.Info("Loading cache-daemon configuration", zap.String("path", path))

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file does not exist: %s", path)
	}

	// Read config file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var config configtypes.CacheDaemonConfig
	if err := yamlutil.UnmarshalStrict(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Apply defaults to configuration
	applyDaemonDefaults(&config)

	logger.Info("Cache-daemon configuration loaded successfully",
		zap.String("daemon_id", config.DaemonID),
		zap.String("eg_config", config.EgConfig),
		zap.String("redis_addr", config.Redis.Addr))

	return &config, nil
}

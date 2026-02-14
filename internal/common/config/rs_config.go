package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/yamlutil"
	"github.com/edgecomet/engine/pkg/types"
)

// RSConfig represents Render Service configuration
type RSConfig struct {
	Server  RSServerConfig            `yaml:"server"`
	Redis   RedisConfig               `yaml:"redis"`
	Chrome  ChromeYAMLConfig          `yaml:"chrome"`
	Log     LogConfig                 `yaml:"log"`
	Metrics configtypes.MetricsConfig `yaml:"metrics"`
}

// RSServerConfig represents RS server configuration
type RSServerConfig struct {
	ID     string `yaml:"id"`
	Listen string `yaml:"listen"`
}

// ChromeYAMLConfig represents Chrome configuration for YAML
type ChromeYAMLConfig struct {
	PoolSize string         `yaml:"pool_size"`
	Warmup   WarmupConfig   `yaml:"warmup"`
	Restart  RestartConfig  `yaml:"restart"`
	Render   RSRenderConfig `yaml:"render"`
}

// WarmupConfig represents Chrome warmup configuration
type WarmupConfig struct {
	URL     string         `yaml:"url"`
	Timeout types.Duration `yaml:"timeout"`
}

// RestartConfig represents Chrome restart policy configuration
type RestartConfig struct {
	AfterCount int            `yaml:"after_count"`
	AfterTime  types.Duration `yaml:"after_time"`
}

const (
	// SafetyMargin is the buffer added to max_timeout for server timeout calculation
	// This ensures FastHTTP doesn't kill connections before render completes
	SafetyMargin = 10 * time.Second

	defaultRestartAfterCount = 100
	defaultRestartAfterTime  = 60 * time.Minute
)

// RSRenderConfig represents rendering timeout configuration for Render Service
type RSRenderConfig struct {
	MaxTimeout types.Duration `yaml:"max_timeout"` // maximum render timeout - cancels stuck renders
}

// CalculateServerTimeout returns the FastHTTP server timeout
// Server timeout = max_timeout + SafetyMargin
func (r *RSRenderConfig) CalculateServerTimeout() time.Duration {
	return time.Duration(r.MaxTimeout) + SafetyMargin
}

// ToInternalConfig converts ChromeYAMLConfig to chrome.Config
// Import chrome package to use this properly
func (c *ChromeYAMLConfig) ToInternalConfig() interface{} {
	// Returns a map that can be used to initialize chrome.Config
	return map[string]interface{}{
		"pool_size":           c.PoolSize,
		"warmup_url":          c.Warmup.URL,
		"warmup_timeout":      c.Warmup.Timeout,
		"restart_after_count": c.Restart.AfterCount,
		"restart_after_time":  c.Restart.AfterTime,
	}
}

// RSConfigManager handles RS configuration
type RSConfigManager struct {
	config     *RSConfig
	configPath string
	logger     *zap.Logger
}

// NewRSConfigManager creates a new RS config manager
func NewRSConfigManager(configPath string, logger *zap.Logger) (*RSConfigManager, error) {
	cm := &RSConfigManager{
		configPath: configPath,
		logger:     logger,
	}

	if err := cm.LoadConfig(); err != nil {
		return nil, err
	}

	return cm, nil
}

// LoadConfig loads configuration from file
func (cm *RSConfigManager) LoadConfig() error {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var cfg RSConfig
	if err := yamlutil.UnmarshalStrict(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	cm.config = &cfg

	// Apply defaults before validation
	cm.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return nil
}

// GetConfig returns the current configuration
func (cm *RSConfigManager) GetConfig() *RSConfig {
	return cm.config
}

// applyDefaults applies default values to configuration
func (cm *RSConfigManager) applyDefaults() {
	cm.config.applyDefaults()
}

// applyDefaults applies default values to configuration fields
func (cfg *RSConfig) applyDefaults() {
	// Apply log configuration defaults
	// If both outputs are disabled (zero values), enable console by default
	if !cfg.Log.Console.Enabled && !cfg.Log.File.Enabled {
		cfg.Log.Console.Enabled = true
	}

	// Set format defaults if not specified
	if cfg.Log.Console.Format == "" {
		cfg.Log.Console.Format = configtypes.LogFormatConsole
	}

	if cfg.Log.File.Format == "" {
		cfg.Log.File.Format = configtypes.LogFormatText
	}

	// Chrome restart defaults
	if cfg.Chrome.Restart.AfterCount == 0 {
		cfg.Chrome.Restart.AfterCount = defaultRestartAfterCount
	}

	if cfg.Chrome.Restart.AfterTime == 0 {
		cfg.Chrome.Restart.AfterTime = types.Duration(defaultRestartAfterTime)
	}
}

// Validate checks configuration validity
func (cfg *RSConfig) Validate() error {
	// Server validation
	if cfg.Server.ID == "" {
		return fmt.Errorf("server.id is required")
	}

	if cfg.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	} else if err := configtypes.ValidateListenAddress(cfg.Server.Listen); err != nil {
		return fmt.Errorf("invalid server.listen: %w", err)
	}

	// Redis validation
	if cfg.Redis.Addr == "" {
		return fmt.Errorf("redis.addr is required")
	}

	// Chrome validation
	if cfg.Chrome.PoolSize != "auto" {
		size, err := strconv.Atoi(cfg.Chrome.PoolSize)
		if err != nil || size <= 0 {
			return fmt.Errorf("chrome.pool_size must be 'auto' or positive integer")
		}
	}

	if cfg.Chrome.Warmup.URL == "" {
		return fmt.Errorf("chrome.warmup.url is required")
	}

	if cfg.Chrome.Warmup.Timeout <= 0 {
		return fmt.Errorf("chrome.warmup.timeout must be positive")
	}

	if cfg.Chrome.Restart.AfterCount <= 0 {
		return fmt.Errorf("chrome.restart.after_count must be positive")
	}

	if cfg.Chrome.Restart.AfterTime <= 0 {
		return fmt.Errorf("chrome.restart.after_time must be positive")
	}

	// Render validation
	if cfg.Chrome.Render.MaxTimeout <= 0 {
		return fmt.Errorf("chrome.render.max_timeout must be positive")
	}

	// Log validation
	validLogLevels := map[string]bool{
		configtypes.LogLevelDebug:  true,
		configtypes.LogLevelInfo:   true,
		configtypes.LogLevelWarn:   true,
		configtypes.LogLevelError:  true,
		configtypes.LogLevelDPanic: true,
		configtypes.LogLevelPanic:  true,
		configtypes.LogLevelFatal:  true,
	}
	if !validLogLevels[cfg.Log.Level] {
		return fmt.Errorf("invalid log.level: %s (must be debug, info, warn, error, dpanic, panic, or fatal)", cfg.Log.Level)
	}

	validConsoleFormats := map[string]bool{
		configtypes.LogFormatJSON:    true,
		configtypes.LogFormatConsole: true,
	}

	// Validate console format
	if cfg.Log.Console.Enabled && cfg.Log.Console.Format != "" && !validConsoleFormats[cfg.Log.Console.Format] {
		return fmt.Errorf("invalid log.console.format: %s (must be json or console)", cfg.Log.Console.Format)
	}

	// Validate file logging
	if cfg.Log.File.Enabled {
		if cfg.Log.File.Path == "" {
			return fmt.Errorf("log.file.path must be specified when file logging is enabled")
		}

		validFileFormats := map[string]bool{
			configtypes.LogFormatJSON: true,
			configtypes.LogFormatText: true,
		}
		if cfg.Log.File.Format != "" && !validFileFormats[cfg.Log.File.Format] {
			return fmt.Errorf("invalid log.file.format: %s (must be json or text)", cfg.Log.File.Format)
		}

		// Validate rotation parameters
		if cfg.Log.File.Rotation.MaxSize < 0 {
			return fmt.Errorf("log.file.rotation.max_size must be >= 0, got %d", cfg.Log.File.Rotation.MaxSize)
		}
		if cfg.Log.File.Rotation.MaxAge < 0 {
			return fmt.Errorf("log.file.rotation.max_age must be >= 0, got %d", cfg.Log.File.Rotation.MaxAge)
		}
		if cfg.Log.File.Rotation.MaxBackups < 0 {
			return fmt.Errorf("log.file.rotation.max_backups must be >= 0, got %d", cfg.Log.File.Rotation.MaxBackups)
		}
	}

	// Metrics validation
	if cfg.Metrics.Enabled {
		if cfg.Metrics.Listen == "" {
			return fmt.Errorf("metrics.listen is required when metrics enabled")
		} else if err := configtypes.ValidateListenAddress(cfg.Metrics.Listen); err != nil {
			return fmt.Errorf("invalid metrics.listen: %w", err)
		}

		// Validate metrics.listen port differs from server.listen port when metrics enabled
		metricsPort, err1 := configtypes.GetPortFromListen(cfg.Metrics.Listen)
		serverPort, err2 := configtypes.GetPortFromListen(cfg.Server.Listen)
		if err1 == nil && err2 == nil && metricsPort == serverPort {
			return fmt.Errorf("metrics.listen port (%d) must differ from server.listen port (%d) when metrics enabled", metricsPort, serverPort)
		}
	}

	if cfg.Metrics.Path != "" && !strings.HasPrefix(cfg.Metrics.Path, "/") {
		return fmt.Errorf("invalid metrics.path: %s (must start with /)", cfg.Metrics.Path)
	}

	if cfg.Metrics.Namespace != "" {
		// Prometheus namespace must match: [a-zA-Z_][a-zA-Z0-9_]*
		if matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_]*$`, cfg.Metrics.Namespace); !matched {
			return fmt.Errorf("invalid metrics.namespace: %s (must match [a-zA-Z_][a-zA-Z0-9_]*)", cfg.Metrics.Namespace)
		}
	}

	return nil
}

// LoadRSConfig loads RS configuration from a file
func LoadRSConfig(configPath string) (*RSConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg RSConfig
	if err := yamlutil.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// GetConfigPath resolves the config file path
func GetConfigPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config path cannot be empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve config path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("config file does not exist: %s", absPath)
	}

	return absPath, nil
}

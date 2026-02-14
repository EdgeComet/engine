package configtypes

import (
	"fmt"
	"time"

	"github.com/edgecomet/engine/pkg/types"
)

// CacheDaemonConfig is the root configuration for cache-daemon service
type CacheDaemonConfig struct {
	EgConfig      string                   `yaml:"eg_config"`      // Path to EG config file (for hosts)
	DaemonID      string                   `yaml:"daemon_id"`      // Unique identifier for this daemon instance
	Redis         RedisConfig              `yaml:"redis"`          // Redis connection configuration
	Scheduler     CacheDaemonScheduler     `yaml:"scheduler"`      // Scheduler configuration
	InternalQueue CacheDaemonInternalQueue `yaml:"internal_queue"` // Internal queue configuration
	Recache       CacheDaemonRecache       `yaml:"recache"`        // Recache behavior configuration
	HTTPApi       CacheDaemonHTTPApi       `yaml:"http_api"`       // HTTP API configuration
	Logging       CacheDaemonLogging       `yaml:"logging"`        // Logging configuration
	Metrics       MetricsConfig            `yaml:"metrics"`        // Metrics configuration
}

// CacheDaemonScheduler defines scheduler timing configuration
type CacheDaemonScheduler struct {
	TickInterval        types.Duration `yaml:"tick_interval"`         // How often scheduler runs (min: 100ms, e.g., 1s)
	NormalCheckInterval types.Duration `yaml:"normal_check_interval"` // How often to check normal/autorecache queues (e.g., 60s)
}

// CacheDaemonInternalQueue defines internal queue configuration
type CacheDaemonInternalQueue struct {
	MaxSize        int            `yaml:"max_size"`         // Maximum entries in internal queue (e.g., 1000)
	MaxRetries     int            `yaml:"max_retries"`      // Maximum retry attempts before discarding (e.g., 3)
	RetryBaseDelay types.Duration `yaml:"retry_base_delay"` // Base delay for exponential backoff (e.g., 5s for production, 100ms for tests; 0 = use default 5s)
}

// CacheDaemonRecache defines recache behavior configuration
type CacheDaemonRecache struct {
	RSCapacityReserved float64        `yaml:"rs_capacity_reserved"` // Percentage of RS capacity reserved for online traffic (0.0-1.0, e.g., 0.30)
	TimeoutPerURL      types.Duration `yaml:"timeout_per_url"`      // Timeout for each URL recache request (e.g., 60s)
}

// CacheDaemonHTTPApi defines HTTP API configuration
type CacheDaemonHTTPApi struct {
	Enabled             bool           `yaml:"enabled"`               // Enable/disable HTTP API
	Listen              string         `yaml:"listen"`                // Listen address (e.g., ":10090")
	RequestTimeout      types.Duration `yaml:"request_timeout"`       // Timeout for incoming API requests (e.g., 30s)
	SchedulerControlAPI bool           `yaml:"scheduler_control_api"` // Enable scheduler pause/resume API (for testing)
}

// CacheDaemonLogging defines logging configuration (type alias for consistency)
type CacheDaemonLogging = LogConfig

// Validate validates cache daemon configuration
func (c *CacheDaemonConfig) Validate() error {
	if c == nil {
		return nil
	}

	// Validate eg_config path is specified
	if c.EgConfig == "" {
		return fmt.Errorf("eg_config must be specified")
	}

	// Validate daemon_id is specified
	if c.DaemonID == "" {
		return fmt.Errorf("daemon_id must be specified")
	}

	// Validate Redis configuration
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis.addr must be specified")
	}
	if c.Redis.DB < 0 {
		return fmt.Errorf("redis.db must be >= 0, got %d", c.Redis.DB)
	}

	// Validate tick_interval >= 100ms (allow faster ticks for tests and high-throughput scenarios)
	tickInterval := time.Duration(c.Scheduler.TickInterval)
	if tickInterval < 100*time.Millisecond {
		return fmt.Errorf("scheduler.tick_interval must be >= 100ms, got %v", tickInterval)
	}

	// Validate normal_check_interval is multiple of tick_interval
	normalCheckInterval := time.Duration(c.Scheduler.NormalCheckInterval)
	if normalCheckInterval%tickInterval != 0 {
		return fmt.Errorf("scheduler.normal_check_interval (%v) must be a multiple of tick_interval (%v)", normalCheckInterval, tickInterval)
	}

	// Validate max_size > 0
	if c.InternalQueue.MaxSize <= 0 {
		return fmt.Errorf("internal_queue.max_size must be > 0, got %d", c.InternalQueue.MaxSize)
	}

	// Validate max_retries >= 1
	if c.InternalQueue.MaxRetries < 1 {
		return fmt.Errorf("internal_queue.max_retries must be >= 1, got %d", c.InternalQueue.MaxRetries)
	}

	// Validate rs_capacity_reserved between 0.0 and 1.0
	if c.Recache.RSCapacityReserved < 0.0 || c.Recache.RSCapacityReserved > 1.0 {
		return fmt.Errorf("recache.rs_capacity_reserved must be between 0.0 and 1.0, got %f", c.Recache.RSCapacityReserved)
	}

	// Validate timeout_per_url > 0
	if time.Duration(c.Recache.TimeoutPerURL) <= 0 {
		return fmt.Errorf("recache.timeout_per_url must be > 0")
	}

	// Validate HTTP API configuration
	var httpApiPort int
	if c.HTTPApi.Enabled {
		if c.HTTPApi.Listen == "" {
			return fmt.Errorf("http_api.listen must be specified when enabled")
		}
		port, err := GetPortFromListen(c.HTTPApi.Listen)
		if err != nil {
			return fmt.Errorf("invalid http_api.listen: %w", err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("http_api.listen port must be between 1 and 65535, got %d", port)
		}
		httpApiPort = port

		if time.Duration(c.HTTPApi.RequestTimeout) <= 0 {
			return fmt.Errorf("http_api.request_timeout must be > 0 when http_api is enabled")
		}
	}

	// Validate metrics configuration
	var metricsPort int
	if c.Metrics.Enabled {
		if c.Metrics.Listen == "" {
			return fmt.Errorf("metrics.listen must be specified when enabled")
		}
		port, err := GetPortFromListen(c.Metrics.Listen)
		if err != nil {
			return fmt.Errorf("invalid metrics.listen: %w", err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("metrics.listen port must be between 1 and 65535, got %d", port)
		}
		metricsPort = port
	}

	// Validate metrics.listen port differs from http_api.listen port when both enabled
	if c.Metrics.Enabled && c.HTTPApi.Enabled && metricsPort == httpApiPort {
		return fmt.Errorf("metrics.listen port (%d) must differ from http_api.listen port (%d) when both enabled", metricsPort, httpApiPort)
	}

	// Validate log level
	validLogLevels := map[string]bool{
		LogLevelDebug: true,
		LogLevelInfo:  true,
		LogLevelWarn:  true,
		LogLevelError: true,
	}
	if c.Logging.Level != "" && !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error, got '%s'", c.Logging.Level)
	}

	// Validate console format
	validConsoleFormats := map[string]bool{
		LogFormatJSON:    true,
		LogFormatConsole: true,
	}
	if c.Logging.Console.Enabled && c.Logging.Console.Format != "" && !validConsoleFormats[c.Logging.Console.Format] {
		return fmt.Errorf("logging.console.format must be 'json' or 'console', got '%s'", c.Logging.Console.Format)
	}

	// Validate file logging configuration
	if c.Logging.File.Enabled {
		if c.Logging.File.Path == "" {
			return fmt.Errorf("logging.file.path must be specified when file logging is enabled")
		}

		validFileFormats := map[string]bool{
			LogFormatJSON: true,
			LogFormatText: true,
		}
		if c.Logging.File.Format != "" && !validFileFormats[c.Logging.File.Format] {
			return fmt.Errorf("logging.file.format must be 'json' or 'text', got '%s'", c.Logging.File.Format)
		}

		// Validate rotation parameters
		if c.Logging.File.Rotation.MaxSize < 0 {
			return fmt.Errorf("logging.file.rotation.max_size must be >= 0, got %d", c.Logging.File.Rotation.MaxSize)
		}
		if c.Logging.File.Rotation.MaxAge < 0 {
			return fmt.Errorf("logging.file.rotation.max_age must be >= 0, got %d", c.Logging.File.Rotation.MaxAge)
		}
		if c.Logging.File.Rotation.MaxBackups < 0 {
			return fmt.Errorf("logging.file.rotation.max_backups must be >= 0, got %d", c.Logging.File.Rotation.MaxBackups)
		}
	}

	return nil
}

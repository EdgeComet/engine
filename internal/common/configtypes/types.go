package configtypes

import (
	"github.com/edgecomet/engine/pkg/types"
)

// Log level constants
const (
	LogLevelDebug  = "debug"
	LogLevelInfo   = "info"
	LogLevelWarn   = "warn"
	LogLevelError  = "error"
	LogLevelDPanic = "dpanic"
	LogLevelPanic  = "panic"
	LogLevelFatal  = "fatal"
)

// Log format constants
const (
	LogFormatJSON    = "json"
	LogFormatConsole = "console"
	LogFormatText    = "text"
)

// EgConfig represents Edge Gateway main application configuration
type EgConfig struct {
	Server         ServerConfig                `yaml:"server"`
	Redis          RedisConfig                 `yaml:"redis"`
	Storage        GlobalStorageConfig         `yaml:"storage"`
	Render         GlobalRenderConfig          `yaml:"render"`
	Bypass         GlobalBypassConfig          `yaml:"bypass"`
	Registry       EdgeRegistryConfig          `yaml:"registry"`
	Log            LogConfig                   `yaml:"log"`
	Metrics        MetricsConfig               `yaml:"metrics"`
	TrackingParams *types.TrackingParamsConfig `yaml:"tracking_params,omitempty"`
	BothitRecache  *types.BothitRecacheConfig  `yaml:"bothit_recache,omitempty"`
	Hosts          HostsIncludeConfig          `yaml:"hosts"`
	CacheSharding  *CacheShardingConfig        `yaml:"cache_sharding,omitempty"`
	Headers        *types.HeadersConfig        `yaml:"headers,omitempty"`
	ClientIP       *types.ClientIPConfig       `yaml:"client_ip,omitempty"`
	EventLogging   *EventLoggingConfig         `yaml:"event_logging,omitempty"`
	EgID           string                      `yaml:"eg_id,omitempty"`
	Internal       InternalConfig              `yaml:"internal"`
}

// InternalConfig configures internal server for inter-EG and daemon communication
type InternalConfig struct {
	Listen  string `yaml:"listen"`
	AuthKey string `yaml:"auth_key"`
}

// TLSConfig holds TLS/HTTPS configuration for the external server
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Listen   string `yaml:"listen"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type ServerConfig struct {
	Listen  string         `yaml:"listen"`
	Timeout types.Duration `yaml:"timeout"`
	TLS     TLSConfig      `yaml:"tls"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type GlobalStorageConfig struct {
	BasePath    string         `yaml:"base_path"`
	Cleanup     *CleanupConfig `yaml:"cleanup,omitempty"`
	Compression string         `yaml:"compression,omitempty"` // Compression algorithm: none, snappy, lz4
}

type CleanupConfig struct {
	Enabled      bool           `yaml:"enabled"`
	Interval     types.Duration `yaml:"interval"`
	SafetyMargin types.Duration `yaml:"safety_margin"`
}

type GlobalRenderConfig struct {
	Cache                types.RenderCacheConfig    `yaml:"cache"`
	Events               types.RenderEvents         `yaml:"events,omitempty"`
	UnmatchedDimension   string                     `yaml:"unmatched_dimension,omitempty"`
	Dimensions           map[string]types.Dimension `yaml:"dimensions,omitempty"`
	BlockedResourceTypes []string                   `yaml:"blocked_resource_types,omitempty"`
	BlockedPatterns      []string                   `yaml:"blocked_patterns,omitempty"`
	StripScripts         *bool                      `yaml:"strip_scripts,omitempty"`
}

type GlobalBypassConfig struct {
	Timeout        *types.Duration         `yaml:"timeout,omitempty"`
	UserAgent      string                  `yaml:"user_agent"`
	Cache          types.BypassCacheConfig `yaml:"cache"`            // Global bypass cache defaults
	SSRFProtection *bool                   `yaml:"ssrf_protection,omitempty"` // Block requests to private IPs (default: true)
}

type EdgeRegistryConfig struct {
	SelectionStrategy string `yaml:"selection_strategy"` // Default: "least_loaded"
}

type LogConfig struct {
	Level   string           `yaml:"level"`
	Console ConsoleLogConfig `yaml:"console"`
	File    FileLogConfig    `yaml:"file"`
}

type ConsoleLogConfig struct {
	Enabled bool   `yaml:"enabled"`
	Format  string `yaml:"format"`
	Level   string `yaml:"level,omitempty"`
}

type FileLogConfig struct {
	Enabled  bool           `yaml:"enabled"`
	Path     string         `yaml:"path"`
	Format   string         `yaml:"format"`
	Level    string         `yaml:"level,omitempty"`
	Rotation RotationConfig `yaml:"rotation"`
}

type RotationConfig struct {
	MaxSize    int  `yaml:"max_size"`
	MaxAge     int  `yaml:"max_age"`
	MaxBackups int  `yaml:"max_backups"`
	Compress   bool `yaml:"compress"`
}

type MetricsConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Listen    string `yaml:"listen"`
	Path      string `yaml:"path"`
	Namespace string `yaml:"namespace"`
}

// HostsIncludeConfig specifies where to load host configurations from
type HostsIncludeConfig struct {
	Include string `yaml:"include"`
}

// HostsConfig represents host configuration file
type HostsConfig struct {
	Hosts []types.Host `yaml:"hosts"`
}

// Type alias for CacheShardingConfig
type CacheShardingConfig = types.CacheShardingConfig

// EventLoggingConfig configures request event logging
type EventLoggingConfig struct {
	File EventFileConfig `yaml:"file"`
}

// EventFileConfig configures file-based event logging
type EventFileConfig struct {
	Enabled  bool           `yaml:"enabled"`
	Path     string         `yaml:"path"`
	Template string         `yaml:"template"`
	Rotation RotationConfig `yaml:"rotation"`
}

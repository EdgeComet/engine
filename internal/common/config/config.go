package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/yamlutil"
	"github.com/edgecomet/engine/internal/edge/validate"
	"github.com/edgecomet/engine/pkg/types"
)

// Type aliases for backward compatibility
type (
	EgConfig            = configtypes.EgConfig
	ServerConfig        = configtypes.ServerConfig
	RedisConfig         = configtypes.RedisConfig
	GlobalStorageConfig = configtypes.GlobalStorageConfig
	GlobalRenderConfig  = configtypes.GlobalRenderConfig
	GlobalBypassConfig  = configtypes.GlobalBypassConfig
	EdgeRegistryConfig  = configtypes.EdgeRegistryConfig
	LogConfig           = configtypes.LogConfig
	HostsIncludeConfig  = configtypes.HostsIncludeConfig
	HostsConfig         = configtypes.HostsConfig
)

// Compile-time interface satisfaction check
var _ configtypes.EGConfigManager = (*EGConfigManager)(nil)

// hostsCache holds cached hosts data for thread-safe O(1) domain lookup
type hostsCache struct {
	hosts    []types.Host
	byDomain map[string]*types.Host // lowercase domain -> host pointer
}

// EGConfigManager handles configuration loading
type EGConfigManager struct {
	config     *EgConfig
	hosts      *HostsConfig
	cache      atomic.Pointer[hostsCache]
	configPath string
	logger     *zap.Logger
}

// buildHostsCache creates a hostsCache from a hosts slice for O(1) domain lookup
func buildHostsCache(hosts []types.Host) *hostsCache {
	cache := &hostsCache{
		hosts:    hosts,
		byDomain: make(map[string]*types.Host),
	}
	for i := range hosts {
		for _, domain := range hosts[i].Domains {
			cache.byDomain[strings.ToLower(domain)] = &hosts[i]
		}
	}
	return cache
}

func NewEGConfigManager(configPath string, logger *zap.Logger) (*EGConfigManager, error) {
	cm := &EGConfigManager{
		configPath: configPath,
		logger:     logger,
	}

	if err := cm.LoadConfig(); err != nil {
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	return cm, nil
}

// LoadConfig loads configuration from files
func (cm *EGConfigManager) LoadConfig() error {
	// Validate configuration files first
	result, err := validate.ValidateConfiguration(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to validate config: %w", err)
	}

	// Check for validation errors
	if !result.Valid {
		// Convert validation errors to runtime error
		return cm.formatValidationErrors(result.Errors)
	}

	// Load main config
	if err := cm.loadMainConfig(cm.configPath); err != nil {
		return fmt.Errorf("failed to load main config: %w", err)
	}

	// Expand bot aliases and compile user agent patterns for global bothit_recache
	if cm.config.BothitRecache != nil && len(cm.config.BothitRecache.MatchUA) > 0 {
		expanded, err := ExpandBotAliases(cm.config.BothitRecache.MatchUA, "global config")
		if err != nil {
			return fmt.Errorf("failed to expand bot aliases in global bothit_recache: %w", err)
		}
		cm.config.BothitRecache.MatchUA = expanded

		// cm.logger.Debug("Expanded bot aliases in global bothit_recache",
		//	zap.String("config_path", cm.configPath),
		//	zap.Int("pattern_count", len(cm.config.BothitRecache.MatchUA)))
	}

	// Compile user agent patterns for global bothit_recache
	if cm.config.BothitRecache != nil {
		if err := cm.config.BothitRecache.CompileMatchUAPatterns(); err != nil {
			return fmt.Errorf("global bothit_recache: %w", err)
		}
	}

	// Expand bot aliases in global dimensions
	if err := ExpandDimensionAliases(cm.config.Render.Dimensions, cm.configPath, cm.logger); err != nil {
		return fmt.Errorf("failed to expand bot aliases in global config: %w", err)
	}

	// cm.logger.Debug("Expanded bot aliases in global configuration",
	//	zap.String("config_path", cm.configPath),
	//	zap.Int("dimension_count", len(cm.config.Render.Dimensions)))

	// Compile user agent patterns for global dimensions
	if err := cm.compileGlobalDimensions(); err != nil {
		return fmt.Errorf("failed to compile global dimensions: %w", err)
	}

	// Apply default global events if not specified (before host loading for inheritance)
	if cm.config.Render.Events.WaitFor == "" {
		cm.config.Render.Events.WaitFor = types.LifecycleEventNetworkIdle
	}
	if cm.config.Render.Events.AdditionalWait == nil {
		zeroDuration := types.Duration(0)
		cm.config.Render.Events.AdditionalWait = &zeroDuration
	}

	// Load hosts config using include pattern
	if err := cm.loadHostsFromInclude(); err != nil {
		return fmt.Errorf("failed to load hosts config: %w", err)
	}

	// Apply defaults to configuration
	cm.applyDefaults()

	// Sanity check: global unmatched_dimension must be non-empty after defaults
	if cm.config.Render.UnmatchedDimension == "" {
		return fmt.Errorf("INTERNAL ERROR: render.unmatched_dimension is empty after applying defaults")
	}

	// Build and store thread-safe hosts cache for O(1) domain lookup
	cache := buildHostsCache(cm.hosts.Hosts)
	cm.cache.Store(cache)

	// Emit runtime warnings (non-validation concerns)
	cm.emitConfigWarnings()

	return nil
}

// loadMainConfig loads main configuration from YAML file
func (cm *EGConfigManager) loadMainConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var config EgConfig
	if err := yamlutil.UnmarshalStrict(data, &config); err != nil {
		return err
	}

	cm.config = &config
	return nil
}

// loadHostsFromInclude loads hosts configuration from files matching the include pattern
func (cm *EGConfigManager) loadHostsFromInclude() error {
	if cm.config.Hosts.Include == "" {
		return fmt.Errorf("hosts.include is required in configuration")
	}

	// Resolve include path (relative to config directory)
	includePath := cm.config.Hosts.Include
	if !filepath.IsAbs(includePath) {
		configDir := filepath.Dir(cm.configPath)
		includePath = filepath.Join(configDir, includePath)
	}

	// Check if it's a directory or glob pattern
	fileInfo, err := os.Stat(includePath)
	if err == nil && fileInfo.IsDir() {
		// It's a directory - append /*.yaml pattern
		includePath = filepath.Join(includePath, "*.yaml")
	}

	// Glob for matching files
	files, err := filepath.Glob(includePath)
	if err != nil {
		return fmt.Errorf("invalid glob pattern '%s': %w", cm.config.Hosts.Include, err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no host files found matching pattern '%s'", cm.config.Hosts.Include)
	}

	// Sort files for deterministic loading order
	sort.Strings(files)

	// Load and merge all host files
	var allHosts []types.Host
	seenIDs := make(map[int]string) // Track host IDs to detect duplicates

	for _, file := range files {
		hosts, err := cm.loadHostsFile(file)
		if err != nil {
			return fmt.Errorf("failed to load host file '%s': %w", file, err)
		}

		// Check for duplicate host IDs
		for _, host := range hosts {
			if existingFile, exists := seenIDs[host.ID]; exists {
				return fmt.Errorf("duplicate host ID %d found in '%s' (already defined in '%s')", host.ID, file, existingFile)
			}
			seenIDs[host.ID] = file
		}

		allHosts = append(allHosts, hosts...)
	}

	cm.hosts = &HostsConfig{Hosts: allHosts}

	cm.logger.Info("Loaded hosts from include pattern",
		zap.String("pattern", cm.config.Hosts.Include),
		zap.Int("files_loaded", len(files)),
		zap.Int("total_hosts", len(allHosts)),
	)

	return nil
}

// loadHostsFile loads hosts from a single YAML file
func (cm *EGConfigManager) loadHostsFile(path string) ([]types.Host, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var hostsConfig HostsConfig
	if err := yamlutil.UnmarshalStrict(data, &hostsConfig); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	// Process each host: apply inheritance, expand aliases, compile patterns, sort URL rules
	for i := range hostsConfig.Hosts {
		host := &hostsConfig.Hosts[i]
		contextPath := fmt.Sprintf("%s:host_id=%d", path, host.ID)

		if err := PrepareHost(host, &cm.config.Render, contextPath, cm.logger); err != nil {
			return nil, fmt.Errorf("host '%s': %w", host.Domain, err)
		}
	}

	return hostsConfig.Hosts, nil
}

// compileGlobalDimensions compiles user agent patterns for global dimensions
func (cm *EGConfigManager) compileGlobalDimensions() error {
	if len(cm.config.Render.Dimensions) == 0 {
		return nil
	}

	for dimensionName, dimension := range cm.config.Render.Dimensions {
		if err := dimension.CompileMatchUAPatterns(); err != nil {
			return fmt.Errorf("global dimension '%s': %w", dimensionName, err)
		}
		cm.config.Render.Dimensions[dimensionName] = dimension
	}

	cm.logger.Info("Compiled global dimensions",
		zap.Int("count", len(cm.config.Render.Dimensions)),
	)

	return nil
}

// GetConfig returns current Edge Gateway configuration
func (cm *EGConfigManager) GetConfig() *EgConfig {
	return cm.config
}

// GetHosts returns current hosts configuration
func (cm *EGConfigManager) GetHosts() []types.Host {
	cache := cm.cache.Load()
	if cache == nil {
		return nil
	}
	return cache.hosts
}

// GetHostByDomain returns the host configuration for a domain.
// Domain matching is case-insensitive and checks all domains in host.Domains array.
// Returns nil if no matching host is found.
func (cm *EGConfigManager) GetHostByDomain(domain string) *types.Host {
	cache := cm.cache.Load()
	if cache == nil {
		return nil
	}
	return cache.byDomain[strings.ToLower(domain)]
}

// SetConfig sets the configuration (for testing)
func (cm *EGConfigManager) SetConfig(cfg *EgConfig) {
	cm.config = cfg
}

// SetHosts sets the hosts configuration (for testing)
func (cm *EGConfigManager) SetHosts(hosts *HostsConfig) {
	cm.hosts = hosts
	// Rebuild cache when hosts are updated, clear when nil
	if hosts != nil {
		cache := buildHostsCache(hosts.Hosts)
		cm.cache.Store(cache)
	} else {
		cm.cache.Store(nil)
	}
}

// applyDefaults applies default values to configuration
func (cm *EGConfigManager) applyDefaults() {
	// Apply log configuration defaults
	// If both outputs are disabled (zero values), enable console by default
	if !cm.config.Log.Console.Enabled && !cm.config.Log.File.Enabled {
		cm.config.Log.Console.Enabled = true
	}

	// Set format defaults if not specified
	if cm.config.Log.Console.Format == "" {
		cm.config.Log.Console.Format = configtypes.LogFormatConsole
	}

	if cm.config.Log.File.Format == "" {
		cm.config.Log.File.Format = configtypes.LogFormatText
	}

	// Apply default selection_strategy if not specified
	if cm.config.Registry.SelectionStrategy == "" {
		cm.config.Registry.SelectionStrategy = types.SelectionStrategyLeastLoaded
	}

	// Apply default unmatched_dimension at global level if not specified
	// This is the ONLY place where the default is applied - no fallbacks elsewhere
	if cm.config.Render.UnmatchedDimension == "" {
		cm.config.Render.UnmatchedDimension = types.UnmatchedDimensionBypass
	}

	// Apply default compression algorithm if not specified
	if cm.config.Storage.Compression == "" {
		cm.config.Storage.Compression = types.CompressionSnappy
	}
}

// emitConfigWarnings emits runtime warnings for configuration (non-validation concerns)
func (cm *EGConfigManager) emitConfigWarnings() {
	// Warn if bypass cache is enabled but TTL is 0 (no actual caching happens)
	if cm.config.Bypass.Cache.Enabled != nil && *cm.config.Bypass.Cache.Enabled {
		if cm.config.Bypass.Cache.TTL != nil && *cm.config.Bypass.Cache.TTL == 0 {
			cm.logger.Warn("bypass.cache.enabled=true but ttl=0 (no caching - all requests fetch from origin)")
		}
	}
}

// formatValidationErrors converts validation errors to a single runtime error
func (cm *EGConfigManager) formatValidationErrors(errors []validate.ValidationError) error {
	if len(errors) == 0 {
		return fmt.Errorf("configuration validation failed")
	}

	// Format first error as the main error message
	firstErr := errors[0]
	var msg string
	if firstErr.Line > 0 {
		msg = fmt.Sprintf("%s line %d: %s", firstErr.File, firstErr.Line, firstErr.Message)
	} else {
		msg = fmt.Sprintf("%s: %s", firstErr.File, firstErr.Message)
	}

	// If there are multiple errors, append count
	if len(errors) > 1 {
		msg = fmt.Sprintf("%s (and %d more errors)", msg, len(errors)-1)
	}

	return fmt.Errorf("%s", msg)
}

package validate

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/yamlutil"
	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// validResourceTypes contains all valid Chrome DevTools Protocol resource types
var validResourceTypes = map[string]bool{
	"Document":           true,
	"Stylesheet":         true,
	"Image":              true,
	"Media":              true,
	"Font":               true,
	"Script":             true,
	"TextTrack":          true,
	"XHR":                true,
	"Fetch":              true,
	"Prefetch":           true,
	"EventSource":        true,
	"WebSocket":          true,
	"Manifest":           true,
	"SignedExchange":     true,
	"Ping":               true,
	"CSPViolationReport": true,
	"Preflight":          true,
	"Other":              true,
}

// requestHeadersDenyList contains headers that must NEVER be forwarded to origin.
// These headers could cause security issues or break HTTP semantics if forwarded.
var requestHeadersDenyList = map[string]bool{
	"host":              true,
	"content-length":    true,
	"transfer-encoding": true,
	"connection":        true,
	"upgrade":           true,
	"keep-alive":        true,
	"te":                true,
	"trailer":           true,
}

// requestHeadersDenyListPrefixes contains header prefixes that are blocked.
var requestHeadersDenyListPrefixes = []string{
	"proxy-",
}

const (
	minStaleTTL                 = 1 * time.Hour
	maxStaleTTL                 = 7 * 24 * time.Hour // 7 days
	suspiciousDurationThreshold = 1 * time.Millisecond
)

// validateDurationUnit checks if a duration value is suspiciously small, indicating missing unit suffix
func validateDurationUnit(value time.Duration, fieldName string, filename string, collector *ErrorCollector) {
	if value > 0 && value < suspiciousDurationThreshold {
		collector.AddWarning(filename, 0,
			"%s value %s is suspiciously small. Did you forget the unit suffix (s, ms, m, h)?",
			fieldName, value)
	}
}

// isValidHTTPHeaderChar checks if a character is valid in an HTTP header name per RFC 7230
// Valid chars: A-Z a-z 0-9 ! # $ % & ' * + - . ^ _ ` | ~
func isValidHTTPHeaderChar(char rune) bool {
	return (char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9') ||
		char == '!' || char == '#' || char == '$' || char == '%' ||
		char == '&' || char == '\'' || char == '*' || char == '+' ||
		char == '-' || char == '.' || char == '^' || char == '_' ||
		char == '`' || char == '|' || char == '~'
}

// ValidateHTTPHeaderName validates a single HTTP header name per RFC 7230
func ValidateHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}

	for i, char := range name {
		if !isValidHTTPHeaderChar(char) {
			if char == ' ' {
				return fmt.Errorf("header name %q contains invalid space at position %d", name, i)
			} else if char == ':' {
				return fmt.Errorf("header name %q contains invalid colon at position %d", name, i)
			} else if char < 32 || char == 127 {
				return fmt.Errorf("header name %q contains invalid control character at position %d", name, i)
			} else {
				return fmt.Errorf("header name %q contains invalid character %q at position %d", name, char, i)
			}
		}
	}

	return nil
}

// validateHeadersConfig validates a HeadersConfig at any config level.
// It checks mutual exclusivity, deny-list compliance, and header name validity.
func validateHeadersConfig(headers *types.HeadersConfig, level string) error {
	if headers == nil {
		return nil
	}

	// Check mutual exclusivity for request headers
	if len(headers.SafeRequest) > 0 && len(headers.SafeRequestAdd) > 0 {
		return fmt.Errorf("%s: cannot use both safe_request and safe_request_add", level)
	}

	// Check mutual exclusivity for response headers
	if len(headers.SafeResponse) > 0 && len(headers.SafeResponseAdd) > 0 {
		return fmt.Errorf("%s: cannot use both safe_response and safe_response_add", level)
	}

	// Validate safe_request headers
	for i, header := range headers.SafeRequest {
		if err := validateRequestHeader(header); err != nil {
			return fmt.Errorf("%s safe_request[%d]: %w", level, i, err)
		}
	}

	// Validate safe_request_add headers
	for i, header := range headers.SafeRequestAdd {
		if err := validateRequestHeader(header); err != nil {
			return fmt.Errorf("%s safe_request_add[%d]: %w", level, i, err)
		}
	}

	// Validate safe_response headers (just name validity, no deny-list)
	for i, header := range headers.SafeResponse {
		if err := ValidateHTTPHeaderName(header); err != nil {
			return fmt.Errorf("%s safe_response[%d]: %w", level, i, err)
		}
	}

	// Validate safe_response_add headers
	for i, header := range headers.SafeResponseAdd {
		if err := ValidateHTTPHeaderName(header); err != nil {
			return fmt.Errorf("%s safe_response_add[%d]: %w", level, i, err)
		}
	}

	return nil
}

// validateRequestHeader checks if a header is valid for request forwarding.
func validateRequestHeader(header string) error {
	if err := ValidateHTTPHeaderName(header); err != nil {
		return err
	}
	return validateRequestHeaderNotDenied(header)
}

// validateRequestHeaderNotDenied checks if a header is in the deny-list.
func validateRequestHeaderNotDenied(header string) error {
	headerLower := strings.ToLower(header)

	// Check exact matches
	if requestHeadersDenyList[headerLower] {
		return fmt.Errorf("header %q is blocked for security reasons", header)
	}

	// Check prefixes
	for _, prefix := range requestHeadersDenyListPrefixes {
		if strings.HasPrefix(headerLower, prefix) {
			return fmt.Errorf("header %q is blocked (prefix %q not allowed)", header, prefix)
		}
	}

	return nil
}

// validateSafeHeadersInternal validates a list of HTTP header names
func validateSafeHeadersInternal(headers []string, level string) error {
	if len(headers) == 0 {
		return nil
	}

	for i, header := range headers {
		if err := ValidateHTTPHeaderName(header); err != nil {
			return fmt.Errorf("%s safe_headers[%d]: %w", level, i, err)
		}
	}

	return nil
}

// validateCacheExpiredConfig validates cache expiration configuration
func validateCacheExpiredConfig(expired *types.CacheExpiredConfig, context string, filename string, collector *ErrorCollector) {
	if expired == nil {
		return
	}

	// Validate strategy
	if expired.Strategy != "" {
		strategy := expired.Strategy
		if strategy != types.ExpirationStrategyServeStale && strategy != types.ExpirationStrategyDelete {
			collector.Add(filename, 0, "%s: invalid expired.strategy '%s', must be '%s' or '%s'",
				context, strategy, types.ExpirationStrategyServeStale, types.ExpirationStrategyDelete)
		}

		// Validate stale_ttl requirement
		if strategy == types.ExpirationStrategyServeStale {
			if expired.StaleTTL == nil {
				collector.Add(filename, 0, "%s: stale_ttl is required when strategy is 'serve_stale'", context)
			} else if *expired.StaleTTL <= 0 {
				collector.Add(filename, 0, "%s: stale_ttl must be positive when strategy is 'serve_stale'", context)
			}
		}
	}

	// Error for stale_ttl without strategy (orphaned config)
	if expired.StaleTTL != nil && expired.Strategy == "" {
		collector.Add(filename, 0, "%s: stale_ttl specified without strategy. Must set strategy: 'serve_stale' or 'delete'", context)
	}

	// Validate stale_ttl is not negative (when strategy is delete or unspecified)
	if expired.StaleTTL != nil && *expired.StaleTTL < 0 {
		collector.Add(filename, 0, "%s: stale_ttl cannot be negative", context)
	}

	// Warn about suspect stale_ttl ranges when strategy is serve_stale
	if expired.StaleTTL != nil && expired.Strategy == types.ExpirationStrategyServeStale {
		staleTTL := time.Duration(*expired.StaleTTL)
		validateDurationUnit(staleTTL, context+".stale_ttl", filename, collector)
		if staleTTL < minStaleTTL {
			collector.AddWarning(filename, 0, "%s: stale_ttl is very short (%s), recommended minimum is %s",
				context, staleTTL, minStaleTTL)
		} else if staleTTL > maxStaleTTL {
			collector.AddWarning(filename, 0, "%s: stale_ttl is very large (%s), recommended maximum is %s",
				context, staleTTL, maxStaleTTL)
		}
	}
}

// ValidationResult contains the result of configuration validation
type ValidationResult struct {
	Valid      bool
	Errors     []ValidationError
	Warnings   []ValidationError
	ConfigPath string
}

// ValidateConfiguration validates configuration files without external dependencies
// Returns validation result with any errors found
func ValidateConfiguration(configPath string) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:      true,
		ConfigPath: configPath,
	}

	collector := NewErrorCollector()

	// Load and validate main config
	egConfig, err := loadAndValidateMainConfig(configPath, collector)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if egConfig == nil {
		// YAML syntax errors were collected, skip further validation
		result.Valid = false
		result.Errors = collector.Errors()
		return result, nil
	}

	// Load hosts using include pattern from config
	hostsConfig, _, err := loadAndValidateHostsFromInclude(egConfig, configPath, collector)
	if err != nil {
		return nil, fmt.Errorf("failed to load hosts config: %w", err)
	}
	if hostsConfig == nil {
		// YAML syntax errors were collected, skip further validation
		result.Valid = false
		result.Errors = collector.Errors()
		return result, nil
	}

	// Validate cross-config dependencies (without external service checks)
	validateCrossConfig(egConfig, hostsConfig, collector)

	// Set result
	if collector.HasErrors() {
		result.Valid = false
		result.Errors = collector.Errors()
	}
	result.Warnings = collector.Warnings()

	return result, nil
}

// loadAndValidateMainConfig loads and validates main configuration
func loadAndValidateMainConfig(path string, collector *ErrorCollector) (*configtypes.EgConfig, error) {
	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg configtypes.EgConfig
	if err := yamlutil.UnmarshalStrict(data, &cfg); err != nil {
		collector.Add(filepath.Base(path), 0, "YAML syntax error: %v", err)
		return nil, nil
	}

	// Load line tracker
	lineTracker, err := NewLineTracker(path)
	if err != nil {
		// Line tracking failed, continue without line numbers
		lineTracker = nil
	}

	// Validate server configuration
	validateServerConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate Redis configuration
	validateRedisConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate metrics configuration
	validateMetricsConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate internal server configuration
	validateInternalConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate render configuration
	validateRenderConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate bypass configuration
	validateBypassConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate registry configuration
	validateRegistryConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate log configuration
	validateLogConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate timeout ranges
	validateTimeoutRanges(&cfg, filepath.Base(path), collector)

	// Validate tracking params
	validateTrackingParamsConfig(&cfg, filepath.Base(path), collector)

	// Validate cache sharding
	validateCacheShardingConfig(&cfg, filepath.Base(path), lineTracker, collector)

	// Validate bothit_recache
	validateBothitRecacheConfig(&cfg, filepath.Base(path), collector)

	// Validate safe_headers
	validateHeadersConfigGlobal(&cfg, filepath.Base(path), collector)

	// Validate client_ip
	validateClientIPConfigGlobal(&cfg, filepath.Base(path), collector)

	// Validate event logging
	validateEventLoggingConfig(&cfg, filepath.Base(path), collector)

	// Validate TLS configuration
	validateTLSConfig(&cfg, filepath.Dir(path), filepath.Base(path), collector)

	return &cfg, nil
}

// loadAndValidateHostsFromInclude loads hosts from include pattern and validates them
func loadAndValidateHostsFromInclude(egConfig *configtypes.EgConfig, configPath string, collector *ErrorCollector) (*configtypes.HostsConfig, []string, error) {
	if egConfig.Hosts.Include == "" {
		return nil, nil, fmt.Errorf("hosts.include is required in configuration")
	}

	// Resolve include path (relative to config directory)
	configDir := filepath.Dir(configPath)
	includePath := egConfig.Hosts.Include
	if !filepath.IsAbs(includePath) {
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
		return nil, nil, fmt.Errorf("invalid glob pattern '%s': %w", egConfig.Hosts.Include, err)
	}

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no host files found matching pattern '%s'", egConfig.Hosts.Include)
	}

	// Load and merge all host files
	var allHosts []types.Host
	seenIDs := make(map[int]string)        // Track host IDs to detect duplicates across files
	seenDomains := make(map[string]string) // Track domains to detect duplicates across files

	hasGlobalDimensions := len(egConfig.Render.Dimensions) > 0

	for _, file := range files {
		hosts, err := loadAndValidateHostsConfig(file, hasGlobalDimensions, collector)
		if err != nil {
			return nil, nil, err
		}
		if hosts == nil {
			// YAML syntax errors were collected, continue to collect all errors
			continue
		}

		// Check for duplicate host IDs and domains across files
		for _, host := range hosts.Hosts {
			if existingFile, exists := seenIDs[host.ID]; exists {
				collector.Add(filepath.Base(file), 0, "duplicate host ID %d found in '%s' (already defined in '%s')",
					host.ID, filepath.Base(file), filepath.Base(existingFile))
			}
			seenIDs[host.ID] = file

			// Check for duplicate domains across files
			for _, domain := range host.Domains {
				normalizedDomain := strings.ToLower(domain)
				if existingFile, exists := seenDomains[normalizedDomain]; exists {
					collector.Add(filepath.Base(file), 0, "duplicate domain %q found in '%s' (already defined in '%s')",
						domain, filepath.Base(file), filepath.Base(existingFile))
				}
				seenDomains[normalizedDomain] = file
			}
		}

		allHosts = append(allHosts, hosts.Hosts...)
	}

	if len(allHosts) == 0 {
		// If collector already has errors (e.g., YAML syntax errors), don't add a misleading generic error
		if collector.HasErrors() {
			return nil, files, nil
		}
		return nil, nil, fmt.Errorf("no hosts loaded from pattern '%s'", egConfig.Hosts.Include)
	}

	return &configtypes.HostsConfig{Hosts: allHosts}, files, nil
}

// loadAndValidateHostsConfig loads and validates hosts configuration from a single file
func loadAndValidateHostsConfig(path string, hasGlobalDimensions bool, collector *ErrorCollector) (*configtypes.HostsConfig, error) {
	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("hosts file not found: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read hosts file: %w", err)
	}

	var hosts configtypes.HostsConfig
	if err := yamlutil.UnmarshalStrict(data, &hosts); err != nil {
		errMsg := fmt.Sprintf("YAML syntax error: %v", err)

		// Provide helpful hints for common mistakes
		errStr := err.Error()
		if strings.Contains(errStr, "cannot unmarshal") && strings.Contains(errStr, "into types.Dimension") {
			if strings.Contains(errStr, "unmatched_dimension") || strings.Contains(string(data), "unmatched_dimension:") {
				errMsg += "\n  → Hint: 'unmatched_dimension' should be at host render level, not inside 'dimensions' map"
				errMsg += "\n  → Correct placement: hosts[].render.unmatched_dimension"
				errMsg += "\n  → Incorrect placement: hosts[].render.dimensions.unmatched_dimension"
			}
		}

		collector.Add(filepath.Base(path), 0, "%s", errMsg)
		return nil, nil
	}

	// Load line tracker for hosts file
	hostsTracker, err := NewHostsLineTracker(path)
	if err != nil {
		// Line tracking failed, continue without line numbers
		hostsTracker = nil
	}

	// Validate hosts
	validateHosts(&hosts, filepath.Base(path), hasGlobalDimensions, hostsTracker, collector)

	return &hosts, nil
}

// validateServerConfig validates server configuration
func validateServerConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	lineNum := 0
	if lt != nil {
		lineNum = lt.GetServerLine("listen")
	}
	if cfg.Server.Listen == "" {
		collector.Add(filename, lineNum, "server.listen is required")
	} else if err := configtypes.ValidateListenAddress(cfg.Server.Listen); err != nil {
		collector.Add(filename, lineNum, "invalid server.listen: %v", err)
	}

	if lt != nil {
		lineNum = lt.GetServerLine("timeout")
	}
	if cfg.Server.Timeout <= 0 {
		collector.Add(filename, lineNum, "server.timeout must be positive, got %s", cfg.Server.Timeout)
	}
}

// extractPort parses the port from a listen address (e.g., ":10070" -> 10070, "192.168.1.1:10443" -> 10443).
func extractPort(listen string) (int, error) {
	if listen == "" {
		return 0, nil
	}
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

// resolvePath resolves a file path relative to configDir.
// If path is absolute, it is used as-is. Otherwise, it is joined with configDir.
// Symlinks are resolved using filepath.EvalSymlinks.
func resolvePath(path, configDir string) (string, error) {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = path
	} else {
		resolved = filepath.Join(configDir, path)
	}

	// Resolve symlinks
	resolved, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// validateTLSConfig validates TLS configuration for HTTPS support.
// configDir is the directory containing the config file, used for resolving relative paths.
func validateTLSConfig(cfg *configtypes.EgConfig, configDir string, filename string, collector *ErrorCollector) {
	tls := cfg.Server.TLS
	if !tls.Enabled {
		return
	}

	// Validate required fields
	if tls.Listen == "" {
		collector.Add(filename, 0, "TLS enabled but tls.listen not specified")
	}
	if tls.CertFile == "" {
		collector.Add(filename, 0, "TLS enabled but tls.cert_file not specified")
	}
	if tls.KeyFile == "" {
		collector.Add(filename, 0, "TLS enabled but tls.key_file not specified")
	}

	// Validate listen address format (only if listen is provided)
	var tlsPort int
	if tls.Listen != "" {
		var err error
		tlsPort, err = extractPort(tls.Listen)
		if err != nil {
			collector.Add(filename, 0, "TLS listen address invalid: %s", tls.Listen)
		} else if tlsPort < 1 || tlsPort > 65535 {
			collector.Add(filename, 0, "TLS listen address invalid: %s", tls.Listen)
		}
	}

	// Validate cert_file (only if path is provided)
	if tls.CertFile != "" {
		certPath, err := resolvePath(tls.CertFile, configDir)
		if err != nil {
			collector.Add(filename, 0, "TLS cert_file not found: %s", tls.CertFile)
		} else if certFile, err := os.Open(certPath); err != nil {
			if os.IsNotExist(err) {
				collector.Add(filename, 0, "TLS cert_file not found: %s", certPath)
			} else {
				collector.Add(filename, 0, "TLS cert_file not readable: %s: %v", certPath, err)
			}
		} else {
			certFile.Close()
		}
	}

	// Validate key_file (only if path is provided)
	if tls.KeyFile != "" {
		keyPath, err := resolvePath(tls.KeyFile, configDir)
		if err != nil {
			collector.Add(filename, 0, "TLS key_file not found: %s", tls.KeyFile)
		} else if keyFile, err := os.Open(keyPath); err != nil {
			if os.IsNotExist(err) {
				collector.Add(filename, 0, "TLS key_file not found: %s", keyPath)
			} else {
				collector.Add(filename, 0, "TLS key_file not readable: %s: %v", keyPath, err)
			}
		} else {
			keyFile.Close()
		}
	}

	// Check for port conflicts (only if we have a valid TLS port)
	if tlsPort > 0 {
		httpPort, err := extractPort(cfg.Server.Listen)
		if err == nil && httpPort > 0 && httpPort == tlsPort {
			collector.Add(filename, 0, "TLS listen port conflicts with server.listen: both use port %d", tlsPort)
		}

		metricsPort := 0
		if cfg.Metrics.Enabled {
			metricsPort, _ = configtypes.GetPortFromListen(cfg.Metrics.Listen)
		}
		if metricsPort > 0 && metricsPort == tlsPort {
			collector.Add(filename, 0, "TLS listen port %d conflicts with metrics.port", tlsPort)
		}

		internalPort, err := extractPort(cfg.Internal.Listen)
		if err == nil && internalPort > 0 && internalPort == tlsPort {
			collector.Add(filename, 0, "TLS listen port %d conflicts with internal_server.listen", tlsPort)
		}
	}
}

// validateRedisConfig validates Redis configuration
func validateRedisConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	lineNum := 0
	if lt != nil {
		lineNum = lt.GetRedisLine("addr")
	}
	if cfg.Redis.Addr == "" {
		collector.Add(filename, lineNum, "redis.addr is required")
	}
}

// validateRenderConfig validates render configuration
func validateRenderConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	cache := &cfg.Render.Cache

	// Validate TTL
	if cache.TTL != nil {
		ttl := time.Duration(*cache.TTL)
		validateDurationUnit(ttl, "render.cache.ttl", filename, collector)
		if *cache.TTL < 0 {
			collector.Add(filename, 0, "render.cache.ttl cannot be negative")
		}
	}

	// Validate expiration configuration
	validateCacheExpiredConfig(cache.Expired, "render.cache", filename, collector)

	// Validate blocked resource types
	for _, rt := range cfg.Render.BlockedResourceTypes {
		if !validResourceTypes[rt] {
			collector.Add(filename, 0, "invalid blocked_resource_type '%s', must be a valid Chrome DevTools Protocol resource type", rt)
		}
	}

	// Validate global dimensions
	validateGlobalDimensions(cfg, filename, collector)

	// Validate global events
	validateGlobalEvents(cfg, filename, collector)

	// Validate global unmatched_dimension
	validateGlobalUnmatchedDimension(cfg, filename, collector)
}

// validateGlobalEvents validates global events configuration
func validateGlobalEvents(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.Render.Events.WaitFor == "" && cfg.Render.Events.AdditionalWait == nil {
		return
	}

	validateRenderEvents(&cfg.Render.Events, "render.events", filename, collector)
}

// validateGlobalDimensions validates global dimension configuration
func validateGlobalDimensions(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if len(cfg.Render.Dimensions) == 0 {
		return
	}

	dimensionIDs := make(map[int]string)

	for dimensionName, dimension := range cfg.Render.Dimensions {
		// Validate dimension ID
		if dimension.ID <= 0 {
			collector.Add(filename, 0, "render.dimensions: dimension '%s' has invalid ID %d (must be positive)",
				dimensionName, dimension.ID)
		}

		// Check for duplicate IDs
		if existingDimension, exists := dimensionIDs[dimension.ID]; exists {
			collector.Add(filename, 0, "render.dimensions: dimension ID %d is used by both '%s' and '%s' (must be unique)",
				dimension.ID, existingDimension, dimensionName)
		}
		dimensionIDs[dimension.ID] = dimensionName

		// Validate dimension fields
		if dimension.Width <= 0 {
			collector.Add(filename, 0, "render.dimensions: dimension '%s' has invalid width %d (must be positive)",
				dimensionName, dimension.Width)
		}
		if dimension.Height <= 0 {
			collector.Add(filename, 0, "render.dimensions: dimension '%s' has invalid height %d (must be positive)",
				dimensionName, dimension.Height)
		}

		// Validate match_ua patterns
		for _, pattern := range dimension.MatchUA {
			if pattern == "" {
				collector.Add(filename, 0, "render.dimensions: dimension '%s': match_ua pattern contains empty string",
					dimensionName)
				continue
			}
			if err := validatePatternSyntax(pattern, "user-agent"); err != nil {
				collector.Add(filename, 0, "render.dimensions: dimension '%s': invalid match_ua pattern '%s': %v",
					dimensionName, pattern, err)
			}
		}
	}
}

// validateBypassConfig validates bypass configuration
func validateBypassConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	// Validate timeout
	if cfg.Bypass.Timeout != nil && *cfg.Bypass.Timeout <= 0 {
		collector.Add(filename, 0, "bypass.timeout must be positive")
	}

	// Validate bypass cache
	if cfg.Bypass.Cache.TTL != nil {
		ttl := time.Duration(*cfg.Bypass.Cache.TTL)
		validateDurationUnit(ttl, "bypass.cache.ttl", filename, collector)
		if *cfg.Bypass.Cache.TTL < 0 {
			collector.Add(filename, 0, "bypass.cache.ttl cannot be negative")
		}
	}

	if len(cfg.Bypass.Cache.StatusCodes) > 0 {
		for _, code := range cfg.Bypass.Cache.StatusCodes {
			if code < 100 || code >= 600 {
				collector.Add(filename, 0, "invalid HTTP status code: %d (must be 100-599)", code)
			}
		}
	}

	// Check for enabled cache without status codes
	if cfg.Bypass.Cache.Enabled != nil && *cfg.Bypass.Cache.Enabled {
		if len(cfg.Bypass.Cache.StatusCodes) == 0 {
			collector.Add(filename, 0, "bypass.cache.status_codes must not be empty when caching enabled")
		}
	}
}

// validateRegistryConfig validates registry configuration
func validateRegistryConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	// Validate selection_strategy
	if cfg.Registry.SelectionStrategy != "" {
		strategy := cfg.Registry.SelectionStrategy
		if strategy != types.SelectionStrategyLeastLoaded && strategy != types.SelectionStrategyMostAvailable {
			collector.Add(filename, 0, "invalid registry.selection_strategy '%s', must be '%s' or '%s'",
				strategy, types.SelectionStrategyLeastLoaded, types.SelectionStrategyMostAvailable)
		}
	}
}

// validateLogConfig validates log configuration
func validateLogConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	// Validate log level
	validLogLevels := map[string]bool{
		configtypes.LogLevelDebug:  true,
		configtypes.LogLevelInfo:   true,
		configtypes.LogLevelWarn:   true,
		configtypes.LogLevelError:  true,
		configtypes.LogLevelDPanic: true,
		configtypes.LogLevelPanic:  true,
		configtypes.LogLevelFatal:  true,
	}
	if cfg.Log.Level != "" && !validLogLevels[cfg.Log.Level] {
		collector.Add(filename, 0, "invalid log.level '%s' (must be debug, info, warn, error, dpanic, panic, or fatal)", cfg.Log.Level)
	}

	// Validate console level if specified
	if cfg.Log.Console.Level != "" && !validLogLevels[cfg.Log.Console.Level] {
		collector.Add(filename, 0, "invalid log.console.level '%s' (must be debug, info, warn, error, dpanic, panic, or fatal)", cfg.Log.Console.Level)
	}

	// Validate file level if specified
	if cfg.Log.File.Level != "" && !validLogLevels[cfg.Log.File.Level] {
		collector.Add(filename, 0, "invalid log.file.level '%s' (must be debug, info, warn, error, dpanic, panic, or fatal)", cfg.Log.File.Level)
	}

	// Validate console format
	validConsoleFormats := map[string]bool{
		configtypes.LogFormatJSON:    true,
		configtypes.LogFormatConsole: true,
	}
	if cfg.Log.Console.Enabled && cfg.Log.Console.Format != "" && !validConsoleFormats[cfg.Log.Console.Format] {
		collector.Add(filename, 0, "invalid log.console.format '%s' (must be json or console)", cfg.Log.Console.Format)
	}

	// Validate file configuration
	if cfg.Log.File.Enabled {
		// File path is required when file logging is enabled
		if cfg.Log.File.Path == "" {
			collector.Add(filename, 0, "log.file.path must be specified when file logging is enabled")
		}

		// Validate file format
		validFileFormats := map[string]bool{
			configtypes.LogFormatJSON: true,
			configtypes.LogFormatText: true,
		}
		if cfg.Log.File.Format != "" && !validFileFormats[cfg.Log.File.Format] {
			collector.Add(filename, 0, "invalid log.file.format '%s' (must be json or text)", cfg.Log.File.Format)
		}

		// Validate rotation parameters
		if cfg.Log.File.Rotation.MaxSize < 0 {
			collector.Add(filename, 0, "log.file.rotation.max_size must be >= 0, got %d", cfg.Log.File.Rotation.MaxSize)
		}
		if cfg.Log.File.Rotation.MaxAge < 0 {
			collector.Add(filename, 0, "log.file.rotation.max_age must be >= 0, got %d", cfg.Log.File.Rotation.MaxAge)
		}
		if cfg.Log.File.Rotation.MaxBackups < 0 {
			collector.Add(filename, 0, "log.file.rotation.max_backups must be >= 0, got %d", cfg.Log.File.Rotation.MaxBackups)
		}
	}
}

// validateMetricsConfig validates metrics configuration
func validateMetricsConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	// Validate metrics listen address
	if cfg.Metrics.Enabled {
		if cfg.Metrics.Listen == "" {
			collector.Add(filename, 0, "metrics.listen is required when metrics enabled")
		} else if err := configtypes.ValidateListenAddress(cfg.Metrics.Listen); err != nil {
			collector.Add(filename, 0, "invalid metrics.listen: %v", err)
		}
	}

	// Validate metrics.listen port differs from server.listen port when metrics enabled
	if cfg.Metrics.Enabled && cfg.Metrics.Listen != "" && cfg.Server.Listen != "" {
		metricsPort, err1 := configtypes.GetPortFromListen(cfg.Metrics.Listen)
		serverPort, err2 := configtypes.GetPortFromListen(cfg.Server.Listen)
		if err1 == nil && err2 == nil && metricsPort == serverPort {
			collector.Add(filename, 0, "metrics.listen port (%d) must differ from server.listen port (%d) when metrics enabled - metrics always run on separate port", metricsPort, serverPort)
		}
	}

	// Validate metrics path
	if cfg.Metrics.Path != "" && !strings.HasPrefix(cfg.Metrics.Path, "/") {
		collector.Add(filename, 0, "invalid metrics.path '%s' (must start with /)", cfg.Metrics.Path)
	}

	// Validate metrics namespace (must follow Prometheus naming rules)
	if cfg.Metrics.Namespace != "" {
		// Prometheus namespace must match: [a-zA-Z_][a-zA-Z0-9_]*
		namespacePattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
		if !namespacePattern.MatchString(cfg.Metrics.Namespace) {
			collector.Add(filename, 0, "invalid metrics.namespace '%s' (must match [a-zA-Z_][a-zA-Z0-9_]*)", cfg.Metrics.Namespace)
		}
	}
}

func validateInternalConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	lineNum := 0

	// internal.listen is required
	if cfg.Internal.Listen == "" {
		collector.Add(filename, lineNum, "internal.listen is required")
	} else if err := configtypes.ValidateListenAddress(cfg.Internal.Listen); err != nil {
		collector.Add(filename, lineNum, "invalid internal.listen: %v", err)
	} else {
		// Validate internal port differs from server port
		internalPort, err1 := configtypes.GetPortFromListen(cfg.Internal.Listen)
		serverPort, err2 := configtypes.GetPortFromListen(cfg.Server.Listen)
		if err1 == nil && err2 == nil && internalPort == serverPort {
			collector.Add(filename, lineNum, "internal.listen port (%d) must differ from server.listen port (%d)", internalPort, serverPort)
		}
	}

	// internal.auth_key is required
	if cfg.Internal.AuthKey == "" {
		collector.Add(filename, lineNum, "internal.auth_key is required")
	} else if len(cfg.Internal.AuthKey) < 16 {
		collector.AddWarning(filename, lineNum, "internal.auth_key is short (%d chars), recommend 32+ characters for security", len(cfg.Internal.AuthKey))
	}
}

// validateTimeoutRanges validates timeout configuration and warns about dangerously low or high values
func validateTimeoutRanges(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	// server.timeout validation
	serverTimeout := time.Duration(cfg.Server.Timeout)
	validateDurationUnit(serverTimeout, "server.timeout", filename, collector)
	if serverTimeout < 60*time.Second {
		collector.AddWarning(filename, 0, "server.timeout (%s) is low. Recommended minimum is 60s to accommodate render timeouts and coordination overhead", cfg.Server.Timeout)
	}
	if serverTimeout > 300*time.Second {
		collector.AddWarning(filename, 0, "server.timeout (%s) is very high. Values over 300s (5 minutes) may indicate architectural issues", cfg.Server.Timeout)
	}

	// bypass.timeout validation
	if cfg.Bypass.Timeout != nil {
		bypassTimeout := time.Duration(*cfg.Bypass.Timeout)
		validateDurationUnit(bypassTimeout, "bypass.timeout", filename, collector)
		if bypassTimeout < 5*time.Second {
			collector.AddWarning(filename, 0, "bypass.timeout (%s) is low. Slow origin servers may timeout. Recommended minimum: 5s", *cfg.Bypass.Timeout)
		}
		if bypassTimeout > 60*time.Second {
			collector.AddWarning(filename, 0, "bypass.timeout (%s) is high. Values over 60s may indicate origin server performance issues", *cfg.Bypass.Timeout)
		}
	}
}

// validateTrackingParamsConfig validates global tracking params configuration
func validateTrackingParamsConfig(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.TrackingParams == nil {
		return
	}

	if err := validateTrackingParamsInternal(cfg.TrackingParams, "global"); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateCacheShardingConfig validates cache sharding configuration
func validateCacheShardingConfig(cfg *configtypes.EgConfig, filename string, lt *LineTracker, collector *ErrorCollector) {
	if cfg.CacheSharding == nil {
		return
	}

	cs := cfg.CacheSharding

	// If sharding is not enabled, no further validation needed
	if cs.Enabled == nil || !*cs.Enabled {
		return
	}

	// EG ID is required when sharding is enabled
	lineNum := 0
	if cfg.EgID == "" {
		collector.Add(filename, lineNum, "eg_id is required when cache_sharding.enabled=true")
	}

	// When sharding is enabled, internal.listen should be an IP address (not localhost)
	// because it will be used for communication between EG instances
	if cfg.Internal.Listen != "" {
		host, _, err := configtypes.ParseListenAddress(cfg.Internal.Listen)
		if err == nil && (host == "localhost" || host == "127.0.0.1") {
			collector.AddWarning(filename, lineNum, "internal.listen is localhost (%q) with cache_sharding enabled; use an IP address for inter-EG communication in production", cfg.Internal.Listen)
		}
	}

	// Replication factor must be non-negative
	if cs.ReplicationFactor != nil && *cs.ReplicationFactor < 0 {
		collector.Add(filename, lineNum, "cache_sharding.replication_factor must be >= 0, got %d", *cs.ReplicationFactor)
	}

	// Distribution strategy validation
	if cs.DistributionStrategy != "" {
		validStrategies := map[string]bool{
			"hash_modulo":  true,
			"random":       true,
			"primary_only": true,
		}
		if !validStrategies[cs.DistributionStrategy] {
			collector.Add(filename, lineNum, "invalid cache_sharding.distribution_strategy: %q (must be: hash_modulo, random, primary_only)", cs.DistributionStrategy)
		}
	}
}

// validateBothitRecacheConfig validates global bothit_recache configuration
func validateBothitRecacheConfig(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.BothitRecache == nil {
		return
	}

	if err := validateBothitRecacheInternal(cfg.BothitRecache, "global"); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateHeadersConfigGlobal validates global headers configuration
func validateHeadersConfigGlobal(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.Headers == nil {
		return
	}

	if err := validateHeadersConfig(cfg.Headers, "global"); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

func validateClientIPConfig(clientIP *types.ClientIPConfig, level string) error {
	if clientIP == nil {
		return nil
	}
	if len(clientIP.Headers) == 0 {
		return fmt.Errorf("%s: client_ip.headers must not be empty when client_ip is configured", level)
	}
	for i, header := range clientIP.Headers {
		if err := ValidateHTTPHeaderName(header); err != nil {
			return fmt.Errorf("%s client_ip.headers[%d]: %w", level, i, err)
		}
	}
	return nil
}

func validateClientIPConfigGlobal(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.ClientIP == nil {
		return
	}
	if err := validateClientIPConfig(cfg.ClientIP, "global"); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

func validateHostClientIP(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	if host.ClientIP == nil {
		return
	}
	if err := validateClientIPConfig(host.ClientIP, fmt.Sprintf("host[%d] (%s)", hostIndex, host.Domain)); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateEventLoggingConfig validates event logging configuration
func validateEventLoggingConfig(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	if cfg.EventLogging == nil || !cfg.EventLogging.File.Enabled {
		return
	}

	file := cfg.EventLogging.File

	if file.Path == "" {
		collector.Add(filename, 0, "event_logging.file.path is required when event logging is enabled")
	}

	// Template is optional - default will be applied by FileEmitter

	if file.Rotation.MaxSize < 0 {
		collector.Add(filename, 0, "event_logging.file.rotation.max_size must be >= 0, got %d", file.Rotation.MaxSize)
	}

	if file.Rotation.MaxAge < 0 {
		collector.Add(filename, 0, "event_logging.file.rotation.max_age must be >= 0, got %d", file.Rotation.MaxAge)
	}

	if file.Rotation.MaxBackups < 0 {
		collector.Add(filename, 0, "event_logging.file.rotation.max_backups must be >= 0, got %d", file.Rotation.MaxBackups)
	}
}

// validateStorageConfig validates storage configuration
func validateStorageConfig(cfg *configtypes.EgConfig, hostsConfig *configtypes.HostsConfig, filename string, collector *ErrorCollector) {
	basePath := strings.TrimSpace(cfg.Storage.BasePath)
	if basePath == "" {
		collector.Add(filename, 0, "storage.base_path is required")
		return
	}

	absPath, err := filepath.Abs(basePath)
	if err != nil {
		collector.Add(filename, 0, "storage.base_path: invalid path '%s': %v", basePath, err)
		return
	}
	cfg.Storage.BasePath = absPath

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(absPath, 0o755); err != nil {
				collector.Add(filename, 0, "storage.base_path: failed to create directory '%s': %v", absPath, err)
				return
			}
		} else {
			collector.Add(filename, 0, "storage.base_path: cannot access directory '%s': %v", absPath, err)
			return
		}
	} else {
		if !fileInfo.IsDir() {
			collector.Add(filename, 0, "storage.base_path: path '%s' exists but is not a directory", absPath)
			return
		}
	}

	testFileName := fmt.Sprintf(".edgecomet-write-test-%d", time.Now().UnixNano())
	testFilePath := filepath.Join(absPath, testFileName)
	testData := []byte("edgecomet write test")

	if err := os.WriteFile(testFilePath, testData, 0o644); err != nil {
		collector.Add(filename, 0, "storage.base_path: directory '%s' is not writable: %v", absPath, err)
		return
	}

	defer func() {
		_ = os.Remove(testFilePath)
	}()

	readData, err := os.ReadFile(testFilePath)
	if err != nil {
		collector.Add(filename, 0, "storage.base_path: directory '%s' is not readable: %v", absPath, err)
		return
	}

	if !bytes.Equal(testData, readData) {
		collector.Add(filename, 0, "storage.base_path: filesystem verification failed for '%s': data mismatch", absPath)
		return
	}

	// Validate compression algorithm
	if cfg.Storage.Compression != "" {
		validCompressions := map[string]bool{
			types.CompressionNone:   true,
			types.CompressionSnappy: true,
			types.CompressionLZ4:    true,
		}
		if !validCompressions[cfg.Storage.Compression] {
			collector.Add(filename, 0,
				"storage.compression must be 'none', 'snappy', 'lz4', or empty, got '%s'",
				cfg.Storage.Compression)
		}
	}

	if cfg.Storage.Cleanup == nil {
		return
	}

	cleanup := cfg.Storage.Cleanup

	if cleanup.Interval <= 0 {
		collector.Add(filename, 0, "storage.cleanup.interval must be positive")
		return
	}

	interval := time.Duration(cleanup.Interval)
	validateDurationUnit(interval, "storage.cleanup.interval", filename, collector)
	minInterval := 10 * time.Minute
	minSafetyMargin := 1 * time.Hour

	// Allow shorter intervals in test mode for fast acceptance testing
	if os.Getenv("TEST_MODE") == "local" {
		minInterval = 1 * time.Second
		minSafetyMargin = 1 * time.Second
	}

	if interval < minInterval {
		collector.Add(filename, 0, "storage.cleanup.interval must be >= %s, got %s", minInterval, cleanup.Interval)
	}
	if interval > 24*time.Hour {
		collector.Add(filename, 0, "storage.cleanup.interval must be <= 24h, got %s", cleanup.Interval)
	}

	if cleanup.SafetyMargin <= 0 {
		collector.Add(filename, 0, "storage.cleanup.safety_margin must be positive")
		return
	}

	safetyMargin := time.Duration(cleanup.SafetyMargin)
	validateDurationUnit(safetyMargin, "storage.cleanup.safety_margin", filename, collector)
	if safetyMargin < minSafetyMargin {
		collector.Add(filename, 0, "storage.cleanup.safety_margin must be >= %s, got %s", minSafetyMargin, cleanup.SafetyMargin)
	}

	// Warning if interval exceeds minimum stale_ttl across hosts
	if hostsConfig != nil {
		minStaleTTL := getMinStaleTTL(hostsConfig)
		if minStaleTTL > 0 && interval > minStaleTTL {
			collector.AddWarning(filename, 0,
				"storage.cleanup.interval (%s) exceeds minimum stale_ttl (%s) across hosts - files may accumulate between cleanup runs",
				cleanup.Interval, minStaleTTL)
		}
	}
}

// validateBothitRecacheInternal validates bothit_recache configuration at any level
func validateBothitRecacheInternal(config *types.BothitRecacheConfig, level string) error {
	if config == nil {
		return nil
	}

	// If enabled, validate required fields
	if config.Enabled != nil && *config.Enabled {
		// Validate interval is within allowed range
		if config.Interval != nil {
			interval := time.Duration(*config.Interval)
			if interval < 30*time.Minute {
				return fmt.Errorf("%s bothit_recache: interval must be >= 30m, got %v", level, interval)
			}
			if interval > 24*time.Hour {
				return fmt.Errorf("%s bothit_recache: interval must be <= 24h, got %v", level, interval)
			}
		}

		// Validate match_ua is non-empty
		if len(config.MatchUA) == 0 {
			return fmt.Errorf("%s bothit_recache: match_ua must be non-empty when enabled=true", level)
		}

		// Validate match_ua patterns
		for _, pattern := range config.MatchUA {
			if pattern == "" {
				return fmt.Errorf("%s bothit_recache: match_ua pattern contains empty string", level)
			}
			if err := validatePatternSyntax(pattern, "user-agent"); err != nil {
				return fmt.Errorf("%s bothit_recache: invalid match_ua pattern '%s': %w", level, pattern, err)
			}
		}
	}

	return nil
}

// validateHostTrackingParams validates host-level tracking params
func validateHostTrackingParams(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	if host.TrackingParams == nil {
		return
	}

	if err := validateTrackingParamsInternal(host.TrackingParams, fmt.Sprintf("host[%d] (%s)", hostIndex, host.Domain)); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateHostBothitRecache validates host-level bothit_recache configuration
func validateHostBothitRecache(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	if host.BothitRecache == nil {
		return
	}

	if err := validateBothitRecacheInternal(host.BothitRecache, fmt.Sprintf("host[%d] (%s)", hostIndex, host.Domain)); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateHostHeaders validates headers at host level
func validateHostHeaders(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	if host.Headers == nil {
		return
	}

	if err := validateHeadersConfig(host.Headers, fmt.Sprintf("host[%d] (%s)", hostIndex, host.Domain)); err != nil {
		collector.Add(filename, 0, "%v", err)
	}
}

// validateHostBlockedResourceTypes validates blocked resource types at host level
func validateHostBlockedResourceTypes(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	for _, rt := range host.Render.BlockedResourceTypes {
		if !validResourceTypes[rt] {
			collector.Add(filename, 0, "host[%d] (%s): invalid blocked_resource_type '%s', must be a valid Chrome DevTools Protocol resource type",
				hostIndex, host.Domain, rt)
		}
	}
}

// CompiledStripPattern represents a compiled pattern for parameter stripping
type CompiledStripPattern struct {
	Original    string
	PatternType pattern.PatternType
	Regex       *regexp.Regexp
}

// validateTrackingParamsInternal validates tracking parameter configuration
// Returns an error if any patterns are invalid (e.g., invalid regex)
func validateTrackingParamsInternal(tp *types.TrackingParamsConfig, level string) error {
	if tp == nil {
		return nil
	}

	// Check mutual exclusivity of params and params_add
	if len(tp.Params) > 0 && len(tp.ParamsAdd) > 0 {
		return fmt.Errorf("tracking_params at %s level: cannot have both 'params' and 'params_add' - use 'params' to replace or 'params_add' to extend", level)
	}

	// Validate and pre-compile params patterns
	if len(tp.Params) > 0 {
		compiled, err := compileStripPatternsForValidation(tp.Params)
		if err != nil {
			return fmt.Errorf("invalid tracking_params.params at %s level: %w", level, err)
		}
		redundant := findRedundantPatterns(compiled)
		for _, pattern := range redundant {
			_ = pattern
		}
	}

	// Validate and pre-compile params_add patterns
	if len(tp.ParamsAdd) > 0 {
		compiled, err := compileStripPatternsForValidation(tp.ParamsAdd)
		if err != nil {
			return fmt.Errorf("invalid tracking_params.params_add at %s level: %w", level, err)
		}
		redundant := findRedundantPatterns(compiled)
		for _, pattern := range redundant {
			_ = pattern
		}
	}

	return nil
}

// compileStripPatternsForValidation compiles patterns for validation purposes
func compileStripPatternsForValidation(patterns []string) ([]CompiledStripPattern, error) {
	compiled := make([]CompiledStripPattern, 0, len(patterns))

	for _, pat := range patterns {
		if pat == "" {
			continue // Skip empty patterns
		}

		cp := CompiledStripPattern{
			Original: pat,
		}

		// Detect pattern type and compile if needed
		pType, cleanPattern, caseInsensitive := types.DetectPatternType(pat)
		cp.PatternType = pType

		// Compile regexp if needed
		if pType == pattern.PatternTypeRegexp {
			var re *regexp.Regexp
			var err error

			if caseInsensitive {
				// Prepend case-insensitive flag to pattern
				re, err = regexp.Compile("(?i)" + cleanPattern)
			} else {
				re, err = regexp.Compile(cleanPattern)
			}

			if err != nil {
				return nil, fmt.Errorf("invalid regexp pattern '%s': %w", pat, err)
			}

			cp.Regex = re
		}

		compiled = append(compiled, cp)
	}

	return compiled, nil
}

// findRedundantPatterns detects patterns that are covered by other patterns
func findRedundantPatterns(patterns []CompiledStripPattern) []string {
	redundant := []string{}

	// Check each pattern against all others
	for i, pattern := range patterns {
		for j, other := range patterns {
			if i == j {
				continue
			}

			// Check if pattern is covered by other
			if isPatternRedundant(pattern, other) {
				redundant = append(redundant, pattern.Original)
				break
			}
		}
	}

	return redundant
}

// isPatternRedundant checks if a pattern is fully covered by another pattern
func isPatternRedundant(pat, other CompiledStripPattern) bool {
	// Exact patterns covered by wildcard
	if pat.PatternType == pattern.PatternTypeExact && other.PatternType == pattern.PatternTypeWildcard {
		if matchWildcardForValidation(strings.ToLower(pat.Original), strings.ToLower(other.Original)) {
			return true
		}
	}

	// Wildcard covered by broader wildcard
	if pat.PatternType == pattern.PatternTypeWildcard && other.PatternType == pattern.PatternTypeWildcard {
		if matchWildcardForValidation(strings.ToLower(pat.Original), strings.ToLower(other.Original)) && pat.Original != other.Original {
			return true
		}
	}

	// Exact/Wildcard covered by regex
	if other.PatternType == pattern.PatternTypeRegexp && other.Regex != nil {
		if pat.PatternType == pattern.PatternTypeExact {
			if other.Regex.MatchString(pat.Original) {
				return true
			}
		}
	}

	return false
}

// matchWildcardForValidation performs wildcard pattern matching for validation
func matchWildcardForValidation(text, pattern string) bool {
	// If no wildcard, do exact match
	if !strings.Contains(pattern, "*") {
		return text == pattern
	}

	// Split pattern by wildcards
	parts := strings.Split(pattern, "*")

	// Text must start with first part
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]

	// Text must end with last part
	if !strings.HasSuffix(text, parts[len(parts)-1]) {
		return false
	}
	text = text[:len(text)-len(parts[len(parts)-1])]

	// Check middle parts exist in order
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		idx := strings.Index(text, parts[i])
		if idx == -1 {
			return false
		}
		text = text[idx+len(parts[i]):]
	}

	return true
}

// validateHosts validates hosts configuration
func validateHosts(hosts *configtypes.HostsConfig, filename string, hasGlobalDimensions bool, ht *HostsLineTracker, collector *ErrorCollector) {
	if len(hosts.Hosts) == 0 {
		collector.Add(filename, 0, "no hosts configured")
		return
	}

	domains := make(map[string]bool)
	renderKeys := make(map[string]bool)
	hostIDs := make(map[int]string) // map of ID to domain for duplicate detection

	for i := range hosts.Hosts {
		host := &hosts.Hosts[i]

		// Validate Domains array
		if len(host.Domains) == 0 {
			collector.Add(filename, 0, "host[%d]: domain is required (at least one domain must be specified)", i)
		} else {
			seenInHost := make(map[string]bool) // Track duplicates within this host
			for j, domain := range host.Domains {
				// Whitespace-only check
				if strings.TrimSpace(domain) == "" {
					collector.Add(filename, 0, "host[%d]: domain[%d]: cannot be empty or whitespace-only", i, j)
					continue
				}
				// Contains protocol check
				if strings.Contains(domain, "://") {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: must not contain protocol (remove http:// or https://)", i, j, domain)
				}
				// Contains path check
				if strings.Contains(domain, "/") {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: must not contain path (domain only, no slashes)", i, j, domain)
				}
				// Contains port check
				if strings.Contains(domain, ":") {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: must not contain port (domain only)", i, j, domain)
				}
				// Contains wildcard check
				if strings.Contains(domain, "*") {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: wildcards not allowed in domain names", i, j, domain)
				}
				// Lowercase check (RFC 1123)
				if domain != strings.ToLower(domain) {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: must be lowercase (RFC 1123)", i, j, domain)
				}
				// Duplicate within host check (case-insensitive)
				normalizedDomain := strings.ToLower(domain)
				if seenInHost[normalizedDomain] {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: duplicate domain within same host", i, j, domain)
				}
				seenInHost[normalizedDomain] = true

				// Cross-host duplicate check (case-insensitive)
				if domains[normalizedDomain] {
					collector.Add(filename, 0, "host[%d]: domain[%d] %q: duplicate domain (already used by another host)", i, j, domain)
				}
				domains[normalizedDomain] = true
			}
		}

		if host.RenderKey == "" {
			collector.Add(filename, 0, "host[%d]: render_key is required", i)
		}

		// Validate host ID (critical for cache key generation)
		if host.ID <= 0 {
			collector.Add(filename, 0, "host[%d] (%s): id must be positive (got %d)", i, host.Domain, host.ID)
		}

		if host.RenderKey != "" {
			if renderKeys[host.RenderKey] {
				collector.Add(filename, 0, "duplicate render_key for domain: %s", host.Domain)
			}
			renderKeys[host.RenderKey] = true
		}

		// Check for duplicate host IDs
		if host.ID > 0 {
			if existingDomain, exists := hostIDs[host.ID]; exists {
				collector.Add(filename, 0, "duplicate host id %d: used by both '%s' and '%s' (cache key collision)",
					host.ID, existingDomain, host.Domain)
			}
			hostIDs[host.ID] = host.Domain
		}

		// Validate render timeout
		if host.Render.Timeout <= 0 {
			collector.Add(filename, 0, "host[%d] (%s): render.timeout must be positive", i, host.Domain)
		}

		// Validate dimensions
		validateDimensions(i, host, filename, hasGlobalDimensions, ht, collector)

		// Validate unmatched_dimension configuration
		validateUnmatchedDimension(i, host, filename, ht, collector)

		// Validate render events configuration
		contextPrefix := fmt.Sprintf("host[%d] (%s)", i, host.Domain)
		validateRenderEvents(&host.Render.Events, contextPrefix, filename, collector)

		// Validate render cache
		if host.Render.Cache != nil {
			validateHostRenderCache(i, host, filename, ht, collector)
		}

		// Validate URL rules
		if len(host.URLRules) > 0 {
			validateURLRules(i, host, filename, ht, collector)
		}

		// Validate timeout ranges
		validateHostTimeoutRanges(i, host, filename, collector)

		// Validate bypass cache
		if host.Bypass != nil && host.Bypass.Cache != nil {
			validateHostBypassCache(i, host, filename, ht, collector)
		}

		// Validate tracking params
		validateHostTrackingParams(i, host, filename, collector)

		// Validate bothit_recache
		validateHostBothitRecache(i, host, filename, collector)

		// Validate safe_headers
		validateHostHeaders(i, host, filename, collector)

		// Validate client_ip
		validateHostClientIP(i, host, filename, collector)

		// Validate blocked resource types
		validateHostBlockedResourceTypes(i, host, filename, collector)
	}
}

// validateDimensions validates dimension configuration
func validateDimensions(hostIndex int, host *types.Host, filename string, hasGlobalDimensions bool, ht *HostsLineTracker, collector *ErrorCollector) {
	if len(host.Render.Dimensions) == 0 {
		if !hasGlobalDimensions {
			collector.Add(filename, 0, "host[%d] (%s): at least one dimension must be configured (no global dimensions defined)", hostIndex, host.Domain)
		}
		return
	}

	dimensionIDs := make(map[int]string)

	for dimensionName, dimension := range host.Render.Dimensions {
		// Validate dimension ID
		if dimension.ID <= 0 {
			collector.Add(filename, 0, "host[%d] (%s): dimension '%s' has invalid ID %d (must be positive)",
				hostIndex, host.Domain, dimensionName, dimension.ID)
		}

		// Check for duplicate IDs
		if existingDimension, exists := dimensionIDs[dimension.ID]; exists {
			collector.Add(filename, 0, "host[%d] (%s): dimension ID %d is used by both '%s' and '%s' (must be unique)",
				hostIndex, host.Domain, dimension.ID, existingDimension, dimensionName)
		}
		dimensionIDs[dimension.ID] = dimensionName

		// Validate dimension fields
		if dimension.Width <= 0 {
			collector.Add(filename, 0, "host[%d] (%s): dimension '%s' has invalid width %d (must be positive)",
				hostIndex, host.Domain, dimensionName, dimension.Width)
		}
		if dimension.Height <= 0 {
			collector.Add(filename, 0, "host[%d] (%s): dimension '%s' has invalid height %d (must be positive)",
				hostIndex, host.Domain, dimensionName, dimension.Height)
		}

		// Validate match_ua patterns
		for _, pattern := range dimension.MatchUA {
			if pattern == "" {
				collector.Add(filename, 0, "host[%d] (%s): dimension '%s': match_ua pattern contains empty string",
					hostIndex, host.Domain, dimensionName)
				continue
			}
			if err := validatePatternSyntax(pattern, "user-agent"); err != nil {
				collector.Add(filename, 0, "host[%d] (%s): dimension '%s': invalid match_ua pattern '%s': %v",
					hostIndex, host.Domain, dimensionName, pattern, err)
			}
		}
	}
}

// validateUnmatchedDimension validates unmatched_dimension configuration
func validateUnmatchedDimension(hostIndex int, host *types.Host, filename string, ht *HostsLineTracker, collector *ErrorCollector) {
	unmatchedDim := host.Render.UnmatchedDimension

	// Empty value is valid - will be defaulted to "bypass" later
	if unmatchedDim == "" {
		return
	}

	// Check if it's a valid constant
	if unmatchedDim == types.UnmatchedDimensionBlock || unmatchedDim == types.UnmatchedDimensionBypass {
		return
	}

	// Otherwise, it must be a valid dimension name
	if _, exists := host.Render.Dimensions[unmatchedDim]; !exists {
		collector.Add(filename, 0, "host[%d] (%s): unmatched_dimension '%s' is invalid (must be '%s', '%s', or a dimension name defined in this host's render.dimensions)",
			hostIndex, host.Domain, unmatchedDim, types.UnmatchedDimensionBlock, types.UnmatchedDimensionBypass)
	}
}

// validateGlobalUnmatchedDimension validates global unmatched_dimension configuration
func validateGlobalUnmatchedDimension(cfg *configtypes.EgConfig, filename string, collector *ErrorCollector) {
	unmatchedDim := cfg.Render.UnmatchedDimension

	// Empty value is valid - will be defaulted to "bypass" later
	if unmatchedDim == "" {
		return
	}

	// Check if it's a valid constant
	if unmatchedDim == types.UnmatchedDimensionBlock || unmatchedDim == types.UnmatchedDimensionBypass {
		return
	}

	// Otherwise, it must be a valid dimension name in global dimensions
	if _, exists := cfg.Render.Dimensions[unmatchedDim]; !exists {
		collector.Add(filename, 0, "render.unmatched_dimension '%s' is invalid (must be '%s', '%s', or a dimension name defined in render.dimensions at the global level)",
			unmatchedDim, types.UnmatchedDimensionBlock, types.UnmatchedDimensionBypass)
	}
}

// validateRenderEvents validates render.events configuration
func validateRenderEvents(events *types.RenderEvents, contextPrefix string, filename string, collector *ErrorCollector) {
	if events == nil {
		return
	}

	// Validate wait_for if specified
	if events.WaitFor != "" {
		validEvents := []string{
			types.LifecycleEventDOMContentLoaded,
			types.LifecycleEventLoad,
			types.LifecycleEventNetworkIdle,
			types.LifecycleEventNetworkAlmostIdle,
		}

		isValid := false
		for _, valid := range validEvents {
			if events.WaitFor == valid {
				isValid = true
				break
			}
		}

		if !isValid {
			collector.Add(filename, 0, "%s: events.wait_for '%s' is invalid (must be '%s', '%s', '%s', or '%s')",
				contextPrefix, events.WaitFor,
				types.LifecycleEventDOMContentLoaded,
				types.LifecycleEventLoad,
				types.LifecycleEventNetworkIdle,
				types.LifecycleEventNetworkAlmostIdle)
		}
	}

	// Validate additional_wait
	if events.AdditionalWait != nil {
		additionalWait := time.Duration(*events.AdditionalWait)
		validateDurationUnit(additionalWait, contextPrefix+".additional_wait", filename, collector)

		// Cannot be negative
		if additionalWait < 0 {
			collector.Add(filename, 0, "%s: events.additional_wait cannot be negative (got %s)",
				contextPrefix, additionalWait)
		}

		// Cannot exceed 30 seconds
		if additionalWait > 30*time.Second {
			collector.Add(filename, 0, "%s: events.additional_wait cannot exceed 30s (got %s)",
				contextPrefix, additionalWait)
		}
	}
}

// validateURLRules validates URL rules configuration
func validateURLRules(hostIndex int, host *types.Host, filename string, ht *HostsLineTracker, collector *ErrorCollector) {
	for i, rule := range host.URLRules {
		// Validate match patterns
		patterns := rule.GetMatchPatterns()
		if len(patterns) == 0 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: match pattern cannot be empty",
				hostIndex, host.Domain, i)
			continue
		}

		// Validate pattern syntax
		for _, pattern := range patterns {
			if pattern == "" {
				collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: match pattern contains empty string",
					hostIndex, host.Domain, i)
				continue
			}
			if err := validatePatternSyntax(pattern, "URL"); err != nil {
				collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: invalid pattern '%s': %v",
					hostIndex, host.Domain, i, pattern, err)
			}
		}

		// Validate action
		if !rule.Action.IsValid() {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: invalid action '%s'",
				hostIndex, host.Domain, i, rule.Action)
		}

		// Validate tracking params at pattern level
		if rule.TrackingParams != nil {
			if err := validateTrackingParamsInternal(rule.TrackingParams, fmt.Sprintf("host[%d] (%s): url_rules[%d]", hostIndex, host.Domain, i)); err != nil {
				collector.Add(filename, 0, "%v", err)
			}
		}

		// Validate bothit_recache at pattern level
		if rule.BothitRecache != nil {
			if err := validateBothitRecacheInternal(rule.BothitRecache, fmt.Sprintf("host[%d] (%s): url_rules[%d]", hostIndex, host.Domain, i)); err != nil {
				collector.Add(filename, 0, "%v", err)
			}
		}

		// Validate headers at pattern level
		if rule.Headers != nil {
			if err := validateHeadersConfig(rule.Headers, fmt.Sprintf("host[%d] (%s): url_rules[%d]", hostIndex, host.Domain, i)); err != nil {
				collector.Add(filename, 0, "%v", err)
			}
		}

		// Validate action-specific configuration
		validateRuleActionConfig(hostIndex, i, &rule, host, filename, ht, collector)
	}
}

// validateRuleActionConfig validates action-specific configuration
func validateRuleActionConfig(hostIndex, ruleIndex int, rule *types.URLRule, host *types.Host, filename string, ht *HostsLineTracker, collector *ErrorCollector) {
	switch rule.Action {
	case types.ActionRender:
		// Validate render overrides
		if rule.Render != nil {
			if rule.Render.Cache != nil {
				validateRenderCacheOverride(hostIndex, ruleIndex, host.Domain, rule.Render.Cache, filename, collector)
			}
			// Validate dimension exists
			if rule.Render.Dimension != "" {
				if _, exists := host.Render.Dimensions[rule.Render.Dimension]; !exists {
					collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: render.dimension '%s' does not exist",
						hostIndex, host.Domain, ruleIndex, rule.Render.Dimension)
				}
			}
			// Validate unmatched_dimension override
			if rule.Render.UnmatchedDimension != "" {
				unmatchedDim := rule.Render.UnmatchedDimension
				// Check if it's a valid constant
				if unmatchedDim != types.UnmatchedDimensionBlock && unmatchedDim != types.UnmatchedDimensionBypass {
					// Otherwise, must be a valid dimension name from host dimensions
					if _, exists := host.Render.Dimensions[unmatchedDim]; !exists {
						collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: render.unmatched_dimension '%s' is invalid (must be '%s', '%s', or a dimension name defined in this host's render.dimensions)",
							hostIndex, host.Domain, ruleIndex, unmatchedDim, types.UnmatchedDimensionBlock, types.UnmatchedDimensionBypass)
					}
				}
			}
			// Validate events override
			if rule.Render.Events != nil {
				contextPrefix := fmt.Sprintf("host[%d] (%s): url_rules[%d]", hostIndex, host.Domain, ruleIndex)
				validateRenderEvents(rule.Render.Events, contextPrefix, filename, collector)
			}
			// Validate blocked resource types
			for _, rt := range rule.Render.BlockedResourceTypes {
				if !validResourceTypes[rt] {
					collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: invalid blocked_resource_type '%s'",
						hostIndex, host.Domain, ruleIndex, rt)
				}
			}
		}

	case types.ActionBypass:
		// Validate bypass cache
		if rule.Bypass != nil && rule.Bypass.Cache != nil {
			validateBypassCacheOverride(hostIndex, ruleIndex, host.Domain, rule.Bypass.Cache, filename, collector)
		}

	case types.ActionBlock, types.ActionStatus403, types.ActionStatus404, types.ActionStatus410, types.ActionStatus:
		// Validate status configuration
		validateStatusConfig(hostIndex, ruleIndex, host.Domain, rule, filename, collector)
	}
}

// validateRenderCacheOverride validates render cache overrides
func validateRenderCacheOverride(hostIndex, ruleIndex int, domain string, cache *types.RenderCacheOverride, filename string, collector *ErrorCollector) {
	if cache.TTL != nil {
		ttl := time.Duration(*cache.TTL)
		validateDurationUnit(ttl, fmt.Sprintf("host[%d] (%s): url_rules[%d]: render.cache.ttl", hostIndex, domain, ruleIndex), filename, collector)
		if *cache.TTL < 0 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: render.cache.ttl cannot be negative",
				hostIndex, domain, ruleIndex)
		}
	}

	if cache.StatusCodes != nil && len(cache.StatusCodes) == 0 {
		collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: render.cache.status_codes cannot be empty",
			hostIndex, domain, ruleIndex)
	}

	for _, code := range cache.StatusCodes {
		if code < 100 || code >= 600 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: invalid HTTP status code: %d",
				hostIndex, domain, ruleIndex, code)
		}
	}

	// Validate expiration configuration
	context := fmt.Sprintf("host[%d] (%s): url_rules[%d]: render.cache", hostIndex, domain, ruleIndex)
	validateCacheExpiredConfig(cache.Expired, context, filename, collector)
}

// validateBypassCacheOverride validates bypass cache overrides
func validateBypassCacheOverride(hostIndex, ruleIndex int, domain string, cache *types.BypassCacheConfig, filename string, collector *ErrorCollector) {
	if cache.TTL != nil {
		ttl := time.Duration(*cache.TTL)
		validateDurationUnit(ttl, fmt.Sprintf("host[%d] (%s): url_rules[%d]: bypass.cache.ttl", hostIndex, domain, ruleIndex), filename, collector)
		if *cache.TTL < 0 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: bypass.cache.ttl cannot be negative",
				hostIndex, domain, ruleIndex)
		}
	}

	if cache.StatusCodes != nil && len(cache.StatusCodes) == 0 {
		collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: bypass.cache.status_codes cannot be empty",
			hostIndex, domain, ruleIndex)
	}

	for _, code := range cache.StatusCodes {
		if code < 100 || code >= 600 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: invalid HTTP status code: %d",
				hostIndex, domain, ruleIndex, code)
		}
	}
}

// validateStatusConfig validates status action configuration
func validateStatusConfig(hostIndex, ruleIndex int, domain string, rule *types.URLRule, filename string, collector *ErrorCollector) {
	// For generic 'status' action, code is required
	if rule.Action == types.ActionStatus {
		if rule.Status == nil || rule.Status.Code == nil {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: status.code is required for action='status'",
				hostIndex, domain, ruleIndex)
			return
		}
	}

	// Validate status code if provided
	if rule.Status != nil && rule.Status.Code != nil {
		code := *rule.Status.Code

		// Valid status codes: 3xx (300-399), 4xx (400-499), 5xx (500-599)
		if code < 300 || code >= 600 {
			collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: status.code must be 3xx, 4xx, or 5xx (got %d)",
				hostIndex, domain, ruleIndex, code)
		}

		// Validate Location header for 3xx redirects
		statusClass := code / 100
		if statusClass == 3 {
			if rule.Status.Headers == nil || rule.Status.Headers["Location"] == "" {
				collector.Add(filename, 0, "host[%d] (%s): url_rules[%d]: status.headers.Location is required for 3xx redirect (code %d)",
					hostIndex, domain, ruleIndex, code)
			}
		}
	}
}

// validateHostBypassCache validates host-level bypass cache configuration
func validateHostBypassCache(hostIndex int, host *types.Host, filename string, ht *HostsLineTracker, collector *ErrorCollector) {
	cache := host.Bypass.Cache

	if cache.TTL != nil {
		ttl := time.Duration(*cache.TTL)
		validateDurationUnit(ttl, fmt.Sprintf("host[%d] (%s): bypass.cache.ttl", hostIndex, host.Domain), filename, collector)
		if *cache.TTL < 0 {
			collector.Add(filename, 0, "host[%d] (%s): bypass.cache.ttl cannot be negative", hostIndex, host.Domain)
		}
	}

	if cache.StatusCodes != nil && len(cache.StatusCodes) == 0 {
		collector.Add(filename, 0, "host[%d] (%s): bypass.cache.status_codes cannot be empty", hostIndex, host.Domain)
	}

	for _, code := range cache.StatusCodes {
		if code < 100 || code >= 600 {
			collector.Add(filename, 0, "host[%d] (%s): invalid HTTP status code: %d", hostIndex, host.Domain, code)
		}
	}
}

// validateHostRenderCache validates host-level render cache configuration
func validateHostRenderCache(hostIndex int, host *types.Host, filename string, ht *HostsLineTracker, collector *ErrorCollector) {
	cache := host.Render.Cache

	// Validate TTL
	if cache.TTL != nil {
		ttl := time.Duration(*cache.TTL)
		validateDurationUnit(ttl, fmt.Sprintf("host[%d] (%s): render.cache.ttl", hostIndex, host.Domain), filename, collector)
		if *cache.TTL < 0 {
			collector.Add(filename, 0, "host[%d] (%s): render.cache.ttl cannot be negative", hostIndex, host.Domain)
		}
	}

	// Validate status codes
	if cache.StatusCodes != nil && len(cache.StatusCodes) == 0 {
		collector.Add(filename, 0, "host[%d] (%s): render.cache.status_codes cannot be empty", hostIndex, host.Domain)
	}

	for _, code := range cache.StatusCodes {
		if code < 100 || code >= 600 {
			collector.Add(filename, 0, "host[%d] (%s): invalid HTTP status code: %d", hostIndex, host.Domain, code)
		}
	}

	// Validate expiration configuration
	context := fmt.Sprintf("host[%d] (%s): render.cache", hostIndex, host.Domain)
	validateCacheExpiredConfig(cache.Expired, context, filename, collector)
}

// validateHostTimeoutRanges validates host-level timeout configuration and warns about dangerous values
func validateHostTimeoutRanges(hostIndex int, host *types.Host, filename string, collector *ErrorCollector) {
	renderTimeout := time.Duration(host.Render.Timeout)
	validateDurationUnit(renderTimeout, fmt.Sprintf("host[%d] (%s): render.timeout", hostIndex, host.Domain), filename, collector)

	// ERROR for critically low timeouts
	if renderTimeout < 5*time.Second {
		collector.Add(filename, 0, "host[%d] (%s): render.timeout (%s) is too low (minimum 5s required). JavaScript execution will fail",
			hostIndex, host.Domain, host.Render.Timeout)
		return // Don't add warning if we already added error
	}

	// WARNING for low timeouts
	if renderTimeout < 10*time.Second {
		collector.AddWarning(filename, 0, "host[%d] (%s): render.timeout (%s) is very low. Only suitable for simple static pages. JavaScript-heavy pages will fail. Recommended minimum: 10s",
			hostIndex, host.Domain, host.Render.Timeout)
	}

	// WARNING for very high timeouts
	if renderTimeout > 120*time.Second {
		collector.AddWarning(filename, 0, "host[%d] (%s): render.timeout (%s) is very high. Pages taking over 120s (2 minutes) are likely broken or have infinite loops. Consider investigating page performance",
			hostIndex, host.Domain, host.Render.Timeout)
	}

	// Host-level bypass timeout (if overridden)
	if host.Bypass != nil && host.Bypass.Timeout != nil {
		bypassTimeout := time.Duration(*host.Bypass.Timeout)
		validateDurationUnit(bypassTimeout, fmt.Sprintf("host[%d] (%s): bypass.timeout", hostIndex, host.Domain), filename, collector)
		if bypassTimeout < 5*time.Second {
			collector.AddWarning(filename, 0, "host[%d] (%s): bypass.timeout (%s) is low. Slow origins may timeout. Recommended minimum: 5s",
				hostIndex, host.Domain, *host.Bypass.Timeout)
		}
		if bypassTimeout > 60*time.Second {
			collector.AddWarning(filename, 0, "host[%d] (%s): bypass.timeout (%s) is high. May indicate origin performance issues",
				hostIndex, host.Domain, *host.Bypass.Timeout)
		}
	}
}

// validateCrossConfig validates cross-config dependencies
func validateCrossConfig(egConfig *configtypes.EgConfig, hostsConfig *configtypes.HostsConfig, collector *ErrorCollector) {
	// Skip cross-validation if configs couldn't be loaded (YAML syntax errors)
	if egConfig == nil || hostsConfig == nil {
		return
	}

	// Validate server timeout against host timeouts
	validateServerTimeout(egConfig, hostsConfig, collector)

	// Validate bypass timeout against server timeout
	validateBypassTimeout(egConfig, collector)

	// Validate storage configuration (with cross-host validation)
	validateStorageConfig(egConfig, hostsConfig, "edge-gateway.yaml", collector)
}

// getMaxHostRenderTimeout finds the maximum render timeout across all hosts
func getMaxHostRenderTimeout(hosts *configtypes.HostsConfig) time.Duration {
	maxTimeout := time.Duration(0)
	for _, host := range hosts.Hosts {
		if time.Duration(host.Render.Timeout) > maxTimeout {
			maxTimeout = time.Duration(host.Render.Timeout)
		}
	}
	return maxTimeout
}

// getMinStaleTTL finds the minimum stale_ttl across all hosts that have it configured
func getMinStaleTTL(hosts *configtypes.HostsConfig) time.Duration {
	minTTL := time.Duration(0)
	for _, host := range hosts.Hosts {
		if host.Render.Cache != nil && host.Render.Cache.Expired != nil &&
			host.Render.Cache.Expired.StaleTTL != nil {
			staleTTL := time.Duration(*host.Render.Cache.Expired.StaleTTL)
			if minTTL == 0 || staleTTL < minTTL {
				minTTL = staleTTL
			}
		}
	}
	return minTTL
}

// validateServerTimeout ensures server timeout is large enough for render operations
func validateServerTimeout(egConfig *configtypes.EgConfig, hostsConfig *configtypes.HostsConfig, collector *ErrorCollector) {
	const OVERHEAD = 10 * time.Second // Network, processing, cache operations overhead

	maxHostTimeout := getMaxHostRenderTimeout(hostsConfig)
	if maxHostTimeout == 0 {
		// No hosts configured or all have 0 timeout - skip validation
		return
	}

	// Calculate max concurrent wait (80% of max timeout, capped at 60s)
	maxConcurrentWait := time.Duration(float64(maxHostTimeout) * 0.8)
	if maxConcurrentWait > 60*time.Second {
		maxConcurrentWait = 60 * time.Second
	}

	requiredMinimum := maxConcurrentWait + maxHostTimeout + OVERHEAD

	if time.Duration(egConfig.Server.Timeout) < requiredMinimum {
		collector.Add("edge-gateway.yaml", 0,
			"server.timeout (%s) is too small. "+
				"Required minimum: max_concurrent_wait (%s) + max_host_render_timeout (%s) + overhead (%s) = %s. "+
				"Either increase server.timeout or reduce host render timeouts",
			egConfig.Server.Timeout,
			maxConcurrentWait,
			maxHostTimeout,
			OVERHEAD,
			requiredMinimum,
		)
	}
}

// validateBypassTimeout ensures bypass timeout doesn't exceed server timeout
func validateBypassTimeout(egConfig *configtypes.EgConfig, collector *ErrorCollector) {
	if egConfig.Bypass.Timeout != nil && *egConfig.Bypass.Timeout > egConfig.Server.Timeout {
		collector.Add("edge-gateway.yaml", 0,
			"bypass.timeout (%s) exceeds server.timeout (%s). "+
				"Bypass operations will never complete successfully",
			*egConfig.Bypass.Timeout,
			egConfig.Server.Timeout,
		)
	}
}

// validatePatternSyntax validates pattern syntax to catch common mistakes
func validatePatternSyntax(pattern, context string) error {
	// Allow match-all pattern
	if pattern == "*" {
		// Intentional catch-all rule
		return nil
	}

	// Check for consecutive wildcards (likely a mistake)
	// Note: ** is treated the same as * (both are recursive), but consecutive wildcards suggest an error
	if strings.Contains(pattern, "**") || strings.Contains(pattern, "***") {
		return fmt.Errorf("pattern contains consecutive wildcards '**' - use single '*' for recursive matching")
	}

	// Validate regexp patterns
	if strings.HasPrefix(pattern, "~*") {
		// Case-insensitive regexp
		regexpPattern := pattern[2:]
		if regexpPattern == "" {
			return fmt.Errorf("%s pattern '~*' is empty", context)
		}
		// Compile with case-insensitive flag (matching runtime behavior)
		if _, err := regexp.Compile("(?i)" + regexpPattern); err != nil {
			return fmt.Errorf("invalid %s case-insensitive regexp '~*%s': %w", context, regexpPattern, err)
		}
	} else if strings.HasPrefix(pattern, "~") {
		// Case-sensitive regexp
		regexpPattern := pattern[1:]
		if regexpPattern == "" {
			return fmt.Errorf("%s pattern '~' is empty", context)
		}
		if _, err := regexp.Compile(regexpPattern); err != nil {
			return fmt.Errorf("invalid %s case-sensitive regexp '~%s': %w", context, regexpPattern, err)
		}
	}

	return nil
}

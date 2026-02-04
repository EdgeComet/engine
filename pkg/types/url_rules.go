package types

import (
	"fmt"
	"regexp"
	"time"

	"github.com/edgecomet/engine/pkg/pattern"
)

// URLRuleAction defines the action type for URL patterns
type URLRuleAction string

// Action constants
const (
	ActionRender    URLRuleAction = "render"     // Render with Chrome, cache result
	ActionBypass    URLRuleAction = "bypass"     // Fetch directly from origin, no rendering
	ActionBlock     URLRuleAction = "block"      // Reject request with 403 (alias for status_403)
	ActionStatus403 URLRuleAction = "status_403" // Return 403 Forbidden (explicit)
	ActionStatus404 URLRuleAction = "status_404" // Return 404 Not Found
	ActionStatus410 URLRuleAction = "status_410" // Return 410 Gone
	ActionStatus    URLRuleAction = "status"     // Generic status with configurable code
)

// URLRule defines behavior for URLs matching specific patterns
// any added config fields should be processed in the config.expandMultiPatternRules
type URLRule struct {
	Match  interface{}   `yaml:"match" json:"match"`   // string or []string - URL pattern(s)
	Action URLRuleAction `yaml:"action" json:"action"` // "render" | "bypass" | "block" | "status_403" | "status_404" | "status_410" | "status"

	// Query parameter matching (optional, all conditions must match - AND logic)
	MatchQuery map[string]interface{} `yaml:"match_query,omitempty" json:"match_query,omitempty"`

	// Render overrides (only for action="render")
	Render *RenderRuleConfig `yaml:"render,omitempty" json:"render,omitempty"`

	// Bypass overrides (only for action="bypass")
	Bypass *BypassRuleConfig `yaml:"bypass,omitempty" json:"bypass,omitempty"`

	// Status configuration (only for status actions: block, status_403, status_404, status_410, status)
	Status *StatusRuleConfig `yaml:"status,omitempty" json:"status,omitempty"`

	// Tracking parameter stripping configuration (pattern-level override)
	TrackingParams *TrackingParamsConfig `yaml:"tracking_params,omitempty" json:"tracking_params,omitempty"`

	// Cache sharding configuration (pattern-level override)
	CacheSharding *CacheShardingBehaviorConfig `yaml:"cache_sharding,omitempty" json:"cache_sharding,omitempty"`

	// Bot hit automatic recache configuration (pattern-level override)
	BothitRecache *BothitRecacheConfig `yaml:"bothit_recache,omitempty" json:"bothit_recache,omitempty"`

	// Headers configuration (pattern-level override)
	Headers *HeadersConfig `yaml:"headers,omitempty" json:"headers,omitempty"`

	// matchPatterns is a cached, pre-computed slice of match patterns
	// Populated during UnmarshalYAML for zero-allocation access
	matchPatterns []string `yaml:"-" json:"-"`

	// patternMetadata stores pre-compiled patterns
	// Index corresponds to matchPatterns slice
	patternMetadata []*pattern.Pattern `yaml:"-" json:"-"`

	// QueryParamMetadata stores pre-compiled query parameter patterns
	// Key is the parameter name, value is array of patterns (for OR logic)
	QueryParamMetadata map[string][]*pattern.Pattern `yaml:"-" json:"-"`
}

// PatternMetadata contains pre-compiled pattern matching data (deprecated, use pattern.Pattern)
type PatternMetadata struct {
	PatternType           pattern.PatternType
	CompiledRegexp        *regexp.Regexp
	RegexpCaseInsensitive bool
}

// RenderCacheOverride defines cache overrides for URL patterns with action="render"
type RenderCacheOverride struct {
	TTL         *Duration           `yaml:"ttl,omitempty" json:"ttl,omitempty"`                   // Override TTL
	StatusCodes []int               `yaml:"status_codes,omitempty" json:"status_codes,omitempty"` // Override cacheable status codes
	Expired     *CacheExpiredConfig `yaml:"expired,omitempty" json:"expired,omitempty"`           // Override expiration behavior
}

// RenderRuleConfig defines render overrides for URL patterns
type RenderRuleConfig struct {
	Timeout              *Duration            `yaml:"timeout,omitempty" json:"timeout,omitempty"`                               // Override render timeout
	Dimension            string               `yaml:"dimension,omitempty" json:"dimension,omitempty"`                           // Force specific dimension
	UnmatchedDimension   string               `yaml:"unmatched_dimension,omitempty" json:"unmatched_dimension,omitempty"`       // Override unmatched User-Agent behavior
	Events               *RenderEvents        `yaml:"events,omitempty" json:"events,omitempty"`                                 // Override events
	Cache                *RenderCacheOverride `yaml:"cache,omitempty" json:"cache,omitempty"`                                   // Override cache behavior
	BlockedPatterns      []string             `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty"`             // Override blocked URL patterns
	BlockedResourceTypes []string             `yaml:"blocked_resource_types,omitempty" json:"blocked_resource_types,omitempty"` // Override blocked resource types
	StripScripts         *bool                `yaml:"strip_scripts,omitempty" json:"strip_scripts,omitempty"`
}

// BypassRuleConfig defines bypass overrides for URL patterns
type BypassRuleConfig struct {
	Timeout   *Duration          `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	UserAgent string             `yaml:"user_agent,omitempty" json:"user_agent,omitempty"`
	Cache     *BypassCacheConfig `yaml:"cache,omitempty" json:"cache,omitempty"`
}

// StatusRuleConfig defines status action configuration for HTTP status responses (3xx, 4xx, 5xx)
type StatusRuleConfig struct {
	Code    *int              `yaml:"code,omitempty" json:"code,omitempty"`       // HTTP status code (required for generic 'status' action, inferred for aliases)
	Reason  string            `yaml:"reason,omitempty" json:"reason,omitempty"`   // Optional reason for response body (4xx/5xx only)
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"` // Custom headers (Location required for 3xx)
}

// BypassCacheConfig defines caching behavior for bypass responses
// Used at global, host, and pattern levels with deep merge semantics
type BypassCacheConfig struct {
	// Enabled controls whether bypass responses are cached
	// nil = inherit from parent level
	// false = explicitly disable caching
	// true = explicitly enable caching
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// TTL is the cache duration for bypass responses
	// nil = inherit from parent level
	// 0 = explicitly disable caching for this pattern
	// >0 = cache for specified duration
	TTL *Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`

	// StatusCodes lists HTTP status codes to cache
	// nil or empty = inherit from parent level
	// non-empty = override with specified codes
	// Common: [200], [200, 404], [200, 301, 302, 404]
	StatusCodes []int `yaml:"status_codes,omitempty" json:"status_codes,omitempty"`
}

// BypassConfig defines bypass behavior configuration (can be global, host-level, or pattern-level)
type BypassConfig struct {
	Enabled   *bool              `yaml:"enabled,omitempty" json:"enabled,omitempty"` // Enable/disable bypass mode (pointer for override detection)
	Timeout   *Duration          `yaml:"timeout,omitempty" json:"timeout,omitempty"` // Timeout for bypass requests
	UserAgent string             `yaml:"user_agent" json:"user_agent"`
	Cache     *BypassCacheConfig `yaml:"cache,omitempty" json:"cache,omitempty"` // Bypass response caching configuration
}

// TrackingParamsConfig defines tracking parameter stripping configuration
// Can be specified at global, host, or URL pattern level with deep merge semantics
type TrackingParamsConfig struct {
	Strip     *bool    `yaml:"strip,omitempty" json:"strip,omitempty"`           // Master enable/disable switch (nil = inherit, true = strip, false = no stripping)
	Params    []string `yaml:"params,omitempty" json:"params,omitempty"`         // Replace all params (defaults + parents) with these patterns
	ParamsAdd []string `yaml:"params_add,omitempty" json:"params_add,omitempty"` // Extend parent params with these patterns
}

// BothitRecacheConfig defines automatic recache behavior triggered by bot hits
// Can be specified at global, host, or URL pattern level with deep merge semantics
// match_ua array uses replacement semantics (child replaces parent completely)
type BothitRecacheConfig struct {
	Enabled  *bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`   // Enable/disable automatic recache (pointer for override detection)
	Interval *Duration `yaml:"interval,omitempty" json:"interval,omitempty"` // Duration between recaches (30m to 24h, pointer for override detection)
	MatchUA  []string  `yaml:"match_ua,omitempty" json:"match_ua,omitempty"` // Bot User-Agent patterns (exact, wildcard, regexp support, replacement semantics)

	// CompiledPatterns stores pre-compiled user agent patterns
	CompiledPatterns []*pattern.Pattern `yaml:"-" json:"-"`
}

// Validate validates bothit_recache configuration
func (c *BothitRecacheConfig) Validate() error {
	if c == nil {
		return nil // nil config is valid (disabled)
	}

	// If enabled, validate required fields
	if c.Enabled != nil && *c.Enabled {
		// Validate interval is within allowed range
		if c.Interval != nil {
			interval := time.Duration(*c.Interval)
			if interval < 30*time.Minute {
				return fmt.Errorf("bothit_recache interval must be >= 30m, got %v", interval)
			}
			if interval > 24*time.Hour {
				return fmt.Errorf("bothit_recache interval must be <= 24h, got %v", interval)
			}
		}

		// Validate match_ua is non-empty
		if len(c.MatchUA) == 0 {
			return fmt.Errorf("bothit_recache.match_ua must be non-empty when enabled=true")
		}
	}

	return nil
}

// CompileMatchUAPatterns pre-compiles patterns for user agent matching
// Uses unified pattern package for consistent behavior:
// - No prefix: exact match (case-sensitive)
// - * wildcard: matches any characters
// - ~ prefix: case-sensitive regexp
// - ~* prefix: case-insensitive regexp
func (c *BothitRecacheConfig) CompileMatchUAPatterns() error {
	if len(c.MatchUA) == 0 {
		return nil
	}

	c.CompiledPatterns = make([]*pattern.Pattern, len(c.MatchUA))

	for i, pat := range c.MatchUA {
		compiled, err := pattern.Compile(pat)
		if err != nil {
			return fmt.Errorf("invalid bothit_recache user agent pattern '%s': %w", pat, err)
		}
		c.CompiledPatterns[i] = compiled
	}

	return nil
}

// CacheShardingConfig defines cache sharding configuration for multi-EG deployments
// Can be specified at global and host levels (not URL pattern level)
type CacheShardingConfig struct {
	Enabled              *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`                             // Enable/disable sharding (pointer for override detection)
	ReplicationFactor    *int   `yaml:"replication_factor,omitempty" json:"replication_factor,omitempty"`       // Number of EG instances to replicate cache to (pointer for override detection)
	DistributionStrategy string `yaml:"distribution_strategy,omitempty" json:"distribution_strategy,omitempty"` // hash_modulo | random | primary_only
	PushOnRender         *bool  `yaml:"push_on_render,omitempty" json:"push_on_render,omitempty"`               // Push to replicas after render (pointer for override detection)
	ReplicateOnPull      *bool  `yaml:"replicate_on_pull,omitempty" json:"replicate_on_pull,omitempty"`         // Store pulled cache locally (pointer for override detection)
}

// CacheShardingBehaviorConfig contains behavioral sharding settings that can be overridden per host/pattern
type CacheShardingBehaviorConfig struct {
	Enabled              *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`                             // Enable/disable sharding (pointer for override detection)
	ReplicationFactor    *int   `yaml:"replication_factor,omitempty" json:"replication_factor,omitempty"`       // Number of EG instances to replicate cache to (pointer for override detection)
	DistributionStrategy string `yaml:"distribution_strategy,omitempty" json:"distribution_strategy,omitempty"` // hash_modulo | random | primary_only
	PushOnRender         *bool  `yaml:"push_on_render,omitempty" json:"push_on_render,omitempty"`               // Push to replicas after render (pointer for override detection)
	ReplicateOnPull      *bool  `yaml:"replicate_on_pull,omitempty" json:"replicate_on_pull,omitempty"`         // Store pulled cache locally (pointer for override detection)
}

// GetMatchPatterns returns URL patterns as string slice (zero-allocation after unmarshaling)
func (r *URLRule) GetMatchPatterns() []string {
	// Return cached patterns if available (populated during UnmarshalYAML)
	if r.matchPatterns != nil {
		return r.matchPatterns
	}

	// Fallback for programmatically created URLRule instances (not from YAML)
	// This ensures backward compatibility with code that creates URLRule directly
	switch v := r.Match.(type) {
	case string:
		return []string{v}
	case []interface{}:
		patterns := make([]string, 0, len(v))
		for _, p := range v {
			if str, ok := p.(string); ok {
				patterns = append(patterns, str)
			}
		}
		return patterns
	case []string:
		return v
	default:
		return []string{}
	}
}

// UnmarshalYAML implements custom YAML unmarshaling to pre-compute match patterns
func (r *URLRule) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Define a temporary type to avoid infinite recursion
	type urlRuleAlias URLRule

	// Unmarshal into the alias
	alias := (*urlRuleAlias)(r)
	if err := unmarshal(alias); err != nil {
		return err
	}

	// Pre-compute match patterns for zero-allocation access
	r.matchPatterns = r.computeMatchPatterns()

	// Pre-compile regexp patterns and validate
	if err := r.compilePatterns(); err != nil {
		return err
	}

	// Validate match_query structure
	if err := r.validateMatchQuery(); err != nil {
		return err
	}

	// Pre-compile query parameter patterns
	if err := r.compileQueryParamPatterns(); err != nil {
		return err
	}

	return nil
}

// computeMatchPatterns converts Match field to []string for caching
func (r *URLRule) computeMatchPatterns() []string {
	switch v := r.Match.(type) {
	case string:
		if v == "" {
			return []string{}
		}
		return []string{v}
	case []interface{}:
		patterns := make([]string, 0, len(v))
		for _, p := range v {
			if str, ok := p.(string); ok && str != "" {
				patterns = append(patterns, str)
			}
		}
		return patterns
	case []string:
		// Filter out empty strings
		patterns := make([]string, 0, len(v))
		for _, p := range v {
			if p != "" {
				patterns = append(patterns, p)
			}
		}
		return patterns
	default:
		return []string{}
	}
}

// compilePatterns pre-compiles patterns using the unified pattern package
func (r *URLRule) compilePatterns() error {
	r.patternMetadata = make([]*pattern.Pattern, len(r.matchPatterns))

	for i, pat := range r.matchPatterns {
		compiled, err := pattern.Compile(pat)
		if err != nil {
			return fmt.Errorf("failed to compile pattern '%s': %w", pat, err)
		}
		r.patternMetadata[i] = compiled
	}

	return nil
}

// DetectPatternType is a compatibility wrapper that calls pattern.DetectPatternType
// Deprecated: Use pattern.DetectPatternType directly
func DetectPatternType(pat string) (pattern.PatternType, string, bool) {
	return pattern.DetectPatternType(pat)
}

// validateMatchQuery validates the match_query structure
func (r *URLRule) validateMatchQuery() error {
	if r.MatchQuery == nil {
		return nil
	}

	for key, value := range r.MatchQuery {
		if key == "" {
			return fmt.Errorf("match_query contains empty key")
		}

		// Validate value type
		switch v := value.(type) {
		case string:
			// Valid: exact match or wildcard
		case []interface{}:
			// Valid: OR logic (array of values)
			if len(v) == 0 {
				return fmt.Errorf("match_query key '%s' has empty array", key)
			}
			// Validate array elements are strings
			for _, item := range v {
				if _, ok := item.(string); !ok {
					return fmt.Errorf("match_query key '%s' array contains non-string value", key)
				}
			}
		default:
			return fmt.Errorf("match_query key '%s' has invalid type (must be string or array of strings)", key)
		}
	}

	return nil
}

// compileQueryParamPatterns pre-compiles query parameter patterns for fast matching
func (r *URLRule) compileQueryParamPatterns() error {
	if r.MatchQuery == nil {
		return nil
	}

	r.QueryParamMetadata = make(map[string][]*pattern.Pattern)

	for key, value := range r.MatchQuery {
		patterns, err := r.parseQueryParamValue(key, value)
		if err != nil {
			return err
		}
		r.QueryParamMetadata[key] = patterns
	}

	return nil
}

// parseQueryParamValue converts a query parameter value (string or []interface{}) into Pattern array
func (r *URLRule) parseQueryParamValue(key string, value interface{}) ([]*pattern.Pattern, error) {
	var patterns []*pattern.Pattern

	switch v := value.(type) {
	case string:
		// Single string value
		pat, err := parseQueryParamString(key, v)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pat)

	case []interface{}:
		// Array of values (OR logic)
		for i, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("match_query key '%s' array item %d is not a string", key, i)
			}
			pat, err := parseQueryParamString(key, str)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pat)
		}

	default:
		return nil, fmt.Errorf("match_query key '%s' has invalid type", key)
	}

	return patterns, nil
}

// parseQueryParamString parses a single query parameter string into a Pattern
func parseQueryParamString(key, value string) (*pattern.Pattern, error) {
	// Use the unified pattern.Compile function
	compiled, err := pattern.Compile(value)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern in match_query key '%s', value '%s': %w", key, value, err)
	}

	return compiled, nil
}

// GetCompiledPattern returns the compiled pattern for a given index
func (r *URLRule) GetCompiledPattern(index int) *pattern.Pattern {
	if index < 0 || index >= len(r.patternMetadata) {
		return nil
	}
	return r.patternMetadata[index]
}

// CompilePatterns compiles patterns for programmatically-created URLRule instances
// This is a convenience method for testing and dynamic rule creation
func (r *URLRule) CompilePatterns() error {
	// Pre-compute match patterns
	r.matchPatterns = r.computeMatchPatterns()

	// Pre-compile regexp patterns
	if err := r.compilePatterns(); err != nil {
		return err
	}

	// Validate match_query structure
	if err := r.validateMatchQuery(); err != nil {
		return err
	}

	// Pre-compile query parameter patterns
	if err := r.compileQueryParamPatterns(); err != nil {
		return err
	}

	return nil
}

// IsValid checks if the action is valid
func (a URLRuleAction) IsValid() bool {
	return a == ActionRender || a == ActionBypass ||
		a == ActionBlock || a == ActionStatus403 ||
		a == ActionStatus404 || a == ActionStatus410 ||
		a == ActionStatus
}

// IsStatusAction returns true if the action is any status-related action
func (a URLRuleAction) IsStatusAction() bool {
	return a == ActionBlock || a == ActionStatus403 ||
		a == ActionStatus404 || a == ActionStatus410 ||
		a == ActionStatus
}

// NormalizeBlockAction returns ActionStatus403 for both "block" and "block_403"
// This ensures consistent handling while maintaining backward compatibility
func (a URLRuleAction) NormalizeBlockAction() URLRuleAction {
	if a == ActionBlock {
		return ActionStatus403
	}
	return a
}

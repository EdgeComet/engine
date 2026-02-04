package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// Fallback constants when optional configuration fields are nil
const (
	defaultBypassTimeout = 30 * time.Second
	defaultCacheTTL      = 1 * time.Hour
)

// ResolvedConfig contains fully resolved configuration for a specific URL
// after merging global → host → pattern levels
type ResolvedConfig struct {
	Action              types.URLRuleAction  // render, bypass, block, status_403, status_404, status_410, status
	Status              ResolvedStatusConfig // status action configuration
	Cache               ResolvedCacheConfig
	Render              ResolvedRenderConfig
	Bypass              ResolvedBypassConfig
	TrackingParams      *ResolvedTrackingParams // tracking parameter stripping configuration
	Sharding            ResolvedShardingConfig  // cache sharding configuration
	BothitRecache       ResolvedBothitRecache   // bot hit automatic recache configuration
	SafeRequestHeaders  []string                // client request headers to forward to origin
	SafeResponseHeaders []string                // response headers to return to client
	MatchedRuleID       string                  // Identifier of the matched URL rule (empty if no rule matched)
	MatchedPattern      string                  // The URL pattern that matched (e.g., "/blog/*")
	Compression         string                  // Storage compression algorithm: none, snappy, lz4
}

// ResolvedCacheConfig contains resolved cache configuration
type ResolvedCacheConfig struct {
	TTL         time.Duration
	StatusCodes []int
	Expired     types.CacheExpiredConfig
}

// ResolvedRenderConfig contains resolved render configuration
type ResolvedRenderConfig struct {
	Timeout              time.Duration
	Dimension            string // empty = use detected
	UnmatchedDimension   string // block, bypass, or dimension name fallback
	Events               types.RenderEvents
	BlockedPatterns      []string // Merged global → host → pattern
	BlockedResourceTypes []string // Merged global → host → pattern
	StripScripts         bool     // Whether to strip executable scripts from rendered HTML
}

// ResolvedBypassConfig contains resolved bypass configuration
type ResolvedBypassConfig struct {
	Timeout   time.Duration
	UserAgent string
	Cache     ResolvedBypassCacheConfig
}

// ResolvedBypassCacheConfig contains resolved bypass cache configuration
type ResolvedBypassCacheConfig struct {
	Enabled     bool
	TTL         time.Duration
	StatusCodes []int
}

// ResolvedStatusConfig contains resolved status action configuration
type ResolvedStatusConfig struct {
	Code    int               // HTTP status code (3xx, 4xx, 5xx)
	Reason  string            // Optional reason for response body
	Headers map[string]string // Custom headers (can override defaults)
}

// ResolvedTrackingParams contains resolved tracking parameter stripping configuration
type ResolvedTrackingParams struct {
	Enabled          bool                   // Master switch: true = strip enabled, false = disabled
	CompiledPatterns []CompiledStripPattern // Pre-compiled patterns for fast matching
}

// CompiledStripPattern contains pre-compiled pattern matching data for tracking params
type CompiledStripPattern struct {
	Compiled *pattern.Pattern // Pre-compiled pattern (handles exact, wildcard, and regexp)
}

// ResolvedShardingConfig contains resolved cache sharding configuration
type ResolvedShardingConfig struct {
	Enabled              bool
	ReplicationFactor    int
	DistributionStrategy string
	PushOnRender         bool
	ReplicateOnPull      bool
}

// ResolvedBothitRecache contains resolved bot hit automatic recache configuration
type ResolvedBothitRecache struct {
	Enabled          bool               // Master switch: true = bot tracking enabled
	Interval         time.Duration      // Duration between recaches (30m to 24h)
	MatchUA          []string           // Bot User-Agent patterns (exact, wildcard, regexp support)
	CompiledPatterns []*pattern.Pattern // Pre-compiled patterns for efficient matching
}

// ConfigResolver resolves configuration from global, host, and pattern levels
type ConfigResolver struct {
	globalRender         *GlobalRenderConfig
	globalBypass         *GlobalBypassConfig
	globalTrackingParams *types.TrackingParamsConfig
	globalSharding       *types.CacheShardingConfig
	globalBothitRecache  *types.BothitRecacheConfig
	globalHeaders        *types.HeadersConfig
	globalCompression    string // Global storage compression algorithm
	host                 *types.Host
	matcher              *PatternMatcher
}

// NewConfigResolver creates a new configuration resolver
func NewConfigResolver(globalRender *GlobalRenderConfig, globalBypass *GlobalBypassConfig, globalTrackingParams *types.TrackingParamsConfig, globalSharding *types.CacheShardingConfig, globalBothitRecache *types.BothitRecacheConfig, globalHeaders *types.HeadersConfig, globalCompression string, host *types.Host) *ConfigResolver {
	return &ConfigResolver{
		globalRender:         globalRender,
		globalBypass:         globalBypass,
		globalTrackingParams: globalTrackingParams,
		globalSharding:       globalSharding,
		globalBothitRecache:  globalBothitRecache,
		globalHeaders:        globalHeaders,
		globalCompression:    globalCompression,
		host:                 host,
		matcher:              NewPatternMatcher(host.URLRules),
	}
}

// ResolveForURL resolves configuration for the given URL
// Deep merge order: Global → Host → URL Pattern (first match)
func (r *ConfigResolver) ResolveForURL(targetURL string) *ResolvedConfig {
	// Find matching URL rule (returns nil, -1 if no match)
	matchedRule, ruleIndex := r.matcher.FindMatchingRule(targetURL)

	var ruleID string
	var matchedPattern string
	if matchedRule != nil {
		// Generate rule ID: rule_<index>:<pattern>
		patterns := matchedRule.GetMatchPatterns()
		patternStr := ""
		if len(patterns) > 0 {
			patternStr = patterns[0]
			matchedPattern = patternStr
		}
		ruleID = formatRuleID(ruleIndex, patternStr, matchedRule)
	}

	// Determine action (default is render if no rule matches)
	action := types.ActionRender
	if matchedRule != nil {
		action = matchedRule.Action
	}

	// Build resolved config based on action
	resolved := &ResolvedConfig{
		Action:         action,
		MatchedRuleID:  ruleID,
		MatchedPattern: matchedPattern,
	}

	// Resolve status configuration (for status actions)
	if action.IsStatusAction() {
		r.resolveStatusConfig(resolved, matchedRule)
	}

	// Resolve cache configuration (only for render action)
	if action == types.ActionRender {
		r.resolveCacheConfig(resolved, matchedRule)
		r.resolveRenderConfig(resolved, matchedRule)
	}

	// Resolve bypass configuration (for bypass action or fallback)
	r.resolveBypassConfig(resolved, matchedRule)

	// Resolve tracking params configuration (applies to all actions)
	r.resolveTrackingParams(resolved, matchedRule)

	// Resolve sharding configuration (applies to all actions)
	r.resolveShardingConfig(resolved, matchedRule)

	// Resolve bothit_recache configuration (applies to render action only)
	r.resolveBothitRecache(resolved, matchedRule)

	// Resolve safe headers configuration (applies to all actions)
	r.resolveHeaders(resolved, matchedRule)

	return resolved
}

// formatRuleID generates a human-readable rule identifier
func formatRuleID(index int, pattern string, rule *types.URLRule) string {
	// Format: rule_<index>:<pattern>[?<query_condition>]
	ruleID := fmt.Sprintf("rule_%d:%s", index, pattern)

	// Add query parameter indicator if match_query is specified
	if len(rule.MatchQuery) > 0 {
		ruleID += "?..."
	}

	return ruleID
}

// resolveCacheConfig resolves cache configuration with deep merge
func (r *ConfigResolver) resolveCacheConfig(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with global render cache defaults
	if r.globalRender.Cache.TTL != nil {
		resolved.Cache.TTL = time.Duration(*r.globalRender.Cache.TTL)
	} else {
		resolved.Cache.TTL = defaultCacheTTL
	}

	if len(r.globalRender.Cache.StatusCodes) > 0 {
		resolved.Cache.StatusCodes = r.globalRender.Cache.StatusCodes
	} else {
		resolved.Cache.StatusCodes = []int{200, 301, 302, 307, 308, 404}
	}

	if r.globalRender.Cache.Expired != nil {
		resolved.Cache.Expired = *r.globalRender.Cache.Expired
	} else {
		resolved.Cache.Expired = types.CacheExpiredConfig{
			Strategy: types.ExpirationStrategyDelete,
			StaleTTL: nil,
		}
	}

	// Compression: global-only setting from storage.compression
	resolved.Compression = r.globalCompression

	// Apply host-level overrides from host.Render.Cache
	if r.host.Render.Cache != nil {
		if r.host.Render.Cache.TTL != nil {
			resolved.Cache.TTL = time.Duration(*r.host.Render.Cache.TTL)
		}
		if len(r.host.Render.Cache.StatusCodes) > 0 {
			resolved.Cache.StatusCodes = r.host.Render.Cache.StatusCodes
		}
		// Atomic replacement: child completely replaces parent
		if r.host.Render.Cache.Expired != nil {
			resolved.Cache.Expired = *r.host.Render.Cache.Expired
		}
	}

	// Apply pattern-level overrides
	if matchedRule != nil && matchedRule.Render != nil && matchedRule.Render.Cache != nil {
		if matchedRule.Render.Cache.TTL != nil {
			resolved.Cache.TTL = time.Duration(*matchedRule.Render.Cache.TTL)
		}
		if len(matchedRule.Render.Cache.StatusCodes) > 0 {
			resolved.Cache.StatusCodes = matchedRule.Render.Cache.StatusCodes
		}
		// Atomic replacement: child completely replaces parent
		if matchedRule.Render.Cache.Expired != nil {
			resolved.Cache.Expired = *matchedRule.Render.Cache.Expired
		}
	}
}

// resolveRenderConfig resolves render configuration with deep merge
func (r *ConfigResolver) resolveRenderConfig(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with global defaults for blocked fields
	if len(r.globalRender.BlockedResourceTypes) > 0 {
		resolved.Render.BlockedResourceTypes = make([]string, len(r.globalRender.BlockedResourceTypes))
		copy(resolved.Render.BlockedResourceTypes, r.globalRender.BlockedResourceTypes)
	}
	if len(r.globalRender.BlockedPatterns) > 0 {
		resolved.Render.BlockedPatterns = make([]string, len(r.globalRender.BlockedPatterns))
		copy(resolved.Render.BlockedPatterns, r.globalRender.BlockedPatterns)
	}

	// Apply host-level defaults
	resolved.Render.Timeout = time.Duration(r.host.Render.Timeout)
	resolved.Render.Events = r.host.Render.Events
	resolved.Render.Dimension = "" // Empty means use detected dimension

	// Resolve UnmatchedDimension: Global → Host (replacement semantics)
	resolved.Render.UnmatchedDimension = r.globalRender.UnmatchedDimension
	if r.host.Render.UnmatchedDimension != "" {
		resolved.Render.UnmatchedDimension = r.host.Render.UnmatchedDimension
	}

	// Host render config overrides blocked fields (replaces global)
	if len(r.host.Render.BlockedResourceTypes) > 0 {
		resolved.Render.BlockedResourceTypes = r.host.Render.BlockedResourceTypes
	}
	if len(r.host.Render.BlockedPatterns) > 0 {
		resolved.Render.BlockedPatterns = r.host.Render.BlockedPatterns
	}

	// Apply pattern-level overrides
	if matchedRule != nil && matchedRule.Render != nil {
		if matchedRule.Render.Timeout != nil {
			resolved.Render.Timeout = time.Duration(*matchedRule.Render.Timeout)
		}
		if matchedRule.Render.Dimension != "" {
			resolved.Render.Dimension = matchedRule.Render.Dimension
		}
		if matchedRule.Render.UnmatchedDimension != "" {
			resolved.Render.UnmatchedDimension = matchedRule.Render.UnmatchedDimension
		}
		if matchedRule.Render.Events != nil {
			r.mergeRenderEvents(&resolved.Render.Events, matchedRule.Render.Events)
		}
		// Pattern-level blocked fields override all previous (highest priority)
		if len(matchedRule.Render.BlockedResourceTypes) > 0 {
			resolved.Render.BlockedResourceTypes = matchedRule.Render.BlockedResourceTypes
		}
		if len(matchedRule.Render.BlockedPatterns) > 0 {
			resolved.Render.BlockedPatterns = matchedRule.Render.BlockedPatterns
		}
	}

	// Resolve StripScripts (default: true - scripts stripped by default)
	stripScripts := true
	if r.globalRender != nil && r.globalRender.StripScripts != nil {
		stripScripts = *r.globalRender.StripScripts
	}
	if r.host.Render.StripScripts != nil {
		stripScripts = *r.host.Render.StripScripts
	}
	if matchedRule != nil && matchedRule.Render != nil && matchedRule.Render.StripScripts != nil {
		stripScripts = *matchedRule.Render.StripScripts
	}
	resolved.Render.StripScripts = stripScripts
}

// mergeRenderEvents performs deep merge of render events configuration
func (r *ConfigResolver) mergeRenderEvents(base *types.RenderEvents, override *types.RenderEvents) {
	if override.WaitFor != "" {
		base.WaitFor = override.WaitFor
	}
	if override.AdditionalWait != nil {
		base.AdditionalWait = override.AdditionalWait
	}
}

// resolveBypassConfig resolves bypass configuration with deep merge
func (r *ConfigResolver) resolveBypassConfig(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with global defaults (convert from types.Duration to time.Duration)
	if r.globalBypass.Timeout != nil {
		resolved.Bypass.Timeout = time.Duration(*r.globalBypass.Timeout)
	} else {
		resolved.Bypass.Timeout = defaultBypassTimeout
	}
	resolved.Bypass.UserAgent = r.globalBypass.UserAgent

	// Resolve bypass cache from global defaults
	if r.globalBypass.Cache.Enabled != nil {
		resolved.Bypass.Cache.Enabled = *r.globalBypass.Cache.Enabled
	} else {
		resolved.Bypass.Cache.Enabled = false // Default: disabled
	}
	if r.globalBypass.Cache.TTL != nil {
		resolved.Bypass.Cache.TTL = time.Duration(*r.globalBypass.Cache.TTL)
	} else {
		resolved.Bypass.Cache.TTL = 30 * time.Minute // Default: 30m
	}
	if len(r.globalBypass.Cache.StatusCodes) > 0 {
		resolved.Bypass.Cache.StatusCodes = r.globalBypass.Cache.StatusCodes
	} else {
		resolved.Bypass.Cache.StatusCodes = []int{200} // Default: [200]
	}

	// Apply host-level overrides
	if r.host.Bypass != nil {
		if r.host.Bypass.Timeout != nil {
			resolved.Bypass.Timeout = time.Duration(*r.host.Bypass.Timeout)
		}
		if r.host.Bypass.UserAgent != "" {
			resolved.Bypass.UserAgent = r.host.Bypass.UserAgent
		}

		// Apply host-level bypass cache overrides
		if r.host.Bypass.Cache != nil {
			if r.host.Bypass.Cache.Enabled != nil {
				resolved.Bypass.Cache.Enabled = *r.host.Bypass.Cache.Enabled
			}
			if r.host.Bypass.Cache.TTL != nil {
				resolved.Bypass.Cache.TTL = time.Duration(*r.host.Bypass.Cache.TTL)
			}
			if len(r.host.Bypass.Cache.StatusCodes) > 0 {
				resolved.Bypass.Cache.StatusCodes = r.host.Bypass.Cache.StatusCodes
			}
		}
	}

	// Apply pattern-level overrides (only for bypass action)
	if matchedRule != nil && matchedRule.Bypass != nil {
		if matchedRule.Bypass.Timeout != nil {
			resolved.Bypass.Timeout = time.Duration(*matchedRule.Bypass.Timeout)
		}
		if matchedRule.Bypass.UserAgent != "" {
			resolved.Bypass.UserAgent = matchedRule.Bypass.UserAgent
		}

		// Apply pattern-level bypass cache overrides
		if matchedRule.Bypass.Cache != nil {
			if matchedRule.Bypass.Cache.Enabled != nil {
				resolved.Bypass.Cache.Enabled = *matchedRule.Bypass.Cache.Enabled
			}
			if matchedRule.Bypass.Cache.TTL != nil {
				resolved.Bypass.Cache.TTL = time.Duration(*matchedRule.Bypass.Cache.TTL)
			}
			if len(matchedRule.Bypass.Cache.StatusCodes) > 0 {
				resolved.Bypass.Cache.StatusCodes = matchedRule.Bypass.Cache.StatusCodes
			}
		}
	}
}

// resolveShardingConfig resolves cache sharding configuration with deep merge
func (r *ConfigResolver) resolveShardingConfig(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with global defaults
	if r.globalSharding != nil && r.globalSharding.Enabled != nil {
		resolved.Sharding.Enabled = *r.globalSharding.Enabled
	} else {
		resolved.Sharding.Enabled = false // Default: disabled
	}

	if r.globalSharding != nil && r.globalSharding.ReplicationFactor != nil {
		resolved.Sharding.ReplicationFactor = *r.globalSharding.ReplicationFactor
	} else {
		resolved.Sharding.ReplicationFactor = 2 // Default: 2
	}

	if r.globalSharding != nil && r.globalSharding.DistributionStrategy != "" {
		resolved.Sharding.DistributionStrategy = r.globalSharding.DistributionStrategy
	} else {
		resolved.Sharding.DistributionStrategy = "hash_modulo" // Default
	}

	if r.globalSharding != nil && r.globalSharding.PushOnRender != nil {
		resolved.Sharding.PushOnRender = *r.globalSharding.PushOnRender
	} else {
		resolved.Sharding.PushOnRender = true // Default: enabled
	}

	if r.globalSharding != nil && r.globalSharding.ReplicateOnPull != nil {
		resolved.Sharding.ReplicateOnPull = *r.globalSharding.ReplicateOnPull
	} else {
		resolved.Sharding.ReplicateOnPull = true // Default: enabled
	}

	// Apply host-level overrides
	if r.host.CacheSharding != nil {
		if r.host.CacheSharding.Enabled != nil {
			resolved.Sharding.Enabled = *r.host.CacheSharding.Enabled
		}
		if r.host.CacheSharding.ReplicationFactor != nil {
			resolved.Sharding.ReplicationFactor = *r.host.CacheSharding.ReplicationFactor
		}
		if r.host.CacheSharding.DistributionStrategy != "" {
			resolved.Sharding.DistributionStrategy = r.host.CacheSharding.DistributionStrategy
		}
		if r.host.CacheSharding.PushOnRender != nil {
			resolved.Sharding.PushOnRender = *r.host.CacheSharding.PushOnRender
		}
		if r.host.CacheSharding.ReplicateOnPull != nil {
			resolved.Sharding.ReplicateOnPull = *r.host.CacheSharding.ReplicateOnPull
		}
	}

	// Apply pattern-level overrides
	if matchedRule != nil && matchedRule.CacheSharding != nil {
		if matchedRule.CacheSharding.PushOnRender != nil {
			resolved.Sharding.PushOnRender = *matchedRule.CacheSharding.PushOnRender
		}
		if matchedRule.CacheSharding.ReplicateOnPull != nil {
			resolved.Sharding.ReplicateOnPull = *matchedRule.CacheSharding.ReplicateOnPull
		}
		if matchedRule.CacheSharding.Enabled != nil {
			resolved.Sharding.Enabled = *matchedRule.CacheSharding.Enabled
		}
		if matchedRule.CacheSharding.ReplicationFactor != nil {
			resolved.Sharding.ReplicationFactor = *matchedRule.CacheSharding.ReplicationFactor
		}
		if matchedRule.CacheSharding.DistributionStrategy != "" {
			resolved.Sharding.DistributionStrategy = matchedRule.CacheSharding.DistributionStrategy
		}
	}
}

// resolveStatusConfig resolves status action configuration
func (r *ConfigResolver) resolveStatusConfig(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Initialize with empty headers map
	resolved.Status.Headers = make(map[string]string)

	// Infer status code from action if not explicitly provided
	statusCode := 0
	switch resolved.Action {
	case types.ActionBlock, types.ActionStatus403:
		statusCode = 403
	case types.ActionStatus404:
		statusCode = 404
	case types.ActionStatus410:
		statusCode = 410
	case types.ActionStatus:
		// For generic status action, code must be provided in Status config
		if matchedRule != nil && matchedRule.Status != nil && matchedRule.Status.Code != nil {
			statusCode = *matchedRule.Status.Code
		}
	}

	resolved.Status.Code = statusCode

	// Apply status configuration from matched rule
	if matchedRule != nil && matchedRule.Status != nil {
		// Reason
		if matchedRule.Status.Reason != "" {
			resolved.Status.Reason = matchedRule.Status.Reason
		}

		// Custom headers (deep copy to avoid mutation)
		if len(matchedRule.Status.Headers) > 0 {
			for key, value := range matchedRule.Status.Headers {
				resolved.Status.Headers[key] = value
			}
		}

		// If code explicitly provided in Status config, use it (override inferred)
		if matchedRule.Status.Code != nil {
			resolved.Status.Code = *matchedRule.Status.Code
		}
	}
}

// resolveTrackingParams resolves tracking parameter stripping configuration
// Deep merge order: Global → Host → Pattern with special list merging logic
func (r *ConfigResolver) resolveTrackingParams(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with defaults
	stripEnabled := true // Default: stripping enabled
	paramList := make([]string, len(defaultTrackingParams))
	copy(paramList, defaultTrackingParams)

	// Layer 1: Global configuration
	if r.globalTrackingParams != nil {
		if r.globalTrackingParams.Strip != nil {
			stripEnabled = *r.globalTrackingParams.Strip
		}

		// params replaces defaults entirely (even if empty), params_add extends them
		if r.globalTrackingParams.Params != nil {
			paramList = make([]string, len(r.globalTrackingParams.Params))
			copy(paramList, r.globalTrackingParams.Params)
		} else if len(r.globalTrackingParams.ParamsAdd) > 0 {
			paramList = append(paramList, r.globalTrackingParams.ParamsAdd...)
		}
	}

	// Layer 2: Host configuration
	if r.host.TrackingParams != nil {
		if r.host.TrackingParams.Strip != nil {
			stripEnabled = *r.host.TrackingParams.Strip
		}

		// params replaces everything (even if empty), params_add extends parent
		if r.host.TrackingParams.Params != nil {
			paramList = make([]string, len(r.host.TrackingParams.Params))
			copy(paramList, r.host.TrackingParams.Params)
		} else if len(r.host.TrackingParams.ParamsAdd) > 0 {
			paramList = append(paramList, r.host.TrackingParams.ParamsAdd...)
		}
	}

	// Layer 3: Pattern configuration
	if matchedRule != nil && matchedRule.TrackingParams != nil {
		if matchedRule.TrackingParams.Strip != nil {
			stripEnabled = *matchedRule.TrackingParams.Strip
		}

		// params replaces everything (even if empty), params_add extends parent
		if matchedRule.TrackingParams.Params != nil {
			paramList = make([]string, len(matchedRule.TrackingParams.Params))
			copy(paramList, matchedRule.TrackingParams.Params)
		} else if len(matchedRule.TrackingParams.ParamsAdd) > 0 {
			paramList = append(paramList, matchedRule.TrackingParams.ParamsAdd...)
		}
	}

	// If stripping is disabled, set resolved config to disabled
	if !stripEnabled {
		resolved.TrackingParams = &ResolvedTrackingParams{
			Enabled:          false,
			CompiledPatterns: nil,
		}
		return
	}

	// If pattern list is empty, disable stripping (prevents wasteful processing)
	// This can happen when params: [] is used to explicitly clear all parameters
	if len(paramList) == 0 {
		resolved.TrackingParams = &ResolvedTrackingParams{
			Enabled:          false,
			CompiledPatterns: nil,
		}
		return
	}

	// Compile patterns (errors already caught in validation)
	compiled, err := CompileStripPatterns(paramList)
	if err != nil {
		// This shouldn't happen if validation worked correctly
		// Fall back to disabled state
		resolved.TrackingParams = &ResolvedTrackingParams{
			Enabled:          false,
			CompiledPatterns: nil,
		}
		return
	}

	// Set resolved tracking params
	resolved.TrackingParams = &ResolvedTrackingParams{
		Enabled:          true,
		CompiledPatterns: compiled,
	}
}

// resolveBothitRecache resolves bot hit automatic recache configuration
// Deep merge order: Global → Host → Pattern
// match_ua array uses REPLACEMENT semantics (child replaces parent completely)
func (r *ConfigResolver) resolveBothitRecache(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Start with defaults (disabled)
	enabled := false
	interval := 24 * time.Hour // Default: 24h
	userAgents := []string{}
	var compiledPatterns []*pattern.Pattern

	// Layer 1: Global configuration
	if r.globalBothitRecache != nil {
		if r.globalBothitRecache.Enabled != nil {
			enabled = *r.globalBothitRecache.Enabled
		}
		if r.globalBothitRecache.Interval != nil {
			interval = time.Duration(*r.globalBothitRecache.Interval)
		}
		userAgents = r.globalBothitRecache.MatchUA
		compiledPatterns = r.globalBothitRecache.CompiledPatterns
	}

	// Layer 2: Host configuration
	if r.host.BothitRecache != nil {
		// Host-level enabled override
		if r.host.BothitRecache.Enabled != nil {
			enabled = *r.host.BothitRecache.Enabled
		}
		// Host-level interval override
		if r.host.BothitRecache.Interval != nil {
			interval = time.Duration(*r.host.BothitRecache.Interval)
		}
		// Host-level user_agents REPLACEMENT (not merge)
		if len(r.host.BothitRecache.MatchUA) > 0 {
			userAgents = r.host.BothitRecache.MatchUA
			compiledPatterns = r.host.BothitRecache.CompiledPatterns
		}
	}

	// Layer 3: Pattern configuration
	if matchedRule != nil && matchedRule.BothitRecache != nil {
		// Pattern-level enabled override
		if matchedRule.BothitRecache.Enabled != nil {
			enabled = *matchedRule.BothitRecache.Enabled
		}
		// Pattern-level interval override
		if matchedRule.BothitRecache.Interval != nil {
			interval = time.Duration(*matchedRule.BothitRecache.Interval)
		}
		// Pattern-level user_agents REPLACEMENT (not merge)
		if len(matchedRule.BothitRecache.MatchUA) > 0 {
			userAgents = matchedRule.BothitRecache.MatchUA
			compiledPatterns = matchedRule.BothitRecache.CompiledPatterns
		}
	}

	// Set resolved bothit_recache config
	resolved.BothitRecache = ResolvedBothitRecache{
		Enabled:          enabled,
		Interval:         interval,
		MatchUA:          userAgents,
		CompiledPatterns: compiledPatterns,
	}
}

// resolveHeaders resolves headers configuration with replacement and additive semantics.
// Each field (request/response) is resolved independently.
func (r *ConfigResolver) resolveHeaders(resolved *ResolvedConfig, matchedRule *types.URLRule) {
	// Default response headers
	responseHeaders := []string{"Content-Type", "Cache-Control", "Expires", "Last-Modified", "ETag", "Location"}
	// Default request headers (empty - opt-in)
	var requestHeaders []string

	// Layer 1: Global configuration
	if r.globalHeaders != nil {
		requestHeaders = applyHeadersDirective(requestHeaders, r.globalHeaders.SafeRequest, r.globalHeaders.SafeRequestAdd)
		responseHeaders = applyHeadersDirective(responseHeaders, r.globalHeaders.SafeResponse, r.globalHeaders.SafeResponseAdd)
	}

	// Layer 2: Host configuration
	if r.host.Headers != nil {
		requestHeaders = applyHeadersDirective(requestHeaders, r.host.Headers.SafeRequest, r.host.Headers.SafeRequestAdd)
		responseHeaders = applyHeadersDirective(responseHeaders, r.host.Headers.SafeResponse, r.host.Headers.SafeResponseAdd)
	}

	// Layer 3: URL pattern configuration
	if matchedRule != nil && matchedRule.Headers != nil {
		requestHeaders = applyHeadersDirective(requestHeaders, matchedRule.Headers.SafeRequest, matchedRule.Headers.SafeRequestAdd)
		responseHeaders = applyHeadersDirective(responseHeaders, matchedRule.Headers.SafeResponse, matchedRule.Headers.SafeResponseAdd)
	}

	resolved.SafeRequestHeaders = requestHeaders
	resolved.SafeResponseHeaders = responseHeaders
}

// applyHeadersDirective applies replace or add directive to a headers list.
// If replace is non-empty, it replaces current. If add is non-empty, it adds (with dedup).
func applyHeadersDirective(current, replace, add []string) []string {
	if len(replace) > 0 {
		return replace
	}
	if len(add) > 0 {
		return deduplicateHeaders(append(current, add...))
	}
	return current
}

// deduplicateHeaders removes duplicate headers (case-insensitive), preserving first occurrence.
func deduplicateHeaders(headers []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(headers))
	for _, h := range headers {
		lower := strings.ToLower(h)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, h)
		}
	}
	return result
}

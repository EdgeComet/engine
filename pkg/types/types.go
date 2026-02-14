package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/edgecomet/engine/pkg/pattern"
)

// HeadersConfig defines safe request and response headers configuration.
// Supports both replacement (safe_*) and additive (safe_*_add) directives.
// At each config level, only ONE of safe_request/safe_request_add can be used (same for response).
type HeadersConfig struct {
	// SafeRequest replaces parent's request headers list
	SafeRequest []string `yaml:"safe_request,omitempty" json:"safe_request,omitempty"`
	// SafeRequestAdd adds to parent's request headers list
	SafeRequestAdd []string `yaml:"safe_request_add,omitempty" json:"safe_request_add,omitempty"`
	// SafeResponse replaces parent's response headers list
	SafeResponse []string `yaml:"safe_response,omitempty" json:"safe_response,omitempty"`
	// SafeResponseAdd adds to parent's response headers list
	SafeResponseAdd []string `yaml:"safe_response_add,omitempty" json:"safe_response_add,omitempty"`
}

// ClientIPConfig defines HTTP headers for extracting the client's real IP address.
type ClientIPConfig struct {
	Headers []string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// Host represents a domain configuration
type Host struct {
	ID             int                          `yaml:"id" json:"id"`
	Domain         string                       `yaml:"-" json:"-"`
	Domains        []string                     `yaml:"-" json:"domain"`
	RenderKey      string                       `yaml:"render_key" json:"render_key"`
	Enabled        bool                         `yaml:"enabled" json:"enabled"`
	Render         RenderConfig                 `yaml:"render" json:"render"`
	Bypass         *BypassConfig                `yaml:"bypass,omitempty" json:"bypass,omitempty"`                   // Host-level bypass override (optional, pointer for override detection)
	TrackingParams *TrackingParamsConfig        `yaml:"tracking_params,omitempty" json:"tracking_params,omitempty"` // Host-level tracking params override
	CacheSharding  *CacheShardingBehaviorConfig `yaml:"cache_sharding,omitempty" json:"cache_sharding,omitempty"`   // Host-level cache sharding override (behavioral settings only)
	BothitRecache  *BothitRecacheConfig         `yaml:"bothit_recache,omitempty" json:"bothit_recache,omitempty"`   // Host-level bot hit recache override
	Headers        *HeadersConfig               `yaml:"headers,omitempty" json:"headers,omitempty"`                 // Host-level headers override
	ClientIP       *ClientIPConfig              `yaml:"client_ip,omitempty" json:"client_ip,omitempty"`             // Host-level client IP override
	URLRules       []URLRule                    `yaml:"url_rules,omitempty" json:"url_rules,omitempty"`             // URL pattern rules
}

// UnmarshalYAML implements custom YAML unmarshaling for Host.
// Handles both string and array formats for domain field and strips trailing dots (FQDN normalization).
func (h *Host) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type hostAlias Host
	type hostRaw struct {
		hostAlias `yaml:",inline"`
		Domain    interface{} `yaml:"domain"`
	}

	var raw hostRaw
	if err := unmarshal(&raw); err != nil {
		return err
	}

	// Copy all fields from the alias
	*h = Host(raw.hostAlias)

	// Handle domain field (string or array)
	switch v := raw.Domain.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			h.Domains = []string{strings.TrimSuffix(trimmed, ".")}
		}
	case []interface{}:
		var domains []string
		for _, d := range v {
			if s, ok := d.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					domains = append(domains, strings.TrimSuffix(trimmed, "."))
				}
			}
		}
		h.Domains = domains
	case nil:
		// Domain not specified, leave Domains empty
	default:
		return fmt.Errorf("domain must be a string or array of strings, got %T", raw.Domain)
	}

	// Set primary Domain from first element
	if len(h.Domains) > 0 {
		h.Domain = h.Domains[0]
	}

	return nil
}

// MarshalYAML implements yaml.Marshaler for Host.
// Outputs Domains as "domain" field (single string if one domain, array if multiple).
func (h Host) MarshalYAML() (interface{}, error) {
	type hostAlias Host

	// Create output struct to control domain field explicitly
	result := struct {
		hostAlias `yaml:",inline"`
		Domain    interface{} `yaml:"domain,omitempty"`
	}{
		hostAlias: hostAlias(h),
	}

	// Output domain as string if single, array if multiple
	switch len(h.Domains) {
	case 0:
		result.Domain = nil
	case 1:
		result.Domain = h.Domains[0]
	default:
		result.Domain = h.Domains
	}

	return result, nil
}

// UnmarshalJSON implements json.Unmarshaler for Host.
// Handles both string and array formats for domain field and strips trailing dots.
func (h *Host) UnmarshalJSON(data []byte) error {
	type hostAlias Host
	type hostRaw struct {
		hostAlias
		Domain json.RawMessage `json:"domain"`
	}

	var raw hostRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Copy all fields from the alias
	*h = Host(raw.hostAlias)

	// Handle domain field (string or array)
	if len(raw.Domain) == 0 || string(raw.Domain) == "null" {
		return nil
	}

	// Try string first
	var single string
	if err := json.Unmarshal(raw.Domain, &single); err == nil {
		trimmed := strings.TrimSpace(single)
		if trimmed != "" {
			h.Domains = []string{strings.TrimSuffix(trimmed, ".")}
		}
	} else {
		// Try array
		var arr []string
		if err := json.Unmarshal(raw.Domain, &arr); err != nil {
			return fmt.Errorf("domain must be a string or array of strings")
		}
		var domains []string
		for _, d := range arr {
			trimmed := strings.TrimSpace(d)
			if trimmed != "" {
				domains = append(domains, strings.TrimSuffix(trimmed, "."))
			}
		}
		h.Domains = domains
	}

	// Set primary Domain from first element
	if len(h.Domains) > 0 {
		h.Domain = h.Domains[0]
	}

	return nil
}

// RenderCacheConfig defines caching behavior for render action
type RenderCacheConfig struct {
	TTL         *Duration           `yaml:"ttl,omitempty" json:"ttl,omitempty"`                   // Optional TTL override (nil = use global, &0 = no cache)
	StatusCodes []int               `yaml:"status_codes,omitempty" json:"status_codes,omitempty"` // HTTP status codes to cache (default: [200, 301, 302, 307, 308, 404])
	Expired     *CacheExpiredConfig `yaml:"expired,omitempty" json:"expired,omitempty"`           // Optional expiration behavior override
}

// Unmatched dimension behavior constants
const (
	UnmatchedDimensionBlock  = "block"  // Return 403 Forbidden
	UnmatchedDimensionBypass = "bypass" // Fetch from origin (default)
)

// Compression algorithm constants
const (
	CompressionNone   = "none"   // No compression
	CompressionSnappy = "snappy" // Snappy compression (default)
	CompressionLZ4    = "lz4"    // LZ4 compression
)

// Compression file extension constants
const (
	ExtSnappy = ".snappy"
	ExtLZ4    = ".lz4"
)

// CompressionMinSize is the minimum content size in bytes for compression to be applied.
// Files smaller than this are stored uncompressed.
const CompressionMinSize = 1024

// RenderConfig defines rendering behavior
type RenderConfig struct {
	Timeout              Duration             `yaml:"timeout" json:"timeout"`
	UnmatchedDimension   string               `yaml:"unmatched_dimension" json:"unmatched_dimension"` // UnmatchedDimensionBlock | UnmatchedDimensionBypass | dimension name (default: UnmatchedDimensionBypass)
	Dimensions           map[string]Dimension `yaml:"dimensions" json:"dimensions"`
	Events               RenderEvents         `yaml:"events" json:"events"`
	Cache                *RenderCacheConfig   `yaml:"cache,omitempty" json:"cache,omitempty"`                                   // Cache configuration for render action
	BlockedResourceTypes []string             `yaml:"blocked_resource_types,omitempty" json:"blocked_resource_types,omitempty"` // Resource types to block during rendering
	BlockedPatterns      []string             `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty"`             // URL patterns to block (domains/paths)
	StripScripts         *bool                `yaml:"strip_scripts,omitempty" json:"strip_scripts,omitempty"`
}

// Dimension defines viewport configuration
type Dimension struct {
	ID       int      `yaml:"id" json:"id"`
	Width    int      `yaml:"width" json:"width"`
	Height   int      `yaml:"height" json:"height"`
	RenderUA string   `yaml:"render_ua" json:"render_ua"`
	MatchUA  []string `yaml:"match_ua" json:"match_ua"`

	// CompiledPatterns stores pre-compiled user agent patterns
	CompiledPatterns []*pattern.Pattern `yaml:"-" json:"-"`
}

// CompileMatchUAPatterns pre-compiles patterns for user agent matching
// Uses unified pattern package for consistent behavior:
// - No prefix: exact match (case-sensitive)
// - * wildcard: matches any characters
// - ~ prefix: case-sensitive regexp
// - ~* prefix: case-insensitive regexp
func (d *Dimension) CompileMatchUAPatterns() error {
	if len(d.MatchUA) == 0 {
		return nil
	}

	d.CompiledPatterns = make([]*pattern.Pattern, len(d.MatchUA))

	for i, pat := range d.MatchUA {
		compiled, err := pattern.Compile(pat)
		if err != nil {
			return fmt.Errorf("invalid user agent pattern '%s': %w", pat, err)
		}
		d.CompiledPatterns[i] = compiled
	}

	return nil
}

// RenderEvents defines page ready detection
type RenderEvents struct {
	WaitFor        string    `yaml:"wait_for" json:"wait_for"`
	AdditionalWait *Duration `yaml:"additional_wait,omitempty" json:"additional_wait,omitempty"`
}

// Lifecycle event constants for wait_for field
const (
	LifecycleEventDOMContentLoaded  = "DOMContentLoaded"  // DOM is ready, images may still be loading
	LifecycleEventLoad              = "load"              // Page fully loaded (all resources)
	LifecycleEventNetworkIdle       = "networkIdle"       // Network has been idle for 500ms
	LifecycleEventNetworkAlmostIdle = "networkAlmostIdle" // At most 2 network connections for 500ms
)

// CacheKey represents a unique cache identifier
type CacheKey struct {
	HostID      int    `json:"h"`
	DimensionID int    `json:"d"`
	URLHash     string `json:"u"`
}

// String returns cache key in Redis format
func (ck CacheKey) String() string {
	return fmt.Sprintf("cache:%d:%d:%s", ck.HostID, ck.DimensionID, ck.URLHash)
}

// EGInfo represents information about an Edge Gateway instance in the registry
type EGInfo struct {
	EgID            string    `json:"eg_id"`
	Address         string    `json:"address"`
	LastHeartbeat   time.Time `json:"last_heartbeat"`
	ShardingEnabled bool      `json:"sharding_enabled"`
}

// ParseCacheKey parses a cache key string in format "cache:host_id:dimension_id:url_hash"
func ParseCacheKey(s string) (*CacheKey, error) {
	var hostID, dimensionID int
	var urlHash string

	// Parse using fmt.Sscanf for cache:host_id:dimension_id:url_hash format
	n, err := fmt.Sscanf(s, "cache:%d:%d:%s", &hostID, &dimensionID, &urlHash)
	if err != nil || n != 3 {
		return nil, fmt.Errorf("invalid cache key format: %s", s)
	}

	return &CacheKey{
		HostID:      hostID,
		DimensionID: dimensionID,
		URLHash:     urlHash,
	}, nil
}

// RenderServer represents a render service instance
type RenderServer struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Status   string    `json:"status"`
	Weight   int       `json:"weight"`
	MaxQueue int       `json:"max_queue"`
	LastSeen time.Time `json:"last_seen"`
}

// RenderRequest represents a page render request (EG→RS communication)
type RenderRequest struct {
	// Request identification
	RequestID string `json:"request_id"`
	URL       string `json:"url"`
	TabID     int    `json:"tab_id"` // Reserved Chrome tab ID from Lua script

	// Rendering configuration
	ViewportWidth  int    `json:"viewport_width"`
	ViewportHeight int    `json:"viewport_height"`
	UserAgent      string `json:"user_agent"`

	// Timing configuration
	Timeout   time.Duration `json:"timeout"`    // render timeout duration
	WaitFor   string        `json:"wait_for"`   // lifecycle event: "DOMContentLoaded", "load", "networkIdle", "networkAlmostIdle"
	ExtraWait time.Duration `json:"extra_wait"` // additional wait duration after event

	// Request blocking configuration
	BlockedPatterns      []string `json:"blocked_patterns,omitempty"`       // URL patterns to block (domains/paths)
	BlockedResourceTypes []string `json:"blocked_resource_types,omitempty"` // Resource types to block (Image, Media, Font, etc.)

	// HAR generation
	IncludeHAR bool `json:"include_har,omitempty"` // Generate HAR data for debugging

	// Client request headers forwarding (same-origin only)
	Headers map[string][]string `json:"headers,omitempty"` // Client request headers to forward to origin

	// HTML processing
	StripScripts bool `json:"strip_scripts"` // Remove executable scripts from rendered HTML
}

// Error type constants - Infrastructure errors
const (
	ErrorTypeHardTimeout         = "hard_timeout"
	ErrorTypeChromeCrash         = "chrome_crash"
	ErrorTypeChromeRestartFailed = "chrome_restart_failed"
	ErrorTypePoolUnavailable     = "pool_unavailable"
)

// Error type constants - Render errors
const (
	ErrorTypeSoftTimeout          = "soft_timeout"
	ErrorTypeNavigationFailed     = "navigation_failed"
	ErrorTypeNetworkError         = "network_error"
	ErrorTypeHTMLExtractionFailed = "html_extraction_failed"
	ErrorTypeStatusCaptureFailed  = "status_capture_failed"
	ErrorTypeInvalidURL           = "invalid_url"
)

// Error type constants - Origin errors
const (
	ErrorTypeOrigin4xx = "origin_4xx"
	ErrorTypeOrigin5xx = "origin_5xx"
)

// Error type constants - Content errors
const (
	ErrorTypeEmptyResponse    = "empty_response"
	ErrorTypeResponseTooLarge = "response_too_large"
)

// RenderResponse represents a render result (unified for RS→EG and Chrome→RS)
type RenderResponse struct {
	RequestID  string              `json:"request_id"`
	Success    bool                `json:"success"`
	HTML       string              `json:"html,omitempty"`
	Error      string              `json:"error,omitempty"`
	ErrorType  string              `json:"error_type,omitempty"` // Structured error category (e.g., "soft_timeout", "navigation_failed")
	RenderTime time.Duration       `json:"render_time"`          // render duration
	HTMLSize   int                 `json:"html_size"`            // bytes
	Timestamp  time.Time           `json:"timestamp"`
	ChromeID   string              `json:"chrome_id"`
	Metrics    PageMetrics         `json:"metrics,omitempty"` // Page rendering metrics
	Headers    map[string][]string `json:"headers,omitempty"` // HTTP response headers from rendered page
	HAR        json.RawMessage     `json:"har,omitempty"`     // HAR data for debugging (JSON bytes)
	PageSEO    *PageSEO            `json:"page_seo,omitempty"`
}

// RenderResponseMetadata contains render metadata without HTML content
// Used for efficient binary protocol (metadata + raw HTML)
type RenderResponseMetadata struct {
	RequestID  string              `json:"request_id"`
	Success    bool                `json:"success"`
	Error      string              `json:"error,omitempty"`
	ErrorType  string              `json:"error_type,omitempty"` // Structured error category
	RenderTime time.Duration       `json:"render_time"`          // render duration
	HTMLSize   int                 `json:"html_size"`            // bytes
	Timestamp  time.Time           `json:"timestamp"`
	ChromeID   string              `json:"chrome_id"`
	Metrics    PageMetrics         `json:"metrics,omitempty"` // Page rendering metrics
	Headers    map[string][]string `json:"headers,omitempty"` // HTTP response headers from rendered page
	HAR        json.RawMessage     `json:"har,omitempty"`     // HAR data for debugging (JSON bytes)
	PageSEO    *PageSEO            `json:"page_seo,omitempty"`
}

// LifecycleEvent represents a single page lifecycle event
type LifecycleEvent struct {
	Name string  `json:"name"` // event name (e.g., "DOMContentLoaded", "load", "networkIdle")
	Time float64 `json:"time"` // seconds from navigation start
}

// ConsoleError represents a console error or warning captured during render
type ConsoleError struct {
	Type           string `json:"type"`            // "error" or "warning"
	SourceURL      string `json:"source_url"`      // Script URL or "<anonymous>"
	SourceLocation string `json:"source_location"` // "line:column" format or "0:0"
	Message        string `json:"message"`         // Error message text
}

// PageMetrics contains metrics collected during page rendering
type PageMetrics struct {
	StatusCode      int              `json:"status_code"`
	FinalURL        string           `json:"final_url"`
	LifecycleEvents []LifecycleEvent `json:"lifecycle_events,omitempty"`
	TimedOut        bool             `json:"timed_out"`
	ConsoleMessages []ConsoleError   `json:"console_messages,omitempty"`

	// Network metrics
	TotalRequests      int              `json:"total_requests"`
	TotalBytes         int64            `json:"total_bytes"`
	BytesByType        map[string]int64 `json:"bytes_by_type,omitempty"`
	RequestsByType     map[string]int64 `json:"requests_by_type,omitempty"`
	StatusCounts       map[string]int64 `json:"status_counts,omitempty"`
	SameOriginRequests int              `json:"same_origin_requests"`
	SameOriginBytes    int64            `json:"same_origin_bytes"`
	ThirdPartyRequests int              `json:"third_party_requests"`
	ThirdPartyBytes    int64            `json:"third_party_bytes"`
	ThirdPartyDomains  int              `json:"third_party_domains"`
	BlockedCount       int              `json:"blocked_count"`
	FailedCount        int              `json:"failed_count"`
	TimeToFirstRequest float64          `json:"time_to_first_request"`
	TimeToLastResponse float64          `json:"time_to_last_response"`

	// Per-domain statistics (max 100 domains, sorted by request count)
	// Key is hostname without port (e.g., "example.com", "api.reviews.io")
	DomainStats map[string]*DomainStats `json:"domain_stats,omitempty"`

	// Render configuration used (for analytics)
	WaitForEvent   string  `json:"wait_for_event,omitempty"`  // target lifecycle event
	ExtraWait      float64 `json:"extra_wait,omitempty"`      // configured extra wait (seconds)
	Timeout        float64 `json:"timeout,omitempty"`         // configured timeout (seconds)
	ViewportWidth  int     `json:"viewport_width,omitempty"`  // viewport width
	ViewportHeight int     `json:"viewport_height,omitempty"` // viewport height
}

// DomainStats contains per-domain network statistics.
type DomainStats struct {
	Requests   int     `json:"requests"`
	Bytes      int64   `json:"bytes"`
	Failed     int     `json:"failed"`
	Blocked    int     `json:"blocked"`
	AvgLatency float64 `json:"avg_latency"` // seconds, excludes failed requests
}

// CacheExpiredConfig defines behavior when cache entries expire
type CacheExpiredConfig struct {
	Strategy string    `yaml:"strategy" json:"strategy"`                       // "serve_stale" | "delete"
	StaleTTL *Duration `yaml:"stale_ttl,omitempty" json:"stale_ttl,omitempty"` // Time to live for stale cache after expiration
}

// Expiration strategy constants
const (
	ExpirationStrategyServeStale = "serve_stale" // Keep serving expired cache while recaching
	ExpirationStrategyDelete     = "delete"      // Delete expired cache and force fresh render
)

// Cache TTL constants
const (
	NoCacheTTL = 0 // Disables caching - content always fetched fresh
)

// Registry selection strategy constants
const (
	SelectionStrategyLeastLoaded   = "least_loaded"   // Select service with lowest load percentage
	SelectionStrategyMostAvailable = "most_available" // Select service with most available tabs
)

// IndexStatus represents the indexability status of a rendered page
type IndexStatus int

// IndexStatus constants for page indexability
const (
	IndexStatusIndexable     IndexStatus = 1 // Page can be indexed
	IndexStatusNon200        IndexStatus = 2 // Non-200 status code
	IndexStatusBlockedByMeta IndexStatus = 3 // Blocked by robots/googlebot meta tag
	IndexStatusNonCanonical  IndexStatus = 4 // Canonical URL points elsewhere
)

// HTTP status class constants for PageMetrics.StatusCounts map keys
const (
	StatusClass2xx = "2xx"
	StatusClass3xx = "3xx"
	StatusClass4xx = "4xx"
	StatusClass5xx = "5xx"
)

// Console error type constants
const (
	ConsoleTypeError      = "error"
	ConsoleTypeWarning    = "warning"
	AnonymousSourceURL    = "<anonymous>"
	UnknownSourceLocation = "0:0"
)

// PageSEO extraction limits - text fields
const (
	MaxSEOTitleLength        = 500
	MaxMetaDescriptionLength = 1000
	MaxCanonicalURLLength    = 2000
	MaxHreflangURLLength     = 2000
	MaxHeadingLength         = 500
	MaxHeadingsPerLevel      = 5
	MaxExternalDomains       = 20
)

// PageSEO extraction limits - performance
const (
	MaxJSONLDSize           = 1024 * 1024 // 1MB per JSON-LD block
	MaxJSONLDRecursionDepth = 10          // Prevent stack overflow
)

// Chrome resource type constants for PageMetrics.BytesByType map keys
const (
	ResourceTypeDocument       = "Document"
	ResourceTypeStylesheet     = "Stylesheet"
	ResourceTypeImage          = "Image"
	ResourceTypeMedia          = "Media"
	ResourceTypeFont           = "Font"
	ResourceTypeScript         = "Script"
	ResourceTypeTextTrack      = "TextTrack"
	ResourceTypeXHR            = "XHR"
	ResourceTypeFetch          = "Fetch"
	ResourceTypePrefetch       = "Prefetch"
	ResourceTypeEventSource    = "EventSource"
	ResourceTypeWebSocket      = "WebSocket"
	ResourceTypeManifest       = "Manifest"
	ResourceTypeSignedExchange = "SignedExchange"
	ResourceTypePing           = "Ping"
	ResourceTypeCSPViolation   = "CSPViolationReport"
	ResourceTypePreflight      = "Preflight"
	ResourceTypeOther          = "Other"
)

// HreflangEntry represents a single hreflang alternate link
type HreflangEntry struct {
	Lang string `json:"lang"` // Language/region code (e.g., "en-US", "x-default")
	URL  string `json:"url"`  // Alternate URL (resolved to absolute)
}

// PageSEO contains SEO-relevant metadata extracted from rendered HTML
type PageSEO struct {
	// Basic metadata
	Title       string      `json:"title,omitempty"`
	IndexStatus IndexStatus `json:"index_status,omitempty"`

	// Meta tags
	MetaDescription string `json:"meta_description,omitempty"`
	CanonicalURL    string `json:"canonical_url,omitempty"`
	MetaRobots      string `json:"meta_robots,omitempty"`

	// Headings (first 5 of each level, max 500 chars each)
	H1s []string `json:"h1s,omitempty"`
	H2s []string `json:"h2s,omitempty"`
	H3s []string `json:"h3s,omitempty"`

	// Links analysis
	LinksTotal      int            `json:"links_total,omitempty"`
	LinksInternal   int            `json:"links_internal,omitempty"`
	LinksExternal   int            `json:"links_external,omitempty"`
	ExternalDomains map[string]int `json:"external_domains,omitempty"`

	// Images analysis
	ImagesTotal    int `json:"images_total,omitempty"`
	ImagesInternal int `json:"images_internal,omitempty"`
	ImagesExternal int `json:"images_external,omitempty"`

	// International SEO
	Hreflang []HreflangEntry `json:"hreflang,omitempty"`

	// Structured data
	StructuredDataTypes []string `json:"structured_data_types,omitempty"`
}

// Duration wraps time.Duration with extended YAML parsing support for days and weeks
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for extended duration formats
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	// Try standard parsing first (handles: ns, us, ms, s, m, h)
	dur, err := time.ParseDuration(s)
	if err == nil {
		*d = Duration(dur)
		return nil
	}

	// Parse extended formats: d (days), w (weeks)
	dur, err = parseExtendedDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalYAML implements yaml.Marshaler
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalJSON implements json.Unmarshaler for Duration.
// Accepts both numbers (nanoseconds, backward-compatible) and strings ("15s", "24h", "30d", "2w").
func (d *Duration) UnmarshalJSON(data []byte) error {
	var ns int64
	if err := json.Unmarshal(data, &ns); err == nil {
		*d = Duration(ns)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string or number, got %s", string(data))
	}

	dur, err := time.ParseDuration(s)
	if err == nil {
		*d = Duration(dur)
		return nil
	}

	dur, err = parseExtendedDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalJSON implements json.Marshaler for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ToDuration converts types.Duration to time.Duration
func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}

// String implements fmt.Stringer for Duration
func (d Duration) String() string {
	return time.Duration(d).String()
}

// parseExtendedDuration parses duration strings with extended suffixes: d (days), w (weeks)
// Examples: "30d", "2w", "1.5d"
func parseExtendedDuration(s string) (time.Duration, error) {
	// Regex: optional sign, number (int or float), suffix (d or w)
	re := regexp.MustCompile(`^(-?)(\d+(?:\.\d+)?)(d|w)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid format, expected format like '30d' or '2w'")
	}

	sign := matches[1]
	valueStr := matches[2]
	suffix := matches[3]

	// Parse the numeric value
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %w", err)
	}

	// Apply sign
	if sign == "-" {
		value = -value
	}

	// Convert to time.Duration based on suffix
	var duration time.Duration
	switch suffix {
	case "d":
		duration = time.Duration(value * float64(24*time.Hour))
	case "w":
		duration = time.Duration(value * float64(7*24*time.Hour))
	default:
		return 0, fmt.Errorf("unsupported suffix %q", suffix)
	}

	return duration, nil
}

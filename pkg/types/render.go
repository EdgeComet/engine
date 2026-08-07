package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgecomet/engine/pkg/pattern"
)

// RenderCacheConfig defines caching behavior for render action
type RenderCacheConfig struct {
	TTL         *Duration           `yaml:"ttl,omitempty" json:"ttl,omitempty"`                   // Optional TTL override (nil = use global, &0 = no cache)
	StatusCodes []int               `yaml:"status_codes,omitempty" json:"status_codes,omitempty"` // HTTP status codes to cache (default: [200, 301, 302, 307, 308, 404])
	Expired     *CacheExpiredConfig `yaml:"expired,omitempty" json:"expired,omitempty"`           // Optional expiration behavior override
}

// RenderConfig defines rendering behavior
type RenderConfig struct {
	Timeout              Duration           `yaml:"timeout" json:"timeout"`
	Events               RenderEvents       `yaml:"events" json:"events"`
	Cache                *RenderCacheConfig `yaml:"cache,omitempty" json:"cache,omitempty"`                                   // Cache configuration for render action
	BlockedResourceTypes []string           `yaml:"blocked_resource_types,omitempty" json:"blocked_resource_types,omitempty"` // Resource types to block during rendering
	BlockedPatterns      []string           `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty"`             // URL patterns to block (domains/paths)
	StripScripts         *bool              `yaml:"strip_scripts,omitempty" json:"strip_scripts,omitempty"`
}

// Dimension defines viewport configuration
type Dimension struct {
	ID       int           `yaml:"id" json:"id"`
	Width    int           `yaml:"width" json:"width"`
	Height   int           `yaml:"height" json:"height"`
	RenderUA string        `yaml:"render_ua" json:"render_ua"`
	MatchUA  []string      `yaml:"match_ua" json:"match_ua"`
	Action   URLRuleAction `yaml:"action,omitempty" json:"action,omitempty"`

	// CompiledPatterns stores pre-compiled user agent patterns
	CompiledPatterns []*pattern.Pattern `yaml:"-" json:"-"`
}

// EffectiveAction returns the dimension's action, defaulting to ActionRender
func (d Dimension) EffectiveAction() URLRuleAction {
	if d.Action == "" {
		return ActionRender
	}
	return d.Action
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

	// RenderKey is the host's render key, sent as X-Render-Key on same-origin requests so the
	// origin can verify the request originated from EdgeComet. Mirrors the bypass path.
	RenderKey string `json:"render_key,omitempty"`
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

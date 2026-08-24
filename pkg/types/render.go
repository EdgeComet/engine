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
	Scroll               *RenderScroll      `yaml:"scroll,omitempty" json:"scroll,omitempty"`
}

// RenderScroll controls whether the renderer scrolls the page to the bottom before capturing
// HTML, so content gated on scroll events is present in the capture.
type RenderScroll struct {
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// ScrollMaxDuration is the wall clock a scroll pass may add to a render on top of the request
// timeout. It lives with the wire types because it is part of the contract of the scroll field:
// the render service enforces it, and any caller sizing a deadline around a scroll-enabled
// render request has to budget for it.
const ScrollMaxDuration = 12 * time.Second

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

// Readiness property constants for wait_for. These are not lifecycle events: each names a window
// property the page itself sets once its content is in the DOM, which is the only signal a
// framework with lazily resolved content gives. Selecting one also marks the page as being
// captured - the renderer seeds window.isPrerender before any page script runs.
const (
	WaitForPrerenderReady        = "prerenderReady"        // page reports itself ready
	WaitForPrerenderContentReady = "prerenderContentReady" // page reports its content resolved, preferred where a page sets both
)

// ValidWaitForValues returns every accepted wait_for value. Configuration validation and its error
// message both read from it so neither can drift from the constants above.
func ValidWaitForValues() []string {
	return []string{
		LifecycleEventDOMContentLoaded,
		LifecycleEventLoad,
		LifecycleEventNetworkIdle,
		LifecycleEventNetworkAlmostIdle,
		WaitForPrerenderReady,
		WaitForPrerenderContentReady,
	}
}

// IsPrerenderWait reports whether wait_for selects the prerender readiness wait instead of a page
// lifecycle event. It is the single place that decision is made: the renderer picks the wait
// mechanism with it, and the render service warns on the timeout budget with it.
func IsPrerenderWait(waitFor string) bool {
	return waitFor == WaitForPrerenderReady || waitFor == WaitForPrerenderContentReady
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
	WaitFor   string        `json:"wait_for"`   // lifecycle event or readiness property, see ValidWaitForValues
	ExtraWait time.Duration `json:"extra_wait"` // additional wait duration after event

	// Request blocking configuration
	BlockedPatterns      []string `json:"blocked_patterns,omitempty"`       // URL patterns to block (domains/paths)
	BlockedResourceTypes []string `json:"blocked_resource_types,omitempty"` // Resource types to block (Image, Media, Font, etc.)

	// Scroll requests a scroll pass to the bottom of the page before HTML capture
	Scroll bool `json:"scroll,omitempty"`

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
	// ErrorTypeRenderUnavailable is EG-side: no render service could be reached or reserved.
	// Distinct from ErrorTypePoolUnavailable, which is the render service's own pool exhaustion.
	ErrorTypeRenderUnavailable = "render_unavailable"
	ErrorTypeCacheWriteFailed  = "cache_write_failed"
)

// Error type constants - Render errors
const (
	ErrorTypeSoftTimeout          = "soft_timeout"
	ErrorTypeNavigationFailed     = "navigation_failed"
	ErrorTypeNetworkError         = "network_error"
	ErrorTypeHTMLExtractionFailed = "html_extraction_failed"
	ErrorTypeStatusCaptureFailed  = "status_capture_failed"
	ErrorTypeInvalidURL           = "invalid_url"
	ErrorTypeInvalidRequest       = "invalid_request"
)

// Error type constants - Origin errors
const (
	ErrorTypeOrigin4xx = "origin_4xx"
	ErrorTypeOrigin5xx = "origin_5xx"
	// ErrorTypeOriginRedirect and ErrorTypeOriginUncacheable cover origin statuses the
	// resolved cache config rejects but that are neither 4xx nor 5xx.
	ErrorTypeOriginRedirect    = "origin_redirect"
	ErrorTypeOriginUncacheable = "origin_uncacheable"
)

// Error type constants - Content errors
const (
	ErrorTypeEmptyResponse    = "empty_response"
	ErrorTypeResponseTooLarge = "response_too_large"
)

// Error type constants - Fallback
const (
	// ErrorTypeUnknown labels a failure whose reporter supplied no error type. An empty
	// error_type means success on emitted events, so a failure must never carry one, and it
	// must not borrow a specific diagnosis nobody made.
	ErrorTypeUnknown = "unknown"
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
	WaitForEvent   string  `json:"wait_for_event,omitempty"`  // configured wait_for value: lifecycle event or readiness property
	ExtraWait      float64 `json:"extra_wait,omitempty"`      // configured extra wait (seconds)
	Timeout        float64 `json:"timeout,omitempty"`         // configured timeout (seconds)
	ViewportWidth  int     `json:"viewport_width,omitempty"`  // viewport width
	ViewportHeight int     `json:"viewport_height,omitempty"` // viewport height
	ScrollEnabled  bool    `json:"scroll_enabled,omitempty"`  // scroll pass requested for this render

	// Scroll pass outcome
	ScrollPerformed     bool    `json:"scroll_performed,omitempty"`      // at least one scroll step ran
	ScrollNoTarget      bool    `json:"scroll_no_target,omitempty"`      // nothing on the page was scrollable, as opposed to the pass failing or being cut short
	ScrollReachedBottom bool    `json:"scroll_reached_bottom,omitempty"` // the page scroller ended at its bottom; false means the budget ran out partway down
	ScrollTarget        string  `json:"scroll_target,omitempty"`         // page scroller driven (scrollingElement or body); empty when the document does not scroll
	ScrollInnerTarget   string  `json:"scroll_inner_target,omitempty"`   // inner container driven after the page settled (TAG.class); empty when none was needed
	ScrollSteps         int     `json:"scroll_steps,omitempty"`          // steps executed before settling or hitting a bound
	ScrollInnerSteps    int     `json:"scroll_inner_steps,omitempty"`    // of those, steps spent on an inner container
	ScrollStopReason    string  `json:"scroll_stop_reason,omitempty"`    // settled, duration, max_steps, no_target, cancelled or error
	ScrollDuration      float64 `json:"scroll_duration,omitempty"`       // wall time spent scrolling (seconds)
	ScrollFinalHeight   int     `json:"scroll_final_height,omitempty"`   // scrollHeight of the page scroller at the last step

	// Readiness wait outcome
	PrerenderRedirectURL string `json:"prerender_redirect_url,omitempty"` // URL the page parked on instead of rendering its own content; a non-empty value means the captured HTML is the page's loading shell, not the destination
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

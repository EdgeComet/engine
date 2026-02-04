package events

import (
	"time"

	"github.com/edgecomet/engine/pkg/types"
)

// RequestEvent contains all data for a single request event
type RequestEvent struct {
	// Identifiers
	RequestID string `json:"request_id"`
	Host      string `json:"host"`
	HostID    int    `json:"host_id"`
	URL       string `json:"url"`
	URLHash   string `json:"url_hash"`

	// Request metadata
	EventType   string `json:"event_type"` // cache_hit, render, bypass, bypass_cache, precache, error
	Dimension   string `json:"dimension"`
	UserAgent   string `json:"user_agent"`
	ClientIP    string `json:"client_ip"`
	MatchedRule string `json:"matched_rule"`

	// Response
	StatusCode int     `json:"status_code"`
	PageSize   int64   `json:"page_size"`
	ServeTime  float64 `json:"serve_time"` // seconds
	Source     string  `json:"source"`     // cache, render, bypass, bypass_cache

	// Render-specific
	RenderServiceID string  `json:"render_service_id"`
	RenderTime      float64 `json:"render_time"` // seconds
	ChromeID        string  `json:"chrome_id"`

	// Cache metadata
	CacheAge int    `json:"cache_age"` // seconds
	CacheKey string `json:"cache_key"`

	// Error info
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`

	// Page metrics (nil for non-render events)
	Metrics *PageMetricsEvent `json:"metrics,omitempty"`

	// SEO metadata (nil for cache hits, bypass)
	PageSEO *PageSEOEvent `json:"page_seo,omitempty"`

	// Timestamps
	CreatedAt    time.Time `json:"created_at"`
	EGInstanceID string    `json:"eg_instance_id"`
}

// PageMetricsEvent contains render performance metrics
type PageMetricsEvent struct {
	FinalURL           string               `json:"final_url"`
	TotalRequests      int                  `json:"total_requests"`
	TotalBytes         int64                `json:"total_bytes"`
	SameOriginRequests int                  `json:"same_origin_requests"`
	SameOriginBytes    int64                `json:"same_origin_bytes"`
	ThirdPartyRequests int                  `json:"third_party_requests"`
	ThirdPartyBytes    int64                `json:"third_party_bytes"`
	ThirdPartyDomains  int                  `json:"third_party_domains"`
	BlockedCount       int                  `json:"blocked_count"`
	FailedCount        int                  `json:"failed_count"`
	TimedOut           bool                 `json:"timed_out"`
	ConsoleMessages    []types.ConsoleError `json:"console_messages,omitempty"`
	ErrorCount         int                  `json:"error_count"`
	WarningCount       int                  `json:"warning_count"`
	TimeToFirstRequest float64              `json:"time_to_first_request"` // seconds
	TimeToLastResponse float64              `json:"time_to_last_response"` // seconds

	// Detailed metrics
	LifecycleEvents []types.LifecycleEvent       `json:"lifecycle_events,omitempty"`
	StatusCounts    map[string]int64             `json:"status_counts,omitempty"`
	BytesByType     map[string]int64             `json:"bytes_by_type,omitempty"`
	RequestsByType  map[string]int64             `json:"requests_by_type,omitempty"`
	DomainStats     map[string]*DomainStatsEvent `json:"domain_stats,omitempty"`
}

// DomainStatsEvent contains per-domain network statistics
type DomainStatsEvent struct {
	Requests   int     `json:"requests"`
	Bytes      int64   `json:"bytes"`
	Failed     int     `json:"failed"`
	Blocked    int     `json:"blocked"`
	AvgLatency float64 `json:"avg_latency"` // seconds
}

// HreflangEntryEvent represents a hreflang entry in events
type HreflangEntryEvent struct {
	Lang string `json:"lang"`
	URL  string `json:"url"`
}

// PageSEOEvent contains SEO metadata for event logging
type PageSEOEvent struct {
	Title               string               `json:"title,omitempty"`
	IndexStatus         int                  `json:"index_status,omitempty"`
	MetaDescription     string               `json:"meta_description,omitempty"`
	CanonicalURL        string               `json:"canonical_url,omitempty"`
	MetaRobots          string               `json:"meta_robots,omitempty"`
	H1s                 []string             `json:"h1s,omitempty"`
	H2s                 []string             `json:"h2s,omitempty"`
	H3s                 []string             `json:"h3s,omitempty"`
	LinksTotal          int                  `json:"links_total,omitempty"`
	LinksInternal       int                  `json:"links_internal,omitempty"`
	LinksExternal       int                  `json:"links_external,omitempty"`
	ExternalDomains     map[string]int       `json:"external_domains,omitempty"`
	ImagesTotal         int                  `json:"images_total,omitempty"`
	ImagesInternal      int                  `json:"images_internal,omitempty"`
	ImagesExternal      int                  `json:"images_external,omitempty"`
	Hreflang            []HreflangEntryEvent `json:"hreflang,omitempty"`
	StructuredDataTypes []string             `json:"structured_data_types,omitempty"`
}

package events

import (
	"encoding/json"
	"time"

	"github.com/edgecomet/engine/pkg/types"
)

// RequestEvent contains all data for a single request event
type RequestEvent struct {
	// Identifiers
	RequestID   string `json:"request_id"`
	Host        string `json:"host"`
	HostID      int    `json:"host_id"`
	URL         string `json:"url"`
	OriginalURL string `json:"original_url"`
	URLHash     uint64 `json:"url_hash"`

	// Request metadata
	EventType       string `json:"event_type"` // cache_hit, render, bypass, bypass_cache, precache, error
	Dimension       string `json:"dimension"`
	DimensionAction string `json:"dimension_action,omitempty"`
	UserAgent       string `json:"user_agent"`
	ClientIP        string `json:"client_ip"`
	MatchedRule     string `json:"matched_rule"`

	// HTTP headers (client request and final response); not written by the
	// file emitter, captured for downstream emitters
	RequestHeaders  map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`

	// Response
	StatusCode int     `json:"status_code"`
	PageSize   int64   `json:"page_size"`
	ServeTime  float64 `json:"serve_time"` // seconds
	Source     string  `json:"source"`     // cache, render, bypass, bypass_cache
	RedirectTo string  `json:"redirect_to,omitempty"`

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

	// SEO metadata. Populated wherever content processing runs: render, precache and
	// bypass. Cache-served events carry at most a minimal blob rebuilt from cache
	// metadata, which is not an inspection result.
	PageSEO *PageSEOEvent `json:"page_seo,omitempty"`

	// Content processor fields
	RuleIDs         []uint32        `json:"rule_ids,omitempty"`
	PageSEOOriginal *PageSEOEvent   `json:"page_seo_original,omitempty"`
	Extraction      json.RawMessage `json:"extraction,omitempty"`

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

// BreadcrumbEntryEvent represents one breadcrumb item in events
type BreadcrumbEntryEvent struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PageLinkEvent is one captured outbound link, carried folded inside the page_seo
// event struct.
type PageLinkEvent struct {
	Target     string   `json:"target"`
	Anchor     string   `json:"anchor,omitempty"`
	IsInternal bool     `json:"is_internal,omitempty"`
	Nofollow   bool     `json:"nofollow,omitempty"`
	Sponsored  bool     `json:"sponsored,omitempty"`
	UGC        bool     `json:"ugc,omitempty"`
	IsImage    bool     `json:"is_image,omitempty"`
	DomPath    []string `json:"dom_path,omitempty"`
}

// DateCandidateEvent is one raw date signal captured from the page or the origin
// response, carried folded inside the page_seo event struct.
type DateCandidateEvent struct {
	Source  string `json:"source"`
	Field   string `json:"field"`
	Raw     string `json:"raw"`
	Context string `json:"context"`
}

// PageSEOEvent contains SEO metadata for event logging
type PageSEOEvent struct {
	Title               string                 `json:"title,omitempty"`
	IndexStatus         int                    `json:"index_status,omitempty"`
	MetaDescription     string                 `json:"meta_description,omitempty"`
	CanonicalURL        string                 `json:"canonical_url,omitempty"`
	MetaRobots          []string               `json:"meta_robots,omitempty"`
	H1s                 []string               `json:"h1s,omitempty"`
	H2s                 []string               `json:"h2s,omitempty"`
	H3s                 []string               `json:"h3s,omitempty"`
	LinksTotal          int                    `json:"links_total,omitempty"`
	LinksInternal       int                    `json:"links_internal,omitempty"`
	LinksExternal       int                    `json:"links_external,omitempty"`
	ExternalDomains     map[string]int         `json:"external_domains,omitempty"`
	ImagesTotal         int                    `json:"images_total,omitempty"`
	ImagesInternal      int                    `json:"images_internal,omitempty"`
	ImagesExternal      int                    `json:"images_external,omitempty"`
	ImagesWithAlt       int                    `json:"images_with_alt,omitempty"`
	ImagesWithoutAlt    int                    `json:"images_without_alt,omitempty"`
	WordCount           int                    `json:"word_count,omitempty"`
	PageMinHash         []uint64               `json:"page_minhash,omitempty"`
	Hreflang            []HreflangEntryEvent   `json:"hreflang,omitempty"`
	HreflangSelf        string                 `json:"hreflang_self,omitempty"`
	StructuredDataTypes []string               `json:"structured_data_types,omitempty"`
	Breadcrumbs         []BreadcrumbEntryEvent `json:"breadcrumbs,omitempty"`
	// Dates carries every captured date signal. The tag is omitzero, not omitempty:
	// an inspected page with no signal keeps its empty array, which is itself the
	// finding, while a blob that never went through content processing - the minimal
	// one rebuilt from cache metadata when a request is served from cache - stays nil
	// and drops the key, so it cannot be read as an inspection result. omitempty would
	// collapse both onto an absent key, and a bare tag would marshal nil as null.
	Dates []DateCandidateEvent `json:"dates,omitzero"`
	// PageLinks carry the per-link outbound graph on the event.
	PageLinks []PageLinkEvent `json:"page_links,omitempty"`
	// PageLinksTruncated marks the outbound graph as incomplete, either because
	// capture hit its budget or because the graph was dropped in transit. It travels
	// to storage so a link-heavy page is never read back as a page without links.
	PageLinksTruncated bool `json:"page_links_truncated,omitempty"`
}

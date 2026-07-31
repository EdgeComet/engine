package types

// IndexStatus represents the indexability status of a rendered page
type IndexStatus int

// IndexStatus constants for page indexability
const (
	IndexStatusIndexable     IndexStatus = 1 // Page can be indexed
	IndexStatusNon200        IndexStatus = 2 // Non-200 status code
	IndexStatusBlockedByMeta IndexStatus = 3 // Blocked by robots/googlebot meta tag
	IndexStatusNonCanonical  IndexStatus = 4 // Canonical URL points elsewhere
)

// PageSEO extraction limits - text fields
const (
	MaxSEOTitleLength        = 500
	MaxMetaDescriptionLength = 1000
	MaxCanonicalURLLength    = 2000
	MaxHreflangURLLength     = 2000
	MaxHeadingLength         = 500
	MaxHeadingsPerLevel      = 30
	MaxExternalDomains       = 20
	MaxBreadcrumbs           = 5
)

// PageSEO extraction limits - performance
const (
	MaxJSONLDSize           = 1024 * 1024 // 1MB per JSON-LD block
	MaxJSONLDRecursionDepth = 10          // Prevent stack overflow
)

// Date capture limits. Real pages sit far below every cap; the caps exist to bound
// adversarial markup.
const (
	MaxDateRawLength = 64 // max runes kept per candidate Raw and Context
	// MaxDateCandidates bounds the candidates collected from markup. The origin
	// header candidate is appended after the cap and is not subject to it.
	MaxDateCandidates = 20
	// MaxTimeElementDates bounds <time> candidates, applied after the structural
	// skip rules so a page-level element behind item-level ones is still captured.
	MaxTimeElementDates = 3
	// DateRepetitionThreshold is the occurrence count at which a repeated JSON-LD
	// (context, property) signature collapses to its first occurrence.
	DateRepetitionThreshold = 3
	// FeedArticleThreshold is the top-level <article> count at which a page reads as
	// a feed and every <time> inside an <article> is treated as item-level.
	FeedArticleThreshold = 2
)

// DateCandidate.Source values, naming where a date signal was found.
const (
	DateSourceJSONLD      = "json_ld"
	DateSourceMeta        = "meta"
	DateSourceTimeElement = "time_element"
	DateSourceHTTPHeader  = "http_header"
)

// DateCandidate.Field values. Semantically equivalent properties collapse onto one
// value; the (Field, Context) pair disambiguates the original property downstream.
const (
	DateFieldPublished = "published"
	DateFieldModified  = "modified"
	DateFieldCreated   = "created"
	DateFieldReleased  = "released"
	DateFieldStart     = "start"
	DateFieldEnd       = "end"
	DateFieldExpires   = "expires"
	DateFieldUnknown   = "unknown"
)

// LastModifiedHeader is the canonical spelling of the only response header captured
// as a date signal, and the Context recorded for that candidate.
const LastModifiedHeader = "Last-Modified"

// DateCandidate is one raw date-bearing signal found on a page. Values are stored
// exactly as found, malformed ones included: interpretation happens downstream, so a
// malformed date is evidence rather than a capture failure. Every key is always
// serialized; consumers rely on Context being visible even when empty.
type DateCandidate struct {
	Source  string `json:"source"`
	Field   string `json:"field"`
	Raw     string `json:"raw"`
	Context string `json:"context"`
}

// Link capture limits. Hard caps that bound the per-page link payload; exceeding
// MaxPageLinks truncates and sets PageSEO.PageLinksTruncated.
const (
	MaxPageLinks    = 1000 // max distinct placement variants captured per page
	MaxAnchorLength = 300  // max runes of anchor text stored per link

	MaxPlacementsPerTarget = 5  // distinct DOM placements kept per link target
	MaxDomPathSteps        = 16 // significant steps kept in a placement path
	DomPathHeadSteps       = 6  // outermost steps kept when a path is depth-truncated
	DomPathTailSteps       = 9  // innermost steps kept when a path is depth-truncated
	MaxDomPathClasses      = 4  // class tokens kept per step after alphabetical sort
	MaxDomPathStepLength   = 64 // max runes of a single step string
)

// PageLink is one captured outbound link from a rendered or bypassed page. Target is
// a normalized absolute URL string; no hash is computed here.
type PageLink struct {
	Target     string   `json:"target"`
	Anchor     string   `json:"anchor,omitempty"`
	IsInternal bool     `json:"is_internal,omitempty"`
	Nofollow   bool     `json:"nofollow,omitempty"`
	Sponsored  bool     `json:"sponsored,omitempty"`
	UGC        bool     `json:"ugc,omitempty"`
	IsImage    bool     `json:"is_image,omitempty"`
	DomPath    []string `json:"dom_path,omitempty"`
}

// HreflangEntry represents a single hreflang alternate link
type HreflangEntry struct {
	Lang string `json:"lang"` // Language/region code (e.g., "en-US", "x-default")
	URL  string `json:"url"`  // Alternate URL (resolved to absolute)
}

// BreadcrumbEntry represents one item in a schema.org BreadcrumbList
type BreadcrumbEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PageSEO contains SEO-relevant metadata extracted from rendered HTML
type PageSEO struct {
	// Basic metadata
	Title       string      `json:"title,omitempty"`
	IndexStatus IndexStatus `json:"index_status,omitempty"`

	// Meta tags
	MetaDescription string   `json:"meta_description,omitempty"`
	CanonicalURL    string   `json:"canonical_url,omitempty"`
	MetaRobots      []string `json:"meta_robots,omitempty"`

	// Headings (first MaxHeadingsPerLevel of each level, each truncated to MaxHeadingLength runes)
	H1s []string `json:"h1s,omitempty"`
	H2s []string `json:"h2s,omitempty"`
	H3s []string `json:"h3s,omitempty"`

	// Links analysis
	LinksTotal            int            `json:"links_total,omitempty"`
	LinksInternal         int            `json:"links_internal,omitempty"`
	LinksExternal         int            `json:"links_external,omitempty"`
	LinksNofollow         int            `json:"links_nofollow,omitempty"`
	LinksNofollowInternal int            `json:"links_nofollow_internal,omitempty"`
	LinksNofollowExternal int            `json:"links_nofollow_external,omitempty"`
	ExternalDomains       map[string]int `json:"external_domains,omitempty"`

	// Images analysis
	ImagesTotal      int `json:"images_total,omitempty"`
	ImagesInternal   int `json:"images_internal,omitempty"`
	ImagesExternal   int `json:"images_external,omitempty"`
	ImagesWithAlt    int `json:"images_with_alt,omitempty"`
	ImagesWithoutAlt int `json:"images_without_alt,omitempty"`

	// Content analysis
	WordCount   int      `json:"word_count,omitempty"`
	PageMinHash []uint64 `json:"page_minhash,omitempty"`

	// International SEO
	Hreflang     []HreflangEntry `json:"hreflang,omitempty"`
	HreflangSelf string          `json:"hreflang_self,omitempty"`

	// Structured data
	StructuredDataTypes []string          `json:"structured_data_types,omitempty"`
	Breadcrumbs         []BreadcrumbEntry `json:"breadcrumbs,omitempty"`

	// Dates carries every captured date signal, grouped by source. The extractor
	// always initializes it, and the tag is omitzero rather than omitempty so that
	// the empty array survives: it records that the page was inspected and carries
	// no date signal, which consumers must be able to tell apart from an absent key.
	// A struct assembled without inspecting a page keeps the nil slice and drops the
	// key instead of claiming an empty result.
	Dates []DateCandidate `json:"dates,omitzero"`

	// Per-link outbound graph captured at render/bypass. Carried on the event alongside
	// the SEO summary; downstream consumers decide how to persist it.
	PageLinks []PageLink `json:"page_links,omitempty"`
	// PageLinksTruncated is set when captured placement variants exceeded MaxPageLinks.
	// In-process signal only (json:"-"), not serialized on this struct.
	PageLinksTruncated bool `json:"-"`
}

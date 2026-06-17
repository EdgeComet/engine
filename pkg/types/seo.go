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
	MaxHeadingsPerLevel      = 5
	MaxExternalDomains       = 20
	MaxBreadcrumbs           = 5
)

// PageSEO extraction limits - performance
const (
	MaxJSONLDSize           = 1024 * 1024 // 1MB per JSON-LD block
	MaxJSONLDRecursionDepth = 10          // Prevent stack overflow
)

// Link capture limits. Hard caps that bound the per-page link payload; exceeding
// MaxPageLinks truncates and sets PageSEO.PageLinksTruncated.
const (
	MaxPageLinks    = 1000 // max distinct outbound link targets captured per page
	MaxAnchorLength = 300  // max runes of anchor text stored per link
)

// PageLink is one captured outbound link from a rendered or bypassed page. Target is
// a normalized absolute URL string; no hash is computed here.
type PageLink struct {
	Target     string `json:"target"`
	Anchor     string `json:"anchor,omitempty"`
	IsInternal bool   `json:"is_internal,omitempty"`
	Nofollow   bool   `json:"nofollow,omitempty"`
	Sponsored  bool   `json:"sponsored,omitempty"`
	UGC        bool   `json:"ugc,omitempty"`
	IsImage    bool   `json:"is_image,omitempty"`
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

	// Headings (first 5 of each level, max 500 chars each)
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

	// Per-link outbound graph captured at render/bypass. Carried on the event alongside
	// the SEO summary; downstream consumers decide how to persist it.
	PageLinks []PageLink `json:"page_links,omitempty"`
	// PageLinksTruncated is set when distinct targets exceeded MaxPageLinks. In-process
	// signal only (json:"-"), not serialized.
	PageLinksTruncated bool `json:"-"`
}

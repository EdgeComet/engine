package har

// HAR 1.2 specification constants
const (
	harVersion     = "1.2"
	creatorName    = "EdgeComet"
	creatorVersion = "1.0"
)

// HAR is the root container for HTTP Archive format
type HAR struct {
	Log      Log          `json:"log"`
	Metadata *HARMetadata `json:"_metadata,omitempty"`
}

// Log contains the main HAR data
type Log struct {
	Version string   `json:"version"`
	Creator Creator  `json:"creator"`
	Browser *Browser `json:"browser,omitempty"`
	Pages   []Page   `json:"pages,omitempty"`
	Entries []Entry  `json:"entries"`
	Comment string   `json:"comment,omitempty"`
}

// Creator contains info about the HAR creator application
type Creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Comment string `json:"comment,omitempty"`
}

// Browser contains info about the browser that created the HAR
type Browser struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Comment string `json:"comment,omitempty"`
}

// Page represents a page within the HAR
type Page struct {
	StartedDateTime string      `json:"startedDateTime"`
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	PageTimings     PageTimings `json:"pageTimings"`
	Comment         string      `json:"comment,omitempty"`
}

// PageTimings contains timing info for page load events
type PageTimings struct {
	OnContentLoad *float64 `json:"onContentLoad,omitempty"`
	OnLoad        *float64 `json:"onLoad,omitempty"`
	Comment       string   `json:"comment,omitempty"`
}

// Entry represents a single HTTP request/response pair
type Entry struct {
	StartedDateTime string   `json:"startedDateTime"`
	Time            float64  `json:"time"`
	Request         Request  `json:"request"`
	Response        Response `json:"response"`
	Cache           Cache    `json:"cache"`
	Timings         Timings  `json:"timings"`
	ServerIPAddress string   `json:"serverIPAddress,omitempty"`
	Connection      string   `json:"connection,omitempty"`
	PageRef         string   `json:"pageref,omitempty"`
	Comment         string   `json:"comment,omitempty"`
}

// Request contains HTTP request details
type Request struct {
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	HTTPVersion string        `json:"httpVersion"`
	Cookies     []Cookie      `json:"cookies"`
	Headers     []Header      `json:"headers"`
	QueryString []QueryString `json:"queryString"`
	PostData    *PostData     `json:"postData,omitempty"`
	HeadersSize int64         `json:"headersSize"`
	BodySize    int64         `json:"bodySize"`
	Comment     string        `json:"comment,omitempty"`
}

// Response contains HTTP response details
type Response struct {
	Status      int      `json:"status"`
	StatusText  string   `json:"statusText"`
	HTTPVersion string   `json:"httpVersion"`
	Cookies     []Cookie `json:"cookies"`
	Headers     []Header `json:"headers"`
	Content     Content  `json:"content"`
	RedirectURL string   `json:"redirectURL"`
	HeadersSize int64    `json:"headersSize"`
	BodySize    int64    `json:"bodySize"`
	Comment     string   `json:"comment,omitempty"`
}

// Cookie represents an HTTP cookie
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Expires  string `json:"expires,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

// Header represents an HTTP header
type Header struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

// QueryString represents a URL query parameter
type QueryString struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

// PostData contains POST request body data
type PostData struct {
	MimeType string  `json:"mimeType"`
	Params   []Param `json:"params,omitempty"`
	Text     string  `json:"text,omitempty"`
	Comment  string  `json:"comment,omitempty"`
}

// Param represents a POST parameter
type Param struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// Content represents response body content
type Content struct {
	Size        int64  `json:"size"`
	Compression int64  `json:"compression,omitempty"`
	MimeType    string `json:"mimeType"`
	Text        string `json:"text,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// Cache contains cache information
type Cache struct {
	BeforeRequest *CacheEntry `json:"beforeRequest,omitempty"`
	AfterRequest  *CacheEntry `json:"afterRequest,omitempty"`
	Comment       string      `json:"comment,omitempty"`
}

// CacheEntry contains cache state details
type CacheEntry struct {
	Expires    string `json:"expires,omitempty"`
	LastAccess string `json:"lastAccess"`
	ETag       string `json:"eTag"`
	HitCount   int    `json:"hitCount"`
	Comment    string `json:"comment,omitempty"`
}

// Timings contains request timing breakdown
type Timings struct {
	Blocked float64 `json:"blocked,omitempty"`
	DNS     float64 `json:"dns,omitempty"`
	Connect float64 `json:"connect,omitempty"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl,omitempty"`
	Comment string  `json:"comment,omitempty"`
}

// Project-specific metadata types

// HARMetadata contains EdgeComet-specific metadata
type HARMetadata struct {
	BlockedRequests []BlockedRequest `json:"blockedRequests,omitempty"`
	FailedRequests  []FailedRequest  `json:"failedRequests,omitempty"`
	LifecycleEvents []LifecycleEvent `json:"lifecycleEvents,omitempty"`
	ConsoleErrors   []string         `json:"consoleErrors,omitempty"`
	RenderMetrics   *RenderMetrics   `json:"renderMetrics,omitempty"`
	RequestConfig   *RequestConfig   `json:"requestConfig,omitempty"`
	Truncated       bool             `json:"truncated,omitempty"`
}

// BlockedRequest contains info about a blocked network request
type BlockedRequest struct {
	URL          string `json:"url"`
	Reason       string `json:"reason"`
	ResourceType string `json:"resourceType"`
}

// FailedRequest contains info about a failed network request
type FailedRequest struct {
	URL          string `json:"url"`
	Error        string `json:"error"`
	ResourceType string `json:"resourceType"`
}

// LifecycleEvent represents a page lifecycle event
type LifecycleEvent struct {
	Name      string `json:"name"`
	Timestamp int64  `json:"timestamp"`
}

// RenderMetrics contains render operation metrics
type RenderMetrics struct {
	Duration  int64  `json:"duration"`
	TimedOut  bool   `json:"timedOut,omitempty"`
	ServiceID string `json:"serviceId,omitempty"`
}

// RequestConfig contains render request configuration
type RequestConfig struct {
	WaitFor              string   `json:"waitFor,omitempty"`
	BlockedPatterns      []string `json:"blockedPatterns,omitempty"`
	BlockedResourceTypes []string `json:"blockedResourceTypes,omitempty"`
	ViewportWidth        int      `json:"viewportWidth,omitempty"`
	ViewportHeight       int      `json:"viewportHeight,omitempty"`
	UserAgent            string   `json:"userAgent,omitempty"`
	Timeout              int64    `json:"timeout,omitempty"`   // milliseconds
	ExtraWait            int64    `json:"extraWait,omitempty"` // milliseconds
}

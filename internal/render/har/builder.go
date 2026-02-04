package har

import (
	"encoding/json"
	"net/url"
	"sort"
	"time"
)

// HARBuilder assembles HAR data incrementally
type HARBuilder struct {
	har       *HAR
	metadata  *HARMetadata
	startTime time.Time
	pageID    string
}

// NewHARBuilder creates a new HAR builder for the given page
func NewHARBuilder(pageURL string, requestID string, browserVersion string) *HARBuilder {
	now := time.Now().UTC()
	pageID := "page_" + requestID

	var browser *Browser
	if browserVersion != "" {
		browser = &Browser{
			Name:    "Chrome",
			Version: browserVersion,
		}
	}

	builder := &HARBuilder{
		har: &HAR{
			Log: Log{
				Version: harVersion,
				Creator: Creator{
					Name:    creatorName,
					Version: creatorVersion,
				},
				Browser: browser,
				Pages: []Page{
					{
						StartedDateTime: formatDateTime(now),
						ID:              pageID,
						Title:           pageURL,
						PageTimings:     PageTimings{},
					},
				},
				Entries: []Entry{},
			},
		},
		metadata:  &HARMetadata{},
		startTime: now,
		pageID:    pageID,
	}

	return builder
}

// SetPageTimings sets the page load timing values in milliseconds
func (b *HARBuilder) SetPageTimings(onContentLoad, onLoad int64) {
	if len(b.har.Log.Pages) == 0 {
		return
	}

	onContentLoadFloat := float64(onContentLoad)
	onLoadFloat := float64(onLoad)

	b.har.Log.Pages[0].PageTimings.OnContentLoad = &onContentLoadFloat
	b.har.Log.Pages[0].PageTimings.OnLoad = &onLoadFloat
}

// Finalize returns the completed HAR struct
func (b *HARBuilder) Finalize() *HAR {
	// Sort entries chronologically by start time (HAR spec requirement)
	sort.Slice(b.har.Log.Entries, func(i, j int) bool {
		return b.har.Log.Entries[i].StartedDateTime < b.har.Log.Entries[j].StartedDateTime
	})

	b.har.Metadata = b.metadata
	return b.har
}

// ToJSON marshals the HAR to JSON bytes
func (b *HARBuilder) ToJSON() ([]byte, error) {
	b.har.Metadata = b.metadata
	return json.Marshal(b.har)
}

// formatDateTime formats a time to ISO 8601 with milliseconds
func formatDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// GetPageID returns the page reference ID
func (b *HARBuilder) GetPageID() string {
	return b.pageID
}

// GetStartTime returns the builder's start time
func (b *HARBuilder) GetStartTime() time.Time {
	return b.startTime
}

// GetMetadata returns the metadata struct for direct manipulation
func (b *HARBuilder) GetMetadata() *HARMetadata {
	return b.metadata
}

// EntryData contains data for creating a HAR entry
type EntryData struct {
	RequestID          string
	URL                string
	Method             string
	RequestHeaders     []Header
	ResponseStatus     int
	ResponseStatusText string
	ResponseHeaders    []Header
	BodySize           int64
	StartTime          time.Time
	FinishTime         time.Time
	Timings            *Timings
	ReceiveHeadersEnd  float64 // Chrome timing for receive calculation
	ResourceType       string
	MimeType           string
	Protocol           string // HTTP protocol version (h2, http/1.1, etc.)
}

// AddEntry adds a network request entry to the HAR
func (b *HARBuilder) AddEntry(data EntryData) {
	timings := timingsOrDefault(data.Timings)

	// Calculate receive time from finish time if available
	if !data.FinishTime.IsZero() && data.ReceiveHeadersEnd > 0 {
		totalDuration := float64(data.FinishTime.Sub(data.StartTime).Milliseconds())
		// Receive time = total duration - time until headers received
		timings.Receive = totalDuration - data.ReceiveHeadersEnd
		if timings.Receive < 0 {
			timings.Receive = 0
		}
	}

	// Convert Chrome protocol to HAR HTTP version format
	httpVersion := protocolToHTTPVersion(data.Protocol)

	entry := Entry{
		StartedDateTime: formatDateTime(data.StartTime),
		Time:            calculateTotalTime(&timings),
		Request: Request{
			Method:      data.Method,
			URL:         data.URL,
			HTTPVersion: httpVersion,
			Cookies:     []Cookie{},
			Headers:     data.RequestHeaders,
			QueryString: parseQueryString(data.URL),
			HeadersSize: -1,
			BodySize:    0,
		},
		Response: Response{
			Status:      data.ResponseStatus,
			StatusText:  data.ResponseStatusText,
			HTTPVersion: httpVersion,
			Cookies:     []Cookie{},
			Headers:     data.ResponseHeaders,
			Content: Content{
				Size:     data.BodySize,
				MimeType: data.MimeType,
			},
			RedirectURL: "",
			HeadersSize: -1,
			BodySize:    data.BodySize,
		},
		Cache:   Cache{},
		Timings: timings,
		PageRef: b.pageID,
	}

	if data.RequestHeaders == nil {
		entry.Request.Headers = []Header{}
	}
	if data.ResponseHeaders == nil {
		entry.Response.Headers = []Header{}
	}

	b.har.Log.Entries = append(b.har.Log.Entries, entry)
}

// calculateTotalTime sums all timing values in milliseconds
func calculateTotalTime(t *Timings) float64 {
	if t == nil {
		return 0
	}

	total := 0.0
	if t.Blocked > 0 {
		total += t.Blocked
	}
	if t.DNS > 0 {
		total += t.DNS
	}
	if t.Connect > 0 {
		total += t.Connect
	}
	if t.SSL > 0 {
		total += t.SSL
	}
	total += t.Send
	total += t.Wait
	total += t.Receive

	return total
}

// timingsOrDefault returns the timings or a default with zeros
func timingsOrDefault(t *Timings) Timings {
	if t == nil {
		return Timings{
			Send:    0,
			Wait:    0,
			Receive: 0,
		}
	}
	return *t
}

// AddBlockedEntry adds a blocked request entry to the HAR
func (b *HARBuilder) AddBlockedEntry(reqURL, reason, resourceType string, startTime time.Time) {
	entry := Entry{
		StartedDateTime: formatDateTime(startTime),
		Time:            0,
		Request: Request{
			Method:      "GET",
			URL:         reqURL,
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			QueryString: parseQueryString(reqURL),
			HeadersSize: -1,
			BodySize:    0,
		},
		Response: Response{
			Status:      0,
			StatusText:  reason,
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			Content: Content{
				Size:     0,
				MimeType: "",
			},
			RedirectURL: "",
			HeadersSize: -1,
			BodySize:    0,
		},
		Cache: Cache{},
		Timings: Timings{
			Send:    0,
			Wait:    0,
			Receive: 0,
		},
		PageRef: b.pageID,
	}

	b.har.Log.Entries = append(b.har.Log.Entries, entry)

	b.metadata.BlockedRequests = append(b.metadata.BlockedRequests, BlockedRequest{
		URL:          reqURL,
		Reason:       reason,
		ResourceType: resourceType,
	})
}

// AddFailedEntry adds a failed request entry to the HAR
func (b *HARBuilder) AddFailedEntry(reqURL, errorMsg, resourceType string, startTime time.Time) {
	entry := Entry{
		StartedDateTime: formatDateTime(startTime),
		Time:            0,
		Request: Request{
			Method:      "GET",
			URL:         reqURL,
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			QueryString: parseQueryString(reqURL),
			HeadersSize: -1,
			BodySize:    0,
		},
		Response: Response{
			Status:      0,
			StatusText:  errorMsg,
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			Content: Content{
				Size:     0,
				MimeType: "",
			},
			RedirectURL: "",
			HeadersSize: -1,
			BodySize:    0,
		},
		Cache: Cache{},
		Timings: Timings{
			Send:    0,
			Wait:    0,
			Receive: 0,
		},
		PageRef: b.pageID,
	}

	b.har.Log.Entries = append(b.har.Log.Entries, entry)

	b.metadata.FailedRequests = append(b.metadata.FailedRequests, FailedRequest{
		URL:          reqURL,
		Error:        errorMsg,
		ResourceType: resourceType,
	})
}

// SetLifecycleEvents sets the page lifecycle events in metadata
func (b *HARBuilder) SetLifecycleEvents(events []LifecycleEvent) {
	b.metadata.LifecycleEvents = events
}

// SetConsoleErrors sets the console errors in metadata
func (b *HARBuilder) SetConsoleErrors(errors []string) {
	b.metadata.ConsoleErrors = errors
}

// SetRenderMetrics sets the render metrics in metadata
func (b *HARBuilder) SetRenderMetrics(duration int64, timedOut bool, serviceID string) {
	b.metadata.RenderMetrics = &RenderMetrics{
		Duration:  duration,
		TimedOut:  timedOut,
		ServiceID: serviceID,
	}
}

// SetRequestConfig sets the request configuration in metadata
func (b *HARBuilder) SetRequestConfig(config RequestConfig) {
	b.metadata.RequestConfig = &config
}

// GetEntryCount returns the number of entries in the HAR
func (b *HARBuilder) GetEntryCount() int {
	return len(b.har.Log.Entries)
}

// protocolToHTTPVersion converts Chrome protocol string to HAR HTTP version format
func protocolToHTTPVersion(protocol string) string {
	switch protocol {
	case "h2":
		return "HTTP/2"
	case "h3":
		return "HTTP/3"
	case "http/1.0":
		return "HTTP/1.0"
	case "http/1.1":
		return "HTTP/1.1"
	default:
		return "HTTP/1.1"
	}
}

// parseQueryString extracts query parameters from URL into HAR QueryString format
func parseQueryString(rawURL string) []QueryString {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return []QueryString{}
	}

	values := parsed.Query()
	result := make([]QueryString, 0, len(values))

	// Sort keys for consistent output
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		for _, val := range values[key] {
			result = append(result, QueryString{Name: key, Value: val})
		}
	}
	return result
}

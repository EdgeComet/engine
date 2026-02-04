package har

import (
	"sort"
	"sync"
	"time"
)

// HARCollector accumulates Chrome DevTools Protocol events during render
type HARCollector struct {
	mu        sync.RWMutex
	requests  map[string]*requestData
	blocked   []blockedData
	failed    []failedData
	startTime time.Time
	pageURL   string
	requestID string
}

// requestData holds data for an in-progress request
type requestData struct {
	URL          string
	Method       string
	Headers      map[string]string
	StartTime    time.Time
	FinishTime   time.Time
	Response     *responseData
	Timings      *TimingData
	ResourceType string
	Finished     bool
	BodySize     int64
}

// responseData holds response information
type responseData struct {
	Status     int
	StatusText string
	Headers    map[string]string
	BodySize   int64
	MimeType   string
	Protocol   string
}

// TimingData mirrors Chrome's ResourceTiming object
type TimingData struct {
	DNSStart          float64
	DNSEnd            float64
	ConnectStart      float64
	ConnectEnd        float64
	SSLStart          float64
	SSLEnd            float64
	SendStart         float64
	SendEnd           float64
	ReceiveHeadersEnd float64
}

// blockedData holds information about a blocked request
type blockedData struct {
	RequestID    string
	URL          string
	Reason       string
	ResourceType string
	Time         time.Time
}

// failedData holds information about a failed request
type failedData struct {
	RequestID    string
	URL          string
	Error        string
	ResourceType string
	Time         time.Time
	Canceled     bool
}

// NewHARCollector creates a new HAR collector
func NewHARCollector(pageURL, requestID string) *HARCollector {
	return &HARCollector{
		requests:  make(map[string]*requestData),
		blocked:   make([]blockedData, 0),
		failed:    make([]failedData, 0),
		startTime: time.Now().UTC(),
		pageURL:   pageURL,
		requestID: requestID,
	}
}

// Reset clears all accumulated data
func (c *HARCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.requests = make(map[string]*requestData)
	c.blocked = make([]blockedData, 0)
	c.failed = make([]failedData, 0)
	c.startTime = time.Now().UTC()
}

// GetRequestCount returns the number of tracked requests
func (c *HARCollector) GetRequestCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.requests)
}

// GetBlockedCount returns the number of blocked requests
func (c *HARCollector) GetBlockedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.blocked)
}

// GetFailedCount returns the number of failed requests
func (c *HARCollector) GetFailedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.failed)
}

// GetPageURL returns the page URL
func (c *HARCollector) GetPageURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pageURL
}

// GetRequestID returns the request ID
func (c *HARCollector) GetRequestID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.requestID
}

// GetStartTime returns the collector's start time
func (c *HARCollector) GetStartTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startTime
}

// OnRequestWillBeSent handles the Network.requestWillBeSent event
func (c *HARCollector) OnRequestWillBeSent(requestID, url, method string, headers map[string]string, resourceType string, timestamp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqTime := c.timestampToTime(timestamp)

	// Check if this is a redirect (same requestID sent again)
	if existing, ok := c.requests[requestID]; ok {
		// Update existing request with redirect info
		existing.URL = url
		existing.Method = method
		existing.Headers = headers
		existing.ResourceType = resourceType
		existing.StartTime = reqTime
		return
	}

	c.requests[requestID] = &requestData{
		URL:          url,
		Method:       method,
		Headers:      headers,
		StartTime:    reqTime,
		ResourceType: resourceType,
		Finished:     false,
	}
}

// OnRequestWillBeSentExtraInfo handles the Network.requestWillBeSentExtraInfo event
func (c *HARCollector) OnRequestWillBeSentExtraInfo(requestID string, headers map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.requests[requestID]
	if !ok {
		return
	}

	// Merge headers (extra info contains actual headers with cookies)
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}

	for k, v := range headers {
		req.Headers[k] = v
	}
}

// convertHeaders converts a map to HAR Header slice, sorted by name
func convertHeaders(h map[string]string) []Header {
	if h == nil {
		return []Header{}
	}

	headers := make([]Header, 0, len(h))
	for name, value := range h {
		headers = append(headers, Header{Name: name, Value: value})
	}

	// Sort for consistent output
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].Name < headers[j].Name
	})

	return headers
}

// timestampToTime converts Chrome's monotonic timestamp to time.Time
func (c *HARCollector) timestampToTime(chromeTimestamp float64) time.Time {
	// Chrome timestamps are in seconds since an arbitrary epoch
	// We convert relative to the collector's start time
	// For simplicity, we use the timestamp as seconds offset from start
	offsetMs := chromeTimestamp * 1000
	return c.startTime.Add(time.Duration(offsetMs) * time.Millisecond)
}

// OnResponseReceived handles the Network.responseReceived event
func (c *HARCollector) OnResponseReceived(requestID string, status int, statusText string, headers map[string]string, mimeType string, timing *TimingData, protocol string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.requests[requestID]
	if !ok {
		return
	}

	req.Response = &responseData{
		Status:     status,
		StatusText: statusText,
		Headers:    headers,
		MimeType:   mimeType,
		Protocol:   protocol,
	}

	if timing != nil {
		req.Timings = timing
	}
}

// OnLoadingFinished handles the Network.loadingFinished event
func (c *HARCollector) OnLoadingFinished(requestID string, encodedDataLength int64, timestamp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.requests[requestID]
	if !ok {
		return
	}

	req.Finished = true
	req.FinishTime = c.timestampToTime(timestamp)
	req.BodySize = encodedDataLength

	if req.Response != nil {
		req.Response.BodySize = encodedDataLength
	}
}

// OnLoadingFailed handles the Network.loadingFailed event
func (c *HARCollector) OnLoadingFailed(requestID string, errorText string, canceled bool, timestamp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.requests[requestID]
	if !ok {
		return
	}

	// Move to failed list
	c.failed = append(c.failed, failedData{
		RequestID:    requestID,
		URL:          req.URL,
		Error:        errorText,
		ResourceType: req.ResourceType,
		Time:         c.timestampToTime(timestamp),
		Canceled:     canceled,
	})

	// Remove from active requests
	delete(c.requests, requestID)
}

// convertTiming converts Chrome's timing data to HAR Timings
func convertTiming(td *TimingData) *Timings {
	if td == nil {
		return nil
	}

	timings := &Timings{
		Send:    td.SendEnd - td.SendStart,
		Wait:    td.ReceiveHeadersEnd - td.SendEnd,
		Receive: 0, // Calculated from loadingFinished
	}

	// Blocked time: time until first network activity begins
	// Chrome timing values are relative to requestTime (which is 0)
	// First activity is DNS lookup, or connection if DNS cached, or send if connection reused
	firstActivity := td.SendStart
	if td.DNSStart >= 0 {
		firstActivity = td.DNSStart
	} else if td.ConnectStart >= 0 {
		firstActivity = td.ConnectStart
	}

	if firstActivity > 0 {
		timings.Blocked = firstActivity
	} else {
		timings.Blocked = -1
	}

	// DNS timing (-1 if not applicable)
	if td.DNSEnd > 0 && td.DNSStart >= 0 {
		timings.DNS = td.DNSEnd - td.DNSStart
	} else {
		timings.DNS = -1
	}

	// Connect timing (-1 if not applicable)
	if td.ConnectEnd > 0 && td.ConnectStart >= 0 {
		timings.Connect = td.ConnectEnd - td.ConnectStart
	} else {
		timings.Connect = -1
	}

	// SSL timing (-1 if no SSL)
	if td.SSLEnd > 0 && td.SSLStart >= 0 {
		timings.SSL = td.SSLEnd - td.SSLStart
	} else {
		timings.SSL = -1
	}

	return timings
}

// OnRequestBlocked handles requests blocked by the renderer
func (c *HARCollector) OnRequestBlocked(requestID, url, reason, resourceType string, timestamp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.blocked = append(c.blocked, blockedData{
		RequestID:    requestID,
		URL:          url,
		Reason:       reason,
		ResourceType: resourceType,
		Time:         c.timestampToTime(timestamp),
	})
}

// Build creates a HAR from collected data
func (c *HARCollector) Build(lifecycleEvents []LifecycleEvent, consoleErrors []string, renderMetrics RenderMetrics, requestConfig RequestConfig, browserVersion string) *HAR {
	c.mu.RLock()
	defer c.mu.RUnlock()

	builder := NewHARBuilder(c.pageURL, c.requestID, browserVersion)

	// Set page timings from lifecycle events
	onContentLoad := findLifecycleTime(lifecycleEvents, "DOMContentLoaded")
	onLoad := findLifecycleTime(lifecycleEvents, "load")
	builder.SetPageTimings(onContentLoad, onLoad)

	// Add completed requests
	for _, req := range c.requests {
		builder.AddEntry(c.buildEntryData(req))
	}

	// Add blocked entries
	for _, blocked := range c.blocked {
		builder.AddBlockedEntry(blocked.URL, blocked.Reason, blocked.ResourceType, blocked.Time)
	}

	// Add failed entries
	for _, failed := range c.failed {
		builder.AddFailedEntry(failed.URL, failed.Error, failed.ResourceType, failed.Time)
	}

	// Set metadata
	builder.SetLifecycleEvents(lifecycleEvents)
	builder.SetConsoleErrors(consoleErrors)
	builder.SetRenderMetrics(renderMetrics.Duration, renderMetrics.TimedOut, renderMetrics.ServiceID)
	builder.SetRequestConfig(requestConfig)

	return builder.Finalize()
}

// buildEntryData converts internal requestData to EntryData for the builder
func (c *HARCollector) buildEntryData(req *requestData) EntryData {
	entry := EntryData{
		URL:        req.URL,
		Method:     req.Method,
		StartTime:  req.StartTime,
		FinishTime: req.FinishTime,
		BodySize:   req.BodySize,
	}

	// Convert request headers
	entry.RequestHeaders = convertHeaders(req.Headers)

	// Add response data if available
	if req.Response != nil {
		entry.ResponseStatus = req.Response.Status
		entry.ResponseStatusText = req.Response.StatusText
		entry.ResponseHeaders = convertHeaders(req.Response.Headers)
		entry.MimeType = req.Response.MimeType
		entry.Protocol = req.Response.Protocol
		if req.Response.BodySize > 0 {
			entry.BodySize = req.Response.BodySize
		}
	} else if !req.Finished {
		// Mark incomplete requests that never received a response
		entry.ResponseStatusText = "Incomplete: no response received"
	}

	// Convert timings if available
	if req.Timings != nil {
		entry.Timings = convertTiming(req.Timings)
		entry.ReceiveHeadersEnd = req.Timings.ReceiveHeadersEnd
	}

	return entry
}

// findLifecycleTime finds a lifecycle event by name and returns its timestamp
func findLifecycleTime(events []LifecycleEvent, name string) int64 {
	for _, event := range events {
		if event.Name == name {
			return event.Timestamp
		}
	}
	return 0
}

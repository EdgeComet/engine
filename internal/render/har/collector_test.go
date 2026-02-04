package har

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHARCollector(t *testing.T) {
	pageURL := "https://example.com/test"
	requestID := "req-123"

	collector := NewHARCollector(pageURL, requestID)

	require.NotNil(t, collector)
	assert.Equal(t, pageURL, collector.GetPageURL())
	assert.Equal(t, requestID, collector.GetRequestID())
	assert.NotNil(t, collector.requests)
	assert.NotNil(t, collector.blocked)
	assert.NotNil(t, collector.failed)
	assert.Equal(t, 0, collector.GetRequestCount())
	assert.Equal(t, 0, collector.GetBlockedCount())
	assert.Equal(t, 0, collector.GetFailedCount())
	assert.False(t, collector.GetStartTime().IsZero())
}

func TestCollectorReset(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Add some data
	collector.requests["req-1"] = &requestData{URL: "https://example.com/1"}
	collector.requests["req-2"] = &requestData{URL: "https://example.com/2"}
	collector.blocked = append(collector.blocked, blockedData{URL: "https://blocked.com"})
	collector.failed = append(collector.failed, failedData{URL: "https://failed.com"})

	originalStartTime := collector.startTime

	// Wait a tiny bit to ensure different timestamp
	time.Sleep(time.Millisecond)

	collector.Reset()

	assert.Equal(t, 0, collector.GetRequestCount())
	assert.Equal(t, 0, collector.GetBlockedCount())
	assert.Equal(t, 0, collector.GetFailedCount())
	assert.True(t, collector.GetStartTime().After(originalStartTime))
}

func TestCollectorConcurrentAccess(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			collector.mu.Lock()
			collector.requests[string(rune('a'+id%26))] = &requestData{URL: "https://example.com"}
			collector.mu.Unlock()
		}(i)
	}

	// Concurrent reads
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = collector.GetRequestCount()
			_ = collector.GetBlockedCount()
			_ = collector.GetFailedCount()
			_ = collector.GetPageURL()
			_ = collector.GetRequestID()
			_ = collector.GetStartTime()
		}()
	}

	wg.Wait()

	// Should complete without race conditions
	assert.Greater(t, collector.GetRequestCount(), 0)
}

func TestCollectorMapOperations(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Test map initialization
	assert.NotNil(t, collector.requests)

	// Test map operations are safe
	collector.mu.Lock()
	collector.requests["test-1"] = &requestData{
		URL:          "https://example.com/test",
		Method:       "GET",
		ResourceType: "document",
	}
	collector.mu.Unlock()

	assert.Equal(t, 1, collector.GetRequestCount())

	// Verify data
	collector.mu.RLock()
	req := collector.requests["test-1"]
	collector.mu.RUnlock()

	assert.Equal(t, "https://example.com/test", req.URL)
	assert.Equal(t, "GET", req.Method)
}

func TestCollectorStartTimeIsUTC(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	assert.Equal(t, time.UTC, collector.GetStartTime().Location())
}

func TestCollectorSliceOperations(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Test blocked slice
	collector.mu.Lock()
	collector.blocked = append(collector.blocked, blockedData{
		RequestID:    "req-1",
		URL:          "https://blocked.com",
		Reason:       "Pattern match",
		ResourceType: "script",
		Time:         time.Now(),
	})
	collector.mu.Unlock()

	assert.Equal(t, 1, collector.GetBlockedCount())

	// Test failed slice
	collector.mu.Lock()
	collector.failed = append(collector.failed, failedData{
		RequestID:    "req-2",
		URL:          "https://failed.com",
		Error:        "Connection refused",
		ResourceType: "xhr",
		Time:         time.Now(),
		Canceled:     false,
	})
	collector.mu.Unlock()

	assert.Equal(t, 1, collector.GetFailedCount())
}

func TestRequestDataStruct(t *testing.T) {
	req := &requestData{
		URL:          "https://example.com/api",
		Method:       "POST",
		Headers:      map[string]string{"Content-Type": "application/json"},
		StartTime:    time.Now(),
		ResourceType: "xhr",
		Finished:     false,
		BodySize:     100,
	}

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "xhr", req.ResourceType)
	assert.False(t, req.Finished)
}

func TestResponseDataStruct(t *testing.T) {
	resp := &responseData{
		Status:     200,
		StatusText: "OK",
		Headers:    map[string]string{"Content-Type": "text/html"},
		BodySize:   1234,
		MimeType:   "text/html",
	}

	assert.Equal(t, 200, resp.Status)
	assert.Equal(t, "text/html", resp.MimeType)
}

func TestTimingDataStruct(t *testing.T) {
	timing := &TimingData{
		DNSStart:          0,
		DNSEnd:            10,
		ConnectStart:      10,
		ConnectEnd:        25,
		SSLStart:          25,
		SSLEnd:            45,
		SendStart:         45,
		SendEnd:           46,
		ReceiveHeadersEnd: 146,
	}

	assert.Equal(t, 10.0, timing.DNSEnd-timing.DNSStart)
	assert.Equal(t, 15.0, timing.ConnectEnd-timing.ConnectStart)
	assert.Equal(t, 20.0, timing.SSLEnd-timing.SSLStart)
}

func TestBlockedDataStruct(t *testing.T) {
	blocked := blockedData{
		RequestID:    "req-123",
		URL:          "https://ads.example.com/track",
		Reason:       "Blocked by pattern: *ads*",
		ResourceType: "script",
		Time:         time.Now(),
	}

	assert.Equal(t, "req-123", blocked.RequestID)
	assert.Contains(t, blocked.Reason, "pattern")
}

func TestFailedDataStruct(t *testing.T) {
	failed := failedData{
		RequestID:    "req-456",
		URL:          "https://example.com/broken",
		Error:        "net::ERR_CONNECTION_REFUSED",
		ResourceType: "xhr",
		Time:         time.Now(),
		Canceled:     false,
	}

	assert.Equal(t, "net::ERR_CONNECTION_REFUSED", failed.Error)
	assert.False(t, failed.Canceled)

	// Test canceled request
	canceled := failedData{
		RequestID: "req-789",
		URL:       "https://example.com/slow",
		Error:     "net::ERR_ABORTED",
		Canceled:  true,
	}

	assert.True(t, canceled.Canceled)
}

func TestOnRequestWillBeSent(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	headers := map[string]string{
		"Accept":     "text/html",
		"User-Agent": "Chrome/120",
	}

	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", headers, "xhr", 0.1)

	assert.Equal(t, 1, collector.GetRequestCount())

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	require.NotNil(t, req)
	assert.Equal(t, "https://example.com/api", req.URL)
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "xhr", req.ResourceType)
	assert.Equal(t, "text/html", req.Headers["Accept"])
	assert.False(t, req.Finished)
}

func TestOnRequestWillBeSentRedirect(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// First request
	collector.OnRequestWillBeSent("req-123", "https://example.com/old", "GET", nil, "document", 0.1)

	// Same requestID sent again (redirect)
	collector.OnRequestWillBeSent("req-123", "https://example.com/new", "GET", nil, "document", 0.2)

	// Should still be 1 request, but with updated URL
	assert.Equal(t, 1, collector.GetRequestCount())

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	assert.Equal(t, "https://example.com/new", req.URL)
}

func TestOnRequestWillBeSentExtraInfo(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Initial request with basic headers
	initialHeaders := map[string]string{
		"Accept": "text/html",
	}
	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", initialHeaders, "xhr", 0.1)

	// Extra info with cookies and more headers
	extraHeaders := map[string]string{
		"Cookie":        "session=abc123",
		"Authorization": "Bearer token",
	}
	collector.OnRequestWillBeSentExtraInfo("req-123", extraHeaders)

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	// Should have merged headers
	assert.Equal(t, "text/html", req.Headers["Accept"])
	assert.Equal(t, "session=abc123", req.Headers["Cookie"])
	assert.Equal(t, "Bearer token", req.Headers["Authorization"])
}

func TestOnRequestWillBeSentExtraInfoMissingRequest(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Extra info for non-existent request should not panic
	collector.OnRequestWillBeSentExtraInfo("non-existent", map[string]string{"key": "value"})

	assert.Equal(t, 0, collector.GetRequestCount())
}

func TestOnRequestWillBeSentExtraInfoNilHeaders(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Request with nil headers
	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", nil, "xhr", 0.1)

	// Extra info should initialize headers
	collector.OnRequestWillBeSentExtraInfo("req-123", map[string]string{"Cookie": "test"})

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	assert.Equal(t, "test", req.Headers["Cookie"])
}

func TestConvertHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected int
	}{
		{
			name:     "nil headers",
			input:    nil,
			expected: 0,
		},
		{
			name:     "empty headers",
			input:    map[string]string{},
			expected: 0,
		},
		{
			name: "multiple headers",
			input: map[string]string{
				"Content-Type": "text/html",
				"Accept":       "application/json",
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHeaders(tt.input)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestConvertHeadersSorted(t *testing.T) {
	headers := map[string]string{
		"Zebra":  "z",
		"Alpha":  "a",
		"Middle": "m",
		"Beta":   "b",
	}

	result := convertHeaders(headers)

	require.Len(t, result, 4)
	assert.Equal(t, "Alpha", result[0].Name)
	assert.Equal(t, "Beta", result[1].Name)
	assert.Equal(t, "Middle", result[2].Name)
	assert.Equal(t, "Zebra", result[3].Name)
}

func TestTimestampToTime(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")
	startTime := collector.GetStartTime()

	// Timestamp of 1.5 seconds
	result := collector.timestampToTime(1.5)

	expected := startTime.Add(1500 * time.Millisecond)
	assert.Equal(t, expected, result)
}

func TestTimestampToTimeZero(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")
	startTime := collector.GetStartTime()

	result := collector.timestampToTime(0)

	assert.Equal(t, startTime, result)
}

func TestOnRequestWillBeSentConcurrent(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := string(rune('A' + id%26))
			collector.OnRequestWillBeSent(reqID, "https://example.com", "GET", nil, "xhr", float64(id)/100)
		}(i)
	}

	wg.Wait()

	// Should complete without race conditions
	assert.Greater(t, collector.GetRequestCount(), 0)
}

func TestOnRequestWillBeSentResourceTypes(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	resourceTypes := []string{"document", "stylesheet", "image", "font", "script", "xhr", "fetch", "other"}

	for i, rt := range resourceTypes {
		reqID := string(rune('A' + i))
		collector.OnRequestWillBeSent(reqID, "https://example.com", "GET", nil, rt, 0.1)
	}

	assert.Equal(t, len(resourceTypes), collector.GetRequestCount())
}

func TestOnResponseReceived(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", nil, "xhr", 0.1)

	headers := map[string]string{"Content-Type": "application/json"}
	timing := &TimingData{
		DNSStart: 0, DNSEnd: 10,
		ConnectStart: 10, ConnectEnd: 25,
		SSLStart: 25, SSLEnd: 45,
		SendStart: 45, SendEnd: 46,
		ReceiveHeadersEnd: 146,
	}

	collector.OnResponseReceived("req-123", 200, "OK", headers, "application/json", timing, "h2")

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	require.NotNil(t, req.Response)
	assert.Equal(t, 200, req.Response.Status)
	assert.Equal(t, "OK", req.Response.StatusText)
	assert.Equal(t, "application/json", req.Response.MimeType)
	assert.Equal(t, "h2", req.Response.Protocol)
	require.NotNil(t, req.Timings)
}

func TestOnResponseReceivedMissingRequest(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Should not panic
	collector.OnResponseReceived("non-existent", 200, "OK", nil, "text/html", nil, "")

	assert.Equal(t, 0, collector.GetRequestCount())
}

func TestOnLoadingFinished(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", nil, "xhr", 0.1)
	collector.OnResponseReceived("req-123", 200, "OK", nil, "application/json", nil, "")
	collector.OnLoadingFinished("req-123", 1234, 0.5)

	collector.mu.RLock()
	req := collector.requests["req-123"]
	collector.mu.RUnlock()

	assert.True(t, req.Finished)
	assert.Equal(t, int64(1234), req.BodySize)
	assert.Equal(t, int64(1234), req.Response.BodySize)
}

func TestOnLoadingFinishedMissingRequest(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Should not panic
	collector.OnLoadingFinished("non-existent", 1234, 0.5)

	assert.Equal(t, 0, collector.GetRequestCount())
}

func TestOnLoadingFailed(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestWillBeSent("req-123", "https://example.com/api", "GET", nil, "xhr", 0.1)
	collector.OnLoadingFailed("req-123", "net::ERR_CONNECTION_REFUSED", false, 0.5)

	// Should be removed from requests
	assert.Equal(t, 0, collector.GetRequestCount())
	// Should be in failed list
	assert.Equal(t, 1, collector.GetFailedCount())

	collector.mu.RLock()
	failed := collector.failed[0]
	collector.mu.RUnlock()

	assert.Equal(t, "https://example.com/api", failed.URL)
	assert.Equal(t, "net::ERR_CONNECTION_REFUSED", failed.Error)
	assert.False(t, failed.Canceled)
}

func TestOnLoadingFailedCanceled(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestWillBeSent("req-123", "https://example.com/slow", "GET", nil, "xhr", 0.1)
	collector.OnLoadingFailed("req-123", "net::ERR_ABORTED", true, 0.5)

	collector.mu.RLock()
	failed := collector.failed[0]
	collector.mu.RUnlock()

	assert.True(t, failed.Canceled)
}

func TestConvertTiming(t *testing.T) {
	td := &TimingData{
		DNSStart:          0,
		DNSEnd:            10,
		ConnectStart:      10,
		ConnectEnd:        25,
		SSLStart:          25,
		SSLEnd:            45,
		SendStart:         45,
		SendEnd:           46,
		ReceiveHeadersEnd: 146,
	}

	timings := convertTiming(td)

	require.NotNil(t, timings)
	assert.Equal(t, 10.0, timings.DNS)
	assert.Equal(t, 15.0, timings.Connect)
	assert.Equal(t, 20.0, timings.SSL)
	assert.Equal(t, 1.0, timings.Send)
	assert.Equal(t, 100.0, timings.Wait)
}

func TestConvertTimingNil(t *testing.T) {
	timings := convertTiming(nil)
	assert.Nil(t, timings)
}

func TestConvertTimingNoSSL(t *testing.T) {
	td := &TimingData{
		DNSStart:          0,
		DNSEnd:            10,
		ConnectStart:      10,
		ConnectEnd:        25,
		SSLStart:          -1,
		SSLEnd:            -1,
		SendStart:         25,
		SendEnd:           26,
		ReceiveHeadersEnd: 126,
	}

	timings := convertTiming(td)

	assert.Equal(t, -1.0, timings.SSL)
	assert.Equal(t, 10.0, timings.DNS)
	assert.Equal(t, 15.0, timings.Connect)
}

func TestOnRequestBlocked(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestBlocked("req-123", "https://ads.com/track", "Blocked by pattern: *ads*", "script", 0.1)

	assert.Equal(t, 1, collector.GetBlockedCount())
	assert.Equal(t, 0, collector.GetRequestCount())

	collector.mu.RLock()
	blocked := collector.blocked[0]
	collector.mu.RUnlock()

	assert.Equal(t, "req-123", blocked.RequestID)
	assert.Equal(t, "https://ads.com/track", blocked.URL)
	assert.Equal(t, "Blocked by pattern: *ads*", blocked.Reason)
	assert.Equal(t, "script", blocked.ResourceType)
}

func TestOnRequestBlockedMultiple(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	collector.OnRequestBlocked("req-1", "https://ads.com/1", "Pattern", "script", 0.1)
	collector.OnRequestBlocked("req-2", "https://analytics.com/2", "Pattern", "image", 0.2)
	collector.OnRequestBlocked("req-3", "https://tracker.com/3", "Resource type", "xhr", 0.3)

	assert.Equal(t, 3, collector.GetBlockedCount())
}

func TestBuild(t *testing.T) {
	collector := NewHARCollector("https://example.com/page", "req-main")

	// Add a request
	collector.OnRequestWillBeSent("req-1", "https://example.com/api", "GET", map[string]string{"Accept": "application/json"}, "xhr", 0.1)
	collector.OnResponseReceived("req-1", 200, "OK", map[string]string{"Content-Type": "application/json"}, "application/json", nil, "h2")
	collector.OnLoadingFinished("req-1", 500, 0.2)

	// Add blocked
	collector.OnRequestBlocked("req-2", "https://ads.com", "Blocked", "script", 0.15)

	lifecycleEvents := []LifecycleEvent{
		{Name: "DOMContentLoaded", Timestamp: 1500},
		{Name: "load", Timestamp: 2500},
	}
	consoleErrors := []string{"Error 1"}
	renderMetrics := RenderMetrics{Duration: 2500, TimedOut: false, ServiceID: "render-1"}
	requestConfig := RequestConfig{WaitFor: "networkidle", BlockedPatterns: []string{"*ads*"}}

	har := collector.Build(lifecycleEvents, consoleErrors, renderMetrics, requestConfig, "Chrome/120.0.0.0")

	require.NotNil(t, har)
	assert.Equal(t, "1.2", har.Log.Version)
	assert.Len(t, har.Log.Pages, 1)
	assert.Equal(t, "https://example.com/page", har.Log.Pages[0].Title)

	// Browser info should be set
	require.NotNil(t, har.Log.Browser)
	assert.Equal(t, "Chrome", har.Log.Browser.Name)
	assert.Equal(t, "Chrome/120.0.0.0", har.Log.Browser.Version)

	// Should have 2 entries (1 request + 1 blocked)
	assert.Len(t, har.Log.Entries, 2)
}

func TestBuildEmpty(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	har := collector.Build(nil, nil, RenderMetrics{}, RequestConfig{}, "")

	require.NotNil(t, har)
	assert.Len(t, har.Log.Entries, 0)
}

func TestFindLifecycleTime(t *testing.T) {
	events := []LifecycleEvent{
		{Name: "DOMContentLoaded", Timestamp: 1500},
		{Name: "load", Timestamp: 2500},
	}

	assert.Equal(t, int64(1500), findLifecycleTime(events, "DOMContentLoaded"))
	assert.Equal(t, int64(2500), findLifecycleTime(events, "load"))
	assert.Equal(t, int64(0), findLifecycleTime(events, "notfound"))
	assert.Equal(t, int64(0), findLifecycleTime(nil, "load"))
}

func TestBuildWithIncompleteRequest(t *testing.T) {
	collector := NewHARCollector("https://example.com", "req-1")

	// Add request that never receives response or finishes
	collector.OnRequestWillBeSent("incomplete-1", "https://example.com/timeout", "GET", nil, "xhr", 0.1)

	// Add complete request for comparison
	collector.OnRequestWillBeSent("complete-1", "https://example.com/success", "GET", nil, "document", 0.2)
	collector.OnResponseReceived("complete-1", 200, "OK", nil, "text/html", nil, "")
	collector.OnLoadingFinished("complete-1", 1000, 0.5)

	har := collector.Build(nil, nil, RenderMetrics{}, RequestConfig{}, "")

	require.Len(t, har.Log.Entries, 2)

	// Find incomplete entry
	var incompleteEntry *Entry
	var completeEntry *Entry
	for i := range har.Log.Entries {
		if har.Log.Entries[i].Request.URL == "https://example.com/timeout" {
			incompleteEntry = &har.Log.Entries[i]
		} else {
			completeEntry = &har.Log.Entries[i]
		}
	}

	require.NotNil(t, incompleteEntry)
	require.NotNil(t, completeEntry)

	// Incomplete request should be marked
	assert.Equal(t, 0, incompleteEntry.Response.Status)
	assert.Equal(t, "Incomplete: no response received", incompleteEntry.Response.StatusText)

	// Complete request should have normal status
	assert.Equal(t, 200, completeEntry.Response.Status)
	assert.Equal(t, "OK", completeEntry.Response.StatusText)
}

package har

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHARBuilder(t *testing.T) {
	pageURL := "https://example.com/test"
	requestID := "req-123"

	builder := NewHARBuilder(pageURL, requestID, "")

	require.NotNil(t, builder)
	require.NotNil(t, builder.har)
	require.NotNil(t, builder.metadata)

	assert.Equal(t, harVersion, builder.har.Log.Version)
	assert.Equal(t, creatorName, builder.har.Log.Creator.Name)
	assert.Equal(t, creatorVersion, builder.har.Log.Creator.Version)

	require.Len(t, builder.har.Log.Pages, 1)
	page := builder.har.Log.Pages[0]
	assert.Equal(t, "page_"+requestID, page.ID)
	assert.Equal(t, pageURL, page.Title)
	assert.NotEmpty(t, page.StartedDateTime)

	assert.NotNil(t, builder.har.Log.Entries)
	assert.Len(t, builder.har.Log.Entries, 0)

	assert.Equal(t, "page_"+requestID, builder.GetPageID())
	assert.False(t, builder.GetStartTime().IsZero())
}

func TestSetPageTimings(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.SetPageTimings(1500, 2500)

	page := builder.har.Log.Pages[0]
	require.NotNil(t, page.PageTimings.OnContentLoad)
	require.NotNil(t, page.PageTimings.OnLoad)
	assert.Equal(t, 1500.0, *page.PageTimings.OnContentLoad)
	assert.Equal(t, 2500.0, *page.PageTimings.OnLoad)
}

func TestSetPageTimingsWithZeroValues(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.SetPageTimings(0, 0)

	page := builder.har.Log.Pages[0]
	require.NotNil(t, page.PageTimings.OnContentLoad)
	require.NotNil(t, page.PageTimings.OnLoad)
	assert.Equal(t, 0.0, *page.PageTimings.OnContentLoad)
	assert.Equal(t, 0.0, *page.PageTimings.OnLoad)
}

func TestFinalize(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	builder.SetPageTimings(1000, 2000)

	har := builder.Finalize()

	require.NotNil(t, har)
	assert.Equal(t, harVersion, har.Log.Version)
	assert.Len(t, har.Log.Pages, 1)
	assert.NotNil(t, har.Log.Entries)
}

func TestToJSON(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	builder.SetPageTimings(1500, 2500)

	data, err := builder.ToJSON()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var result HAR
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, harVersion, result.Log.Version)
	assert.Equal(t, creatorName, result.Log.Creator.Name)
	assert.Len(t, result.Log.Pages, 1)
}

func TestToJSONProducesValidStructure(t *testing.T) {
	builder := NewHARBuilder("https://example.com/page", "test-123", "")
	builder.SetPageTimings(1500, 3000)

	data, err := builder.ToJSON()
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	log, ok := parsed["log"].(map[string]interface{})
	require.True(t, ok, "log should be an object")

	assert.Equal(t, harVersion, log["version"])

	creator, ok := log["creator"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, creatorName, creator["name"])

	pages, ok := log["pages"].([]interface{})
	require.True(t, ok)
	require.Len(t, pages, 1)

	page := pages[0].(map[string]interface{})
	assert.Equal(t, "https://example.com/page", page["title"])
	assert.Equal(t, "page_test-123", page["id"])
}

func TestMultipleCallsDoNotCorruptState(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.SetPageTimings(1000, 2000)
	har1 := builder.Finalize()
	json1, err := builder.ToJSON()
	require.NoError(t, err)

	builder.SetPageTimings(3000, 4000)
	har2 := builder.Finalize()
	json2, err := builder.ToJSON()
	require.NoError(t, err)

	assert.Same(t, har1, har2, "Finalize should return the same HAR instance")

	assert.Equal(t, 3000.0, *har2.Log.Pages[0].PageTimings.OnContentLoad)
	assert.Equal(t, 4000.0, *har2.Log.Pages[0].PageTimings.OnLoad)

	assert.NotEqual(t, json1, json2, "JSON should reflect updated timings")
}

func TestFormatDateTime(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC)

	result := formatDateTime(testTime)

	assert.Equal(t, "2024-01-15T10:30:45.123Z", result)
}

func TestFormatDateTimeConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	localTime := time.Date(2024, 1, 15, 5, 30, 0, 0, loc)

	result := formatDateTime(localTime)

	assert.Equal(t, "2024-01-15T10:30:00.000Z", result)
}

func TestGetMetadata(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	metadata := builder.GetMetadata()
	require.NotNil(t, metadata)

	metadata.ConsoleErrors = []string{"error 1"}

	assert.Len(t, builder.metadata.ConsoleErrors, 1)
}

func TestBuilderStartTimeIsUTC(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	assert.Equal(t, time.UTC, builder.GetStartTime().Location())
}

func TestEmptyEntriesArrayInJSON(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	data, err := builder.ToJSON()
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	log := result["log"].(map[string]interface{})
	entries, ok := log["entries"].([]interface{})
	require.True(t, ok, "entries should be present as array")
	assert.Len(t, entries, 0)
}

func TestAddEntryWithCompleteData(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	timings := &Timings{
		Blocked: 5,
		DNS:     10,
		Connect: 15,
		SSL:     20,
		Send:    1,
		Wait:    100,
		Receive: 50,
	}

	builder.AddEntry(EntryData{
		RequestID:          "req-123",
		URL:                "https://example.com/api/data",
		Method:             "GET",
		RequestHeaders:     []Header{{Name: "Accept", Value: "application/json"}},
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		ResponseHeaders:    []Header{{Name: "Content-Type", Value: "application/json"}},
		BodySize:           1234,
		StartTime:          startTime,
		Timings:            timings,
		ResourceType:       "xhr",
		MimeType:           "application/json",
	})

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 1)

	entry := har.Log.Entries[0]
	assert.Equal(t, "2024-01-15T10:30:00.000Z", entry.StartedDateTime)
	assert.Equal(t, 201.0, entry.Time)
	assert.Equal(t, "GET", entry.Request.Method)
	assert.Equal(t, "https://example.com/api/data", entry.Request.URL)
	assert.Len(t, entry.Request.Headers, 1)
	assert.Equal(t, 200, entry.Response.Status)
	assert.Equal(t, "OK", entry.Response.StatusText)
	assert.Equal(t, int64(1234), entry.Response.BodySize)
	assert.Equal(t, "application/json", entry.Response.Content.MimeType)
	assert.Equal(t, builder.GetPageID(), entry.PageRef)
}

func TestAddEntryWithMinimalData(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	builder.AddEntry(EntryData{
		URL:                "https://example.com/test",
		Method:             "GET",
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		StartTime:          startTime,
		Timings:            nil,
	})

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 1)

	entry := har.Log.Entries[0]
	assert.Equal(t, 0.0, entry.Time)
	assert.Equal(t, 0.0, entry.Timings.Send)
	assert.Equal(t, 0.0, entry.Timings.Wait)
	assert.Equal(t, 0.0, entry.Timings.Receive)
	assert.NotNil(t, entry.Request.Headers)
	assert.NotNil(t, entry.Response.Headers)
}

func TestAddMultipleEntriesMaintainOrder(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	urls := []string{
		"https://example.com/first",
		"https://example.com/second",
		"https://example.com/third",
	}

	for i, url := range urls {
		builder.AddEntry(EntryData{
			URL:                url,
			Method:             "GET",
			ResponseStatus:     200,
			ResponseStatusText: "OK",
			StartTime:          baseTime.Add(time.Duration(i) * time.Second),
		})
	}

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 3)

	for i, url := range urls {
		assert.Equal(t, url, har.Log.Entries[i].Request.URL)
	}
}

func TestCalculateTotalTime(t *testing.T) {
	tests := []struct {
		name     string
		timings  *Timings
		expected float64
	}{
		{
			name:     "nil timings",
			timings:  nil,
			expected: 0,
		},
		{
			name: "all positive values",
			timings: &Timings{
				Blocked: 5,
				DNS:     10,
				Connect: 15,
				SSL:     20,
				Send:    1,
				Wait:    100,
				Receive: 50,
			},
			expected: 201,
		},
		{
			name: "only required fields",
			timings: &Timings{
				Send:    1,
				Wait:    100,
				Receive: 50,
			},
			expected: 151,
		},
		{
			name: "negative values ignored for optional",
			timings: &Timings{
				Blocked: -1,
				DNS:     -1,
				Connect: -1,
				SSL:     -1,
				Send:    1,
				Wait:    100,
				Receive: 50,
			},
			expected: 151,
		},
		{
			name: "fractional values",
			timings: &Timings{
				Send:    0.5,
				Wait:    99.25,
				Receive: 50.75,
			},
			expected: 150.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTotalTime(tt.timings)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddEntryNilHeaders(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.AddEntry(EntryData{
		URL:                "https://example.com/test",
		Method:             "GET",
		RequestHeaders:     nil,
		ResponseHeaders:    nil,
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		StartTime:          time.Now(),
	})

	har := builder.Finalize()
	entry := har.Log.Entries[0]

	assert.NotNil(t, entry.Request.Headers)
	assert.NotNil(t, entry.Response.Headers)
	assert.Len(t, entry.Request.Headers, 0)
	assert.Len(t, entry.Response.Headers, 0)
}

func TestAddEntryPageRef(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "test-req-456", "")

	builder.AddEntry(EntryData{
		URL:                "https://example.com/test",
		Method:             "GET",
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		StartTime:          time.Now(),
	})

	har := builder.Finalize()
	assert.Equal(t, "page_test-req-456", har.Log.Entries[0].PageRef)
}

func TestAddBlockedEntry(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	builder.AddBlockedEntry(
		"https://analytics.example.com/track",
		"Blocked by pattern: *analytics*",
		"script",
		startTime,
	)

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 1)

	entry := har.Log.Entries[0]
	assert.Equal(t, 0, entry.Response.Status)
	assert.Equal(t, "Blocked by pattern: *analytics*", entry.Response.StatusText)
	assert.Equal(t, "https://analytics.example.com/track", entry.Request.URL)
	assert.Equal(t, "2024-01-15T10:30:00.000Z", entry.StartedDateTime)
	assert.Equal(t, 0.0, entry.Time)
	assert.Equal(t, builder.GetPageID(), entry.PageRef)

	metadata := builder.GetMetadata()
	require.Len(t, metadata.BlockedRequests, 1)
	assert.Equal(t, "https://analytics.example.com/track", metadata.BlockedRequests[0].URL)
	assert.Equal(t, "Blocked by pattern: *analytics*", metadata.BlockedRequests[0].Reason)
	assert.Equal(t, "script", metadata.BlockedRequests[0].ResourceType)
}

func TestAddFailedEntry(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	builder.AddFailedEntry(
		"https://example.com/broken",
		"net::ERR_CONNECTION_REFUSED",
		"xhr",
		startTime,
	)

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 1)

	entry := har.Log.Entries[0]
	assert.Equal(t, 0, entry.Response.Status)
	assert.Equal(t, "net::ERR_CONNECTION_REFUSED", entry.Response.StatusText)
	assert.Equal(t, "https://example.com/broken", entry.Request.URL)
	assert.Equal(t, "2024-01-15T10:30:00.000Z", entry.StartedDateTime)
	assert.Equal(t, 0.0, entry.Time)

	metadata := builder.GetMetadata()
	require.Len(t, metadata.FailedRequests, 1)
	assert.Equal(t, "https://example.com/broken", metadata.FailedRequests[0].URL)
	assert.Equal(t, "net::ERR_CONNECTION_REFUSED", metadata.FailedRequests[0].Error)
	assert.Equal(t, "xhr", metadata.FailedRequests[0].ResourceType)
}

func TestMultipleBlockedAndFailedEntries(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	builder.AddBlockedEntry("https://ads.com/1", "Blocked: ads", "script", baseTime)
	builder.AddBlockedEntry("https://ads.com/2", "Blocked: ads", "image", baseTime.Add(time.Second))
	builder.AddFailedEntry("https://broken.com/1", "Connection refused", "xhr", baseTime.Add(2*time.Second))
	builder.AddFailedEntry("https://broken.com/2", "Timeout", "fetch", baseTime.Add(3*time.Second))

	har := builder.Finalize()
	assert.Len(t, har.Log.Entries, 4)

	metadata := builder.GetMetadata()
	assert.Len(t, metadata.BlockedRequests, 2)
	assert.Len(t, metadata.FailedRequests, 2)
}

func TestBlockedEntryHasZeroTimings(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.AddBlockedEntry("https://blocked.com", "Blocked", "script", time.Now())

	har := builder.Finalize()
	entry := har.Log.Entries[0]

	assert.Equal(t, 0.0, entry.Timings.Send)
	assert.Equal(t, 0.0, entry.Timings.Wait)
	assert.Equal(t, 0.0, entry.Timings.Receive)
	assert.Equal(t, 0.0, entry.Timings.DNS)
	assert.Equal(t, 0.0, entry.Timings.Connect)
	assert.Equal(t, 0.0, entry.Timings.SSL)
}

func TestMixedEntriesOrder(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	builder.AddEntry(EntryData{
		URL:                "https://example.com/first",
		Method:             "GET",
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		StartTime:          baseTime,
	})

	builder.AddBlockedEntry("https://blocked.com", "Blocked", "script", baseTime.Add(time.Second))

	builder.AddEntry(EntryData{
		URL:                "https://example.com/second",
		Method:             "GET",
		ResponseStatus:     200,
		ResponseStatusText: "OK",
		StartTime:          baseTime.Add(2 * time.Second),
	})

	builder.AddFailedEntry("https://failed.com", "Error", "xhr", baseTime.Add(3*time.Second))

	har := builder.Finalize()
	require.Len(t, har.Log.Entries, 4)

	assert.Equal(t, "https://example.com/first", har.Log.Entries[0].Request.URL)
	assert.Equal(t, "https://blocked.com", har.Log.Entries[1].Request.URL)
	assert.Equal(t, "https://example.com/second", har.Log.Entries[2].Request.URL)
	assert.Equal(t, "https://failed.com", har.Log.Entries[3].Request.URL)
}

func TestSetLifecycleEvents(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	events := []LifecycleEvent{
		{Name: "DOMContentLoaded", Timestamp: 1500},
		{Name: "load", Timestamp: 2500},
	}

	builder.SetLifecycleEvents(events)

	metadata := builder.GetMetadata()
	require.Len(t, metadata.LifecycleEvents, 2)
	assert.Equal(t, "DOMContentLoaded", metadata.LifecycleEvents[0].Name)
	assert.Equal(t, int64(1500), metadata.LifecycleEvents[0].Timestamp)
	assert.Equal(t, "load", metadata.LifecycleEvents[1].Name)
	assert.Equal(t, int64(2500), metadata.LifecycleEvents[1].Timestamp)
}

func TestSetConsoleErrors(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	errors := []string{"Error 1", "Error 2", "Error 3"}
	builder.SetConsoleErrors(errors)

	metadata := builder.GetMetadata()
	require.Len(t, metadata.ConsoleErrors, 3)
	assert.Equal(t, "Error 1", metadata.ConsoleErrors[0])
}

func TestSetRenderMetrics(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.SetRenderMetrics(2500, false, "render-1")

	metadata := builder.GetMetadata()
	require.NotNil(t, metadata.RenderMetrics)
	assert.Equal(t, int64(2500), metadata.RenderMetrics.Duration)
	assert.False(t, metadata.RenderMetrics.TimedOut)
	assert.Equal(t, "render-1", metadata.RenderMetrics.ServiceID)
}

func TestSetRenderMetricsTimedOut(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	builder.SetRenderMetrics(30000, true, "render-2")

	metadata := builder.GetMetadata()
	assert.True(t, metadata.RenderMetrics.TimedOut)
}

func TestSetRequestConfig(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	config := RequestConfig{
		WaitFor:              "networkidle",
		BlockedPatterns:      []string{"*.analytics.com", "*.ads.com"},
		BlockedResourceTypes: []string{"Image", "Font"},
		ViewportWidth:        1920,
		ViewportHeight:       1080,
		UserAgent:            "TestBot/1.0",
		Timeout:              30000,
		ExtraWait:            500,
	}
	builder.SetRequestConfig(config)

	metadata := builder.GetMetadata()
	require.NotNil(t, metadata.RequestConfig)
	assert.Equal(t, "networkidle", metadata.RequestConfig.WaitFor)
	require.Len(t, metadata.RequestConfig.BlockedPatterns, 2)
	assert.Equal(t, "*.analytics.com", metadata.RequestConfig.BlockedPatterns[0])
	require.Len(t, metadata.RequestConfig.BlockedResourceTypes, 2)
	assert.Equal(t, "Image", metadata.RequestConfig.BlockedResourceTypes[0])
	assert.Equal(t, 1920, metadata.RequestConfig.ViewportWidth)
	assert.Equal(t, 1080, metadata.RequestConfig.ViewportHeight)
	assert.Equal(t, "TestBot/1.0", metadata.RequestConfig.UserAgent)
	assert.Equal(t, int64(30000), metadata.RequestConfig.Timeout)
	assert.Equal(t, int64(500), metadata.RequestConfig.ExtraWait)
}

func TestGetEntryCount(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	assert.Equal(t, 0, builder.GetEntryCount())

	builder.AddEntry(EntryData{
		URL: "https://example.com/1", Method: "GET",
		ResponseStatus: 200, ResponseStatusText: "OK", StartTime: time.Now(),
	})
	assert.Equal(t, 1, builder.GetEntryCount())

	builder.AddBlockedEntry("https://blocked.com", "Blocked", "script", time.Now())
	assert.Equal(t, 2, builder.GetEntryCount())
}

func TestMetadataIncludedInJSONOutput(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	// Set various metadata
	builder.SetLifecycleEvents([]LifecycleEvent{
		{Name: "DOMContentLoaded", Timestamp: 100},
		{Name: "load", Timestamp: 200},
	})
	builder.SetConsoleErrors([]string{"Error: test error"})
	builder.SetRenderMetrics(1500, false, "render-1")
	builder.SetRequestConfig(RequestConfig{WaitFor: "networkIdle", BlockedPatterns: []string{"*.analytics.com"}})

	// Add a blocked entry to test blocked requests in metadata
	builder.AddBlockedEntry("https://blocked.com", "Pattern match", "script", time.Now())

	// Marshal to JSON
	data, err := builder.ToJSON()
	require.NoError(t, err)

	// Parse JSON to verify metadata is present
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Check _metadata field exists
	metadata, ok := result["_metadata"].(map[string]interface{})
	require.True(t, ok, "_metadata field should be present in JSON output")

	// Verify lifecycle events
	lifecycleEvents, ok := metadata["lifecycleEvents"].([]interface{})
	require.True(t, ok, "lifecycleEvents should be present")
	assert.Len(t, lifecycleEvents, 2)

	// Verify console errors
	consoleErrors, ok := metadata["consoleErrors"].([]interface{})
	require.True(t, ok, "consoleErrors should be present")
	assert.Len(t, consoleErrors, 1)
	assert.Equal(t, "Error: test error", consoleErrors[0])

	// Verify render metrics
	renderMetrics, ok := metadata["renderMetrics"].(map[string]interface{})
	require.True(t, ok, "renderMetrics should be present")
	assert.Equal(t, float64(1500), renderMetrics["duration"])
	// timedOut=false is omitted with omitempty, so it won't be present
	_, hasTimedOut := renderMetrics["timedOut"]
	assert.False(t, hasTimedOut, "timedOut=false should be omitted")
	assert.Equal(t, "render-1", renderMetrics["serviceId"])

	// Verify request config
	requestConfig, ok := metadata["requestConfig"].(map[string]interface{})
	require.True(t, ok, "requestConfig should be present")
	assert.Equal(t, "networkIdle", requestConfig["waitFor"])

	// Verify blocked requests
	blockedRequests, ok := metadata["blockedRequests"].([]interface{})
	require.True(t, ok, "blockedRequests should be present")
	assert.Len(t, blockedRequests, 1)
}

func TestMetadataAttachedOnFinalize(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	// Set metadata
	builder.SetConsoleErrors([]string{"test error"})

	// Finalize should attach metadata
	har := builder.Finalize()

	require.NotNil(t, har.Metadata, "Metadata should be attached after Finalize")
	assert.Len(t, har.Metadata.ConsoleErrors, 1)
}

func TestFinalizeSortsEntriesChronologically(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	baseTime := time.Now().UTC()

	// Add entries out of order
	builder.AddEntry(EntryData{
		URL:       "https://example.com/third",
		Method:    "GET",
		StartTime: baseTime.Add(200 * time.Millisecond),
	})
	builder.AddEntry(EntryData{
		URL:       "https://example.com/first",
		Method:    "GET",
		StartTime: baseTime,
	})
	builder.AddEntry(EntryData{
		URL:       "https://example.com/second",
		Method:    "GET",
		StartTime: baseTime.Add(100 * time.Millisecond),
	})

	har := builder.Finalize()

	require.Len(t, har.Log.Entries, 3)
	assert.Equal(t, "https://example.com/first", har.Log.Entries[0].Request.URL)
	assert.Equal(t, "https://example.com/second", har.Log.Entries[1].Request.URL)
	assert.Equal(t, "https://example.com/third", har.Log.Entries[2].Request.URL)
}

func TestReceiveTimeCalculation(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	startTime := time.Now().UTC()
	finishTime := startTime.Add(500 * time.Millisecond)

	builder.AddEntry(EntryData{
		URL:               "https://example.com/test",
		Method:            "GET",
		StartTime:         startTime,
		FinishTime:        finishTime,
		ReceiveHeadersEnd: 200, // Headers received 200ms after start
		Timings: &Timings{
			Send: 10,
			Wait: 190,
		},
	})

	har := builder.Finalize()

	require.Len(t, har.Log.Entries, 1)
	entry := har.Log.Entries[0]

	// Receive time = 500ms (total) - 200ms (headers received) = 300ms
	assert.Equal(t, 300.0, entry.Timings.Receive)
}

func TestReceiveTimeCalculationWithZeroReceiveHeadersEnd(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	startTime := time.Now().UTC()
	finishTime := startTime.Add(500 * time.Millisecond)

	builder.AddEntry(EntryData{
		URL:               "https://example.com/test",
		Method:            "GET",
		StartTime:         startTime,
		FinishTime:        finishTime,
		ReceiveHeadersEnd: 0, // No timing data
		Timings: &Timings{
			Send:    10,
			Wait:    190,
			Receive: 0,
		},
	})

	har := builder.Finalize()

	require.Len(t, har.Log.Entries, 1)
	// Receive should remain 0 when ReceiveHeadersEnd is 0
	assert.Equal(t, 0.0, har.Log.Entries[0].Timings.Receive)
}

func TestReceiveTimeCalculationWithZeroFinishTime(t *testing.T) {
	builder := NewHARBuilder("https://example.com", "req-1", "")

	startTime := time.Now().UTC()

	builder.AddEntry(EntryData{
		URL:               "https://example.com/test",
		Method:            "GET",
		StartTime:         startTime,
		FinishTime:        time.Time{}, // Zero finish time
		ReceiveHeadersEnd: 200,
		Timings: &Timings{
			Send:    10,
			Wait:    190,
			Receive: 0,
		},
	})

	har := builder.Finalize()

	require.Len(t, har.Log.Entries, 1)
	// Receive should remain 0 when FinishTime is zero
	assert.Equal(t, 0.0, har.Log.Entries[0].Timings.Receive)
}

func TestProtocolToHTTPVersion(t *testing.T) {
	tests := []struct {
		protocol string
		expected string
	}{
		{"h2", "HTTP/2"},
		{"h3", "HTTP/3"},
		{"http/1.0", "HTTP/1.0"},
		{"http/1.1", "HTTP/1.1"},
		{"", "HTTP/1.1"},
		{"unknown", "HTTP/1.1"},
		{"HTTP/2", "HTTP/1.1"}, // uppercase not recognized
	}

	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			result := protocolToHTTPVersion(tt.protocol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

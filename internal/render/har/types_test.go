package har

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHARJSONMarshal(t *testing.T) {
	har := HAR{
		Log: Log{
			Version: harVersion,
			Creator: Creator{
				Name:    creatorName,
				Version: creatorVersion,
			},
			Pages: []Page{
				{
					StartedDateTime: "2024-01-15T10:30:00.000Z",
					ID:              "page_1",
					Title:           "Test Page",
					PageTimings:     PageTimings{},
				},
			},
			Entries: []Entry{
				{
					StartedDateTime: "2024-01-15T10:30:00.100Z",
					Time:            150.5,
					Request: Request{
						Method:      "GET",
						URL:         "https://example.com/test",
						HTTPVersion: "HTTP/1.1",
						Cookies:     []Cookie{},
						Headers: []Header{
							{Name: "Host", Value: "example.com"},
						},
						QueryString: []QueryString{},
						HeadersSize: 100,
						BodySize:    0,
					},
					Response: Response{
						Status:      200,
						StatusText:  "OK",
						HTTPVersion: "HTTP/1.1",
						Cookies:     []Cookie{},
						Headers: []Header{
							{Name: "Content-Type", Value: "text/html"},
						},
						Content: Content{
							Size:     1234,
							MimeType: "text/html",
						},
						RedirectURL: "",
						HeadersSize: 150,
						BodySize:    1234,
					},
					Cache: Cache{},
					Timings: Timings{
						Send:    1.0,
						Wait:    100.0,
						Receive: 49.5,
					},
					PageRef: "page_1",
				},
			},
		},
	}

	data, err := json.Marshal(har)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	log, ok := result["log"].(map[string]interface{})
	require.True(t, ok, "log should be an object")

	assert.Equal(t, harVersion, log["version"])

	creator, ok := log["creator"].(map[string]interface{})
	require.True(t, ok, "creator should be an object")
	assert.Equal(t, creatorName, creator["name"])
	assert.Equal(t, creatorVersion, creator["version"])

	entries, ok := log["entries"].([]interface{})
	require.True(t, ok, "entries should be an array")
	assert.Len(t, entries, 1)
}

func TestOptionalFieldsOmitted(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		notInKey string
	}{
		{
			name: "PageTimings without onContentLoad",
			input: PageTimings{
				Comment: "test",
			},
			notInKey: "onContentLoad",
		},
		{
			name: "PageTimings without onLoad",
			input: PageTimings{
				Comment: "test",
			},
			notInKey: "onLoad",
		},
		{
			name: "Entry without serverIPAddress",
			input: Entry{
				StartedDateTime: "2024-01-15T10:30:00.000Z",
				Time:            100,
				Request: Request{
					Method:      "GET",
					URL:         "https://example.com",
					HTTPVersion: "HTTP/1.1",
					Cookies:     []Cookie{},
					Headers:     []Header{},
					QueryString: []QueryString{},
					HeadersSize: 0,
					BodySize:    0,
				},
				Response: Response{
					Status:      200,
					StatusText:  "OK",
					HTTPVersion: "HTTP/1.1",
					Cookies:     []Cookie{},
					Headers:     []Header{},
					Content:     Content{MimeType: "text/html"},
					HeadersSize: 0,
					BodySize:    0,
				},
				Cache:   Cache{},
				Timings: Timings{Send: 1, Wait: 50, Receive: 49},
			},
			notInKey: "serverIPAddress",
		},
		{
			name: "Request without postData",
			input: Request{
				Method:      "GET",
				URL:         "https://example.com",
				HTTPVersion: "HTTP/1.1",
				Cookies:     []Cookie{},
				Headers:     []Header{},
				QueryString: []QueryString{},
				HeadersSize: 0,
				BodySize:    0,
			},
			notInKey: "postData",
		},
		{
			name: "HARMetadata without truncated",
			input: HARMetadata{
				BlockedRequests: []BlockedRequest{},
			},
			notInKey: "truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			_, exists := result[tt.notInKey]
			assert.False(t, exists, "field %s should be omitted when empty", tt.notInKey)
		})
	}
}

func TestRequiredFieldsPresent(t *testing.T) {
	entry := Entry{
		StartedDateTime: "2024-01-15T10:30:00.000Z",
		Time:            100,
		Request: Request{
			Method:      "GET",
			URL:         "https://example.com",
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			QueryString: []QueryString{},
			HeadersSize: 0,
			BodySize:    0,
		},
		Response: Response{
			Status:      200,
			StatusText:  "OK",
			HTTPVersion: "HTTP/1.1",
			Cookies:     []Cookie{},
			Headers:     []Header{},
			Content:     Content{Size: 0, MimeType: "text/html"},
			RedirectURL: "",
			HeadersSize: 0,
			BodySize:    0,
		},
		Cache:   Cache{},
		Timings: Timings{Send: 1, Wait: 50, Receive: 49},
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	requiredFields := []string{
		"startedDateTime",
		"time",
		"request",
		"response",
		"cache",
		"timings",
	}

	for _, field := range requiredFields {
		_, exists := result[field]
		assert.True(t, exists, "required field %s should be present", field)
	}

	request, ok := result["request"].(map[string]interface{})
	require.True(t, ok)
	requestRequiredFields := []string{"method", "url", "httpVersion", "cookies", "headers", "queryString", "headersSize", "bodySize"}
	for _, field := range requestRequiredFields {
		_, exists := request[field]
		assert.True(t, exists, "required request field %s should be present", field)
	}

	response, ok := result["response"].(map[string]interface{})
	require.True(t, ok)
	responseRequiredFields := []string{"status", "statusText", "httpVersion", "cookies", "headers", "content", "redirectURL", "headersSize", "bodySize"}
	for _, field := range responseRequiredFields {
		_, exists := response[field]
		assert.True(t, exists, "required response field %s should be present", field)
	}

	timings, ok := result["timings"].(map[string]interface{})
	require.True(t, ok)
	timingsRequiredFields := []string{"send", "wait", "receive"}
	for _, field := range timingsRequiredFields {
		_, exists := timings[field]
		assert.True(t, exists, "required timings field %s should be present", field)
	}
}

func TestOptionalPageTimingsWithValues(t *testing.T) {
	onContentLoad := 1500.0
	onLoad := 2500.0

	pt := PageTimings{
		OnContentLoad: &onContentLoad,
		OnLoad:        &onLoad,
	}

	data, err := json.Marshal(pt)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, onContentLoad, result["onContentLoad"])
	assert.Equal(t, onLoad, result["onLoad"])
}

func TestBlockedRequestMetadata(t *testing.T) {
	blocked := BlockedRequest{
		URL:          "https://analytics.example.com/track",
		Reason:       "Blocked by pattern: *analytics*",
		ResourceType: "script",
	}

	data, err := json.Marshal(blocked)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, blocked.URL, result["url"])
	assert.Equal(t, blocked.Reason, result["reason"])
	assert.Equal(t, blocked.ResourceType, result["resourceType"])
}

func TestFailedRequestMetadata(t *testing.T) {
	failed := FailedRequest{
		URL:          "https://example.com/broken",
		Error:        "net::ERR_CONNECTION_REFUSED",
		ResourceType: "xhr",
	}

	data, err := json.Marshal(failed)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, failed.URL, result["url"])
	assert.Equal(t, failed.Error, result["error"])
	assert.Equal(t, failed.ResourceType, result["resourceType"])
}

func TestRenderMetrics(t *testing.T) {
	metrics := RenderMetrics{
		Duration:  2500,
		TimedOut:  false,
		ServiceID: "render-1",
	}

	data, err := json.Marshal(metrics)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, float64(2500), result["duration"])
	assert.Equal(t, "render-1", result["serviceId"])
	_, hasTimedOut := result["timedOut"]
	assert.False(t, hasTimedOut, "timedOut should be omitted when false")
}

func TestHARMetadataTruncated(t *testing.T) {
	metadata := HARMetadata{
		Truncated: true,
	}

	data, err := json.Marshal(metadata)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, true, result["truncated"])
}

func TestLifecycleEvent(t *testing.T) {
	event := LifecycleEvent{
		Name:      "DOMContentLoaded",
		Timestamp: 1500,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "DOMContentLoaded", result["name"])
	assert.Equal(t, float64(1500), result["timestamp"])
}

func TestRequestConfig(t *testing.T) {
	config := RequestConfig{
		WaitFor:         "networkidle",
		BlockedPatterns: []string{"*.analytics.com", "*.ads.com"},
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "networkidle", result["waitFor"])

	patterns, ok := result["blockedPatterns"].([]interface{})
	require.True(t, ok)
	assert.Len(t, patterns, 2)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "1.2", harVersion)
	assert.Equal(t, "EdgeComet", creatorName)
	assert.Equal(t, "1.0", creatorVersion)
}

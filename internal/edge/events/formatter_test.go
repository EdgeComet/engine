package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestNewTemplateFormatter_ValidTemplate(t *testing.T) {
	tests := []struct {
		name           string
		template       string
		expectedCount  int
		expectedFields []string
	}{
		{
			name:           "single placeholder",
			template:       "{url}",
			expectedCount:  1,
			expectedFields: []string{"url"},
		},
		{
			name:           "multiple placeholders",
			template:       "{timestamp} {host} {url} {status_code}",
			expectedCount:  4,
			expectedFields: []string{"timestamp", "host", "url", "status_code"},
		},
		{
			name:           "nested placeholder",
			template:       "{metrics.total_requests}",
			expectedCount:  1,
			expectedFields: []string{"metrics.total_requests"},
		},
		{
			name:           "mixed placeholders",
			template:       "{timestamp} {url} {metrics.total_bytes} {status_code}",
			expectedCount:  4,
			expectedFields: []string{"timestamp", "url", "metrics.total_bytes", "status_code"},
		},
		{
			name:           "static text only",
			template:       "This is static text without placeholders",
			expectedCount:  0,
			expectedFields: []string{},
		},
		{
			name:           "placeholders with static text",
			template:       "Request: {request_id} Host: {host} Status: {status_code}",
			expectedCount:  3,
			expectedFields: []string{"request_id", "host", "status_code"},
		},
		{
			name:           "all metrics fields",
			template:       "{metrics.final_url} {metrics.total_requests} {metrics.timed_out}",
			expectedCount:  3,
			expectedFields: []string{"metrics.final_url", "metrics.total_requests", "metrics.timed_out"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter, err := NewTemplateFormatter(tt.template)
			require.NoError(t, err)
			require.NotNil(t, formatter)

			assert.Equal(t, tt.template, formatter.Template())
			assert.Len(t, formatter.Placeholders(), tt.expectedCount)

			for i, expected := range tt.expectedFields {
				assert.Equal(t, "{"+expected+"}", formatter.Placeholders()[i].raw)
			}
		})
	}
}

func TestNewTemplateFormatter_EmptyTemplate(t *testing.T) {
	formatter, err := NewTemplateFormatter("")
	assert.Nil(t, formatter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template cannot be empty")
}

func TestNewTemplateFormatter_UnknownPlaceholder(t *testing.T) {
	tests := []struct {
		name          string
		template      string
		expectedField string
	}{
		{
			name:          "typo in field name",
			template:      "{stauts_code}",
			expectedField: "stauts_code",
		},
		{
			name:          "non-existent field",
			template:      "{unknown_field}",
			expectedField: "unknown_field",
		},
		{
			name:          "invalid nested field",
			template:      "{metrics.invalid_field}",
			expectedField: "metrics.invalid_field",
		},
		{
			name:          "unknown field among valid ones",
			template:      "{timestamp} {invalid} {url}",
			expectedField: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter, err := NewTemplateFormatter(tt.template)
			assert.Nil(t, formatter)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "unknown placeholder")
			assert.Contains(t, err.Error(), tt.expectedField)
		})
	}
}

func TestNewTemplateFormatter_MalformedPlaceholder(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		errContains string
	}{
		{
			name:        "unclosed brace",
			template:    "{url",
			errContains: "unclosed placeholder",
		},
		{
			name:        "empty placeholder",
			template:    "{}",
			errContains: "empty placeholder",
		},
		{
			name:        "unclosed brace with text after",
			template:    "{url some text",
			errContains: "unclosed placeholder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter, err := NewTemplateFormatter(tt.template)
			assert.Nil(t, formatter)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestNewTemplateFormatter_NestedPlaceholderParsing(t *testing.T) {
	formatter, err := NewTemplateFormatter("{metrics.total_requests}")
	require.NoError(t, err)
	require.NotNil(t, formatter)

	placeholders := formatter.Placeholders()
	require.Len(t, placeholders, 1)

	// Check field path is correctly parsed
	assert.Equal(t, []string{"metrics", "total_requests"}, placeholders[0].fieldPath)
	assert.Equal(t, "{metrics.total_requests}", placeholders[0].raw)
}

func TestNewTemplateFormatter_AllValidFields(t *testing.T) {
	// Test that all documented valid fields are actually valid
	validFieldsList := []string{
		"timestamp", "request_id", "host", "host_id", "url", "url_hash",
		"event_type", "dimension", "user_agent", "client_ip", "matched_rule",
		"status_code", "page_size", "serve_time", "source",
		"render_service_id", "render_time", "chrome_id",
		"title", "index_status", "cache_age", "cache_key",
		"error_type", "error_message", "eg_instance_id",
		"metrics.final_url", "metrics.total_requests", "metrics.total_bytes",
		"metrics.same_origin_requests", "metrics.same_origin_bytes",
		"metrics.third_party_requests", "metrics.third_party_bytes",
		"metrics.third_party_domains", "metrics.blocked_count",
		"metrics.failed_count", "metrics.timed_out", "metrics.console_messages",
		"metrics.error_count", "metrics.warning_count",
		"metrics.time_to_first_request", "metrics.time_to_last_response",
	}

	for _, field := range validFieldsList {
		t.Run(field, func(t *testing.T) {
			template := "{" + field + "}"
			formatter, err := NewTemplateFormatter(template)
			require.NoError(t, err, "field %s should be valid", field)
			require.NotNil(t, formatter)
		})
	}
}

func TestNewTemplateFormatter_PlaceholderPositions(t *testing.T) {
	template := "Start {url} middle {status_code} end"
	formatter, err := NewTemplateFormatter(template)
	require.NoError(t, err)

	placeholders := formatter.Placeholders()
	require.Len(t, placeholders, 2)

	// First placeholder: {url} starts at position 6
	assert.Equal(t, 6, placeholders[0].start)
	assert.Equal(t, 11, placeholders[0].end)

	// Second placeholder: {status_code} starts at position 19
	assert.Equal(t, 19, placeholders[1].start)
	assert.Equal(t, 32, placeholders[1].end)
}

// Format method tests

func TestFormat_StringFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{host}")
	require.NoError(t, err)

	event := &RequestEvent{Host: "example.com"}
	result := formatter.Format(event)
	assert.Equal(t, `"example.com"`, result)
}

func TestFormat_StringWithQuotesEscaped(t *testing.T) {
	formatter, err := NewTemplateFormatter("{title}")
	require.NoError(t, err)

	event := &RequestEvent{PageSEO: &PageSEOEvent{Title: `Page "Title" Here`}}
	result := formatter.Format(event)
	assert.Equal(t, `"Page \"Title\" Here"`, result)
}

func TestFormat_StringWithSpecialCharsEscaped(t *testing.T) {
	formatter, err := NewTemplateFormatter("{title}")
	require.NoError(t, err)

	event := &RequestEvent{PageSEO: &PageSEOEvent{Title: "Line1\nLine2\tTabbed\rReturn\\Backslash"}}
	result := formatter.Format(event)
	assert.Equal(t, `"Line1\nLine2\tTabbed\rReturn\\Backslash"`, result)
}

func TestFormat_EmptyStringBecomesDash(t *testing.T) {
	formatter, err := NewTemplateFormatter("{host}")
	require.NoError(t, err)

	event := &RequestEvent{Host: ""}
	result := formatter.Format(event)
	assert.Equal(t, "-", result)
}

func TestFormat_IntFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{status_code}")
	require.NoError(t, err)

	tests := []struct {
		name     string
		code     int
		expected string
	}{
		{"positive", 200, "200"},
		{"zero", 0, "0"},
		{"large", 50000, "50000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &RequestEvent{StatusCode: tt.code}
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormat_Int64Formatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{page_size}")
	require.NoError(t, err)

	event := &RequestEvent{PageSize: 1234567890}
	result := formatter.Format(event)
	assert.Equal(t, "1234567890", result)
}

func TestFormat_FloatFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{serve_time}")
	require.NoError(t, err)

	tests := []struct {
		name     string
		value    float64
		expected string
	}{
		{"three decimals", 1.234, "1.234"},
		{"truncated", 1.23456789, "1.235"},
		{"zero", 0.0, "0.000"},
		{"small", 0.001, "0.001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &RequestEvent{ServeTime: tt.value}
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormat_BoolFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{metrics.timed_out}")
	require.NoError(t, err)

	tests := []struct {
		name     string
		value    bool
		expected string
	}{
		{"true", true, "true"},
		{"false", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &RequestEvent{
				Metrics: &PageMetricsEvent{TimedOut: tt.value},
			}
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormat_TimeFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{timestamp}")
	require.NoError(t, err)

	// Create a specific time
	ts := time.Date(2024, 1, 5, 14, 30, 22, 123000000, time.UTC)
	event := &RequestEvent{CreatedAt: ts}
	result := formatter.Format(event)
	assert.Equal(t, "2024-01-05T14:30:22.123Z", result)
}

func TestFormat_TimeFormattingNonUTC(t *testing.T) {
	formatter, err := NewTemplateFormatter("{timestamp}")
	require.NoError(t, err)

	// Create time in different timezone - should be converted to UTC
	loc, _ := time.LoadLocation("America/New_York")
	ts := time.Date(2024, 1, 5, 9, 30, 22, 123000000, loc)
	event := &RequestEvent{CreatedAt: ts}
	result := formatter.Format(event)
	assert.Equal(t, "2024-01-05T14:30:22.123Z", result)
}

func TestFormat_ConsoleMessagesFormatting(t *testing.T) {
	formatter, err := NewTemplateFormatter("{metrics.console_messages}")
	require.NoError(t, err)

	tests := []struct {
		name     string
		messages []types.ConsoleError
		expected string
	}{
		{
			name: "multiple messages",
			messages: []types.ConsoleError{
				{Type: "error", SourceURL: "script.js", SourceLocation: "10:5", Message: "error1"},
				{Type: "warning", SourceURL: "other.js", SourceLocation: "20:1", Message: "warn1"},
			},
			expected: `[{"type":"error","source_url":"script.js","source_location":"10:5","message":"error1"},{"type":"warning","source_url":"other.js","source_location":"20:1","message":"warn1"}]`,
		},
		{
			name: "single message",
			messages: []types.ConsoleError{
				{Type: "error", SourceURL: "script.js", SourceLocation: "5:1", Message: "single error"},
			},
			expected: `[{"type":"error","source_url":"script.js","source_location":"5:1","message":"single error"}]`,
		},
		{
			name: "message with quotes and special chars",
			messages: []types.ConsoleError{
				{Type: "error", SourceURL: "app.js", SourceLocation: "15:3", Message: `Can't read property "foo" of undefined`},
			},
			expected: `[{"type":"error","source_url":"app.js","source_location":"15:3","message":"Can't read property \"foo\" of undefined"}]`,
		},
		{"empty slice", []types.ConsoleError{}, "-"},
		{"nil slice", nil, "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &RequestEvent{
				Metrics: &PageMetricsEvent{ConsoleMessages: tt.messages},
			}
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormat_NilMetricsRendersDash(t *testing.T) {
	formatter, err := NewTemplateFormatter("{metrics.total_requests}")
	require.NoError(t, err)

	event := &RequestEvent{Metrics: nil}
	result := formatter.Format(event)
	assert.Equal(t, "-", result)
}

func TestFormat_MetricsWithZeroValues(t *testing.T) {
	formatter, err := NewTemplateFormatter("{metrics.total_requests} {metrics.total_bytes}")
	require.NoError(t, err)

	event := &RequestEvent{
		Metrics: &PageMetricsEvent{
			TotalRequests: 0,
			TotalBytes:    0,
		},
	}
	result := formatter.Format(event)
	assert.Equal(t, "0 0", result)
}

func TestFormat_FullTemplateMultiplePlaceholders(t *testing.T) {
	template := "{timestamp} {host} {url} {status_code} {event_type} {source} {serve_time}"
	formatter, err := NewTemplateFormatter(template)
	require.NoError(t, err)

	ts := time.Date(2024, 1, 5, 14, 30, 22, 123000000, time.UTC)
	event := &RequestEvent{
		CreatedAt:  ts,
		Host:       "example.com",
		URL:        "https://example.com/page",
		StatusCode: 200,
		EventType:  "render",
		Source:     "render",
		ServeTime:  1.234,
	}

	result := formatter.Format(event)
	expected := `2024-01-05T14:30:22.123Z "example.com" "https://example.com/page" 200 "render" "render" 1.234`
	assert.Equal(t, expected, result)
}

func TestFormat_StaticTextOnly(t *testing.T) {
	formatter, err := NewTemplateFormatter("Static text without placeholders")
	require.NoError(t, err)

	event := &RequestEvent{Host: "example.com"}
	result := formatter.Format(event)
	assert.Equal(t, "Static text without placeholders", result)
}

func TestFormat_MixedStaticAndPlaceholders(t *testing.T) {
	formatter, err := NewTemplateFormatter("Host: {host} Status: {status_code}")
	require.NoError(t, err)

	event := &RequestEvent{
		Host:       "example.com",
		StatusCode: 200,
	}
	result := formatter.Format(event)
	assert.Equal(t, `Host: "example.com" Status: 200`, result)
}

func TestFormat_AllTopLevelFields(t *testing.T) {
	ts := time.Date(2024, 1, 5, 14, 30, 22, 123000000, time.UTC)
	event := &RequestEvent{
		RequestID:       "req-123",
		Host:            "example.com",
		HostID:          1,
		URL:             "https://example.com/page",
		URLHash:         "abc123",
		EventType:       "render",
		Dimension:       "desktop",
		UserAgent:       "Mozilla/5.0",
		ClientIP:        "203.0.113.50",
		MatchedRule:     "/page/*",
		StatusCode:      200,
		PageSize:        15000,
		ServeTime:       1.234,
		Source:          "render",
		RenderServiceID: "rs-1",
		RenderTime:      0.856,
		ChromeID:        "chrome-1",
		PageSEO: &PageSEOEvent{
			Title:       "Example Page",
			IndexStatus: int(types.IndexStatusIndexable),
		},
		CacheAge:     3600,
		CacheKey:     "cache:1:1:abc123",
		ErrorType:    "none",
		ErrorMessage: "no error",
		CreatedAt:    ts,
		EGInstanceID: "eg-1",
	}

	tests := []struct {
		field    string
		expected string
	}{
		{"request_id", `"req-123"`},
		{"host", `"example.com"`},
		{"host_id", "1"},
		{"url", `"https://example.com/page"`},
		{"url_hash", `"abc123"`},
		{"event_type", `"render"`},
		{"dimension", `"desktop"`},
		{"user_agent", `"Mozilla/5.0"`},
		{"client_ip", `"203.0.113.50"`},
		{"matched_rule", `"/page/*"`},
		{"status_code", "200"},
		{"page_size", "15000"},
		{"serve_time", "1.234"},
		{"source", `"render"`},
		{"render_service_id", `"rs-1"`},
		{"render_time", "0.856"},
		{"chrome_id", `"chrome-1"`},
		{"title", `"Example Page"`},
		{"index_status", "1"},
		{"cache_age", "3600"},
		{"cache_key", `"cache:1:1:abc123"`},
		{"error_type", `"none"`},
		{"error_message", `"no error"`},
		{"timestamp", "2024-01-05T14:30:22.123Z"},
		{"eg_instance_id", `"eg-1"`},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			formatter, err := NewTemplateFormatter("{" + tt.field + "}")
			require.NoError(t, err)
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormat_AllMetricsFields(t *testing.T) {
	event := &RequestEvent{
		Metrics: &PageMetricsEvent{
			FinalURL:           "https://example.com/final",
			TotalRequests:      25,
			TotalBytes:         150000,
			SameOriginRequests: 15,
			SameOriginBytes:    100000,
			ThirdPartyRequests: 10,
			ThirdPartyBytes:    50000,
			ThirdPartyDomains:  3,
			BlockedCount:       2,
			FailedCount:        1,
			TimedOut:           false,
			ConsoleMessages:    []types.ConsoleError{{Type: "error", SourceURL: "test.js", SourceLocation: "1:1", Message: "error1"}},
			ErrorCount:         1,
			WarningCount:       2,
			TimeToFirstRequest: 0.05,
			TimeToLastResponse: 1.5,
		},
	}

	tests := []struct {
		field    string
		expected string
	}{
		{"metrics.final_url", `"https://example.com/final"`},
		{"metrics.total_requests", "25"},
		{"metrics.total_bytes", "150000"},
		{"metrics.same_origin_requests", "15"},
		{"metrics.same_origin_bytes", "100000"},
		{"metrics.third_party_requests", "10"},
		{"metrics.third_party_bytes", "50000"},
		{"metrics.third_party_domains", "3"},
		{"metrics.blocked_count", "2"},
		{"metrics.failed_count", "1"},
		{"metrics.timed_out", "false"},
		{"metrics.console_messages", `[{"type":"error","source_url":"test.js","source_location":"1:1","message":"error1"}]`},
		{"metrics.error_count", "1"},
		{"metrics.warning_count", "2"},
		{"metrics.time_to_first_request", "0.050"},
		{"metrics.time_to_last_response", "1.500"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			formatter, err := NewTemplateFormatter("{" + tt.field + "}")
			require.NoError(t, err)
			result := formatter.Format(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

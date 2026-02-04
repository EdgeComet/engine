package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/pkg/types"
)

func TestRequestEvent_Instantiation(t *testing.T) {
	now := time.Now()

	event := &RequestEvent{
		RequestID:       "req-123",
		Host:            "example.com",
		HostID:          1,
		URL:             "https://example.com/page",
		URLHash:         "abc123",
		EventType:       "render",
		Dimension:       "desktop",
		UserAgent:       "Mozilla/5.0",
		MatchedRule:     "/page/*",
		StatusCode:      200,
		PageSize:        15000,
		ServeTime:       1.234,
		Source:          "render",
		RenderServiceID: "rs-1",
		RenderTime:      0.856,
		ChromeID:        "chrome-1",
		CacheAge:        0,
		CacheKey:        "cache:1:1:abc123",
		ErrorType:       "",
		ErrorMessage:    "",
		Metrics:         nil,
		PageSEO: &PageSEOEvent{
			Title:       "Example Page",
			IndexStatus: int(types.IndexStatusIndexable),
		},
		CreatedAt:    now,
		EGInstanceID: "eg-1",
	}

	assert.Equal(t, "req-123", event.RequestID)
	assert.Equal(t, "example.com", event.Host)
	assert.Equal(t, 1, event.HostID)
	assert.Equal(t, "https://example.com/page", event.URL)
	assert.Equal(t, "abc123", event.URLHash)
	assert.Equal(t, "render", event.EventType)
	assert.Equal(t, "desktop", event.Dimension)
	assert.Equal(t, "Mozilla/5.0", event.UserAgent)
	assert.Equal(t, "/page/*", event.MatchedRule)
	assert.Equal(t, 200, event.StatusCode)
	assert.Equal(t, int64(15000), event.PageSize)
	assert.Equal(t, 1.234, event.ServeTime)
	assert.Equal(t, "render", event.Source)
	assert.Equal(t, "rs-1", event.RenderServiceID)
	assert.Equal(t, 0.856, event.RenderTime)
	assert.Equal(t, "chrome-1", event.ChromeID)
	assert.NotNil(t, event.PageSEO)
	assert.Equal(t, "Example Page", event.PageSEO.Title)
	assert.Equal(t, int(types.IndexStatusIndexable), event.PageSEO.IndexStatus)
	assert.Equal(t, 0, event.CacheAge)
	assert.Equal(t, "cache:1:1:abc123", event.CacheKey)
	assert.Empty(t, event.ErrorType)
	assert.Empty(t, event.ErrorMessage)
	assert.Nil(t, event.Metrics)
	assert.Equal(t, now, event.CreatedAt)
	assert.Equal(t, "eg-1", event.EGInstanceID)
}

func TestPageMetricsEvent_Instantiation(t *testing.T) {
	consoleMessages := []types.ConsoleError{
		{Type: "error", SourceURL: "script.js", SourceLocation: "10:5", Message: "error1"},
		{Type: "warning", SourceURL: "other.js", SourceLocation: "20:1", Message: "warn1"},
	}
	metrics := &PageMetricsEvent{
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
		ConsoleMessages:    consoleMessages,
		ErrorCount:         1,
		WarningCount:       1,
		TimeToFirstRequest: 0.05,
		TimeToLastResponse: 1.5,
	}

	assert.Equal(t, "https://example.com/final", metrics.FinalURL)
	assert.Equal(t, 25, metrics.TotalRequests)
	assert.Equal(t, int64(150000), metrics.TotalBytes)
	assert.Equal(t, 15, metrics.SameOriginRequests)
	assert.Equal(t, int64(100000), metrics.SameOriginBytes)
	assert.Equal(t, 10, metrics.ThirdPartyRequests)
	assert.Equal(t, int64(50000), metrics.ThirdPartyBytes)
	assert.Equal(t, 3, metrics.ThirdPartyDomains)
	assert.Equal(t, 2, metrics.BlockedCount)
	assert.Equal(t, 1, metrics.FailedCount)
	assert.False(t, metrics.TimedOut)
	assert.Equal(t, consoleMessages, metrics.ConsoleMessages)
	assert.Equal(t, 1, metrics.ErrorCount)
	assert.Equal(t, 1, metrics.WarningCount)
	assert.Equal(t, 0.05, metrics.TimeToFirstRequest)
	assert.Equal(t, 1.5, metrics.TimeToLastResponse)
}

func TestRequestEvent_WithMetrics(t *testing.T) {
	metrics := &PageMetricsEvent{
		FinalURL:      "https://example.com/final",
		TotalRequests: 10,
		TotalBytes:    50000,
	}

	event := &RequestEvent{
		RequestID: "req-456",
		EventType: "render",
		Metrics:   metrics,
	}

	assert.NotNil(t, event.Metrics)
	assert.Equal(t, "https://example.com/final", event.Metrics.FinalURL)
	assert.Equal(t, 10, event.Metrics.TotalRequests)
	assert.Equal(t, int64(50000), event.Metrics.TotalBytes)
}

func TestRequestEvent_CacheHitEvent(t *testing.T) {
	event := &RequestEvent{
		RequestID:  "req-789",
		Host:       "example.com",
		HostID:     1,
		URL:        "https://example.com/cached",
		EventType:  "cache_hit",
		Source:     "cache",
		StatusCode: 200,
		CacheAge:   3600,
		Metrics:    nil, // Cache hits don't have render metrics
	}

	assert.Equal(t, "cache_hit", event.EventType)
	assert.Equal(t, "cache", event.Source)
	assert.Equal(t, 3600, event.CacheAge)
	assert.Nil(t, event.Metrics)
}

func TestRequestEvent_ErrorEvent(t *testing.T) {
	event := &RequestEvent{
		RequestID:    "req-error",
		Host:         "example.com",
		EventType:    "error",
		StatusCode:   500,
		ErrorType:    "timeout",
		ErrorMessage: "render timeout exceeded",
	}

	assert.Equal(t, "error", event.EventType)
	assert.Equal(t, 500, event.StatusCode)
	assert.Equal(t, "timeout", event.ErrorType)
	assert.Equal(t, "render timeout exceeded", event.ErrorMessage)
}

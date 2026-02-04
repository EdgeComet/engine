package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

func TestBuildRequestEvent_CacheHit(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromCache,
		StatusCode:  200,
		BytesServed: 5000,
		CacheAge:    5 * time.Minute,
	}

	event := BuildRequestEvent(renderCtx, result, 100*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeCacheHit, event.EventType)
	assert.Equal(t, SourceCache, event.Source)
	assert.Equal(t, 200, event.StatusCode)
	assert.Equal(t, int64(5000), event.PageSize)
	assert.Equal(t, 300, event.CacheAge) // 5 minutes = 300 seconds
	assert.Nil(t, event.PageSEO)         // No PageSEO for cache hits
	assert.Nil(t, event.Metrics)         // No metrics for cache hits
	assert.Equal(t, "eg-1", event.EGInstanceID)
}

func TestBuildRequestEvent_Render(t *testing.T) {
	renderCtx := createTestRenderContext()
	pageMetrics := &types.PageMetrics{
		FinalURL:           "https://example.com/page",
		TotalRequests:      50,
		TotalBytes:         100000,
		SameOriginRequests: 30,
		SameOriginBytes:    60000,
		ThirdPartyRequests: 20,
		ThirdPartyBytes:    40000,
		ThirdPartyDomains:  5,
		BlockedCount:       2,
		FailedCount:        1,
		TimedOut:           false,
		ConsoleMessages: []types.ConsoleError{
			{Type: types.ConsoleTypeWarning, Message: "slow resource"},
		},
		TimeToFirstRequest: 0.05,
		TimeToLastResponse: 2.5,
	}

	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromRender,
		StatusCode:  200,
		BytesServed: 10000,
		ServiceID:   "rs-1",
		ChromeID:    "chrome-abc",
		RenderTime:  2 * time.Second,
		Metrics:     pageMetrics,
		PageSEO: &types.PageSEO{
			Title:       "Rendered Page",
			IndexStatus: types.IndexStatusIndexable,
		},
	}

	event := BuildRequestEvent(renderCtx, result, 2500*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeRender, event.EventType)
	assert.Equal(t, SourceRender, event.Source)
	assert.Equal(t, "rs-1", event.RenderServiceID)
	assert.Equal(t, "chrome-abc", event.ChromeID)
	assert.Equal(t, 2.0, event.RenderTime)
	require.NotNil(t, event.PageSEO)
	assert.Equal(t, "Rendered Page", event.PageSEO.Title)

	// Verify metrics are populated
	require.NotNil(t, event.Metrics)
	assert.Equal(t, "https://example.com/page", event.Metrics.FinalURL)
	assert.Equal(t, 50, event.Metrics.TotalRequests)
	assert.Equal(t, int64(100000), event.Metrics.TotalBytes)
	assert.Equal(t, 30, event.Metrics.SameOriginRequests)
	assert.Equal(t, 5, event.Metrics.ThirdPartyDomains)
	assert.Equal(t, 2, event.Metrics.BlockedCount)
	assert.Equal(t, 1, event.Metrics.FailedCount)
	assert.False(t, event.Metrics.TimedOut)
	require.Len(t, event.Metrics.ConsoleMessages, 1)
	assert.Equal(t, types.ConsoleTypeWarning, event.Metrics.ConsoleMessages[0].Type)
	assert.Equal(t, "slow resource", event.Metrics.ConsoleMessages[0].Message)
	assert.Equal(t, 0, event.Metrics.ErrorCount)
	assert.Equal(t, 1, event.Metrics.WarningCount)
}

func TestBuildRequestEvent_Bypass(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromBypass,
		StatusCode:  200,
		BytesServed: 3000,
	}

	event := BuildRequestEvent(renderCtx, result, 50*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeBypass, event.EventType)
	assert.Equal(t, SourceBypass, event.Source)
	assert.Equal(t, 200, event.StatusCode)
	assert.Nil(t, event.Metrics)
}

func TestBuildRequestEvent_BypassWithSEO(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromBypass,
		StatusCode:  200,
		BytesServed: 5000,
		PageSEO: &types.PageSEO{
			Title:           "Bypass SEO Page",
			IndexStatus:     types.IndexStatusIndexable,
			MetaDescription: "A bypass page with SEO data",
			H1s:             []string{"Main Heading"},
		},
	}

	event := BuildRequestEvent(renderCtx, result, 80*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeBypass, event.EventType)
	assert.Equal(t, SourceBypass, event.Source)
	assert.Equal(t, 200, event.StatusCode)
	require.NotNil(t, event.PageSEO)
	assert.Equal(t, "Bypass SEO Page", event.PageSEO.Title)
	assert.Equal(t, int(types.IndexStatusIndexable), event.PageSEO.IndexStatus)
	assert.Equal(t, "A bypass page with SEO data", event.PageSEO.MetaDescription)
	require.Len(t, event.PageSEO.H1s, 1)
	assert.Equal(t, "Main Heading", event.PageSEO.H1s[0])
	assert.Nil(t, event.Metrics)
}

func TestBuildRequestEvent_BypassCache(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromBypassCache,
		StatusCode:  200,
		BytesServed: 4000,
		CacheAge:    10 * time.Minute,
	}

	event := BuildRequestEvent(renderCtx, result, 30*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeBypassCache, event.EventType)
	assert.Equal(t, SourceBypassCache, event.Source)
	assert.Equal(t, 600, event.CacheAge) // 10 minutes = 600 seconds
}

func TestBuildRequestEvent_PrecacheOverride(t *testing.T) {
	renderCtx := createTestRenderContext()
	renderCtx.IsPrecache = true

	result := &orchestrator.RenderResult{
		Source:      orchestrator.ServedFromRender,
		StatusCode:  200,
		BytesServed: 8000,
	}

	event := BuildRequestEvent(renderCtx, result, 3*time.Second, "eg-1")

	// Should override to precache even though source is render
	assert.Equal(t, EventTypePrecache, event.EventType)
	assert.Equal(t, SourceRender, event.Source)
}

func TestBuildRequestEvent_MatchedRule(t *testing.T) {
	renderCtx := createTestRenderContext()
	renderCtx.ResolvedConfig = &config.ResolvedConfig{
		MatchedPattern: "/blog/*",
	}

	result := &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromRender,
		StatusCode: 200,
	}

	event := BuildRequestEvent(renderCtx, result, 100*time.Millisecond, "eg-1")

	assert.Equal(t, "/blog/*", event.MatchedRule)
}

func TestBuildRequestEvent_ContextFields(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromCache,
		StatusCode: 200,
	}

	event := BuildRequestEvent(renderCtx, result, 50*time.Millisecond, "eg-instance-123")

	assert.Equal(t, "req-123", event.RequestID)
	assert.Equal(t, "example.com", event.Host)
	assert.Equal(t, 1, event.HostID)
	assert.Equal(t, "https://example.com/page", event.URL)
	assert.Equal(t, "abc123", event.URLHash)
	assert.Equal(t, "desktop", event.Dimension)
	assert.Equal(t, "Googlebot/2.1", event.UserAgent)
	assert.Equal(t, "", event.ClientIP)
	assert.Equal(t, "cache:1:1:abc123", event.CacheKey)
	assert.Equal(t, "eg-instance-123", event.EGInstanceID)
	assert.InDelta(t, 0.05, event.ServeTime, 0.001)
}

func TestBuildErrorEvent(t *testing.T) {
	event := BuildErrorEvent(
		"req-456",
		"example.com",
		1,
		"https://example.com/admin",
		"Googlebot/2.1",
		"",
		"auth",
		"invalid API key",
		403,
		"eg-1",
	)

	assert.Equal(t, "req-456", event.RequestID)
	assert.Equal(t, "example.com", event.Host)
	assert.Equal(t, 1, event.HostID)
	assert.Equal(t, "https://example.com/admin", event.URL)
	assert.Equal(t, "Googlebot/2.1", event.UserAgent)
	assert.Equal(t, "", event.ClientIP)
	assert.Equal(t, EventTypeError, event.EventType)
	assert.Equal(t, 403, event.StatusCode)
	assert.Equal(t, "auth", event.ErrorType)
	assert.Equal(t, "invalid API key", event.ErrorMessage)
	assert.Equal(t, "eg-1", event.EGInstanceID)
	assert.False(t, event.CreatedAt.IsZero())
}

func TestBuildErrorEventWithClientIP(t *testing.T) {
	event := BuildErrorEvent(
		"req-789",
		"example.com",
		1,
		"https://example.com/page",
		"Googlebot/2.1",
		"203.0.113.50",
		"auth",
		"invalid API key",
		403,
		"eg-1",
	)

	assert.Equal(t, "203.0.113.50", event.ClientIP)
}

func TestBuildRequestEvent_NilContext(t *testing.T) {
	result := &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromBypass,
		StatusCode: 200,
	}

	event := BuildRequestEvent(nil, result, 100*time.Millisecond, "eg-1")

	assert.Equal(t, EventTypeBypass, event.EventType)
	assert.Equal(t, "", event.RequestID)
	assert.Equal(t, "", event.Host)
}

func TestBuildRequestEvent_NilResult(t *testing.T) {
	renderCtx := createTestRenderContext()

	event := BuildRequestEvent(renderCtx, nil, 100*time.Millisecond, "eg-1")

	assert.Equal(t, "req-123", event.RequestID)
	assert.Equal(t, "example.com", event.Host)
	assert.Equal(t, "", event.EventType)
	assert.Equal(t, 0, event.StatusCode)
}

func TestConvertPageMetrics(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:           "https://example.com/final",
		TotalRequests:      100,
		TotalBytes:         500000,
		SameOriginRequests: 60,
		SameOriginBytes:    300000,
		ThirdPartyRequests: 40,
		ThirdPartyBytes:    200000,
		ThirdPartyDomains:  10,
		BlockedCount:       5,
		FailedCount:        2,
		TimedOut:           true,
		ConsoleMessages: []types.ConsoleError{
			{Type: types.ConsoleTypeError, Message: "timeout"},
			{Type: types.ConsoleTypeWarning, Message: "blocked"},
		},
		TimeToFirstRequest: 0.1,
		TimeToLastResponse: 5.5,
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	assert.Equal(t, "https://example.com/final", result.FinalURL)
	assert.Equal(t, 100, result.TotalRequests)
	assert.Equal(t, int64(500000), result.TotalBytes)
	assert.Equal(t, 60, result.SameOriginRequests)
	assert.Equal(t, int64(300000), result.SameOriginBytes)
	assert.Equal(t, 40, result.ThirdPartyRequests)
	assert.Equal(t, int64(200000), result.ThirdPartyBytes)
	assert.Equal(t, 10, result.ThirdPartyDomains)
	assert.Equal(t, 5, result.BlockedCount)
	assert.Equal(t, 2, result.FailedCount)
	assert.True(t, result.TimedOut)
	require.Len(t, result.ConsoleMessages, 2)
	assert.Equal(t, types.ConsoleTypeError, result.ConsoleMessages[0].Type)
	assert.Equal(t, types.ConsoleTypeWarning, result.ConsoleMessages[1].Type)
	assert.Equal(t, 1, result.ErrorCount)
	assert.Equal(t, 1, result.WarningCount)
	assert.Equal(t, 0.1, result.TimeToFirstRequest)
	assert.Equal(t, 5.5, result.TimeToLastResponse)
}

func TestConvertPageMetrics_Nil(t *testing.T) {
	result := convertPageMetrics(nil)
	assert.Nil(t, result)
}

func TestConvertPageMetrics_WithLifecycleEvents(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:      "https://example.com/page",
		TotalRequests: 10,
		LifecycleEvents: []types.LifecycleEvent{
			{Name: "DOMContentLoaded", Time: 0.5},
			{Name: "load", Time: 1.2},
			{Name: "networkIdle", Time: 2.0},
		},
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	require.Len(t, result.LifecycleEvents, 3)
	assert.Equal(t, "DOMContentLoaded", result.LifecycleEvents[0].Name)
	assert.Equal(t, 0.5, result.LifecycleEvents[0].Time)
	assert.Equal(t, "load", result.LifecycleEvents[1].Name)
	assert.Equal(t, 1.2, result.LifecycleEvents[1].Time)
	assert.Equal(t, "networkIdle", result.LifecycleEvents[2].Name)
	assert.Equal(t, 2.0, result.LifecycleEvents[2].Time)
}

func TestConvertPageMetrics_WithStatusCounts(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:      "https://example.com/page",
		TotalRequests: 50,
		StatusCounts: map[string]int64{
			types.StatusClass2xx: 45,
			types.StatusClass3xx: 3,
			types.StatusClass4xx: 2,
		},
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	require.NotNil(t, result.StatusCounts)
	assert.Equal(t, int64(45), result.StatusCounts[types.StatusClass2xx])
	assert.Equal(t, int64(3), result.StatusCounts[types.StatusClass3xx])
	assert.Equal(t, int64(2), result.StatusCounts[types.StatusClass4xx])
}

func TestConvertPageMetrics_WithBytesByType(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:   "https://example.com/page",
		TotalBytes: 500000,
		BytesByType: map[string]int64{
			types.ResourceTypeDocument:   50000,
			types.ResourceTypeScript:     150000,
			types.ResourceTypeStylesheet: 80000,
			types.ResourceTypeImage:      200000,
			types.ResourceTypeFont:       20000,
		},
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	require.NotNil(t, result.BytesByType)
	assert.Equal(t, int64(50000), result.BytesByType[types.ResourceTypeDocument])
	assert.Equal(t, int64(150000), result.BytesByType[types.ResourceTypeScript])
	assert.Equal(t, int64(80000), result.BytesByType[types.ResourceTypeStylesheet])
	assert.Equal(t, int64(200000), result.BytesByType[types.ResourceTypeImage])
	assert.Equal(t, int64(20000), result.BytesByType[types.ResourceTypeFont])
}

func TestConvertPageMetrics_NilMaps(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:        "https://example.com/page",
		TotalRequests:   10,
		TotalBytes:      5000,
		LifecycleEvents: nil,
		StatusCounts:    nil,
		BytesByType:     nil,
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	assert.Equal(t, "https://example.com/page", result.FinalURL)
	assert.Equal(t, 10, result.TotalRequests)
	assert.Equal(t, int64(5000), result.TotalBytes)
	// Verify nil maps stay nil, not empty
	assert.Nil(t, result.LifecycleEvents)
	assert.Nil(t, result.StatusCounts)
	assert.Nil(t, result.BytesByType)
	assert.Nil(t, result.DomainStats)
}

func TestConvertPageMetrics_WithDomainStats(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:      "https://example.com/page",
		TotalRequests: 50,
		DomainStats: map[string]*types.DomainStats{
			"example.com": {
				Requests:   30,
				Bytes:      150000,
				Failed:     1,
				Blocked:    0,
				AvgLatency: 0.25,
			},
			"cdn.example.com": {
				Requests:   15,
				Bytes:      80000,
				Failed:     0,
				Blocked:    2,
				AvgLatency: 0.12,
			},
			"api.thirdparty.io": {
				Requests:   5,
				Bytes:      5000,
				Failed:     2,
				Blocked:    0,
				AvgLatency: 0.85,
			},
		},
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	require.NotNil(t, result.DomainStats)
	require.Len(t, result.DomainStats, 3)

	// Verify example.com stats
	exampleStats := result.DomainStats["example.com"]
	require.NotNil(t, exampleStats)
	assert.Equal(t, 30, exampleStats.Requests)
	assert.Equal(t, int64(150000), exampleStats.Bytes)
	assert.Equal(t, 1, exampleStats.Failed)
	assert.Equal(t, 0, exampleStats.Blocked)
	assert.InDelta(t, 0.25, exampleStats.AvgLatency, 0.001)

	// Verify cdn.example.com stats
	cdnStats := result.DomainStats["cdn.example.com"]
	require.NotNil(t, cdnStats)
	assert.Equal(t, 15, cdnStats.Requests)
	assert.Equal(t, int64(80000), cdnStats.Bytes)
	assert.Equal(t, 0, cdnStats.Failed)
	assert.Equal(t, 2, cdnStats.Blocked)
	assert.InDelta(t, 0.12, cdnStats.AvgLatency, 0.001)

	// Verify api.thirdparty.io stats
	apiStats := result.DomainStats["api.thirdparty.io"]
	require.NotNil(t, apiStats)
	assert.Equal(t, 5, apiStats.Requests)
	assert.Equal(t, int64(5000), apiStats.Bytes)
	assert.Equal(t, 2, apiStats.Failed)
	assert.Equal(t, 0, apiStats.Blocked)
	assert.InDelta(t, 0.85, apiStats.AvgLatency, 0.001)
}

func TestConvertPageMetrics_EmptyDomainStats(t *testing.T) {
	metrics := &types.PageMetrics{
		FinalURL:      "https://example.com/page",
		TotalRequests: 10,
		DomainStats:   map[string]*types.DomainStats{},
	}

	result := convertPageMetrics(metrics)

	require.NotNil(t, result)
	// Empty map should remain nil (len check in convertPageMetrics)
	assert.Nil(t, result.DomainStats)
}

func TestConvertPageMetrics_ConsoleMessages(t *testing.T) {
	tests := []struct {
		name            string
		consoleMessages []types.ConsoleError
		expectedErrors  int
		expectedWarns   int
	}{
		{
			name:            "empty messages",
			consoleMessages: nil,
			expectedErrors:  0,
			expectedWarns:   0,
		},
		{
			name: "errors only",
			consoleMessages: []types.ConsoleError{
				{Type: types.ConsoleTypeError, Message: "err1"},
				{Type: types.ConsoleTypeError, Message: "err2"},
			},
			expectedErrors: 2,
			expectedWarns:  0,
		},
		{
			name: "warnings only",
			consoleMessages: []types.ConsoleError{
				{Type: types.ConsoleTypeWarning, Message: "warn1"},
			},
			expectedErrors: 0,
			expectedWarns:  1,
		},
		{
			name: "mixed errors and warnings",
			consoleMessages: []types.ConsoleError{
				{Type: types.ConsoleTypeError, Message: "err1"},
				{Type: types.ConsoleTypeWarning, Message: "warn1"},
				{Type: types.ConsoleTypeError, Message: "err2"},
				{Type: types.ConsoleTypeWarning, Message: "warn2"},
			},
			expectedErrors: 2,
			expectedWarns:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &types.PageMetrics{
				ConsoleMessages: tt.consoleMessages,
			}

			result := convertPageMetrics(metrics)

			assert.Equal(t, tt.expectedErrors, result.ErrorCount)
			assert.Equal(t, tt.expectedWarns, result.WarningCount)
			assert.Equal(t, tt.consoleMessages, result.ConsoleMessages)
		})
	}
}

func TestCountConsoleType(t *testing.T) {
	messages := []types.ConsoleError{
		{Type: types.ConsoleTypeError},
		{Type: types.ConsoleTypeWarning},
		{Type: types.ConsoleTypeError},
	}

	assert.Equal(t, 2, countConsoleType(messages, types.ConsoleTypeError))
	assert.Equal(t, 1, countConsoleType(messages, types.ConsoleTypeWarning))
	assert.Equal(t, 0, countConsoleType(nil, types.ConsoleTypeError))
}

// createTestRenderContext creates a minimal RenderContext for testing
func createTestRenderContext() *edgectx.RenderContext {
	// Create a mock fasthttp context
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetUserAgent("Googlebot/2.1")

	return &edgectx.RenderContext{
		RequestID: "req-123",
		HTTPCtx:   ctx,
		TargetURL: "https://example.com/page",
		URLHash:   "abc123",
		Host: &types.Host{
			Domain: "example.com",
			ID:     1,
		},
		Dimension: "desktop",
		CacheKey: &types.CacheKey{
			HostID:      1,
			DimensionID: 1,
			URLHash:     "abc123",
		},
	}
}

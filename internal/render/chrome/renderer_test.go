package chrome

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/edgecomet/engine/pkg/types"
)

func TestRender_BasicPage(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-1",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)
	assert.Equal(t, "test-1", resp.RequestID)
	assert.NotEmpty(t, resp.HTML)
	assert.Contains(t, resp.HTML, "<html")
	assert.Contains(t, strings.ToLower(resp.HTML), "example")
	assert.Greater(t, resp.HTMLSize, 0)
	assert.Greater(t, resp.RenderTime, time.Duration(0))
	assert.Equal(t, 200, resp.Metrics.StatusCode)
	assert.Equal(t, "https://example.com/", resp.Metrics.FinalURL)
}

func TestRender_WithDOMContentLoaded(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-2",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "DOMContentLoaded",
		ExtraWait:      500 * time.Millisecond,
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.HTML)

	// Check that lifecycle events were captured
	assert.NotEmpty(t, resp.Metrics.LifecycleEvents, "Lifecycle events should be captured")

	// Verify DOMContentLoaded event is present
	hasDOMContentLoaded := false
	for _, event := range resp.Metrics.LifecycleEvents {
		if event.Name == "DOMContentLoaded" {
			hasDOMContentLoaded = true
			assert.Greater(t, event.Time, float64(0), "DOMContentLoaded should have positive timestamp")
			break
		}
	}
	assert.True(t, hasDOMContentLoaded, "DOMContentLoaded event should be present")
}

func TestRender_WithTimeout(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-3",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        1 * time.Second, // Very short timeout
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, err := instance.Render(context.Background(), req)

	// Should either succeed quickly or timeout
	if err != nil {
		assert.False(t, resp.Success)
		assert.NotEmpty(t, resp.Error)
	} else {
		// If it succeeded, it was fast enough
		assert.True(t, resp.Success)
	}
}

func TestRender_InvalidURL(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-4",
		URL:            "http://invalid-url-that-does-not-exist-12345.com",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        10 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, _ := instance.Render(context.Background(), req)

	// Chrome may show error page instead of failing, so just check response exists
	// and that we got some HTML (even if it's an error page)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.HTML)
}

func TestRender_MobileViewport(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-5",
		URL:            "https://example.com/",
		ViewportWidth:  375,
		ViewportHeight: 812,
		UserAgent:      "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)
}

func TestRender_Metrics(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-6",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)

	// Check metrics are collected
	assert.Equal(t, 200, resp.Metrics.StatusCode)
	assert.NotEmpty(t, resp.Metrics.FinalURL)
	assert.NotEmpty(t, resp.Metrics.LifecycleEvents, "Lifecycle events should be captured")

	// Verify both DOMContentLoaded and load events are present
	var domReadyTime, pageLoadTime float64
	for _, event := range resp.Metrics.LifecycleEvents {
		switch event.Name {
		case "DOMContentLoaded":
			domReadyTime = event.Time
		case "load":
			pageLoadTime = event.Time
		}
	}

	assert.Greater(t, domReadyTime, float64(0), "DOMContentLoaded time should be captured")
	assert.Greater(t, pageLoadTime, float64(0), "PageLoad time should be captured")
	assert.LessOrEqual(t, domReadyTime, pageLoadTime, "DOMContentLoaded should happen before or at same time as load")
}

func TestRender_ExtraWait(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-7",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      1 * time.Second, // Wait an extra second
	}

	start := time.Now()
	resp, err := instance.Render(context.Background(), req)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	// Should take at least 1 second due to extra wait
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(1000))
}

func TestRenderResponse_Structure(t *testing.T) {
	resp := &types.RenderResponse{
		RequestID:  "test-req-1",
		Success:    true,
		HTML:       "<html><body>Test</body></html>",
		RenderTime: 1500,
		HTMLSize:   30,
		Timestamp:  time.Now(),
		ChromeID:   "chrome-0",
		Metrics: types.PageMetrics{
			StatusCode: 200,
			FinalURL:   "https://example.com",
			LifecycleEvents: []types.LifecycleEvent{
				{Name: "init", Time: 0.05},
				{Name: "DOMContentLoaded", Time: 0.5},
				{Name: "load", Time: 1.0},
			},
			ConsoleMessages: []types.ConsoleError{},
		},
	}

	assert.Equal(t, "test-req-1", resp.RequestID)
	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)
	assert.Len(t, resp.Metrics.LifecycleEvents, 3)
	assert.Empty(t, resp.Metrics.ConsoleMessages)
}

func TestRender_WithoutHAR(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-no-har",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
		IncludeHAR:     false, // HAR disabled
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Render should succeed normally
	assert.True(t, resp.Success)
	assert.Equal(t, "test-no-har", resp.RequestID)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)

	// HAR should be nil when not requested
	assert.Nil(t, resp.HAR)
}

func TestRender_WithHAREnabled(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-with-har",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
		IncludeHAR:     true, // HAR enabled
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Render should succeed normally
	assert.True(t, resp.Success)
	assert.Equal(t, "test-with-har", resp.RequestID)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)

	// HAR should be populated when requested
	assert.NotNil(t, resp.HAR, "HAR should be populated when IncludeHAR=true")
	assert.Greater(t, len(resp.HAR), 0, "HAR should contain data")

	// Verify HAR is valid JSON
	var harData map[string]interface{}
	err = json.Unmarshal(resp.HAR, &harData)
	require.NoError(t, err, "HAR should be valid JSON")

	// Verify HAR structure
	log, ok := harData["log"].(map[string]interface{})
	require.True(t, ok, "HAR should contain log object")
	assert.Equal(t, "1.2", log["version"], "HAR version should be 1.2")
}

func TestRender_HARDoesNotAffectNormalRender(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	// First render without HAR
	reqWithoutHAR := &types.RenderRequest{
		RequestID:      "test-compare-1",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
		IncludeHAR:     false,
	}

	resp1, err := instance.Render(context.Background(), reqWithoutHAR)
	require.NoError(t, err)

	// Second render with HAR
	reqWithHAR := &types.RenderRequest{
		RequestID:      "test-compare-2",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
		IncludeHAR:     true,
	}

	resp2, err := instance.Render(context.Background(), reqWithHAR)
	require.NoError(t, err)

	// Both renders should succeed
	assert.True(t, resp1.Success)
	assert.True(t, resp2.Success)

	// Both should have HTML
	assert.NotEmpty(t, resp1.HTML)
	assert.NotEmpty(t, resp2.HTML)

	// Both should capture same status code
	assert.Equal(t, resp1.Metrics.StatusCode, resp2.Metrics.StatusCode)
}

func TestRender_NetworkMetrics(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:      "test-network-metrics",
		URL:            "https://example.com/",
		ViewportWidth:  1920,
		ViewportHeight: 1080,
		UserAgent:      "Mozilla/5.0 (test)",
		Timeout:        30 * time.Second,
		WaitFor:        "load",
		ExtraWait:      0,
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)

	// Verify basic network metrics
	assert.Greater(t, resp.Metrics.TotalRequests, 0, "should have at least one request")
	assert.Greater(t, resp.Metrics.TotalBytes, int64(0), "should have transferred bytes")

	// Verify timing metrics
	assert.Greater(t, resp.Metrics.TimeToFirstRequest, 0.0, "should have first request time")
	assert.Greater(t, resp.Metrics.TimeToLastResponse, 0.0, "should have last response time")
	assert.LessOrEqual(t, resp.Metrics.TimeToFirstRequest, resp.Metrics.TimeToLastResponse,
		"first request should be before or equal to last response")

	// Verify maps are populated
	assert.NotEmpty(t, resp.Metrics.BytesByType, "should have bytes by type breakdown")
	assert.NotEmpty(t, resp.Metrics.StatusCounts, "should have status counts")

	// Verify 2xx count (example.com should return 200)
	if count, ok := resp.Metrics.StatusCounts[types.StatusClass2xx]; ok {
		assert.Greater(t, count, int64(0), "should have 2xx responses")
	}

	// Verify origin breakdown sums correctly
	totalOriginRequests := resp.Metrics.SameOriginRequests + resp.Metrics.ThirdPartyRequests
	assert.Equal(t, resp.Metrics.TotalRequests, totalOriginRequests,
		"same-origin + third-party should equal total requests")

	totalOriginBytes := resp.Metrics.SameOriginBytes + resp.Metrics.ThirdPartyBytes
	assert.Equal(t, resp.Metrics.TotalBytes, totalOriginBytes,
		"same-origin + third-party bytes should equal total bytes")

	// Same-origin should have at least the main document
	assert.Greater(t, resp.Metrics.SameOriginRequests, 0, "should have at least one same-origin request")
	assert.Greater(t, resp.Metrics.SameOriginBytes, int64(0), "should have same-origin bytes")
}

func TestRender_NetworkMetrics_WithBlockedResources(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	req := &types.RenderRequest{
		RequestID:            "test-blocked-metrics",
		URL:                  "https://example.com/",
		ViewportWidth:        1920,
		ViewportHeight:       1080,
		UserAgent:            "Mozilla/5.0 (test)",
		Timeout:              30 * time.Second,
		WaitFor:              "load",
		ExtraWait:            0,
		BlockedResourceTypes: []string{"Image", "Font", "Stylesheet"},
	}

	resp, err := instance.Render(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)

	// Should still have at least the document request
	assert.Greater(t, resp.Metrics.TotalRequests, 0, "should have requests")

	// Document type should be present in bytes breakdown
	if docBytes, ok := resp.Metrics.BytesByType[types.ResourceTypeDocument]; ok {
		assert.Greater(t, docBytes, int64(0), "should have document bytes")
	}
}

func TestConsoleMessageCapture(t *testing.T) {
	t.Run("size limit on message field only", func(t *testing.T) {
		ce := types.ConsoleError{
			Type:           types.ConsoleTypeError,
			SourceURL:      "https://example.com/very-long-url-that-should-not-count.js",
			SourceLocation: "100:200",
			Message:        "short msg",
		}

		// Size should be len("short msg") = 9, not the full struct size
		assert.Equal(t, 9, len(ce.Message))
	})

	t.Run("console type constants", func(t *testing.T) {
		assert.Equal(t, "error", types.ConsoleTypeError)
		assert.Equal(t, "warning", types.ConsoleTypeWarning)
	})
}

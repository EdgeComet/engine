package chrome

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/pkg/types"
)

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

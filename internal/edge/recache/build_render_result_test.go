package recache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestBuildRenderResult_AllFieldsPopulated(t *testing.T) {
	headers := map[string][]string{
		"Content-Type":  {"text/html"},
		"Cache-Control": {"public, max-age=3600"},
	}
	renderResp := &types.RenderResponse{
		HTML:       "<html><body>test</body></html>",
		RenderTime: 2 * time.Second,
		ChromeID:   "chrome-1",
		Metrics: types.PageMetrics{
			StatusCode: 200,
			FinalURL:   "https://example.com/final",
		},
		Headers: headers,
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.Equal(t, []byte(renderResp.HTML), result.HTML)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, "https://example.com/final", result.RedirectLocation,
		"RedirectLocation mirrors the live path: it comes from Metrics.FinalURL, unconditionally")
	assert.Equal(t, 2*time.Second, result.RenderTime)
	assert.Equal(t, "chrome-1", result.ChromeID,
		"the real Chrome instance must be recorded, not the literal \"recache\"")
	assert.Equal(t, renderResp.Metrics, result.Metrics)

	require.NotNil(t, result.Headers, "Headers must be forwarded")
	assert.Equal(t, headers, result.Headers)
}

// The render service reports a structured error type even on responses the EG accepts
// (soft_timeout renders return HTML). Dropping it lost the only machine-readable cause.
func TestBuildRenderResult_ErrorFieldsCopied(t *testing.T) {
	renderResp := &types.RenderResponse{
		HTML:      "<html></html>",
		ErrorType: types.ErrorTypeSoftTimeout,
		Error:     "navigation timed out after 10s",
		Metrics: types.PageMetrics{
			StatusCode: 200,
		},
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.Equal(t, types.ErrorTypeSoftTimeout, result.ErrorType)
	assert.Equal(t, "navigation timed out after 10s", result.ErrorMessage)
}

func TestBuildRenderResult_NilHeaders(t *testing.T) {
	renderResp := &types.RenderResponse{
		HTML: "<html></html>",
		Metrics: types.PageMetrics{
			StatusCode: 200,
		},
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.Nil(t, result.Headers)
}

func TestBuildRenderResult_FieldParity(t *testing.T) {
	renderResp := &types.RenderResponse{
		HTML:       "<html><head><title>Parity</title></head></html>",
		RenderTime: 1 * time.Second,
		ChromeID:   "chrome-5",
		Metrics: types.PageMetrics{
			StatusCode: 200,
			FinalURL:   "https://example.com/page",
		},
		Headers: map[string][]string{
			"X-Custom": {"value"},
		},
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.NotNil(t, result.HTML, "HTML is required for cache file storage")
	assert.NotZero(t, result.StatusCode, "StatusCode is stored in cache metadata")
	assert.NotNil(t, result.Headers, "Headers are filtered and stored in cache metadata")
}

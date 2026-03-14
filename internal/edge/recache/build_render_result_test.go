package recache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestBuildRenderResult_AllFieldsPopulated(t *testing.T) {
	seo := &types.PageSEO{
		Title:       "Test Page",
		IndexStatus: types.IndexStatusIndexable,
	}
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
		},
		Headers: headers,
		PageSEO: seo,
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.Equal(t, []byte(renderResp.HTML), result.HTML)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, "", result.RedirectLocation)
	assert.Equal(t, 2*time.Second, result.RenderTime)
	assert.Equal(t, "recache", result.ChromeID)
	assert.Equal(t, renderResp.Metrics, result.Metrics)

	require.NotNil(t, result.Headers, "Headers must be forwarded")
	assert.Equal(t, headers, result.Headers)

	require.NotNil(t, result.PageSEO, "PageSEO must be forwarded")
	assert.Equal(t, "Test Page", result.PageSEO.Title)
	assert.Equal(t, types.IndexStatusIndexable, result.PageSEO.IndexStatus)
}

func TestBuildRenderResult_NilPageSEO(t *testing.T) {
	renderResp := &types.RenderResponse{
		HTML: "<html></html>",
		Metrics: types.PageMetrics{
			StatusCode: 200,
		},
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	assert.Nil(t, result.PageSEO)
	assert.Nil(t, result.Headers)
}

func TestBuildRenderResult_IndexStatusValues(t *testing.T) {
	tests := []struct {
		name        string
		indexStatus types.IndexStatus
	}{
		{"indexable", types.IndexStatusIndexable},
		{"non-200", types.IndexStatusNon200},
		{"blocked by meta", types.IndexStatusBlockedByMeta},
		{"non-canonical", types.IndexStatusNonCanonical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderResp := &types.RenderResponse{
				HTML: "<html></html>",
				Metrics: types.PageMetrics{
					StatusCode: 200,
				},
				PageSEO: &types.PageSEO{
					IndexStatus: tt.indexStatus,
				},
			}

			rs := &RecacheService{}
			result := rs.buildRenderResult(renderResp)

			require.NotNil(t, result.PageSEO)
			assert.Equal(t, tt.indexStatus, result.PageSEO.IndexStatus)
		})
	}
}

// TestBuildRenderResult_FieldParity verifies that buildRenderResult populates
// all fields used by SaveRenderCache (cache_coordinator.go).
// This test exists because a prior bug silently dropped PageSEO and Headers,
// causing index_status=0 and missing response headers in cached entries.
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
		PageSEO: &types.PageSEO{
			Title:       "Parity",
			IndexStatus: types.IndexStatusIndexable,
		},
	}

	rs := &RecacheService{}
	result := rs.buildRenderResult(renderResp)

	// Fields consumed by SaveRenderCache in cache_coordinator.go
	assert.NotNil(t, result.HTML, "HTML is required for cache file storage")
	assert.NotZero(t, result.StatusCode, "StatusCode is stored in cache metadata")
	assert.NotNil(t, result.Headers, "Headers are filtered and stored in cache metadata")
	assert.NotNil(t, result.PageSEO, "PageSEO provides IndexStatus and Title for cache metadata")
}

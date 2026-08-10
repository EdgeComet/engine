package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// The live render path returns these errors verbatim and falls back to stale cache or bypass on
// any of them, so the extracted classifier must keep both the order and the exact wording.
func TestValidateRenderResponse_Violations(t *testing.T) {
	tests := []struct {
		name          string
		resp          *types.RenderResponse
		wantReason    RenderResponseFailureReason
		wantErrorType string
		wantMessage   string
		wantStatus    int
	}{
		{
			name: "unsuccessful render wins over every later check",
			resp: &types.RenderResponse{
				Success:   false,
				Error:     "chrome exited unexpectedly",
				ErrorType: types.ErrorTypeChromeCrash,
				Metrics:   types.PageMetrics{StatusCode: 0},
			},
			wantReason:    RenderFailureNotSuccessful,
			wantErrorType: types.ErrorTypeChromeCrash,
			wantMessage:   "render failed: chrome exited unexpectedly",
			wantStatus:    0,
		},
		{
			name: "status not captured",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "<html></html>",
				Metrics: types.PageMetrics{StatusCode: 0},
			},
			wantReason:    RenderFailureStatusNotCaptured,
			wantErrorType: types.ErrorTypeStatusCaptureFailed,
			wantMessage:   "status code not captured",
			wantStatus:    0,
		},
		{
			name: "empty html on a non-redirect",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "",
				Metrics: types.PageMetrics{StatusCode: 404},
			},
			wantReason:    RenderFailureEmptyHTML,
			wantErrorType: types.ErrorTypeEmptyResponse,
			wantMessage:   "render service returned empty HTML",
			wantStatus:    404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := ValidateRenderResponse(tt.resp)

			require.NotNil(t, failure)
			assert.Equal(t, tt.wantReason, failure.Reason)
			assert.Equal(t, tt.wantErrorType, failure.ErrorType)
			assert.Equal(t, tt.wantMessage, failure.Message)
			assert.Equal(t, tt.wantStatus, failure.StatusCode)
		})
	}
}

func TestValidateRenderResponse_UsableResponses(t *testing.T) {
	tests := []struct {
		name string
		resp *types.RenderResponse
	}{
		{
			name: "successful render with html",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "<html><body>ok</body></html>",
				Metrics: types.PageMetrics{StatusCode: 200},
			},
		},
		{
			name: "redirect without html is allowed",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "",
				Metrics: types.PageMetrics{StatusCode: 302},
			},
		},
		{
			name: "soft timeout still returns usable html",
			resp: &types.RenderResponse{
				Success:   true,
				HTML:      "<html></html>",
				ErrorType: types.ErrorTypeSoftTimeout,
				Metrics:   types.PageMetrics{StatusCode: 200},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Nil(t, ValidateRenderResponse(tt.resp))
		})
	}
}

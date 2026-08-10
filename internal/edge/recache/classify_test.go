package recache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/pkg/types"
)

// Of the origin statuses only an uncacheable 5xx is worth another attempt. The rest report a
// stable decision by the origin, so retrying them (403, 404, 429) only adds load to a site that
// already answered. Identical for render and bypass - both route through this function.
func TestClassifyUncacheableStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorType  string
		permanent  bool
	}{
		{name: "500 is retryable", statusCode: 500, errorType: types.ErrorTypeOrigin5xx, permanent: false},
		{name: "503 is retryable", statusCode: 503, errorType: types.ErrorTypeOrigin5xx, permanent: false},
		{name: "404 is permanent", statusCode: 404, errorType: types.ErrorTypeOrigin4xx, permanent: true},
		{name: "403 is permanent", statusCode: 403, errorType: types.ErrorTypeOrigin4xx, permanent: true},
		{name: "429 is permanent", statusCode: 429, errorType: types.ErrorTypeOrigin4xx, permanent: true},
		{name: "301 is permanent", statusCode: 301, errorType: types.ErrorTypeOriginRedirect, permanent: true},
		{name: "303 is permanent", statusCode: 303, errorType: types.ErrorTypeOriginRedirect, permanent: true},
		{name: "204 falls back to uncacheable", statusCode: 204, errorType: types.ErrorTypeOriginUncacheable, permanent: true},
		{name: "100 falls back to uncacheable", statusCode: 100, errorType: types.ErrorTypeOriginUncacheable, permanent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := classifyUncacheableStatus(tt.statusCode)

			require.NotNil(t, failure)
			assert.Equal(t, tt.errorType, failure.errorType)
			assert.Equal(t, tt.permanent, failure.permanent)
			assert.Equal(t, tt.statusCode, failure.statusCode, "the origin status must survive onto the event row")
			assert.NotEmpty(t, failure.Error())
		})
	}
}

// A configured cacheable non-200 is a refresh, not a failure: a host with
// status_codes: [200, 404] could never refresh its 404 pages while recache hard-failed
// every non-200.
func TestClassifyStatus_CacheableStatusIsSuccess(t *testing.T) {
	rs := &RecacheService{cacheCoord: &orchestrator.CacheCoordinator{}}

	tests := []struct {
		name           string
		statusCode     int
		cacheableCodes []int
		wantFailure    bool
		wantErrorType  string
	}{
		{name: "200 with default codes", statusCode: 200, cacheableCodes: []int{200}, wantFailure: false},
		{name: "404 configured cacheable", statusCode: 404, cacheableCodes: []int{200, 404}, wantFailure: false},
		{name: "301 configured cacheable", statusCode: 301, cacheableCodes: []int{200, 301, 302}, wantFailure: false},
		{
			name: "404 not configured", statusCode: 404, cacheableCodes: []int{200},
			wantFailure: true, wantErrorType: types.ErrorTypeOrigin4xx,
		},
		{
			name: "502 not configured", statusCode: 502, cacheableCodes: []int{200},
			wantFailure: true, wantErrorType: types.ErrorTypeOrigin5xx,
		},
		{
			name: "302 not configured", statusCode: 302, cacheableCodes: []int{200},
			wantFailure: true, wantErrorType: types.ErrorTypeOriginRedirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := rs.classifyStatus(tt.statusCode, tt.cacheableCodes)

			if !tt.wantFailure {
				assert.Nil(t, failure)
				return
			}
			require.NotNil(t, failure)
			assert.Equal(t, tt.wantErrorType, failure.errorType)
		})
	}
}

// Render failures carry no origin status, so the 5xx-only retry rule must not reach them:
// a Chrome crash or a lost status code is exactly what retries exist for.
func TestClassifyRenderFailure(t *testing.T) {
	tests := []struct {
		name          string
		resp          *types.RenderResponse
		wantErrorType string
		wantStatus    int
	}{
		{
			name: "render service reported failure keeps its error type",
			resp: &types.RenderResponse{
				Success:   false,
				ErrorType: types.ErrorTypeChromeCrash,
				Error:     "chrome exited",
			},
			wantErrorType: types.ErrorTypeChromeCrash,
			wantStatus:    0,
		},
		{
			name: "failure without an error type is not given one",
			resp: &types.RenderResponse{
				Success: false,
				Error:   "no type reported",
			},
			wantErrorType: types.ErrorTypeUnknown,
			wantStatus:    0,
		},
		{
			name: "status not captured",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "<html></html>",
				Metrics: types.PageMetrics{StatusCode: 0},
			},
			wantErrorType: types.ErrorTypeStatusCaptureFailed,
			wantStatus:    0,
		},
		{
			name: "empty html on a non-redirect",
			resp: &types.RenderResponse{
				Success: true,
				HTML:    "",
				Metrics: types.PageMetrics{StatusCode: 200},
			},
			wantErrorType: types.ErrorTypeEmptyResponse,
			wantStatus:    200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violation := orchestrator.ValidateRenderResponse(tt.resp)
			require.NotNil(t, violation)

			failure := classifyRenderFailure(violation)

			assert.Equal(t, tt.wantErrorType, failure.errorType)
			assert.Equal(t, tt.wantStatus, failure.statusCode)
			assert.False(t, failure.permanent, "render failures carry no origin status and stay retryable")
			assert.NotEmpty(t, failure.errorType, "error_type is the success discriminator and must never be empty on a failure")
		})
	}
}

// Empty HTML is legitimate for a redirect, and the render path must not reject it before the
// cacheable-status rule gets to accept a configured 301.
func TestValidateRenderResponse_RedirectWithoutHTMLIsUsable(t *testing.T) {
	resp := &types.RenderResponse{
		Success: true,
		HTML:    "",
		Metrics: types.PageMetrics{StatusCode: 301, FinalURL: "https://example.com/moved"},
	}

	assert.Nil(t, orchestrator.ValidateRenderResponse(resp))
}

// The render service answers 503 for pool exhaustion and a dead Chrome and 504 for a hard
// timeout, so those causes only ever reach the edge gateway as a non-200 body. Collapsing them
// all into render_unavailable would leave the taxonomy unable to name the failures it exists for.
func TestClassifyRenderCallError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantErrorType string
	}{
		{
			name:          "pool exhaustion keeps the render service error type",
			err:           &rsclient.ServiceError{HTTPStatus: 503, ErrorType: types.ErrorTypePoolUnavailable, Body: "pool"},
			wantErrorType: types.ErrorTypePoolUnavailable,
		},
		{
			name:          "chrome crash keeps the render service error type",
			err:           &rsclient.ServiceError{HTTPStatus: 503, ErrorType: types.ErrorTypeChromeCrash, Body: "crash"},
			wantErrorType: types.ErrorTypeChromeCrash,
		},
		{
			name:          "hard timeout keeps the render service error type",
			err:           &rsclient.ServiceError{HTTPStatus: 504, ErrorType: types.ErrorTypeHardTimeout, Body: "timeout"},
			wantErrorType: types.ErrorTypeHardTimeout,
		},
		{
			name:          "non-200 without a structured type is unattributable",
			err:           &rsclient.ServiceError{HTTPStatus: 502, Body: "<html>proxy error</html>"},
			wantErrorType: types.ErrorTypeRenderUnavailable,
		},
		{
			name:          "transport failure is unattributable",
			err:           errors.New("dial tcp 10.0.0.1:10080: connect: connection refused"),
			wantErrorType: types.ErrorTypeRenderUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := classifyRenderCallError(tt.err)

			assert.Equal(t, tt.wantErrorType, failure.errorType)
			assert.Equal(t, noOriginStatus, failure.statusCode)
			assert.False(t, failure.permanent, "an unreachable render service is exactly what retries exist for")
			assert.ErrorIs(t, failure, tt.err, "the cause must stay reachable through the classification")
		})
	}
}

func TestRecacheError_ClassificationFields(t *testing.T) {
	permanent := permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus, "dimension 7 not found for host 1")
	assert.True(t, permanent.permanent)
	assert.Equal(t, types.ErrorTypeInvalidRequest, permanent.errorType)
	assert.Equal(t, 0, permanent.statusCode)
	assert.Equal(t, "dimension 7 not found for host 1", permanent.Error())

	retryable := retryableFailure(types.ErrorTypeRenderUnavailable, noOriginStatus, "no render service available")
	assert.False(t, retryable.permanent)
	assert.Equal(t, types.ErrorTypeRenderUnavailable, retryable.errorType)
}

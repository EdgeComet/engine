package recache

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/pkg/types"
)

// recacheOutcomeEnvelope is the response as the cache daemon reads it.
type recacheOutcomeEnvelope struct {
	Success bool                     `json:"success"`
	Message string                   `json:"message"`
	Data    types.RecacheOutcomeData `json:"data"`
}

func postRecache(t *testing.T, rs *RecacheService, req RecacheRequest) (*fasthttp.RequestCtx, recacheOutcomeEnvelope) {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetBody(body)
	rs.handleRecache(ctx)

	var envelope recacheOutcomeEnvelope
	require.NoError(t, json.Unmarshal(ctx.Response.Body(), &envelope))
	return ctx, envelope
}

// The HTTP status is the daemon's retry instruction. A permanent rejection answered with 500 burns
// three attempts and three error logs on a condition no retry can change - the pathology commit
// 7c74761 fixed for configuration declines, for a different class of error.
func TestHandleRecache_FailureOutcomeProtocol(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		hostID        int
		dimensionID   int
		mode          string
		wantStatus    int
		wantPermanent bool
	}{
		{
			name: "host not found is retryable", url: "https://example.com/page",
			hostID: 99, dimensionID: 0,
			wantStatus: fasthttp.StatusInternalServerError, wantPermanent: false,
		},
		{
			name: "dimension not found is permanent", url: "https://example.com/page",
			hostID: 1, dimensionID: 7,
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
		},
		{
			name: "private ip is permanent", url: "http://127.0.0.1/page",
			hostID: 1, dimensionID: 0,
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
		},
		{
			name: "domain mismatch is permanent", url: "https://not-configured.com/page",
			hostID: 1, dimensionID: 0,
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
		},
		{
			name: "mode render without render config is permanent", url: "https://example.com/page",
			hostID: 1, dimensionID: 0, mode: types.RecacheModeRender,
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, envelope := postRecache(t, validationTestService(), RecacheRequest{
				URL: tt.url, HostID: tt.hostID, DimensionID: tt.dimensionID, Mode: tt.mode,
			})

			assert.Equal(t, tt.wantStatus, ctx.Response.StatusCode())
			assert.False(t, envelope.Success, "a failed outcome must not be enveloped as a success")
			assert.Equal(t, types.RecacheOutcomeFailed, envelope.Data.Outcome)
			assert.Equal(t, tt.wantPermanent, envelope.Data.Permanent)
			assert.Equal(t, types.ErrorTypeInvalidRequest, envelope.Data.ErrorType)
			assert.NotEmpty(t, envelope.Message, "the message keeps the human-readable cause")
		})
	}
}

// A configuration decline is terminal and benign: the daemon must stop, not retry, and must be
// able to count it apart from a real refresh.
func TestHandleRecache_SkippedOutcome(t *testing.T) {
	ctx, envelope := postRecache(t, statusRuleService(), RecacheRequest{
		URL: "https://example.com/blocked", HostID: 1, DimensionID: 0,
	})

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.True(t, envelope.Success)
	assert.Equal(t, types.RecacheOutcomeSkipped, envelope.Data.Outcome)
	assert.NotEmpty(t, envelope.Data.Reason, "the daemon reports which configuration decision declined")
	assert.Empty(t, envelope.Data.ErrorType, "a decline is not a failure class")
}

// Pins the Section 4.1 retry table at the protocol boundary. Of the origin statuses only an
// uncacheable 5xx is worth another attempt; the failure classes that carry no origin status are
// transient infrastructure conditions and are exactly what retries exist for, so an over-literal
// reading of "5xx only" must not make them single-shot.
func TestRespondRecacheFailure_RetryTable(t *testing.T) {
	tests := []struct {
		name          string
		failure       *recacheError
		wantStatus    int
		wantPermanent bool
		wantErrorType string
	}{
		{
			name: "uncacheable 503", failure: classifyUncacheableStatus(503),
			wantStatus: fasthttp.StatusInternalServerError, wantPermanent: false,
			wantErrorType: types.ErrorTypeOrigin5xx,
		},
		{
			name: "uncacheable 404", failure: classifyUncacheableStatus(404),
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
			wantErrorType: types.ErrorTypeOrigin4xx,
		},
		{
			name: "uncacheable 403", failure: classifyUncacheableStatus(403),
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
			wantErrorType: types.ErrorTypeOrigin4xx,
		},
		{
			name: "uncacheable 429", failure: classifyUncacheableStatus(429),
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
			wantErrorType: types.ErrorTypeOrigin4xx,
		},
		{
			name: "uncacheable 301", failure: classifyUncacheableStatus(301),
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
			wantErrorType: types.ErrorTypeOriginRedirect,
		},
		{
			name: "uncacheable 204", failure: classifyUncacheableStatus(204),
			wantStatus: fasthttp.StatusUnprocessableEntity, wantPermanent: true,
			wantErrorType: types.ErrorTypeOriginUncacheable,
		},
		{
			name: "chrome crash", wantStatus: fasthttp.StatusInternalServerError, wantPermanent: false,
			failure: classifyRenderCallError(&rsclient.ServiceError{
				HTTPStatus: 503, ErrorType: types.ErrorTypeChromeCrash, Body: "crash",
			}),
			wantErrorType: types.ErrorTypeChromeCrash,
		},
		{
			name: "render service unreachable", wantStatus: fasthttp.StatusInternalServerError,
			wantPermanent: false,
			failure: retryableFailure(types.ErrorTypeRenderUnavailable, noOriginStatus,
				"no render service available"),
			wantErrorType: types.ErrorTypeRenderUnavailable,
		},
		{
			name: "cache write failure", wantStatus: fasthttp.StatusInternalServerError,
			wantPermanent: false,
			failure: retryableFailure(types.ErrorTypeCacheWriteFailed, 200,
				"failed to save cache: disk full"),
			wantErrorType: types.ErrorTypeCacheWriteFailed,
		},
		{
			name: "origin unreachable", wantStatus: fasthttp.StatusInternalServerError,
			wantPermanent: false,
			failure: retryableFailure(types.ErrorTypeNetworkError, noOriginStatus,
				"origin unreachable: dial timeout"),
			wantErrorType: types.ErrorTypeNetworkError,
		},
	}

	rs := &RecacheService{logger: zap.NewNop()}
	req := RecacheRequest{URL: "https://example.com/page", HostID: 1, DimensionID: 0}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			rs.respondRecacheFailure(ctx, req, tt.failure)

			var envelope recacheOutcomeEnvelope
			require.NoError(t, json.Unmarshal(ctx.Response.Body(), &envelope))

			assert.Equal(t, tt.wantStatus, ctx.Response.StatusCode())
			assert.Equal(t, types.RecacheOutcomeFailed, envelope.Data.Outcome)
			assert.Equal(t, tt.wantPermanent, envelope.Data.Permanent)
			assert.Equal(t, tt.wantErrorType, envelope.Data.ErrorType)
			assert.False(t, envelope.Success)
		})
	}
}

// Origin-class and capacity failures burst - one origin outage carries a whole autorecache queue -
// so only edge-gateway-side faults may reach the error-tracking floor.
func TestRecacheError_LogLevel(t *testing.T) {
	warnLevel := []string{
		types.ErrorTypeNetworkError, types.ErrorTypeOrigin4xx, types.ErrorTypeOrigin5xx,
		types.ErrorTypeOriginRedirect, types.ErrorTypeOriginUncacheable,
		types.ErrorTypeStatusCaptureFailed, types.ErrorTypeEmptyResponse,
		types.ErrorTypeRenderUnavailable, types.ErrorTypePoolUnavailable,
		types.ErrorTypeInvalidRequest,
	}
	for _, errorType := range warnLevel {
		assert.False(t, retryableFailure(errorType, noOriginStatus, "").logAtError(),
			"%s is not the edge gateway's fault and must stay below the error-tracking floor", errorType)
	}

	errorLevel := []string{
		types.ErrorTypeCacheWriteFailed, types.ErrorTypeChromeCrash, types.ErrorTypeHardTimeout,
		types.ErrorTypeChromeRestartFailed, types.ErrorTypeNavigationFailed, types.ErrorTypeUnknown,
	}
	for _, errorType := range errorLevel {
		assert.True(t, retryableFailure(errorType, noOriginStatus, "").logAtError(),
			"%s names a fault on our side and must reach error tracking", errorType)
	}
}

// The bypass half of the retry table, driven through the real fetch so it also pins that bypass
// consults Bypass.Cache.StatusCodes rather than a hardcoded 200.
func TestProcessBypassRecache_OriginStatusRetryTable(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			w.Header().Set("Location", "https://example.com/elsewhere")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer origin.Close()

	tests := []struct {
		name          string
		path          string
		cacheable     []int
		wantErrorType string
		wantStatus    int
		wantPermanent bool
		wantRedirect  string
	}{
		{
			name: "uncacheable 503 is retryable", path: "/page", cacheable: []int{200},
			wantErrorType: types.ErrorTypeOrigin5xx, wantStatus: 503, wantPermanent: false,
		},
		{
			name: "uncacheable 301 is permanent and keeps its target", path: "/moved",
			cacheable:     []int{200},
			wantErrorType: types.ErrorTypeOriginRedirect, wantStatus: 301, wantPermanent: true,
			wantRedirect: "https://example.com/elsewhere",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := localOriginBypassService()

			renderCtx := bypassRecacheContext(true, time.Minute)
			renderCtx.Host = &types.Host{ID: 1, Domain: "example.com"}
			renderCtx.ResolvedConfig.Bypass.Cache.StatusCodes = tt.cacheable

			err := rs.processBypassRecache(context.Background(), origin.URL+tt.path, renderCtx, time.Now())

			require.Error(t, err)
			var failure *recacheError
			require.True(t, errors.As(err, &failure))
			assert.Equal(t, tt.wantErrorType, failure.errorType)
			assert.Equal(t, tt.wantStatus, failure.statusCode)
			assert.Equal(t, tt.wantPermanent, failure.permanent)
			assert.Equal(t, tt.wantRedirect, failure.redirectTo)
		})
	}
}

// localOriginBypassService fetches from a loopback test origin, which the SSRF-safe dialer would
// otherwise refuse.
func localOriginBypassService() *RecacheService {
	ssrfDisabled := false
	return &RecacheService{
		logger:     zap.NewNop(),
		cacheCoord: &orchestrator.CacheCoordinator{},
		bypassSvc: bypass.NewBypassService(&config.GlobalBypassConfig{
			UserAgent:      "EdgeCometTest/1.0",
			SSRFProtection: &ssrfDisabled,
		}, zap.NewNop()),
	}
}

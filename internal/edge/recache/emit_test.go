package recache

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/pkg/types"
)

// captureEmitter records emitted events so a test can assert on the row a failure produced.
type captureEmitter struct {
	emitted []*events.RequestEvent
}

func (c *captureEmitter) Emit(event *events.RequestEvent) { c.emitted = append(c.emitted, event) }

func (c *captureEmitter) Close() error { return nil }

func emittingValidationService() (*RecacheService, *captureEmitter) {
	emitter := &captureEmitter{}
	rs := validationTestService()
	rs.eventEmitter = emitter
	rs.instanceID = "eg-test-1"
	return rs, emitter
}

// A request rejected before its render context exists has no RenderContext to build an event from,
// and BuildRequestEvent without one falls back to the response source: the row would land as
// event_type='bypass' on host_id 0, so the cluster-move case - the one a dashboard most needs to
// explain - would be unfindable under both the precache filter and the host filter.
func TestProcessRecache_HostNotFoundEmitsPrecacheRowForTheRequestedHost(t *testing.T) {
	rs, emitter := emittingValidationService()

	err := rs.ProcessRecache(context.Background(), "https://example.com/page", 99, 0, "")
	require.Error(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, events.EventTypePrecache, event.EventType)
	assert.Equal(t, 99, event.HostID, "the row must name the host the daemon asked about")
	assert.Equal(t, types.ErrorTypeInvalidRequest, event.ErrorType)
	assert.Contains(t, event.ErrorMessage, "host not found")
	assert.Equal(t, "https://example.com/page", event.URL)
	assert.NotZero(t, event.URLHash, "a parseable URL still joins the attempts that got further")
	assert.Empty(t, event.Source, "the attempt never chose render or bypass")
	assert.Equal(t, "eg-test-1", event.EGInstanceID)
}

func TestProcessRecache_PreContextFailuresEmitPrecacheRows(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		hostID        int
		dimensionID   int
		mode          string
		wantHostID    int
		wantDimension string
		wantURLHash   bool
	}{
		{
			name: "dimension not found", url: "https://example.com/page",
			hostID: 1, dimensionID: 7, wantHostID: 1,
			wantDimension: "", wantURLHash: true,
		},
		{
			name: "unparseable url", url: "http://exa mple.com/\x7f",
			hostID: 1, dimensionID: 0, wantHostID: 1,
			wantDimension: "desktop", wantURLHash: false,
		},
		{
			name: "private ip", url: "http://127.0.0.1/page",
			hostID: 1, dimensionID: 0, wantHostID: 1,
			wantDimension: "desktop", wantURLHash: true,
		},
		{
			name: "domain mismatch", url: "https://not-configured.com/page",
			hostID: 1, dimensionID: 0, wantHostID: 1,
			wantDimension: "desktop", wantURLHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, emitter := emittingValidationService()

			err := rs.ProcessRecache(context.Background(), tt.url, tt.hostID, tt.dimensionID, tt.mode)
			require.Error(t, err)

			require.Len(t, emitter.emitted, 1)
			event := emitter.emitted[0]
			assert.Equal(t, events.EventTypePrecache, event.EventType)
			assert.Equal(t, tt.wantHostID, event.HostID)
			assert.Equal(t, tt.wantDimension, event.Dimension,
				"an unresolved dimension name IS the failure and must stay empty")
			assert.Equal(t, types.ErrorTypeInvalidRequest, event.ErrorType)
			assert.NotEmpty(t, event.ErrorMessage)
			assert.Equal(t, tt.url, event.URL)
			assert.Equal(t, noOriginStatus, event.StatusCode, "no origin answered a rejected request")

			if tt.wantURLHash {
				assert.NotZero(t, event.URLHash)
			} else {
				assert.Zero(t, event.URLHash, "a URL that does not normalize has no honest hash")
			}
		})
	}
}

// The mode:render rejection happens after the render context exists, so its row carries the real
// context - routing it through the synthetic builder would emit a worse row than it already has.
func TestProcessRecache_PostContextRejectionUsesTheRealContext(t *testing.T) {
	rs, emitter := emittingValidationService()

	err := rs.ProcessRecache(context.Background(), "https://example.com/page", 1, 0, types.RecacheModeRender)
	require.Error(t, err)

	require.Len(t, emitter.emitted, 1)
	event := emitter.emitted[0]
	assert.Equal(t, events.EventTypePrecache, event.EventType)
	assert.Equal(t, 1, event.HostID)
	assert.Equal(t, "desktop", event.Dimension)
	assert.Equal(t, events.SourceRender, event.Source, "a forced render names render as its source")
	assert.NotEmpty(t, event.CacheKey, "the real context knows the cache key the attempt targeted")
	assert.NotZero(t, event.URLHash)
	assert.Equal(t, types.ErrorTypeInvalidRequest, event.ErrorType)
}

// A permanently misconfigured host must not accumulate junk rows forever, so a configuration
// decline is answered and logged but never persisted.
func TestProcessRecache_ConfigDeclineEmitsNothing(t *testing.T) {
	emitter := &captureEmitter{}
	rs := statusRuleService()
	rs.eventEmitter = emitter

	err := rs.ProcessRecache(context.Background(), "https://example.com/blocked", 1, 0, "")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRecacheSkipped))
	assert.Empty(t, emitter.emitted, "a configuration decline is not a failed attempt")
}

// The emitter is optional: an edge gateway with event logging disabled must still recache.
func TestProcessRecache_NilEmitterDoesNotPanic(t *testing.T) {
	rs := validationTestService()
	require.Nil(t, rs.eventEmitter)

	assert.NotPanics(t, func() {
		_ = rs.ProcessRecache(context.Background(), "https://example.com/page", 99, 0, "")
	})
}

// An empty error_type is the success discriminator, so anything that is not a classified
// failure must leave the emission path untouched and let the existing success rows stand.
func TestClassifiedFailure_SuccessAndDeclinesAreNotFailures(t *testing.T) {
	assert.Nil(t, classifiedFailure(nil),
		"a successful recache emits its own success row and no failure row")
	assert.Nil(t, classifiedFailure(fmt.Errorf("%w: bypass cache disabled", ErrRecacheSkipped)),
		"a configuration decline is not persisted")

	failure := classifiedFailure(errors.New("something nobody classified"))
	require.NotNil(t, failure, "an unclassified failure must not vanish from the ledger")
	assert.Equal(t, types.ErrorTypeUnknown, failure.errorType)
	assert.False(t, failure.permanent)
}

// Failure rows have to carry enough to answer "what happened to this URL": the mode, the origin
// status where one exists, and the redirect target when the origin sent one.
func TestEmitPrecacheFailure_RowCarriesTheAttempt(t *testing.T) {
	tests := []struct {
		name       string
		action     types.URLRuleAction
		failure    *recacheError
		wantSource string
		wantStatus int
		wantRedir  string
	}{
		{
			name: "bypass origin redirect", action: types.ActionBypass,
			failure:    classifyUncacheableStatus(302).withRedirect("https://example.com/moved"),
			wantSource: events.SourceBypass, wantStatus: 302, wantRedir: "https://example.com/moved",
		},
		{
			name: "bypass transport failure", action: types.ActionBypass,
			failure: retryableFailure(types.ErrorTypeNetworkError, noOriginStatus, "origin unreachable: dial"),
			// The synthetic 502 was never sent by the origin, so no status reaches the row.
			wantSource: events.SourceBypass, wantStatus: noOriginStatus,
		},
		{
			name: "render origin 5xx", action: types.ActionRender,
			failure:    classifyUncacheableStatus(503),
			wantSource: events.SourceRender, wantStatus: 503,
		},
		{
			name: "render chrome crash", action: types.ActionRender,
			failure:    retryableFailure(types.ErrorTypeChromeCrash, noOriginStatus, "chrome exited"),
			wantSource: events.SourceRender, wantStatus: noOriginStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &captureEmitter{}
			rs := &RecacheService{eventEmitter: emitter, instanceID: "eg-test-1"}

			renderCtx := bypassRecacheContext(true, time.Minute)
			renderCtx.ResolvedConfig.Action = tt.action
			renderCtx.Host = &types.Host{ID: 7, Domain: "example.com"}
			renderCtx.TargetURL = "https://example.com/page"
			renderCtx.OriginalURL = "https://example.com/page"
			renderCtx.URLHash = 4242
			renderCtx.Dimension = "desktop"
			renderCtx.IsPrecache = true

			attempt := &precacheAttempt{
				url: renderCtx.TargetURL, hostID: 7, dimensionID: 0, startTime: time.Now(),
				host: renderCtx.Host, dimension: "desktop", renderCtx: renderCtx,
			}

			rs.emitPrecacheFailure(attempt, tt.failure)

			require.Len(t, emitter.emitted, 1)
			event := emitter.emitted[0]
			assert.Equal(t, events.EventTypePrecache, event.EventType)
			assert.Equal(t, tt.wantSource, event.Source)
			assert.Equal(t, 7, event.HostID)
			assert.Equal(t, "example.com", event.Host)
			assert.Equal(t, "desktop", event.Dimension)
			assert.Equal(t, "https://example.com/page", event.URL)
			assert.Equal(t, uint64(4242), event.URLHash)
			assert.Equal(t, tt.wantStatus, event.StatusCode)
			assert.Equal(t, tt.failure.errorType, event.ErrorType)
			assert.Equal(t, tt.failure.Error(), event.ErrorMessage)
			assert.Equal(t, tt.wantRedir, event.RedirectTo)
		})
	}
}

// Only a redirect has a redirect target. The render service reports a final URL for every status,
// so copying it onto a 404 would invent one.
func TestWithRedirect_OnlyAnnotatesRedirects(t *testing.T) {
	redirect := classifyUncacheableStatus(301).withRedirect("https://example.com/moved")
	assert.Equal(t, "https://example.com/moved", redirect.redirectTo)

	notFound := classifyUncacheableStatus(404).withRedirect("https://example.com/page")
	assert.Empty(t, notFound.redirectTo)
}

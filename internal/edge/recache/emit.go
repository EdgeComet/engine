package recache

import (
	"time"

	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

// precacheAttempt accumulates what an emitted failure row needs about one recache attempt.
// Validation rejects a request before its render context exists, so the fields fill in as the
// flow resolves them and the emission works with whatever was reached.
type precacheAttempt struct {
	url         string
	hostID      int
	dimensionID int
	startTime   time.Time

	host      *types.Host
	dimension string
	renderCtx *edgectx.RenderContext
}

// syntheticContext builds the minimal context a pre-context failure is emitted from. IsPrecache is
// what makes it a precache row: without it events.BuildRequestEvent falls back to the response
// source, and a validation rejection would be filed as live bypass traffic.
func (a *precacheAttempt) syntheticContext() *edgectx.RenderContext {
	return &edgectx.RenderContext{
		TargetURL:   a.url,
		OriginalURL: a.url,
		URLHash:     bestEffortURLHash(a.url),
		Host:        a.host,
		Dimension:   a.dimension,
		IsPrecache:  true,
	}
}

// bestEffortURLHash hashes the URL the way buildRecacheContext would, so a row written before the
// render context exists still joins the attempts that got further. Returns 0 when the URL is
// itself the failure: there is no honest hash for a URL that does not normalize.
func bestEffortURLHash(url string) uint64 {
	normalizer := hash.NewURLNormalizer()
	normalized, err := normalizer.Normalize(url, nil)
	if err != nil {
		return 0
	}
	return normalizer.Hash(normalized.NormalizedURL)
}

// precacheSource names which half of the flow the attempt was in, mirroring ProcessRecache's own
// routing decision (bypass action goes to the origin fetch, anything else renders), so failure
// rows slice by mode exactly like success rows.
func precacheSource(renderCtx *edgectx.RenderContext) orchestrator.ResponseSource {
	if renderCtx.ResolvedConfig != nil && renderCtx.ResolvedConfig.Action == types.ActionBypass {
		return orchestrator.ServedFromBypass
	}
	return orchestrator.ServedFromRender
}

// emitPrecacheFailure records a terminal failure as a precache event. Without it the events table
// holds successful recaches only, so the failure denominator is unrecoverable and a dashboard
// cannot answer whether last night's pre-cache worked.
func (rs *RecacheService) emitPrecacheFailure(attempt *precacheAttempt, failure *recacheError) {
	if rs.eventEmitter == nil {
		return
	}

	renderCtx := attempt.renderCtx
	if renderCtx == nil {
		renderCtx = attempt.syntheticContext()
	}

	duration := time.Since(attempt.startTime)
	result := &orchestrator.RenderResult{
		Source:       precacheSource(renderCtx),
		Duration:     duration,
		StatusCode:   failure.statusCode,
		ErrorType:    failure.errorType,
		ErrorMessage: failure.message,
		RedirectTo:   failure.redirectTo,
	}

	event := events.BuildRequestEvent(renderCtx, result, duration, rs.instanceID)

	if attempt.renderCtx == nil {
		// The attempt was rejected before it chose render or bypass. ResponseSource has no
		// "undecided" value and its zero value means cache, so the guess is cleared here.
		event.Source = ""

		if attempt.host == nil {
			// Host-not-found is the cluster-move case and the one a dashboard most needs to
			// explain, so the row still has to name the host the daemon asked about.
			event.HostID = attempt.hostID
		}
	}

	rs.eventEmitter.Emit(event)
}

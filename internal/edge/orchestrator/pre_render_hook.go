package orchestrator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
)

const (
	// preRenderHookTimeout bounds the context handed to the hook. It is cooperative: a hook that
	// ignores its context blocks the request goroutine for as long as it likes, holding the render
	// lock. Honouring the context is part of the contract, not something this enforces.
	preRenderHookTimeout = 5 * time.Second

	// A decision is served with no body, so only statuses where an empty body means something may
	// come back: 204, and everything from 300 up. See decisionStatusServable.
	statusCodeNoContent = 204
	minNonSuccessStatus = 300
	maxHTTPStatusCode   = 599
)

// decisionStatusServable reports whether a status can be served as a bodyless final response.
//
// A 1xx is a protocol artefact that is never a final response, and a 2xx other than 204 would
// publish an empty page under a status asserting it is the real content - and cache it for a full
// TTL. The mistake that catches is a hook mapping an origin's "this URL is fine" to Handled
// instead of declining, which would blank every good page on that host until the cache expired.
func decisionStatusServable(statusCode int) bool {
	return statusCode == statusCodeNoContent ||
		(statusCode >= minNonSuccessStatus && statusCode <= maxHTTPStatusCode)
}

// PreRenderHook answers a request without rendering it. It runs after the cache lookup and after
// the render lock is held, so it costs nothing on a cache hit and runs once per URL under
// contention, and it runs before a render service tab is reserved so a short-circuit does not
// occupy Chrome.
//
// It exists for origins that cannot report a status honestly - a single-page app whose shell
// answers 200 for every URL, where whether a URL is really a 404 or a redirect has to be
// established some other way.
//
// The hook is an optimisation, never a gate: declining, erroring, panicking and returning nonsense
// all mean "render normally". See RunPreRenderHook.
//
// Two obligations on implementations, both of which bite only in production:
//
// Use the ctx argument for any outbound call, never renderCtx.GetContext(). renderCtx carries a
// timeout budget on the live path but not on the recache path, where GetContext returns an
// already-cancelled context and IsTimedOut always reports true.
//
// Read only the fields both paths populate: TargetURL, OriginalURL, URLHash, Host, Dimension,
// CacheKey, ResolvedConfig, RequestID, Logger and IsPrecache. HTTPCtx, ClientHeaders and ClientIP
// are live-request only and are nil during recache - and recache is where the volume is, so a hook
// that reaches for HTTPCtx passes every live test and then panics on the scheduled pass.
type PreRenderHook func(ctx context.Context, renderCtx *edgectx.RenderContext) (*PreRenderDecision, error)

// PreRenderDecision short-circuits a render with a status response. It is a struct rather than
// bare return values so a further decision kind can be added without breaking existing hooks.
type PreRenderDecision struct {
	// Handled false means the hook has no opinion about this request.
	Handled    bool
	StatusCode int
	// Location is written as the Location header, for redirect status codes.
	Location string
}

// RunPreRenderHook returns the decision to act on, or nil to render normally.
//
// Every failure mode collapses to nil on purpose. A hook that errors, hangs or returns an
// impossible status must not be able to make the request worse than it would have been without
// it, and in particular must never serve a bogus status to a crawler.
func RunPreRenderHook(ctx context.Context, hook PreRenderHook, renderCtx *edgectx.RenderContext) (decision *PreRenderDecision) {
	if hook == nil {
		return nil
	}

	// A panic here would unwind past the caller's explicit lock release - the deferred one is not
	// installed until a render tab is held - leaving the URL locked for the whole lock TTL while
	// every later request waits out the poll and degrades to bypass. That is the one outcome the
	// fail-open contract exists to prevent, so a panicking hook is treated exactly like an
	// erroring one.
	defer func() {
		if recovered := recover(); recovered != nil {
			renderCtx.Logger.Error("Pre-render hook panicked, rendering normally",
				zap.String("url", renderCtx.TargetURL),
				zap.Any("panic", recovered))

			decision = nil
		}
	}()

	hookCtx, cancel := context.WithTimeout(ctx, preRenderHookTimeout)
	defer cancel()

	decision, err := hook(hookCtx, renderCtx)
	if err != nil {
		renderCtx.Logger.Warn("Pre-render hook failed, rendering normally",
			zap.String("url", renderCtx.TargetURL),
			zap.Error(err))

		return nil
	}

	if decision == nil || !decision.Handled {
		return nil
	}

	if !decisionStatusServable(decision.StatusCode) {
		renderCtx.Logger.Error("Pre-render hook returned an unusable status code, rendering normally",
			zap.String("url", renderCtx.TargetURL),
			zap.Int("status_code", decision.StatusCode))

		return nil
	}

	renderCtx.Logger.Info("Pre-render hook answered without rendering",
		zap.String("url", renderCtx.TargetURL),
		zap.Int("status_code", decision.StatusCode),
		zap.String("location", decision.Location))

	return decision
}

// AsProcessedContent adapts the decision to the shape the override serving and caching paths
// already take, so a hook result is stored and served exactly like a content processor override:
// a metadata-only cache entry plus a status response.
func (d *PreRenderDecision) AsProcessedContent() *ProcessedContent {
	return &ProcessedContent{
		Override: &ResponseOverride{
			StatusCode: d.StatusCode,
			Location:   d.Location,
		},
	}
}

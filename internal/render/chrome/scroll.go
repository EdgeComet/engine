package chrome

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

const (
	// scrollStepPause is the wait between steps, giving lazy sections time to mount. It is the
	// pause that matters: it is held while the pass confirms the page has stopped changing.
	scrollStepPause = 400 * time.Millisecond
	// scrollTravelPause is the shorter wait used while the page still has distance to cover.
	// A travelling step only needs the page to keep moving - an IntersectionObserver records an
	// intersection however fast it is passed, and anything mounting late is caught by the settle
	// rounds, which always use the full pause. Measured against the scroll fixtures this is a
	// third off the pass with byte-identical captures.
	scrollTravelPause = 150 * time.Millisecond
	// scrollMaxSteps caps the loop. The duration budget is the limit that should normally bind;
	// this exists so a page that answers instantly cannot spin.
	scrollMaxSteps = 40
	// scrollSettleRounds is how many consecutive steps must report the page at its bottom with
	// nothing new arriving before it counts as settled. Content keeps coming for a beat after the
	// position stops advancing, so settling on position alone truncates it.
	scrollSettleRounds = 3
	// scrollInnerMaxSteps bounds the inner-container phase. An app shell keeps its content in a
	// container and needs a handful of steps; a virtualized list is effectively bottomless and
	// must not be allowed to consume the whole budget.
	scrollInnerMaxSteps = 12
	// scrollMaxEmptySteps is how many consecutive steps may find nothing scrollable before the
	// pass gives up. An early start simply means the page has not laid out yet, so the first
	// empty answer is not a verdict.
	scrollMaxEmptySteps = 5
	// scrollMinContainerHeight is the smallest visible height a descendant may have to be
	// considered a scroll container, keeping small widgets out of the ranking.
	scrollMinContainerHeight = 200
	// scrollMinDelta is the smallest scrollHeight-clientHeight difference worth scrolling.
	scrollMinDelta = 50
	// scrollInnerDominance is how many times taller than the page a container must be before the
	// pass spends budget on it. Side panels and odds lists scroll, but the content that matters is
	// in the page; a container only earns the extra seconds when it clearly holds the page's
	// content instead, which is the app-shell layout the inner phase exists for.
	scrollInnerDominance = 2
)

// Scroll phases. The page carries page-level lazy content, including the footer, so it is walked
// first; a container is only worth driving once the page has nothing left to give.
const (
	scrollModePage  = "page"
	scrollModeInner = "inner"
)

// Reasons a pass stopped, recorded on the render metrics. Without this, a pass that ran out of
// budget halfway down a page is indistinguishable from one that reached the end.
const (
	scrollStopSettled   = "settled"
	scrollStopDuration  = "duration"
	scrollStopMaxSteps  = "max_steps"
	scrollStopNoTarget  = "no_target"
	scrollStopCancelled = "cancelled"
	scrollStopError     = "error"
)

// scrollStepTemplate performs one scroll step and reports page state. Format arguments, in order:
// page mode, minimum scrollable delta, minimum container height, step size (0 = one viewport),
// inner-container dominance factor.
//
// In page mode both document scrollers are driven blind. On any given layout exactly one of them
// is the real scroll container - `html { overflow: hidden }` with a scrolling body is common
// enough to have motivated this whole feature - and the other reports a delta of 0, which makes
// the write to it inert. Testing beats detecting: a ranking picks whichever element happens to be
// tallest at the instant it runs, and on a page still mounting its content that is routinely a
// sidebar rather than the page itself.
//
// In inner mode the container is brought back into the viewport before it is advanced. Walking the
// page to its bottom moves an inner panel off screen, and observers with the implicit (viewport)
// root then never fire no matter how far that panel is scrolled.
//
// behavior:'instant' defeats CSS scroll-behavior:smooth, whose animation would outlast the step
// pause and make the next step read a stale position.
const scrollStepTemplate = `
(() => {
  const PAGE_MODE = %v;
  const MIN_DELTA = %d, MIN_HEIGHT = %d, STEP_PX = %d, DOMINANCE = %d;

  const delta = el => (el && el.isConnected) ? el.scrollHeight - el.clientHeight : -1;
  const atBottom = el => el.scrollTop + el.clientHeight >= el.scrollHeight - 2;
  const name = el => el === document.body ? 'body'
      : el === (document.scrollingElement || document.documentElement) ? 'scrollingElement'
      : el.tagName + (el.className ? '.' + String(el.className).split(' ')[0] : '');

  const de = document.scrollingElement || document.documentElement;
  const page = [de, document.body].filter(el => delta(el) >= MIN_DELTA);

  // A container has to clearly dominate the page before it is worth any budget. Without this the
  // pass spends seconds on whichever side panel happens to scroll, which on a page whose own
  // content is taller is time bought for nothing.
  const pageDelta = page.length ? Math.max(...page.map(delta)) : 0;

  function largestInner() {
    let best = null, bestDelta = Math.max(MIN_DELTA, pageDelta * DOMINANCE);
    for (const el of document.querySelectorAll('*')) {
      if (el.clientHeight < MIN_HEIGHT || page.includes(el)) continue;
      const d = delta(el);
      if (d <= bestDelta || atBottom(el)) continue;
      const oy = getComputedStyle(el).overflowY;
      if (oy === 'auto' || oy === 'scroll') { best = el; bestDelta = d; }
    }
    return best;
  }

  const advance = el => {
    const by = STEP_PX > 0 ? STEP_PX : el.clientHeight;
    el.scrollTo({ top: el.scrollTop + by, behavior: 'instant' });
  };

  let driven = '', inner = '';
  if (PAGE_MODE) {
    for (const el of page) {
      if (!atBottom(el)) { advance(el); driven += (driven ? '+' : '') + name(el); }
    }
  } else {
    const el = largestInner();
    if (el) {
      el.scrollIntoView({ block: 'nearest', behavior: 'instant' });
      advance(el);
      inner = driven = name(el);
    }
  }

  const p = page.length ? page[0] : null;
  return {
    anyTarget: page.length > 0 || driven !== '' || !!largestInner(),
    pageFound: page.length > 0,
    pageTarget: p ? name(p) : '',
    pageTop: p ? Math.round(p.scrollTop) : 0,
    pageClient: p ? p.clientHeight : 0,
    pageHeight: p ? p.scrollHeight : 0,
    pageAtBottom: p ? atBottom(p) : false,
    innerLeft: !!largestInner(),
    innerTarget: inner,
    links: document.links.length,
  };
})()
`

// scrollRestoreJS returns the document to the top, so the captured DOM serializes as top-of-page.
// Both scrollers are reset for the same reason the step drives both, and inner mode's
// scrollIntoView can have moved the page as a side effect.
// behavior:'instant' matters here too: under CSS scroll-behavior:smooth a plain assignment
// animates back to the top, firing scroll handlers while HTML extraction is running.
const scrollRestoreJS = `
(() => {
  const de = document.scrollingElement || document.documentElement;
  for (const el of [de, document.body]) {
    if (el && el.isConnected) el.scrollTo({ top: 0, behavior: 'instant' });
  }
})()
`

// scrollStepJS renders the step script for one step. mode and the step size change between steps,
// so unlike the previous single-target script this cannot be built once.
func scrollStepJS(mode string, stepPx int) string {
	return fmt.Sprintf(scrollStepTemplate, mode == scrollModePage, scrollMinDelta,
		scrollMinContainerHeight, stepPx, scrollInnerDominance)
}

// scrollState is the page state reported by a single scroll step.
type scrollState struct {
	// AnyTarget is false only when nothing on the page is scrollable at all, which on an early
	// start means the page has not laid out rather than that it never will.
	AnyTarget bool `json:"anyTarget"`
	// PageFound distinguishes an app shell whose document does not scroll from a normal page.
	PageFound    bool   `json:"pageFound"`
	PageTarget   string `json:"pageTarget"`
	PageTop      int    `json:"pageTop"`
	PageClient   int    `json:"pageClient"`
	PageHeight   int    `json:"pageHeight"`
	PageAtBottom bool   `json:"pageAtBottom"`
	InnerLeft    bool   `json:"innerLeft"`
	InnerTarget  string `json:"innerTarget"`
	// Links is a cheap proxy for content still arriving: a page can gain content without gaining
	// height, and settling on height alone cuts those pages short.
	Links int `json:"links"`
}

// scrollOutcome summarizes a scroll pass, including a pass that stopped on a bound or an error.
type scrollOutcome struct {
	Performed bool
	// NoTarget separates the page having nothing scrollable from a pass that failed or was cut
	// short. Only the first says anything about the page.
	NoTarget bool
	// ReachedBottom reports whether the page scroller ended at its bottom. A pass that ran out of
	// budget partway down still looks successful without it.
	ReachedBottom bool
	PageTarget    string
	InnerTarget   string
	Steps         int
	InnerSteps    int
	FinalHeight   int
	Duration      time.Duration
	StopReason    string
}

// scrollLoop drives the scroll from Go, one synchronous page evaluation per step.
// chromedp.Evaluate does not await promises, so an in-page loop would return immediately and let
// HTML capture race a scroll still running. Driving it from Go also makes the step and duration
// bounds enforceable and lets a cancelled context stop the loop.
// Every page interaction is injected, so the pass can be exercised without a browser.
type scrollLoop struct {
	step    func(ctx context.Context, mode string, stepPx int) (scrollState, error)
	pause   func(ctx context.Context, d time.Duration) error
	restore func(ctx context.Context) error
	now     func() time.Time
}

// run walks the page to its bottom, then any container that still has content to reveal, and
// returns what it achieved. A step error ends the pass and is returned with the partial outcome.
func (l scrollLoop) run(ctx context.Context) (scrollOutcome, error) {
	start := l.now()
	deadline := start.Add(types.ScrollMaxDuration)

	var (
		outcome    scrollOutcome
		lastHeight int
		lastBottom int
		lastClient int
		stable     int
		empty      int
		mode       = scrollModePage
	)
	lastLinks := -1

	for step := 0; step < scrollMaxSteps; step++ {
		if ctx.Err() != nil {
			outcome.StopReason = scrollStopCancelled
			break
		}
		if !l.now().Before(deadline) {
			outcome.StopReason = scrollStopDuration
			break
		}

		stepPx := 0
		if mode == scrollModePage && outcome.Performed {
			stepPx = l.stepSize(step, lastHeight-lastBottom, lastClient, deadline)
		}

		state, err := l.step(ctx, mode, stepPx)
		if err != nil {
			outcome.Duration = l.now().Sub(start)
			outcome.StopReason = scrollStopError
			return outcome, err
		}

		if !state.AnyTarget {
			empty++
			if empty >= scrollMaxEmptySteps {
				outcome.NoTarget = !outcome.Performed
				outcome.StopReason = scrollStopNoTarget
				break
			}
			if err := l.pause(ctx, scrollStepPause); err != nil {
				outcome.StopReason = scrollStopCancelled
				break
			}
			continue
		}
		empty = 0

		outcome.Performed = true
		outcome.Steps = step + 1
		outcome.PageTarget = state.PageTarget
		outcome.FinalHeight = state.PageHeight
		outcome.ReachedBottom = state.PageAtBottom
		if state.InnerTarget != "" {
			outcome.InnerTarget = state.InnerTarget
			outcome.InnerSteps++
		}
		lastBottom = state.PageTop + state.PageClient
		lastClient = state.PageClient

		// A page can gain content without gaining height, so both are watched.
		grew := state.PageHeight != lastHeight || state.Links != lastLinks
		lastHeight, lastLinks = state.PageHeight, state.Links

		mode, stable = l.advancePhase(mode, stable, grew, state, outcome.InnerSteps, &outcome)
		if outcome.StopReason != "" {
			break
		}

		if err := l.pause(ctx, stepPause(mode, stable)); err != nil {
			outcome.StopReason = scrollStopCancelled
			break
		}
	}

	if outcome.StopReason == "" {
		outcome.StopReason = scrollStopMaxSteps
	}
	outcome.Duration = l.now().Sub(start)
	return outcome, nil
}

// stepPause returns how long to wait after a step. The full pause is held once the page is at its
// bottom and being watched for late arrivals, and throughout the inner phase, whose steps are both
// bounded and the ones most likely to be revealing content. Everything else is travel.
func stepPause(mode string, stable int) time.Duration {
	if mode == scrollModeInner || stable > 0 {
		return scrollStepPause
	}
	return scrollTravelPause
}

// stepSize returns how far the next page step should travel, in pixels, or 0 for one viewport.
// It only ever enlarges the step: a viewport at a time is the default because it gives every lazy
// section a chance to intersect. The enlargement is the escape hatch for a document that grows
// faster than the remaining budget can walk, where a fixed step never reaches the bottom and the
// content anchored there - the SEO footer, on the site that motivated this - is never mounted.
func (l scrollLoop) stepSize(step, distance, viewport int, deadline time.Time) int {
	if distance <= 0 || viewport <= 0 {
		return 0
	}

	remaining := int(deadline.Sub(l.now()) / scrollStepPause)
	if left := scrollMaxSteps - step; left < remaining {
		remaining = left
	}
	if remaining <= 0 {
		return 0
	}

	if want := (distance + remaining - 1) / remaining; want > viewport {
		return want
	}
	return 0
}

// advancePhase decides what the next step should drive, and sets a stop reason when the pass is
// done. It returns the mode and settle counter to carry forward.
func (l scrollLoop) advancePhase(mode string, stable int, grew bool, state scrollState, innerSteps int, outcome *scrollOutcome) (string, int) {
	// An app shell whose document does not scroll has no page phase to settle, so go straight to
	// the container that does scroll.
	if mode == scrollModePage && !state.PageFound && state.InnerLeft {
		return scrollModeInner, 0
	}

	switch mode {
	case scrollModePage:
		if state.PageAtBottom && !grew {
			stable++
		} else {
			stable = 0
		}
		if stable < scrollSettleRounds {
			return scrollModePage, stable
		}
		// The page is done. A container that still has room may hold content the page never
		// reveals, so it gets the remainder of the budget.
		if state.InnerLeft && innerSteps < scrollInnerMaxSteps {
			return scrollModeInner, 0
		}
		outcome.StopReason = scrollStopSettled
		return scrollModePage, stable

	case scrollModeInner:
		if !state.InnerLeft || innerSteps >= scrollInnerMaxSteps {
			if !state.PageFound {
				outcome.StopReason = scrollStopSettled
				return scrollModeInner, stable
			}
			return scrollModePage, 0
		}
		// The container revealed something, which may have made the page taller; the page takes
		// priority again.
		if grew && state.PageFound {
			return scrollModePage, 0
		}
		return scrollModeInner, stable
	}

	return mode, stable
}

// scrollIfRequested runs the scroll pass when the request asks for it, and is a no-op otherwise,
// so an unconfigured host keeps exactly its current render behavior.
func (ci *ChromeInstance) scrollIfRequested(req *types.RenderRequest, metrics *types.PageMetrics) chromedp.ActionFunc {
	if !req.Scroll {
		return func(context.Context) error { return nil }
	}
	return ci.scrollPage(req, metrics)
}

// scrollPage scrolls the page to the bottom before capture so content gated on scroll events is
// present in the HTML. It never fails the render: every failure is logged and capture proceeds.
func (ci *ChromeInstance) scrollPage(req *types.RenderRequest, metrics *types.PageMetrics) chromedp.ActionFunc {
	loop := scrollLoop{
		step: func(ctx context.Context, mode string, stepPx int) (scrollState, error) {
			var state scrollState
			if err := chromedp.Evaluate(scrollStepJS(mode, stepPx), &state).Do(ctx); err != nil {
				return state, fmt.Errorf("scroll step evaluation failed: %w", err)
			}
			return state, nil
		},
		pause: func(ctx context.Context, d time.Duration) error {
			return chromedp.Sleep(d).Do(ctx)
		},
		restore: func(ctx context.Context) error {
			return chromedp.Evaluate(scrollRestoreJS, nil).Do(ctx)
		},
		now: time.Now,
	}

	return func(ctx context.Context) error {
		return ci.runScroll(ctx, loop, req, metrics)
	}
}

// runScroll executes one scroll pass, records the outcome on the render metrics and reports it.
// It always returns nil: a failed scroll must not fail the render.
func (ci *ChromeInstance) runScroll(ctx context.Context, loop scrollLoop, req *types.RenderRequest, metrics *types.PageMetrics) error {
	outcome, err := loop.run(ctx)

	metrics.ScrollPerformed = outcome.Performed
	metrics.ScrollNoTarget = outcome.NoTarget
	metrics.ScrollReachedBottom = outcome.ReachedBottom
	metrics.ScrollTarget = outcome.PageTarget
	metrics.ScrollInnerTarget = outcome.InnerTarget
	metrics.ScrollSteps = outcome.Steps
	metrics.ScrollInnerSteps = outcome.InnerSteps
	metrics.ScrollStopReason = outcome.StopReason
	metrics.ScrollDuration = outcome.Duration.Seconds()
	metrics.ScrollFinalHeight = outcome.FinalHeight

	// A step error can surface after its evaluation already moved the page, so the position has
	// to be restored on that path too. The restore script is a no-op when nothing scrolled.
	if outcome.Performed || err != nil {
		if restoreErr := loop.restore(ctx); restoreErr != nil {
			ci.logger.Debug("Failed to restore scroll position before capture",
				zap.String("request_id", req.RequestID),
				zap.Int("instance_id", ci.ID),
				zap.String("url", req.URL),
				zap.Error(restoreErr))
		}
	}

	switch {
	case err != nil:
		ci.logger.Warn("Scroll pass failed, capturing page as-is",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Int("steps", outcome.Steps),
			zap.Error(err))
	case outcome.NoTarget:
		// Nothing on the page was scrollable for several consecutive checks. On a host that
		// enabled scroll this is worth seeing, and it is otherwise invisible.
		ci.logger.Warn("Scroll enabled but no scrollable element found",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL))
	case !outcome.Performed:
		// The context was already done when the pass started, so the render is ending anyway.
		ci.logger.Debug("Scroll pass ended before it could look at the page",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Error(ctx.Err()))
	case !outcome.ReachedBottom:
		// The budget ran out partway down. The capture is still usable, but anything anchored to
		// the bottom of the page is missing from it.
		ci.logger.Warn("Scroll ended without reaching the bottom of the page",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.String("target", outcome.PageTarget),
			zap.String("stop_reason", outcome.StopReason),
			zap.Int("steps", outcome.Steps),
			zap.Int("final_height", outcome.FinalHeight),
			zap.Float64("duration", outcome.Duration.Seconds()))
	default:
		ci.logger.Debug("Scroll completed",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.String("target", outcome.PageTarget),
			zap.String("inner_target", outcome.InnerTarget),
			zap.String("stop_reason", outcome.StopReason),
			zap.Int("steps", outcome.Steps),
			zap.Int("inner_steps", outcome.InnerSteps),
			zap.Int("final_height", outcome.FinalHeight),
			zap.Float64("duration", outcome.Duration.Seconds()))
	}

	return nil
}

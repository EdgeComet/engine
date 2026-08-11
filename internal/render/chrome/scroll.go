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
	// scrollStepPause is the wait between steps, giving lazy sections time to mount.
	scrollStepPause = 400 * time.Millisecond
	// scrollMaxSteps caps the number of viewport-sized jumps on infinite-scroll pages.
	scrollMaxSteps = 25
	// scrollSettleRounds is how many consecutive steps must report an unchanged scrollHeight
	// while sitting at the bottom before the page counts as settled. Content keeps arriving
	// for a beat after the position stops advancing, so settling on position alone truncates it.
	scrollSettleRounds = 3
	// scrollMinContainerHeight is the smallest visible height a descendant may have to be
	// considered a scroll container, keeping small widgets out of the ranking.
	scrollMinContainerHeight = 200
	// scrollMinDelta is the smallest scrollHeight-clientHeight difference worth scrolling.
	scrollMinDelta = 50

	// scrollTargetWindowKey caches the detected scroller on the page so the DOM walk runs once.
	scrollTargetWindowKey = "__ecScrollTarget"
)

// scrollStepTemplate performs exactly one scroll step and reports the resulting state.
// Candidates are ranked by scrollable delta rather than taken in priority order: on pages where
// the document overflows slightly but the content lives in a much taller inner container,
// priority order picks the document and scrolls nothing useful.
// behavior:'instant' defeats CSS scroll-behavior:smooth, whose animation would outlast the step
// pause and make the next step read a stale position.
const scrollStepTemplate = `
(() => {
  const KEY = '%s';
  const MIN_DELTA = %d, MIN_HEIGHT = %d;

  const delta = el => (el && el.isConnected) ? el.scrollHeight - el.clientHeight : -1;

  function detect() {
    let best = null, bestDelta = MIN_DELTA;
    const consider = el => {
      const d = delta(el);
      if (d > bestDelta) { best = el; bestDelta = d; }
    };
    consider(document.scrollingElement || document.documentElement);
    consider(document.body);
    for (const el of document.querySelectorAll('*')) {
      if (el.clientHeight < MIN_HEIGHT || delta(el) <= bestDelta) continue;
      const oy = getComputedStyle(el).overflowY;
      if (oy === 'auto' || oy === 'scroll') consider(el);
    }
    return best;
  }

  let sc = window[KEY];
  if (!sc || !sc.isConnected) sc = window[KEY] = detect();
  if (!sc) return { found: false };

  sc.scrollTo({ top: sc.scrollTop + sc.clientHeight, behavior: 'instant' });

  return {
    found: true,
    target: sc === document.body ? 'body'
          : sc === (document.scrollingElement || document.documentElement) ? 'scrollingElement'
          : sc.tagName + (sc.className ? '.' + String(sc.className).split(' ')[0] : ''),
    scrollTop: Math.round(sc.scrollTop),
    clientHeight: sc.clientHeight,
    scrollHeight: sc.scrollHeight,
    atBottom: sc.scrollTop + sc.clientHeight >= sc.scrollHeight - 2,
  };
})()
`

// scrollRestoreTemplate returns the page to the top and drops the cached target, so the captured
// DOM serializes as top-of-page and a reused tab cannot inherit a stale element reference.
// behavior:'instant' matters here too: under CSS scroll-behavior:smooth a plain scrollTop
// assignment animates back to the top, firing scroll handlers while HTML extraction is running.
const scrollRestoreTemplate = `
(() => {
  const KEY = '%s';
  const sc = window[KEY];
  if (sc && sc.isConnected) sc.scrollTo({ top: 0, behavior: 'instant' });
  delete window[KEY];
})()
`

var (
	scrollStepJS    = fmt.Sprintf(scrollStepTemplate, scrollTargetWindowKey, scrollMinDelta, scrollMinContainerHeight)
	scrollRestoreJS = fmt.Sprintf(scrollRestoreTemplate, scrollTargetWindowKey)
)

// scrollState is the page state reported by a single scroll step.
type scrollState struct {
	Found        bool   `json:"found"`
	Target       string `json:"target"`
	ScrollTop    int    `json:"scrollTop"`
	ClientHeight int    `json:"clientHeight"`
	ScrollHeight int    `json:"scrollHeight"`
	AtBottom     bool   `json:"atBottom"`
}

// scrollOutcome summarizes a scroll pass, including a pass that stopped on a bound or an error.
type scrollOutcome struct {
	Performed bool
	// NoTarget separates the detection heuristic finding nothing from a pass that failed or was
	// cut short. Only the first tells anyone the heuristic needs work on this page.
	NoTarget    bool
	Target      string
	Steps       int
	FinalHeight int
	Duration    time.Duration
}

// scrollLoop drives the scroll from Go, one synchronous page evaluation per step.
// chromedp.Evaluate does not await promises, so an in-page loop would return immediately and let
// HTML capture race a scroll still running. Driving it from Go also makes the step and duration
// bounds enforceable and lets a cancelled context stop the loop.
// Every page interaction is injected, so the pass can be exercised without a browser.
type scrollLoop struct {
	step    func(ctx context.Context) (scrollState, error)
	pause   func(ctx context.Context, d time.Duration) error
	restore func(ctx context.Context) error
	now     func() time.Time
}

// run scrolls until the target settles or a bound is reached, returning what it achieved.
// A step error ends the pass and is returned with the partial outcome.
func (l scrollLoop) run(ctx context.Context) (scrollOutcome, error) {
	start := l.now()
	deadline := start.Add(types.ScrollMaxDuration)

	var outcome scrollOutcome
	var lastHeight, stable int

	for step := 0; step < scrollMaxSteps; step++ {
		if ctx.Err() != nil || l.now().After(deadline) {
			break
		}

		state, err := l.step(ctx)
		if err != nil {
			outcome.Duration = l.now().Sub(start)
			return outcome, err
		}
		if !state.Found {
			// Only a first step that finds nothing means the page has no scroller. A later one
			// means the container being driven was removed after the pass had already scrolled.
			outcome.NoTarget = !outcome.Performed
			break
		}

		outcome.Performed = true
		outcome.Target = state.Target
		outcome.Steps = step + 1
		outcome.FinalHeight = state.ScrollHeight

		if state.ScrollHeight == lastHeight && state.AtBottom {
			stable++
		} else {
			stable = 0
		}
		lastHeight = state.ScrollHeight

		if stable >= scrollSettleRounds {
			break
		}

		if err := l.pause(ctx, scrollStepPause); err != nil {
			break
		}
	}

	outcome.Duration = l.now().Sub(start)
	return outcome, nil
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
		step: func(ctx context.Context) (scrollState, error) {
			var state scrollState
			if err := chromedp.Evaluate(scrollStepJS, &state).Do(ctx); err != nil {
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
	metrics.ScrollTarget = outcome.Target
	metrics.ScrollSteps = outcome.Steps
	metrics.ScrollDuration = outcome.Duration.Seconds()
	metrics.ScrollFinalHeight = outcome.FinalHeight

	// A step error can surface after its evaluation already moved the page, so the position has
	// to be restored on that path too. The restore script is a no-op when detection never ran.
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
		// The detection heuristic found nothing to scroll. On a host that enabled scroll this
		// is the signature of the heuristic failing, and it is otherwise invisible.
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
	default:
		ci.logger.Debug("Scroll completed",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.String("target", outcome.Target),
			zap.Int("steps", outcome.Steps),
			zap.Int("final_height", outcome.FinalHeight),
			zap.Float64("duration", outcome.Duration.Seconds()))
	}

	return nil
}

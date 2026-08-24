package chrome

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

// prerenderPollInterval is how often the page is asked whether it is ready. The signal is a plain
// property read, so the cost of a tick is one round trip; anything slower simply adds latency to
// every render of a page that is already done.
const prerenderPollInterval = 50 * time.Millisecond

// prerenderProbeTimeout bounds a single sample. The page's own main thread answers an evaluation,
// so a page running a long synchronous task answers late or not at all, and the budget is only
// consulted between samples: without a bound here a page that stays busy past the render's hard
// deadline would end the render outright instead of soft timing out with the DOM it had.
const prerenderProbeTimeout = 10 * prerenderPollInterval

// prerenderRedirectProperty is the property a page sets instead of navigating, once it has been
// marked as being captured, when the URL it was given is a redirect or a not-found. The page then
// parks on its loading shell and never renders content, so waiting for readiness on such a URL
// burns the whole timeout for nothing.
const prerenderRedirectProperty = "prerenderRedirectUrl"

// prerenderSeedScript marks the page as being captured rather than browsed, before any of its own
// scripts run. This is the whole point of the feature: an application that implements this contract
// keeps its lazily resolved content unresolved, and never sets a readiness property, until it sees
// the flag.
const prerenderSeedScript = `
(() => {
  try { window.isPrerender = true; } catch (e) {}
})()
`

// prerenderPollTemplate reads the readiness property, named by the format argument, and the
// redirect property in one evaluation. Two evaluations per tick would double the round trips for
// a signal that is read together anyway.
//
// A property read is wrapped individually so a page that defines a throwing getter for one of them
// still yields the other, and the whole script is wrapped so any failure reads as "not ready,
// no redirect" rather than surfacing as an exception that aborts the wait.
const prerenderPollTemplate = `
(() => {
  try {
    var ready = false, redirectUrl = "";
    try { ready = !!window[%q]; } catch (e) {}
    try { redirectUrl = String(window.` + prerenderRedirectProperty + ` || ""); } catch (e) {}
    return { ready: ready, redirectUrl: redirectUrl };
  } catch (e) {
    return { ready: false, redirectUrl: "" };
  }
})()
`

// prerenderState is one sample of the page's readiness contract.
type prerenderState struct {
	Ready       bool   `json:"ready"`
	RedirectURL string `json:"redirectUrl"`
}

// prerenderProbe takes one sample. Injecting it keeps the poll loop testable without a browser.
type prerenderProbe func(ctx context.Context) (prerenderState, error)

// evaluatePrerenderState returns the Chrome-backed probe for one readiness property.
func evaluatePrerenderState(name string) prerenderProbe {
	script := fmt.Sprintf(prerenderPollTemplate, name)

	return func(ctx context.Context) (prerenderState, error) {
		var state prerenderState
		if err := chromedp.Evaluate(script, &state).Do(ctx); err != nil {
			return prerenderState{}, fmt.Errorf("prerender state evaluation failed: %w", err)
		}
		return state, nil
	}
}

// pollPrerenderReady samples the page until it reports itself ready, parks on a redirect, or the
// budget runs out. A parked redirect wins over readiness within a tick: a page can set both, and
// on the URLs where it does the redirect is the truth - the readiness that follows it describes
// content belonging to a different URL.
//
// A probe error is a sample that says nothing, not a failure: an evaluation issued while a
// navigation is in flight fails routinely, one a busy page leaves unanswered is abandoned, and
// failing the render over either would turn a page that is merely busy into a dead render.
func pollPrerenderReady(ctx context.Context, probe prerenderProbe, timeout time.Duration) (string, error) {
	ticker := time.NewTicker(prerenderPollInterval)
	defer ticker.Stop()

	deadline := time.Now().Add(timeout)

	for {
		if state, err := probeWithin(ctx, probe, prerenderProbeTimeout); err == nil {
			if state.RedirectURL != "" {
				return state.RedirectURL, nil
			}
			if state.Ready {
				return "", nil
			}
		}

		// Checked here rather than as a third case of the select below: a sample can outlast
		// several ticks, which would leave expiry and the next tick both ready and the exit a
		// coin toss.
		if !time.Now().Before(deadline) {
			return "", ErrWaitTimeout
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// probeWithin takes one sample under a deadline of its own, so the wait notices its budget whether
// or not the page answers. Abandoning an evaluation costs nothing beyond the answer: the browser
// context is untouched and the next tick asks again.
func probeWithin(ctx context.Context, probe prerenderProbe, limit time.Duration) (prerenderState, error) {
	probeCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	return probe(probeCtx)
}

// waitForPrerenderReady waits for the page to signal that its content is in the DOM, and returns
// the URL it parked on instead, if any. It returns ErrWaitTimeout on expiry, exactly as the
// lifecycle-event wait does, so the caller's soft-timeout handling is unchanged.
//
// The lifecycle recorder runs with no target event throughout: the wait is not driven by lifecycle
// events, but a render still has to carry its load and networkIdle timings. The readiness entry is
// recorded alongside them, which is what makes a page that stopped setting the property
// distinguishable from a page whose lifecycle event never fired.
func waitForPrerenderReady(ctx context.Context, name, frameID, loaderID string, timeout time.Duration,
	metrics *types.PageMetrics, timeOrigin int64) (string, error) {
	recorder, cancel := recordLifecycleEvents(ctx, frameID, loaderID, "", metrics, timeOrigin)
	defer cancel()

	redirectURL, err := pollPrerenderReady(ctx, evaluatePrerenderState(name), timeout)
	if err != nil || redirectURL != "" {
		return redirectURL, err
	}

	recorder.record(name, float64(time.Now().UnixMilli()-timeOrigin)/millisPerSecond)

	return "", nil
}

// seedPrerenderFlag installs window.isPrerender at document-start for a request that waits on a
// readiness property, and is a no-op for every other request, so a host that did not ask for it
// renders exactly as before.
func (ci *ChromeInstance) seedPrerenderFlag(req *types.RenderRequest) chromedp.ActionFunc {
	if !types.IsPrerenderWait(req.WaitFor) {
		return func(context.Context) error { return nil }
	}

	return func(ctx context.Context) error {
		if _, err := page.AddScriptToEvaluateOnNewDocument(prerenderSeedScript).Do(ctx); err != nil {
			// The render continues without the flag: the page will very likely never signal
			// readiness and the wait will time out, but a partial capture beats no capture.
			ci.logger.Warn("Failed to seed window.isPrerender, the page may never signal readiness",
				zap.String("request_id", req.RequestID),
				zap.Int("instance_id", ci.ID),
				zap.String("url", req.URL),
				zap.String("wait_for", req.WaitFor),
				zap.Error(err))
		}
		return nil
	}
}

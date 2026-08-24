package acceptance_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

const (
	// What the fixtures write into the DOM. The two flag reports carry the value of
	// window.isPrerender as a string, so a spec distinguishes seeded from unseeded rather
	// than merely asserting that the fixture ran.
	prerenderFlagAtFirstScript = "PRERENDER_AT_FIRST_SCRIPT:"
	prerenderFlagAtDOMReady    = "PRERENDER_AT_DOM_READY:"
	prerenderFlagInIframe      = "PRERENDER_IN_IFRAME:"

	prerenderFlagSeeded   = "true"
	prerenderFlagUnseeded = "undefined"

	prerenderImmediateMarker = "PRERENDER_IMMEDIATE_CONTENT"
	prerenderAjaxMarker      = "PRERENDER_AJAX_CONTENT"
	prerenderParkedMarker    = "PRERENDER_PARKED_SHELL"
	prerenderLateMarker      = "PRERENDER_LATE_CONTENT"
)

const (
	// The budgets the host rules give these paths. Named here because every timing assertion
	// below is about the relationship between the wall clock and the budget, and a bare
	// duration would stop meaning anything the moment one of those rules changed.
	prerenderGenerousTimeout = 20 * time.Second
	prerenderShortTimeout    = 3 * time.Second
	prerenderExtraWait       = 10 * time.Second

	// prerenderRenderOverhead is the slack a render costs beyond its wait: navigation,
	// extraction, and the gateway hop. Generous on purpose - every assertion using it
	// separates outcomes that differ by whole timeouts, not by fractions of a second.
	prerenderRenderOverhead = 5 * time.Second

	// prerenderRedirectExitBound bounds a render that leaves its wait on a parked redirect.
	// It sits far below the budget those rules hand the page, so a render finishing inside it
	// cannot have waited for anything.
	prerenderRedirectExitBound = 8 * time.Second
)

// The HAR debug endpoint is the only channel that exposes lifecycle events and the timed-out
// flag; the served body carries neither.
const (
	prerenderInternalBaseURL = "http://localhost:10071"
	prerenderInternalAuthKey = "test-auth-key-12345"

	prerenderHARClientTimeout = 90 * time.Second
)

// prerenderHAR is the slice of the debug HAR these specs read.
type prerenderHAR struct {
	Metadata struct {
		LifecycleEvents []struct {
			Name string `json:"name"`
		} `json:"lifecycleEvents"`
		RenderMetrics struct {
			TimedOut             bool   `json:"timedOut"`
			PrerenderRedirectURL string `json:"prerenderRedirectUrl"`
		} `json:"renderMetrics"`
		RequestConfig struct {
			WaitFor string `json:"waitFor"`
		} `json:"requestConfig"`
	} `json:"_metadata"`
}

// lifecycleNames flattens the recorded events for containment assertions.
func (h prerenderHAR) lifecycleNames() []string {
	names := make([]string, len(h.Metadata.LifecycleEvents))
	for i, event := range h.Metadata.LifecycleEvents {
		names[i] = event.Name
	}
	return names
}

// renderPrerenderHAR renders one fixture through the debug endpoint. The timeout is passed
// explicitly because a URL pattern timeout does not reach a debug render, so without it the
// render would run against the host budget instead of the one the spec is testing.
func renderPrerenderHAR(targetPath string, timeout time.Duration) prerenderHAR {
	GinkgoHelper()

	params := url.Values{}
	params.Set("url", testEnv.Config.TestPagesURL()+targetPath)
	params.Set("dimension", "desktop")
	params.Set("timeout", timeout.String())

	req, err := http.NewRequest("GET", prerenderInternalBaseURL+"/debug/har/render?"+params.Encode(), nil)
	Expect(err).To(BeNil())
	req.Header.Set("X-Internal-Auth", prerenderInternalAuthKey)

	client := &http.Client{Timeout: prerenderHARClientTimeout}
	resp, err := client.Do(req)
	Expect(err).To(BeNil())
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	Expect(err).To(BeNil())
	Expect(resp.StatusCode).To(Equal(200), string(body))

	var parsed prerenderHAR
	Expect(json.Unmarshal(body, &parsed)).To(Succeed())

	return parsed
}

var _ = Describe("Readiness Property Wait", Serial, func() {

	Context("when the wait is a readiness property", func() {

		// The spec that matters most. An application that implements this contract reads the flag
		// in its bootstrap and never re-reads it, so a flag installed after the page's own
		// first script is worth exactly nothing - and a spec that only checked the flag at
		// capture time would pass anyway.
		It("should seed the flag before the page's own first script runs", func() {
			By("Rendering a page that records the flag from its first inline script")
			resp := testEnv.RequestRender(testutil.PrerenderPathPrefix + "flag-report.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the flag was already set when that script ran")
			Expect(resp.Body).To(ContainSubstring(prerenderFlagAtFirstScript + prerenderFlagSeeded))

			By("Verifying it is still set later in the page lifecycle")
			Expect(resp.Body).To(ContainSubstring(prerenderFlagAtDOMReady + prerenderFlagSeeded))
		})

		It("should wait for a property the page sets only after an AJAX round trip", func() {
			By("Rendering a page whose readiness follows its content, and its content an origin request")
			resp := testEnv.RequestRender(testutil.PrerenderPathPrefix + "ajax-gated.html")

			By("Verifying the render succeeded")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderImmediateMarker))

			By("Verifying the gated content reached the capture")
			Expect(resp.Body).To(ContainSubstring(prerenderAjaxMarker))
		})
	})

	Context("when the page never signals readiness", func() {

		// The customer-bundle-changed case, and the reason the wait is soft. Nothing about a
		// missing property should cost the render its HTML.
		It("should capture the immediate DOM once the budget runs out", func() {
			By("Rendering a page that sets neither readiness property nor a parked redirect")
			resp := testEnv.RequestRender(testutil.PrerenderTimeoutPathPrefix + "never-ready.html")

			By("Verifying the render still succeeded with the DOM it had")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderImmediateMarker))

			By("Verifying it spent its budget rather than exiting early or running away")
			Expect(resp.Duration).To(BeNumerically(">", prerenderShortTimeout-time.Second))
			Expect(resp.Duration).To(BeNumerically("<", prerenderShortTimeout+prerenderRenderOverhead))
		})

		It("should mark the render as timed out", func() {
			By("Rendering the same page through the debug endpoint")
			har := renderPrerenderHAR(testutil.PrerenderTimeoutPathPrefix+"never-ready.html", prerenderShortTimeout)

			By("Verifying the readiness wait was the one that ran")
			Expect(har.Metadata.RequestConfig.WaitFor).To(Equal("prerenderContentReady"))

			By("Verifying expiry is reported as a soft timeout, not as a failure")
			Expect(har.Metadata.RenderMetrics.TimedOut).To(BeTrue())
		})

		// additional_wait exists to give late JavaScript a moment after a successful signal.
		// Paying it after an expiry would add its whole length to every render of a page that
		// stopped signalling, which is precisely when the budget is already gone.
		It("should skip the additional wait after the budget runs out", func() {
			By("Rendering the same page through a rule whose additional wait dwarfs its timeout")
			resp := testEnv.RequestRender(testutil.PrerenderExtraWaitPathPrefix + "never-ready.html")

			By("Verifying the render succeeded")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderImmediateMarker))

			By("Verifying the additional wait was not paid on top of the expired budget")
			Expect(resp.Duration).To(BeNumerically("<", prerenderShortTimeout+prerenderRenderOverhead),
				"a render that paid the %s additional wait could not finish before %s",
				prerenderExtraWait, prerenderShortTimeout+prerenderExtraWait)
		})
	})

	Context("when the page parks a redirect instead of rendering", func() {

		// Without this exit every not-found and every redirect URL on such a host costs a
		// full timeout and caches a loading shell. The budget on this rule is deliberately far
		// larger than the render needs, so finishing quickly can only mean the exit fired.
		It("should leave the wait as soon as the parked URL appears", func() {
			By("Rendering a page that parks a redirect and never signals readiness")
			resp := testEnv.RequestRender(testutil.PrerenderPathPrefix + "redirect-park.html")

			By("Verifying the render succeeded and captured the shell")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderParkedMarker))

			By("Verifying it finished immediately rather than burning the budget")
			Expect(resp.Duration).To(BeNumerically("<", prerenderRedirectExitBound),
				"the rule gives this page %s, and only the redirect exit can end it sooner",
				prerenderGenerousTimeout)
		})

		// The soft-404 shape: the application parks a redirect and then builds a page anyway.
		// Waiting for the readiness that follows would capture content belonging to a
		// different URL and serve it under this one.
		It("should leave on the parked URL even though readiness follows it", func() {
			By("Rendering a page that parks a redirect first and signals readiness seconds later")
			resp := testEnv.RequestRender(testutil.PrerenderPathPrefix + "redirect-late-ready.html")

			By("Verifying the render succeeded and captured the shell")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderParkedMarker))

			By("Verifying the wait did not stay for the content that arrives after the redirect")
			Expect(resp.Body).NotTo(ContainSubstring(prerenderLateMarker))
			Expect(resp.Duration).To(BeNumerically("<", prerenderRedirectExitBound))
		})

		// The additional wait is settling time for a page that signalled it had rendered. A
		// parked page never will, and the only thing it can build while the render sleeps is
		// the destination's content - which is what leaving on the parked URL exists to avoid
		// capturing, so paying the wait here would hand it back.
		It("should skip the additional wait after leaving on the parked URL", func() {
			By("Rendering the same page through a rule whose additional wait dwarfs its timeout")
			resp := testEnv.RequestRender(testutil.PrerenderExtraWaitPathPrefix + "redirect-late-ready.html")

			By("Verifying the render succeeded and captured the shell")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderParkedMarker))

			By("Verifying the additional wait was not paid on top of the redirect exit")
			Expect(resp.Duration).To(BeNumerically("<", prerenderRedirectExitBound),
				"a render that paid the %s additional wait could not finish that quickly",
				prerenderExtraWait)

			By("Verifying the content that arrives during that wait stayed out of the capture")
			Expect(resp.Body).NotTo(ContainSubstring(prerenderLateMarker))
		})

		// The parked URL is the handoff to origin status handling, and the render metrics are
		// where it lands. A render that leaves on the parked URL but records nothing looks
		// exactly like a page that had nothing to render.
		It("should record the parked URL on the render metrics", func() {
			By("Rendering the parked page through the debug endpoint")
			har := renderPrerenderHAR(testutil.PrerenderPathPrefix+"redirect-park.html", prerenderGenerousTimeout)

			By("Verifying the readiness wait ran and did not expire")
			Expect(har.Metadata.RequestConfig.WaitFor).To(Equal("prerenderContentReady"))
			Expect(har.Metadata.RenderMetrics.TimedOut).To(BeFalse())

			By("Verifying the URL the page parked reached the render metrics")
			Expect(har.Metadata.RenderMetrics.PrerenderRedirectURL).
				To(Equal(testutil.PrerenderPathPrefix + "destination.html"))
		})
	})

	Context("when the wait is a lifecycle event", func() {

		It("should leave window.isPrerender unset", func() {
			By("Rendering the flag-reporting page through a rule that waits on networkIdle")
			resp := testEnv.RequestRender(testutil.PrerenderLifecyclePathPrefix + "flag-report.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the page never saw the flag, at any point in its lifecycle")
			Expect(resp.Body).To(ContainSubstring(prerenderFlagAtFirstScript + prerenderFlagUnseeded))
			Expect(resp.Body).To(ContainSubstring(prerenderFlagAtDOMReady + prerenderFlagUnseeded))
		})

		// The load-bearing assumption behind seeding at all: the flag is installed on a tab,
		// and every render gets a new one. If any path reused a tab, one host's flag would
		// reach the next host's render.
		It("should not inherit the flag from an earlier render on the same Chrome instance", func() {
			By("Rendering once through the readiness wait, which seeds the flag")
			seeded := testEnv.RequestRender(testutil.PrerenderPathPrefix + "flag-report.html")
			expectRendered(seeded)
			Expect(seeded.Body).To(ContainSubstring(prerenderFlagAtFirstScript + prerenderFlagSeeded))

			By("Rendering without the flag once per Chrome instance in the pool")
			// The pool hands instances out in turn and takes them back at the end of the queue,
			// so the instance that carried the seeded render is reached again within one lap.
			// Looping the whole pool is what makes the spec deterministic instead of hopeful.
			for lap := 0; lap < testEnv.Config.RenderService.Chrome.PoolSize; lap++ {
				plain := testEnv.RequestRender(testutil.PrerenderLifecyclePathPrefix + "flag-report.html")
				expectRendered(plain)
				Expect(plain.Body).To(ContainSubstring(prerenderFlagAtFirstScript+prerenderFlagUnseeded),
					"lap %d saw a flag left behind by an earlier render", lap)
			}
		})
	})

	Context("when the page embeds a same-origin iframe", func() {

		// Pins observed behaviour rather than asserting a requirement: the flag is installed
		// for the whole target, so every frame in it starts out marked. Harmless for the
		// applications this exists for, and a change here should be a deliberate one.
		It("should seed the flag inside the iframe as well", func() {
			By("Rendering a page whose parent reads the flag out of its child frame")
			resp := testEnv.RequestRender(testutil.PrerenderPathPrefix + "iframe-scope.html")

			By("Verifying the render succeeded")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(prerenderImmediateMarker))

			By("Verifying the child frame was marked too")
			Expect(resp.Body).To(ContainSubstring(prerenderFlagInIframe + prerenderFlagSeeded))
		})
	})

	Context("lifecycle analytics", func() {

		// The readiness wait is not driven by lifecycle events, so recording them is a
		// separate job that has to keep running throughout. Losing it would leave such a render
		// with no load or networkIdle timings anywhere, and would make a page that
		// stopped signalling indistinguishable from a page whose lifecycle event never fired.
		It("should record the page lifecycle events under a readiness wait", func() {
			By("Rendering through the debug endpoint with room for the page to signal")
			har := renderPrerenderHAR(testutil.PrerenderPathPrefix+"ajax-gated.html", prerenderGenerousTimeout)

			By("Verifying the readiness wait was the one that ran, and it did not expire")
			Expect(har.Metadata.RequestConfig.WaitFor).To(Equal("prerenderContentReady"))
			Expect(har.Metadata.RenderMetrics.TimedOut).To(BeFalse())

			By("Verifying the real lifecycle events were still recorded")
			Expect(har.lifecycleNames()).To(ContainElements("DOMContentLoaded", "load"))

			By("Verifying the readiness signal was recorded beside them")
			Expect(har.lifecycleNames()).To(ContainElement("prerenderContentReady"))
		})

		It("should record nothing extra under a lifecycle wait", func() {
			By("Rendering the same page through a rule that waits on networkIdle")
			har := renderPrerenderHAR(testutil.PrerenderLifecyclePathPrefix+"ajax-gated.html", prerenderGenerousTimeout)

			By("Verifying the lifecycle wait ran to its event")
			Expect(har.Metadata.RequestConfig.WaitFor).To(Equal("networkIdle"))
			Expect(har.Metadata.RenderMetrics.TimedOut).To(BeFalse())

			By("Verifying the recorded events are the browser's own, unchanged")
			Expect(har.lifecycleNames()).To(ContainElements("DOMContentLoaded", "load", "networkIdle"))
			Expect(har.lifecycleNames()).NotTo(ContainElement("prerenderContentReady"))
		})
	})
})

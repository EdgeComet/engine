package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

const (
	// Markers the fixtures write into the DOM. The lazy marker only ever reaches the page
	// through an IntersectionObserver callback, so its presence means the page really scrolled.
	scrollLazyMarker      = "LAZY_SECTION_LOADED"
	scrollAboveFoldMarker = "ABOVE_FOLD_CONTENT"
	scrollShortMarker     = "SHORT_PAGE_CONTENT"
	scrollAjaxMarker      = "AJAX_CONTENT_LOADED"
)

func expectRendered(resp *TestResponse) {
	Expect(resp.Error).To(BeNil())
	Expect(resp.StatusCode).To(Equal(200))
	Expect(resp.Headers.Get("EC-Source")).To(Equal("render"))
}

var _ = Describe("Scroll Before Capture", Serial, func() {

	Context("when the document does not scroll and body is the scroll container", func() {

		It("should omit scroll-gated content when scroll is disabled", func() {
			By("Rendering the page with the default configuration")
			resp := testEnv.RequestRender(testutil.ScrollPathPrefix + "body-scroller.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the above-fold content is present")
			Expect(resp.Body).To(ContainSubstring(scrollAboveFoldMarker))

			By("Verifying the scroll-gated section never mounted")
			Expect(resp.Body).NotTo(ContainSubstring(scrollLazyMarker))
		})

		It("should capture scroll-gated content when scroll is enabled", func() {
			By("Rendering the same page through a scroll-enabled rule")
			resp := testEnv.RequestRender(testutil.ScrollEnabledPathPrefix + "body-scroller.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the above-fold content is still present")
			Expect(resp.Body).To(ContainSubstring(scrollAboveFoldMarker))

			By("Verifying the scroll-gated section reached the capture")
			Expect(resp.Body).To(ContainSubstring(scrollLazyMarker))

			By("Verifying the links the section carries reached the capture")
			Expect(resp.Body).To(ContainSubstring("Lazy internal link"))
		})
	})

	Context("when the page asks for animated scrolling", func() {

		// The page sets scroll-behavior: smooth, so every scroll it is asked to perform is
		// animated unless the caller opts out. An animation outlasting the step pause would
		// make each step read a stale position and the pass fall short of the bottom.
		It("should still reach the scroll-gated content", func() {
			By("Rendering a smooth-scrolling page with the pass enabled")
			resp := testEnv.RequestRender(testutil.ScrollEnabledPathPrefix + "smooth-scroller.html")

			By("Verifying the render succeeded")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(scrollAboveFoldMarker))

			By("Verifying the section at the bottom reached the capture")
			Expect(resp.Body).To(ContainSubstring(scrollLazyMarker))
		})
	})

	Context("when the render is cached", func() {

		// What ends up in the cache is what every later bot sees, so the post-scroll HTML
		// has to survive the write and the read, not just the render.
		It("should cache the post-scroll HTML and serve it on the next request", func() {
			url := testutil.ScrollCachedPathPrefix + "body-scroller.html"

			By("Rendering the page through a scroll-enabled rule that caches")
			first := testEnv.RequestRender(url)
			expectRendered(first)
			Expect(first.Body).To(ContainSubstring(scrollLazyMarker))

			By("Requesting the same URL again")
			second := testEnv.RequestRender(url)
			Expect(second.Error).To(BeNil())
			Expect(second.StatusCode).To(Equal(200))
			Expect(second.Headers.Get("EC-Source")).To(Equal("render_cache"))

			By("Verifying the cached entry carries the scroll-gated section")
			Expect(second.Body).To(ContainSubstring(scrollLazyMarker))
			Expect(second.Body).To(ContainSubstring(scrollAboveFoldMarker))
		})
	})

	Context("when the lifecycle event never fires", func() {

		// The regression guard for this feature. The pages that need scrolling are the ones
		// whose feeds poll continuously, so networkIdle never fires and every render ends on
		// the soft timeout. A scroll placed behind that timeout flag would never run here.
		It("should still scroll after a soft timeout", func() {
			By("Rendering a page that polls forever, against a short render timeout")
			resp := testEnv.RequestRender(testutil.ScrollEnabledPathPrefix + "polling.html")

			By("Verifying the render succeeded despite the soft timeout")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(scrollAboveFoldMarker))

			By("Verifying the scroll ran anyway and the gated section reached the capture")
			Expect(resp.Body).To(ContainSubstring(scrollLazyMarker))
		})
	})

	Context("when the scrollable element is an inner container", func() {

		// The document overflows the viewport a little while the panel overflows it hugely.
		// Detection ranks by scrollable delta, so it picks the panel; priority ordering would
		// pick the document and scroll nothing that matters.
		It("should scroll the tallest scrollable element rather than the document", func() {
			By("Rendering a page whose content lives in an inner scroller")
			resp := testEnv.RequestRender(testutil.ScrollEnabledPathPrefix + "inner-scroller.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the section gated inside the inner container reached the capture")
			Expect(resp.Body).To(ContainSubstring(scrollLazyMarker))
		})
	})

	Context("when nothing on the page is scrollable", func() {

		It("should render successfully without spending the scroll budget", func() {
			By("Rendering a page that fits inside the viewport")
			resp := testEnv.RequestRender(testutil.ScrollEnabledPathPrefix + "no-scroller.html")

			By("Verifying the render succeeded")
			expectRendered(resp)

			By("Verifying the page content is intact")
			Expect(resp.Body).To(ContainSubstring(scrollShortMarker))
			Expect(resp.Body).To(ContainSubstring(scrollAjaxMarker))

			By("Verifying the pass gave up instead of looping to its budget")
			// Detection reports that it found nothing on the first step and the loop stops
			// there, so the render cannot have absorbed the whole scroll budget.
			Expect(resp.Duration).To(BeNumerically("<", types.ScrollMaxDuration))
		})
	})

	Context("when scroll is disabled", func() {

		It("should not change an ordinary page", func() {
			By("Rendering a page with nothing to lazy-load")
			resp := testEnv.RequestRender(testutil.ScrollPathPrefix + "no-scroller.html")

			By("Verifying the render succeeded quickly")
			expectRendered(resp)
			Expect(resp.Body).To(ContainSubstring(scrollShortMarker))
			Expect(resp.Body).To(ContainSubstring(scrollAjaxMarker))
			Expect(resp.Duration).To(BeNumerically("<", 30*time.Second))
		})
	})
})

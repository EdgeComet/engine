package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

// Date capture writes its evidence into the page_seo blob on the request event. This
// suite has no sink that carries page_seo: the file emitter formats a fixed set of
// scalar placeholders and cache metadata persists only title and index_status, so the
// dates array itself cannot be asserted from here. What these specs pin is the input
// side of the contract that only a live pipeline can exercise: the origin's date markup
// survives rendering, and the origin's Last-Modified header reaches the raw response
// header map that the edge hands to content processing. The capture that reads that map
// is covered in internal/edge/orchestrator and internal/edge/events.
var _ = Describe("Date Capture Inputs", Serial, func() {

	Context("Render path", func() {
		It("should keep origin date markup and the Last-Modified header", func() {
			By("Rendering a page carrying JSON-LD, meta and time date signals")
			resp := testEnv.RequestRender("/dates-test/render/article")
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("render"), "Should be freshly rendered")

			By("Verifying the rendered HTML the processor sees still carries every date source")
			Expect(resp.Body).To(ContainSubstring("DATES_TEST_PAGE"))
			Expect(resp.Body).To(ContainSubstring(`"datePublished": "` + testutil.DatesTestPublished + `"`))
			Expect(resp.Body).To(ContainSubstring(`"dateModified": "` + testutil.DatesTestModified + `"`))
			Expect(resp.Body).To(ContainSubstring(`<meta property="article:published_time" content="` + testutil.DatesTestPublished + `">`))
			Expect(resp.Body).To(ContainSubstring(`<time datetime="` + testutil.DatesTestPublished + `">`))

			By("Verifying the origin's Last-Modified header reached the edge")
			Expect(resp.Headers.Get(testutil.DatesTestLastModified)).To(Equal(testutil.DatesTestLastModifiedValue),
				"The header the render path captures must be the origin's verbatim value")
		})
	})

	Context("Bypass path", func() {
		It("should keep origin date markup and the Last-Modified header", func() {
			By("Bypassing the same page")
			resp := testEnv.RequestRender("/dates-test/bypass/article")
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("bypass"), "Should be served without rendering")

			By("Verifying the fetched HTML carries every date source")
			Expect(resp.Body).To(ContainSubstring("DATES_TEST_PAGE"))
			Expect(resp.Body).To(ContainSubstring(`"datePublished": "` + testutil.DatesTestPublished + `"`))
			Expect(resp.Body).To(ContainSubstring(`<meta property="article:published_time" content="` + testutil.DatesTestPublished + `">`))

			By("Verifying the origin's Last-Modified header reached the edge")
			Expect(resp.Headers.Get(testutil.DatesTestLastModified)).To(Equal(testutil.DatesTestLastModifiedValue),
				"The header the bypass path captures must be the origin's verbatim value")
		})
	})
})

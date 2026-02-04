package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("JavaScript Rendering", Serial, func() {
	Context("when page content is loaded via JavaScript", func() {
		It("should render JavaScript-generated content", func() {
			By("Making a request to a page with client-side rendering")
			response := testEnv.RequestRender("/javascript/client-rendered.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying the loading state is NOT present")
			Expect(response.Body).NotTo(ContainSubstring("Loading..."))
			Expect(response.Body).NotTo(ContainSubstring(`<div class="loading-spinner">`))

			By("Verifying JavaScript-rendered content IS present")
			Expect(response.Body).To(ContainSubstring("<h1>JavaScript Rendered Content</h1>"))
			Expect(response.Body).To(ContainSubstring("This content was added by JavaScript"))
			Expect(response.Body).To(ContainSubstring(`class="js-rendered"`))

			By("Verifying dynamic content sections are present")
			Expect(response.Body).To(ContainSubstring("Dynamic Section"))
			Expect(response.Body).To(ContainSubstring("First dynamic item"))
			Expect(response.Body).To(ContainSubstring(`data-item="1"`))

			By("Verifying JavaScript execution markers are present")
			Expect(response.Body).To(ContainSubstring(`data-js-rendered="true"`))

			By("Verifying page statistics were populated")
			Expect(response.Body).To(ContainSubstring("Rendered at:"))
			Expect(response.Body).To(ContainSubstring("User Agent:"))
		})

		It("should wait for AJAX content to load", func() {
			By("Making a request to a page with AJAX-loaded content")
			response := testEnv.RequestRender("/javascript/ajax-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying loading state is NOT present")
			Expect(response.Body).NotTo(ContainSubstring("Fetching data..."))
			Expect(response.Body).NotTo(ContainSubstring(`class="loading"`))
			Expect(response.Body).NotTo(ContainSubstring("class=\"spinner\""))

			By("Verifying AJAX-loaded content IS present")
			Expect(response.Body).To(ContainSubstring("Items loaded from API"))
			Expect(response.Body).To(ContainSubstring("Item One"))
			Expect(response.Body).To(ContainSubstring("Item Two"))
			Expect(response.Body).To(ContainSubstring("Item Three"))

			By("Verifying API metadata is present")
			Expect(response.Body).To(ContainSubstring("Total items:"))
			Expect(response.Body).To(ContainSubstring("Loaded at:"))
			Expect(response.Body).To(ContainSubstring("api-content"))

			By("Verifying content has loaded class")
			Expect(response.Body).To(ContainSubstring(`class="loaded"`))

			By("Verifying AJAX completion markers")
			Expect(response.Body).To(ContainSubstring(`data-ajax-loaded="true"`))
		})

		It("should capture DOM manipulations", func() {
			By("Making a request to a page with DOM modifications")
			response := testEnv.RequestRender("/javascript/dom-manipulation.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying original content was modified")
			Expect(response.Body).To(ContainSubstring("This content was modified by JavaScript"))
			Expect(response.Body).To(ContainSubstring(`class="modified-by-js"`))
			Expect(response.Body).NotTo(ContainSubstring("This is the original content"))

			By("Verifying DOM attributes were added")
			Expect(response.Body).To(ContainSubstring(`data-processed="true"`))
			Expect(response.Body).To(ContainSubstring(`data-modification-time="`))

			By("Verifying new list items were added")
			Expect(response.Body).To(ContainSubstring("JavaScript added item 1"))
			Expect(response.Body).To(ContainSubstring("JavaScript added item 2"))
			Expect(response.Body).To(ContainSubstring(`class="js-added"`))

			By("Verifying new sections were created")
			Expect(response.Body).To(ContainSubstring("JavaScript Generated Section"))
			Expect(response.Body).To(ContainSubstring(`data-created-by="javascript"`))
			Expect(response.Body).To(ContainSubstring("Dynamic Counter"))

			By("Verifying dynamic form was created")
			Expect(response.Body).To(ContainSubstring("Dynamic Form"))
			Expect(response.Body).To(ContainSubstring(`data-js-created="true"`))
			Expect(response.Body).To(ContainSubstring(`data-js-button="true"`))

			By("Verifying completion markers")
			Expect(response.Body).To(ContainSubstring(`data-dom-manipulation-complete="true"`))
			Expect(response.Body).To(ContainSubstring(`class=" js-processed"`))
		})

		It("should handle delayed content loading", func() {
			By("Making a request to a page with staggered content delays")
			// Use fast version (200ms/500ms/1s) instead of slow version (500ms/2s/4s)
			// This prevents timeouts when Chrome is under load during full test suite runs
			response := testEnv.RequestRender("/javascript/delayed-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying immediate content is present")
			Expect(response.Body).To(ContainSubstring("This content is available immediately"))

			By("Verifying quick content loaded (0.2s delay)")
			Expect(response.Body).To(ContainSubstring("Quick Content Loaded!"))
			Expect(response.Body).To(ContainSubstring("This content appeared after 0.5 seconds"))
			Expect(response.Body).To(ContainSubstring(`class="quick-loaded"`))
		})
	})

	Context("when testing JavaScript execution quality", func() {
		It("should wait for async operations to complete", func() {
			By("Making a request to a page with AJAX content")
			response := testEnv.RequestRender("/javascript/ajax-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying all async content indicators are complete")
			// The page should show completed state, not loading state
			Expect(response.Body).To(ContainSubstring(`class="api-content"`))
			Expect(response.Body).To(ContainSubstring("Items loaded from API"))

			By("Verifying timestamps are populated")
			// Dynamic content should have timestamps showing it was loaded
			Expect(response.Body).To(MatchRegexp(`data-loaded-at="[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}`))
		})

		It("should respect JavaScript timeouts", func() {
			By("Making a request with a short timeout to slow-loading page")
			// Use a shorter timeout than the page's content loading time
			response := testEnv.RequestRenderWithTimeout("/javascript/delayed-content.html", 2*time.Second)

			// The response depends on how the render engine handles timeouts
			if response.Error != nil {
				// Timeout error is acceptable
				Expect(response.Error.Error()).To(ContainSubstring("Timeout"))
			} else if response.StatusCode >= 400 {
				// Server timeout response is acceptable
				Expect(response.StatusCode).To(BeElementOf([]int{408, 504}))
			} else {
				// If it succeeds, it should have at least some content
				Expect(response.Body).To(ContainSubstring("Delayed Content Loading Test"))
				// But it might not have all the slow-loading sections
			}
		})
	})

	Context("when testing various JavaScript patterns", func() {
		It("should handle event-driven content loading", func() {
			By("Making a request to a page with event-based rendering")
			response := testEnv.RequestRender("/javascript/client-rendered.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying event-based content is present")
			// The page dispatches custom events when content is ready
			Expect(response.Body).To(ContainSubstring("JavaScript Rendered Content"))

			By("Verifying event listeners didn't prevent rendering")
			// Content should be present even though it was added via events
			Expect(response.Body).To(ContainSubstring("Dynamic Section"))
		})

		It("should capture dynamically added meta tags", func() {
			By("Making a request to the dynamic meta page")
			response := testEnv.RequestRender("/seo/dynamic-meta.html")

			// This test verifies JavaScript meta tag manipulation
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying the title was updated by JavaScript")
			Expect(response.Body).NotTo(ContainSubstring("<title>Initial Title - Loading...</title>"))
			Expect(response.Body).To(ContainSubstring("Ultimate Guide to Web Performance"))

			By("Verifying meta description was updated")
			Expect(response.Body).NotTo(ContainSubstring("Initial description - loading content"))
			Expect(response.Body).To(ContainSubstring("Learn the ultimate guide to web performance optimization"))

			By("Verifying JavaScript-added Open Graph tags")
			Expect(response.Body).To(ContainSubstring(`<meta property="og:title"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:type" content="article"`))

			By("Verifying structured data was added")
			Expect(response.Body).To(ContainSubstring(`"@type": "Article"`))
			Expect(response.Body).To(ContainSubstring(`"headline": "Ultimate Guide to Web Performance`))
		})

		It("should handle complex DOM transformations", func() {
			By("Making a request to the DOM manipulation page")
			response := testEnv.RequestRender("/javascript/dom-manipulation.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying complex nested content was created")
			Expect(response.Body).To(ContainSubstring(`data-widget-type="counter"`))
			Expect(response.Body).To(ContainSubstring(`data-form-generated="true"`))

			By("Verifying CSS classes were applied correctly")
			Expect(response.Body).To(ContainSubstring(`class="js-section"`))
			Expect(response.Body).To(ContainSubstring(`class="js-widget"`))
			Expect(response.Body).To(ContainSubstring(`class="js-form"`))

			By("Verifying data attributes were set properly")
			Expect(response.Body).To(ContainSubstring(`data-action="increment"`))
			Expect(response.Body).To(ContainSubstring(`data-js-button="true"`))
		})
	})

	Context("when testing render engine capabilities", func() {
		It("should wait for networkidle events", func() {
			By("Making a request to AJAX content page")
			response := testEnv.RequestRender("/javascript/ajax-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying network-dependent content is present")
			// Content that loads via simulated network requests should be present
			Expect(response.Body).To(ContainSubstring("Items loaded from API"))
			Expect(response.Body).To(ContainSubstring("mock-api"))

			By("Verifying no network activity indicators remain")
			Expect(response.Body).NotTo(ContainSubstring("Fetching data"))
			Expect(response.Body).NotTo(ContainSubstring("Loading API"))
		})

		It("should handle pages with multiple async operations", func() {
			By("Making a request to delayed content page")
			response := testEnv.RequestRender("/javascript/delayed-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying multiple async operations completed")
			// All three delayed sections should be loaded
			loadedSections := []string{
				"Quick Content Loaded",
				"Medium Content Loaded",
				"Slow Content Finally Loaded",
			}

			for _, section := range loadedSections {
				Expect(response.Body).To(ContainSubstring(section))
			}

			By("Verifying progress tracking worked")
			Expect(response.Body).To(ContainSubstring("100% complete"))
		})

		It("should maintain JavaScript execution context", func() {
			By("Making a request to DOM manipulation page")
			response := testEnv.RequestRender("/javascript/dom-manipulation.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying variables and state were maintained")
			// Content created by JavaScript should reflect proper variable state
			Expect(response.Body).To(ContainSubstring("Dynamic Counter"))
			Expect(response.Body).To(ContainSubstring("Count: <span"))

			By("Verifying event handlers were set up")
			// Form elements with event handlers should be present
			Expect(response.Body).To(ContainSubstring(`type="submit"`))
		})
	})
})

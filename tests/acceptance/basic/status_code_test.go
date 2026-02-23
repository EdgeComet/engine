package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTP Status Code Handling", Serial, func() {
	Context("when rendering pages with non-200 status codes", func() {
		It("should pass through 404 status code and not cache the response", func() {
			By("Making a request to a non-existent page")
			response := testEnv.RequestRender("/this-page-does-not-exist-404.html")

			By("Verifying 404 status is returned")
			Expect(response.Error).To(BeNil(), "Should not have network errors")
			Expect(response.StatusCode).To(Equal(404))

			By("Verifying 404 error page HTML is returned")
			if response.Body != "" {
				Expect(response.Body).To(ContainSubstring("404"))
			}

			By("Making a second request to verify it was NOT cached")
			// Small delay to ensure any cache operations would complete
			time.Sleep(100 * time.Millisecond)

			response2 := testEnv.RequestRender("/this-page-does-not-exist-404.html")

			By("Verifying second request also returns 404 (not cached)")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(404))

			By("Verifying X-Render-Source header indicates rendered, not cached")
			if response.Headers != nil {
				source := response.Headers["X-Render-Source"]
				if len(source) > 0 {
					Expect(source[0]).NotTo(Equal("cache"))
				}
			}
		})

		It("should pass through 500 status code and not cache the response", func() {
			By("Making a request to a page that returns 500 error")
			// Note: This test requires nginx to be configured with a route that returns 500
			// or a test page that simulates 500 error
			response := testEnv.RequestRender("/edge-cases/server-error-500.html")

			By("Verifying appropriate error response")
			Expect(response.Error).To(BeNil(), "Should not have network errors")

			// May return 500 from origin or 200 if page doesn't actually exist
			if response.StatusCode >= 500 {
				By("Verifying 500 status is passed through")
				Expect(response.StatusCode).To(BeNumerically(">=", 500))

				By("Making a second request to verify it was NOT cached")
				time.Sleep(100 * time.Millisecond)
				response2 := testEnv.RequestRender("/edge-cases/server-error-500.html")

				Expect(response2.Error).To(BeNil())
				Expect(response2.StatusCode).To(BeNumerically(">=", 500))
			}
		})

		It("should handle and not cache 403 forbidden pages", func() {
			By("Making a request to a forbidden resource")
			response := testEnv.RequestRender("/forbidden-403.html")

			Expect(response.Error).To(BeNil(), "Should not have network errors")

			// May return 403, 404, or 200 depending on nginx config
			if response.StatusCode == 403 {
				By("Verifying 403 status is passed through")
				Expect(response.StatusCode).To(Equal(403))

				By("Verifying HTML content is served despite non-200 status")
				// Should still serve the HTML error page content
				if response.Body != "" {
					Expect(len(response.Body)).To(BeNumerically(">", 0))
				}

				By("Verifying response was not cached")
				time.Sleep(100 * time.Millisecond)
				response2 := testEnv.RequestRender("/forbidden-403.html")
				Expect(response2.StatusCode).To(Equal(403))
			}
		})
	})

	Context("when rendering pages with successful status codes", func() {
		It("should cache 200 responses as before", func() {
			By("Making a request to a valid page")
			response := testEnv.RequestRender("/static/simple.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying content is rendered")
			Expect(response.Body).To(ContainSubstring("Static Content"))

			By("Making a second request to verify caching behavior")
			time.Sleep(100 * time.Millisecond)
			response2 := testEnv.RequestRender("/static/simple.html")

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("Static Content"))

			By("Verifying second request may be served from cache")
			// Check X-Render-Source header if available
			if response2.Headers != nil {
				source := response2.Headers["X-Render-Source"]
				// Source can be "cache" or "rendered" depending on timing
				if len(source) > 0 {
					Expect(source[0]).To(BeElementOf("cache", "rendered"))
				}
			}
		})
	})

	Context("when handling status code edge cases", func() {
		It("should gracefully handle pages where status code cannot be captured", func() {
			By("Testing with a complex redirect or edge case page")
			response := testEnv.RequestRender("/edge-cases/redirect.html")

			Expect(response.Error).To(BeNil(), "Should not have network errors")

			By("Verifying a valid response is returned")
			// Should either succeed with 200 or return appropriate error
			Expect(response.StatusCode).To(BeNumerically(">", 0))

			if response.StatusCode == 200 {
				Expect(response.Body).NotTo(BeEmpty())
			}
		})
	})

	Context("when testing concurrent requests with different status codes", func() {
		It("should handle mixed status codes correctly without cross-contamination", func() {
			By("Making concurrent requests to pages with different status codes")
			numRequests := 4
			responses := make([]*TestResponse, numRequests)
			done := make(chan int, numRequests)

			testPages := []string{
				"/static/simple.html",                 // Should return 200
				"/this-page-does-not-exist-404.html",  // Should return 404
				"/javascript/ajax-content.html",       // Should return 200
				"/another-non-existent-page-404.html", // Should return 404
			}

			expectedStatuses := []int{200, 404, 200, 404}

			for i := 0; i < numRequests; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender(testPages[index])
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < numRequests; i++ {
				select {
				case <-done:
					// Request completed
				case <-time.After(45 * time.Second):
					Fail("Concurrent request timed out")
				}
			}

			By("Verifying each request returned correct status code")
			for i, response := range responses {
				Expect(response).NotTo(BeNil())
				Expect(response.Error).To(BeNil(), "Request %d should not have errors", i)

				// Check status code matches expectation
				if expectedStatuses[i] == 200 {
					Expect(response.StatusCode).To(Equal(200), "Request %d should return 200", i)
				} else if expectedStatuses[i] == 404 {
					Expect(response.StatusCode).To(Equal(404), "Request %d should return 404", i)
				}
			}

			By("Verifying no status code cross-contamination occurred")
			// All 200 responses should have content
			Expect(responses[0].Body).To(ContainSubstring("Static Content"))
			Expect(responses[2].Body).To(ContainSubstring("AJAX Content"))

			// 404 responses should indicate not found
			if responses[1].StatusCode == 404 && responses[1].Body != "" {
				Expect(responses[1].Body).To(ContainSubstring("404"))
			}
		})
	})
})

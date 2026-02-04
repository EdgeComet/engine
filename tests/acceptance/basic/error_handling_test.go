package acceptance_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Error Handling", Serial, func() {
	Context("when rendering fails due to page issues", func() {
		It("should handle slow-loading pages with timeout", func() {
			By("Making a request to a page with slow AJAX requests")
			// HTTP client timeout must be longer than render timeout (15s) to receive the response
			response := testEnv.RequestRenderWithTimeout(
				"/edge-cases/slow-loading.html",
				20*time.Second,
			)

			By("Verifying page renders successfully despite slow AJAX requests")
			Expect(response.Error).To(BeNil(), "Should not have network errors")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 even with failed AJAX requests")

			By("Verifying rendered content includes page structure with AJAX requests")
			Expect(response.Body).To(ContainSubstring("Slow Loading Page"))
			Expect(response.Body).To(ContainSubstring("AJAX"))
			Expect(response.Body).To(ContainSubstring("Network Requests Status"))

			By("Verifying AJAX requests failed as expected")

			if strings.Contains(response.Body, "Section 3 - Even Slower") {
				Expect(response.Body).To(ContainSubstring("AJAX request failed"))
			}

			By("Verifying response time was within render timeout")
			Expect(response.Duration).To(BeNumerically("<=", 20*time.Second))
		})

		It("should handle pages with JavaScript execution errors", func() {
			By("Making a request to a page with JavaScript runtime error")
			response := testEnv.RequestRender("/edge-cases/javascript-error.html")

			By("Verifying page renders successfully despite JavaScript error")
			Expect(response.Error).To(BeNil(), "Should not have network errors")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 even with JS errors")

			By("Verifying static HTML content is present")
			Expect(response.Body).To(ContainSubstring("JavaScript Error Test Page"))
			Expect(response.Body).To(ContainSubstring("<h1>JavaScript Error Test</h1>"))
			Expect(response.Body).To(ContainSubstring("This is static content that should always be visible"))

			By("Verifying dynamic content added before error is present")
			Expect(response.Body).To(ContainSubstring("Dynamic Content Added by JavaScript"))
			Expect(response.Body).To(ContainSubstring("This content was added by JavaScript before the error occurred"))

			By("Verifying test information section is present")
			Expect(response.Body).To(ContainSubstring("Test Information"))
			Expect(response.Body).To(ContainSubstring("This page tests how the render service handles JavaScript runtime errors"))
		})

	})

	Context("when handling edge case scenarios", func() {
		It("should handle extremely large responses", func() {
			By("Making a request to a page that might generate large output")
			response := testEnv.RequestRender("/javascript/dom-manipulation.html")

			Expect(response.Error).To(BeNil())

			if response.StatusCode == 200 {
				By("Verifying large response is handled properly")
				Expect(len(response.Body)).To(BeNumerically(">", 1000))

				By("Verifying content is complete")
				Expect(response.Body).To(ContainSubstring("DOM Manipulation Test"))
				Expect(response.Body).To(ContainSubstring("JavaScript Generated Section"))
			}
		})

		It("should handle concurrent error conditions", func() {
			By("Making multiple requests to problematic pages concurrently")
			numRequests := 3
			responses := make([]*TestResponse, numRequests)
			done := make(chan int, numRequests)

			// Mix of potentially problematic pages
			testPages := []string{
				"/edge-cases/malformed.html",
				"/javascript/delayed-content.html",
				"/static/simple.html",
			}

			for i := 0; i < numRequests; i++ {
				go func(index int) {
					page := testPages[index%len(testPages)]
					responses[index] = testEnv.RequestRender(page)
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < numRequests; i++ {
				select {
				case <-done:
					// Request completed
				case <-time.After(45 * time.Second):
					Fail("Concurrent request timed out after 45 seconds")
				}
			}

			By("Verifying error handling under load")
			completedRequests := 0
			for i, response := range responses {
				if response != nil {
					completedRequests++
					Expect(response.Error).To(BeNil(),
						"Request %d should not have network errors", i)

					// Should get some response, even if error
					Expect(response.StatusCode).To(BeNumerically(">", 0))
				}
			}

			By("Verifying reasonable success rate under error conditions")
			Expect(completedRequests).To(BeNumerically(">=", 3),
				"At least 3 out of 3 concurrent requests should complete")
		})

		It("should maintain system stability under error conditions", func() {
			By("Creating error conditions and then testing recovery")

			// Make a request that might fail
			errorResponse := testEnv.RequestRenderWithTimeout("/edge-cases/slow-loading.html", 1*time.Second)
			_ = errorResponse // May succeed or fail

			By("Verifying system can still handle normal requests")
			normalResponse := testEnv.RequestRender("/static/simple.html")

			Expect(normalResponse.Error).To(BeNil())

			// System should recover and handle normal requests
			if normalResponse.StatusCode == 200 {
				Expect(normalResponse.Body).To(ContainSubstring("Static Content"))
			} else {
				// If still failing, should be a reasonable error
				Expect(normalResponse.StatusCode).To(BeNumerically(">=", 500))
			}

			By("Verifying response times remain reasonable")
			Expect(normalResponse.Duration).To(BeNumerically("<", 30*time.Second))
		})
	})

	Context("when testing resource limits and constraints", func() {
		It("should handle memory-intensive pages appropriately", func() {
			By("Making a request to a page with complex DOM manipulation")
			response := testEnv.RequestRender("/javascript/dom-manipulation.html")

			Expect(response.Error).To(BeNil())

			By("Verifying complex page rendered successfully")
			Expect(response.Body).To(ContainSubstring("DOM Manipulation Test"))
			Expect(response.Body).To(ContainSubstring("JavaScript Generated Section"))

			By("Verifying memory-intensive operations completed")
			Expect(response.Body).To(ContainSubstring("Dynamic Counter"))
			Expect(response.Body).To(ContainSubstring("Dynamic Form"))

		})

		It("should respect rendering time limits", func() {
			By("Making a request with reasonable timeout expectations")
			start := time.Now()
			response := testEnv.RequestRender("/javascript/delayed-content.html")
			elapsed := time.Since(start)

			Expect(response.Error).To(BeNil())

			By("Verifying reasonable response time")
			// Should complete within a reasonable time (30 seconds max for complex pages)
			Expect(elapsed).To(BeNumerically("<", 30*time.Second))

			By("Verifying timeout was respected")

			// If successful, should have content
			Expect(response.Body).To(ContainSubstring("Delayed Content Loading Test"))
		})

		It("should handle network-related errors gracefully", func() {
			By("Testing network error handling")
			// Request to a page that simulates network requests
			response := testEnv.RequestRender("/javascript/ajax-content.html")

			Expect(response.Error).To(BeNil())

			By("Verifying network operations in page were handled")
			if response.StatusCode == 200 {
				// Should have either completed network ops or failed gracefully
				Expect(response.Body).To(ContainSubstring("AJAX Content Loading Test"))

				// Network content should either be loaded or show error state
				if response.Body != "" {
					networkCompleted := strings.Contains(response.Body, "Items loaded from API") ||
						strings.Contains(response.Body, "Fetching data")
					Expect(networkCompleted).To(BeTrue())
				}
			}
		})
	})
})

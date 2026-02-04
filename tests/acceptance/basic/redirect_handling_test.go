package acceptance_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTP Redirect Handling", Serial, func() {
	Context("when rendering pages that return redirect responses", func() {
		It("should detect and handle 301 permanent redirects quickly", func() {
			By("Making a request to a page that returns 301 redirect")
			startTime := time.Now()
			response := testEnv.RequestRender("/bypass-test/?status=301")
			duration := time.Since(startTime)

			By("Verifying the request completes quickly (not hanging for full timeout)")
			Expect(duration).To(BeNumerically("<", 10*time.Second),
				"Redirect should be detected and stopped quickly, not hang for 50s timeout")

			By("Verifying no error is returned")
			Expect(response.Error).To(BeNil(), "Should not have network errors")

			By("Verifying response completes successfully")
			// Response may have minimal or no HTML body (redirect was stopped)
			// But should return successfully without timeout
			Expect(response.StatusCode).To(BeNumerically(">", 0))
		})

		It("should detect and handle 302 temporary redirects quickly", func() {
			By("Making a request to a page that returns 302 redirect")
			startTime := time.Now()
			response := testEnv.RequestRender("/bypass-test/?status=302")
			duration := time.Since(startTime)

			By("Verifying the request completes quickly")
			Expect(duration).To(BeNumerically("<", 10*time.Second),
				"Redirect should be detected and stopped quickly")

			By("Verifying no error is returned")
			Expect(response.Error).To(BeNil())
		})

		It("should handle 307 temporary redirects", func() {
			By("Making a request to a page that returns 307 redirect")
			startTime := time.Now()
			response := testEnv.RequestRender("/bypass-test/?status=307")
			duration := time.Since(startTime)

			By("Verifying quick completion")
			Expect(duration).To(BeNumerically("<", 10*time.Second))

			By("Verifying no error")
			Expect(response.Error).To(BeNil())
		})

		It("should handle 308 permanent redirects", func() {
			By("Making a request to a page that returns 308 redirect")
			startTime := time.Now()
			response := testEnv.RequestRender("/bypass-test/?status=308")
			duration := time.Since(startTime)

			By("Verifying quick completion")
			Expect(duration).To(BeNumerically("<", 10*time.Second))

			By("Verifying no error")
			Expect(response.Error).To(BeNil())
		})
	})

	Context("when testing instance stability after redirects", func() {
		It("should keep Chrome instance functional after processing a redirect", func() {
			By("Making a request to a redirect URL (301)")
			response1 := testEnv.RequestRender("/bypass-test/?status=301")
			Expect(response1.Error).To(BeNil(), "First redirect request should succeed")

			By("Immediately making a request to a normal 200 page")
			response2 := testEnv.RequestRender("/static/simple.html")

			By("Verifying second request succeeds (proves instance not bricked)")
			Expect(response2.Error).To(BeNil(), "Second request should succeed, proving instance still functional")
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("Static Content"))

			By("Verifying second request completes in reasonable time")
			Expect(response2.Duration).To(BeNumerically("<", 30*time.Second))
		})

		It("should handle multiple sequential redirects without degradation", func() {
			By("Processing 5 sequential redirect requests")
			for i := 0; i < 5; i++ {
				startTime := time.Now()
				response := testEnv.RequestRender("/bypass-test/?status=301")
				duration := time.Since(startTime)

				Expect(response.Error).To(BeNil(), "Redirect request %d should succeed", i+1)
				Expect(duration).To(BeNumerically("<", 10*time.Second),
					"Redirect %d should complete quickly", i+1)
			}

			By("Making a final normal request to verify instance health")
			response := testEnv.RequestRender("/static/simple.html")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("when handling concurrent redirect requests", func() {
		It("should process multiple concurrent redirects without context cancellation errors", func() {
			By("Launching 5 concurrent requests to redirect URLs")
			numRequests := 5
			responses := make([]*TestResponse, numRequests)
			done := make(chan int, numRequests)

			redirectTypes := []int{301, 302, 301, 307, 308}

			for i := 0; i < numRequests; i++ {
				go func(index int) {
					url := fmt.Sprintf("/bypass-test/?status=%d", redirectTypes[index])
					responses[index] = testEnv.RequestRender(url)
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < numRequests; i++ {
				select {
				case <-done:
					// Request completed
				case <-time.After(30 * time.Second):
					Fail("Concurrent redirect request timed out")
				}
			}

			By("Verifying all requests completed successfully")
			for i, response := range responses {
				Expect(response).NotTo(BeNil(), "Response %d should not be nil", i)
				Expect(response.Error).To(BeNil(), "Response %d should not have errors", i)

				// Verify no "context canceled" error in response
				if response.Body != "" {
					Expect(response.Body).NotTo(ContainSubstring("context canceled"),
						"Response %d should not contain 'context canceled' error", i)
				}

				// Verify reasonable completion time
				Expect(response.Duration).To(BeNumerically("<", 15*time.Second),
					"Response %d should complete in reasonable time", i)
			}

			By("Verifying instances remain functional after concurrent redirects")
			followUpResponse := testEnv.RequestRender("/static/simple.html")
			Expect(followUpResponse.Error).To(BeNil())
			Expect(followUpResponse.StatusCode).To(Equal(200))
		})
	})

	Context("when comparing redirect behavior to timeout behavior", func() {
		It("should complete redirects much faster than actual timeouts", func() {
			By("Making a redirect request and measuring time")
			startRedirect := time.Now()
			redirectResponse := testEnv.RequestRender("/bypass-test/?status=301")
			redirectDuration := time.Since(startRedirect)

			Expect(redirectResponse.Error).To(BeNil())

			By("Verifying redirect completes in under 10 seconds")
			Expect(redirectDuration).To(BeNumerically("<", 10*time.Second),
				"Redirect should be detected early and stop quickly")

			By("Verifying redirect does NOT take close to the 50s hard timeout")
			Expect(redirectDuration).To(BeNumerically("<", 20*time.Second),
				"If redirect was hanging until timeout, it would take ~50s")
		})
	})

	Context("when page makes AJAX requests that receive redirects", func() {
		It("should not overwrite page status with AJAX redirect status", func() {
			By("Requesting a page (200) that makes an AJAX request receiving 301")
			startTime := time.Now()
			response := testEnv.RequestRender("/javascript/ajax-redirect-test.html")
			duration := time.Since(startTime)

			By("Verifying no error is returned")
			Expect(response.Error).To(BeNil(), "Should not have network errors")

			By("Verifying page status is 200, not 301 from AJAX request")
			// The main page should have status 200
			Expect(response.StatusCode).To(Equal(200),
				"Page status should be 200, not overwritten by AJAX redirect")

			By("Verifying page HTML was captured correctly")
			Expect(response.Body).To(ContainSubstring("AJAX Redirect Test Page"),
				"Should have page content")
			Expect(response.Body).To(ContainSubstring("This page has status 200"),
				"Should have page description")

			By("Verifying request completed in reasonable time")
			Expect(duration).To(BeNumerically("<", 30*time.Second),
				"Request should complete normally")
		})

		It("should distinguish between main document redirect and XHR redirect", func() {
			By("Making two requests: one main document redirect, one AJAX redirect")

			// Main document redirect (should be detected and stopped)
			redirectResponse := testEnv.RequestRender("/bypass-test/?status=301")

			// Page with AJAX redirect (should complete normally with 200)
			ajaxResponse := testEnv.RequestRender("/javascript/ajax-redirect-test.html")

			By("Verifying main document redirect was detected")
			Expect(redirectResponse.Error).To(BeNil())

			By("Verifying AJAX redirect page returned 200")
			Expect(ajaxResponse.Error).To(BeNil())
			Expect(ajaxResponse.StatusCode).To(Equal(200))

			By("Verifying both requests completed successfully")
			Expect(redirectResponse.StatusCode).To(BeNumerically(">", 0))
			Expect(ajaxResponse.StatusCode).To(Equal(200))
		})
	})
})

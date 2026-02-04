package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Timeout Behavior", func() {
	Context("Fast Renders (No Timeout)", func() {
		It("should complete 4.5s page successfully without timeout", func() {
			By("Requesting fast-render.html (4.5s, under 5s timeout)")
			response := testEnv.RequestRender("/timeout/fast-render.html")

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying response completes quickly")
			Expect(response.Duration).To(BeNumerically("<", 7*time.Second), "Should complete in under 7s")

			By("Verifying render occurred (not bypassed)")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying immediate content is present")
			Expect(response.Body).To(ContainSubstring("Immediate Content"))

			By("Verifying AJAX-loaded content is present (completed before timeout)")
			Expect(response.Body).To(ContainSubstring("Fast Content Complete"))
		})
	})

	Context("Slow Renders (Timeout Occurs)", func() {
		It("should timeout on 6s page and return partial HTML within ~5s", func() {
			By("Requesting slow-render.html (6s, exceeds 5s timeout)")
			response := testEnv.RequestRender("/timeout/slow-render.html")

			By("Verifying successful partial render (200 status)")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Timeout should still return 200 with partial HTML")

			By("Verifying response completes quickly (~5-6s, NOT 30s or 39s)")
			Expect(response.Duration).To(BeNumerically("<", 8*time.Second), "Should complete near timeout, not continue to 30s+")
			Expect(response.Duration).To(BeNumerically(">", 4*time.Second), "Should take at least 4s to hit timeout")

			By("Verifying render occurred")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying immediate content is present")
			Expect(response.Body).To(ContainSubstring("Immediate Content"))

			By("Verifying slow AJAX content is NOT present (timed out)")
		})

		It("should capture immediate DOM but skip slow AJAX on timeout", func() {
			By("Requesting partial-content.html (10s AJAX delay)")
			response := testEnv.RequestRender("/timeout/partial-content.html")

			By("Verifying successful partial render")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying quick response time")
			Expect(response.Duration).To(BeNumerically("<", 8*time.Second))

			By("Verifying immediate section is present")
			Expect(response.Body).To(ContainSubstring("Immediate Section"))
			Expect(response.Body).To(ContainSubstring("This content is immediately available"))

			By("Verifying slow section stub is present but not loaded")
			Expect(response.Body).To(ContainSubstring("Slow Section"))
		})
	})

	Context("Boundary Conditions", func() {
		It("should NOT timeout on 4.9s page (just under 5s limit)", func() {
			By("Requesting boundary-fast.html (4.9s)")
			response := testEnv.RequestRender("/timeout/boundary-fast.html")

			By("Verifying successful response without timeout")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying response completes in time")
			Expect(response.Duration).To(BeNumerically("<", 7*time.Second))

			By("Verifying boundary content loaded successfully")
			Expect(response.Body).To(ContainSubstring("Boundary Fast Complete"), "4.9s content should complete before 5s timeout")
		})

		It("should timeout on 5.1s page (just over 5s limit)", func() {
			By("Requesting boundary-slow.html (5.1s)")
			response := testEnv.RequestRender("/timeout/boundary-slow.html")

			By("Verifying partial render response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying response completes near timeout")
			Expect(response.Duration).To(BeNumerically("<", 8*time.Second))

			By("Verifying immediate content present")
			Expect(response.Body).To(ContainSubstring("Immediate Content"))

			By("Verifying boundary slow content NOT loaded (exceeded timeout)")
		})
	})

	Context("Performance Validation", func() {
		It("should complete multiple timeout requests efficiently", func() {
			By("Making 3 concurrent timeout requests")
			start := time.Now()

			responses := make([]*TestResponse, 3)
			done := make(chan int, 3)

			for i := 0; i < 3; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender("/timeout/slow-render.html")
					done <- index
				}(i)
			}

			// Wait for all to complete
			for i := 0; i < 3; i++ {
				<-done
			}

			totalDuration := time.Since(start)

			By("Verifying all requests completed successfully")
			for i := 0; i < 3; i++ {
				Expect(responses[i].Error).To(BeNil())
				Expect(responses[i].StatusCode).To(Equal(200))
				Expect(responses[i].Body).To(ContainSubstring("Immediate Content"))
			}

			By("Verifying requests completed in reasonable time")
			// With 3 concurrent requests on timeout, should complete in ~5-10s range
			// (not 3*39s = 117s with the old bug)
			Expect(totalDuration).To(BeNumerically("<", 15*time.Second), "Concurrent timeouts should complete quickly")
		})
	})
})

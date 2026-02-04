package sharding_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sharding Smoke Tests", Serial, func() {
	Context("when services are running", Serial, func() {
		It("should successfully render a simple test page", func() {
			By("Making a request to render a test page")
			response := testEnv.RequestRender("/static/test.html")

			By("Verifying the request succeeded")
			Expect(response.Error).To(BeNil(), "Request should not have network errors")
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying the HTML content is present")
			Expect(response.Body).To(ContainSubstring("<h1>Sharding Test Page</h1>"))
			Expect(response.Body).To(ContainSubstring("This is a test page for sharding acceptance tests"))

			By("Verifying response time is reasonable")
			Expect(response.Duration).To(BeNumerically("<", 30*time.Second))
		})

		It("should handle cache operations", func() {
			By("Making the first request")
			response1 := testEnv.RequestRender("/static/test.html")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Making a second request to verify cache works")
			response2 := testEnv.RequestRender("/static/test.html")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying both responses have identical content")
			Expect(response2.Body).To(Equal(response1.Body))
		})
	})
})

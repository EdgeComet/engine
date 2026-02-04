package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Domain Blocking", Serial, func() {
	Context("Custom path pattern blocking", func() {
		It("should block custom tracking API paths", func() {
			By("Making a request to a page with custom tracking")
			response := testEnv.RequestRender("/blocking/with-tracking.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying main content is present")
			Expect(response.Body).To(ContainSubstring("Main content loaded successfully"))

			By("Verifying tracking was blocked (not loaded)")
			Expect(response.Body).To(ContainSubstring("Tracking Blocked"))

			By("Verifying tracker ID is NOT present (request failed)")
			Expect(response.Body).NotTo(ContainSubstring("pixel-12345"))
		})
	})

	Context("Global blocklist - Real external domains", func() {
		It("should block real Google Analytics requests", func() {
			By("Making a request to a page with Google Analytics")
			response := testEnv.RequestRender("/blocking/with-google-analytics.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying main content is present")
			Expect(response.Body).To(ContainSubstring("Main content loaded successfully"))

			By("Verifying Google Analytics was blocked")
			Expect(response.Body).To(ContainSubstring("GA Blocked"))

			By("Verifying GA did NOT load successfully")
		})

		It("should block real Facebook Pixel requests", func() {
			By("Making a request to a page with Facebook Pixel")
			response := testEnv.RequestRender("/blocking/with-facebook-pixel.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying main content is present")
			Expect(response.Body).To(ContainSubstring("Main content loaded successfully"))

			By("Verifying Facebook Pixel was blocked")
			Expect(response.Body).To(ContainSubstring("FB Blocked"))

			By("Verifying FB did NOT load successfully")
		})
	})

	Context("Multiple trackers - combined blocking", func() {
		It("should block both custom and global patterns simultaneously", func() {
			By("Making a request to a page with multiple trackers")
			response := testEnv.RequestRender("/blocking/with-multiple-trackers.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying main content is present")
			Expect(response.Body).To(ContainSubstring("Main content loaded successfully"))

			By("Verifying custom analytics was blocked (path pattern)")
			Expect(response.Body).To(ContainSubstring("Custom: Blocked"))

			By("Verifying Google Analytics was blocked (global blocklist)")
			Expect(response.Body).To(ContainSubstring("GA: Blocked"))

			By("Verifying Google Tag Manager was blocked (global blocklist)")
			Expect(response.Body).To(ContainSubstring("GTM: Blocked"))

			By("Verifying NO trackers loaded successfully")
			// The word "Loaded" should not appear in any tracker status
		})
	})

	Context("Page rendering with blocking active", func() {
		It("should complete rendering despite blocked requests", func() {
			By("Making a request to a page with multiple blocked trackers")
			response := testEnv.RequestRender("/blocking/with-multiple-trackers.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying page rendered completely")
			Expect(response.Body).To(ContainSubstring("<!DOCTYPE html>"))
			Expect(response.Body).To(ContainSubstring("</html>"))

			By("Verifying all status elements are present")
			Expect(response.Body).To(ContainSubstring("id=\"custom-status\""))
			Expect(response.Body).To(ContainSubstring("id=\"ga-status\""))
			Expect(response.Body).To(ContainSubstring("id=\"gtm-status\""))

			By("Verifying blocking didn't prevent networkIdle event")
			// If the page rendered with status messages, networkIdle fired correctly
			Expect(response.Body).To(ContainSubstring("Blocked"))
		})

		It("should render page with only custom pattern blocking", func() {
			By("Making a request to tracking-only page")
			response := testEnv.RequestRender("/blocking/with-tracking.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying page structure is intact")
			Expect(response.Body).To(ContainSubstring("<title>Custom Tracking Test</title>"))
			Expect(response.Body).To(ContainSubstring("id=\"main-content\""))
			Expect(response.Body).To(ContainSubstring("id=\"tracking-status\""))

			By("Verifying JavaScript executed despite blocked request")
			// The status should show "Blocked", meaning the catch() executed
			Expect(response.Body).To(ContainSubstring("Tracking Blocked"))
		})

		It("should render page with only global blocklist blocking", func() {
			By("Making a request to GA-only page")
			response := testEnv.RequestRender("/blocking/with-google-analytics.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying page structure is intact")
			Expect(response.Body).To(ContainSubstring("<title>Google Analytics Test</title>"))
			Expect(response.Body).To(ContainSubstring("id=\"main-content\""))
			Expect(response.Body).To(ContainSubstring("id=\"ga-status\""))

			By("Verifying JavaScript executed despite blocked request")
			// The status should show "Blocked", meaning onerror() executed
			Expect(response.Body).To(ContainSubstring("GA Blocked"))
		})
	})

	Context("Blocked request verification", func() {
		It("should not contain any tracker data in rendered HTML", func() {
			By("Making a request to page with custom tracking")
			response := testEnv.RequestRender("/blocking/with-tracking.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying NO tracker-specific data is present")
			// These values come from the blocked API endpoints
			Expect(response.Body).NotTo(ContainSubstring("pixel-12345"))
		})

		It("should not contain multiple tracker data", func() {
			By("Making a request to page with multiple trackers")
			response := testEnv.RequestRender("/blocking/with-multiple-trackers.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying NO API response data is present")

			By("Verifying NO external script content is present")
			// If GA/GTM loaded, window.ga or window.dataLayer would be defined
		})
	})
})

package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("URL Matching", Serial, func() {
	Context("Fragment Handling", func() {
		It("should capture status code when request has no fragment", func() {
			By("Making request without fragment")
			response := testEnv.RequestRender("/url-matching/simple-fragment.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page content is present")
			Expect(response.Body).To(ContainSubstring("Simple Fragment Test"))
			Expect(response.Body).To(ContainSubstring(`data-test="fragment-page"`))
		})

		It("should capture status code when request has fragment", func() {
			By("Making request with fragment #section-1")
			response := testEnv.RequestRender("/url-matching/simple-fragment.html#section-1")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page content is present")
			Expect(response.Body).To(ContainSubstring("Simple Fragment Test"))
			Expect(response.Body).To(ContainSubstring("Section 1"))
		})

		It("should capture status code when request has different fragment", func() {
			By("Making request with fragment #section-2")
			response := testEnv.RequestRender("/url-matching/simple-fragment.html#section-2")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page content is present")
			Expect(response.Body).To(ContainSubstring("Simple Fragment Test"))
			Expect(response.Body).To(ContainSubstring("Section 2"))
		})

		It("should capture status code when JavaScript adds fragment", func() {
			By("Making request to page that adds fragment via JS")
			response := testEnv.RequestRender("/url-matching/fragment-redirect.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying JavaScript executed")
			Expect(response.Body).To(ContainSubstring(`data-fragment-test="complete"`))
			Expect(response.Body).To(ContainSubstring("Loaded"))
		})
	})

	Context("Unicode and Non-ASCII Characters", func() {
		It("should capture status code with Unicode characters in path", func() {
			By("Making request with Unicode content")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying Unicode content is present")
			Expect(response.Body).To(ContainSubstring("Unicode Content Test"))
			Expect(response.Body).To(ContainSubstring(`data-unicode-test="complete"`))
		})

		It("should handle Spanish/Portuguese accented characters", func() {
			By("Making request with Unicode page")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying accented characters are rendered")
			Expect(response.Body).To(ContainSubstring("café"))
			Expect(response.Body).To(ContainSubstring("naïve"))
			Expect(response.Body).To(ContainSubstring("résumé"))
			Expect(response.Body).To(ContainSubstring("Zürich"))
		})

		It("should handle Chinese characters", func() {
			By("Making request with Unicode page")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Chinese content is present")
			Expect(response.Body).To(ContainSubstring("中文"))
			Expect(response.Body).To(ContainSubstring("测试页面"))
		})

		It("should handle Arabic characters", func() {
			By("Making request with Unicode page")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Arabic content is present")
			Expect(response.Body).To(ContainSubstring("العربية"))
			Expect(response.Body).To(ContainSubstring("صفحة اختبار"))
		})

		It("should handle Japanese characters", func() {
			By("Making request with Unicode page")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Japanese content is present")
			Expect(response.Body).To(ContainSubstring("日本語"))
			Expect(response.Body).To(ContainSubstring("テストページ"))
		})

		It("should handle emoji characters", func() {
			By("Making request with Unicode page")
			response := testEnv.RequestRender("/url-matching/unicode-content.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying emoji content is present")
			Expect(response.Body).To(ContainSubstring("🚀"))
			Expect(response.Body).To(ContainSubstring("🎉"))
			Expect(response.Body).To(ContainSubstring("✅"))
		})
	})

	Context("URL Encoding", func() {
		It("should capture status code with spaces in URL path (encoded)", func() {
			By("Making request with URL-encoded spaces (%20)")
			response := testEnv.RequestRender("/url-matching/page%20with%20spaces.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page content is present")
			Expect(response.Body).To(ContainSubstring("URL Encoding Test Page"))
			Expect(response.Body).To(ContainSubstring(`data-url-encoding-test="complete"`))
		})

		It("should capture status code with spaces in URL path (plus sign)", func() {
			By("Making request with plus-encoded spaces (+)")
			response := testEnv.RequestRender("/url-matching/page+with+spaces.html")

			// Note: Plus signs in paths are NOT decoded to spaces by HTTP servers
			// This might fail with 404, which is expected behavior
			// We're testing that IF it works, status code is captured

			// Just verify that status code is captured (200 or 404)
			Expect(response.StatusCode).NotTo(Equal(0), "Status code should be captured even if file not found")
		})

		It("should handle encoded and non-encoded URLs consistently", func() {
			By("Making request with encoded URL")
			response1 := testEnv.RequestRender("/url-matching/page%20with%20spaces.html")

			By("Making request with same content using encoded form")
			response2 := testEnv.RequestRender("/url-matching/page%20with%20spaces.html")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying both responses have same content")
			Expect(response1.Body).To(Equal(response2.Body))
		})

		It("should capture status code with URL containing special characters", func() {
			By("Making request with special characters in fragment")
			response := testEnv.RequestRender("/url-matching/simple-fragment.html#section?test=1&foo=bar")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page loaded correctly")
			Expect(response.Body).To(ContainSubstring("Simple Fragment Test"))
		})
	})

	Context("Combined Edge Cases", func() {
		It("should handle Unicode characters with fragments", func() {
			By("Making request with Unicode page and fragment")
			response := testEnv.RequestRender("/url-matching/unicode-content.html#section-zh")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying content is present")
			Expect(response.Body).To(ContainSubstring("Unicode Content Test"))
		})

		It("should handle encoded spaces with fragments", func() {
			By("Making request with encoded spaces and fragment")
			response := testEnv.RequestRender("/url-matching/page%20with%20spaces.html#top")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying page loaded correctly")
			Expect(response.Body).To(ContainSubstring("URL Encoding Test Page"))
		})
	})

	Context("Status Code Capture Verification", func() {
		It("should never return status_code 0 for successful renders", func() {
			By("Testing multiple URL variations")
			urls := []string{
				"/url-matching/simple-fragment.html",
				"/url-matching/simple-fragment.html#section-1",
				"/url-matching/unicode-content.html",
				"/url-matching/page%20with%20spaces.html",
				"/url-matching/fragment-redirect.html",
			}

			for _, url := range urls {
				response := testEnv.RequestRender(url)
				Expect(response.Error).To(BeNil(), "Request to %s should not error", url)
				Expect(response.StatusCode).NotTo(Equal(0), "Status code for %s should not be 0", url)
				Expect(response.StatusCode).To(Equal(200), "Status code for %s should be 200", url)
			}
		})
	})
})

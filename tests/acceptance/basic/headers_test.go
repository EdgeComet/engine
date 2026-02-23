package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Headers - Response Header Filtering", Serial, func() {
	// Test response headers (configured in headers.response):
	// - Content-Type
	// - Cache-Control
	// - X-Custom-Header
	//
	// Test non-allowed headers (should be filtered):
	// - X-Secret-Header
	// - Set-Cookie
	// - X-Internal-Debug

	Context("Rendered Response Headers", func() {
		It("should include allowed response headers with correct values in rendered response", func() {
			url := "/headers-test/render/basic"

			By("Making a render request to the headers test page")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying X-Render-Source is rendered")
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("rendered"), "Should be freshly rendered")

			By("Verifying allowed response headers are present with correct values")
			Expect(resp.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should be text/html")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control value mismatch")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")

			By("Verifying non-allowed headers are filtered out")
			Expect(resp.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
			Expect(resp.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered")
			Expect(resp.Headers.Get("X-Internal-Debug")).To(BeEmpty(), "X-Internal-Debug should be filtered")
		})

		It("should filter out non-safe headers in rendered response", func() {
			url := "/headers-test/render/filtered"

			By("Making a render request")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying non-safe headers are NOT present")
			Expect(resp.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
			Expect(resp.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered")
			Expect(resp.Headers.Get("X-Internal-Debug")).To(BeEmpty(), "X-Internal-Debug should be filtered")
		})

		It("should handle case-insensitive header matching", func() {
			url := "/headers-test/render/case-variation/case-test"

			By("Making a render request where origin sends lowercase header names")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying headers match case-insensitively")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("lowercase-header-name"), "Header should match case-insensitively")
		})
	})

	Context("Cached Response Headers", func() {
		It("should persist safe headers in cache and serve them", func() {
			url := "/headers-test/render/cache-persist"

			By("Step 1: Make initial render request")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil(), "Initial request should not error")
			Expect(resp1.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"), "Should be freshly rendered")

			By("Step 2: Verify safe headers in initial response")
			Expect(resp1.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should be text/html")
			Expect(resp1.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control value mismatch")
			Expect(resp1.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")

			By("Step 3: Make second request - should be served from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil(), "Cache request should not error")
			Expect(resp2.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"), "Should be served from cache")

			By("Step 4: Verify safe headers are served from cache with same values")
			Expect(resp2.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Cached Content-Type should be text/html")
			Expect(resp2.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cached Cache-Control mismatch")
			Expect(resp2.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "Cached X-Custom-Header mismatch")

			By("Step 5: Verify non-safe headers not in cached response")
			Expect(resp2.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should not be in cache")
			Expect(resp2.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should not be in cache")
		})

		It("should not store non-safe headers in cache", func() {
			url := "/headers-test/render/cache-filtered"

			By("Step 1: Make initial render request")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil(), "Initial request should not error")
			Expect(resp1.StatusCode).To(Equal(200), "Status code should be 200")

			By("Step 2: Verify non-safe headers not in initial response")
			Expect(resp1.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
			Expect(resp1.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered")

			By("Step 3: Make second request from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil(), "Cache request should not error")
			Expect(resp2.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"), "Should be served from cache")

			By("Step 4: Verify non-safe headers not in cached response")
			Expect(resp2.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should not be in cache")
			Expect(resp2.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should not be in cache")
			Expect(resp2.Headers.Get("X-Internal-Debug")).To(BeEmpty(), "X-Internal-Debug should not be in cache")
		})

		It("should verify headers in cache metadata", func() {
			url := "/headers-test/render/metadata"

			By("Step 1: Make render request to create cache entry")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil(), "Should get cache key without error")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue(), "Cache entry should exist")

			By("Step 3: Check cache metadata has headers field with content")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil(), "Should get metadata without error")
			headersJSON, hasHeaders := metadata["headers"]
			Expect(hasHeaders).To(BeTrue(), "Cache metadata should contain headers field")
			Expect(headersJSON).NotTo(BeEmpty(), "Headers field should contain data")
		})
	})

	Context("Bypass Response Headers", func() {
		It("should include safe headers in bypass response", func() {
			url := "/headers-test/bypass/basic"

			By("Making a bypass request")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying X-Render-Source is bypass")
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("bypass"), "Should be bypass response")

			By("Verifying allowed response headers are present with correct values")
			Expect(resp.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should be text/html")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control value mismatch")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")

			By("Verifying non-allowed headers are filtered out")
			Expect(resp.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
			Expect(resp.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered")
		})

		It("should filter out non-safe headers in bypass response", func() {
			url := "/headers-test/bypass/filtered"

			By("Making a bypass request")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying non-safe headers are NOT present")
			Expect(resp.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
			Expect(resp.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered")
			Expect(resp.Headers.Get("X-Internal-Debug")).To(BeEmpty(), "X-Internal-Debug should be filtered")
		})

		It("should serve cached bypass response with safe headers", func() {
			url := "/headers-test/bypass/cache-serve"

			By("Step 1: Make initial bypass request")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil(), "Initial request should not error")
			Expect(resp1.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"), "Should be bypass response")

			By("Step 2: Verify safe headers in initial response")
			Expect(resp1.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")

			By("Step 3: Make second request - should be served from bypass cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil(), "Cache request should not error")
			Expect(resp2.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"), "Should be from bypass cache")

			By("Step 4: Verify safe headers in cached bypass response")
			Expect(resp2.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Cached Content-Type should be text/html")
			Expect(resp2.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cached Cache-Control mismatch")
			Expect(resp2.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "Cached X-Custom-Header mismatch")

			By("Step 5: Verify non-safe headers not in cached bypass response")
			Expect(resp2.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should not be in cache")
			Expect(resp2.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should not be in cache")
		})
	})

	Context("Edge Cases", func() {
		It("should handle empty safe headers configuration gracefully", func() {
			url := "/headers-test/render/edge-case"

			By("Making a render request")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying configured safe headers are present")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")

			By("Verifying non-allowed headers are filtered")
			Expect(resp.Headers.Get("X-Secret-Header")).To(BeEmpty(), "X-Secret-Header should be filtered")
		})

		It("should preserve header values exactly as received", func() {
			url := "/headers-test/render/exact-value"

			By("Making a render request")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying header values are preserved exactly")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control should be preserved exactly")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header should be preserved exactly")
		})
	})

	Context("Header Edge Cases", func() {
		It("should handle empty header values", func() {
			url := "/headers-test/render/empty-value/test"

			By("Making a render request with empty header value")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying Content-Type is present")
			Expect(resp.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should be text/html")

			By("Verifying safe headers work with empty value scenario")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header value mismatch")
		})

		It("should preserve special characters in header values", func() {
			url := "/headers-test/render/special-chars/test"

			By("Making a render request with special characters in header values")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying Cache-Control with semicolon is preserved")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600; must-revalidate"), "Cache-Control with semicolon should be preserved")

			By("Verifying X-Custom-Header with quotes and commas is preserved")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal(`value with "quotes", commas; and semicolons`), "Special characters should be preserved")
		})

		It("should match headers case-insensitively from origin", func() {
			url := "/headers-test/render/case-variation/test"

			By("Making a render request where origin sends lowercase header names")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying headers are matched case-insensitively")
			Expect(resp.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should match case-insensitively")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control should match case-insensitively")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("lowercase-header-name"), "X-Custom-Header should match case-insensitively")
		})

		It("should handle missing safe header from origin gracefully", func() {
			url := "/headers-test/render/missing-header/test"

			By("Making a render request where origin omits X-Custom-Header")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying present safe headers are returned")
			Expect(resp.Headers.Get("Content-Type")).To(HavePrefix("text/html"), "Content-Type should be present")

			By("Verifying missing header returns empty (not error)")
			Expect(resp.Headers.Get("X-Custom-Header")).To(BeEmpty(), "Missing header should return empty")
			Expect(resp.Headers.Get("Cache-Control")).To(BeEmpty(), "Missing Cache-Control should return empty")
		})

		It("should handle duplicate headers (multi-value)", func() {
			url := "/headers-test/render/duplicate/test"

			By("Making a render request where origin sends duplicate X-Custom-Header")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying multi-value headers are preserved as separate values")
			customHeaderValues := resp.Headers.Values("X-Custom-Header")
			Expect(customHeaderValues).To(ContainElement("value1"), "Should contain first value")
			Expect(customHeaderValues).To(ContainElement("value2"), "Should contain second value")

			By("Verifying other safe headers are present")
			Expect(resp.Headers.Get("Cache-Control")).To(Equal("public, max-age=3600"), "Cache-Control should be present")
		})

		It("should filter non-safe multi-value headers", func() {
			url := "/headers-test/render/multi-value/test"

			By("Making a render request where origin sends multiple Set-Cookie")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying Set-Cookie is filtered (not in safe headers)")
			Expect(resp.Headers.Get("Set-Cookie")).To(BeEmpty(), "Set-Cookie should be filtered even with multiple values")

			By("Verifying safe headers are present")
			Expect(resp.Headers.Get("X-Custom-Header")).To(Equal("custom-value-123"), "X-Custom-Header should be present")
		})
	})
})

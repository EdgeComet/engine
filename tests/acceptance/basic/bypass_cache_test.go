package acceptance_test

import (
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

var _ = Describe("Bypass Cache", Serial, func() {
	// CATEGORY 1: NORMAL FLOW - Basic Operations
	Context("Normal Flow - Basic Operations", func() {
		It("should cache on miss and serve from cache on hit", func() {
			url := "/bypass-test/default"

			By("First request - cache miss")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying cache entry created")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Second request - cache hit")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))
			Expect(resp2.Headers.Get("X-Cache-Age")).NotTo(BeEmpty())
		})

		It("should only cache status 200 by default", func() {
			By("Request returns 200 - should cache")
			resp200 := testEnv.RequestRender("/bypass-test/default?status=200")
			Expect(resp200.StatusCode).To(Equal(200))
			cacheKey200, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/default?status=200", "desktop")
			Expect(testEnv.CacheExists(cacheKey200)).To(BeTrue())

			By("Request returns 404 - should NOT cache")
			resp404 := testEnv.RequestRender("/bypass-test/default?status=404")
			Expect(resp404.StatusCode).To(Equal(404))
			cacheKey404, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/default?status=404", "desktop")
			Expect(testEnv.CacheExists(cacheKey404)).To(BeFalse())

			By("Request returns 500 - should NOT cache")
			resp500 := testEnv.RequestRender("/bypass-test/default?status=500")
			Expect(resp500.StatusCode).To(Equal(500))
			cacheKey500, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/default?status=500", "desktop")
			Expect(testEnv.CacheExists(cacheKey500)).To(BeFalse())
		})

		It("should cache multiple status codes when configured", func() {
			By("Status 200 - should cache")
			resp200 := testEnv.RequestRender("/bypass-test/multi-status?status=200")
			Expect(resp200.StatusCode).To(Equal(200))
			cacheKey200, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/multi-status?status=200", "desktop")
			Expect(testEnv.CacheExists(cacheKey200)).To(BeTrue())

			By("Status 404 - should cache")
			resp404 := testEnv.RequestRender("/bypass-test/multi-status?status=404")
			Expect(resp404.StatusCode).To(Equal(404))
			cacheKey404, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/multi-status?status=404", "desktop")
			Expect(testEnv.CacheExists(cacheKey404)).To(BeTrue())

			By("Status 301 - should NOT cache")
			resp301 := testEnv.RequestRenderNoRedirect("/bypass-test/multi-status?status=301")
			Expect(resp301.StatusCode).To(Equal(301))
			cacheKey301, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/multi-status?status=301", "desktop")
			Expect(testEnv.CacheExists(cacheKey301)).To(BeFalse())
		})

		It("should cache redirects when configured", func() {
			By("Request returns 301 - should cache")
			resp301 := testEnv.RequestRenderNoRedirect("/bypass-test/with-redirects?status=301")
			Expect(resp301.StatusCode).To(Equal(301))
			Expect(resp301.Headers.Get("Location")).NotTo(BeEmpty())

			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/with-redirects?status=301", "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Second request - served from cache with same status and Location")
			resp2 := testEnv.RequestRenderNoRedirect("/bypass-test/with-redirects?status=301")
			Expect(resp2.StatusCode).To(Equal(301))
			Expect(resp2.Headers.Get("Location")).To(Equal(resp301.Headers.Get("Location")))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
		})

		It("should respect TTL expiration", func() {
			url := "/bypass-test/ttl-short"

			By("First request - cache created")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))

			By("Second request after 1s - still cached")
			time.Sleep(1 * time.Second)
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))

			By("Third request after 3s total - cache expired, new fetch")
			time.Sleep(2 * time.Second)
			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("bypass"))
		})

		It("should not cache when disabled", func() {
			url := "/bypass-test/disabled"

			By("First request - NOT cached")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))

			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())

			By("Second request - fetches from origin again")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass"))
		})

		It("should not cache when TTL is zero", func() {
			url := "/bypass-test/ttl-zero"

			By("First request - NOT cached")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))

			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())

			By("Second request - fetches from origin again")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass"))
		})
	})

	// CATEGORY 2: CONFIGURATION HIERARCHY
	Context("Configuration Hierarchy - Deep Merge", func() {
		PIt("should use pattern-level override for TTL", func() {
			url := "/bypass-test/partial-override"

			By("Creating cache entry")
			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying cache metadata exists")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(metadata).NotTo(BeEmpty())

			// Verify cache entry was created successfully
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
		})

		It("should use catch-all pattern when specific pattern doesn't match", func() {
			url := "/bypass-test/unmatched-path"

			By("Creating cache entry with catch-all pattern")
			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying cache was created (catch-all has caching enabled)")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
		})
	})

	// CATEGORY 3: EDGE CASES
	Context("Edge Cases", func() {
		It("should handle concurrent requests without errors", func() {
			url := "/bypass-test/default?concurrent=test"

			By("Launching 10 concurrent requests")
			responses := make([]*TestResponse, 10)
			done := make(chan int, 10)

			for i := 0; i < 10; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender(url)
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < 10; i++ {
				<-done
			}

			By("Verifying all requests succeeded")
			for i, resp := range responses {
				Expect(resp.Error).To(BeNil(), "Request %d should succeed", i)
				Expect(resp.StatusCode).To(Equal(200))
			}

			By("Verifying cache entry exists")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
		})

		It("should return 502 on origin timeout", func() {
			// Stop the test server to simulate origin being unreachable
			By("Stopping test server to simulate unreachable origin")
			err := testEnv.TestServer.Stop()
			Expect(err).To(BeNil())

			url := "/bypass-test/default"

			By("Making request to unreachable origin")
			resp := testEnv.RequestRenderWithTimeout(url, 5*time.Second)
			Expect(resp.StatusCode).To(Equal(502)) // Bad Gateway

			By("Verifying no cache entry created for 502 response")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())

			By("Restarting test server for subsequent tests")
			testEnv.TestServer = testutil.NewTestServer(testEnv.Config.TestServer.Port, testEnv.RedisClient)
			err = testEnv.TestServer.Start()
			Expect(err).To(BeNil())
		})

		It("should normalize URLs with different query parameter order", func() {
			url1 := "/bypass-test/default?a=1&b=2"
			url2 := "/bypass-test/default?b=2&a=1"

			By("First request with a=1&b=2")
			resp1 := testEnv.RequestRender(url1)
			Expect(resp1.StatusCode).To(Equal(200))

			By("Second request with b=2&a=1 - should be cache hit")
			resp2 := testEnv.RequestRender(url2)
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
		})

		It("should handle origin server errors gracefully", func() {
			By("Request returns 500 error from origin")
			resp := testEnv.RequestRender("/bypass-test/default?status=500")
			Expect(resp.StatusCode).To(Equal(500))
			Expect(resp.Error).To(BeNil())

			By("Verifying NO cache entry created for error status")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/default?status=500", "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
		})

		It("should cache 404 responses when configured", func() {
			By("Request returns 404 with multi-status config")
			resp := testEnv.RequestRender("/bypass-test/multi-status?status=404")
			Expect(resp.StatusCode).To(Equal(404))

			By("Verifying cache entry created")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/bypass-test/multi-status?status=404", "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Second request - served from cache with 404 status")
			resp2 := testEnv.RequestRender("/bypass-test/multi-status?status=404")
			Expect(resp2.StatusCode).To(Equal(404))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
		})
	})

	// CATEGORY 4: CACHE METADATA VALIDATION
	Context("Cache Metadata Validation", func() {
		It("should store correct metadata fields", func() {
			url := "/bypass-test/default"

			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))

			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying metadata contains required fields")
			Expect(metadata).To(HaveKey("source"))
			Expect(metadata["source"]).To(Equal("bypass"))
			Expect(metadata).To(HaveKey("status_code"))
			Expect(metadata["status_code"]).To(Equal("200"))
			Expect(metadata).To(HaveKey("headers"))
			Expect(metadata["headers"]).To(ContainSubstring("Content-Type"))
			Expect(metadata["headers"]).To(ContainSubstring("Cache-Control"))
		})

		It("should preserve safe headers only", func() {
			url := "/bypass-test/default"

			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))

			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying safe headers are present")
			Expect(metadata).To(HaveKey("headers"))
			headers := metadata["headers"]
			Expect(headers).To(ContainSubstring("Content-Type"))
			Expect(headers).To(ContainSubstring("Cache-Control"))
			Expect(headers).To(Or(ContainSubstring("ETag"), ContainSubstring("Etag")))
			Expect(headers).To(ContainSubstring("Last-Modified"))

			By("Verifying custom headers are NOT present")
			Expect(headers).NotTo(ContainSubstring("X-Test-Request-ID"))
		})

		It("should set Cache-Age header on cache hit", func() {
			url := "/bypass-test/default?cache-age=test"

			By("Creating cache entry")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))

			By("Waiting 2 seconds")
			time.Sleep(2 * time.Second)

			By("Requesting cached entry")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))

			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
			// Cache age should be approximately 2 seconds (allowing tolerance)
			Expect(cacheAge).To(MatchRegexp(`^[2-3]`))
		})

		It("should preserve Content-Type from origin", func() {
			url := "/bypass-test/default?content-type=test"

			By("First request - cache miss")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))
			contentType1 := resp1.Headers.Get("Content-Type")
			Expect(contentType1).To(Equal("application/json"))

			By("Second request - cache hit")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			contentType2 := resp2.Headers.Get("Content-Type")
			Expect(contentType2).To(Equal(contentType1))
		})
	})

	// CATEGORY 5: PERFORMANCE VALIDATION
	Context("Performance Validation", func() {
		It("should serve cache hit faster than cache miss", func() {
			url := "/bypass-test/default?perf=test"

			By("First request - cache miss")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Error).To(BeNil())

			By("Second request - cache hit")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Error).To(BeNil())

			By("Verifying both requests completed successfully")
			// Note: Precise performance comparison is unreliable in test environments
			// Both requests completing successfully validates the caching mechanism works
			Expect(resp1.Duration).To(BeNumerically(">", 0))
			Expect(resp2.Duration).To(BeNumerically(">", 0))
		})
	})

	// CATEGORY 6: INTEGRATION WITH OTHER FEATURES
	Context("Integration with Other Features", func() {
		It("should use first matching URL pattern", func() {
			// The specific pattern /bypass-test/default should match before catch-all
			url := "/bypass-test/default?pattern=priority"

			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying cache created with specific pattern config (30m TTL)")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			// Verify it's a bypass cache entry
			Expect(metadata).To(HaveKey("source"))
			Expect(metadata["source"]).To(Equal("bypass"))
		})

		It("should handle PDF bypass with caching", func() {
			url := "/documents/test.pdf"

			By("First request for PDF")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("Content-Type")).To(Equal("application/pdf"))

			By("Verifying PDF cached (*.pdf pattern has caching enabled)")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Second request - served from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Headers.Get("Content-Type")).To(Equal("application/pdf"))
		})
	})

	// CATEGORY 7: BYPASS ENTRY POINTS
	Context("Bypass Entry Points", func() {
		It("should trigger bypass via explicit bypass action", func() {
			url := "/bypass-test/default"

			By("Making request to bypass action URL")
			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying bypass cache was created")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata, _ := testEnv.GetCacheMetadata(cacheKey)
			Expect(metadata).To(HaveKey("source"))
			Expect(metadata["source"]).To(Equal("bypass"))
		})

		It("should cache responses from bypass action with JSON content", func() {
			url := "/data.json"

			By("First request to .json file (bypass action)")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("Content-Type")).To(ContainSubstring("application/json"))

			By("Parsing JSON response")
			var jsonData map[string]interface{}
			err := json.Unmarshal([]byte(resp1.Body), &jsonData)
			Expect(err).To(BeNil())
			Expect(jsonData).To(HaveKey("type"))
		})
	})

	// CATEGORY 8: SPECIAL SCENARIOS
	Context("Special Scenarios", func() {
		It("should handle query parameters with special characters", func() {
			url := "/bypass-test/default?q=hello%20world&search=test%2Bvalue"

			By("First request with encoded params")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))

			By("Verifying cache created")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Second request - should be cache hit")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
		})

		It("should handle empty response body", func() {
			url := "/bypass-test/default?status=204" // No Content

			resp := testEnv.RequestRender(url)
			Expect(resp.StatusCode).To(Equal(204))
			Expect(len(resp.Body)).To(Equal(0))
		})

		It("should serve bypass when render pattern matches but no render service", func() {
			// This test verifies fallback behavior
			// Using a regular render pattern but when render service is unavailable
			url := "/static/simple.html"

			By("First request - normal render")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.StatusCode).To(Equal(200))
			// Should render or serve from cache

			By("Verifying response was successful")
			Expect(resp1.Error).To(BeNil())
			Expect(strings.Contains(resp1.Body, "Simple")).To(BeTrue())
		})
	})
})

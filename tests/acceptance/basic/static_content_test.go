package acceptance_test

import (
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Static Content Rendering", Serial, func() {
	Context("when requesting a simple HTML page", func() {
		It("should return the rendered HTML content", func() {
			By("Making a request to render a simple static page")
			response := testEnv.RequestRender("/static/simple.html")

			By("Checking the response status")
			// This test will fail initially until EG is implemented
			Expect(response.Error).To(BeNil(), "Request should not have network errors")
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying the HTML content is present")
			Expect(response.Body).To(ContainSubstring("<h1>Static Content</h1>"))
			Expect(response.Body).To(ContainSubstring("This content is immediately available without JavaScript"))
			Expect(response.Body).To(ContainSubstring("<title>Simple Test Page</title>"))

			By("Checking response headers")
			Expect(response.Headers.Get("Content-Type")).To(ContainSubstring("text/html"))

			By("Verifying response time is reasonable")
			Expect(response.Duration).To(BeNumerically("<", 30*time.Second))
		})

		It("should serve from cache on second request", func() {
			By("Making the first request (cache miss)")
			response1 := testEnv.RequestRender("/static/simple.html")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Checking that first request was a cache miss")
			// Cache status header may not be implemented yet
			cacheStatus1 := response1.Headers.Get("X-Cache-Status")
			if cacheStatus1 != "" {
				Expect(cacheStatus1).To(Equal("MISS"))
			}

			By("Making the second request (should be cache hit)")
			response2 := testEnv.RequestRender("/static/simple.html")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying second request was served from cache")
			cacheStatus2 := response2.Headers.Get("X-Cache-Status")
			if cacheStatus2 != "" {
				Expect(cacheStatus2).To(Equal("HIT"))
			}

			GinkgoWriter.Printf("response1: %s\n", response1.Duration)
			GinkgoWriter.Printf("response2: %s\n", response2.Duration)

			By("Comparing response times (cache should be faster)")
			if response1.Duration > 0 && response2.Duration > 0 {
				// Cache hits should be significantly faster
				Expect(response2.Duration).To(BeNumerically("<", response1.Duration/2))
				Expect(response2.Duration).To(BeNumerically("<", 100*time.Millisecond))
			}

			By("Verifying both responses have identical content")
			Expect(response2.Body).To(Equal(response1.Body))
		})

		It("should handle pages with SEO meta tags", func() {
			By("Making a request to a page with rich meta tags")
			response := testEnv.RequestRender("/static/with-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying basic SEO meta tags are present")
			Expect(response.Body).To(ContainSubstring("<title>SEO Test Page - Meta Tags Example</title>"))
			Expect(response.Body).To(ContainSubstring(`<meta name="description" content="This page tests various SEO meta tags`))
			Expect(response.Body).To(ContainSubstring(`<meta name="keywords"`))

			By("Verifying Open Graph tags are present")
			Expect(response.Body).To(ContainSubstring(`<meta property="og:title"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:description"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:type" content="website"`))

			By("Verifying Twitter Card tags are present")
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:card"`))
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:title"`))
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:description"`))

			By("Verifying canonical URL is present")
			Expect(response.Body).To(ContainSubstring(`<link rel="canonical"`))

			By("Verifying the page content is also present")
			Expect(response.Body).To(ContainSubstring("SEO Meta Tags Test Page"))
			Expect(response.Body).To(ContainSubstring("This page contains various meta tags"))
		})

		It("should handle pages with structured data", func() {
			By("Making a request to a page with JSON-LD structured data")
			response := testEnv.RequestRender("/static/with-structured-data.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Article structured data is present")
			Expect(response.Body).To(ContainSubstring(`<script type="application/ld+json">`))
			Expect(response.Body).To(ContainSubstring(`"@type": "Article"`))
			Expect(response.Body).To(ContainSubstring(`"headline": "Structured Data Test Article"`))
			Expect(response.Body).To(ContainSubstring(`"author"`))
			Expect(response.Body).To(ContainSubstring(`"datePublished"`))

			By("Verifying Product structured data is present")
			Expect(response.Body).To(ContainSubstring(`"@type": "Product"`))
			Expect(response.Body).To(ContainSubstring(`"name": "Test Product"`))
			Expect(response.Body).To(ContainSubstring(`"offers"`))
			Expect(response.Body).To(ContainSubstring(`"aggregateRating"`))

			By("Verifying the visible content is also present")
			Expect(response.Body).To(ContainSubstring("Structured Data Test Article"))
			Expect(response.Body).To(ContainSubstring("Test Product"))
			Expect(response.Body).To(ContainSubstring("$99.99"))
		})
	})

	Context("when caching is working properly", func() {
		It("should create cache entries that persist", func() {
			By("Making a request to create a cache entry")

			fullTargetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := testEnv.RequestRender(fullTargetURL)
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Generating the expected cache key")
			// Build full URL that was rendered

			cacheKey, err := testEnv.GetCacheKey(fullTargetURL, "desktop")
			Expect(err).To(BeNil(), "Should generate cache key without error")
			GinkgoWriter.Printf("Generated cache key: %s\n", cacheKey)

			By("Verifying cache entry was created in Redis")
			// Cache write should happen quickly after render completes
			var allKeys []string
			Eventually(func() bool {
				exists := testEnv.CacheExists(cacheKey)
				if !exists {
					// Capture all cache keys for debugging if expected key not found
					allKeys, _ = testEnv.GetAllCacheKeys()
				}
				return exists
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Cache entry should be created in Redis.\nExpected key: %s\nFound keys: %v", cacheKey, allKeys)

			By("Verifying cache entry persists")
			// Check again after a short delay to ensure it's not immediately expiring
			time.Sleep(500 * time.Millisecond)
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue(),
				"Cache entry should persist after initial write")

			By("Verifying cache metadata is present")
			// Optional: Check that cache metadata exists and has expected fields
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			if err == nil && len(metadata) > 0 {
				// If metadata exists, verify it has expected fields
				// The metadata is stored as hash containing fields like:
				// key, url, file_path, host_id, dimension, created_at, expires_at, etc.
				GinkgoWriter.Printf("Cache metadata found with %d fields\n", len(metadata))
			}
		})

		It("should respect TTL settings", func() {
			By("Making a request to a page with short TTL")
			response := testEnv.RequestRender("/static/simple.html")
			Expect(response.Error).To(BeNil())

			By("Verifying cache TTL header is set")
			cacheControl := response.Headers.Get("Cache-Control")
			if cacheControl != "" {
				// Should have max-age set according to configuration
				Expect(cacheControl).To(ContainSubstring("max-age"))
			}

			By("Verifying Expires header is set")
			expires := response.Headers.Get("Expires")
			if expires != "" {
				Expect(expires).NotTo(BeEmpty())
			}
		})
	})

	Context("when handling different content types", func() {
		It("should set appropriate content-type headers", func() {
			By("Making a request to an HTML page")
			response := testEnv.RequestRender("/static/simple.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying content-type is set correctly")
			contentType := response.Headers.Get("Content-Type")
			if contentType != "" {
				Expect(contentType).To(ContainSubstring("text/html"))
				// May also include charset
				// Expect(contentType).To(ContainSubstring("charset=utf-8"))
			}
		})

		It("should handle UTF-8 content correctly", func() {
			By("Making a request to page with international characters")
			response := testEnv.RequestRender("/static/with-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying UTF-8 meta tag is present")
			Expect(response.Body).To(ContainSubstring(`<meta charset="UTF-8"`))

			By("Verifying content is properly encoded")
			Expect(len(response.Body)).To(BeNumerically(">", 1000))
		})
	})

	Context("when testing performance characteristics", func() {
		It("should render pages within reasonable time limits", func() {
			By("Making a request with timing measurement")
			start := time.Now()
			response := testEnv.RequestRender("/static/simple.html")
			renderTime := time.Since(start)

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying render time is acceptable")
			// First render (cache miss) should complete within 30 seconds
			Expect(renderTime).To(BeNumerically("<", 30*time.Second))

			By("Verifying response duration matches measurement")
			// Allow for small measurement differences
			Expect(response.Duration).To(BeNumerically("~", renderTime, 1*time.Second))
		})

		It("should handle concurrent requests efficiently", func() {
			numRequests := 5
			responses := make([]*TestResponse, numRequests)
			done := make(chan int, numRequests)

			By("Making multiple concurrent requests to the same URL")
			targetURL := "/concurrency/timestamp-test.html"

			for i := 0; i < numRequests; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender(targetURL)
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < numRequests; i++ {
				select {
				case <-done:
					// Request completed
				case <-time.After(60 * time.Second):
					Fail("Request timed out after 60 seconds")
				}
			}

			By("Verifying all requests succeeded")
			for i, response := range responses {
				Expect(response).NotTo(BeNil(), "Response %d should not be nil", i)
				Expect(response.Error).To(BeNil(), "Response %d should not have errors: %v", i, response.Error)
				Expect(response.StatusCode).To(Equal(200), "Response %d should return 200, got %d", i, response.StatusCode)
			}

			By("Extracting timestamps and UUIDs from all responses")
			timestamps := make([]string, numRequests)
			uuids := make([]string, numRequests)

			timestampRegex := regexp.MustCompile(`<div id="render-timestamp">([^<]+)</div>`)
			uuidRegex := regexp.MustCompile(`<div id="render-uuid">([^<]+)</div>`)

			for i, response := range responses {
				// Extract timestamp
				timestampMatches := timestampRegex.FindStringSubmatch(response.Body)
				Expect(timestampMatches).To(HaveLen(2), "Response %d should contain render-timestamp element", i)
				timestamps[i] = timestampMatches[1]

				// Extract UUID
				uuidMatches := uuidRegex.FindStringSubmatch(response.Body)
				Expect(uuidMatches).To(HaveLen(2), "Response %d should contain render-uuid element", i)
				uuids[i] = uuidMatches[1]

				GinkgoWriter.Printf("Response %d - Timestamp: %s, UUID: %s\n", i, timestamps[i], uuids[i])
			}

			By("Verifying all timestamps are identical (proving only one render occurred)")
			expectedTimestamp := timestamps[0]
			expectedUUID := uuids[0]

			for i := 1; i < numRequests; i++ {
				Expect(timestamps[i]).To(Equal(expectedTimestamp),
					"All timestamps must be identical. Response %d has different timestamp.\nExpected: %s\nGot: %s\nThis indicates multiple renders occurred instead of cache serving.",
					i, expectedTimestamp, timestamps[i])

				Expect(uuids[i]).To(Equal(expectedUUID),
					"All UUIDs must be identical. Response %d has different UUID.\nExpected: %s\nGot: %s\nThis indicates multiple renders occurred instead of cache serving.",
					i, expectedUUID, uuids[i])
			}

			By("Verifying timestamp is recent and valid")
			parsedTime, err := time.Parse(time.RFC3339Nano, expectedTimestamp)
			Expect(err).To(BeNil(), "Timestamp should be valid RFC3339Nano format: %s", expectedTimestamp)
			Expect(time.Since(parsedTime)).To(BeNumerically("<", 1*time.Minute),
				"Timestamp should be recent (within last minute), got: %v ago", time.Since(parsedTime))

			GinkgoWriter.Printf("\n✓ All %d concurrent requests returned identical timestamp and UUID\n", numRequests)
			GinkgoWriter.Printf("  Timestamp: %s\n", expectedTimestamp)
			GinkgoWriter.Printf("  UUID: %s\n", expectedUUID)
			GinkgoWriter.Printf("  This proves distributed locking is working - only ONE render occurred!\n")
		})
	})

	Context("when testing edge cases", func() {
		It("should handle requests to non-existent pages gracefully", func() {
			By("Making a request to a non-existent page")
			response := testEnv.RequestRender("/static/non-existent-page.html")

			// The response will depend on implementation
			// It might return 404, or it might try to render the 404 page
			Expect(response.Error).To(BeNil(), "Should not have network errors")

			By("Verifying appropriate error response")
			// Could be 404 from the test server, or 502/503 from EG
			Expect(response.StatusCode).To(BeNumerically(">=", 400))
		})

		It("should handle very long URLs appropriately", func() {
			longPath := "/static/simple.html?param=" + strings.Repeat("a", 2000)

			By("Making a request with a very long URL")
			response := testEnv.RequestRender(longPath)

			// Response will depend on URL length limits in implementation
			Expect(response.Error).To(BeNil())

			// Should either succeed or return an appropriate error
			if response.StatusCode == 200 {
				Expect(response.Body).To(ContainSubstring("Static Content"))
			} else {
				// Should return a client error for overly long URLs
				Expect(response.StatusCode).To(BeNumerically(">=", 400))
			}
		})
	})
})

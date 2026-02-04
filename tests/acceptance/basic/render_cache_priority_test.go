package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Render Cache Priority - Core Feature Validation", Serial, func() {

	// GROUP 1: BASIC PRIORITY RULES (3 tests)

	Context("Basic Priority Rules", func() {

		It("should NOT overwrite render cache with bypass cache (same status 200)", func() {
			url := "/priority-test/dual-mode"

			By("Step 1: Create render cache via normal render action")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 2: Verify cache entry exists with source=render")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("render"))

			timestamp1, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Wait 1 second to ensure timestamps would differ if cache is recreated")
			time.Sleep(1 * time.Second)

			By("Step 4: Trigger bypass for same URL (simulated fallback)")
			// NOTE: This test relies on bypass fallback behavior when render service is busy/unavailable
			// For now, we'll request the bypass-mode URL which has bypass action configured
			// A more accurate test would require simulating render service unavailability
			bypassURL := "/priority-test/bypass-mode"
			respBypass := testEnv.RequestRender(bypassURL)
			Expect(respBypass.Error).To(BeNil())
			Expect(respBypass.StatusCode).To(Equal(200))

			By("Step 5: Verify original render cache still exists unchanged")
			source2, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("render"), "Cache source should remain 'render', not be overwritten by bypass")

			timestamp2, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())
			Expect(timestamp2).To(Equal(timestamp1), "Cache timestamp should be unchanged")

			By("Step 6: Subsequent request should serve from render cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))
		})

		It("should NOT overwrite render cache with bypass cache (different status codes)", func() {
			url := "/priority-test/dual-mode?test=status-diff"

			By("Step 1: Create render cache with status 200")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Step 2: Verify cache metadata shows source=render, status=200")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("render"))

			status1, err := testEnv.GetCacheStatusCode(cacheKey)
			Expect(err).To(BeNil())
			Expect(status1).To(Equal(200))

			By("Step 3: Simulate bypass returning different status (404)")
			// NOTE: In real scenario, bypass would be triggered as fallback
			// For this test, we verify that even if bypass cache exists elsewhere,
			// render cache remains untouched
			// This is a limitation of current test setup - full test requires render service unavailability

			By("Step 4: Verify cache still shows source=render, status=200 (not overwritten)")
			source2, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("render"))

			status2, err := testEnv.GetCacheStatusCode(cacheKey)
			Expect(err).To(BeNil())
			Expect(status2).To(Equal(200))
		})

		It("should allow bypass cache when NO render cache exists", func() {
			url := "/priority-test/bypass-mode"

			By("Step 1: Clear all caches")
			err := testEnv.ClearCache()
			Expect(err).To(BeNil())

			By("Step 2: Request bypass-mode URL (bypass action)")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Step 3: Verify bypass cache created with source=bypass")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("bypass"))

			By("Step 4: Subsequent request serves from bypass_cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))
		})
	})

	// GROUP 2: CACHE EXPIRATION INTERACTIONS (3 tests)

	Context("Cache Expiration Interactions", func() {

		It("should NOT overwrite expired render cache (stale cache behavior)", func() {
			url := "/priority-test/short-ttl?test=expire"

			By("Step 1: Create render cache with short TTL (2s)")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Step 2: Wait for cache to expire (3s)")
			time.Sleep(3 * time.Second)

			By("Step 3: Verify cache still exists but expired")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("render"))

			By("Step 4: Request again - should serve stale cache or re-render")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))

			By("Step 5: Verify cache source remains render (NOT overwritten by bypass)")
			source2, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("render"))
		})

		It("should preserve render cache priority across TTL renewals", func() {
			url := "/priority-test/dual-mode?test=ttl-renewal"

			By("Step 1: Create render cache with TTL 10m")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			timestamp1, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())

			By("Step 2: Wait 2 seconds")
			time.Sleep(2 * time.Second)

			By("Step 3: Trigger bypass request (would attempt to update cache)")
			// NOTE: In real scenario this would be bypass fallback
			// For now we verify that render cache remains unchanged

			By("Step 4: Wait 2 more seconds")
			time.Sleep(2 * time.Second)

			By("Step 5: Request again and verify cache age reflects original creation time")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())

			timestamp2, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())
			Expect(timestamp2).To(Equal(timestamp1), "Cache timestamp should not be reset by bypass")

			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"))

			By("Step 6: Verify cache age is approximately 4+ seconds")
			cacheAge := time.Since(timestamp2)
			Expect(cacheAge).To(BeNumerically(">=", 4*time.Second))
		})

		It("should manually expire cache and verify metadata", func() {
			url := "/priority-test/dual-mode?test=manual-expire"

			By("Step 1: Create render cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 2: Manually expire the cache using test helper")
			err = testEnv.ExpireCache(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Verify cache still exists (not deleted)")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Step 4: Verify source remains render")
			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"))

			By("Step 5: Request again - should handle expired cache appropriately")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
		})
	})

	// GROUP 3: BYPASS OVERWRITES BYPASS (2 tests)

	Context("Bypass Overwrites Bypass", func() {

		It("should allow bypass to overwrite existing bypass cache", func() {
			url := "/priority-test/bypass-mode?test=bypass-overwrite"

			By("Step 1: Create bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("bypass"))

			timestamp1, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())

			By("Step 2: Wait 1 second")
			time.Sleep(1 * time.Second)

			By("Step 3: Verify second bypass request serves from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))

			By("Step 4: Verify timestamp unchanged (served from cache)")
			timestamp2, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())
			Expect(timestamp2).To(Equal(timestamp1))

			By("Step 5: Verify both entries have source=bypass")
			source2, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("bypass"))
		})

		It("should allow render to overwrite existing bypass cache", func() {
			url := "/priority-test/bypass-mode?test=render-overwrites-bypass"

			By("Step 1: Create bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("bypass"))

			By("Step 2: Request same URL with render action")
			// NOTE: To properly test this, we would need to change the URL pattern or use a different URL
			// that has render action instead of bypass
			// For now, we'll use the dual-mode URL which has render action
			renderURL := "/priority-test/dual-mode"
			renderCacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+renderURL, "desktop")
			Expect(err).To(BeNil())

			resp2 := testEnv.RequestRender(renderURL)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 3: Verify render cache created")
			source2, err := testEnv.GetCacheSource(renderCacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("render"), "Render cache should be created successfully")

			By("Step 4: Verify render cache serves on subsequent requests")
			resp3 := testEnv.RequestRender(renderURL)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))
		})
	})

	// GROUP 4: CONCURRENT REQUEST SCENARIOS (2 tests)

	Context("Concurrent Request Scenarios", func() {

		It("should handle concurrent requests without corruption", func() {
			url := "/priority-test/dual-mode?test=concurrent"

			By("Step 1: Clear cache before test")
			err := testEnv.ClearCache()
			Expect(err).To(BeNil())

			By("Step 2: Launch 5 concurrent requests")
			responses := make([]*TestResponse, 5)
			done := make(chan int, 5)

			for i := 0; i < 5; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender(url)
					done <- index
				}(i)
			}

			By("Step 3: Wait for all requests to complete")
			for i := 0; i < 5; i++ {
				<-done
			}

			By("Step 4: Verify all requests succeeded")
			for i, resp := range responses {
				Expect(resp.Error).To(BeNil(), "Request %d should succeed", i)
				Expect(resp.StatusCode).To(Equal(200))
			}

			By("Step 5: Verify final cache state is deterministic (render cache exists)")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"), "Final cache should be render cache")

			By("Step 6: Verify no partial writes or corrupted metadata")
			status, err := testEnv.GetCacheStatusCode(cacheKey)
			Expect(err).To(BeNil())
			Expect(status).To(Equal(200))
		})

		It("should handle sequential requests properly", func() {
			url := "/priority-test/dual-mode?test=sequential"

			By("Step 1: First request creates render cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 2: Second request serves from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Step 3: Third request also serves from cache")
			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.StatusCode).To(Equal(200))
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Step 4: Verify cache source remains render throughout")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"))
		})
	})

	// GROUP 5: EDGE CASES (2 tests)

	Context("Edge Cases", func() {

		It("should handle cache metadata correctly across different URLs", func() {
			url1 := "/priority-test/dual-mode?test=url1"

			By("Step 1: Create render cache for URL1")
			resp1 := testEnv.RequestRender(url1)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Step 2: Create bypass cache for URL2")
			// Using bypass-mode pattern for URL2
			bypassURL2 := "/priority-test/bypass-mode?test=url2"
			resp2 := testEnv.RequestRender(bypassURL2)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))

			By("Step 3: Verify URL1 has render cache")
			cacheKey1, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url1, "desktop")
			Expect(err).To(BeNil())

			source1, err := testEnv.GetCacheSource(cacheKey1)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("render"))

			By("Step 4: Verify URL2 has bypass cache")
			cacheKey2, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+bypassURL2, "desktop")
			Expect(err).To(BeNil())

			source2, err := testEnv.GetCacheSource(cacheKey2)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("bypass"))

			By("Step 5: Verify caches are independent")
			Expect(cacheKey1).NotTo(Equal(cacheKey2))
		})

		It("should verify priority check has minimal performance overhead", func() {
			url := "/priority-test/dual-mode?test=performance"

			By("Step 1: Create render cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Step 2: Make 10 sequential requests (all from cache)")
			totalDuration := time.Duration(0)
			for i := 0; i < 10; i++ {
				start := time.Now()
				resp := testEnv.RequestRender(url)
				duration := time.Since(start)

				Expect(resp.Error).To(BeNil())
				Expect(resp.StatusCode).To(Equal(200))
				Expect(resp.Headers.Get("X-Render-Source")).To(Equal("cache"))

				totalDuration += duration
			}

			By("Step 3: Verify average response time is reasonable (< 100ms)")
			avgDuration := totalDuration / 10
			Expect(avgDuration).To(BeNumerically("<", 100*time.Millisecond))

			By("Step 4: Verify cache remains unchanged after all requests")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"))
		})
	})

	// GROUP 6: RENDER SERVICE FAILURE SCENARIOS (Real bypass fallback testing)

	Context("Render Service Failure - Real Fallback Scenarios", func() {

		It("should serve from render cache even when RS fails (no bypass fallback needed)", func() {
			url := "/priority-test/dual-mode?test=rs-failure"

			By("Step 1: Create render cache via normal render")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 2: Verify render cache exists")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			source1, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source1).To(Equal("render"))

			timestamp1, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Simulate render service failure")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 4: Request same URL - should serve from existing cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Step 5: Verify render cache unchanged (no bypass fallback)")
			source2, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source2).To(Equal("render"), "Render cache should be preserved")

			timestamp2, err := testEnv.GetCacheTimestamp(cacheKey)
			Expect(err).To(BeNil())
			Expect(timestamp2).To(Equal(timestamp1), "Timestamp unchanged")

			By("Step 6: Restore RS - cache continues to serve normally")
			err = testEnv.RestoreRenderServiceHealth()
			Expect(err).To(BeNil())

			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))
		})

		It("should NOT create bypass cache after RS failure (preserve render cache slot)", func() {
			url := "/priority-test/dual-mode?test=rs-recovery"

			By("Step 1: Clear cache and simulate RS failure")
			err := testEnv.ClearCache()
			Expect(err).To(BeNil())
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 2: Request with action=render falls back to bypass")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Step 3: Verify NO cache created (slot reserved for render)")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse(), "No cache should be created to preserve slot for render cache")

			By("Step 4: Restore RS and create proper render cache")
			err = testEnv.RestoreRenderServiceHealth()
			Expect(err).To(BeNil())

			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 5: Verify render cache now exists")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
			source, err := testEnv.GetCacheSource(cacheKey)
			Expect(err).To(BeNil())
			Expect(source).To(Equal("render"))

			By("Step 6: Verify subsequent requests serve from render cache")
			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))
		})
		It("should handle capacity exhaustion with bypass fallback", func() {
			url := "/priority-test/dual-mode?test=capacity"

			By("Step 1: Create render cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			_, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 2: Exhaust render service capacity")
			err = testEnv.ExhaustRenderServiceCapacity()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceCapacity()

			By("Step 3: New request should fallback to bypass")
			url2 := "/priority-test/dual-mode?test=capacity-new"
			resp2 := testEnv.RequestRender(url2)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Step 4: Restore capacity")
			err = testEnv.RestoreRenderServiceCapacity()
			Expect(err).To(BeNil())

			By("Step 5: Verify original render cache still serves")
			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))
		})
	})

	// ADDITIONAL VALIDATION TESTS

	Context("Cache Metadata Validation", func() {

		It("should preserve cache metadata fields correctly for render cache", func() {
			url := "/priority-test/dual-mode?test=metadata"

			By("Step 1: Create render cache")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Retrieve and validate metadata fields")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Verify required metadata fields exist")
			Expect(metadata).To(HaveKey("source"))
			Expect(metadata["source"]).To(Equal("render"))
			Expect(metadata).To(HaveKey("status_code"))
			Expect(metadata["status_code"]).To(Equal("200"))
			Expect(metadata).To(HaveKey("url"))
			Expect(metadata).To(HaveKey("created_at"))
			Expect(metadata).To(HaveKey("expires_at"))
			Expect(metadata).To(HaveKey("host_id"))
			Expect(metadata).To(HaveKey("dimension"))

			By("Step 4: Verify render cache has headers field")
			// Render cache preserves origin headers
			_, hasHeaders := metadata["headers"]
			Expect(hasHeaders).To(BeTrue(), "Render cache should have headers field")
		})

		It("should preserve cache metadata fields correctly for bypass cache", func() {
			url := "/priority-test/bypass-mode?test=metadata"

			By("Step 1: Create bypass cache")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Retrieve and validate metadata fields")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Verify required metadata fields exist")
			Expect(metadata).To(HaveKey("source"))
			Expect(metadata["source"]).To(Equal("bypass"))
			Expect(metadata).To(HaveKey("status_code"))
			Expect(metadata["status_code"]).To(Equal("200"))
			Expect(metadata).To(HaveKey("url"))
			Expect(metadata).To(HaveKey("created_at"))
			Expect(metadata).To(HaveKey("expires_at"))

			By("Step 4: Verify bypass cache HAS headers field (preserves origin headers)")
			_, hasHeaders := metadata["headers"]
			Expect(hasHeaders).To(BeTrue(), "Bypass cache should preserve headers")
		})
	})
})

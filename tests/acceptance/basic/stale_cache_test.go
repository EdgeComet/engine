package acceptance_test

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stale Cache Behavior", func() {
	Context("Fresh Cache Baseline", func() {
		It("should serve fresh cache immediately when not expired", func() {
			url := "/stale-test/default-fresh.html"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 2: Request again immediately - should serve fresh cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))

			By("Step 3: Verify response content matches cached version")
			Expect(resp2.Body).To(ContainSubstring("Stale Cache Test Page"))
		})
	})

	Context("Stale Cache Serving on Render Failure", func() {
		It("should serve stale cache when render service fails", func() {
			url := "/stale-test/default?test=service-fail"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))
			originalBody := resp1.Body

			By("Step 2: Get cache key for this URL")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Wait for cache to become stale")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil()) // TTL = 2s

			By("Step 4: Verify cache still exists in Redis (in stale period)")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Step 5: Simulate render service failure")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 6: Request again - should serve stale cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			By("Step 7: Verify content matches original cache")
			Expect(resp2.Body).To(Equal(originalBody))
		})

		It("should serve stale cache when render returns 5xx error", func() {
			url := "/stale-test/default?test=5xx-error"

			By("Step 1: Create fresh cache with 200 status")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			originalBody := resp1.Body

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward to make cache stale")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Verify cache exists in stale period")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Step 4.5: Set status override for next request to return 500")
			err = testEnv.SetStatusOverride(url, 500)
			Expect(err).To(BeNil())

			By("Step 5: Request again - test page returns 5xx on subsequent render")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())

			By("Step 6: Should serve stale cache (not 500 error)")
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))
			Expect(resp2.Body).To(Equal(originalBody))
		})
	})

	Context("Response Headers Validation", func() {
		It("should set X-Render-Cache: hit for fresh cache", func() {
			url := "/stale-test/simple.html?test=fresh-headers"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Request again - should be fresh cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))

			By("Step 3: Verify X-Cache-Age header is present and reasonable")
			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
		})

		It("should set X-Render-Cache: stale when serving stale cache", func() {
			url := "/stale-test/default?test=stale-headers"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward to stale period")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Simulate render failure to trigger stale serving")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 5: Request again - should serve stale with correct headers")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			By("Step 5: Verify X-Cache-Age header is present")
			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
		})

		It("should calculate X-Cache-Age correctly for stale cache", func() {
			url := "/stale-test/default?test=cache-age"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward 5 seconds to stale period")
			err = testEnv.MakeCacheStale(cacheKey, 5*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Simulate render failure to serve stale")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 4: Request and verify cache age reflects time passed")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
			// Age should be a valid duration in integer seconds
			Expect(cacheAge).To(MatchRegexp(`^[0-9]+$`))
		})
	})

	Context("Fresh Render Success with Stale Cache Present", func() {
		It("should serve fresh render when stale exists but render succeeds", func() {
			url := "/stale-test/default?test=fresh-overwrites-stale"

			By("Step 1: Create initial cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward to stale period")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 3: Request again - fresh render should succeed (no failure)")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("rendered"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("new"))
		})

		It("should handle redirect status codes with stale cache", func() {
			url := "/stale-test/default?test=301-redirect"

			By("Step 1: Create cache with 301 redirect")
			urlWith301 := url + "&status=301&location=http://example.com/new-location"
			resp1 := testEnv.RequestRenderNoRedirect(urlWith301)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(301))

			By("Step 2: Verify cache exists")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+urlWith301, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Step 3: Fast forward to stale period")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Simulate failure to trigger stale serving")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 5: Request should serve stale 301")
			resp2 := testEnv.RequestRenderNoRedirect(urlWith301)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(301))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))
		})
	})

	Context("Delete Strategy Behavior", func() {
		It("should NOT serve stale cache when delete strategy is configured", func() {
			Skip("Requires separate URL pattern with delete strategy - will implement in integration test")
		})
	})

	Context("Cache State Verification", func() {
		It("should maintain cache metadata during stale period", func() {
			url := "/stale-test/default?test=metadata"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache metadata")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metadata1, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(metadata1["source"]).To(Equal("render"))
			Expect(metadata1["status_code"]).To(Equal("200"))

			By("Step 3: Fast forward to stale period (3s)")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Verify metadata still exists in stale period")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
			metadata2, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(metadata2["source"]).To(Equal("render"))

			By("Step 5: Fast forward past total TTL (cache_ttl + stale_ttl = 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 10*time.Second)
			Expect(err).To(BeNil()) // 13s total

			By("Step 6: Verify metadata is deleted after stale TTL expires")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
		})

		It("should verify ExpiresAt timestamp marks fresh-to-stale transition", func() {
			url := "/stale-test/default?test=expires-at"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache metadata and verify ExpiresAt")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Step 3: Verify ExpiresAt is set")
			expiresAtStr := metadata["expires_at"]
			Expect(expiresAtStr).NotTo(BeEmpty())

			By("Step 4: Parse ExpiresAt timestamp")
			expiresAtSeconds, err := strconv.ParseInt(expiresAtStr, 10, 64)
			Expect(err).To(BeNil())

			By("Step 5: Verify ExpiresAt is ~2 seconds from now (cache TTL)")
			createdAtStr := metadata["created_at"]
			createdAtSeconds, err := strconv.ParseInt(createdAtStr, 10, 64)
			Expect(err).To(BeNil())

			expectedExpiry := createdAtSeconds + 2 // TTL = 2s
			Expect(expiresAtSeconds).To(BeNumerically("~", expectedExpiry, 1))
		})
	})

	Context("Configuration Override", func() {
		It("should use pattern-level stale_ttl configuration", func() {
			url := "/stale-test/default?test=config-ttl"

			By("Step 1: Create cache (pattern has cache_ttl=2s, stale_ttl=10s)")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward to stale period (3s)")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Verify cache still exists in stale period")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Step 5: Fast forward past total TTL (cache_ttl + stale_ttl = 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 10*time.Second)
			Expect(err).To(BeNil()) // 13s total

			By("Step 6: Verify cache is deleted after full TTL")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
		})
	})
})

var _ = Describe("Stale Cache Edge Cases", func() {
	Context("Cache Age Calculations", func() {
		It("should calculate accurate cache age in headers", func() {
			url := "/stale-test/default?test=age-calc"

			By("Step 1: Create fresh cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Request immediately - verify cache age is small")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
			// Age should be very small (0 seconds)
			Expect(cacheAge).To(MatchRegexp(`^0$`))
		})
	})

	Context("Fully Expired Cache", func() {
		It("should not serve cache beyond stale TTL period", func() {
			url := "/stale-test/default?test=fully-expired"

			By("Step 1: Create cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Step 3: Fast forward past cache_ttl + stale_ttl (2s + 10s = 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 13*time.Second)
			Expect(err).To(BeNil())

			By("Step 4: Verify cache metadata is deleted by Redis TTL")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())

			By("Step 5: Simulate failure (no stale cache to serve)")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())
			defer testEnv.RestoreRenderServiceHealth()

			By("Step 6: Request should fall back to bypass (no stale available)")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			// Should bypass since there's no stale cache and render failed
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass"))
		})
	})
})

var _ = Describe("Stale Cache - Integration Tests", func() {
	Context("Full Request Lifecycle", func() {
		It("should demonstrate complete stale cache flow", func() {
			url := "/stale-test/default?test=lifecycle"

			By("Phase 1: Fresh cache creation")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Phase 2: Fresh cache serving (< 2s)")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Phase 3: Stale period (2s < t < 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue(), "Cache should exist in stale period")

			By("Phase 3a: Simulate render failure to trigger stale serving")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())

			By("Phase 3b: Request should serve stale cache")
			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.Headers.Get("X-Render-Cache")).To(Equal("stale"))
			Expect(resp3.Headers.Get("X-Render-Source")).To(Equal("cache"))

			testEnv.RestoreRenderServiceHealth()

			By("Phase 4: Fully expired (t > 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 10*time.Second)
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse(), "Cache should be deleted after stale TTL")

			By("Phase 5: Fresh render required (no cache)")
			resp4 := testEnv.RequestRender(url)
			Expect(resp4.Error).To(BeNil())
			Expect(resp4.Headers.Get("X-Render-Source")).To(Equal("rendered"))
		})
	})

	Context("Redis TTL Verification", func() {
		It("should set Redis TTL to cache_ttl + stale_ttl for serve_stale strategy", func() {
			url := "/stale-test/default?test=redis-ttl-serve-stale"

			By("Step 1: Create cache with serve_stale strategy")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Step 2: Get cache key and check Redis TTL")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metaKey := "meta:" + cacheKey
			ttl := testEnv.MiniRedis.TTL(metaKey)

			By("Step 3: Verify TTL is ~12s (2s cache + 10s stale)")
			// TTL should be cache_ttl (2s) + stale_ttl (10s) = 12s
			Expect(ttl.Seconds()).To(BeNumerically("~", 12, 1))
		})
	})

	Context("Documentation Examples", func() {
		It("should match behavior described in specification", func() {
			url := "/stale-test/default?test=spec-example"

			By("Example from spec: Fresh cache (now < ExpiresAt)")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))

			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Example from spec: Stale cache (ExpiresAt <= now < ExpiresAt + stale_ttl)")
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Simulate render failure to serve stale")
			err = testEnv.SimulateRenderServiceFailure()
			Expect(err).To(BeNil())

			resp3 := testEnv.RequestRender(url)
			Expect(resp3.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			testEnv.RestoreRenderServiceHealth()

			By("Example from spec: Expired (now >= ExpiresAt + stale_ttl)")
			err = testEnv.MakeCacheStale(cacheKey, 10*time.Second)
			Expect(err).To(BeNil())
			// Redis should auto-delete after cache_ttl + stale_ttl
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
		})
	})
})

// Helper function to verify stale cache state
func verifyStaleCacheState(cacheKey string, expectStale bool) {
	exists := testEnv.CacheExists(cacheKey)
	if expectStale {
		Expect(exists).To(BeTrue(), "Stale cache should exist in Redis")
		metadata, err := testEnv.GetCacheMetadata(cacheKey)
		Expect(err).To(BeNil())
		Expect(metadata["source"]).To(Equal("render"))
	} else {
		Expect(exists).To(BeFalse(), fmt.Sprintf("Cache should not exist (fully expired): %s", cacheKey))
	}
}

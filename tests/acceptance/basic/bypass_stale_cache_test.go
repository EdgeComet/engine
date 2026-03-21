package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bypass Stale Cache", Serial, func() {

	Context("Stale Serving on Origin Failure", func() {

		It("should serve stale bypass cache when origin returns 5xx", func() {
			url := "/bypass-stale-test/default?test=origin-5xx"

			By("Seeding bypass cache with successful response")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))
			originalBody := resp1.Body

			By("Getting cache key and making it stale")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Verifying cache still exists in stale period")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Setting status override to simulate origin failure")
			err = testEnv.SetStatusOverride(url, 500)
			Expect(err).To(BeNil())

			By("Requesting again - should serve stale bypass cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			By("Verifying response body matches original cached content")
			Expect(resp2.Body).To(Equal(originalBody))
		})

		It("should serve fresh content when origin succeeds even with stale available", func() {
			url := "/bypass-stale-test/default?test=fresh-over-stale"

			By("Seeding bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Making cache stale")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Requesting again with healthy origin - should serve fresh bypass")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying new cache entry was created")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
		})

		It("should not serve stale when fully expired beyond stale_ttl", func() {
			url := "/bypass-stale-test/default?test=fully-expired"

			By("Seeding bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Getting cache key")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			By("Fast forwarding beyond stale_ttl (cache_ttl=2s + stale_ttl=10s = 12s)")
			err = testEnv.MakeCacheStale(cacheKey, 13*time.Second)
			Expect(err).To(BeNil())

			By("Verifying cache is fully expired")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())

			By("Setting status override to simulate origin failure")
			err = testEnv.SetStatusOverride(url, 500)
			Expect(err).To(BeNil())

			By("Requesting - should get 500 since no stale cache is available")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(500))
		})

		It("should not serve stale with delete strategy", func() {
			url := "/bypass-stale-test/delete-strategy?test=no-stale"

			By("Seeding bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Making cache stale")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Setting status override to simulate origin failure")
			err = testEnv.SetStatusOverride(url, 500)
			Expect(err).To(BeNil())

			By("Requesting - should get 500 since delete strategy does not serve stale")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(500))
		})
	})

	Context("Response Headers Validation", func() {

		It("should set correct response headers for stale bypass cache", func() {
			url := "/bypass-stale-test/default?test=stale-headers"

			By("Seeding bypass cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Making cache stale")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			err = testEnv.MakeCacheStale(cacheKey, 3*time.Second)
			Expect(err).To(BeNil())

			By("Setting status override to trigger stale serving")
			err = testEnv.SetStatusOverride(url, 500)
			Expect(err).To(BeNil())

			By("Requesting and verifying stale response headers")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.StatusCode).To(Equal(200))
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("stale"))

			By("Verifying X-Cache-Age header is present and reasonable")
			cacheAge := resp2.Headers.Get("X-Cache-Age")
			Expect(cacheAge).NotTo(BeEmpty())
			Expect(cacheAge).To(MatchRegexp(`^[0-9]+$`))
		})
	})

	Context("Redis TTL Verification", func() {

		It("should set Redis TTL to include stale_ttl for serve_stale strategy", func() {
			url := "/bypass-stale-test/default?test=redis-ttl"

			By("Seeding bypass cache (ttl=2s, stale_ttl=10s)")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Getting cache key and checking Redis TTL")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())

			metaKey := "meta:" + cacheKey
			ttl := testEnv.MiniRedis.TTL(metaKey)

			By("Verifying TTL is approximately 12s (2s cache + 10s stale)")
			Expect(ttl.Seconds()).To(BeNumerically("~", 12, 1))
		})
	})
})

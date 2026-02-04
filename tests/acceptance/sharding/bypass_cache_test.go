package sharding_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Bypass Cache Sharding", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	Context("Large Response Replication", Serial, func() {
		It("should replicate bypass cache for large responses", func() {
			// Use unique test parameter to isolate this test
			testURL := "/api/large-json?test=bypass_large_replicate"
			testName := "bypass-large-replicate"

			By("Computing cache key for large JSON response")
			fullURL := testEnv.Config.TestPagesURL() + testURL
			result, err := normalizer.Normalize(fullURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 to create bypass cache")
			response1 := testEnv.RequestViaEG1(testURL, testName)

			By("Verifying the request succeeded")
			Expect(response1.Error).To(BeNil(), "Request should not have network errors")
			Expect(response1.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Render-Source header indicates bypass")
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"First request should be bypass (not rendered)")

			By("Verifying bypass cache metadata exists in Redis")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil(), "Bypass cache metadata should exist in Redis")
			Expect(metadata).NotTo(BeEmpty())

			By("Verifying source is bypass")
			Expect(metadata["source"]).To(Equal("bypass"),
				"Cache source should be bypass")

			By("Verifying eg_ids field is present and non-empty")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil(), "Should successfully parse eg_ids")
			Expect(egIDs).NotTo(BeEmpty(), "eg_ids should not be empty")

			By("Verifying eg_ids count matches replication factor (RF=2, body >100 bytes)")
			GinkgoWriter.Printf("DEBUG: eg_ids after initial request = %v\n", egIDs)
			Expect(len(egIDs)).To(Equal(2),
				"eg_ids should contain exactly 2 EGs (RF=2, large response replicates)")

			By("Verifying rendering EG (eg1) is in eg_ids")
			Expect(egIDs).To(ContainElement("eg1"),
				"Rendering EG (eg1) must be included in eg_ids")

			By("Determining which EG received the push")
			var nonMemberEG int
			egIDMap := make(map[string]bool)
			for _, id := range egIDs {
				egIDMap[id] = true
			}

			if !egIDMap["eg2"] {
				nonMemberEG = 2
			} else if !egIDMap["eg3"] {
				nonMemberEG = 3
			} else {
				// Both eg2 and eg3 in eg_ids - should not happen with RF=2
				Fail("Both eg2 and eg3 are in eg_ids with RF=2")
			}

			By("Making request via non-member EG (should pull from cluster)")
			var response2 *TestResponse
			switch nonMemberEG {
			case 2:
				response2 = testEnv.RequestViaEG2(testURL, testName+"-pull-eg2")
			case 3:
				response2 = testEnv.RequestViaEG3(testURL, testName+"-pull-eg3")
			}

			By("Verifying pull request succeeded")
			Expect(response2.Error).To(BeNil(), "Pull request should succeed")
			Expect(response2.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Render-Source indicates bypass_cache (pulled)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("bypass_cache"),
				"Should be served from bypass cache (pulled from cluster)")

			By("Verifying pulled content matches original response")
			// Both are JSON, so we can just compare the bodies (UUIDs will differ, but structure should match)
			Expect(response2.Body).To(ContainSubstring("large-json-response"),
				"Pulled response should have same type as original")
		})
	})

	Context("Small Response Behavior", Serial, func() {
		It("should skip replication for small bypass responses", func() {
			// Use unique test parameter to isolate this test
			testURL := "/api/small-response?test=bypass_small_no_replicate"
			testName := "bypass-small-no-replicate"

			By("Computing cache key for small response")
			fullURL := testEnv.Config.TestPagesURL() + testURL
			result, err := normalizer.Normalize(fullURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 (body < 100 bytes)")
			response1 := testEnv.RequestViaEG1(testURL, testName)

			By("Verifying the request succeeded with 200 status")
			Expect(response1.Error).To(BeNil(), "Request should not have network errors")
			Expect(response1.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying bypass cache metadata exists in Redis")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil(), "Bypass cache metadata should exist")
			Expect(metadata).NotTo(BeEmpty())

			By("Verifying source is bypass")
			Expect(metadata["source"]).To(Equal("bypass"),
				"Cache source should be bypass")

			By("Verifying eg_ids contains only eg1 (body ≤100 bytes, no replication)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil(), "Should successfully parse eg_ids")

			// Small body (< 100 bytes) should NOT replicate, only stored on eg1
			Expect(egIDs).To(Equal([]string{"eg1"}),
				"Small bypass response should only be cached on rendering EG (no replication)")

			By("Making request via EG2 (should NOT have local cache)")
			response2 := testEnv.RequestViaEG2(testURL, testName+"-eg2")

			By("Verifying EG2 request succeeded")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying EG2 response source")
			// EG2 should either:
			// 1. Fetch fresh from origin (X-Render-Source: bypass), OR
			// 2. Pull from eg1 if configured (X-Render-Source: bypass_cache)
			renderSource := response2.Headers.Get("X-Render-Source")
			Expect(renderSource).To(BeElementOf([]string{"bypass", "bypass_cache"}),
				"EG2 should either fetch fresh or pull from cluster")
		})
	})

	Context("Status Code Filtering", Serial, func() {
		It("should replicate cacheable status codes only", func() {
			// Config has status_codes: [200, 404] for /api/status-filter

			By("Testing status 200 - should cache and replicate")
			testURL200 := "/api/status-filter?test=bypass_status_200&status=200"
			testName200 := "bypass-status-200"

			fullURL200 := testEnv.Config.TestPagesURL() + testURL200
			result200, err := normalizer.Normalize(fullURL200, nil)
			Expect(err).To(BeNil())
			urlHash200 := normalizer.Hash(result200.NormalizedURL)
			cacheKey200 := testEnv.BuildCacheKey(1, 1, urlHash200)

			response200 := testEnv.RequestViaEG1(testURL200, testName200)
			Expect(response200.Error).To(BeNil())
			Expect(response200.StatusCode).To(Equal(200))
			Expect(response200.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying status 200 response is cached")
			metadata200, err := testEnv.GetRedisMetadata(cacheKey200)
			Expect(err).To(BeNil(), "Status 200 should be cached")
			Expect(metadata200).NotTo(BeEmpty())
			Expect(metadata200["source"]).To(Equal("bypass"))
			Expect(metadata200["status_code"]).To(Equal("200"))

			By("Verifying status 200 response is replicated (RF=2)")
			egIDs200, err := testEnv.GetEGIDs(cacheKey200)
			Expect(err).To(BeNil())
			Expect(len(egIDs200)).To(Equal(2),
				"Status 200 response should replicate to RF=2 EGs (body >100 bytes)")

			By("Testing status 404 - should cache and replicate")
			testURL404 := "/api/status-filter?test=bypass_status_404&status=404"
			testName404 := "bypass-status-404"

			fullURL404 := testEnv.Config.TestPagesURL() + testURL404
			result404, err := normalizer.Normalize(fullURL404, nil)
			Expect(err).To(BeNil())
			urlHash404 := normalizer.Hash(result404.NormalizedURL)
			cacheKey404 := testEnv.BuildCacheKey(1, 1, urlHash404)

			response404 := testEnv.RequestViaEG1(testURL404, testName404)
			Expect(response404.Error).To(BeNil())
			Expect(response404.StatusCode).To(Equal(404))
			Expect(response404.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying status 404 response is cached")
			metadata404, err := testEnv.GetRedisMetadata(cacheKey404)
			Expect(err).To(BeNil(), "Status 404 should be cached (in status_codes list)")
			Expect(metadata404).NotTo(BeEmpty())
			Expect(metadata404["source"]).To(Equal("bypass"))
			Expect(metadata404["status_code"]).To(Equal("404"))

			By("Verifying status 404 response is replicated (RF=2)")
			egIDs404, err := testEnv.GetEGIDs(cacheKey404)
			Expect(err).To(BeNil())
			Expect(len(egIDs404)).To(Equal(2),
				"Status 404 response should replicate to RF=2 EGs (body >100 bytes)")

			By("Testing status 500 - should NOT cache")
			testURL500 := "/api/status-filter?test=bypass_status_500&status=500"
			testName500 := "bypass-status-500"

			fullURL500 := testEnv.Config.TestPagesURL() + testURL500
			result500, err := normalizer.Normalize(fullURL500, nil)
			Expect(err).To(BeNil())
			urlHash500 := normalizer.Hash(result500.NormalizedURL)
			cacheKey500 := testEnv.BuildCacheKey(1, 1, urlHash500)

			response500 := testEnv.RequestViaEG1(testURL500, testName500)
			Expect(response500.Error).To(BeNil())
			Expect(response500.StatusCode).To(Equal(500))
			Expect(response500.Headers.Get("X-Render-Source")).To(Equal("bypass"))

			By("Verifying status 500 response is NOT cached")
			metadata500, err := testEnv.GetRedisMetadata(cacheKey500)
			// Should get error or empty metadata (status 500 not in cacheable list)
			if err == nil {
				Expect(metadata500).To(BeEmpty(),
					"Status 500 should NOT be cached (not in status_codes list)")
			}
		})
	})
})

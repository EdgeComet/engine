package sharding_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Basic Sharding Operations", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
		testURL    string
		urlHash    string
		cacheKey   string
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
		testURL = testEnv.Config.TestPagesURL() + "/static/test.html"

		// Compute URL hash for cache key verification
		result, err := normalizer.Normalize(testURL, nil)
		Expect(err).To(BeNil())
		urlHash = normalizer.Hash(result.NormalizedURL)

		// Build cache key: meta:cache:{host_id}:{dimension_id}:{url_hash}
		// Using hostID=1, dimensionID=1 from test config
		cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)
	})

	Context("Render and Push", Serial, func() {
		It("should render on EG1 and push to cluster", func() {
			// Override BeforeEach URL for this test
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=basic_render_push"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1")
			response := testEnv.RequestViaEG1("/static/test.html?test=basic_render_push")

			By("Verifying the request succeeded")
			Expect(response.Error).To(BeNil(), "Request should not have network errors")
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying the HTML content is present")
			Expect(response.Body).To(ContainSubstring("Sharding Test Page"))
			Expect(response.Body).To(ContainSubstring("This is a test page for sharding acceptance tests"))

			By("Verifying X-Render-Source header indicates fresh render")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"First request should be freshly rendered")

			By("Verifying cache metadata exists in Redis")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil(), "Cache metadata should exist in Redis")
			Expect(metadata).NotTo(BeEmpty())

			By("Verifying eg_ids field is present and non-empty")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil(), "Should successfully parse eg_ids")
			Expect(egIDs).NotTo(BeEmpty(), "eg_ids should not be empty")

			By("Verifying eg_ids count matches replication factor (RF=2)")
			Expect(len(egIDs)).To(Equal(2),
				"eg_ids should contain exactly 2 EGs (replication_factor=2)")

			By("Verifying rendering EG (eg1) is always in eg_ids")
			Expect(egIDs).To(ContainElement("eg1"),
				"Rendering EG (eg1) must always be included in eg_ids")
		})

		It("should serve from local cache on subsequent request", func() {
			// Override BeforeEach URL for this test
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=basic_local_cache"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making first request via EG1 to populate cache")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=basic_local_cache")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Making second request via EG1 (should hit cache)")
			response2 := testEnv.RequestViaEG1("/static/test.html?test=basic_local_cache")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying X-Render-Source indicates cache hit")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Second request should be served from cache")

			By("Verifying X-Render-Cache header")
			Expect(response2.Headers.Get("X-Render-Cache")).To(Equal("hit"),
				"Cache header should indicate hit")

			By("Verifying response content matches original render")
			Expect(response2.Body).To(Equal(response1.Body),
				"Cached content should be identical to original render")

			By("Verifying cache response is fast")
			Expect(response2.Duration).To(BeNumerically("<", 500*time.Millisecond),
				"Cache hit should be fast (< 500ms)")
		})

		It("should pull from remote EG on cache miss", func() {
			// Override BeforeEach URL for this test
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=basic_remote_pull"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 to populate cache")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=basic_remote_pull")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Getting eg_ids to determine which EG has the cache")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).NotTo(BeEmpty())

			By("Determining which EG does NOT have cache")
			// Find an EG that's not in eg_ids to test remote pull
			var pullFromEG int
			egIDMap := make(map[string]bool)
			for _, id := range egIDs {
				egIDMap[id] = true
			}

			// Choose an EG that doesn't have the cache
			if !egIDMap["eg2"] {
				pullFromEG = 2
			} else if !egIDMap["eg3"] {
				pullFromEG = 3
			} else {
				// All EGs have cache, skip this test
				Skip("All EGs have cache, cannot test remote pull")
			}

			By("Making request via EG without local cache (should pull from cluster)")
			var response2 *TestResponse
			switch pullFromEG {
			case 2:
				response2 = testEnv.RequestViaEG2("/static/test.html?test=basic_remote_pull")
			case 3:
				response2 = testEnv.RequestViaEG3("/static/test.html?test=basic_remote_pull")
			}

			By("Verifying pull request succeeded")
			Expect(response2.Error).To(BeNil(), "Pull request should succeed")
			Expect(response2.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Render-Source indicates cache (pulled from remote)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should be served from cache (pulled from remote EG)")

			By("Verifying pulled content matches original render")
			Expect(response2.Body).To(Equal(response1.Body),
				"Pulled content should match original render")
		})
	})

	Context("Replication Management", Serial, func() {
		It("should add self to eg_ids after pull when under-replicated", func() {
			// Compute cache key for pull-only.html (different from BeforeEach which uses test.html)
			pullOnlyURL := testEnv.Config.TestPagesURL() + "/static/pull-only.html?test=basic_under_replicated"
			pullOnlyResult, err := normalizer.Normalize(pullOnlyURL, nil)
			Expect(err).To(BeNil())
			pullOnlyHash := normalizer.Hash(pullOnlyResult.NormalizedURL)
			pullOnlyCacheKey := testEnv.BuildCacheKey(1, 1, pullOnlyHash)

			By("Making initial request via EG1 to create cache (pull-only, no push)")
			response1 := testEnv.RequestViaEG1("/static/pull-only.html?test=basic_under_replicated", "test4-initial-render-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Verifying content is from pull-only test page")
			Expect(response1.Body).To(ContainSubstring("Pull-Only Sharding Test Page"))

			By("Verifying eg_ids contains only eg1 (no push occurred)")
			// With push_on_render: false, cache should only exist on rendering EG
			egIDs, err := testEnv.GetEGIDs(pullOnlyCacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}), "eg_ids should contain only eg1 (no push)")

			By("Making request via EG2 (should pull from cluster and add self)")
			response2 := testEnv.RequestViaEG2("/static/pull-only.html?test=basic_under_replicated", "test4-pull-from-eg2")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Waiting for eg_ids update to complete")
			Eventually(func() []string {
				ids, _ := testEnv.GetEGIDs(pullOnlyCacheKey)
				//fmt.Println("==================v")
				//fmt.Println("IDS:", ids)
				return ids
			}, 3*time.Second, 200*time.Millisecond).Should(HaveLen(2),
				"eg_ids should be updated to contain 2 EGs")

			By("Verifying eg_ids now contains both eg1 and eg2")
			finalEgIDs, err := testEnv.GetEGIDs(pullOnlyCacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).To(ContainElement("eg1"))
			Expect(finalEgIDs).To(ContainElement("eg2"))
			Expect(len(finalEgIDs)).To(Equal(2),
				"eg_ids should have exactly 2 EGs (moved toward RF=2)")
		})

		It("should serve from local cache after push without pulling", func() {
			// Override BeforeEach URL for this test
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=basic_push_no_pull"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 to create cache (with push enabled)")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=basic_push_no_pull", "test-push-initial")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying content is from standard test page")
			Expect(response1.Body).To(ContainSubstring("Sharding Test Page"))

			By("Verifying eg_ids contains 2 EGs due to push_on_render")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2), "Push should distribute cache to RF=2 EGs")
			Expect(egIDs).To(ContainElement("eg1"), "eg1 (rendering EG) must be in eg_ids")

			By("Determining which EG received the push")
			var pushedToEG int
			var pushedToID string
			for _, id := range egIDs {
				if id != "eg1" {
					pushedToEG, _ = map[string]int{"eg2": 2, "eg3": 3}[id]
					pushedToID = id
					break
				}
			}

			By("Making request via EG that received push (should serve from local cache)")
			var response2 *TestResponse
			switch pushedToEG {
			case 2:
				response2 = testEnv.RequestViaEG2("/static/test.html?test=basic_push_no_pull", "test-push-eg2-local")
			case 3:
				response2 = testEnv.RequestViaEG3("/static/test.html?test=basic_push_no_pull", "test-push-eg3-local")
			}

			By("Verifying request served from local cache (not pulled)")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should be served from cache (local file from push)")

			By("Verifying content matches original render")
			Expect(response2.Body).To(Equal(response1.Body))

			By("Verifying eg_ids remains unchanged (no pull occurred)")
			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(finalEgIDs)).To(Equal(2), "eg_ids count should remain at 2")
			Expect(finalEgIDs).To(ContainElement("eg1"))
			Expect(finalEgIDs).To(ContainElement(pushedToID))

			By("Verifying no additional EG was added to eg_ids")
			// This confirms no pull operation occurred (pull would add the requesting EG)
			for _, id := range finalEgIDs {
				Expect(egIDs).To(ContainElement(id), "Original eg_ids should be preserved")
			}
		})

		It("should NOT add self when already at replication factor", func() {
			// Override BeforeEach URL for this test
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=basic_at_rf"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 to create cache with RF=2")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=basic_at_rf")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Getting initial eg_ids (should be 2 EGs due to RF=2)")
			initialEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(initialEgIDs)).To(Equal(2),
				"Initial eg_ids should have exactly 2 EGs (RF=2)")

			By("Determining which EG is NOT in eg_ids")
			egIDMap := make(map[string]bool)
			for _, id := range initialEgIDs {
				egIDMap[id] = true
			}

			var nonMemberEG int
			var nonMemberID string

			if !egIDMap["eg1"] {
				nonMemberEG = 1
				nonMemberID = "eg1"
			} else if !egIDMap["eg2"] {
				nonMemberEG = 2
				nonMemberID = "eg2"
			} else if !egIDMap["eg3"] {
				nonMemberEG = 3
				nonMemberID = "eg3"
			} else {
				Fail("All EGs are in eg_ids, cannot test this scenario")
			}

			By("Making request via non-member EG (should NOT add self)")
			var response2 *TestResponse
			switch nonMemberEG {
			case 1:
				response2 = testEnv.RequestViaEG1("/static/test.html?test=basic_at_rf")
			case 2:
				response2 = testEnv.RequestViaEG2("/static/test.html?test=basic_at_rf")
			case 3:
				response2 = testEnv.RequestViaEG3("/static/test.html?test=basic_at_rf")
			}

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Request should be served from cache (pulled)")

			By("Verifying eg_ids is unchanged")
			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(finalEgIDs)).To(Equal(2),
				"eg_ids count should remain at 2 (already at RF)")

			By("Verifying non-member EG was NOT added to eg_ids")
			Expect(finalEgIDs).NotTo(ContainElement(nonMemberID),
				"Non-member EG should NOT be added when already at RF")

			By("Verifying eg_ids contains same EGs as before")
			for _, id := range initialEgIDs {
				Expect(finalEgIDs).To(ContainElement(id),
					"Original EG IDs should still be present")
			}
		})
	})

	Context("Proxy Mode (replicate_on_pull: false)", Serial, func() {
		It("should pull and serve without storing when replicate_on_pull is false", func() {
			// Compute cache key for proxy-mode.html
			proxyModeURL := testEnv.Config.TestPagesURL() + "/static/proxy-mode.html?test=basic_proxy_mode"
			proxyModeResult, err := normalizer.Normalize(proxyModeURL, nil)
			Expect(err).To(BeNil())
			proxyModeHash := normalizer.Hash(proxyModeResult.NormalizedURL)
			proxyModeCacheKey := testEnv.BuildCacheKey(1, 1, proxyModeHash)

			By("Making initial request via EG1 to create cache")
			response1 := testEnv.RequestViaEG1("/static/proxy-mode.html?test=basic_proxy_mode", "proxy-mode-initial-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying content is from proxy-mode test page")
			Expect(response1.Body).To(ContainSubstring("Proxy Mode Test Page"))

			By("Verifying eg_ids contains only eg1 (rendering EG)")
			initialEgIDs, err := testEnv.GetEGIDs(proxyModeCacheKey)
			Expect(err).To(BeNil())
			Expect(initialEgIDs).To(ContainElement("eg1"))

			By("Determining an EG that doesn't have the cache")
			egIDMap := make(map[string]bool)
			for _, id := range initialEgIDs {
				egIDMap[id] = true
			}

			var proxyEG int
			var proxyID string
			if !egIDMap["eg2"] {
				proxyEG = 2
				proxyID = "eg2"
			} else if !egIDMap["eg3"] {
				proxyEG = 3
				proxyID = "eg3"
			} else {
				Skip("All EGs have cache, cannot test proxy mode")
			}

			By("Making request via EG without cache (should pull but NOT store)")
			var response2 *TestResponse
			switch proxyEG {
			case 2:
				response2 = testEnv.RequestViaEG2("/static/proxy-mode.html?test=basic_proxy_mode", "proxy-mode-pull-eg2")
			case 3:
				response2 = testEnv.RequestViaEG3("/static/proxy-mode.html?test=basic_proxy_mode", "proxy-mode-pull-eg3")
			}

			By("Verifying request succeeded via pull")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should be served from cache (pulled from remote)")

			By("Verifying pulled content matches original render")
			Expect(response2.Body).To(Equal(response1.Body),
				"Pulled content should match original render")

			By("Verifying eg_ids is unchanged (proxy EG NOT added)")
			finalEgIDs, err := testEnv.GetEGIDs(proxyModeCacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).NotTo(ContainElement(proxyID),
				"Proxy EG should NOT be added to eg_ids when replicate_on_pull is false")
			Expect(finalEgIDs).To(Equal(initialEgIDs),
				"eg_ids should remain unchanged (proxy mode)")

			By("Making another request to same EG (should pull again, no local cache)")
			var response3 *TestResponse
			switch proxyEG {
			case 2:
				response3 = testEnv.RequestViaEG2("/static/proxy-mode.html?test=basic_proxy_mode", "proxy-mode-pull-again-eg2")
			case 3:
				response3 = testEnv.RequestViaEG3("/static/proxy-mode.html?test=basic_proxy_mode", "proxy-mode-pull-again-eg3")
			}

			By("Verifying subsequent request still works (pulls again)")
			Expect(response3.Error).To(BeNil())
			Expect(response3.StatusCode).To(Equal(200))
			Expect(response3.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should still serve from cache (pulled again)")
			Expect(response3.Body).To(Equal(response1.Body))

			By("Final verification: eg_ids still unchanged")
			veryFinalEgIDs, err := testEnv.GetEGIDs(proxyModeCacheKey)
			Expect(err).To(BeNil())
			Expect(veryFinalEgIDs).To(Equal(initialEgIDs),
				"eg_ids should never change in proxy mode")
		})
	})
})

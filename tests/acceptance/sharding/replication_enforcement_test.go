package sharding_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Replication Factor Enforcement", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
		testURL    string
		urlHash    string
		cacheKey   string
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	Context("Render Cache Replication Limits", Serial, func() {
		It("should not exceed replication factor when pulling from remote", func() {
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=replication_limit"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making first request via EG1 to trigger render and push")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=replication_limit")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying cache exists with replication factor = 2")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2), "Should have exactly 2 replicas (RF=2)")

			By("Recording which EGs have the cache")
			initialEGIDs := make(map[string]bool)
			for _, egID := range egIDs {
				initialEGIDs[egID] = true
			}

			By("Finding an EG that does NOT have the cache")
			var nonOwnerEG string
			allEGs := []string{"eg1", "eg2", "eg3"}
			for _, eg := range allEGs {
				if !initialEGIDs[eg] {
					nonOwnerEG = eg
					break
				}
			}
			Expect(nonOwnerEG).NotTo(BeEmpty(), "Should find at least one EG without cache")

			By("Making request via non-owner EG (should pull but NOT store)")
			var response2 *TestResponse
			switch nonOwnerEG {
			case "eg1":
				response2 = testEnv.RequestViaEG1("/static/test.html?test=replication_limit")
			case "eg2":
				response2 = testEnv.RequestViaEG2("/static/test.html?test=replication_limit")
			case "eg3":
				response2 = testEnv.RequestViaEG3("/static/test.html?test=replication_limit")
			}
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("Sharding Test Page"))

			By("Verifying cache is served (pull to memory worked)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should serve from cache (pulled to memory)")

			By("Waiting briefly for any potential metadata update")
			time.Sleep(500 * time.Millisecond)

			By("Verifying replication factor is STILL 2 (not increased)")
			egIDsAfter, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDsAfter)).To(Equal(2),
				"Should still have exactly 2 replicas (replication factor enforced)")

			By("Verifying the same EGs have the cache (non-owner didn't store)")
			afterEGIDs := make(map[string]bool)
			for _, egID := range egIDsAfter {
				afterEGIDs[egID] = true
			}
			Expect(afterEGIDs).To(Equal(initialEGIDs),
				"eg_ids should be unchanged (non-owner EG didn't add itself)")
		})
	})

	Context("Bypass Cache Replication Limits", Serial, func() {
		It("should not store bypass cache when at replication factor", func() {
			// Enable bypass mode for this URL pattern
			testURL = testEnv.Config.TestPagesURL() + "/bypass-test/replication.html"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)

			// Bypass cache uses same key structure
			cacheKey = "meta:bypass_cache:1:1:" + urlHash

			By("Making first bypass request via EG1")
			response1 := testEnv.RequestViaEG1("/bypass-test/replication.html")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Verifying X-Render-Source indicates bypass")
			source := response1.Headers.Get("X-Render-Source")
			Expect(source).To(Or(Equal("bypass"), Equal("bypass_cache")),
				"Should be served via bypass mode")

			By("Waiting for bypass cache to propagate")
			time.Sleep(1 * time.Second)

			By("Checking if bypass cache exists in Redis")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			if err != nil || len(egIDs) == 0 {
				Skip("Bypass cache not stored in Redis, skipping replication test")
			}

			By("Verifying bypass cache has correct replication")
			Expect(len(egIDs)).To(BeNumerically("<=", 2),
				"Bypass cache should not exceed replication factor")

			By("Recording initial eg_ids")
			initialCount := len(egIDs)
			initialEGIDs := make(map[string]bool)
			for _, egID := range egIDs {
				initialEGIDs[egID] = true
			}

			By("Finding an EG without the bypass cache")
			var nonOwnerEG string
			allEGs := []string{"eg1", "eg2", "eg3"}
			for _, eg := range allEGs {
				if !initialEGIDs[eg] {
					nonOwnerEG = eg
					break
				}
			}

			if nonOwnerEG == "" {
				Skip("All EGs already have bypass cache, cannot test replication limit")
			}

			By("Making bypass request via non-owner EG")
			var response2 *TestResponse
			switch nonOwnerEG {
			case "eg1":
				response2 = testEnv.RequestViaEG1("/bypass-test/replication.html")
			case "eg2":
				response2 = testEnv.RequestViaEG2("/bypass-test/replication.html")
			case "eg3":
				response2 = testEnv.RequestViaEG3("/bypass-test/replication.html")
			}
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Waiting for potential metadata update")
			time.Sleep(500 * time.Millisecond)

			By("Verifying replication count didn't increase beyond limit")
			egIDsAfter, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())

			if initialCount >= 2 {
				Expect(len(egIDsAfter)).To(Equal(initialCount),
					"Bypass cache replication should not exceed configured limit")
			} else {
				Expect(len(egIDsAfter)).To(BeNumerically("<=", 2),
					"Bypass cache should not exceed replication factor=2")
			}
		})
	})

	Context("Under-Replicated Cache Healing", Serial, func() {
		It("should store cache when under-replicated", func() {
			testURL = testEnv.Config.TestPagesURL() + "/static/test.html?test=healing"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash = normalizer.Hash(result.NormalizedURL)
			cacheKey = testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making first request via EG1")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=healing")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Verifying initial replication count")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			initialCount := len(egIDs)
			Expect(initialCount).To(BeNumerically(">=", 1))

			if initialCount >= 2 {
				Skip("Cache already at replication factor, cannot test healing")
			}

			By("Making request via different EG to trigger healing")
			response2 := testEnv.RequestViaEG2("/static/test.html?test=healing")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Waiting for replication to complete")
			time.Sleep(1 * time.Second)

			By("Verifying replication count increased")
			egIDsAfter, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDsAfter)).To(BeNumerically(">", initialCount),
				"Under-replicated cache should allow storage until reaching RF")
			Expect(len(egIDsAfter)).To(BeNumerically("<=", 2),
				"Should not exceed replication factor")
		})
	})
})

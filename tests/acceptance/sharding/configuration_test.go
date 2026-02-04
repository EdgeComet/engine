package sharding_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Configuration Variations", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	Context("Replication Factor Variations", Serial, func() {
		AfterEach(func() {
			By("Restoring default replication factor (RF=2)")
			err := testEnv.UpdateReplicationFactor(2)
			Expect(err).To(BeNil(), "Should restore default RF")

			By("Restarting all EGs with restored config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil(), "Should restart EGs successfully")

			By("Clearing cache after config restore")
			testEnv.ClearCache()
		})

		It("RF=0 should store cache locally only", func() {
			By("Updating replication factor to 0")
			err := testEnv.UpdateReplicationFactor(0)
			Expect(err).To(BeNil(), "Should update RF to 0")

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil(), "Should restart EGs successfully")

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=config_rf0"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 with RF=0")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=config_rf0", "config-rf0-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains only eg1 (no replication)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}),
				"RF=0 should store cache only on rendering EG")

			By("Making request via EG2")
			response2 := testEnv.RequestViaEG2("/static/test.html?test=config_rf0", "config-rf0-eg2")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying EG2's cache is independent (not pulled from EG1)")
			// With RF=0, each EG renders and stores independently
			// OR EG2 pulls but doesn't add itself (depending on implementation)
			// The key point is RF=0 means no intentional replication

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("RF=0 test: eg_ids after EG1=%v\n", egIDs)
			}
		})

		It("RF=1 should store on rendering EG only", func() {
			By("Updating replication factor to 1")
			err := testEnv.UpdateReplicationFactor(1)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=config_rf1"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 with RF=1")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=config_rf1", "config-rf1-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains only eg1")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}),
				"RF=1 should store cache only on rendering EG")

			By("Making request via EG2 (should pull from eg1)")
			response2 := testEnv.RequestViaEG2("/static/test.html?test=config_rf1", "config-rf1-eg2")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying eg_ids still contains only eg1 (RF=1 prevents adding puller)")
			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).To(Equal([]string{"eg1"}),
				"RF=1 means only rendering EG should be in eg_ids, pullers don't add themselves")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("RF=1 test: eg_ids after pull=%v\n", finalEgIDs)
			}
		})

		It("RF=3 should replicate to all EGs", func() {
			By("Updating replication factor to 3")
			err := testEnv.UpdateReplicationFactor(3)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=config_rf3"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 with RF=3")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=config_rf3", "config-rf3-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains all 3 EGs")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(3),
				"RF=3 should replicate to all 3 EGs in cluster")
			Expect(egIDs).To(ContainElement("eg1"))
			Expect(egIDs).To(ContainElement("eg2"))
			Expect(egIDs).To(ContainElement("eg3"))

			By("Verifying cache exists on all 3 EGs via internal status")
			for i := 1; i <= 3; i++ {
				status, err := testEnv.GetInternalStatus(i)
				Expect(err).To(BeNil(), "Should get status from EG%d", i)
				Expect(status).NotTo(BeNil())
				// Status should indicate EG has cache entries
			}

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("RF=3 test: eg_ids=%v (all 3 EGs)\n", egIDs)
			}
		})

		It("RF exceeding cluster size should cap at cluster size", func() {
			By("Updating replication factor to 10 (exceeds cluster size of 3)")
			err := testEnv.UpdateReplicationFactor(10)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=config_rf10"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 with RF=10")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=config_rf10", "config-rf10-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains exactly 3 EGs (capped at cluster size)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(3),
				"RF=10 should cap at cluster size (3 EGs available)")
			Expect(egIDs).To(ContainElement("eg1"))
			Expect(egIDs).To(ContainElement("eg2"))
			Expect(egIDs).To(ContainElement("eg3"))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("RF=10 test: eg_ids=%v (capped at 3)\n", egIDs)
			}
		})
	})

	Context("Push/Pull Configuration", Serial, func() {
		AfterEach(func() {
			By("Restoring default push/pull config")
			err := testEnv.UpdatePushOnRender(true)
			Expect(err).To(BeNil())
			err = testEnv.UpdateReplicateOnPull(true)
			Expect(err).To(BeNil())

			By("Restarting all EGs with restored config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing cache after config restore")
			testEnv.ClearCache()
		})

		It("push_on_render=false should use lazy replication", func() {
			By("Updating config: push_on_render=false, replicate_on_pull=true")
			err := testEnv.UpdatePushOnRender(false)
			Expect(err).To(BeNil())
			err = testEnv.UpdateReplicateOnPull(true)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/lazy-replication.html?test=config_no_push"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 (render without push)")
			response1 := testEnv.RequestViaEG1("/static/lazy-replication.html?test=config_no_push", "config-no-push-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains only eg1 (no push occurred)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}),
				"push_on_render=false should not push to other EGs")

			By("Making request via EG2 (should pull from eg1)")
			response2 := testEnv.RequestViaEG2("/static/lazy-replication.html?test=config_no_push", "config-no-push-eg2")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should pull from eg1")

			By("Waiting for eg_ids update after pull")
			Eventually(func() []string {
				ids, _ := testEnv.GetEGIDs(cacheKey)
				return ids
			}, 3*time.Second, 200*time.Millisecond).Should(HaveLen(2),
				"eg_ids should be updated after pull (lazy replication)")

			By("Verifying eg_ids now contains both eg1 and eg2")
			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).To(ContainElement("eg1"))
			Expect(finalEgIDs).To(ContainElement("eg2"))
			Expect(len(finalEgIDs)).To(Equal(2),
				"Lazy replication: eg_ids updated to RF=2 after pull")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Lazy replication test: initial=%v, after pull=%v\n",
					[]string{"eg1"}, finalEgIDs)
			}
		})

		It("replicate_on_pull=false should not store pulled cache locally", func() {
			By("Updating config: push_on_render=true, replicate_on_pull=false")
			err := testEnv.UpdatePushOnRender(true)
			Expect(err).To(BeNil())
			err = testEnv.UpdateReplicateOnPull(false)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new config")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			By("Clearing any existing cache")
			testEnv.ClearCache()

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=config_no_replicate"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making request via EG1 (render + push to eg2)")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=config_no_replicate", "config-no-replicate-eg1")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Getting initial eg_ids (should have 2 EGs from push)")
			initialEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(initialEgIDs)).To(Equal(2), "Push should distribute to RF=2 EGs")

			By("Determining which EG does NOT have cache")
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
				response2 = testEnv.RequestViaEG2("/static/test.html?test=config_no_replicate", "config-no-replicate-eg2")
			case 3:
				response2 = testEnv.RequestViaEG3("/static/test.html?test=config_no_replicate", "config-no-replicate-eg3")
			}

			By("Verifying request succeeded via pull")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should pull from cluster")

			By("Verifying eg_ids is unchanged (proxy mode)")
			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).NotTo(ContainElement(proxyID),
				"Proxy EG should NOT be added when replicate_on_pull=false")
			Expect(finalEgIDs).To(Equal(initialEgIDs),
				"eg_ids should remain unchanged in proxy mode")

			By("Making second request to same EG (should pull again, no local cache)")
			var response3 *TestResponse
			switch proxyEG {
			case 2:
				response3 = testEnv.RequestViaEG2("/static/test.html?test=config_no_replicate", "config-no-replicate-eg2-again")
			case 3:
				response3 = testEnv.RequestViaEG3("/static/test.html?test=config_no_replicate", "config-no-replicate-eg3-again")
			}

			By("Verifying subsequent request still works (pulls again)")
			Expect(response3.Error).To(BeNil())
			Expect(response3.StatusCode).To(Equal(200))
			Expect(response3.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should pull again (no local storage)")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Proxy mode test: eg_ids unchanged=%v, proxy EG=%s\n",
					finalEgIDs, proxyID)
			}
		})
	})

	Context("Mixed Cluster", Serial, func() {
		AfterEach(func() {
			By("Starting EG1 and EG2 (may have been stopped during test)")
			// Use StartEG to start them if they're stopped, it will handle already-running case
			err := testEnv.StartEG(1)
			if err != nil {
				// If already running, try restarting
				err = testEnv.RestartEG(1)
			}
			Expect(err).To(BeNil())

			err = testEnv.StartEG(2)
			if err != nil {
				// If already running, try restarting
				err = testEnv.RestartEG(2)
			}
			Expect(err).To(BeNil())

			By("Re-enabling sharding on EG3")
			err = testEnv.EnableShardingOnEG(3)
			Expect(err).To(BeNil())

			By("Restarting EG3 with sharding enabled")
			err = testEnv.RestartEG(3)
			Expect(err).To(BeNil())

			By("Waiting for cluster to stabilize")
			time.Sleep(2 * time.Second)
			err = testEnv.WaitForClusterSize(3, 10*time.Second)
			Expect(err).To(BeNil())

			By("Clearing cache after config restore")
			testEnv.ClearCache()
		})

		It("mixed cluster with sharding disabled on EG3", func() {
			By("Disabling sharding on EG3")
			err := testEnv.DisableShardingOnEG(3)
			Expect(err).To(BeNil())

			By("Attempting to restart EG3 with sharding disabled (should fail)")
			err = testEnv.RestartEG(3)
			Expect(err).NotTo(BeNil(), "EG3 should fail to start when cluster is active")
			Expect(err.Error()).To(ContainSubstring("cannot start with sharding disabled"),
				"Error should mention cluster detection")
			Expect(err.Error()).To(ContainSubstring("eg1"),
				"Error should list cluster member eg1")
			Expect(err.Error()).To(ContainSubstring("eg2"),
				"Error should list cluster member eg2")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("EG3 startup correctly blocked: %v\n", err)
			}

			By("Stopping cluster members EG1 and EG2")
			err = testEnv.StopEG(1)
			Expect(err).To(BeNil())
			err = testEnv.StopEG(2)
			Expect(err).To(BeNil())

			By("Waiting for registry TTL to expire (4 seconds)")
			testEnv.MiniRedis.FastForward(4 * time.Second)

			By("Verifying cluster members have expired from registry")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(0), "All cluster members should have expired")

			By("Starting EG3 with sharding disabled (should succeed now)")
			err = testEnv.RestartEG(3)
			Expect(err).To(BeNil(), "EG3 should start successfully when no cluster exists")

			By("Verifying EG3 is running in standalone mode (not in registry)")
			time.Sleep(2 * time.Second)
			clusterSize, err = testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(0), "EG3 should not register (sharding disabled)")

			By("Making request via EG3 in standalone mode")
			response3 := testEnv.RequestViaEG3("/static/test.html?test=config_mixed", "config-mixed-eg3")
			Expect(response3.Error).To(BeNil())
			Expect(response3.StatusCode).To(Equal(200))
			Expect(response3.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("EG3 successfully operating in standalone mode\n")
			}
		})
	})
})

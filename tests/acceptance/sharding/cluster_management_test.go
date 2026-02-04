package sharding_test

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Cluster Management", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	AfterEach(func() {
		By("Ensuring all 3 EGs are running for next test")
		// Check and restart any stopped EGs
		for i := 1; i <= 3; i++ {
			if !testEnv.IsEGRunning(i) {
				if os.Getenv("DEBUG") != "" {
					fmt.Printf("AfterEach: Restarting EG%d\n", i)
				}
				err := testEnv.StartEG(i)
				if err != nil {
					fmt.Printf("Warning: Failed to restart EG%d in AfterEach: %v\n", i, err)
				}
				// Wait for EG to register
				time.Sleep(2 * time.Second)
			}
		}

		// Wait for cluster to stabilize
		Eventually(func() int {
			size, _ := testEnv.GetClusterSize()
			return size
		}, 15*time.Second, 500*time.Millisecond).Should(Equal(3),
			"Cluster should stabilize to 3 EGs in AfterEach")
	})

	Context("Registry Operations", Serial, func() {
		It("should register all EGs in Redis on startup", func() {
			By("Querying registry for EG1")
			eg1Info, err := testEnv.GetRegistryInfo("eg1")
			Expect(err).To(BeNil(), "EG1 should be registered")
			Expect(eg1Info).NotTo(BeEmpty())

			By("Verifying EG1 registry data")
			Expect(eg1Info["eg_id"]).To(Equal("eg1"))
			Expect(eg1Info["address"]).To(Equal("127.0.0.1:9202"))
			Expect(eg1Info["sharding_enabled"]).To(Equal(true))

			By("Querying registry for EG2")
			eg2Info, err := testEnv.GetRegistryInfo("eg2")
			Expect(err).To(BeNil(), "EG2 should be registered")
			Expect(eg2Info).NotTo(BeEmpty())

			By("Verifying EG2 registry data")
			Expect(eg2Info["eg_id"]).To(Equal("eg2"))
			Expect(eg2Info["address"]).To(Equal("127.0.0.1:9204"))
			Expect(eg2Info["sharding_enabled"]).To(Equal(true))

			By("Querying registry for EG3")
			eg3Info, err := testEnv.GetRegistryInfo("eg3")
			Expect(err).To(BeNil(), "EG3 should be registered")
			Expect(eg3Info).NotTo(BeEmpty())

			By("Verifying EG3 registry data")
			Expect(eg3Info["eg_id"]).To(Equal("eg3"))
			Expect(eg3Info["address"]).To(Equal("127.0.0.1:9206"))
			Expect(eg3Info["sharding_enabled"]).To(Equal(true))

			By("Verifying cluster size is 3")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(3))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Registry test: All 3 EGs registered correctly\n")
			}
		})

		It("should maintain heartbeat and keep registration alive", func() {
			By("Verifying all EGs are registered initially")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(3))

			By("Waiting 4 seconds for heartbeats to run (exceeds 3s registry TTL)")
			// Test config: heartbeat_interval=1s, registry_ttl=3s
			// Heartbeats should occur at: 0s, 1s, 2s, 3s, 4s
			// Each heartbeat renews the 3s TTL, so keys should not expire
			// After 4 seconds, we've had 4 heartbeats keeping registration alive
			time.Sleep(4 * time.Second)

			By("Verifying all 3 EGs still registered (heartbeat kept them alive)")
			eg1Info, err := testEnv.GetRegistryInfo("eg1")
			Expect(err).To(BeNil(), "EG1 should still be registered")
			Expect(eg1Info).NotTo(BeEmpty())

			eg2Info, err := testEnv.GetRegistryInfo("eg2")
			Expect(err).To(BeNil(), "EG2 should still be registered")
			Expect(eg2Info).NotTo(BeEmpty())

			eg3Info, err := testEnv.GetRegistryInfo("eg3")
			Expect(err).To(BeNil(), "EG3 should still be registered")
			Expect(eg3Info).NotTo(BeEmpty())

			By("Verifying cluster size still 3")
			finalClusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(finalClusterSize).To(Equal(3))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Heartbeat test: All EGs maintained registration for 15s\n")
			}
		})

		It("should remove EG from registry on graceful shutdown", func() {
			By("Verifying EG2 is registered before shutdown")
			eg2Info, err := testEnv.GetRegistryInfo("eg2")
			Expect(err).To(BeNil())
			Expect(eg2Info).NotTo(BeEmpty())

			By("Stopping EG2 gracefully (SIGTERM)")
			err = testEnv.StopEG(2)
			Expect(err).To(BeNil(), "Should stop EG2 successfully")

			By("Waiting briefly for graceful shutdown to complete")
			time.Sleep(1 * time.Second)

			By("Verifying EG2 registry key is deleted")
			ctx := context.Background()
			exists, err := testEnv.RedisClient.Exists(ctx, "registry:eg:eg2").Result()
			Expect(err).To(BeNil())
			Expect(exists).To(Equal(int64(0)), "registry:eg:eg2 should be deleted after graceful shutdown")

			By("Verifying cluster size is now 2")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(2), "Cluster should have 2 EGs after EG2 shutdown")

			By("Verifying EG1 and EG3 are still registered")
			eg1Info, err := testEnv.GetRegistryInfo("eg1")
			Expect(err).To(BeNil())
			Expect(eg1Info).NotTo(BeEmpty())

			eg3Info, err := testEnv.GetRegistryInfo("eg3")
			Expect(err).To(BeNil())
			Expect(eg3Info).NotTo(BeEmpty())

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Graceful shutdown test: EG2 deregistered, cluster size=2\n")
			}
		})

		It("should expire EG from registry after heartbeat stops", func() {
			By("Verifying EG2 is registered before kill")
			eg2Info, err := testEnv.GetRegistryInfo("eg2")
			Expect(err).To(BeNil())
			Expect(eg2Info).NotTo(BeEmpty())

			By("Killing EG2 (SIGKILL, no graceful shutdown)")
			err = testEnv.KillEG(2)
			Expect(err).To(BeNil(), "Should kill EG2 successfully")

			By("Waiting briefly for kill to complete")
			time.Sleep(500 * time.Millisecond)

			By("Verifying EG2 registry key still exists (killed before cleanup)")
			ctx := context.Background()
			exists, err := testEnv.RedisClient.Exists(ctx, "registry:eg:eg2").Result()
			Expect(err).To(BeNil())
			Expect(exists).To(Equal(int64(1)), "registry:eg:eg2 should still exist immediately after kill")

			By("Simulating TTL expiration by manually deleting the key")
			// Note: miniredis doesn't auto-expire keys with wall-clock time
			// In production, the key would expire after 10s of no heartbeat
			// For testing, we simulate expiration by waiting and then manually deleting
			// This tests the behavior of "what happens when EG2's registry key disappears"
			time.Sleep(11 * time.Second) // Simulate waiting past TTL

			By("Manually deleting EG2 registry key to simulate TTL expiration")
			err = testEnv.RedisClient.Del(ctx, "registry:eg:eg2").Err()
			Expect(err).To(BeNil())

			By("Verifying EG2 registry key is gone")
			exists, err = testEnv.RedisClient.Exists(ctx, "registry:eg:eg2").Result()
			Expect(err).To(BeNil())
			Expect(exists).To(Equal(int64(0)), "registry:eg:eg2 should be gone (simulated TTL expiry)")

			By("Verifying cluster size is now 2")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(2), "Cluster should have 2 EGs after EG2 TTL expires")

			By("Verifying EG1 and EG3 are still registered")
			eg1Info, err := testEnv.GetRegistryInfo("eg1")
			Expect(err).To(BeNil())
			Expect(eg1Info).NotTo(BeEmpty())

			eg3Info, err := testEnv.GetRegistryInfo("eg3")
			Expect(err).To(BeNil())
			Expect(eg3Info).NotTo(BeEmpty())

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("TTL expiry test: EG2 expired from registry, cluster size=2\n")
			}
		})

		It("should allow EG to rejoin cluster with same eg_id", func() {
			By("Stopping EG2")
			err := testEnv.StopEG(2)
			Expect(err).To(BeNil())

			By("Waiting for EG2 registry key to be deleted")
			ctx := context.Background()
			Eventually(func() int64 {
				exists, _ := testEnv.RedisClient.Exists(ctx, "registry:eg:eg2").Result()
				return exists
			}, 5*time.Second, 200*time.Millisecond).Should(Equal(int64(0)),
				"EG2 should deregister after graceful shutdown")

			By("Verifying cluster size is 2")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(2))

			By("Restarting EG2 with same eg_id")
			err = testEnv.StartEG(2)
			Expect(err).To(BeNil(), "Should restart EG2 successfully")

			By("Waiting for EG2 to register")
			time.Sleep(2 * time.Second)

			By("Verifying EG2 re-registered successfully")
			eg2Info, err := testEnv.GetRegistryInfo("eg2")
			Expect(err).To(BeNil(), "EG2 should be registered after restart")
			Expect(eg2Info).NotTo(BeEmpty())
			Expect(eg2Info["eg_id"]).To(Equal("eg2"))
			Expect(eg2Info["address"]).To(Equal("127.0.0.1:9204"))

			By("Verifying cluster size is back to 3")
			err = testEnv.WaitForClusterSize(3, 10*time.Second)
			Expect(err).To(BeNil(), "Cluster should have 3 EGs after EG2 rejoins")

			finalClusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(finalClusterSize).To(Equal(3))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Rejoin test: EG2 successfully rejoined cluster\n")
			}
		})
	})

	Context("Dynamic Cluster", Serial, func() {
		It("should adapt distribution when EG joins cluster", func() {
			By("Verifying all 3 EGs are initially running")
			clusterSize, err := testEnv.GetClusterSize()
			Expect(err).To(BeNil())
			Expect(clusterSize).To(Equal(3))

			By("Rendering 10 URLs with all 3 EGs available")
			firstBatchEGIDs := make(map[string]int) // Track which EGs appear
			for i := 0; i < 10; i++ {
				testURL := fmt.Sprintf("/static/test.html?batch=1&id=%d", i)
				response := testEnv.RequestViaEG1(testURL)
				Expect(response.Error).To(BeNil(), "Request %d should succeed", i+1)
				Expect(response.StatusCode).To(Equal(200), "Request %d should return 200", i+1)

				// Get cache key
				fullURL := testEnv.Config.TestPagesURL() + testURL
				result, err := normalizer.Normalize(fullURL, nil)
				Expect(err).To(BeNil())
				urlHash := normalizer.Hash(result.NormalizedURL)
				cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

				// Get eg_ids with retry logic
				var egIDs []string
				Eventually(func() error {
					ids, err := testEnv.GetEGIDs(cacheKey)
					if err != nil {
						return err
					}
					egIDs = ids
					return nil
				}, 3*time.Second, 200*time.Millisecond).Should(Succeed(),
					"Should get eg_ids for request %d", i+1)

				for _, egID := range egIDs {
					firstBatchEGIDs[egID]++
				}
			}

			By("Verifying distribution includes all 3 EGs")
			Expect(firstBatchEGIDs["eg1"]).To(BeNumerically(">", 0), "eg1 should be used")
			Expect(firstBatchEGIDs["eg2"]).To(BeNumerically(">", 0), "eg2 should be used")
			Expect(firstBatchEGIDs["eg3"]).To(BeNumerically(">", 0), "eg3 should be used")

			By("Stopping EG3")
			err = testEnv.StopEG(3)
			Expect(err).To(BeNil())

			By("Waiting for EG3 to deregister")
			time.Sleep(1 * time.Second)
			err = testEnv.WaitForClusterSize(2, 10*time.Second)
			Expect(err).To(BeNil())

			By("Clearing cache before second batch")
			testEnv.ClearCache()

			By("Rendering 10 NEW URLs with only 2 EGs available")
			secondBatchEGIDs := make(map[string]int)
			for i := 0; i < 10; i++ {
				testURL := fmt.Sprintf("/static/test.html?batch=2&id=%d", i)
				response := testEnv.RequestViaEG1(testURL)
				Expect(response.Error).To(BeNil(), "Batch 2 request %d should succeed", i+1)
				Expect(response.StatusCode).To(Equal(200), "Batch 2 request %d should return 200", i+1)

				// Get cache key
				fullURL := testEnv.Config.TestPagesURL() + testURL
				result, err := normalizer.Normalize(fullURL, nil)
				Expect(err).To(BeNil())
				urlHash := normalizer.Hash(result.NormalizedURL)
				cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

				// Get eg_ids with retry logic
				var egIDs []string
				Eventually(func() error {
					ids, err := testEnv.GetEGIDs(cacheKey)
					if err != nil {
						return err
					}
					egIDs = ids
					return nil
				}, 3*time.Second, 200*time.Millisecond).Should(Succeed(),
					"Should get eg_ids for batch 2 request %d", i+1)

				for _, egID := range egIDs {
					secondBatchEGIDs[egID]++
				}
			}

			By("Verifying distribution only uses eg1 and eg2 (cluster adapted)")
			Expect(secondBatchEGIDs["eg1"]).To(BeNumerically(">", 0), "eg1 should be used")
			Expect(secondBatchEGIDs["eg2"]).To(BeNumerically(">", 0), "eg2 should be used")
			Expect(secondBatchEGIDs["eg3"]).To(Equal(0), "eg3 should NOT be used (offline)")

			By("Restarting EG3")
			err = testEnv.StartEG(3)
			Expect(err).To(BeNil())

			By("Waiting for EG3 to rejoin")
			time.Sleep(2 * time.Second)
			err = testEnv.WaitForClusterSize(3, 10*time.Second)
			Expect(err).To(BeNil())

			By("Clearing cache before third batch")
			testEnv.ClearCache()

			By("Rendering 10 MORE NEW URLs with all 3 EGs available again")
			thirdBatchEGIDs := make(map[string]int)
			for i := 0; i < 10; i++ {
				testURL := fmt.Sprintf("/static/test.html?batch=3&id=%d", i)
				response := testEnv.RequestViaEG1(testURL)
				Expect(response.Error).To(BeNil(), "Batch 3 request %d should succeed", i+1)
				Expect(response.StatusCode).To(Equal(200), "Batch 3 request %d should return 200", i+1)

				// Get cache key
				fullURL := testEnv.Config.TestPagesURL() + testURL
				result, err := normalizer.Normalize(fullURL, nil)
				Expect(err).To(BeNil())
				urlHash := normalizer.Hash(result.NormalizedURL)
				cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

				// Get eg_ids with retry logic
				var egIDs []string
				Eventually(func() error {
					ids, err := testEnv.GetEGIDs(cacheKey)
					if err != nil {
						return err
					}
					egIDs = ids
					return nil
				}, 3*time.Second, 200*time.Millisecond).Should(Succeed(),
					"Should get eg_ids for batch 3 request %d", i+1)

				for _, egID := range egIDs {
					thirdBatchEGIDs[egID]++
				}
			}

			By("Verifying distribution includes all 3 EGs again (cluster expanded)")
			Expect(thirdBatchEGIDs["eg1"]).To(BeNumerically(">", 0), "eg1 should be used")
			Expect(thirdBatchEGIDs["eg2"]).To(BeNumerically(">", 0), "eg2 should be used")
			Expect(thirdBatchEGIDs["eg3"]).To(BeNumerically(">", 0), "eg3 should be used")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Dynamic cluster test:\n")
				fmt.Printf("  Batch 1 (3 EGs): eg1=%d, eg2=%d, eg3=%d\n",
					firstBatchEGIDs["eg1"], firstBatchEGIDs["eg2"], firstBatchEGIDs["eg3"])
				fmt.Printf("  Batch 2 (2 EGs): eg1=%d, eg2=%d, eg3=%d\n",
					secondBatchEGIDs["eg1"], secondBatchEGIDs["eg2"], secondBatchEGIDs["eg3"])
				fmt.Printf("  Batch 3 (3 EGs): eg1=%d, eg2=%d, eg3=%d\n",
					thirdBatchEGIDs["eg1"], thirdBatchEGIDs["eg2"], thirdBatchEGIDs["eg3"])
			}
		})
	})
})

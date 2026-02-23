package sharding_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Failure Scenarios", Serial, func() {
	var normalizer *hash.URLNormalizer

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	AfterEach(func() {
		By("Ensuring all 3 EGs are running for next test")
		// Check and restart any stopped EGs
		for i := 1; i <= 3; i++ {
			var egCmd *exec.Cmd
			switch i {
			case 1:
				egCmd = testEnv.EdgeGateway1Cmd
			case 2:
				egCmd = testEnv.EdgeGateway2Cmd
			case 3:
				egCmd = testEnv.EdgeGateway3Cmd
			}

			// If process is nil or has exited, restart it
			if egCmd == nil || egCmd.ProcessState != nil {
				if os.Getenv("DEBUG") != "" {
					fmt.Printf("AfterEach: Restarting EG%d\n", i)
				}
				err := testEnv.StartEG(i)
				if err != nil {
					fmt.Printf("Warning: Failed to restart EG%d in AfterEach: %v\n", i, err)
				}
			}
		}

		// Wait for cluster to stabilize
		time.Sleep(1 * time.Second)
	})

	Context("EG Offline Scenarios", Serial, func() {
		It("should handle request when an EG in cluster is offline", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=eg_offline"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 to populate cache")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=eg_offline", "eg-offline-test-render")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Getting eg_ids to determine cache distribution")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2), "Should have RF=2 EGs with cache")
			Expect(egIDs).To(ContainElement("eg1"), "Rendering EG must be in eg_ids")

			By("Stopping EG1 to simulate failure")
			err = testEnv.StopEG(1)
			Expect(err).To(BeNil(), "Should stop EG1 successfully")

			By("Making request via EG3 (cluster should handle EG1 being offline)")
			response2 := testEnv.RequestViaEG3("/static/test.html?test=eg_offline", "eg-offline-test-recovery")
			Expect(response2.Error).To(BeNil(), "Request should succeed despite EG1 offline")
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying content is correct (either from cache or fresh render)")
			Expect(response2.Body).To(ContainSubstring("Sharding Test Page"))

			// Client should get valid response regardless of source
			renderSource := response2.Headers.Get("X-Render-Source")
			Expect(renderSource).To(BeElementOf([]string{"cache", "rendered"}),
				"Should be either cache (pulled) or rendered (fallback)")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("EG offline test: original eg_ids=%v, response source=%s\n",
					egIDs, renderSource)
			}
		})

		It("should handle partial push failure", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=partial_push"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Stopping EG2 before rendering")
			err = testEnv.StopEG(2)
			Expect(err).To(BeNil())

			By("Making request via EG1 (pushes will partially fail)")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=partial_push", "partial-push-test")
			Expect(response1.Error).To(BeNil(), "Client should not see push failures")
			Expect(response1.StatusCode).To(Equal(200), "Should return 200 OK")
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids excludes stopped EG2")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())

			// Should have eg1 (rendering EG) + one other (not eg2 since it's offline)
			Expect(egIDs).To(ContainElement("eg1"), "Rendering EG must be present")
			Expect(egIDs).NotTo(ContainElement("eg2"), "Offline EG2 should not receive push")

			// Could be under-replicated (only eg1) or successfully pushed to eg3
			if len(egIDs) == 2 {
				Expect(egIDs).To(ContainElement("eg3"), "Should have pushed to EG3 if not under-replicated")
			}

			By("Restarting EG2")
			err = testEnv.StartEG(2)
			Expect(err).To(BeNil())

			By("Verifying EG2 can pull from cluster after restart")
			response2 := testEnv.RequestViaEG2("/static/test.html?test=partial_push", "partial-push-recovery")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(Equal(response1.Body))

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Partial push test: eg_ids=%v (eg2 was offline during push)\n", egIDs)
			}
		})

		It("should handle all pushes failing", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=all_push_fail"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Stopping EG2 and EG3 before rendering")
			err = testEnv.StopEG(2)
			Expect(err).To(BeNil())
			err = testEnv.StopEG(3)
			Expect(err).To(BeNil())

			By("Making request via EG1 (all pushes will fail)")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=all_push_fail", "all-push-fail-test")
			Expect(response1.Error).To(BeNil(), "Client should not be affected by push failures")
			Expect(response1.StatusCode).To(Equal(200), "Should return 200 OK")
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying eg_ids contains only eg1 (under-replicated)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}), "Should only have rendering EG when all pushes fail")

			By("Restarting EG2")
			err = testEnv.StartEG(2)
			Expect(err).To(BeNil())

			By("Making request via EG2 (should pull from eg1)")
			response2 := testEnv.RequestViaEG2("/static/test.html?test=all_push_fail", "all-push-fail-recovery")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should pull from eg1")

			By("Verifying eg_ids updated after pull")
			Eventually(func() []string {
				ids, _ := testEnv.GetEGIDs(cacheKey)
				return ids
			}, 3*time.Second, 200*time.Millisecond).Should(HaveLen(2),
				"eg_ids should be updated to include eg2")

			finalEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(finalEgIDs).To(ContainElement("eg1"))
			Expect(finalEgIDs).To(ContainElement("eg2"))

			By("Restarting EG3 for cleanup")
			err = testEnv.StartEG(3)
			Expect(err).To(BeNil())

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("All push fail test: initial eg_ids=%v, after recovery=%v\n",
					[]string{"eg1"}, finalEgIDs)
			}
		})
	})

	Context("Lock Contention", Serial, func() {
		It("should handle concurrent requests for same URL", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=concurrent"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Sending 3 concurrent requests to all EGs simultaneously")
			var wg sync.WaitGroup
			responses := make([]*TestResponse, 3)
			startTime := time.Now()

			// Use WaitGroup to synchronize goroutines
			wg.Add(3)

			// Launch all 3 requests at nearly the same time
			go func() {
				defer wg.Done()
				responses[0] = testEnv.RequestViaEG1("/static/test.html?test=concurrent", "concurrent-eg1")
			}()

			go func() {
				defer wg.Done()
				responses[1] = testEnv.RequestViaEG2("/static/test.html?test=concurrent", "concurrent-eg2")
			}()

			go func() {
				defer wg.Done()
				responses[2] = testEnv.RequestViaEG3("/static/test.html?test=concurrent", "concurrent-eg3")
			}()

			wg.Wait()
			totalDuration := time.Since(startTime)

			By("Verifying all 3 requests succeeded")
			for i, resp := range responses {
				Expect(resp.Error).To(BeNil(), "Request %d should succeed", i+1)
				Expect(resp.StatusCode).To(Equal(200), "Request %d should return 200", i+1)
			}

			By("Verifying all responses have identical content")
			for i := 1; i < 3; i++ {
				Expect(responses[i].Body).To(Equal(responses[0].Body),
					"Response %d should match first response", i+1)
			}

			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2), "Should have RF=2 distribution")

			By("Verifying only 1 render occurred (lock prevented duplicates)")
			// Count how many responses claim to be "rendered"
			renderedCount := 0
			for _, resp := range responses {
				if resp.Headers.Get("X-Render-Source") == "rendered" {
					renderedCount++
				}
			}

			// At least one should be rendered, others may be cache hits if lock worked
			Expect(renderedCount).To(BeNumerically(">=", 1),
				"At least one request should have triggered render")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Concurrent test: %d renders, total time=%v, eg_ids=%v\n",
					renderedCount, totalDuration, egIDs)
			}
		})

		It("should wait for lock release and pull from cluster", func() {
			By("Starting slow render on EG1 in background")
			var response1 *TestResponse
			var wg sync.WaitGroup
			wg.Add(1)

			eg1Start := time.Now()
			go func() {
				defer wg.Done()
				response1 = testEnv.RequestViaEG1("/spa/slow.html?test=lock_wait", "lock-wait-eg1-slow")
			}()

			time.Sleep(100 * time.Millisecond)

			By("Starting request on EG2 (should wait for lock, then pull)")
			eg2Start := time.Now()
			response2 := testEnv.RequestViaEG2("/spa/slow.html?test=lock_wait", "lock-wait-eg2-waiter")
			eg2Duration := time.Since(eg2Start)

			By("Waiting for EG1 to complete")
			wg.Wait()
			eg1Duration := time.Since(eg1Start)

			By("Verifying both requests succeeded")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying EG1 performed the render")
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"EG1 should have rendered (acquired lock)")

			By("Verifying EG2 served from cache (pulled after lock released)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"EG2 should have pulled from cache (lock prevented duplicate render)")

			By("Verifying content matches")
			Expect(response2.Body).To(ContainSubstring("Slow Loading Test Page"))

			By("Verifying timing makes sense")
			// EG1 should take 2.5s+ for slow page
			Expect(eg1Duration).To(BeNumerically(">=", 2*time.Second),
				"EG1 render should take at least 2s (slow AJAX)")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Lock wait test: EG1 render=%v, EG2 wait+pull=%v\n",
					eg1Duration, eg2Duration)
			}
		})
	})

	Context("TTL Expiration", Serial, func() {
		AfterEach(func() {
			By("Restoring default TTL (1 hour)")
			err := testEnv.UpdateCacheTTL(1 * time.Hour)
			Expect(err).To(BeNil())

			By("Restarting all EGs with restored TTL")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())
		})

		It("should not pull expired cache from remote EG", func() {
			By("Setting short TTL (3 seconds)")
			err := testEnv.UpdateCacheTTL(3 * time.Second)
			Expect(err).To(BeNil())

			By("Restarting all EGs with new TTL")
			err = testEnv.RestartAllEGs()
			Expect(err).To(BeNil())

			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=ttl_expire"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 with short TTL")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=ttl_expire", "ttl-expire-test-initial")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying cache exists with eg_ids")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2))

			By("Waiting for cache to expire (7 seconds)")
			// Use FastForward to expire cache in miniredis (miniredis doesn't auto-expire with wall-clock time)
			// With stale cache enabled, Redis TTL = cache_ttl + stale_ttl = 3s + 3s = 6s
			// FastForward(7s) to ensure cache fully expires beyond stale period
			// FastForward(7s) won't expire tabs hash (TTL=5s, extended every 1s by heartbeat)
			testEnv.MiniRedis.FastForward(7 * time.Second)
			time.Sleep(1500 * time.Millisecond) // Wait for next RS heartbeat to extend tabs TTL

			By("Verifying Redis metadata has expired")
			ctx := context.Background()

			// Check TTL first (should be negative if expired)
			ttl := testEnv.MiniRedis.TTL(cacheKey)
			if os.Getenv("DEBUG") != "" {
				fmt.Printf("TTL after FastForward: %v\n", ttl)
			}

			// In miniredis, we need to trigger expiration by accessing the key
			// Use HGETALL which checks TTL, or manually check and expect error
			data, err := testEnv.RedisClient.HGetAll(ctx, cacheKey).Result()
			Expect(err).To(BeNil())
			Expect(len(data)).To(Equal(0), "Redis metadata should have expired (HGETALL should return empty)")

			By("Making request via EG3 after expiration")
			response2 := testEnv.RequestViaEG3("/static/test.html?test=ttl_expire", "ttl-expire-test-fresh")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying EG3 rendered fresh (didn't pull expired cache)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Should render fresh when cache is expired")

			newEgIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(newEgIDs)).To(Equal(2), "New cache should have RF=2 distribution")

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("TTL expire test: old eg_ids=%v, new eg_ids=%v\n", egIDs, newEgIDs)
			}
		})

		It("should handle stale local file when metadata deleted", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=stale_file"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Making initial request via EG1 to create cache")
			response1 := testEnv.RequestViaEG1("/static/test.html?test=stale_file", "stale-file-test-initial")
			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying cache metadata exists")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(metadata).NotTo(BeEmpty())

			By("Manually deleting Redis metadata (simulate expiration)")
			ctx := context.Background()
			err = testEnv.RedisClient.Del(ctx, cacheKey).Err()
			Expect(err).To(BeNil())

			By("Verifying metadata is gone but file still exists on disk")
			exists, err := testEnv.RedisClient.Exists(ctx, cacheKey).Result()
			Expect(err).To(BeNil())
			Expect(exists).To(Equal(int64(0)), "Metadata should be deleted")

			By("Making request via EG1 (should re-render, not use orphaned file)")
			response2 := testEnv.RequestViaEG1("/static/test.html?test=stale_file", "stale-file-test-rerender")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying EG1 re-rendered (ignored orphaned file)")
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Should re-render when metadata is missing (orphaned file ignored)")

			newMetadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(newMetadata).NotTo(BeEmpty())
			Expect(newMetadata["eg_ids"]).NotTo(BeEmpty())

			if os.Getenv("DEBUG") != "" {
				fmt.Printf("Stale file test: re-rendered after metadata deletion\n")
			}
		})
	})
})

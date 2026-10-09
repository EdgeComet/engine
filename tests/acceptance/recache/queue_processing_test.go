package recache_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduler Processing", func() {
	BeforeEach(func() {
		// Restart daemon for clean internal queue. Redis is cleared while the
		// daemon is down so the shutdown flush cannot hand the previous spec's
		// in-flight entries to the fresh daemon.
		err := testEnv.RestartDaemonWithCleanRedis()
		Expect(err).ToNot(HaveOccurred())

		testEnv.DrainMockEGReceivedChannel()
		testEnv.DrainMockEGResponses()

		// Add mock RS with capacity for processing
		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		// Re-register mock EG (cleared by ClearRedis)
		// Note: daemon prepends "http://" so store address without protocol
		err = testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))
		Expect(err).ToNot(HaveOccurred())

		// Wait for registry to be propagated and queryable by daemon
		err = testEnv.WaitForRegistryReady(2 * time.Second)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("High Priority Queue Processing", func() {
		It("should process high priority queue every tick (100ms)", func() {
			// Add entry to high priority ZSET
			score := float64(time.Now().Unix())
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", "https://example.com/page1", 1, score)
			Expect(err).ToNot(HaveOccurred())

			// Verify entry exists in ZSET
			zsetKey := "recache:1:high"
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(1)))

			// Wait for ZSET to be empty (entry pulled and processed)
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(zsetKey)
				return size
			}, 2*time.Second, 100*time.Millisecond).Should(Equal(int64(0)), "Entry should be pulled from high priority ZSET")

			// Verify mock EG received the request
			receivedCount, requests := testEnv.DrainChannelUntilCount(1, 2*time.Second)
			Expect(receivedCount).To(Equal(1), "Mock EG should receive 1 recache request")
			Expect(requests[0].URL).To(Equal("https://example.com/page1"))
			Expect(requests[0].DimensionID).To(Equal(1))
		})
	})

	Context("Normal/Autorecache Queue Processing", func() {
		It("drains normal and autorecache at the same per-tick cadence as high", func() {
			// Under the unified drain (no normal_check_interval gate), normal
			// and due autorecache flow at the same rate as high whenever
			// concurrency and RS budget have headroom.
			score := float64(time.Now().Unix())
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal", "https://example.com/normal", 1, score)
			Expect(err).ToNot(HaveOccurred())

			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache", "https://example.com/autorecache", 1, score)
			Expect(err).ToNot(HaveOccurred())

			normalKey := "recache:1:normal"
			autorecacheKey := "recache:1:autorecache"

			size, _ := testEnv.GetZSETSize(normalKey)
			Expect(size).To(Equal(int64(1)))
			size, _ = testEnv.GetZSETSize(autorecacheKey)
			Expect(size).To(Equal(int64(1)))

			// Both queues should drain within a small number of ticks, not 60.
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(normalKey)
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)), "Normal queue should drain at per-tick cadence (no 60s gate)")

			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(autorecacheKey)
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)), "Autorecache queue should drain at per-tick cadence")
		})

		It("respects due semantics under batched ZPopMin (only popped scheduled<=now)", func() {
			// Regression guard for the autorecache-specific dueCount clamp.
			// With max_concurrent=5 and batched ZPopMin, the limiter would
			// otherwise pull 5 lowest-score entries even if only 3 are due,
			// dispatching 2 future-scheduled URLs ahead of their scheduled time.
			//
			// Use dimension_id=0 (bypass) so the dispatch path doesn't depend on
			// RS budget — the mock RS heartbeat would otherwise expire before the
			// autorecache check fires at ~6s.
			autorecacheKey := fmt.Sprintf("recache:%d:autorecache", testEnv.TestHostID)
			now := time.Now().Unix()

			for i := 0; i < 3; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache",
					fmt.Sprintf("https://example.com/due-%d", i), 0, float64(now-10))
				Expect(err).ToNot(HaveOccurred())
			}
			for i := 0; i < 7; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache",
					fmt.Sprintf("https://example.com/future-%d", i), 0, float64(now+300))
				Expect(err).ToNot(HaveOccurred())
			}

			size, _ := testEnv.GetZSETSize(autorecacheKey)
			Expect(size).To(Equal(int64(10)))

			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(autorecacheKey)
				return size
			}, 10*time.Second, 200*time.Millisecond).Should(Equal(int64(7)),
				"only the 3 due entries should be popped; 7 future-scheduled entries must remain")

			received, _ := testEnv.DrainChannelUntilCount(3, 2*time.Second)
			Expect(received).To(Equal(3),
				"mock EG receives exactly 3 dispatches (the due entries)")

			Consistently(func() int {
				select {
				case <-testEnv.MockEGReceivedCh:
					return 1
				default:
					return 0
				}
			}, 1500*time.Millisecond, 200*time.Millisecond).Should(Equal(0),
				"no future-scheduled entries must be dispatched ahead of time")

			entries, err := getZSETEntries(testEnv.RedisClient, autorecacheKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(7))
			for _, e := range entries {
				Expect(e.Score).To(BeNumerically(">", float64(now)),
					"every remaining entry must be future-scheduled")
			}
		})
	})

	Context("Priority Hierarchy", func() {
		It("dispatches high before normal before autorecache within a host (strict priority order)", func() {
			// Pause scheduler for deterministic setup
			err := testEnv.PauseScheduler()
			Expect(err).ToNot(HaveOccurred())

			score := float64(time.Now().Unix())

			for i := 1; i <= 2; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high",
					fmt.Sprintf("https://example.com/high%d", i), 1, score)
				Expect(err).ToNot(HaveOccurred())
			}
			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal",
				"https://example.com/normal1", 1, score)
			Expect(err).ToNot(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache",
				"https://example.com/autorecache1", 1, score)
			Expect(err).ToNot(HaveOccurred())

			highSize, _ := testEnv.GetZSETSize("recache:1:high")
			normalSize, _ := testEnv.GetZSETSize("recache:1:normal")
			autorecacheSize, _ := testEnv.GetZSETSize("recache:1:autorecache")
			Expect(highSize).To(Equal(int64(2)))
			Expect(normalSize).To(Equal(int64(1)))
			Expect(autorecacheSize).To(Equal(int64(1)))

			err = testEnv.ResumeScheduler()
			Expect(err).ToNot(HaveOccurred())

			// All three priorities drain within a few ticks under the unified
			// drain. Strict priority order within a host means high entries
			// pop before normal, which pops before autorecache. Sibling
			// dispatches within the same batch are concurrent (goroutine
			// scheduling), so we only assert the cross-priority order: the
			// first two received are both high, the third is normal, the
			// fourth is autorecache.
			received, requests := testEnv.DrainChannelUntilCount(4, 5*time.Second)
			Expect(received).To(Equal(4), "all 4 entries should dispatch to mock EG")

			firstTwo := []string{requests[0].URL, requests[1].URL}
			Expect(firstTwo).To(ConsistOf(
				"https://example.com/high1",
				"https://example.com/high2",
			), "first two dispatches must be the high-priority pair")
			Expect(requests[2].URL).To(Equal("https://example.com/normal1"),
				"normal must dispatch after both high entries")
			Expect(requests[3].URL).To(Equal("https://example.com/autorecache1"),
				"autorecache must dispatch last")

			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:high")
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)))
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:normal")
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)))
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:autorecache")
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)))
		})
	})

	Context("Priority under render-service starvation", func() {
		It("parks at most max_concurrent entries and dispatches a late high entry first", func() {
			const (
				maxConcurrent = 5
				normalCount   = 20
				highURL       = "https://example.com/starved-high"
			)

			// Remove the mock RS so the render budget is zero. Dimension 1 renders, so
			// pulled entries park in the internal queue instead of dispatching.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			Expect(testEnv.RedisClient.Del(ctx, "service:render:rs-1").Err()).To(Succeed())
			Expect(testEnv.RedisClient.HDel(ctx, "services:render:list", "rs-1").Err()).To(Succeed())
			Eventually(func() int {
				return testEnv.GetRSCapacityStatus().TotalFreeTabs
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(0))

			score := float64(time.Now().Unix())
			for i := 0; i < normalCount; i++ {
				Expect(addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal",
					fmt.Sprintf("https://example.com/starved-normal-%d", i), 1, score)).To(Succeed())
			}

			// Assert on the Redis backlog rather than the internal queue size: every gate
			// pass empties the internal queue for a Redis round trip before re-enqueueing
			// what it defers, so a size sample can briefly read low.
			Eventually(testEnv.GetInternalQueueSize, 3*time.Second, 50*time.Millisecond).
				Should(Equal(maxConcurrent))
			Consistently(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:normal")
				return size
			}, time.Second, 100*time.Millisecond).Should(Equal(int64(normalCount-maxConcurrent)),
				"a starved host must not keep pulling its backlog into the internal queue")

			Expect(addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", highURL, 1, score)).To(Succeed())
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:high")
				return size
			}, 3*time.Second, 50*time.Millisecond).Should(Equal(int64(0)),
				"waiting normal entries must not keep the high entry in Redis")
			Eventually(testEnv.GetInternalQueueSize, 3*time.Second, 50*time.Millisecond).
				Should(Equal(maxConcurrent + 1))

			// One free tab: the first entry through the render gate must be the high one,
			// although every waiting normal entry was pulled before it.
			Expect(testEnv.AddMockRSToRegistry("rs-1", 1, 0)).To(Succeed())
			received, requests := testEnv.DrainChannelUntilCount(1, 5*time.Second)
			Expect(received).To(Equal(1))
			Expect(requests[0].URL).To(Equal(highURL))

			// Open the budget so the normal backlog drains and nothing stays parked.
			Expect(testEnv.AddMockRSToRegistry("rs-1", 100, 0)).To(Succeed())
			received, _ = testEnv.DrainChannelUntilCount(normalCount, 10*time.Second)
			Expect(received).To(Equal(normalCount))
			Eventually(testEnv.GetInternalQueueSize, 3*time.Second, 50*time.Millisecond).Should(Equal(0))
		})
	})
})

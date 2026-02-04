package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scheduler Processing", func() {
	BeforeEach(func() {
		// Restart daemon for clean internal queue
		err := testEnv.RestartDaemon()
		Expect(err).ToNot(HaveOccurred())

		testEnv.ClearRedis()
		testEnv.DrainMockEGReceivedChannel()

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
		It("should process normal and autorecache queues every 60 ticks", func() {
			// Add entries to normal and autorecache ZSETs
			score := float64(time.Now().Unix())
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal", "https://example.com/normal", 1, score)
			Expect(err).ToNot(HaveOccurred())

			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache", "https://example.com/autorecache", 1, score)
			Expect(err).ToNot(HaveOccurred())

			// Verify entries exist
			normalKey := "recache:1:normal"
			autorecacheKey := "recache:1:autorecache"

			size, _ := testEnv.GetZSETSize(normalKey)
			Expect(size).To(Equal(int64(1)))
			size, _ = testEnv.GetZSETSize(autorecacheKey)
			Expect(size).To(Equal(int64(1)))

			// Wait for entries to be pulled from ZSETs (processed after 60 ticks = 6 seconds)
			// Use Eventually to poll for empty state with 10 second timeout
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(normalKey)
				return size
			}, 10*time.Second, 200*time.Millisecond).Should(Equal(int64(0)), "Normal queue should be processed after 60 ticks")

			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(autorecacheKey)
				return size
			}, 10*time.Second, 200*time.Millisecond).Should(Equal(int64(0)), "Autorecache queue should be processed after 60 ticks")
		})
	})

	Context("Priority Hierarchy", func() {
		It("should process queues in strict priority order: high -> normal -> autorecache (Scenario 3)", func() {
			// Pause scheduler for deterministic setup
			err := testEnv.PauseScheduler()
			Expect(err).ToNot(HaveOccurred())

			// Add entries to all three priority queues
			score := float64(time.Now().Unix())

			// Add 2 high priority entries (processed every tick)
			for i := 1; i <= 2; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high",
					fmt.Sprintf("https://example.com/high%d", i), 1, score)
				Expect(err).ToNot(HaveOccurred())
			}

			// Add 1 normal priority entry (processed every 60 ticks)
			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal",
				"https://example.com/normal1", 1, score)
			Expect(err).ToNot(HaveOccurred())

			// Add 1 autorecache entry (processed every 60 ticks)
			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache",
				"https://example.com/autorecache1", 1, score)
			Expect(err).ToNot(HaveOccurred())

			// Verify initial state
			highSize, _ := testEnv.GetZSETSize("recache:1:high")
			normalSize, _ := testEnv.GetZSETSize("recache:1:normal")
			autorecacheSize, _ := testEnv.GetZSETSize("recache:1:autorecache")

			Expect(highSize).To(Equal(int64(2)))
			Expect(normalSize).To(Equal(int64(1)))
			Expect(autorecacheSize).To(Equal(int64(1)))

			// Resume scheduler to start processing
			err = testEnv.ResumeScheduler()
			Expect(err).ToNot(HaveOccurred())

			// Wait for high priority to be drained (2 entries, processed every tick)
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:high")
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)), "High priority should be drained first")

			// Normal and autorecache should still be untouched (need 60 ticks)
			normalSize, _ = testEnv.GetZSETSize("recache:1:normal")
			autorecacheSize, _ = testEnv.GetZSETSize("recache:1:autorecache")
			Expect(normalSize).To(Equal(int64(1)), "Normal queue should not be processed yet")
			Expect(autorecacheSize).To(Equal(int64(1)), "Autorecache queue should not be processed yet")

			// After 60 ticks (6 seconds), scheduler pulls entries from normal and autorecache
			// Use Eventually to poll for empty state with 10 second timeout
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:normal")
				return size
			}, 10*time.Second, 200*time.Millisecond).Should(Equal(int64(0)), "Normal queue should be processed after 60 ticks")

			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize("recache:1:autorecache")
				return size
			}, 10*time.Second, 200*time.Millisecond).Should(Equal(int64(0)), "Autorecache queue should be processed after 60 ticks")
		})
	})
})

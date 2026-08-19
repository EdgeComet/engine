package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Queue Purge API", func() {
	// dimension 1 is a render dimension, so its entries need render-service budget
	// to dispatch. The permanence spec relies on that.
	const renderDimensionID = 1

	// Every spec seeds a queue and then counts what survived, so the scheduler must
	// not drain underneath the assertions.
	BeforeEach(pauseSchedulerForSpec)

	seedLabelled := func(priority, label string, count int) {
		score := float64(time.Now().Unix())
		for i := 0; i < count; i++ {
			url := fmt.Sprintf("https://example.com/%s-%s-%d", label, priority, i)
			Expect(addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, priority, url, renderDimensionID, score)).To(Succeed())
		}
	}

	seedQueue := func(priority string, count int) {
		seedLabelled(priority, "purge", count)
	}

	queueDepth := func(priority string) int64 {
		size, err := testEnv.GetZSETSize(fmt.Sprintf("recache:%d:%s", testEnv.TestHostID, priority))
		Expect(err).ToNot(HaveOccurred())
		return size
	}

	queueMembers := func(priority string) []string {
		members, err := testEnv.GetZSETMembers(fmt.Sprintf("recache:%d:%s", testEnv.TestHostID, priority))
		Expect(err).ToNot(HaveOccurred())
		return members
	}

	Context("Default priorities", func() {
		It("removes high and normal but leaves autorecache in place", func() {
			seedQueue("high", 6)
			seedQueue("normal", 4)
			seedQueue("autorecache", 3)

			resp, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID: testEnv.TestHostID,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesPurged).To(Equal(10))

			Expect(queueDepth("high")).To(Equal(int64(0)))
			Expect(queueDepth("normal")).To(Equal(int64(0)))
			Expect(queueDepth("autorecache")).To(Equal(int64(3)),
				"autorecache holds earned bot-hit refresh times and must survive a default purge")
		})
	})

	Context("Explicit priorities", func() {
		It("removes autorecache when it is named", func() {
			seedQueue("high", 5)
			seedQueue("autorecache", 7)

			resp, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID:     testEnv.TestHostID,
				Priorities: []string{"autorecache"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesPurged).To(Equal(7))

			Expect(queueDepth("autorecache")).To(Equal(int64(0)))
			Expect(queueDepth("high")).To(Equal(int64(5)), "only the named priority is touched")
		})

		It("removes a single named priority and leaves the other default one", func() {
			seedQueue("high", 5)
			seedQueue("normal", 5)

			resp, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID:     testEnv.TestHostID,
				Priorities: []string{"high"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesPurged).To(Equal(5))

			Expect(queueDepth("high")).To(Equal(int64(0)))
			Expect(queueDepth("normal")).To(Equal(int64(5)))
		})
	})

	Context("Validation", func() {
		It("rejects an unknown priority without touching the queues", func() {
			seedQueue("high", 3)

			_, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID:     testEnv.TestHostID,
				Priorities: []string{"urgent"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
			Expect(queueDepth("high")).To(Equal(int64(3)))
		})

		It("rejects an uppercase priority", func() {
			_, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID:     testEnv.TestHostID,
				Priorities: []string{"HIGH"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})

		It("rejects an unknown host_id", func() {
			_, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{HostID: 999})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})
	})

	Context("Permanence across a daemon restart", func() {
		It("hands back only the internal-queue residue, never the purged bulk", func() {
			const (
				residueCount = 20
				bulkPerQueue = 40
			)

			// Manufacture residue the way an RS-starved render host does. No render
			// service is registered, so these entries clear the pull gate, fail the
			// render-budget gate, and sit in the internal queue indefinitely: that
			// gate re-queues without incrementing a retry counter, so nothing ever
			// discards them. This is exactly the state the shutdown flush writes back.
			seedLabelled("normal", "residue", residueCount)

			Expect(testEnv.ResumeScheduler()).To(Succeed())
			Eventually(testEnv.GetInternalQueueSize, 10*time.Second, 100*time.Millisecond).
				Should(Equal(residueCount), "the whole seed should end up parked in the internal queue")
			Expect(queueDepth("normal")).To(Equal(int64(0)))
			Expect(testEnv.PauseScheduler()).To(Succeed())

			// The bulk arrives after the residue is already past the pull gate, so a
			// purge cannot reach the residue and the two sets stay distinguishable.
			seedLabelled("high", "bulk", bulkPerQueue)
			seedLabelled("normal", "bulk", bulkPerQueue)

			resp, statusCode, err := testEnv.SendQueuePurgeRequest(types.QueuePurgeAPIRequest{
				HostID: testEnv.TestHostID,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesPurged).To(Equal(2 * bulkPerQueue))

			// Shutdown writes the internal queue back into these same ZSETs.
			testEnv.StopDaemon()

			Expect(queueDepth("high")).To(Equal(int64(0)),
				"the purged bulk must not come back through the shutdown flush")
			Expect(queueDepth("normal")).To(Equal(int64(residueCount)),
				"the flush may return the residue and nothing beyond it")
			for _, member := range queueMembers("normal") {
				Expect(member).To(ContainSubstring("residue-"),
					"only entries that were past the pull gate at purge time may return")
			}

			// Clear before the daemon is back: a fresh daemon would immediately pull
			// the residue into its own internal queue, out of the next spec's reach.
			Expect(testEnv.ClearRedis()).To(Succeed())
			Expect(testEnv.StartDaemon()).To(Succeed())
		})
	})
})

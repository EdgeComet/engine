package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Recache Pause API", func() {
	// The pause specs let the scheduler run: what they assert is whether it pulls.
	// A fresh daemon keeps a previous spec's in-flight entries out of the counts,
	// and the registry entries are re-added because the suite clears Redis.
	BeforeEach(func() {
		Expect(testEnv.RestartDaemonWithCleanRedis()).To(Succeed())

		testEnv.DrainMockEGReceivedChannel()
		testEnv.DrainMockEGResponses()

		Expect(testEnv.AddMockRSToRegistry("rs-1", 100, 0)).To(Succeed())
		Expect(testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))).To(Succeed())
		Expect(testEnv.WaitForRegistryReady(2 * time.Second)).To(Succeed())
	})

	seedHigh := func(count int) {
		score := float64(time.Now().Unix())
		for i := 0; i < count; i++ {
			url := fmt.Sprintf("https://example.com/paused-%d", i)
			Expect(addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", url, 1, score)).To(Succeed())
		}
	}

	queueDepth := func() int64 {
		size, err := testEnv.GetZSETSize(fmt.Sprintf("recache:%d:high", testEnv.TestHostID))
		Expect(err).ToNot(HaveOccurred())
		return size
	}

	Context("Draining", func() {
		It("leaves a paused host's work queued instead of pulling it", func() {
			resp, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.Paused).To(BeTrue())
			Expect(resp.ExpiresAt).To(BeNumerically(">", time.Now().Unix()))

			seedHigh(5)

			Consistently(queueDepth, 1*time.Second, 100*time.Millisecond).Should(Equal(int64(5)),
				"a paused host must not move work out of durable Redis")

			received, _ := testEnv.DrainChannelUntilCount(1, 500*time.Millisecond)
			Expect(received).To(Equal(0), "no origin request may leave while the host is paused")
		})

		It("drains again after resume", func() {
			_, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			seedHigh(3)
			Consistently(queueDepth, 500*time.Millisecond, 100*time.Millisecond).Should(Equal(int64(3)))

			resumeResp, statusCode, err := testEnv.SendRecacheResumeRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resumeResp.Paused).To(BeFalse())

			Eventually(queueDepth, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)))

			received, _ := testEnv.DrainChannelUntilCount(3, 3*time.Second)
			Expect(received).To(Equal(3))
		})

		It("treats resuming a host that is not paused as a success", func() {
			resp, statusCode, err := testEnv.SendRecacheResumeRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.Paused).To(BeFalse())
		})

		It("extends the window when the same host is paused again", func() {
			first, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			// One tick is enough for the second pause to land in a later second.
			time.Sleep(1100 * time.Millisecond)

			second, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(second.ExpiresAt).To(BeNumerically(">", first.ExpiresAt))
		})
	})

	Context("Work already past the pull gate", func() {
		It("finishes in-flight requests and leaves the internal queue empty", func() {
			// Block every mock-EG response so the first batch keeps its concurrency
			// slots. With all slots held the scheduler cannot pull more, which fixes
			// how much work is past the gate when the pause lands.
			release := testEnv.HoldMockEG()
			defer release()

			seedHigh(20)

			const maxConcurrent = 5
			received, _ := testEnv.DrainChannelUntilCount(maxConcurrent, 5*time.Second)
			Expect(received).To(Equal(maxConcurrent),
				"the first batch fills the host's concurrency and then blocks on the held gateway")

			_, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			release()

			// The batch that was already past the pull gate runs to completion and
			// gives its slots back, so nothing is left holding the capacity that
			// every host shares.
			Eventually(func() float64 {
				return getInFlight(testEnv, testEnv.TestHostID)
			}, 5*time.Second, 100*time.Millisecond).Should(Equal(float64(0)))
			Expect(testEnv.GetInternalQueueSize()).To(Equal(0))

			Consistently(queueDepth, 1*time.Second, 100*time.Millisecond).Should(Equal(int64(15)),
				"the remaining work stays queued for the operator, untouched")
		})
	})

	Context("Enqueue while paused", func() {
		It("accepts new work and reports that it is not draining", func() {
			_, statusCode, err := testEnv.SendRecachePauseRequest(testEnv.TestHostID)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			resp, statusCode, err := testEnv.SendRecacheRequest(types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/queued-while-paused"},
				DimensionIDs: []int{1},
				Priority:     "high",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesEnqueued).To(Equal(1),
				"a pause protects the origin, it does not cost the operator the work list")
			Expect(resp.Paused).To(BeTrue())

			Consistently(queueDepth, 1*time.Second, 100*time.Millisecond).Should(Equal(int64(1)))
		})
	})
})

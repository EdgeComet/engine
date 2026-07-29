package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Regression guard for the unified drain. Before this PR, normal/autorecache
// queues were gated by scheduler.normal_check_interval (60s in production),
// capping throughput at max_concurrent / 60s. With the gate removed, a batch
// of normal entries must drain at the same per-tick cadence as high
// whenever per-host concurrency and RS budget have headroom.
var _ = Describe("Normal queue drain rate", func() {
	BeforeEach(func() {
		err := testEnv.RestartDaemonWithCleanRedis()
		Expect(err).ToNot(HaveOccurred())

		testEnv.DrainMockEGReceivedChannel()
		testEnv.DrainMockEGResponses()

		// Generous RS capacity so the RS-budget gate never throttles.
		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		err = testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))
		Expect(err).ToNot(HaveOccurred())

		err = testEnv.WaitForRegistryReady(2 * time.Second)
		Expect(err).ToNot(HaveOccurred())
	})

	It("drains N normal entries within a small number of ticks (no 60s gate)", func() {
		const total = 20

		// Pause the scheduler so the seed-then-assert sequence is deterministic.
		// Without this, the unified drain (the very behaviour under test) can
		// start pulling entries before we finish enqueueing, racing the initial
		// size sanity check.
		Expect(testEnv.PauseScheduler()).To(Succeed())

		score := float64(time.Now().Unix())
		for i := 0; i < total; i++ {
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "normal",
				fmt.Sprintf("https://example.com/n-%d", i), 1, score+float64(i))
			Expect(err).ToNot(HaveOccurred())
		}

		size, err := testEnv.GetZSETSize("recache:1:normal")
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(Equal(int64(total)))

		// Resume so the unified drain kicks in. Under max_concurrent=5
		// (acceptance default) and 100ms tick, all 20 entries should drain
		// within a couple of seconds — well under the pre-PR 60-second
		// normal_check_interval floor.
		Expect(testEnv.ResumeScheduler()).To(Succeed())

		Eventually(func() int64 {
			s, _ := testEnv.GetZSETSize("recache:1:normal")
			return s
		}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)),
			"normal queue should drain at per-tick cadence (no normal_check_interval gate)")

		received, _ := testEnv.DrainChannelUntilCount(total, 3*time.Second)
		Expect(received).To(Equal(total), "mock EG should receive all %d normal entries", total)
	})
})

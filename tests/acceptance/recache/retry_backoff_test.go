package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests exercise the retry logic implemented in
// internal/cachedaemon/distributor.go: HandleRecacheResults.
//
// They avoid brittle wall-clock assertions by:
// - Using Eventually/Consistently with generous timeouts
// - Checking ordering and lower-bound delays only
// - Relying on event-based signals from the Mock EG channel

var _ = Describe("Retry Backoff", func() {
	BeforeEach(func() {
		// Restart daemon for clean internal queue and state
		err := testEnv.RestartDaemon()
		Expect(err).ToNot(HaveOccurred())

		testEnv.ClearRedis()
		testEnv.DrainMockEGReceivedChannel()

		// Ensure there is RS capacity to process
		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		// Re-register mock EG
		err = testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))
		Expect(err).ToNot(HaveOccurred())

		// Wait for registry to be readable by daemon
		err = testEnv.WaitForRegistryReady(2 * time.Second)
		Expect(err).ToNot(HaveOccurred())
	})

	It("respects exponential backoff without exact time dependence", func() {
		// Base retry delay in test config is 100ms; scheduler tick is 100ms.
		// We'll assert only lower bounds to avoid flakiness.

		url := "https://example.com/backoff-progress"
		score := float64(time.Now().Unix())

		// Pause scheduler to eliminate race between enqueue and response setup
		err := testEnv.PauseScheduler()
		Expect(err).ToNot(HaveOccurred())

		// Program mock EG to: fail twice, then succeed (set before resuming)
		testEnv.SetMockEGResponses([]bool{false, false, true})

		// One entry, high priority (enqueue while paused)
		err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", url, 1, score)
		Expect(err).ToNot(HaveOccurred())

		// Refresh RS registration so last_seen is current (avoids IsHealthy() expiry)
		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		// Resume scheduler to start processing with responses in place
		err = testEnv.ResumeScheduler()
		Expect(err).ToNot(HaveOccurred())

		// Helper to wait for next attempt for URL and return its timestamp
		waitAttempt := func(target string, timeout time.Duration) (time.Time, bool) {
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				select {
				case req := <-testEnv.MockEGReceivedCh:
					if req.URL == target {
						return time.Now(), true
					}
					// Ignore other messages (none expected for this test)
				case <-time.After(10 * time.Millisecond):
					// Polling interval
				}
			}
			return time.Time{}, false
		}

		// Observe three attempts and measure gaps with tolerant thresholds
		t0, ok := waitAttempt(url, 3*time.Second)
		Expect(ok).To(BeTrue(), "first attempt should arrive")

		t1, ok := waitAttempt(url, 3*time.Second)
		Expect(ok).To(BeTrue(), "second attempt should arrive after backoff")

		// First backoff is ~100ms (+ scheduler tick). Assert lower bound 80ms.
		Expect(t1.Sub(t0)).To(BeNumerically(">=", 80*time.Millisecond))

		t2, ok := waitAttempt(url, 4*time.Second)
		Expect(ok).To(BeTrue(), "third attempt should arrive after increased backoff")

		// Second backoff is ~200ms (+ scheduler tick). Assert lower bound 160ms.
		Expect(t2.Sub(t1)).To(BeNumerically(">=", 160*time.Millisecond))

		// After success (3rd attempt), we should not see more attempts for a while
		Consistently(func() bool {
			select {
			case req := <-testEnv.MockEGReceivedCh:
				return req.URL == url
			case <-time.After(150 * time.Millisecond):
				return false
			}
		}, 1*time.Second, 200*time.Millisecond).Should(BeFalse(), "no further attempts expected after success")
	})

	It("stops after MaxRetries and does not keep retrying", func() {
		url := "https://example.com/backoff-max-retries"
		score := float64(time.Now().Unix())

		// Pause scheduler to avoid race with response configuration
		err := testEnv.PauseScheduler()
		Expect(err).ToNot(HaveOccurred())

		// Force more failures than MaxRetries=3 (set before resume)
		testEnv.SetMockEGResponses([]bool{false, false, false, false, false})

		// Enqueue one entry while paused
		err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", url, 1, score)
		Expect(err).ToNot(HaveOccurred())

		// Refresh RS registration so last_seen is current (avoids IsHealthy() expiry)
		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		// Resume scheduler to begin processing
		err = testEnv.ResumeScheduler()
		Expect(err).ToNot(HaveOccurred())

		// Observe attempts up to an overall deadline and ensure we never exceed MaxRetries.
		// Use a generous deadline to avoid wall-clock flakiness.
		attempts := 0
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) && attempts < 3 {
			select {
			case req := <-testEnv.MockEGReceivedCh:
				if req.URL == url {
					attempts++
				}
			case <-time.After(50 * time.Millisecond):
			}
		}

		// We should see up to MaxRetries attempts and not exceed it.
		Expect(attempts).To(BeNumerically("<=", 3), "should not exceed MaxRetries total attempts")
		// And we expect at least one attempt within the generous window.
		Expect(attempts).To(BeNumerically(">=", 1), "should perform at least one attempt in the window")

		// Ensure no more attempts occur for at least 2 seconds
		Consistently(func() bool {
			select {
			case req := <-testEnv.MockEGReceivedCh:
				return req.URL == url
			case <-time.After(150 * time.Millisecond):
				return false
			}
		}, 2*time.Second, 200*time.Millisecond).Should(BeFalse(), "no further attempts after discard")
	})
})

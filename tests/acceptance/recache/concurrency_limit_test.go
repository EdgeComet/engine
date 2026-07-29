package recache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Per-Host Concurrency Limiter", func() {
	BeforeEach(func() {
		err := testEnv.RestartDaemonWithCleanRedis()
		Expect(err).ToNot(HaveOccurred())

		testEnv.DrainMockEGReceivedChannel()
		testEnv.DrainMockEGResponses()

		err = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
		Expect(err).ToNot(HaveOccurred())

		err = testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))
		Expect(err).ToNot(HaveOccurred())

		err = testEnv.WaitForRegistryReady(2 * time.Second)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Status API exposes concurrency stats", func() {
		It("includes a concurrency map in the /status response", func() {
			score := float64(time.Now().Unix())
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", "https://example.com/concurrency-stats", 1, score)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() int {
				received, _ := testEnv.DrainChannelUntilCount(1, 500*time.Millisecond)
				return received
			}, 3*time.Second, 100*time.Millisecond).Should(BeNumerically(">=", 1))

			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status map[string]interface{}
			Expect(json.Unmarshal(respBody, &status)).To(Succeed())

			concurrency, ok := status["concurrency"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "status response must include concurrency map")

			hostKey := fmt.Sprintf("%d", testEnv.TestHostID)
			hostStats, ok := concurrency[hostKey].(map[string]interface{})
			Expect(ok).To(BeTrue(), "concurrency map must include host %d", testEnv.TestHostID)

			Expect(hostStats).To(HaveKey("max_concurrent"))
			Expect(hostStats).To(HaveKey("in_flight"))
			Expect(hostStats).To(HaveKey("acquired_total"))
			Expect(hostStats).To(HaveKey("denied_total"))

			acquired, _ := hostStats["acquired_total"].(float64)
			Expect(acquired).To(BeNumerically(">=", 1), "acquired_total must record at least one acquire")
		})
	})

	Context("EG registry failure does not leak slots", func() {
		It("releases slots after the EG registry is emptied so retries succeed", func() {
			// Initial push: entry should flow through normally with EG registered.
			score := float64(time.Now().Unix())
			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high", "https://example.com/leak-1", 1, score)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() int {
				received, _ := testEnv.DrainChannelUntilCount(1, 500*time.Millisecond)
				return received
			}, 3*time.Second, 100*time.Millisecond).Should(BeNumerically(">=", 1))

			// Wait for in_flight to settle to 0 after the request completes.
			Eventually(func() float64 {
				return getInFlight(testEnv, testEnv.TestHostID)
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(float64(0)))

			// Remove the mock EG from the registry. The daemon's next dispatch will
			// see no healthy EGs and must release every slot it acquired before
			// re-enqueueing the entries.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			Expect(testEnv.RedisClient.Del(ctx, "registry:eg:test-eg-1").Err()).To(Succeed())

			// Push more entries than the default concurrency limit (5) plus a margin.
			// If slots leak, the host's semaphore would saturate and never recover.
			for i := 0; i < 10; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high",
					fmt.Sprintf("https://example.com/leak-%d", i+2), 1, score)
				Expect(err).ToNot(HaveOccurred())
			}

			// Allow several scheduler ticks for the daemon to process the no-EG branch.
			Eventually(func() float64 {
				return getInFlight(testEnv, testEnv.TestHostID)
			}, 5*time.Second, 100*time.Millisecond).Should(Equal(float64(0)),
				"in_flight must drain to 0 even when EG registry is empty (no slot leak)")

			// Re-register the EG and confirm dispatch resumes — proves no zombie slots.
			err = testEnv.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", testEnv.MockEGPort))
			Expect(err).ToNot(HaveOccurred())

			err = testEnv.WaitForRegistryReady(2 * time.Second)
			Expect(err).ToNot(HaveOccurred())

			// Drain however many requests the EG eventually receives (entries may
			// have been retried-with-backoff so timing is loose).
			Eventually(func() int {
				received, _ := testEnv.DrainChannelUntilCount(1, 500*time.Millisecond)
				return received
			}, 30*time.Second, 200*time.Millisecond).Should(BeNumerically(">=", 1),
				"after EG returns, the daemon must successfully dispatch — proving slots were not leaked")
		})
	})

	Context("Saturation under burst", func() {
		It("acquires max_concurrent slots within the first tick, not 1 per tick", func() {
			// Block every mock-EG response so dispatched requests hold their slots.
			// Without the throughput fix, only 1 URL/sec reaches the limiter and
			// in_flight peaks at 1. With the fix, the first tick pulls MaxConcurrent
			// (5) and all 5 saturate the host's semaphore.
			release := testEnv.HoldMockEG()
			defer release()

			// Refresh RS registration so last_seen is current (RegistryTTL=3s);
			// otherwise a slow BeforeEach could leave RS borderline-stale and
			// dispatches would defer on rs_budget=0.
			err := testEnv.AddMockRSToRegistry("rs-1", 100, 0)
			Expect(err).ToNot(HaveOccurred())

			score := float64(time.Now().Unix())
			for i := 0; i < 50; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high",
					fmt.Sprintf("https://example.com/saturation-%d", i), 1, score)
				Expect(err).ToNot(HaveOccurred())
			}

			Eventually(func() float64 {
				return getInFlight(testEnv, testEnv.TestHostID)
			}, 3*time.Second, 50*time.Millisecond).Should(Equal(float64(5)),
				"in_flight must saturate at max_concurrent=5 within the first tick window")

			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status map[string]interface{}
			Expect(json.Unmarshal(respBody, &status)).To(Succeed())
			concurrency, ok := status["concurrency"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			hostStats, ok := concurrency[fmt.Sprintf("%d", testEnv.TestHostID)].(map[string]interface{})
			Expect(ok).To(BeTrue())

			acquired, _ := hostStats["acquired_total"].(float64)
			Expect(acquired).To(Equal(float64(5)),
				"acquired_total must be exactly 5: drain blocks at iter 1 inside DistributeToEGs.wg.Wait so no further acquires happen; without the fix it would be 1-2 in this window")
		})
	})

	Context("Drain mode for high priority", func() {
		It("drains a 200-URL burst across ticks instead of 200 ticks", func() {
			// Dispatch is detached from the tick: each tick pulls
			// free = max_concurrent - in_flight and refills slots freed by the
			// PRIOR tick's completed dispatches. So a fast-URL burst drains at
			// roughly max_concurrent per tick (tick-granularity refill) rather
			// than within a single tick's synchronous drain loop. At
			// max_concurrent=5 and a 100ms tick, 200 URLs take a few seconds --
			// still far from the ~200s of the original per-60s-gate bug, just no
			// longer sub-tick.
			//
			// Dimension 1 is render mode, so dispatch is gated on RS capacity.
			// The real daemon heartbeats RS every 1s; the test harness does not,
			// and the registry entry goes stale after RegistryTTL (3s). Because
			// the tick-paced drain now outlives that window, refresh RS in the
			// background for the duration so staleness -- not the fix -- does not
			// halt render dispatch midway.
			stopRefresh := make(chan struct{})
			refreshDone := make(chan struct{})
			go func() {
				defer close(refreshDone)
				ticker := time.NewTicker(1 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-stopRefresh:
						return
					case <-ticker.C:
						_ = testEnv.AddMockRSToRegistry("rs-1", 100, 0)
					}
				}
			}()
			defer func() { close(stopRefresh); <-refreshDone }()

			Expect(testEnv.AddMockRSToRegistry("rs-1", 100, 0)).ToNot(HaveOccurred())

			score := float64(time.Now().Unix())
			for i := 0; i < 200; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "high",
					fmt.Sprintf("https://example.com/drain-%d", i), 1, score)
				Expect(err).ToNot(HaveOccurred())
			}

			received, _ := testEnv.DrainChannelUntilCount(200, 20*time.Second)
			Expect(received).To(Equal(200),
				"all 200 URLs must reach the mock EG within the tick-paced drain window; without drain mode this would take ~200s")
		})
	})

	Context("Bypass entries are not gated by RS budget", func() {
		It("flows ActionBypass entries through when no RS is healthy", func() {
			// Remove the mock RS so RS budget is 0.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			Expect(testEnv.RedisClient.Del(ctx, "service:render:rs-1").Err()).To(Succeed())
			Expect(testEnv.RedisClient.SRem(ctx, "services:render:list", "rs-1").Err()).To(Succeed())

			Eventually(func() int {
				return testEnv.GetRSCapacityStatus().TotalFreeTabs
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(0))

			// Enqueue against the configured bypass dimension (ID 0, action="bypass"
			// in suite_test.go's hosts.yaml). The composed gate must NOT charge
			// the RS budget for this entry; the per-host concurrency gate is the
			// only check it has to pass. This is the regression test for the
			// no-RS-bypass-only deadlock the redesign was meant to fix.
			score := float64(time.Now().Unix())
			member := types.RecacheMember{
				URL:         "https://example.com/no-rs-bypass",
				DimensionID: 0,
			}
			memberJSON, err := json.Marshal(member)
			Expect(err).ToNot(HaveOccurred())

			err = testEnv.RedisClient.ZAdd(ctx, fmt.Sprintf("recache:%d:high", testEnv.TestHostID),
				&redis.Z{Score: score, Member: string(memberJSON)}).Err()
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() int {
				received, _ := testEnv.DrainChannelUntilCount(1, 500*time.Millisecond)
				return received
			}, 5*time.Second, 100*time.Millisecond).Should(BeNumerically(">=", 1),
				"bypass entry must reach EG with zero RS budget")
		})

		It("discards entries with unresolved dimensions instead of dispatching", func() {
			// dimension_id 99 is not configured on the test host. After the H1
			// fix, the daemon discards such entries before slot acquisition
			// rather than letting them through both gates.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			score := float64(time.Now().Unix())
			member := types.RecacheMember{
				URL:         "https://example.com/unresolved",
				DimensionID: 99,
			}
			memberJSON, err := json.Marshal(member)
			Expect(err).ToNot(HaveOccurred())

			err = testEnv.RedisClient.ZAdd(ctx, fmt.Sprintf("recache:%d:high", testEnv.TestHostID),
				&redis.Z{Score: score, Member: string(memberJSON)}).Err()
			Expect(err).ToNot(HaveOccurred())

			// Entry should be pulled from the ZSET (daemon dequeues + discards).
			Eventually(func() int64 {
				size, _ := testEnv.GetZSETSize(fmt.Sprintf("recache:%d:high", testEnv.TestHostID))
				return size
			}, 3*time.Second, 100*time.Millisecond).Should(Equal(int64(0)),
				"daemon must dequeue the entry from the ZSET")

			// EG must not receive any request for the discarded entry.
			Consistently(func() int {
				select {
				case <-testEnv.MockEGReceivedCh:
					return 1
				default:
					return 0
				}
			}, 1*time.Second, 100*time.Millisecond).Should(Equal(0),
				"discarded entry must not reach the EG")
		})
	})
})

// getInFlight reads the in_flight counter for hostID from the daemon /status endpoint.
func getInFlight(env *RecacheTestEnvironment, hostID int) float64 {
	respBody, statusCode, err := env.SendStatusRequest()
	if err != nil || statusCode != 200 {
		return -1
	}
	var status map[string]interface{}
	if err := json.Unmarshal(respBody, &status); err != nil {
		return -1
	}
	concurrency, ok := status["concurrency"].(map[string]interface{})
	if !ok {
		return -1
	}
	hostStats, ok := concurrency[fmt.Sprintf("%d", hostID)].(map[string]interface{})
	if !ok {
		return 0
	}
	inFlight, _ := hostStats["in_flight"].(float64)
	return inFlight
}

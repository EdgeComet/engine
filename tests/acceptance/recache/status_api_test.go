package recache_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/cachedaemon"
	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Status API", func() {
	Context("Queue Status", func() {
		It("should return queue depths for all priorities", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"https://example.com/page1", "https://example.com/page2"},
				Priority: "high",
			}
			testEnv.SendRecacheRequest(req)

			normalReq := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"https://example.com/page3", "https://example.com/page4", "https://example.com/page5"},
				Priority: "normal",
			}
			testEnv.SendRecacheRequest(normalReq)

			err := addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache", "https://example.com/auto1", 1, 123456.0)
			Expect(err).ToNot(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testEnv.TestHostID, "autorecache", "https://example.com/auto2", 1, 123457.0)
			Expect(err).ToNot(HaveOccurred())

			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status cachedaemon.StatusResponse
			err = json.Unmarshal(respBody, &status)
			Expect(err).ToNot(HaveOccurred())

			Expect(status.Queues).To(HaveKey(testEnv.TestHostID))
			hostQueues := status.Queues[testEnv.TestHostID]

			Expect(hostQueues.High.Total).To(BeNumerically(">", 0))
			Expect(hostQueues.Normal.Total).To(BeNumerically(">", 0))
			Expect(hostQueues.Autorecache.Total).To(Equal(2))
		})
	})

	Context("Internal Queue Status", func() {
		It("should return internal queue size and capacity", func() {
			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status cachedaemon.StatusResponse
			err = json.Unmarshal(respBody, &status)
			Expect(err).ToNot(HaveOccurred())

			Expect(status.InternalQueue.Size).To(BeNumerically(">=", 0))
			Expect(status.InternalQueue.MaxSize).To(Equal(1000))
			Expect(status.InternalQueue.CapacityUsedPercent).To(BeNumerically(">=", 0))
			Expect(status.InternalQueue.CapacityUsedPercent).To(BeNumerically("<=", 100))

			expectedPercent := float64(status.InternalQueue.Size) / float64(status.InternalQueue.MaxSize) * 100
			Expect(status.InternalQueue.CapacityUsedPercent).To(BeNumerically("~", expectedPercent, 0.1))

			Expect(status.Daemon.DaemonID).To(Equal("test-daemon"))
			Expect(status.Daemon.UptimeSeconds).To(BeNumerically(">=", 0))

			Expect(status.RSCapacity.TotalFreeTabs).To(BeNumerically(">=", 0))
			Expect(status.RSCapacity.ReservationPercent).To(Equal(30.0))
		})
	})

	Context("Authentication", func() {
		It("should reject requests with invalid auth", func() {
			_, statusCode, err := testEnv.SendStatusRequestWithAuth("invalid-key")
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(401))
		})
	})

	Context("Daemon Information", func() {
		It("should include daemon metadata in status response", func() {
			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status cachedaemon.StatusResponse
			err = json.Unmarshal(respBody, &status)
			Expect(err).ToNot(HaveOccurred())

			Expect(status.Daemon.DaemonID).ToNot(BeEmpty())
			Expect(status.Daemon.UptimeSeconds).To(BeNumerically(">=", 0))
			Expect(status.Daemon.LastTick).ToNot(BeEmpty())
		})
	})

	Context("RS Capacity", func() {
		It("should return RS capacity information", func() {
			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status cachedaemon.StatusResponse
			err = json.Unmarshal(respBody, &status)
			Expect(err).ToNot(HaveOccurred())

			Expect(status.RSCapacity.ReservationPercent).To(Equal(30.0))
			Expect(status.RSCapacity.TotalFreeTabs).To(BeNumerically(">=", 0))
			Expect(status.RSCapacity.ReservedForOnline).To(BeNumerically(">=", 0))
			Expect(status.RSCapacity.AvailableForRecache).To(BeNumerically(">=", 0))
		})
	})

	Context("Multiple Hosts", func() {
		It("should show queue status for all configured hosts", func() {
			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var status cachedaemon.StatusResponse
			err = json.Unmarshal(respBody, &status)
			Expect(err).ToNot(HaveOccurred())

			Expect(status.Queues).To(HaveKey(testEnv.TestHostID))

			for hostID, queues := range status.Queues {
				Expect(hostID).To(BeNumerically(">", 0))
				Expect(queues.High.Total).To(BeNumerically(">=", 0))
				Expect(queues.Normal.Total).To(BeNumerically(">=", 0))
				Expect(queues.Autorecache.Total).To(BeNumerically(">=", 0))

				Expect(queues.High.DueNow).To(BeNumerically(">=", 0))
				Expect(queues.High.DueNow).To(BeNumerically("<=", queues.High.Total))
			}
		})
	})

	Context("JSON Response Format", func() {
		It("should return valid JSON with correct structure", func() {
			respBody, statusCode, err := testEnv.SendStatusRequest()
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			var rawJSON map[string]interface{}
			err = json.Unmarshal(respBody, &rawJSON)
			Expect(err).ToNot(HaveOccurred())

			Expect(rawJSON).To(HaveKey("daemon"))
			Expect(rawJSON).To(HaveKey("internal_queue"))
			Expect(rawJSON).To(HaveKey("rs_capacity"))
			Expect(rawJSON).To(HaveKey("queues"))

			daemon := rawJSON["daemon"].(map[string]interface{})
			Expect(daemon).To(HaveKey("daemon_id"))
			Expect(daemon).To(HaveKey("uptime_seconds"))
			Expect(daemon).To(HaveKey("last_tick"))

			internalQueue := rawJSON["internal_queue"].(map[string]interface{})
			Expect(internalQueue).To(HaveKey("size"))
			Expect(internalQueue).To(HaveKey("max_size"))
			Expect(internalQueue).To(HaveKey("capacity_used_percent"))
		})
	})
})

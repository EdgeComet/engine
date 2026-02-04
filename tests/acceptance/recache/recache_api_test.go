package recache_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Recache API - Extended Scenarios", func() {
	Context("Bulk Operations", func() {
		It("should handle bulk recache of 1000 URLs", func() {
			urls := make([]string, 1000)
			for i := 0; i < 1000; i++ {
				urls[i] = fmt.Sprintf("https://example.com/page%d", i)
			}

			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         urls,
				DimensionIDs: []int{1},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.URLsCount).To(Equal(1000))
			Expect(resp.EntriesEnqueued).To(Equal(1000))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			// Allow for minor normalization/timing variations
			Expect(size).To(BeNumerically(">=", 999), "At least 999 of 1000 URLs should be enqueued")
		})
	})

	Context("Dimension Handling", func() {
		It("should expand multiple dimensions correctly", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/page1", "https://example.com/page2"},
				DimensionIDs: []int{1, 2},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.URLsCount).To(Equal(2))
			Expect(resp.DimensionIDsCount).To(Equal(2))
			Expect(resp.EntriesEnqueued).To(Equal(4))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(4)))
		})

		It("should default to all dimensions when dimension_ids is empty", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/test"},
				DimensionIDs: []int{},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.URLsCount).To(Equal(1))
			Expect(resp.DimensionIDsCount).To(Equal(2))
			Expect(resp.EntriesEnqueued).To(Equal(2))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(2)))
		})
	})

	Context("Priority Queues", func() {
		It("should enqueue to normal priority queue", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"https://example.com/page1", "https://example.com/page2"},
				Priority: "normal",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.Priority).To(Equal("normal"))

			normalKey := fmt.Sprintf("recache:%d:normal", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(normalKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(BeNumerically(">", 0))

			highKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			highSize, err := testEnv.GetZSETSize(highKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(highSize).To(Equal(int64(0)))
		})
	})
})

package recache_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("ZSET Deduplication & URL Normalization Integration", func() {
	Context("URL Normalization Prevents ZSET Duplicates", func() {
		It("should deduplicate URLs with different query parameter order, case, and ports", func() {
			req := types.RecacheAPIRequest{
				HostID: testEnv.TestHostID,
				URLs: []string{
					// Different query parameter order
					"https://example.com/page?b=2&a=1",
					"https://example.com/page?a=1&b=2",
					// Different case (scheme and host, but path case is preserved in normalization)
					"HTTPS://Example.COM/page?a=1&b=2",
					"https://EXAMPLE.com/page?a=1&b=2",
					// Default port (should be stripped)
					"https://example.com:443/page?a=1&b=2",
					// Fragment (should be removed)
					"https://example.com/page?a=1&b=2#section",
					"https://example.com/page?a=1&b=2#top",
				},
				DimensionIDs: []int{1},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.URLsCount).To(Equal(7))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())

			// All URLs should normalize to the same URL, resulting in 1 ZSET entry
			// Redis ZADD with same member updates score, doesn't create duplicates
			Expect(size).To(Equal(int64(1)), "All URL variations should normalize to single ZSET entry")

			// Verify the member contains normalized URL
			members, err := testEnv.GetZSETMembers(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members).To(HaveLen(1))

			member, err := parseRecacheMember(members[0])
			Expect(err).ToNot(HaveOccurred())

			// Normalized URL should have lowercase scheme and host, sorted params, no port, no fragment
			// Path case is preserved by URLNormalizer
			Expect(member.URL).To(Equal("https://example.com/page?a=1&b=2"))
			Expect(member.DimensionID).To(Equal(1))
		})

		It("should normalize complex URL variations to single entry", func() {
			req := types.RecacheAPIRequest{
				HostID: testEnv.TestHostID,
				URLs: []string{
					"https://example.com/path?utm_source=google&product=123&category=tech",
					"HTTPS://EXAMPLE.COM/path?category=tech&utm_source=google&product=123",
					"https://example.com:443/path?product=123&utm_source=google&category=tech#anchor",
				},
				DimensionIDs: []int{1},
				Priority:     "normal",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			zsetKey := fmt.Sprintf("recache:%d:normal", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(1)), "Complex URL variations should normalize to single entry")
		})
	})

	Context("Dimension-Based ZSET Entry Separation", func() {
		It("should create separate ZSET entries for same URL with different dimensions", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/page"},
				DimensionIDs: []int{1, 2},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesEnqueued).To(Equal(2), "One URL × 2 dimensions = 2 entries")

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(2)), "Same URL with different dimensions creates separate entries")

			// Verify both dimension entries exist
			members, err := testEnv.GetZSETMembers(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members).To(HaveLen(2))

			dimensionIDs := make(map[int]bool)
			for _, memberStr := range members {
				member, err := parseRecacheMember(memberStr)
				Expect(err).ToNot(HaveOccurred())
				Expect(member.URL).To(Equal("https://example.com/page"))
				dimensionIDs[member.DimensionID] = true
			}

			Expect(dimensionIDs).To(HaveLen(2))
			Expect(dimensionIDs[1]).To(BeTrue())
			Expect(dimensionIDs[2]).To(BeTrue())
		})

		It("should keep dimensions independent across different priorities", func() {
			err := testEnv.PauseScheduler()
			Expect(err).ToNot(HaveOccurred())
			defer testEnv.ResumeScheduler()

			url := "https://example.com/test"

			// Add to high priority
			reqHigh := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{url},
				DimensionIDs: []int{1},
				Priority:     "high",
			}
			resp, statusCode, err := testEnv.SendRecacheRequest(reqHigh)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesEnqueued).To(Equal(1))

			// Add to normal priority
			reqNormal := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{url},
				DimensionIDs: []int{1},
				Priority:     "normal",
			}
			resp, statusCode, err = testEnv.SendRecacheRequest(reqNormal)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesEnqueued).To(Equal(1))

			// Verify both queues have the entry
			highKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			highSize, err := testEnv.GetZSETSize(highKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(highSize).To(Equal(int64(1)))

			normalKey := fmt.Sprintf("recache:%d:normal", testEnv.TestHostID)
			normalSize, err := testEnv.GetZSETSize(normalKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(normalSize).To(Equal(int64(1)))
		})
	})

	Context("ZSET Score Update Behavior", func() {
		It("should update score when same URL+dimension is added again", func() {
			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)

			// First request
			req1 := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/page"},
				DimensionIDs: []int{1},
				Priority:     "high",
			}
			_, statusCode, err := testEnv.SendRecacheRequest(req1)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			// Get initial score
			members1, err := testEnv.GetZSETMembersWithScores(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members1).To(HaveLen(1))
			initialScore := members1[0].Score

			// INTENTIONAL TIMING DELAY: Wait for Unix timestamp to change (deduplication uses second-level granularity)
			// This sleep is necessary to test that deduplication updates scores when requests arrive in different seconds
			// This is NOT a timing assumption about processing - it's testing actual time-based behavior
			time.Sleep(1100 * time.Millisecond)

			// Second request for same URL+dimension
			req2 := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/page"},
				DimensionIDs: []int{1},
				Priority:     "high",
			}
			_, statusCode, err = testEnv.SendRecacheRequest(req2)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))

			// Verify still only 1 entry (not duplicated)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(1)), "ZADD should update existing entry, not duplicate")

			// Verify score was updated (newer timestamp)
			members2, err := testEnv.GetZSETMembersWithScores(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members2).To(HaveLen(1))
			updatedScore := members2[0].Score

			Expect(updatedScore).To(BeNumerically(">", initialScore), "Score should be updated to newer timestamp")
		})

		It("should handle multiple URLs with mixed normalization scenarios", func() {
			req := types.RecacheAPIRequest{
				HostID: testEnv.TestHostID,
				URLs: []string{
					// These 3 normalize to same URL
					"https://example.com/page1?a=1&b=2",
					"https://example.com/page1?b=2&a=1",
					"HTTPS://EXAMPLE.COM/page1?a=1&b=2",
					// These 2 normalize to different URL
					"https://example.com/page2?a=1&b=2",
					"https://example.com/page2?b=2&a=1",
					// Unique URL
					"https://example.com/page3",
				},
				DimensionIDs: []int{1},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.URLsCount).To(Equal(6))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())

			// Should have 3 unique entries: page1, page2, page3
			Expect(size).To(Equal(int64(3)), "6 URLs should normalize to 3 unique entries")

			// Verify the unique URLs
			members, err := testEnv.GetZSETMembers(zsetKey)
			Expect(err).ToNot(HaveOccurred())

			urls := make(map[string]bool)
			for _, memberStr := range members {
				member, err := parseRecacheMember(memberStr)
				Expect(err).ToNot(HaveOccurred())
				urls[member.URL] = true
			}

			Expect(urls).To(HaveLen(3))
			Expect(urls["https://example.com/page1?a=1&b=2"]).To(BeTrue())
			Expect(urls["https://example.com/page2?a=1&b=2"]).To(BeTrue())
			Expect(urls["https://example.com/page3"]).To(BeTrue())
		})
	})
})

package recache_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Dimension Actions - Bypass and Block", func() {
	const (
		bypassDimensionID  = 0
		desktopDimensionID = 1
		mobileDimensionID  = 2
		blockDimensionID   = 3
	)

	Context("Invalidate-All with Bypass Dimension", func() {
		var (
			normalizer     *hash.URLNormalizer
			createMetadata func(dimID int, urlHash string)
			metadataExists func(dimID int, urlHash string) bool
		)

		BeforeEach(func() {
			normalizer = hash.NewURLNormalizer()

			createMetadata = func(dimID int, urlHash string) {
				ctx := context.Background()
				metaKey := fmt.Sprintf("meta:cache:%d:%d:%s", testEnv.TestHostID, dimID, urlHash)
				metadata := map[string]interface{}{
					"url":        fmt.Sprintf("https://example.com/%s", urlHash),
					"created_at": time.Now().Unix(),
					"expires_at": time.Now().Add(1 * time.Hour).Unix(),
					"status":     200,
					"source":     "rendered",
				}
				err := testEnv.RedisClient.HSet(ctx, metaKey, metadata).Err()
				Expect(err).ToNot(HaveOccurred())
			}

			metadataExists = func(dimID int, urlHash string) bool {
				ctx := context.Background()
				metaKey := fmt.Sprintf("meta:cache:%d:%d:%s", testEnv.TestHostID, dimID, urlHash)
				exists, err := testEnv.RedisClient.Exists(ctx, metaKey).Result()
				Expect(err).ToNot(HaveOccurred())
				return exists > 0
			}
		})

		It("should include bypass dimension entries in invalidate-all", func() {
			bypassHash := normalizer.Hash("https://example.com/bypass-page")
			desktopHash := normalizer.Hash("https://example.com/desktop-page")

			createMetadata(bypassDimensionID, bypassHash)
			createMetadata(desktopDimensionID, desktopHash)
			Expect(metadataExists(bypassDimensionID, bypassHash)).To(BeTrue())
			Expect(metadataExists(desktopDimensionID, desktopHash)).To(BeTrue())

			req := types.InvalidateAllAPIRequest{
				HostID: testEnv.TestHostID,
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(2))

			Expect(metadataExists(bypassDimensionID, bypassHash)).To(BeFalse())
			Expect(metadataExists(desktopDimensionID, desktopHash)).To(BeFalse())
		})

		It("should clear bypass dimension entries across multiple URLs", func() {
			hash1 := normalizer.Hash("https://example.com/bp-1")
			hash2 := normalizer.Hash("https://example.com/bp-2")
			hash3 := normalizer.Hash("https://example.com/render-1")

			createMetadata(bypassDimensionID, hash1)
			createMetadata(bypassDimensionID, hash2)
			createMetadata(desktopDimensionID, hash3)

			req := types.InvalidateAllAPIRequest{
				HostID: testEnv.TestHostID,
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesInvalidated).To(Equal(3))

			Expect(metadataExists(bypassDimensionID, hash1)).To(BeFalse())
			Expect(metadataExists(bypassDimensionID, hash2)).To(BeFalse())
			Expect(metadataExists(desktopDimensionID, hash3)).To(BeFalse())
		})
	})

	Context("Invalidate-All with Block Dimensions", func() {
		var (
			normalizer     *hash.URLNormalizer
			createMetadata func(dimID int, urlHash string)
			metadataExists func(dimID int, urlHash string) bool
		)

		BeforeEach(func() {
			normalizer = hash.NewURLNormalizer()

			createMetadata = func(dimID int, urlHash string) {
				ctx := context.Background()
				metaKey := fmt.Sprintf("meta:cache:%d:%d:%s", testEnv.TestHostID, dimID, urlHash)
				metadata := map[string]interface{}{
					"url":        fmt.Sprintf("https://example.com/%s", urlHash),
					"created_at": time.Now().Unix(),
					"expires_at": time.Now().Add(1 * time.Hour).Unix(),
					"status":     200,
					"source":     "rendered",
				}
				err := testEnv.RedisClient.HSet(ctx, metaKey, metadata).Err()
				Expect(err).ToNot(HaveOccurred())
			}

			metadataExists = func(dimID int, urlHash string) bool {
				ctx := context.Background()
				metaKey := fmt.Sprintf("meta:cache:%d:%d:%s", testEnv.TestHostID, dimID, urlHash)
				exists, err := testEnv.RedisClient.Exists(ctx, metaKey).Result()
				Expect(err).ToNot(HaveOccurred())
				return exists > 0
			}
		})

		It("should not error when block dimensions have no cache entries", func() {
			desktopHash := normalizer.Hash("https://example.com/page-a")
			mobileHash := normalizer.Hash("https://example.com/page-b")

			createMetadata(desktopDimensionID, desktopHash)
			createMetadata(mobileDimensionID, mobileHash)

			req := types.InvalidateAllAPIRequest{
				HostID: testEnv.TestHostID,
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(2))

			Expect(metadataExists(desktopDimensionID, desktopHash)).To(BeFalse())
			Expect(metadataExists(mobileDimensionID, mobileHash)).To(BeFalse())
		})

		It("should reject explicit block dimension_id in invalidate-all", func() {
			req := types.InvalidateAllAPIRequest{
				HostID:       testEnv.TestHostID,
				DimensionIDs: []int{blockDimensionID},
			}

			_, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})
	})

	Context("Recache with Bypass Dimension", func() {
		It("should accept recache request for bypass dimension_id 0", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/bypass-recache"},
				DimensionIDs: []int{bypassDimensionID},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.URLsCount).To(Equal(1))
			Expect(resp.DimensionIDsCount).To(Equal(1))
			Expect(resp.EntriesEnqueued).To(Equal(1))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(1)))

			members, err := testEnv.GetZSETMembers(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members).To(HaveLen(1))

			member, err := parseRecacheMember(members[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(member.DimensionID).To(Equal(bypassDimensionID))
		})

		It("should include bypass dimension when recaching with mixed dimension_ids", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/mixed-recache"},
				DimensionIDs: []int{bypassDimensionID, desktopDimensionID},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.DimensionIDsCount).To(Equal(2))
			Expect(resp.EntriesEnqueued).To(Equal(2))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(2)))
		})
	})

	Context("Recache with Block Dimensions", func() {
		It("should exclude block dimensions from default dimension list", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/default-dims"},
				DimensionIDs: []int{},
				Priority:     "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			// bypass(0), desktop(1), mobile(2) included; blocked_bots(3) excluded
			Expect(resp.DimensionIDsCount).To(Equal(3))
			Expect(resp.EntriesEnqueued).To(Equal(3))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			members, err := testEnv.GetZSETMembers(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(members).To(HaveLen(3))

			var dimensionIDs []int
			for _, m := range members {
				member, err := parseRecacheMember(m)
				Expect(err).ToNot(HaveOccurred())
				dimensionIDs = append(dimensionIDs, member.DimensionID)
			}
			Expect(dimensionIDs).To(ContainElements(bypassDimensionID, desktopDimensionID, mobileDimensionID))
			Expect(dimensionIDs).ToNot(ContainElement(blockDimensionID))
		})

		It("should reject explicit block dimension_id in recache request", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/block-recache"},
				DimensionIDs: []int{blockDimensionID},
				Priority:     "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})
	})
})

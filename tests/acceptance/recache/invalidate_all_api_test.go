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

var _ = Describe("Invalidate All API", func() {
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

	Context("Basic Invalidation", func() {
		It("should delete all cache metadata for host", func() {
			hash1 := normalizer.Hash("https://example.com/page1")
			hash2 := normalizer.Hash("https://example.com/page2")

			createMetadata(1, hash1)
			createMetadata(2, hash2)
			Expect(metadataExists(1, hash1)).To(BeTrue())
			Expect(metadataExists(2, hash2)).To(BeTrue())

			req := types.InvalidateAllAPIRequest{
				HostID: testEnv.TestHostID,
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(2))

			Expect(metadataExists(1, hash1)).To(BeFalse())
			Expect(metadataExists(2, hash2)).To(BeFalse())
		})

		It("should delete bypass dimension 0 entries", func() {
			hash1 := normalizer.Hash("https://example.com/bypass-page")
			hash2 := normalizer.Hash("https://example.com/render-page")

			createMetadata(0, hash1)
			createMetadata(1, hash2)
			Expect(metadataExists(0, hash1)).To(BeTrue())
			Expect(metadataExists(1, hash2)).To(BeTrue())

			req := types.InvalidateAllAPIRequest{
				HostID: testEnv.TestHostID,
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesInvalidated).To(Equal(2))

			Expect(metadataExists(0, hash1)).To(BeFalse())
			Expect(metadataExists(1, hash2)).To(BeFalse())
		})
	})

	Context("Selective Dimension Invalidation", func() {
		It("should invalidate only specified dimension_ids", func() {
			hash1 := normalizer.Hash("https://example.com/selective")

			createMetadata(1, hash1)
			createMetadata(2, hash1)
			Expect(metadataExists(1, hash1)).To(BeTrue())
			Expect(metadataExists(2, hash1)).To(BeTrue())

			req := types.InvalidateAllAPIRequest{
				HostID:       testEnv.TestHostID,
				DimensionIDs: []int{1},
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesInvalidated).To(Equal(1))

			Expect(metadataExists(1, hash1)).To(BeFalse())
			Expect(metadataExists(2, hash1)).To(BeTrue())
		})

		It("should accept dimension_id 0 explicitly", func() {
			hash1 := normalizer.Hash("https://example.com/bypass-explicit")

			createMetadata(0, hash1)
			createMetadata(1, hash1)
			Expect(metadataExists(0, hash1)).To(BeTrue())
			Expect(metadataExists(1, hash1)).To(BeTrue())

			req := types.InvalidateAllAPIRequest{
				HostID:       testEnv.TestHostID,
				DimensionIDs: []int{0},
			}

			resp, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp.EntriesInvalidated).To(Equal(1))

			Expect(metadataExists(0, hash1)).To(BeFalse())
			Expect(metadataExists(1, hash1)).To(BeTrue())
		})
	})

	Context("Error Handling", func() {
		It("should return 400 for invalid dimension_id", func() {
			req := types.InvalidateAllAPIRequest{
				HostID:       testEnv.TestHostID,
				DimensionIDs: []int{999},
			}

			_, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})

		It("should return 400 for unknown host_id", func() {
			req := types.InvalidateAllAPIRequest{
				HostID: 999,
			}

			_, statusCode, err := testEnv.SendInvalidateAllRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))
		})
	})
})

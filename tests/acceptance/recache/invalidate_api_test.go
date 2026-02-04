package recache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Invalidate API", func() {
	var (
		testURL        = "https://example.com/test"
		normalizedURL  = "https://example.com/test"
		normalizer     *hash.URLNormalizer
		urlHash        string
		dimensionID1   = 1
		dimensionID2   = 2
		createMetadata func(dimensionID int)
		metadataExists func(dimensionID int) bool
		getCacheKey    func(dimensionID int) string
		getMetadataKey func(dimensionID int) string
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
		urlHash = normalizer.Hash(normalizedURL)

		getCacheKey = func(dimensionID int) string {
			return fmt.Sprintf("cache:%d:%d:%s", testEnv.TestHostID, dimensionID, urlHash)
		}

		getMetadataKey = func(dimensionID int) string {
			cacheKey := getCacheKey(dimensionID)
			return fmt.Sprintf("meta:%s", cacheKey)
		}

		createMetadata = func(dimensionID int) {
			ctx := context.Background()
			metaKey := getMetadataKey(dimensionID)

			metadata := map[string]interface{}{
				"url":        normalizedURL,
				"created_at": time.Now().Unix(),
				"expires_at": time.Now().Add(1 * time.Hour).Unix(),
				"status":     200,
				"source":     "rendered",
			}

			err := testEnv.RedisClient.HSet(ctx, metaKey, metadata).Err()
			Expect(err).ToNot(HaveOccurred())
		}

		metadataExists = func(dimensionID int) bool {
			ctx := context.Background()
			metaKey := getMetadataKey(dimensionID)
			exists, err := testEnv.RedisClient.Exists(ctx, metaKey).Result()
			Expect(err).ToNot(HaveOccurred())
			return exists > 0
		}
	})

	Context("Basic Invalidation", func() {
		It("should delete cache metadata", func() {
			createMetadata(dimensionID1)
			Expect(metadataExists(dimensionID1)).To(BeTrue())

			req := types.InvalidateAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{testURL},
				DimensionIDs: []int{dimensionID1},
			}

			resp, statusCode, err := testEnv.SendInvalidateRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(1))

			Expect(metadataExists(dimensionID1)).To(BeFalse())
		})
	})

	Context("Selective Dimension Invalidation", func() {
		It("should invalidate only specific dimension_ids", func() {
			createMetadata(dimensionID1)
			createMetadata(dimensionID2)
			Expect(metadataExists(dimensionID1)).To(BeTrue())
			Expect(metadataExists(dimensionID2)).To(BeTrue())

			req := types.InvalidateAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{testURL},
				DimensionIDs: []int{dimensionID1},
			}

			resp, statusCode, err := testEnv.SendInvalidateRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(1))

			Expect(metadataExists(dimensionID1)).To(BeFalse())
			Expect(metadataExists(dimensionID2)).To(BeTrue())
		})

		It("should invalidate all dimensions when dimension_ids is empty", func() {
			createMetadata(dimensionID1)
			createMetadata(dimensionID2)
			Expect(metadataExists(dimensionID1)).To(BeTrue())
			Expect(metadataExists(dimensionID2)).To(BeTrue())

			req := types.InvalidateAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{testURL},
				DimensionIDs: []int{},
			}

			resp, statusCode, err := testEnv.SendInvalidateRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesInvalidated).To(Equal(2))

			Expect(metadataExists(dimensionID1)).To(BeFalse())
			Expect(metadataExists(dimensionID2)).To(BeFalse())
		})
	})

	Context("Queue Independence (Scenario 9)", func() {
		It("should not affect autorecache ZSET entries when invalidating cache", func() {
			ctx := context.Background()
			createMetadata(dimensionID1)

			autorecacheKey := fmt.Sprintf("recache:%d:autorecache", testEnv.TestHostID)
			member := types.RecacheMember{
				URL:         normalizedURL,
				DimensionID: dimensionID1,
			}
			memberJSON, _ := json.Marshal(member)

			scheduledTime := time.Now().Add(1 * time.Hour)
			err := testEnv.RedisClient.ZAdd(ctx, autorecacheKey, &redis.Z{
				Score:  float64(scheduledTime.Unix()),
				Member: string(memberJSON),
			}).Err()
			Expect(err).ToNot(HaveOccurred())

			size, _ := testEnv.GetZSETSize(autorecacheKey)
			Expect(size).To(Equal(int64(1)))

			req := types.InvalidateAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{testURL},
				DimensionIDs: []int{dimensionID1},
			}

			resp, statusCode, err := testEnv.SendInvalidateRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())

			Expect(metadataExists(dimensionID1)).To(BeFalse())

			size, _ = testEnv.GetZSETSize(autorecacheKey)
			Expect(size).To(Equal(int64(1)))
		})
	})

	Context("Error Handling", func() {
		It("should return 400 for invalid dimension_id", func() {
			req := types.InvalidateAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{testURL},
				DimensionIDs: []int{999},
			}

			_, statusCode, err := testEnv.SendInvalidateRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawInvalidateRequest(reqBody, testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("dimension_id 999 not configured"))
		})
	})
})

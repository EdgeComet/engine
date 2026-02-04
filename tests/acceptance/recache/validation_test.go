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

var _ = Describe("Recache Validation Tests", func() {
	Context("ZSET Operations", func() {
		It("should create and query ZSET entries", func() {
			// Simple test to verify MinRedis works
			ctx := context.Background()
			key := "test:zset"
			member := "test-member"
			score := float64(time.Now().Unix())

			// Add to ZSET
			err := testEnv.RedisClient.ZAdd(ctx, key, &redis.Z{
				Score:  score,
				Member: member,
			}).Err()
			Expect(err).ToNot(HaveOccurred())

			// Verify size
			size, err := testEnv.GetZSETSize(key)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(1)))

			// Verify member exists
			members, err := testEnv.GetZSETMembers(key)
			Expect(err).ToNot(HaveOccurred())
			Expect(members).To(ContainElement(member))
		})

		It("should handle RecacheMember JSON marshaling", func() {
			member := types.RecacheMember{
				URL:         "https://example.com/test",
				DimensionID: 1,
			}

			// Marshal
			data, err := json.Marshal(member)
			Expect(err).ToNot(HaveOccurred())

			// Unmarshal
			var unmarshaled types.RecacheMember
			err = json.Unmarshal(data, &unmarshaled)
			Expect(err).ToNot(HaveOccurred())
			Expect(unmarshaled.URL).To(Equal(member.URL))
			Expect(unmarshaled.DimensionID).To(Equal(member.DimensionID))
		})
	})

	Context("Time Manipulation", func() {
		It("should advance time with FastForward", func() {
			ctx := context.Background()
			key := "test:ttl"

			// Set key with 10 second TTL
			err := testEnv.RedisClient.Set(ctx, key, "value", 10*time.Second).Err()
			Expect(err).ToNot(HaveOccurred())

			// Verify key exists
			exists, err := testEnv.RedisClient.Exists(ctx, key).Result()
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(Equal(int64(1)))

			// Fast forward 11 seconds
			testEnv.FastForwardTime(11 * time.Second)

			// Key should be expired
			exists, err = testEnv.RedisClient.Exists(ctx, key).Result()
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(Equal(int64(0)))
		})
	})

	Context("Helper Functions", func() {
		It("should wait for conditions successfully", func() {
			counter := 0
			condition := func() bool {
				counter++
				return counter >= 3
			}

			result := testEnv.WaitForCondition(condition, 2*time.Second, 100*time.Millisecond)
			Expect(result).To(BeTrue())
			Expect(counter).To(BeNumerically(">=", 3))
		})

		It("should timeout on false conditions", func() {
			condition := func() bool {
				return false
			}

			result := testEnv.WaitForCondition(condition, 500*time.Millisecond, 100*time.Millisecond)
			Expect(result).To(BeFalse())
		})
	})

	Context("API Request Validation", func() {
		It("should reject invalid JSON", func() {
			rawBody := []byte(`{invalid json}`)
			respBody, statusCode, err := testEnv.SendRawRecacheRequest(rawBody, testEnv.InternalAuthKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			var errorResp map[string]interface{}
			err = json.Unmarshal(respBody, &errorResp)
			Expect(err).ToNot(HaveOccurred())
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("invalid json"))
		})

		It("should reject missing host_id", func() {
			req := types.RecacheAPIRequest{
				HostID:   0,
				URLs:     []string{"https://example.com/test"},
				Priority: "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			respBody, _, _ := testEnv.SendRawRecacheRequest([]byte(`{"host_id":0,"urls":["https://example.com"],"priority":"high"}`), testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("host_id is required"))
		})

		It("should reject empty URLs array", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{},
				Priority: "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			respBody, _, _ := testEnv.SendRawRecacheRequest([]byte(`{"host_id":1,"urls":[],"priority":"high"}`), testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("urls array cannot be empty"))
		})

		It("should reject too many URLs", func() {
			urls := make([]string, 10001)
			for i := range urls {
				urls[i] = "https://example.com/test"
			}

			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     urls,
				Priority: "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawRecacheRequest(reqBody, testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("cannot exceed 10000 entries"))
		})

		It("should reject invalid priority", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"https://example.com/test"},
				Priority: "urgent",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawRecacheRequest(reqBody, testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("must be 'high' or 'normal'"))
		})

		It("should reject unknown host_id", func() {
			req := types.RecacheAPIRequest{
				HostID:   999,
				URLs:     []string{"https://example.com/test"},
				Priority: "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawRecacheRequest(reqBody, testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("host_id 999 not found"))
		})

		It("should reject invalid dimension_id", func() {
			req := types.RecacheAPIRequest{
				HostID:       testEnv.TestHostID,
				URLs:         []string{"https://example.com/test"},
				DimensionIDs: []int{999},
				Priority:     "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(400))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawRecacheRequest(reqBody, testEnv.InternalAuthKey)
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(ContainSubstring("dimension_id 999 not configured"))
		})

		It("should reject invalid auth header", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"https://example.com/test"},
				Priority: "high",
			}

			_, statusCode, err := testEnv.SendRecacheRequestWithAuth(req, "wrong-auth-key")
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(401))

			reqBody, _ := json.Marshal(req)
			respBody, _, _ := testEnv.SendRawRecacheRequest(reqBody, "wrong-auth-key")
			var errorResp map[string]interface{}
			json.Unmarshal(respBody, &errorResp)
			Expect(errorResp["success"]).To(BeFalse())
			Expect(errorResp["message"]).To(Equal("unauthorized"))
		})

		It("should skip malformed URLs but return success", func() {
			req := types.RecacheAPIRequest{
				HostID:   testEnv.TestHostID,
				URLs:     []string{"not-a-valid-url", "https://example.com/valid"},
				Priority: "high",
			}

			resp, statusCode, err := testEnv.SendRecacheRequest(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(statusCode).To(Equal(200))
			Expect(resp).NotTo(BeNil())
			Expect(resp.EntriesEnqueued).To(Equal(2))
		})

		It("should accept valid request and create ZSET entries", func() {
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
			Expect(resp.HostID).To(Equal(testEnv.TestHostID))
			Expect(resp.URLsCount).To(Equal(2))
			Expect(resp.DimensionIDsCount).To(Equal(2))
			Expect(resp.EntriesEnqueued).To(Equal(4))
			Expect(resp.Priority).To(Equal("high"))

			zsetKey := fmt.Sprintf("recache:%d:high", testEnv.TestHostID)
			size, err := testEnv.GetZSETSize(zsetKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(4)))
		})
	})
})

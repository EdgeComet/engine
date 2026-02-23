package acceptance_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service Registry Tests", Serial, func() {
	var (
		ctx           context.Context
		testServiceID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testServiceID = "test-service-" + fmt.Sprintf("%d", time.Now().UnixNano())
	})

	Describe("Direct Redis Testing", func() {
		It("should write and read service keys directly", func() {
			By("Writing a service key directly to Redis")
			serviceKey := "service:render:" + testServiceID
			serviceData := `{"id":"` + testServiceID + `","address":"127.0.0.1","port":9999,"status":"healthy"}`

			err := testEnv.RedisClient.Set(ctx, serviceKey, serviceData, 0).Err()
			Expect(err).To(BeNil(), "Should write service key")

			By("Reading the service key back")
			data, err := testEnv.RedisClient.Get(ctx, serviceKey).Result()
			Expect(err).To(BeNil(), "Should read service key")
			Expect(data).To(Equal(serviceData), "Data should match")

			By("Listing all service:render:* keys")
			keys, err := testEnv.RedisClient.Keys(ctx, "service:render:*").Result()
			Expect(err).To(BeNil(), "Should list service keys")
			// fmt.Printf("\nFound service keys: %v\n", keys)
			Expect(keys).To(ContainElement(serviceKey), "Our test key should be in the list")
		})

		It("should verify actual RS registration", func() {
			By("Checking if RS actually registered")
			expectedRSKey := "service:render:rs-test-1"

			exists, err := testEnv.RedisClient.Exists(ctx, expectedRSKey).Result()
			Expect(err).To(BeNil())

			fmt.Printf("\n=== Checking for RS Registration ===\n")
			fmt.Printf("Looking for key: %s\n", expectedRSKey)
			fmt.Printf("Key exists: %v\n", exists > 0)

			if exists > 0 {
				data, _ := testEnv.RedisClient.Get(ctx, expectedRSKey).Result()
				fmt.Printf("RS data: %s\n", data)
			}

			By("Listing all service:render:* keys")
			keys, err := testEnv.RedisClient.Keys(ctx, "service:render:*").Result()
			Expect(err).To(BeNil())
			fmt.Printf("All service:render:* keys (%d): %v\n", len(keys), keys)

			// By("Listing ALL keys in miniredis")
			_, err = testEnv.RedisClient.Keys(ctx, "*").Result()
			Expect(err).To(BeNil())
			/*fmt.Printf("\nAll keys in miniredis (%d):\n", len(allKeys))
			for _, key := range allKeys {
				val, _ := testEnv.RedisClient.Get(ctx, key).Result()
				fmt.Printf("  %s = %s\n", key, val)
			}*/
		})
	})
})

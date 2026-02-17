package recache_test

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cache Reader", func() {
	const testHostID = 1

	var baseURL string

	BeforeEach(func() {
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", testEnv.DaemonPort)
	})

	Context("GET /internal/cache/urls", func() {
		It("returns cached URL items with no filters", func() {
			now := time.Now().Unix()
			expiresAt := now + 3600

			for i := 0; i < 5; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("hash%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/page-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now, 10),
					"expires_at":  strconv.FormatInt(expiresAt, 10),
					"size":        "1024",
					"status_code": "200",
					"source":      "render",
				})
			}

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))
			Expect(result["success"]).To(BeTrue())

			data, ok := result["data"].(map[string]interface{})
			Expect(ok).To(BeTrue())

			items, ok := data["items"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(items).To(HaveLen(5))
		})

		It("filters by status=active", func() {
			now := time.Now().Unix()

			// Active entries (expires in the future)
			for i := 0; i < 3; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("active%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/active-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now-100, 10),
					"expires_at":  strconv.FormatInt(now+3600, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			// Expired entries (expires_at in the past, beyond stale TTL)
			for i := 0; i < 2; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("expired%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/expired-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now-7200, 10),
					"expires_at":  strconv.FormatInt(now-3600, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&status=active", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))
			Expect(result["success"]).To(BeTrue())

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(Equal(3))

			for _, item := range items {
				itemMap := item.(map[string]interface{})
				Expect(itemMap["status"]).To(Equal("active"))
			}
		})

		It("filters by dimension", func() {
			now := time.Now().Unix()
			expiresAt := now + 3600

			// Desktop entries (dimension_id=1)
			for i := 0; i < 3; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("desk%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/desk-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now, 10),
					"expires_at":  strconv.FormatInt(expiresAt, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			// Mobile entries (dimension_id=2)
			for i := 0; i < 2; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 2, fmt.Sprintf("mob%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/mob-%d", i),
					"dimension":   "mobile",
					"created_at":  strconv.FormatInt(now, 10),
					"expires_at":  strconv.FormatInt(expiresAt, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&dimension=desktop", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(Equal(3))

			for _, item := range items {
				itemMap := item.(map[string]interface{})
				Expect(itemMap["dimension"]).To(Equal("desktop"))
			}
		})

		It("filters by urlContains", func() {
			now := time.Now().Unix()
			expiresAt := now + 3600

			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "prod1", map[string]string{
				"url":         "https://example.com/products/shoes",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now, 10),
				"expires_at":  strconv.FormatInt(expiresAt, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "prod2", map[string]string{
				"url":         "https://example.com/products/hats",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now, 10),
				"expires_at":  strconv.FormatInt(expiresAt, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "blog1", map[string]string{
				"url":         "https://example.com/blog/article",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now, 10),
				"expires_at":  strconv.FormatInt(expiresAt, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&urlContains=products", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(Equal(2))

			for _, item := range items {
				itemMap := item.(map[string]interface{})
				Expect(itemMap["url"]).To(ContainSubstring("products"))
			}
		})

		It("supports cursor pagination", func() {
			now := time.Now().Unix()
			expiresAt := now + 3600

			for i := 0; i < 10; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("pag%02d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/page-%02d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now, 10),
					"expires_at":  strconv.FormatInt(expiresAt, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			// First page
			resp1, result1 := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&limit=3", testHostID), testEnv.InternalAuthKey)

			Expect(resp1.StatusCode).To(Equal(200))
			data1 := result1["data"].(map[string]interface{})
			items1 := data1["items"].([]interface{})
			Expect(len(items1)).To(Equal(3))

			_, hasCursor := data1["cursor"]
			Expect(hasCursor).To(BeTrue())
			_, hasMore := data1["hasMore"]
			Expect(hasMore).To(BeTrue())
		})

		It("applies combined filters", func() {
			now := time.Now().Unix()

			// Active render desktop (dimension_id=1)
			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "combo1", map[string]string{
				"url":         "https://example.com/active-render-desktop",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now-100, 10),
				"expires_at":  strconv.FormatInt(now+3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			// Active bypass desktop
			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "combo2", map[string]string{
				"url":         "https://example.com/active-bypass-desktop",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now-100, 10),
				"expires_at":  strconv.FormatInt(now+3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "bypass",
			})

			// Active render mobile (dimension_id=2)
			populateCacheEntry(testEnv.MiniRedis, testHostID, 2, "combo3", map[string]string{
				"url":         "https://example.com/active-render-mobile",
				"dimension":   "mobile",
				"created_at":  strconv.FormatInt(now-100, 10),
				"expires_at":  strconv.FormatInt(now+3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			// Expired render desktop
			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "combo4", map[string]string{
				"url":         "https://example.com/expired-render-desktop",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now-7200, 10),
				"expires_at":  strconv.FormatInt(now-3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&status=active&dimension=desktop&source=render", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(Equal(1))

			itemMap := items[0].(map[string]interface{})
			Expect(itemMap["status"]).To(Equal("active"))
			Expect(itemMap["dimension"]).To(Equal("desktop"))
			Expect(itemMap["source"]).To(Equal("render"))
		})
	})

	Context("GET /internal/cache/summary", func() {
		It("returns correct aggregate counts", func() {
			now := time.Now().Unix()

			// 3 active entries (desktop, dimension_id=1)
			for i := 0; i < 3; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("sumact%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/active-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now-100, 10),
					"expires_at":  strconv.FormatInt(now+3600, 10),
					"size":        "1000",
					"status_code": "200",
					"source":      "render",
				})
			}

			// 2 stale entries (expired but within stale TTL of 60s)
			for i := 0; i < 2; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("sumstale%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/stale-%d", i),
					"dimension":   "mobile",
					"created_at":  strconv.FormatInt(now-200, 10),
					"expires_at":  strconv.FormatInt(now-10, 10),
					"size":        "2000",
					"status_code": "200",
					"source":      "bypass",
				})
			}

			// 1 expired entry (beyond stale TTL)
			populateCacheEntry(testEnv.MiniRedis, testHostID, 1, "sumexp0", map[string]string{
				"url":         "https://example.com/expired-0",
				"dimension":   "desktop",
				"created_at":  strconv.FormatInt(now-7200, 10),
				"expires_at":  strconv.FormatInt(now-3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "render",
			})

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/summary?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))
			Expect(result["success"]).To(BeTrue())

			data := result["data"].(map[string]interface{})
			Expect(data["totalUrls"]).To(BeNumerically("==", 6))
			Expect(data["activeCount"]).To(BeNumerically("==", 3))
			Expect(data["staleCount"]).To(BeNumerically("==", 2))
			Expect(data["expiredCount"]).To(BeNumerically("==", 1))
		})

		It("returns byDimension and bySource breakdowns", func() {
			now := time.Now().Unix()

			// 2 desktop render (dimension_id=1)
			for i := 0; i < 2; i++ {
				populateCacheEntry(testEnv.MiniRedis, testHostID, 1, fmt.Sprintf("bddr%d", i), map[string]string{
					"url":         fmt.Sprintf("https://example.com/dr-%d", i),
					"dimension":   "desktop",
					"created_at":  strconv.FormatInt(now, 10),
					"expires_at":  strconv.FormatInt(now+3600, 10),
					"size":        "500",
					"status_code": "200",
					"source":      "render",
				})
			}

			// 1 mobile bypass (dimension_id=2)
			populateCacheEntry(testEnv.MiniRedis, testHostID, 2, "bdmb0", map[string]string{
				"url":         "https://example.com/mb-0",
				"dimension":   "mobile",
				"created_at":  strconv.FormatInt(now, 10),
				"expires_at":  strconv.FormatInt(now+3600, 10),
				"size":        "500",
				"status_code": "200",
				"source":      "bypass",
			})

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/summary?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})

			byDimension := data["byDimension"].(map[string]interface{})
			Expect(byDimension["desktop"]).To(BeNumerically("==", 2))
			Expect(byDimension["mobile"]).To(BeNumerically("==", 1))

			bySource := data["bySource"].(map[string]interface{})
			Expect(bySource["render"]).To(BeNumerically("==", 2))
			Expect(bySource["bypass"]).To(BeNumerically("==", 1))
		})

		It("returns zeros for empty host", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/summary?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})
			Expect(data["totalUrls"]).To(BeNumerically("==", 0))
			Expect(data["activeCount"]).To(BeNumerically("==", 0))
			Expect(data["staleCount"]).To(BeNumerically("==", 0))
			Expect(data["expiredCount"]).To(BeNumerically("==", 0))
		})
	})

	Context("GET /internal/cache/queue", func() {
		It("returns queue items", func() {
			now := float64(time.Now().Unix())

			err := addToRecacheZSET(testEnv.RedisClient, testHostID, "high", "https://example.com/q1", 1, now)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "normal", "https://example.com/q2", 2, now+10)
			Expect(err).NotTo(HaveOccurred())

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))
			Expect(result["success"]).To(BeTrue())

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(BeNumerically(">=", 2))

			firstItem := items[0].(map[string]interface{})
			Expect(firstItem).To(HaveKey("url"))
			Expect(firstItem).To(HaveKey("dimension"))
			Expect(firstItem).To(HaveKey("priority"))
			Expect(firstItem).To(HaveKey("scheduledAt"))
		})

		It("filters by priority=high", func() {
			now := float64(time.Now().Unix())

			err := addToRecacheZSET(testEnv.RedisClient, testHostID, "high", "https://example.com/high1", 1, now)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "high", "https://example.com/high2", 1, now+1)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "normal", "https://example.com/normal1", 1, now+2)
			Expect(err).NotTo(HaveOccurred())

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue?host_id=%d&priority=high", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))

			data := result["data"].(map[string]interface{})
			items := data["items"].([]interface{})
			Expect(len(items)).To(Equal(2))

			for _, item := range items {
				itemMap := item.(map[string]interface{})
				Expect(itemMap["priority"]).To(Equal("high"))
			}
		})

		It("supports cursor pagination", func() {
			now := float64(time.Now().Unix())

			for i := 0; i < 8; i++ {
				err := addToRecacheZSET(testEnv.RedisClient, testHostID, "high",
					fmt.Sprintf("https://example.com/pq%d", i), 1, now+float64(i))
				Expect(err).NotTo(HaveOccurred())
			}

			// First page with limit=3
			resp1, result1 := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue?host_id=%d&limit=3", testHostID), testEnv.InternalAuthKey)

			Expect(resp1.StatusCode).To(Equal(200))

			data1 := result1["data"].(map[string]interface{})
			items1 := data1["items"].([]interface{})
			Expect(len(items1)).To(Equal(3))
			Expect(data1["hasMore"]).To(BeTrue())

			cursor := data1["cursor"].(string)
			Expect(cursor).NotTo(Equal("0"))

			// Second page
			resp2, result2 := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue?host_id=%d&limit=3&cursor=%s", testHostID, cursor), testEnv.InternalAuthKey)

			Expect(resp2.StatusCode).To(Equal(200))

			data2 := result2["data"].(map[string]interface{})
			items2 := data2["items"].([]interface{})
			Expect(len(items2)).To(Equal(3))
		})
	})

	Context("GET /internal/cache/queue/summary", func() {
		It("returns pending count", func() {
			// Pause scheduler to prevent it from consuming ZSET entries via ZPOPMIN
			err := testEnv.PauseScheduler()
			Expect(err).NotTo(HaveOccurred())

			now := float64(time.Now().Unix())

			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "high", "https://example.com/qs1", 1, now)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "high", "https://example.com/qs2", 1, now+1)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "normal", "https://example.com/qs3", 2, now+2)
			Expect(err).NotTo(HaveOccurred())
			err = addToRecacheZSET(testEnv.RedisClient, testHostID, "autorecache", "https://example.com/qs4", 1, now+3)
			Expect(err).NotTo(HaveOccurred())

			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue/summary?host_id=%d", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(200))
			Expect(result["success"]).To(BeTrue())

			data := result["data"].(map[string]interface{})
			Expect(data["pending"]).To(BeNumerically("==", 4))
			Expect(data).To(HaveKey("processing"))

			err = testEnv.ResumeScheduler()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("error responses", func() {
		It("returns 401 without auth", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d", testHostID), "")

			Expect(resp.StatusCode).To(Equal(401))
			Expect(result["success"]).To(BeFalse())
			Expect(result["message"]).To(Equal("unauthorized"))
		})

		It("returns 401 with wrong auth key", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d", testHostID), "wrong-key")

			Expect(resp.StatusCode).To(Equal(401))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 404 for unknown host", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				"/internal/cache/urls?host_id=999", testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(404))
			Expect(result["success"]).To(BeFalse())
			Expect(result["message"]).To(ContainSubstring("not found"))
		})

		It("returns 400 for invalid params - sizeMax less than sizeMin", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&sizeMin=1000&sizeMax=500", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for invalid params - cacheAgeMax less than cacheAgeMin", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&cacheAgeMin=1000&cacheAgeMax=500", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for invalid status filter value", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&status=invalid", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for limit out of range - zero", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&limit=0", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for limit out of range - exceeds max", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&limit=101", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for missing host_id", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				"/internal/cache/urls", testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for invalid source filter", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/urls?host_id=%d&source=invalid", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})

		It("returns 400 for invalid priority filter on queue endpoint", func() {
			resp, result := makeDaemonGETRequest(baseURL,
				fmt.Sprintf("/internal/cache/queue?host_id=%d&priority=invalid", testHostID), testEnv.InternalAuthKey)

			Expect(resp.StatusCode).To(Equal(400))
			Expect(result["success"]).To(BeFalse())
		})
	})
})

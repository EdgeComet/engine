package sharding_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
)

var _ = Describe("Edge Cases", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	AfterEach(func() {
		By("Ensuring all 3 EGs are running for next test")
		// Check and restart any stopped EGs
		for i := 1; i <= 3; i++ {
			if !testEnv.IsEGRunning(i) {
				if os.Getenv("DEBUG") != "" {
					fmt.Printf("AfterEach: Restarting EG%d\n", i)
				}
				err := testEnv.StartEG(i)
				if err != nil {
					fmt.Printf("Warning: Failed to restart EG%d in AfterEach: %v\n", i, err)
				}
				// Wait for EG to register
				time.Sleep(2 * time.Second)
			}
		}

		// Wait for cluster to stabilize
		Eventually(func() int {
			size, _ := testEnv.GetClusterSize()
			return size
		}, 15*time.Second, 500*time.Millisecond).Should(Equal(3),
			"Cluster should stabilize to 3 EGs in AfterEach")
	})

	Context("Security", Serial, func() {
		It("should reject internal API requests with bad auth", func() {
			By("Calling internal API with incorrect auth key")
			resp, err := testEnv.CallInternalAPIWithAuth(1, "/internal/cache/status", "GET", nil, "wrong-key")
			Expect(err).To(BeNil(), "HTTP request should succeed (even if auth fails)")

			By("Verifying 401 Unauthorized response")
			Expect(resp.StatusCode).To(Equal(401), "Should return 401 Unauthorized")

			By("Verifying error message in response body")
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			Expect(err).To(BeNil())
			Expect(string(body)).To(ContainSubstring("unauthorized"), "Response should indicate unauthorized")
		})

		It("should reject internal API requests without auth header", func() {
			By("Calling internal API without auth header")
			resp, err := testEnv.CallInternalAPIWithAuth(1, "/internal/cache/status", "GET", nil, "")
			Expect(err).To(BeNil(), "HTTP request should succeed (even if auth fails)")

			By("Verifying 401 Unauthorized response")
			Expect(resp.StatusCode).To(Equal(401), "Should return 401 Unauthorized")

			By("Verifying error message in response body")
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			Expect(err).To(BeNil())
			Expect(string(body)).To(ContainSubstring("unauthorized"), "Response should indicate unauthorized")
		})

		It("should NOT expose internal API on public port", func() {
			By("Attempting to access internal API on public port 9201")
			req, err := http.NewRequest("GET", "http://127.0.0.1:9201/internal/cache/status", nil)
			Expect(err).To(BeNil())
			req.Header.Set("X-Internal-Auth", "test-shared-secret-key-12345678")

			resp, err := testEnv.HTTPClient.Do(req)
			Expect(err).To(BeNil(), "HTTP request should complete")
			defer resp.Body.Close()

			By("Verifying 404 Not Found (route doesn't exist on public server)")
			Expect(resp.StatusCode).To(Equal(404), "Internal API should not be exposed on public port")
		})

		It("internal API should only respond on internal port", func() {
			By("Verifying internal API works on internal port 9202")
			resp, err := testEnv.CallInternalAPIWithAuth(1, "/internal/cache/status", "GET", nil, "test-shared-secret-key-12345678")
			Expect(err).To(BeNil())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(200), "Internal API should work on internal port with valid auth")

			By("Verifying internal API returns 404 on public port 9201")
			req, err := http.NewRequest("GET", "http://127.0.0.1:9201/internal/cache/status", nil)
			Expect(err).To(BeNil())
			req.Header.Set("X-Internal-Auth", "test-shared-secret-key-12345678")

			publicResp, err := testEnv.HTTPClient.Do(req)
			Expect(err).To(BeNil())
			defer publicResp.Body.Close()
			Expect(publicResp.StatusCode).To(Equal(404), "Internal API should not exist on public port")
		})
	})

	Context("Cluster Edge Cases", Serial, func() {
		It("should handle zero EGs in cluster (all offline)", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?test=zero_egs"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Stopping EG2 and EG3")
			err = testEnv.StopEG(2)
			Expect(err).To(BeNil(), "Should successfully stop EG2")
			err = testEnv.StopEG(3)
			Expect(err).To(BeNil(), "Should successfully stop EG3")

			By("Waiting for EG2 and EG3 to be removed from registry")
			err = testEnv.WaitForEGOffline("eg2", 15*time.Second)
			Expect(err).To(BeNil(), "EG2 should expire from registry")
			err = testEnv.WaitForEGOffline("eg3", 15*time.Second)
			Expect(err).To(BeNil(), "EG3 should expire from registry")

			By("Verifying cluster size is 1")
			Eventually(func() int {
				size, _ := testEnv.GetClusterSize()
				return size
			}, 5*time.Second, 500*time.Millisecond).Should(Equal(1), "Cluster should have only 1 EG")

			By("Making request via EG1 (only available EG)")
			response := testEnv.RequestViaEG1("/static/test.html?test=zero_egs")

			By("Verifying request succeeded")
			Expect(response.Error).To(BeNil(), "Request should succeed")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 OK")
			Expect(response.Body).To(ContainSubstring("Sharding Test Page"))

			By("Verifying eg_ids contains only eg1 (no other EGs available)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}), "eg_ids should contain only eg1")
		})

		It("should handle concurrent push to same EG", func() {
			// Use 2 different URLs that generate different content
			testURL1 := testEnv.Config.TestPagesURL() + "/qparam-test/page1?id=1"
			testURL2 := testEnv.Config.TestPagesURL() + "/qparam-test/page2?id=2"

			result1, err := normalizer.Normalize(testURL1, nil)
			Expect(err).To(BeNil())
			urlHash1 := normalizer.Hash(result1.NormalizedURL)
			cacheKey1 := testEnv.BuildCacheKey(1, 1, urlHash1)

			result2, err := normalizer.Normalize(testURL2, nil)
			Expect(err).To(BeNil())
			urlHash2 := normalizer.Hash(result2.NormalizedURL)
			cacheKey2 := testEnv.BuildCacheKey(1, 1, urlHash2)

			By("Sending 2 requests simultaneously to EG1")
			var wg sync.WaitGroup
			var response1, response2 *TestResponse

			wg.Add(2)
			go func() {
				defer wg.Done()
				response1 = testEnv.RequestViaEG1("/qparam-test/page1?id=1")
			}()
			go func() {
				defer wg.Done()
				response2 = testEnv.RequestViaEG1("/qparam-test/page2?id=2")
			}()
			wg.Wait()

			By("Verifying both requests succeeded")
			Expect(response1.Error).To(BeNil(), "Request 1 should succeed")
			Expect(response1.StatusCode).To(Equal(200), "Request 1 should return 200")
			Expect(response2.Error).To(BeNil(), "Request 2 should succeed")
			Expect(response2.StatusCode).To(Equal(200), "Request 2 should return 200")

			By("Verifying both caches were created")
			egIDs1, err := testEnv.GetEGIDs(cacheKey1)
			Expect(err).To(BeNil(), "Cache 1 metadata should exist")
			Expect(len(egIDs1)).To(Equal(2), "Cache 1 should have RF=2")

			egIDs2, err := testEnv.GetEGIDs(cacheKey2)
			Expect(err).To(BeNil(), "Cache 2 metadata should exist")
			Expect(len(egIDs2)).To(Equal(2), "Cache 2 should have RF=2")

			By("Verifying no file corruption (content is correct)")
			Expect(response1.Body).To(ContainSubstring("QPARAM_TEST_PAGE"), "Content 1 should be valid")
			Expect(response2.Body).To(ContainSubstring("QPARAM_TEST_PAGE"), "Content 2 should be valid")
			Expect(response1.Body).To(ContainSubstring("page1"), "Content 1 should contain page1")
			Expect(response2.Body).To(ContainSubstring("page2"), "Content 2 should contain page2")
		})
	})

	Context("Data Handling", Serial, func() {
		It("should handle very large HTML (10MB+)", func() {
			testURL := testEnv.Config.TestPagesURL() + "/static/large.html"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Requesting large HTML file via EG1")
			startTime := time.Now()
			response := testEnv.RequestViaEG1("/static/large.html")
			renderDuration := time.Since(startTime)

			By("Verifying request succeeded")
			Expect(response.Error).To(BeNil(), "Request should succeed")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 OK")

			By("Verifying content is large (>10MB)")
			contentSize := len(response.Body)
			Expect(contentSize).To(BeNumerically(">", 10*1024*1024), "Content should be larger than 10MB")

			By("Verifying render completed within timeout (30s)")
			Expect(renderDuration).To(BeNumerically("<", 30*time.Second), "Render should complete within 30s")

			By("Waiting for push operations to complete (large file may take longer)")
			time.Sleep(2 * time.Second)

			By("Verifying push succeeded (eg_ids has at least 1 EG)")
			egIDs, err := testEnv.WaitForStableEGIDs(cacheKey, 10*time.Second)
			Expect(err).To(BeNil(), "Cache metadata should exist")
			Expect(len(egIDs)).To(BeNumerically(">=", 1), "Should have at least rendering EG in eg_ids")
			Expect(egIDs).To(ContainElement("eg1"), "Rendering EG (eg1) should be in eg_ids")

			By("Requesting same file via EG2 (pulls from cluster)")
			response2 := testEnv.RequestViaEG2("/static/large.html")

			By("Verifying pull succeeded")
			Expect(response2.Error).To(BeNil(), "Pull request should succeed")
			Expect(response2.StatusCode).To(Equal(200), "Should return 200 OK")

			By("Verifying content matches original")
			Expect(len(response2.Body)).To(Equal(contentSize), "Pulled content size should match original")
			Expect(response2.Body).To(ContainSubstring("Large Test Page"), "Content should be valid HTML")
		})

		It("should handle special characters in cache key", func() {
			// Test with URL-encoded special characters
			testURL := testEnv.Config.TestPagesURL() + "/static/test.html?foo=bar&baz=qux&special=%20%2F%3F"
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Requesting URL with special characters via EG1")
			response := testEnv.RequestViaEG1("/static/test.html?foo=bar&baz=qux&special=%20%2F%3F")

			By("Verifying request succeeded")
			Expect(response.Error).To(BeNil(), "Request should succeed")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 OK")

			By("Verifying cache metadata stored correctly")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil(), "Metadata should exist")
			Expect(metadata).NotTo(BeEmpty())

			By("Verifying eg_ids retrieved without corruption")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil(), "eg_ids should parse correctly")
			Expect(len(egIDs)).To(Equal(2), "Should have RF=2")

			By("Requesting via different EG to test pull with special chars")
			response2 := testEnv.RequestViaEG3("/static/test.html?foo=bar&baz=qux&special=%20%2F%3F")
			Expect(response2.Error).To(BeNil(), "Pull should work with special chars")
			Expect(response2.StatusCode).To(Equal(200))

			By("Testing with Unicode characters")
			// Note: URL encoding will handle these, but testing full path
			response3 := testEnv.RequestViaEG1("/static/test.html?name=caf%C3%A9")
			Expect(response3.Error).To(BeNil(), "Request with Unicode should succeed")
			Expect(response3.StatusCode).To(Equal(200))
		})
	})
})

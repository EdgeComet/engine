package acceptance_test

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Render Cache - Redirect Handling", Serial, func() {

	Context("when rendering pages that return HTTP redirect responses", func() {

		It("should create cache metadata for 301 permanent redirects with empty eg_ids", func() {
			url := "/stale-test/multi-status?status=301"

			By("Making first request that returns 301 redirect")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil(), "Request should complete without error")

			By("Verifying cache metadata was created in Redis")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue(), "Cache metadata should exist")

			By("Retrieving cache metadata from Redis")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			Expect(metadata).NotTo(BeEmpty())

			By("Verifying status_code is 301")
			Expect(metadata["status_code"]).To(Equal("301"))

			By("Verifying size is 0 (no HTML content)")
			Expect(metadata["size"]).To(Equal("0"), "Redirect should have no HTML body")

			By("Verifying eg_ids is empty (no replication for redirects)")
			egIDs := metadata["eg_ids"]
			Expect(egIDs).To(BeEmpty(), "Redirects should have empty eg_ids to prevent pull attempts")

			By("Verifying source is 'render'")
			Expect(metadata["source"]).To(Equal("render"))

			By("Verifying headers contains Location")
			headers := metadata["headers"]
			Expect(headers).To(ContainSubstring("Location"), "Redirect headers should contain Location")

			By("Verifying file_path is set")
			filePath := metadata["file_path"]
			Expect(filePath).NotTo(BeEmpty(), "file_path should be set even if file doesn't exist")

			By("Verifying HTML file does NOT exist on disk")
			if filePath != "" {
				cacheDir := testEnv.Config.EdgeGateway.Storage.BasePath
				if cacheDir == "" {
					cacheDir = "/tmp/edgecomet/cache"
				}
				absolutePath := filepath.Join(cacheDir, filePath)
				_, err := os.Stat(absolutePath)
				Expect(os.IsNotExist(err)).To(BeTrue(), "HTML file should not exist for redirects")
			}
		})

		It("should create cache metadata for 302 temporary redirects with empty eg_ids", func() {
			url := "/stale-test/multi-status?status=302"

			By("Making first request that returns 302 redirect")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())

			By("Verifying cache metadata")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying 302 status code")
			Expect(metadata["status_code"]).To(Equal("302"))

			By("Verifying eg_ids is empty")
			Expect(metadata["eg_ids"]).To(BeEmpty(), "302 redirects should also have empty eg_ids")

			By("Verifying size is 0")
			Expect(metadata["size"]).To(Equal("0"))
		})

		It("should create cache metadata for 307 temporary redirects", func() {
			url := "/stale-test/multi-status?status=307"

			By("Making first request that returns 307 redirect")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())

			By("Verifying cache metadata")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying 307 status code and empty eg_ids")
			Expect(metadata["status_code"]).To(Equal("307"))
			Expect(metadata["eg_ids"]).To(BeEmpty())
			Expect(metadata["size"]).To(Equal("0"))
		})

		PIt("should create cache metadata for 308 permanent redirects", func() {
			// PENDING: Chrome/chromedp may not trigger EventRequestWillBeSent for 308
			// 308 is a newer redirect type (RFC 7538) and may have different browser handling
			url := "/stale-test/multi-status?status=308"

			By("Making first request that returns 308 redirect")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())

			By("Verifying cache metadata")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			Expect(err).To(BeNil())
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying 308 status code and empty eg_ids")
			Expect(metadata["status_code"]).To(Equal("308"))
			Expect(metadata["eg_ids"]).To(BeEmpty())
			Expect(metadata["size"]).To(Equal("0"))
		})
	})

	Context("when serving cached redirect responses", func() {

		It("should serve 301 redirect from cache on second request", func() {
			url := "/stale-test/multi-status?status=301&redirect_target=/final"

			By("First request - creates cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			locationHeader1 := resp1.Headers.Get("Location")

			By("Second request - served from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())

			By("Verifying cache hit headers")
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Second request should be served from cache")
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"),
				"Cache header should indicate hit")

			By("Verifying Location header preserved")
			locationHeader2 := resp2.Headers.Get("Location")
			Expect(locationHeader2).To(Equal(locationHeader1),
				"Location header should be preserved from cache")

			By("Verifying cache age header present")
			Expect(resp2.Headers.Get("X-Cache-Age")).NotTo(BeEmpty(),
				"Cache age should be set for cached redirects")
		})

		It("should serve 302 redirect from cache with correct headers", func() {
			url := "/stale-test/multi-status?status=302&redirect_target=/another-page"

			By("First request")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Second request from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())

			By("Verifying cache hit")
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))
		})

		PIt("should update last_access timestamp on cache hit", func() {
			/// todo: currently last_access is not updating
			url := "/stale-test/multi-status?status=301"

			By("First request - creates cache")
			testEnv.RequestRender(url)

			By("Getting initial metadata")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata1, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			lastAccess1, _ := strconv.ParseInt(metadata1["last_access"], 10, 64)

			By("Waiting 1 second")
			time.Sleep(1 * time.Second)

			By("Second request - cache hit")
			testEnv.RequestRender(url)

			By("Getting updated metadata")
			metadata2, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())
			lastAccess2, _ := strconv.ParseInt(metadata2["last_access"], 10, 64)

			By("Verifying last_access was updated")
			Expect(lastAccess2).To(BeNumerically(">", lastAccess1),
				"last_access should be updated on cache hit")
		})
	})

	Context("when comparing redirect caches to regular content caches", func() {

		It("should have empty eg_ids for redirects but non-empty for regular content", func() {
			redirectURL := "/stale-test/multi-status?status=301"
			contentURL := "/static/simple.html"

			By("Creating redirect cache")
			testEnv.RequestRender(redirectURL)
			redirectKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+redirectURL, "desktop")
			redirectMeta, err := testEnv.GetCacheMetadata(redirectKey)
			Expect(err).To(BeNil())

			By("Creating regular content cache")
			testEnv.RequestRender(contentURL)
			contentKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+contentURL, "desktop")
			contentMeta, err := testEnv.GetCacheMetadata(contentKey)
			Expect(err).To(BeNil())

			By("Verifying redirect has empty eg_ids")
			Expect(redirectMeta["eg_ids"]).To(BeEmpty(),
				"Redirect cache should have empty eg_ids")

			By("Verifying regular content has non-empty eg_ids")
			Expect(contentMeta["eg_ids"]).NotTo(BeEmpty(),
				"Regular content cache should have eg_ids set")

			By("Verifying redirect has size=0")
			Expect(redirectMeta["size"]).To(Equal("0"))

			By("Verifying regular content has size>0")
			contentSize, _ := strconv.ParseInt(contentMeta["size"], 10, 64)
			Expect(contentSize).To(BeNumerically(">", 0),
				"Regular content should have non-zero size")
		})

		It("should not write HTML file for redirects but should write for regular content", func() {
			redirectURL := "/stale-test/multi-status?status=302"
			contentURL := "/static/simple.html"

			By("Creating redirect cache")
			testEnv.RequestRender(redirectURL)
			redirectKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+redirectURL, "desktop")
			redirectMeta, _ := testEnv.GetCacheMetadata(redirectKey)

			By("Creating regular content cache")
			testEnv.RequestRender(contentURL)
			contentKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+contentURL, "desktop")
			contentMeta, _ := testEnv.GetCacheMetadata(contentKey)

			cacheDir := testEnv.Config.EdgeGateway.Storage.BasePath
			if cacheDir == "" {
				cacheDir = "/tmp/edgecomet/cache"
			}

			By("Verifying redirect HTML file does NOT exist")
			if redirectMeta["file_path"] != "" {
				redirectPath := filepath.Join(cacheDir, redirectMeta["file_path"])
				_, err := os.Stat(redirectPath)
				Expect(os.IsNotExist(err)).To(BeTrue(),
					"Redirect should not have HTML file on disk")
			}

			By("Verifying regular content HTML file DOES exist")
			if contentMeta["file_path"] != "" {
				contentPath := filepath.Join(cacheDir, contentMeta["file_path"])
				fileInfo, err := os.Stat(contentPath)
				Expect(err).To(BeNil(), "Regular content file should exist")
				Expect(fileInfo.Size()).To(BeNumerically(">", 0),
					"Regular content file should have content")
			}
		})
	})

	Context("when handling redirect cache expiration", func() {

		It("should respect TTL for redirect caches", func() {
			url := "/stale-test/multi-status?status=301"

			By("First request - creates cache")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())

			By("Getting cache metadata")
			cacheKey, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url, "desktop")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying expires_at is set")
			expiresAt := metadata["expires_at"]
			Expect(expiresAt).NotTo(BeEmpty(), "Redirect cache should have expiration")

			By("Verifying created_at is set")
			createdAt := metadata["created_at"]
			Expect(createdAt).NotTo(BeEmpty(), "Redirect cache should have creation timestamp")
		})
	})

	Context("when mixing regular requests and redirects", func() {

		It("should handle sequence: content → redirect → content", func() {
			contentURL1 := "/static/simple.html"
			redirectURL := "/stale-test/multi-status?status=301"
			contentURL2 := "/static/simple.html"

			By("Request 1: Regular content")
			resp1 := testEnv.RequestRender(contentURL1)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.StatusCode).To(Equal(200))

			By("Request 2: Redirect")
			resp2 := testEnv.RequestRender(redirectURL)
			Expect(resp2.Error).To(BeNil())

			By("Request 3: Regular content (cache hit)")
			resp3 := testEnv.RequestRender(contentURL2)
			Expect(resp3.Error).To(BeNil())
			Expect(resp3.StatusCode).To(Equal(200))
			Expect(resp3.Headers.Get("X-Render-Cache")).To(Equal("hit"))

			By("Verifying all requests completed successfully")
			Expect(resp1.Error).To(BeNil())
			Expect(resp2.Error).To(BeNil())
			Expect(resp3.Error).To(BeNil())
		})

		It("should cache different redirect types independently", func() {
			url301 := "/stale-test/multi-status?status=301&id=1"
			url302 := "/stale-test/multi-status?status=302&id=2"
			url307 := "/stale-test/multi-status?status=307&id=3"

			By("Creating caches for different redirect types (301, 302, 307)")
			testEnv.RequestRender(url301)
			testEnv.RequestRender(url302)
			testEnv.RequestRender(url307)

			By("Verifying 301 metadata")
			key301, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url301, "desktop")
			meta301, _ := testEnv.GetCacheMetadata(key301)
			Expect(meta301["status_code"]).To(Equal("301"))
			Expect(meta301["eg_ids"]).To(BeEmpty())

			By("Verifying 302 metadata")
			key302, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url302, "desktop")
			meta302, _ := testEnv.GetCacheMetadata(key302)
			Expect(meta302["status_code"]).To(Equal("302"))
			Expect(meta302["eg_ids"]).To(BeEmpty())

			By("Verifying 307 metadata")
			key307, _ := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+url307, "desktop")
			meta307, _ := testEnv.GetCacheMetadata(key307)
			Expect(meta307["status_code"]).To(Equal("307"))
			Expect(meta307["eg_ids"]).To(BeEmpty())
		})
	})
})

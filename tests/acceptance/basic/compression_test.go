package acceptance_test

import (
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Cache Compression", Serial, func() {
	Context("with snappy compression enabled (default)", func() {
		It("should compress cached content and serve correctly on cache hit", func() {
			By("Making a render request to a page that generates content > 1024 bytes")
			// Use compression-test page which generates > 2KB of HTML content
			response := testEnv.RequestRender("/compression-test/article")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Compression Test Page"))

			By("Getting cache key for the URL")
			targetURL := testEnv.Config.TestPagesURL() + "/compression-test/article"
			cacheKey, err := testEnv.GetCacheKey(targetURL, "desktop")
			Expect(err).To(BeNil())
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Verifying cache metadata contains compressed file info")
			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			// Check file_path has .snappy extension
			filePath, exists := metadata["file_path"]
			Expect(exists).To(BeTrue(), "file_path should exist in metadata")
			Expect(filePath).To(HaveSuffix(types.ExtSnappy), "File should have .snappy extension")

			By("Verifying file on disk has .snappy extension")
			// Get absolute path
			basePath, absErr := filepath.Abs(testEnv.Config.EdgeGateway.Storage.BasePath)
			Expect(absErr).To(BeNil())
			absoluteFilePath := filepath.Join(basePath, filePath)
			Expect(absoluteFilePath).To(HaveSuffix(types.ExtSnappy))

			// Verify file exists on disk
			_, statErr := os.Stat(absoluteFilePath)
			Expect(statErr).To(BeNil(), "Compressed file should exist on disk")

			By("Verifying disk size is smaller than original size")
			sizeStr, sizeExists := metadata["size"]
			Expect(sizeExists).To(BeTrue())
			originalSize, parseErr := strconv.ParseInt(sizeStr, 10, 64)
			Expect(parseErr).To(BeNil())

			diskSizeStr, diskSizeExists := metadata["disk_size"]
			Expect(diskSizeExists).To(BeTrue())
			diskSize, parseErr := strconv.ParseInt(diskSizeStr, 10, 64)
			Expect(parseErr).To(BeNil())

			// Verify compression ratio (disk size should be smaller for compressible HTML content)
			// Only check if content was actually compressed (> 1024 bytes)
			if originalSize >= types.CompressionMinSize {
				Expect(diskSize).To(BeNumerically("<", originalSize),
					"Compressed disk size should be smaller than original size")
			}

			By("Making a second request (cache hit) and verifying content")
			secondResponse := testEnv.RequestRender("/compression-test/article")

			Expect(secondResponse.Error).To(BeNil())
			Expect(secondResponse.StatusCode).To(Equal(200))

			// Verify cache hit
			Expect(secondResponse.Headers.Get("X-Render-Source")).To(Equal("cache"))

			// Verify content is correctly decompressed
			Expect(secondResponse.Body).To(ContainSubstring("Compression Test Page"))
			Expect(secondResponse.Body).To(ContainSubstring("COMPRESSION_TEST_PAGE"))

			// Verify response body matches original (decompression worked)
			Expect(secondResponse.Body).To(Equal(response.Body),
				"Decompressed content should match original render")
		})

		It("should not compress content below minimum threshold", func() {
			By("Making a request to a small page")
			// Use a page that generates minimal content
			response := testEnv.RequestRender("/static/simple.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Getting cache metadata")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			cacheKey, err := testEnv.GetCacheKey(targetURL, "desktop")
			Expect(err).To(BeNil())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			// Check if content was below threshold
			sizeStr, _ := metadata["size"]
			originalSize, _ := strconv.ParseInt(sizeStr, 10, 64)

			filePath, exists := metadata["file_path"]
			Expect(exists).To(BeTrue())

			// If content is below threshold, file should NOT have compression extension
			if originalSize < types.CompressionMinSize {
				Expect(filePath).NotTo(HaveSuffix(types.ExtSnappy),
					"Small content should not be compressed")
				Expect(filePath).NotTo(HaveSuffix(types.ExtLZ4),
					"Small content should not be compressed")
				Expect(filePath).To(HaveSuffix(".html"),
					"Small content should have plain .html extension")
			}
		})
	})

	Context("with backward compatibility", func() {
		It("should serve uncompressed legacy cache files", func() {
			By("Creating an uncompressed cache file manually")
			// First, render a page to get the cache key and path structure
			// Use compression-test page which generates > 2KB of HTML content
			response := testEnv.RequestRender("/compression-test/legacy")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			// Get the metadata
			targetURL := testEnv.Config.TestPagesURL() + "/compression-test/legacy"
			cacheKey, err := testEnv.GetCacheKey(targetURL, "desktop")
			Expect(err).To(BeNil())

			metadata, err := testEnv.GetCacheMetadata(cacheKey)
			Expect(err).To(BeNil())

			compressedPath := metadata["file_path"]
			Expect(compressedPath).To(HaveSuffix(types.ExtSnappy))

			By("Simulating legacy uncompressed file by creating .html version")
			// Get base path
			basePath, absErr := filepath.Abs(testEnv.Config.EdgeGateway.Storage.BasePath)
			Expect(absErr).To(BeNil())

			// Create the legacy .html path (remove .snappy extension)
			legacyPath := compressedPath[:len(compressedPath)-len(types.ExtSnappy)]
			Expect(legacyPath).To(HaveSuffix(".html"))

			absoluteLegacyPath := filepath.Join(basePath, legacyPath)

			// Write uncompressed content to legacy path
			legacyContent := []byte(response.Body)
			err = os.MkdirAll(filepath.Dir(absoluteLegacyPath), 0o755)
			Expect(err).To(BeNil())
			err = os.WriteFile(absoluteLegacyPath, legacyContent, 0o644)
			Expect(err).To(BeNil())

			// Update metadata to point to legacy uncompressed file
			ctx, cancel := testEnv.RedisClient.Context().Deadline()
			if !cancel {
				// No deadline, create one
			}
			_ = ctx
			metaKey := "meta:" + cacheKey
			err = testEnv.RedisClient.HSet(
				testEnv.RedisClient.Context(),
				metaKey,
				"file_path", legacyPath,
			).Err()
			Expect(err).To(BeNil())

			By("Requesting the page and verifying legacy file is served correctly")
			legacyResponse := testEnv.RequestRender("/compression-test/legacy")

			Expect(legacyResponse.Error).To(BeNil())
			Expect(legacyResponse.StatusCode).To(Equal(200))
			Expect(legacyResponse.Headers.Get("X-Render-Source")).To(Equal("cache"))

			// Content should be served correctly (uncompressed file works)
			Expect(legacyResponse.Body).To(ContainSubstring("Compression Test Page"))
			Expect(legacyResponse.Body).To(ContainSubstring("COMPRESSION_TEST_PAGE"))
		})
	})
})

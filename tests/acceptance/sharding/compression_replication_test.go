package sharding_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/pkg/types"
)

var _ = Describe("Compression Replication", Serial, func() {
	var (
		normalizer *hash.URLNormalizer
	)

	BeforeEach(func() {
		normalizer = hash.NewURLNormalizer()
	})

	Context("Push Flow with Compression", Serial, func() {
		It("should push compressed content to remote EG and serve correctly", func() {
			By("Making a render request via EG1 to generate compressed cache")
			testURL := testEnv.Config.TestPagesURL() + "/compression-test/push-test"
			response1 := testEnv.RequestViaEG1("/compression-test/push-test", "compression-push-test")

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))
			Expect(response1.Body).To(ContainSubstring("Compression Test Page"))
			Expect(response1.Body).To(ContainSubstring("COMPRESSION_TEST_PAGE"))

			By("Computing cache key")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Verifying cache metadata exists and has .snappy extension")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())

			filePath, exists := metadata["file_path"]
			Expect(exists).To(BeTrue(), "file_path should exist in metadata")
			Expect(filePath).To(HaveSuffix(types.ExtSnappy),
				"Compressed file should have .snappy extension")

			By("Verifying eg_ids contains 2 EGs (RF=2, push occurred)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(len(egIDs)).To(Equal(2), "eg_ids should have 2 EGs after push")
			Expect(egIDs).To(ContainElement("eg1"), "eg1 must be in eg_ids")

			By("Determining which EG received the push")
			var pushedToEG int
			for _, id := range egIDs {
				if id != "eg1" {
					switch id {
					case "eg2":
						pushedToEG = 2
					case "eg3":
						pushedToEG = 3
					}
					break
				}
			}
			Expect(pushedToEG).NotTo(Equal(0), "Should have found pushed EG")

			By("Requesting via the EG that received the push (should serve from local cache)")
			var response2 *TestResponse
			switch pushedToEG {
			case 2:
				response2 = testEnv.RequestViaEG2("/compression-test/push-test", "compression-push-verify-eg2")
			case 3:
				response2 = testEnv.RequestViaEG3("/compression-test/push-test", "compression-push-verify-eg3")
			}

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should serve from cache (local file from push)")

			By("Verifying decompressed content matches original")
			Expect(response2.Body).To(Equal(response1.Body),
				"Decompressed content should match original render")
		})

		It("should verify compressed file exists on disk with correct extension", func() {
			By("Making a render request via EG1")
			testURL := testEnv.Config.TestPagesURL() + "/compression-test/disk-verify"
			response := testEnv.RequestViaEG1("/compression-test/disk-verify", "compression-disk-verify")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Getting cache metadata")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())

			filePath := metadata["file_path"]
			Expect(filePath).To(HaveSuffix(types.ExtSnappy))

			By("Verifying file exists on EG1's disk")
			// EG1 storage path from test config (convert to absolute path)
			eg1StoragePath := testEnv.Config.EdgeGateway1.Storage.BasePath
			absEg1StoragePath, _ := filepath.Abs(eg1StoragePath)
			absoluteFilePath := filepath.Join(absEg1StoragePath, filePath)

			_, statErr := os.Stat(absoluteFilePath)
			Expect(statErr).To(BeNil(), "Compressed file should exist on EG1's disk")

			By("Verifying disk file has .snappy extension")
			Expect(absoluteFilePath).To(HaveSuffix(types.ExtSnappy))
		})
	})

	Context("Pull Flow with Compression", Serial, func() {
		It("should pull compressed content from remote EG and store locally", func() {
			By("Making initial request via EG1 (no push, pull-only mode)")
			// Use compression-pull-only path that has push_on_render: false and generates > 1024 bytes
			testURL := testEnv.Config.TestPagesURL() + "/compression-pull-only/pull-test"
			response1 := testEnv.RequestViaEG1("/compression-pull-only/pull-test", "compression-pull-initial")

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Computing cache key")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Verifying eg_ids contains only eg1 (no push)")
			egIDs, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDs).To(Equal([]string{"eg1"}), "eg_ids should only contain eg1")

			By("Verifying file has compression extension")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())
			filePath := metadata["file_path"]
			Expect(filePath).To(HaveSuffix(types.ExtSnappy),
				"File should have .snappy extension")

			By("Making request via EG2 (should pull from EG1)")
			response2 := testEnv.RequestViaEG2("/compression-pull-only/pull-test", "compression-pull-eg2")

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"),
				"Should serve from cache (pulled from EG1)")

			By("Verifying decompressed content matches original")
			Expect(response2.Body).To(Equal(response1.Body),
				"Pulled and decompressed content should match original")

			By("Verifying EG2 added itself to eg_ids (replicate_on_pull=true for pull-only)")
			Eventually(func() []string {
				ids, _ := testEnv.GetEGIDs(cacheKey)
				return ids
			}, "3s", "200ms").Should(HaveLen(2),
				"eg_ids should be updated to contain 2 EGs")

			egIDsAfterPull, err := testEnv.GetEGIDs(cacheKey)
			Expect(err).To(BeNil())
			Expect(egIDsAfterPull).To(ContainElement("eg1"))
			Expect(egIDsAfterPull).To(ContainElement("eg2"))
		})

		It("should store pulled compressed file with correct extension on target EG", func() {
			By("Making initial request via EG1")
			testURL := testEnv.Config.TestPagesURL() + "/compression-pull-only/pull-disk"
			response1 := testEnv.RequestViaEG1("/compression-pull-only/pull-disk", "compression-pull-disk-initial")

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Getting cache metadata")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())
			filePath := metadata["file_path"]

			By("Pulling via EG2")
			response2 := testEnv.RequestViaEG2("/compression-pull-only/pull-disk", "compression-pull-disk-eg2")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Waiting for EG2 to replicate locally")
			Eventually(func() []string {
				ids, _ := testEnv.GetEGIDs(cacheKey)
				return ids
			}, "3s", "200ms").Should(ContainElement("eg2"))

			By("Verifying compressed file exists on EG2's disk")
			// EG2 storage path from test config (convert to absolute path)
			eg2StoragePath := testEnv.Config.EdgeGateway2.Storage.BasePath
			absEg2StoragePath, _ := filepath.Abs(eg2StoragePath)
			absoluteFilePath := filepath.Join(absEg2StoragePath, filePath)

			Eventually(func() error {
				_, err := os.Stat(absoluteFilePath)
				return err
			}, "3s", "200ms").Should(BeNil(),
				"Compressed file should exist on EG2's disk after pull")

			By("Verifying disk file has .snappy extension")
			Expect(absoluteFilePath).To(HaveSuffix(types.ExtSnappy))
		})
	})

	Context("Backward Compatibility", Serial, func() {
		It("should handle mixed compression states in cluster", func() {
			By("Rendering a page via EG1 with compression")
			testURL := testEnv.Config.TestPagesURL() + "/compression-test/backward-compat"
			response1 := testEnv.RequestViaEG1("/compression-test/backward-compat", "compression-backward-initial")

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response1.Body).To(ContainSubstring("Compression Test Page"))

			By("Getting cache metadata")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			By("Waiting for push to complete")
			Eventually(func() int {
				egIDs, _ := testEnv.GetEGIDs(cacheKey)
				return len(egIDs)
			}, "3s", "200ms").Should(Equal(2))

			By("Verifying metadata file_path has compression extension")
			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())
			filePath := metadata["file_path"]
			Expect(filePath).To(HaveSuffix(types.ExtSnappy))

			By("Making request from EG without local cache (EG3 if not in eg_ids)")
			egIDs, _ := testEnv.GetEGIDs(cacheKey)
			egIDMap := make(map[string]bool)
			for _, id := range egIDs {
				egIDMap[id] = true
			}

			var response3 *TestResponse
			if !egIDMap["eg3"] {
				response3 = testEnv.RequestViaEG3("/compression-test/backward-compat", "compression-backward-eg3")
			} else if !egIDMap["eg2"] {
				response3 = testEnv.RequestViaEG2("/compression-test/backward-compat", "compression-backward-eg2")
			} else {
				Skip("All EGs have cache, cannot test pull scenario")
			}

			Expect(response3.Error).To(BeNil())
			Expect(response3.StatusCode).To(Equal(200))
			Expect(response3.Headers.Get("X-Render-Source")).To(Equal("cache"))

			By("Verifying pulled content matches original")
			Expect(response3.Body).To(Equal(response1.Body))
		})
	})

	Context("Compression Metadata Integrity", Serial, func() {
		It("should preserve size and disk_size through replication", func() {
			By("Making initial render request")
			testURL := testEnv.Config.TestPagesURL() + "/compression-test/metadata-test"
			response := testEnv.RequestViaEG1("/compression-test/metadata-test", "compression-metadata-test")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Getting cache metadata")
			result, err := normalizer.Normalize(testURL, nil)
			Expect(err).To(BeNil())
			urlHash := normalizer.Hash(result.NormalizedURL)
			cacheKey := testEnv.BuildCacheKey(1, 1, urlHash)

			metadata, err := testEnv.GetRedisMetadata(cacheKey)
			Expect(err).To(BeNil())

			By("Verifying size and disk_size are present")
			sizeStr, sizeExists := metadata["size"]
			Expect(sizeExists).To(BeTrue(), "size should exist in metadata")

			diskSizeStr, diskSizeExists := metadata["disk_size"]
			Expect(diskSizeExists).To(BeTrue(), "disk_size should exist in metadata")

			By("Verifying disk_size < size (compression worked)")
			var size, diskSize int64
			_, err = parseMetadataInt64(sizeStr, &size)
			Expect(err).To(BeNil())
			_, err = parseMetadataInt64(diskSizeStr, &diskSize)
			Expect(err).To(BeNil())

			// Content should be > 1024 bytes and compressible
			if size >= types.CompressionMinSize {
				Expect(diskSize).To(BeNumerically("<", size),
					"disk_size should be smaller than size for compressed content")
			}

			By("Verifying file_path has compression extension")
			filePath := metadata["file_path"]
			Expect(filePath).To(HaveSuffix(types.ExtSnappy))
		})
	})
})

// parseMetadataInt64 is a helper to parse int64 from metadata string
func parseMetadataInt64(s string, result *int64) (bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, nil
	}

	var n int64
	_, err := parseSimpleInt64(s, &n)
	if err != nil {
		return false, err
	}
	*result = n
	return true, nil
}

// parseSimpleInt64 parses a simple int64 from string
func parseSimpleInt64(s string, result *int64) (bool, error) {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else {
			return false, nil
		}
	}
	*result = n
	return true, nil
}

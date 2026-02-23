package acceptance_test

import (
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Indexation Extraction", Serial, func() {
	// Helper function to get indexation metadata from cache
	getIndexationMetadata := func(url string) (indexStatus int, title string, err error) {
		fullURL := testEnv.Config.TestPagesURL() + url
		cacheKey, err := testEnv.GetCacheKey(fullURL, "desktop")
		if err != nil {
			return 0, "", err
		}

		metadata, err := testEnv.GetCacheMetadata(cacheKey)
		if err != nil {
			return 0, "", err
		}

		title = metadata["title"]

		if indexStatusStr, ok := metadata["index_status"]; ok && indexStatusStr != "" {
			indexStatus, err = strconv.Atoi(indexStatusStr)
			if err != nil {
				return 0, title, err
			}
		}

		return indexStatus, title, nil
	}

	Context("Indexable Pages", func() {
		It("should return IndexStatus 1 and extract title for indexable page", func() {
			url := "/indexation/indexable.html"

			By("Step 1: Render the page")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Step 2: Verify cache metadata has correct indexation data")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(1), "IndexStatus should be 1 (indexable)")
			Expect(title).To(Equal("Indexable Test Page"))
		})

		It("should return IndexStatus 1 when googlebot overrides robots noindex", func() {
			url := "/indexation/googlebot_priority.html"

			By("Step 1: Render the page with googlebot priority")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify googlebot index overrides robots noindex")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(1), "IndexStatus should be 1 (googlebot index overrides robots noindex)")
			Expect(title).To(Equal("Googlebot Priority Test"))
		})

		It("should return IndexStatus 1 for relative canonical matching finalURL", func() {
			url := "/indexation/relative_canonical.html"

			By("Step 1: Render the page with relative canonical")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify relative canonical resolves to match finalURL")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(1), "IndexStatus should be 1 (relative canonical matches after resolution)")
			Expect(title).To(Equal("Relative Canonical Test"))
		})
	})

	Context("Non-Indexable Pages", func() {
		It("should return IndexStatus 3 for page with noindex meta tag", func() {
			url := "/indexation/noindex.html"

			By("Step 1: Render the page with noindex")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify cache metadata shows blocked by meta")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(3), "IndexStatus should be 3 (blocked by meta)")
			Expect(title).To(Equal("Noindex Test Page"))
		})

		It("should return IndexStatus 4 for page with non-canonical URL", func() {
			url := "/indexation/non_canonical.html"

			By("Step 1: Render the page with non-matching canonical")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify cache metadata shows non-canonical")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(4), "IndexStatus should be 4 (non-canonical)")
			Expect(title).To(Equal("Non-Canonical Page"))
		})

		It("should return IndexStatus 2 for 404 response", func() {
			url := "/indexation/not-found.html"

			By("Step 1: Request non-existent page")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(404))

			By("Step 2: Verify cache metadata shows non-200 status")
			indexStatus, _, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(2), "IndexStatus should be 2 (non-200)")
		})

		It("should block when any robots tag has noindex", func() {
			url := "/indexation/multiple_robots.html"

			By("Step 1: Render the page with multiple robots tags")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify any noindex in multiple robots tags blocks")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(3), "IndexStatus should be 3 (blocked by any noindex)")
			Expect(title).To(Equal("Multiple Robots Test"))
		})
	})

	Context("Title Extraction", func() {
		It("should truncate title to 500 characters", func() {
			url := "/indexation/long_title.html"

			By("Step 1: Render the page with long title")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify title is truncated to exactly 500 characters")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(1), "IndexStatus should be 1 (indexable)")
			Expect(len(title)).To(Equal(500), "Title should be truncated to 500 characters")

			// Verify it starts with expected prefix (less fragile than exact match)
			Expect(strings.HasPrefix(title, "This is a very long")).To(BeTrue())
		})

		It("should preserve unicode characters in title", func() {
			url := "/indexation/unicode_title.html"

			By("Step 1: Render the page with unicode title")
			resp := testEnv.RequestRender(url)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Step 2: Verify unicode characters are preserved")
			indexStatus, title, err := getIndexationMetadata(url)
			Expect(err).To(BeNil())
			Expect(indexStatus).To(Equal(1), "IndexStatus should be 1 (indexable)")
			Expect(title).To(Equal("Unicode Test: Привет мир 你好世界"))
		})
	})
})

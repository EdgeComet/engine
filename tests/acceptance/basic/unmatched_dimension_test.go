package acceptance_test

import (
	"io"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Unmatched Dimension Handling", Serial, func() {
	Context("Pattern-Level Override - Block", func() {
		It("should return 403 for unmatched User-Agent", func() {
			By("Making request with unknown User-Agent to /test-unmatched/block/")
			response := makeRequestWithCustomUA("/test-unmatched/block/", "UnknownBot/1.0")

			By("Verifying 403 response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Should return HTTP 403 Forbidden for unmatched UA with block config")

			By("Verifying X-Unmatched-Dimension header is set")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"X-Unmatched-Dimension header should be set to 'true'")
		})
	})

	Context("Pattern-Level Override - Desktop Fallback", func() {
		It("should render with desktop dimension for unmatched User-Agent", func() {
			By("Making request with unknown User-Agent to /test-unmatched/desktop/")
			response := makeRequestWithCustomUA("/test-unmatched/desktop/", "UnknownBot/1.0")

			By("Verifying successful render")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Unmatched-Dimension header is set")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"X-Unmatched-Dimension header should be set to 'true'")

			By("Verifying X-Render-Source indicates rendering occurred")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Content should be rendered")

			By("Verifying rendered content")
			Expect(response.Body).To(ContainSubstring("Desktop Fallback Test Page"),
				"Rendered content should be present")

			By("Verifying cache key uses desktop dimension")
			cacheKey, err := testEnv.GetCacheKey(
				testEnv.Config.TestPagesURL()+"/test-unmatched/desktop/",
				"desktop")
			Expect(err).To(BeNil(), "Should generate cache key successfully")
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue(),
				"Cache entry should exist for desktop dimension")
		})
	})

	Context("Pattern-Level Override - Bypass", func() {
		It("should bypass rendering for unmatched User-Agent", func() {
			By("Making request with unknown User-Agent to /test-unmatched/bypass/")
			response := makeRequestWithCustomUA("/test-unmatched/bypass/", "UnknownBot/1.0")

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Unmatched-Dimension header is set")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"X-Unmatched-Dimension header should be set to 'true'")

			By("Verifying X-Render-Source indicates bypass")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"Content should be bypassed (not rendered)")

			By("Verifying bypass content is served")
			Expect(response.Body).To(ContainSubstring("Bypass Test Page"),
				"Bypass content should be present")
		})
	})

	Context("Matched Dimension - No Fallback", func() {
		It("should NOT set X-Unmatched-Dimension header when User-Agent matches", func() {
			By("Making request with Googlebot User-Agent")
			response := makeRequestWithCustomUA("/test-unmatched/desktop/", "Googlebot/2.1 (+http://www.google.com/bot.html)")

			By("Verifying successful render")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Unmatched-Dimension header is NOT set")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(BeEmpty(),
				"X-Unmatched-Dimension header should NOT be set when dimension matches")

			By("Verifying content was rendered")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Content should be rendered")
		})
	})

	Context("Host Default - Uses Bypass", func() {
		It("should use host default (bypass) when no pattern override", func() {
			By("Making request with unknown User-Agent to /test-unmatched/default/")
			response := makeRequestWithCustomUA("/test-unmatched/default/", "UnknownBot/1.0")

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying X-Unmatched-Dimension header is set")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"X-Unmatched-Dimension header should be set to 'true'")

			By("Verifying X-Render-Source indicates bypass (host default)")
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"Should bypass rendering (host default unmatched_dimension: bypass)")
		})
	})
})

// makeRequestWithCustomUA creates a request with a custom User-Agent header
func makeRequestWithCustomUA(targetPath string, userAgent string) *TestResponse {
	fullTargetURL := testEnv.Config.TestPagesURL() + targetPath
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	req, err := http.NewRequest("GET", testEnv.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", testEnv.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", userAgent)

	start := time.Now()
	resp, err := testEnv.HTTPClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &TestResponse{Error: err, Duration: duration}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Duration:   duration,
			Error:      err,
		}
	}

	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(body),
		Duration:   duration,
	}
}

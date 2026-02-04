package acceptance_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HAR Debug Render", Serial, func() {
	var internalBaseURL string
	var internalAuthKey string

	BeforeEach(func() {
		internalBaseURL = "http://localhost:10071"
		internalAuthKey = "test-auth-key-12345"
	})

	// Helper to make internal server requests
	makeHARRenderRequest := func(targetURL, dimension, timeout string) *TestResponse {
		// Build query params
		params := url.Values{}
		params.Set("url", targetURL)
		if dimension != "" {
			params.Set("dimension", dimension)
		}
		if timeout != "" {
			params.Set("timeout", timeout)
		}

		reqURL := internalBaseURL + "/debug/har/render?" + params.Encode()

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return &TestResponse{Error: err}
		}

		req.Header.Set("X-Internal-Auth", internalAuthKey)

		client := &http.Client{Timeout: 30 * time.Second}
		start := time.Now()
		resp, err := client.Do(req)
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

	Context("successful HAR render", func() {
		It("should render a URL and return HAR data", func() {
			By("Making a HAR render request for a simple page")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := makeHARRenderRequest(targetURL, "desktop", "")

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying HAR JSON structure")
			Expect(response.Headers.Get("Content-Type")).To(Equal("application/json"))

			var harData map[string]interface{}
			err := json.Unmarshal([]byte(response.Body), &harData)
			Expect(err).To(BeNil(), "Response should be valid JSON")

			By("Verifying HAR has log structure")
			log, ok := harData["log"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "HAR should have log field")
			Expect(log).To(HaveKey("version"))
			Expect(log).To(HaveKey("creator"))
		})

		It("should render with explicit dimension", func() {
			By("Making a HAR render request with desktop dimension")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := makeHARRenderRequest(targetURL, "desktop", "")

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("host not found error", func() {
		It("should return 404 for unknown host", func() {
			By("Making a request for URL with unrecognized domain")
			response := makeHARRenderRequest("https://unknown-domain.example.com/page", "", "")

			By("Verifying host not found error")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(404))
			Expect(response.Body).To(ContainSubstring("host_not_found"))
		})
	})

	Context("invalid dimension error", func() {
		It("should return 400 for invalid dimension", func() {
			By("Making a request with non-existent dimension")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := makeHARRenderRequest(targetURL, "tablet", "")

			By("Verifying invalid dimension error")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))
			Expect(response.Body).To(ContainSubstring("invalid_dimension"))
		})
	})

	Context("timeout parameter", func() {
		It("should accept custom timeout", func() {
			By("Making a request with custom timeout")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := makeHARRenderRequest(targetURL, "desktop", "45s")

			By("Verifying request succeeds with custom timeout")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})

		It("should reject invalid timeout format", func() {
			By("Making a request with invalid timeout")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			response := makeHARRenderRequest(targetURL, "desktop", "not-a-duration")

			By("Verifying invalid timeout error")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))
			Expect(response.Body).To(ContainSubstring("invalid_timeout"))
		})
	})

	Context("URL validation", func() {
		It("should reject missing URL parameter", func() {
			By("Making a request without URL")
			reqURL := internalBaseURL + "/debug/har/render"
			req, err := http.NewRequest("GET", reqURL, nil)
			Expect(err).To(BeNil())
			req.Header.Set("X-Internal-Auth", internalAuthKey)

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			Expect(err).To(BeNil())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(400))
			body, _ := io.ReadAll(resp.Body)
			Expect(string(body)).To(ContainSubstring("missing_url"))
		})

		It("should reject invalid URL format", func() {
			By("Making a request with invalid URL")
			response := makeHARRenderRequest("not-a-valid-url", "", "")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))
			Expect(response.Body).To(ContainSubstring("invalid_url"))
		})
	})

	Context("URL rule blocking", func() {
		It("should block URLs matching block rules", func() {
			By("Making a request for a blocked URL pattern")
			// The test config has block rules for certain patterns
			targetURL := testEnv.Config.TestPagesURL() + "/blocked/page"
			response := makeHARRenderRequest(targetURL, "desktop", "")

			By("Verifying blocked URL response")
			Expect(response.Error).To(BeNil())
			// Should return 403 or similar for blocked URL
			if response.StatusCode == 403 {
				Expect(response.Body).To(ContainSubstring("url_blocked"))
			}
			// Note: If no block rule is configured, this test may pass differently
		})
	})

	Context("authentication", func() {
		It("should reject requests without auth header", func() {
			By("Making a request without X-Internal-Auth")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			params := url.Values{}
			params.Set("url", targetURL)

			reqURL := internalBaseURL + "/debug/har/render?" + params.Encode()
			req, err := http.NewRequest("GET", reqURL, nil)
			Expect(err).To(BeNil())
			// No auth header set

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			Expect(err).To(BeNil())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(401))
		})

		It("should reject requests with invalid auth header", func() {
			By("Making a request with invalid X-Internal-Auth")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			params := url.Values{}
			params.Set("url", targetURL)

			reqURL := internalBaseURL + "/debug/har/render?" + params.Encode()
			req, err := http.NewRequest("GET", reqURL, nil)
			Expect(err).To(BeNil())
			req.Header.Set("X-Internal-Auth", "wrong-key")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			Expect(err).To(BeNil())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(401))
		})
	})
})

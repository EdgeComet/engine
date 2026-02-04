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

var _ = Describe("Console Messages Capture", Serial, func() {
	var internalBaseURL string
	var internalAuthKey string

	BeforeEach(func() {
		internalBaseURL = "http://localhost:10071"
		internalAuthKey = "test-auth-key-12345"
	})

	// HARMetadata mirrors the HAR metadata structure for assertions
	type HARMetadata struct {
		ConsoleErrors []string `json:"consoleErrors"`
	}

	// HARLog contains the log structure
	type HARLog struct {
		Version string `json:"version"`
	}

	// HARResponse is the full HAR JSON structure
	type HARResponse struct {
		Log      HARLog       `json:"log"`
		Metadata *HARMetadata `json:"_metadata"`
	}

	// Helper to make HAR render request
	makeHARRenderRequest := func(targetURL string) (*HARResponse, int, error) {
		params := url.Values{}
		params.Set("url", targetURL)
		params.Set("dimension", "desktop")

		reqURL := internalBaseURL + "/debug/har/render?" + params.Encode()

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, 0, err
		}

		req.Header.Set("X-Internal-Auth", internalAuthKey)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, resp.StatusCode, nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, err
		}

		var harData HARResponse
		if err := json.Unmarshal(body, &harData); err != nil {
			return nil, resp.StatusCode, err
		}

		return &harData, resp.StatusCode, nil
	}

	Context("when page generates console errors and warnings", func() {
		It("should capture console.error messages", func() {
			By("Making a HAR render request to console-messages test page")
			targetURL := testEnv.Config.TestPagesURL() + "/console-messages.html"
			harData, statusCode, err := makeHARRenderRequest(targetURL)

			By("Verifying successful response")
			Expect(err).To(BeNil())
			Expect(statusCode).To(Equal(200))
			Expect(harData).NotTo(BeNil())

			By("Verifying HAR metadata contains console errors")
			Expect(harData.Metadata).NotTo(BeNil(), "HAR metadata should be present")
			Expect(harData.Metadata.ConsoleErrors).NotTo(BeEmpty(), "Console errors should be captured")

			By("Verifying captured error messages")
			consoleErrors := harData.Metadata.ConsoleErrors

			// Look for the inline script error message
			foundInlineError := false
			for _, msg := range consoleErrors {
				if msg == "Test error message from inline script" {
					foundInlineError = true
					break
				}
			}
			Expect(foundInlineError).To(BeTrue(), "Should capture inline script error: got %v", consoleErrors)

			// Look for the thrown error message
			foundThrownError := false
			for _, msg := range consoleErrors {
				if msg == "Intentional error for testing" {
					foundThrownError = true
					break
				}
			}
			Expect(foundThrownError).To(BeTrue(), "Should capture thrown error: got %v", consoleErrors)
		})

		It("should capture console.warn messages", func() {
			By("Making a HAR render request to console-messages test page")
			targetURL := testEnv.Config.TestPagesURL() + "/console-messages.html"
			harData, statusCode, err := makeHARRenderRequest(targetURL)

			By("Verifying successful response")
			Expect(err).To(BeNil())
			Expect(statusCode).To(Equal(200))
			Expect(harData).NotTo(BeNil())

			By("Verifying HAR metadata contains warning messages")
			Expect(harData.Metadata).NotTo(BeNil())
			Expect(harData.Metadata.ConsoleErrors).NotTo(BeEmpty())

			// Look for the warning message
			foundWarning := false
			for _, msg := range harData.Metadata.ConsoleErrors {
				if msg == "Test warning message from inline script" {
					foundWarning = true
					break
				}
			}
			Expect(foundWarning).To(BeTrue(), "Should capture warning message: got %v", harData.Metadata.ConsoleErrors)
		})

		It("should capture multiple console messages", func() {
			By("Making a HAR render request to console-messages test page")
			targetURL := testEnv.Config.TestPagesURL() + "/console-messages.html"
			harData, statusCode, err := makeHARRenderRequest(targetURL)

			By("Verifying successful response")
			Expect(err).To(BeNil())
			Expect(statusCode).To(Equal(200))
			Expect(harData).NotTo(BeNil())

			By("Verifying multiple messages are captured")
			Expect(harData.Metadata).NotTo(BeNil())
			// The page generates 5 console messages: 2 errors, 2 warnings, 1 thrown error
			Expect(len(harData.Metadata.ConsoleErrors)).To(BeNumerically(">=", 5),
				"Should capture at least 5 console messages, got %d: %v",
				len(harData.Metadata.ConsoleErrors), harData.Metadata.ConsoleErrors)
		})

		It("should join multiple arguments with space separator", func() {
			By("Making a HAR render request to console-messages test page")
			targetURL := testEnv.Config.TestPagesURL() + "/console-messages.html"
			harData, statusCode, err := makeHARRenderRequest(targetURL)

			By("Verifying successful response")
			Expect(err).To(BeNil())
			Expect(statusCode).To(Equal(200))
			Expect(harData).NotTo(BeNil())

			By("Verifying multi-argument error is joined")
			Expect(harData.Metadata).NotTo(BeNil())
			consoleErrors := harData.Metadata.ConsoleErrors

			// Look for the multi-argument error message joined with space
			foundMultiArgError := false
			for _, msg := range consoleErrors {
				if msg == "Error: code 42 occurred" {
					foundMultiArgError = true
					break
				}
			}
			Expect(foundMultiArgError).To(BeTrue(),
				"Multi-argument console.error should be joined with space separator: got %v", consoleErrors)

			// Look for the multi-argument warning message joined with space
			foundMultiArgWarning := false
			for _, msg := range consoleErrors {
				if msg == "Warning: in module auth" {
					foundMultiArgWarning = true
					break
				}
			}
			Expect(foundMultiArgWarning).To(BeTrue(),
				"Multi-argument console.warn should be joined with space separator: got %v", consoleErrors)
		})
	})

	Context("when page has no console errors", func() {
		It("should have empty console errors in metadata", func() {
			By("Making a HAR render request to a simple page without errors")
			targetURL := testEnv.Config.TestPagesURL() + "/static/simple.html"
			harData, statusCode, err := makeHARRenderRequest(targetURL)

			By("Verifying successful response")
			Expect(err).To(BeNil())
			Expect(statusCode).To(Equal(200))
			Expect(harData).NotTo(BeNil())

			By("Verifying HAR metadata exists but console errors may be empty or nil")
			Expect(harData.Metadata).NotTo(BeNil())
			// A page without errors should have empty or nil console errors
			if harData.Metadata.ConsoleErrors != nil {
				Expect(harData.Metadata.ConsoleErrors).To(BeEmpty(),
					"Page without errors should have empty console errors")
			}
		})
	})
})

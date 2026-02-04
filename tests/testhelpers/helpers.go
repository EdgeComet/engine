package testhelpers

import (
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/gomega"
)

// TestResponse represents the response from a render request
type TestResponse struct {
	StatusCode int
	Headers    http.Header
	Body       string
	Duration   time.Duration
	Error      error
}

// RequestOptions contains optional parameters for render requests
type RequestOptions struct {
	APIKey  *string
	Timeout *time.Duration
	Headers map[string]string
}

// Helper functions for common test patterns
// Note: Functions that depend on testEnv are removed since testEnv is in acceptance_test package

// ExpectNoError checks that the response has no network errors
func ExpectNoError(response *TestResponse) {
	Expect(response).NotTo(BeNil(), "Response should not be nil")
	Expect(response.Error).To(BeNil(), "Request should not have network errors")
}

// ExpectHTMLContent verifies that response contains expected HTML content
func ExpectHTMLContent(response *TestResponse, expectedContent ...string) {
	Expect(response.Body).NotTo(BeEmpty(), "Response body should not be empty")
	for _, content := range expectedContent {
		Expect(response.Body).To(ContainSubstring(content),
			"Response should contain: %s", content)
	}
}

// ExpectNotHTMLContent verifies that response does not contain specific content
func ExpectNotHTMLContent(response *TestResponse, unexpectedContent ...string) {
	for _, content := range unexpectedContent {
		Expect(response.Body).NotTo(ContainSubstring(content),
			"Response should not contain: %s", content)
	}
}

// ExpectMetaTag verifies that a meta tag with specific name and content exists
func ExpectMetaTag(response *TestResponse, name, content string) {
	Expect(response.Body).To(ContainSubstring(`<meta name="`+name+`"`),
		"Should have meta tag: %s", name)
	if content != "" {
		Expect(response.Body).To(ContainSubstring(`content="`+content),
			"Meta tag %s should have content: %s", name, content)
	}
}

// ExpectOpenGraphTag verifies that an Open Graph meta tag exists
func ExpectOpenGraphTag(response *TestResponse, property, content string) {
	Expect(response.Body).To(ContainSubstring(`<meta property="og:`+property+`"`),
		"Should have OG property: %s", property)
	if content != "" {
		Expect(response.Body).To(ContainSubstring(`content="`+content),
			"OG property %s should have content: %s", property, content)
	}
}

// ExpectTwitterCardTag verifies that a Twitter Card meta tag exists
func ExpectTwitterCardTag(response *TestResponse, name, content string) {
	Expect(response.Body).To(ContainSubstring(`<meta name="twitter:`+name+`"`),
		"Should have Twitter Card: %s", name)
	if content != "" {
		Expect(response.Body).To(ContainSubstring(`content="`+content),
			"Twitter Card %s should have content: %s", name, content)
	}
}

// ExpectStructuredData verifies that JSON-LD structured data exists
func ExpectStructuredData(response *TestResponse, schemaType string) {
	Expect(response.Body).To(ContainSubstring(`<script type="application/ld+json">`),
		"Should have JSON-LD structured data")
	Expect(response.Body).To(ContainSubstring(`"@type": "`+schemaType+`"`),
		"Should have schema type: %s", schemaType)
}

// ExpectJavaScriptRendered verifies that JavaScript has executed and generated content
func ExpectJavaScriptRendered(response *TestResponse, expectedContent string, loadingText string) {
	// Verify loading state is gone
	if loadingText != "" {
		ExpectNotHTMLContent(response, loadingText)
	}

	// Verify rendered content is present
	ExpectHTMLContent(response, expectedContent)
}

// ExpectCacheStatus verifies the cache status header
func ExpectCacheStatus(response *TestResponse, expectedStatus string) {
	cacheStatus := response.Headers.Get("X-Cache-Status")
	if cacheStatus != "" {
		Expect(strings.ToUpper(cacheStatus)).To(Equal(strings.ToUpper(expectedStatus)),
			"Cache status should be: %s", expectedStatus)
	}
}

// ExpectResponseTime verifies that response time is within acceptable limits
func ExpectResponseTime(response *TestResponse, maxDuration time.Duration) {
	Expect(response.Duration).To(BeNumerically("<=", maxDuration),
		"Response time should be under %v, got %v", maxDuration, response.Duration)
}

// ExpectErrorResponse verifies that the response is an error with specific status code
func ExpectErrorResponse(response *TestResponse, statusCode int) {
	ExpectNoError(response)
	Expect(response.StatusCode).To(Equal(statusCode),
		"Expected status code %d, got %d", statusCode, response.StatusCode)
}

// ExpectAuthenticationError verifies authentication/authorization failure
func ExpectAuthenticationError(response *TestResponse) {
	ExpectNoError(response)
	Expect(response.StatusCode).To(BeElementOf([]int{401, 403}),
		"Expected authentication error (401 or 403), got %d", response.StatusCode)

	if response.Body != "" {
		body := strings.ToLower(response.Body)
		Expect(body).To(Or(
			ContainSubstring("unauthorized"),
			ContainSubstring("forbidden"),
			ContainSubstring("invalid"),
			ContainSubstring("denied"),
		), "Error message should indicate authentication failure")
	}
}

// ExpectServerError verifies that a server error occurred
func ExpectServerError(response *TestResponse) {
	ExpectNoError(response)
	Expect(response.StatusCode).To(BeNumerically(">=", 500),
		"Expected server error (5xx), got %d", response.StatusCode)
}

// ExpectClientError verifies that a client error occurred
func ExpectClientError(response *TestResponse) {
	ExpectNoError(response)
	Expect(response.StatusCode).To(BeNumerically(">=", 400),
		"Expected client error (4xx), got %d", response.StatusCode)
	Expect(response.StatusCode).To(BeNumerically("<", 500),
		"Expected client error (4xx), got %d", response.StatusCode)
}

// CountSuccessfulResponses counts responses with 200 status code
func CountSuccessfulResponses(responses []*TestResponse) int {
	count := 0
	for _, response := range responses {
		if response != nil && response.Error == nil && response.StatusCode == 200 {
			count++
		}
	}
	return count
}

// ExpectMinSuccessRate verifies that at least a certain percentage of requests succeeded
func ExpectMinSuccessRate(responses []*TestResponse, minRate float64) {
	total := len(responses)
	successful := CountSuccessfulResponses(responses)
	actualRate := float64(successful) / float64(total)

	Expect(actualRate).To(BeNumerically(">=", minRate),
		"Expected at least %.0f%% success rate, got %.0f%% (%d/%d)",
		minRate*100, actualRate*100, successful, total)
}

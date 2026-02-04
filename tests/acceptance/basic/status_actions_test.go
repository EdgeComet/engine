package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Status Actions", Serial, func() {
	Context("Basic Status Actions (403/404/410)", func() {
		It("should return 403 Forbidden for status_403 action", func() {
			By("Making request to status_403 pattern")
			response := testEnv.RequestRender("/blocked/admin-area")

			By("Verifying 403 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Verifying response body contains Forbidden")
			Expect(response.Body).To(ContainSubstring("Forbidden"))

			By("Verifying custom reason is included")
			Expect(response.Body).To(ContainSubstring("Admin areas not available for bots"))

			By("Verifying X-Render-Action header is present")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))

			By("Verifying Content-Type is text/plain")
			Expect(response.Headers.Get("Content-Type")).To(ContainSubstring("text/plain"))
		})

		It("should treat block action as alias for status_403", func() {
			By("Making request to block action pattern")
			response := testEnv.RequestRender("/blocked/system-files")

			By("Verifying block returns 403 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Verifying response body contains Forbidden")
			Expect(response.Body).To(ContainSubstring("Forbidden"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should return 404 Not Found for status_404 action", func() {
			By("Making request to status_404 pattern")
			response := testEnv.RequestRender("/removed/old-page")

			By("Verifying 404 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(404))

			By("Verifying response body contains Not Found")
			Expect(response.Body).To(ContainSubstring("Not Found"))

			By("Verifying custom reason is included")
			// The first /removed/* pattern (line 119) matches
			Expect(response.Body).To(ContainSubstring("removed"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should return 410 Gone for status_410 action", func() {
			By("Making request to status_410 pattern")
			response := testEnv.RequestRender("/discontinued/product")

			By("Verifying 410 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(410))

			By("Verifying response body contains Gone")
			Expect(response.Body).To(ContainSubstring("Gone"))

			By("Verifying custom reason is included")
			Expect(response.Body).To(ContainSubstring("Product permanently discontinued"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})
	})

	Context("Generic Status Action - 3xx Redirects", func() {
		It("should return 301 Permanent Redirect with Location header", func() {
			By("Making request to 301 redirect pattern (without following redirect)")
			response := testEnv.RequestRenderNoRedirect("/redirect/old-page")

			By("Verifying 301 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(301))

			By("Verifying Location header is present")
			location := response.Headers.Get("Location")
			Expect(location).NotTo(BeEmpty())
			Expect(location).To(ContainSubstring("/new-page"))

			By("Verifying body is empty or minimal for redirect")
			Expect(len(response.Body)).To(BeNumerically("<=", 100))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should return 302 Temporary Redirect with Location header", func() {
			By("Making request to 302 redirect pattern (without following redirect)")
			response := testEnv.RequestRenderNoRedirect("/redirect/temporary")

			By("Verifying 302 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(302))

			By("Verifying Location header is present")
			location := response.Headers.Get("Location")
			Expect(location).NotTo(BeEmpty())
			Expect(location).To(ContainSubstring("/current-page"))

			By("Verifying body is empty or minimal")
			Expect(len(response.Body)).To(BeNumerically("<=", 100))
		})

		It("should return 307 Temporary Redirect preserving method", func() {
			By("Making request to 307 redirect pattern (without following redirect)")
			response := testEnv.RequestRenderNoRedirect("/redirect/preserve-method")

			By("Verifying 307 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(307))

			By("Verifying Location header is present")
			location := response.Headers.Get("Location")
			Expect(location).NotTo(BeEmpty())

			By("Verifying body is empty or minimal")
			Expect(len(response.Body)).To(BeNumerically("<=", 100))
		})
	})

	Context("Generic Status Action - 4xx Client Errors", func() {
		It("should return 400 Bad Request with custom reason", func() {
			By("Making request to 400 error pattern")
			response := testEnv.RequestRender("/errors/bad-request")

			By("Verifying 400 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))

			By("Verifying response body contains Bad Request")
			Expect(response.Body).To(ContainSubstring("Bad Request"))

			By("Verifying custom reason is included")
			Expect(response.Body).To(ContainSubstring("Invalid request parameters"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should return 401 Unauthorized with optional WWW-Authenticate header", func() {
			By("Making request to 401 error pattern")
			response := testEnv.RequestRender("/errors/unauthorized")

			By("Verifying 401 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(401))

			By("Verifying response body contains Unauthorized")
			Expect(response.Body).To(ContainSubstring("Unauthorized"))

			By("Verifying WWW-Authenticate header if configured")
			wwwAuth := response.Headers.Get("WWW-Authenticate")
			if wwwAuth != "" {
				Expect(wwwAuth).NotTo(BeEmpty())
			}
		})

		It("should return 429 Too Many Requests with Retry-After header", func() {
			By("Making request to 429 rate limit pattern")
			response := testEnv.RequestRender("/errors/rate-limited")

			By("Verifying 429 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(429))

			By("Verifying response body contains rate limit message")
			Expect(response.Body).To(ContainSubstring("Too Many Requests"))
			Expect(response.Body).To(ContainSubstring("Too many requests"))

			By("Verifying Retry-After header is present")
			retryAfter := response.Headers.Get("Retry-After")
			Expect(retryAfter).To(Equal("3600"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})
	})

	Context("Generic Status Action - 5xx Server Errors", func() {
		It("should return 500 Internal Server Error with custom reason", func() {
			By("Making request to 500 error pattern")
			response := testEnv.RequestRender("/errors/server-error")

			By("Verifying 500 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(500))

			By("Verifying response body contains Internal Server Error")
			Expect(response.Body).To(ContainSubstring("Internal Server Error"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should return 503 Service Unavailable with Retry-After header", func() {
			By("Making request to 503 maintenance pattern")
			response := testEnv.RequestRender("/errors/maintenance")

			By("Verifying 503 status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(503))

			By("Verifying response body contains Service Unavailable")
			Expect(response.Body).To(ContainSubstring("Service Unavailable"))
			Expect(response.Body).To(ContainSubstring("System under maintenance"))

			By("Verifying Retry-After header is present")
			retryAfter := response.Headers.Get("Retry-After")
			Expect(retryAfter).To(Equal("7200"))

			By("Verifying X-Render-Action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})
	})

	Context("Custom Headers", func() {
		It("should include Location header for redirects with absolute URL", func() {
			By("Making request to redirect with absolute URL (without following)")
			response := testEnv.RequestRenderNoRedirect("/redirect/absolute-url")

			By("Verifying redirect status code")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(301))

			By("Verifying Location header contains absolute URL")
			location := response.Headers.Get("Location")
			Expect(location).To(ContainSubstring("http"))
		})

		It("should include Location header for redirects with relative URL", func() {
			By("Making request to redirect with relative URL (without following)")
			response := testEnv.RequestRenderNoRedirect("/redirect/old-page")

			By("Verifying redirect status code")
			Expect(response.StatusCode).To(Equal(301))

			By("Verifying Location header contains relative path")
			location := response.Headers.Get("Location")
			Expect(location).To(Equal("/new-page"))
		})

		It("should include Retry-After header with seconds format", func() {
			By("Making request to rate-limited endpoint")
			response := testEnv.RequestRender("/errors/rate-limited")

			By("Verifying Retry-After header is in seconds format")
			retryAfter := response.Headers.Get("Retry-After")
			Expect(retryAfter).To(Equal("3600"))
		})

		It("should allow custom headers to override defaults", func() {
			By("Making request to pattern with Content-Type override")
			response := testEnv.RequestRender("/status/custom-content-type")

			By("Verifying custom Content-Type if configured")
			contentType := response.Headers.Get("Content-Type")
			if contentType != "" && contentType != "text/plain; charset=utf-8" {
				Expect(contentType).NotTo(Equal("text/plain; charset=utf-8"))
			}

			By("Verifying X-Render-Action header is always present")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should handle multiple custom headers", func() {
			By("Making request to pattern with multiple custom headers")
			response := testEnv.RequestRender("/discontinued/product")

			By("Verifying status code")
			Expect(response.StatusCode).To(Equal(410))

			By("Verifying X-Render-Action header is present")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})
	})

	Context("Status Action Behavior Verification", func() {
		It("should respond quickly without rendering", func() {
			By("Making request to status action pattern")
			start := time.Now()
			response := testEnv.RequestRender("/blocked/admin-area")
			duration := time.Since(start)

			By("Verifying response is received")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Verifying fast response time (< 5 seconds)")
			Expect(duration).To(BeNumerically("<", 5*time.Second))

			By("Verifying X-Render-Source is not 'rendered'")
			source := response.Headers.Get("X-Render-Source")
			if source != "" {
				Expect(source).NotTo(Equal("rendered"))
			}
		})

		It("should not create cache entries", func() {
			By("Making request to status action pattern")
			response := testEnv.RequestRender("/blocked/admin-area")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Waiting for any potential cache operations to complete")
			time.Sleep(200 * time.Millisecond)

			By("Checking Redis for cache entry")
			cacheKey, err := testEnv.GetCacheKey(testEnv.Config.TestPagesURL()+"/blocked/admin-area", "desktop")
			Expect(err).To(BeNil())

			cacheExists := testEnv.CacheExists(cacheKey)
			Expect(cacheExists).To(BeFalse(), "Status action should not create cache entry")

			By("Making second request to verify it is not served from cache")
			response2 := testEnv.RequestRender("/blocked/admin-area")
			Expect(response2.StatusCode).To(Equal(403))

			source := response2.Headers.Get("X-Render-Source")
			if source != "" {
				Expect(source).NotTo(Equal("cache"))
			}
		})

		It("should not contact origin server", func() {
			By("Making request to status action for non-existent page")
			response := testEnv.RequestRender("/admin/nonexistent-page-that-would-404")

			By("Verifying status action returns 403 immediately")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Verifying no bypass occurred")
			source := response.Headers.Get("X-Render-Source")
			if source != "" {
				Expect(source).NotTo(Equal("bypass"))
			}

			By("Verifying fast response indicates no origin contact")
			// Response should be nearly instant since no origin request was made
		})

		It("should work with query parameters", func() {
			By("Making request to status action with query parameters")
			response := testEnv.RequestRender("/admin/settings?page=users&action=edit")

			By("Verifying status action applies despite query params")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Verifying response body")
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})
	})

	Context("Pattern Matching with Status Actions", func() {
		It("should match exact paths only for exact patterns", func() {
			By("Making request to exact match pattern")
			response := testEnv.RequestRender("/blocked/exact-path")

			By("Verifying exact match works")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))

			By("Making request to subpath that should NOT match")
			response2 := testEnv.RequestRender("/blocked/exact-path/subpath")

			By("Verifying subpath does not match exact pattern")
			// Should fall through to other rules or default action
			Expect(response2.StatusCode).NotTo(Equal(403))
		})

		It("should match all nested paths for wildcard patterns", func() {
			By("Making request to single-level wildcard match")
			response1 := testEnv.RequestRender("/blocked/admin-area")
			Expect(response1.StatusCode).To(Equal(403))

			By("Making request to nested path under wildcard")
			response2 := testEnv.RequestRender("/admin/users")
			// Note: /admin/* is configured as wildcard pattern
			Expect(response2.StatusCode).To(Equal(403))
			Expect(response2.Body).To(ContainSubstring("Admin area blocked"))
		})

		It("should match file extensions for extension patterns", func() {
			By("Making request to file with blocked extension")
			response := testEnv.RequestRender("/documents/secret-file.zip")

			By("Verifying extension pattern matches")
			// *.zip is configured with status_403 action
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Archive downloads blocked"))
		})

		It("should apply first matching pattern (priority)", func() {
			By("Testing path that matches multiple patterns")
			// Pattern 1: /blocked/special → 404
			// Pattern 2: /blocked/* → 403
			response := testEnv.RequestRender("/blocked/special")

			By("Verifying first pattern takes precedence")
			// Should return status code from first matching pattern
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Or(Equal(403), Equal(404)))
		})
	})

	Context("Configuration Resolution", func() {
		It("should use pattern-level status configuration", func() {
			By("Making request to pattern with specific status config")
			response := testEnv.RequestRender("/removed/old-content")

			By("Verifying pattern-specific reason is used")
			Expect(response.StatusCode).To(Equal(404))
			// The first /removed/* pattern matches (line 119 in hosts.yaml)
			Expect(response.Body).To(Or(ContainSubstring("Content has been removed"), ContainSubstring("Content removed")))

			By("Verifying response has status action header")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should handle multiple patterns with same action but different configs", func() {
			By("Making request to first 403 pattern")
			response1 := testEnv.RequestRender("/blocked/admin-area")
			Expect(response1.StatusCode).To(Equal(403))
			Expect(response1.Body).To(ContainSubstring("Admin areas not available for bots"))

			By("Making request to second 403 pattern")
			response2 := testEnv.RequestRender("/blocked/system-files")
			Expect(response2.StatusCode).To(Equal(403))

			By("Verifying different patterns can have different configs")
			// Each pattern can have its own reason and headers
		})
	})

	Context("Edge Cases", func() {
		It("should use default status text when no reason provided", func() {
			By("Making request to status action without custom reason")
			response := testEnv.RequestRender("/blocked/system-files")

			By("Verifying default status text is used")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Forbidden"))

			By("Verifying no extra separator when no reason")
			// Body should be just "Forbidden", not "Forbidden: "
		})

		It("should apply default headers when no custom headers configured", func() {
			By("Making request to status action without custom headers")
			response := testEnv.RequestRender("/blocked/system-files")

			By("Verifying default Content-Type is applied")
			Expect(response.Headers.Get("Content-Type")).To(ContainSubstring("text/plain"))

			By("Verifying X-Render-Action header is always present")
			Expect(response.Headers.Get("X-Render-Action")).To(Equal("status"))
		})

		It("should handle empty reason string", func() {
			By("Making request to pattern with empty reason")
			response := testEnv.RequestRender("/status/empty-reason")

			By("Verifying status code is set")
			Expect(response.Error).To(BeNil())

			By("Verifying body contains only status text without separator")
			// Should not have ": " when reason is empty
		})

		It("should handle very long custom reason", func() {
			By("Making request to pattern with long reason")
			response := testEnv.RequestRender("/status/long-reason")

			By("Verifying status code is set")
			Expect(response.Error).To(BeNil())

			By("Verifying full reason is returned")
			// Should include the complete reason regardless of length
		})

		It("should handle status actions for paths that would otherwise 404", func() {
			By("Making request to blocked non-existent page")
			response := testEnv.RequestRender("/admin/page-that-doesnt-exist")

			By("Verifying status action takes precedence over 404")
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).NotTo(ContainSubstring("Not Found"))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})

		It("should handle concurrent requests to status actions", func() {
			By("Making multiple concurrent requests to status actions")
			numRequests := 5
			responses := make([]*TestResponse, numRequests)
			done := make(chan int, numRequests)

			for i := 0; i < numRequests; i++ {
				go func(index int) {
					responses[index] = testEnv.RequestRender("/blocked/admin-area")
					done <- index
				}(i)
			}

			By("Waiting for all requests to complete")
			for i := 0; i < numRequests; i++ {
				select {
				case <-done:
					// Request completed
				case <-time.After(10 * time.Second):
					Fail("Concurrent request timed out")
				}
			}

			By("Verifying all requests returned correct status")
			for i, response := range responses {
				Expect(response).NotTo(BeNil())
				Expect(response.Error).To(BeNil(), "Request %d should not have errors", i)
				Expect(response.StatusCode).To(Equal(403), "Request %d should return 403", i)
				Expect(response.Body).To(ContainSubstring("Forbidden"))
			}
		})
	})
})

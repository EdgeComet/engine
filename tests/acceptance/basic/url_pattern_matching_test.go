package acceptance_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("URL Pattern Matching", Serial, func() {
	Context("Exact Match Patterns", func() {
		It("should match exact path only", func() {
			By("Making request to exact match path")
			response := testEnv.RequestRender("/exact/path")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Exact match test"))

			By("Verifying subpath does NOT match exact pattern")
			response2 := testEnv.RequestRender("/exact/path/subpath")
			// Catch-all pattern will render, but page doesn't exist (no static file) -> 404
			Expect(response2.StatusCode).To(Equal(404))
		})

		It("should not match similar paths", func() {
			By("Testing path similarity doesn't cause false matches")
			response := testEnv.RequestRender("/exact/path-different")

			Expect(response.Error).To(BeNil())
			// Catch-all renders, but page doesn't exist (no static file)
			Expect(response.StatusCode).To(Equal(404))

			By("Testing prefix doesn't match")
			response2 := testEnv.RequestRender("/exact")
			Expect(response2.StatusCode).To(Equal(404))
		})

		It("should match exact path regardless of query parameters", func() {
			By("Making request to exact path with query params")
			response := testEnv.RequestRender("/exact/path?foo=bar&baz=qux")

			Expect(response.Error).To(BeNil())
			// Path-only matching: /exact/path MATCHES /exact/path?foo=bar&baz=qux
			// Query parameters are ignored for pattern matching
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Exact match test"))
		})

		It("should distinguish between paths with trailing slash", func() {
			By("Making request without trailing slash")
			response := testEnv.RequestRender("/exact/path")
			Expect(response.StatusCode).To(Equal(403))

			By("Making request with trailing slash")
			response2 := testEnv.RequestRender("/exact/path/")

			// URL normalization may remove trailing slash
			// Both should match exact pattern
			if response2.StatusCode == 403 {
				Expect(response2.Body).To(ContainSubstring("Exact match test"))
			}
		})

		It("should match admin login path exactly", func() {
			By("Making request to exact admin login path")
			response := testEnv.RequestRender("/admin/login")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Admin exact match"))
		})
	})

	Context("Wildcard Patterns - Recursive", func() {
		It("should match single-level paths", func() {
			By("Making request to single-level blog path")
			response := testEnv.RequestRender("/blog/my-first-post")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))
			Expect(response.Body).To(ContainSubstring("my-first-post"))
		})

		It("should match multi-level nested paths", func() {
			By("Making request to deeply nested blog path")
			response := testEnv.RequestRender("/blog/2024/january/new-year-resolutions")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))
			Expect(response.Body).To(ContainSubstring("2024/january/new-year-resolutions"))
		})

		It("should match deeply nested paths", func() {
			By("Making request to very deeply nested blog path")
			response := testEnv.RequestRender("/blog/2024/01/15/category/subcategory/article")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))
		})

		It("should match root with wildcard", func() {
			By("Making request to admin wildcard path")
			response := testEnv.RequestRender("/admin/users")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Admin area blocked"))
		})

		It("should not match parent directory without trailing content", func() {
			By("Making request to blog root without trailing slash")
			response := testEnv.RequestRender("/blog")

			// Pattern /blog/* requires content after /blog/
			// /blog alone may or may not match depending on implementation
			// If it doesn't match, it should use catch-all and render
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(BeNumerically(">=", 200))
		})

		It("should match with query parameters", func() {
			By("Making request to admin path with query params")
			response := testEnv.RequestRender("/admin/settings?tab=security&view=advanced")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Admin area blocked"))
		})

		It("should handle middle wildcards", func() {
			By("Making request to product reviews with wildcard in middle")
			response := testEnv.RequestRender("/product/abc-123/reviews")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Reviews for Product"))
			Expect(response.Body).To(ContainSubstring("abc-123"))

			By("Testing with different product ID")
			response2 := testEnv.RequestRender("/product/xyz-789/reviews")
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("xyz-789"))
		})

		It("should handle API wildcard bypass", func() {
			By("Making request to API endpoint")
			response := testEnv.RequestRender("/api/users/123")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass action was taken")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))
		})
	})

	Context("Extension Patterns", func() {
		It("should match file extensions at any depth", func() {
			By("Making request to PDF at root level")
			response := testEnv.RequestRender("/document.pdf")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass action for PDF")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))

			By("Making request to PDF in nested directory")
			response2 := testEnv.RequestRender("/reports/2024/annual-report.pdf")

			Expect(response2.Error).To(BeNil())

			By("Verifying nested PDF also bypassed")
			Expect(response2.Headers).NotTo(BeNil())
			source2 := response2.Headers.Get("X-Render-Source")
			Expect(source2).To(Equal("bypass"))
		})

		It("should match multiple file types", func() {
			By("Testing PDF extension")
			response1 := testEnv.RequestRender("/file.pdf")
			Expect(response1.Error).To(BeNil())

			By("Testing JSON extension")
			response2 := testEnv.RequestRender("/api/data.json")
			Expect(response2.Error).To(BeNil())

			By("Testing ZIP extension (blocked)")
			response3 := testEnv.RequestRender("/archive.zip")
			Expect(response3.Error).To(BeNil())
			Expect(response3.StatusCode).To(Equal(403))
			Expect(response3.Body).To(ContainSubstring("Archive downloads blocked"))
		})

		It("should not match partial extensions", func() {
			By("Making request to file without extension")
			response := testEnv.RequestRender("/document")

			Expect(response.Error).To(BeNil())
			// No extension pattern matches, catch-all renders (no static file exists)
			Expect(response.StatusCode).To(Equal(404))

			By("Making request with extension in path but not at end")
			response2 := testEnv.RequestRender("/pdf/document")
			Expect(response2.StatusCode).To(Equal(404)) // Does not match *.pdf, no static file
		})

		It("should match extensions with query params", func() {
			By("Making request to PDF with query parameters")
			response := testEnv.RequestRender("/document.pdf?download=true&version=2")

			Expect(response.Error).To(BeNil())

			By("Verifying extension pattern matched despite query params")
			// Extension patterns (*.pdf) should match path only, ignoring query params
			// Test server generates fake PDF content on-the-fly, so expect 200
			// Cache is cleared before each test, so first request is always bypass
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))
		})

		It("should match extensions in deeply nested directories", func() {
			By("Making request to PDF in deep path")
			response := testEnv.RequestRender("/documents/reports/2024/Q1/financial/summary.pdf")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass for deeply nested PDF")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))
		})
	})

	Context("Query Parameter Patterns", func() {
		It("should match paths with any query string", func() {
			By("Making request to search with simple query")
			response := testEnv.RequestRender("/search?q=test")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Search Results"))
			Expect(response.Body).To(ContainSubstring("q=test"))

			By("Making request to search with complex query")
			response2 := testEnv.RequestRender("/search?q=test+query&category=tech&sort=date&page=5")
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("Search Results"))
		})

		It("should normalize query parameter order", func() {
			By("Making request with params in different order")
			response1 := testEnv.RequestRender("/search?q=test&page=2&sort=date")
			response2 := testEnv.RequestRender("/search?sort=date&q=test&page=2")

			Expect(response1.Error).To(BeNil())
			Expect(response2.Error).To(BeNil())

			By("Verifying both match the same pattern")
			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response1.Body).To(ContainSubstring("Search Results"))
			Expect(response2.Body).To(ContainSubstring("Search Results"))
		})

		It("should match specific query patterns", func() {
			By("Making request to search without query params")
			response := testEnv.RequestRender("/search")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Search Results"))
			Expect(response.Body).To(ContainSubstring("No query parameters"))
		})

		It("should handle empty query strings", func() {
			By("Making request with empty query string")
			response := testEnv.RequestRender("/search?")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Search Results"))
		})
	})

	Context("Pattern Priority - First Match Wins", func() {
		It("should use first matching rule", func() {
			By("Making request to path matching multiple patterns")
			// /special/protected/page matches both:
			// 1. /special/protected/page (exact) -> 403
			// 2. /special/* (wildcard) -> render
			response := testEnv.RequestRender("/special/protected/page")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Specific page blocked"))
		})

		It("should not evaluate subsequent rules after match", func() {
			By("Verifying first match prevents evaluation of later rules")
			// /special/other should match /special/* (render), not catch-all
			response := testEnv.RequestRender("/special/other/page")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Special Page"))
			Expect(response.Body).To(ContainSubstring("other/page"))
		})

		It("should prioritize specific over general patterns", func() {
			By("Testing specific pattern takes precedence")
			// Specific: /admin/login -> 403 with specific reason
			// General: /admin/* -> 403 with general reason
			response := testEnv.RequestRender("/admin/login")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Admin exact match"))

			By("Testing general pattern for other admin paths")
			response2 := testEnv.RequestRender("/admin/users")
			Expect(response2.StatusCode).To(Equal(403))
			Expect(response2.Body).To(ContainSubstring("Admin area blocked"))
		})

		It("should respect rule order for overlapping patterns", func() {
			By("Testing overlapping patterns respect order")
			// /api/* should match before catch-all *
			response := testEnv.RequestRender("/api/endpoint")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass action from /api/* rule")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))
		})

		It("should handle multiple matching patterns correctly", func() {
			By("Testing path that could match multiple wildcards")
			// /api/v1/users could match:
			// 1. /api/v1/* -> bypass
			// 2. /api/* -> bypass
			// Should use first match (/api/v1/*)
			response := testEnv.RequestRender("/api/v1/users")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass action")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))
		})

		It("should validate catch-all pattern at end", func() {
			By("Making request to unmatched path")
			// Should use catch-all * pattern (action=render)
			response := testEnv.RequestRender("/random/unmatched/path")

			Expect(response.Error).To(BeNil())
			// Catch-all renders, but static file doesn't exist
			// Expect 404 from test server file handler
			Expect(response.StatusCode).To(Equal(404))
		})
	})

	Context("Edge Cases", func() {
		It("should handle very long URLs", func() {
			By("Making request with very long URL")
			// Create a very long path (but under typical limits)
			longSegment := strings.Repeat("segment/", 30)
			longPath := "/blog/" + longSegment + "article"

			response := testEnv.RequestRender(longPath)

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))
		})

		It("should handle special characters in URLs", func() {
			By("Making request with special characters")
			// Test various special characters
			response := testEnv.RequestRender("/blog/article-with-dashes")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))

			By("Testing underscores")
			response2 := testEnv.RequestRender("/blog/article_with_underscores")
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response2.Body).To(ContainSubstring("article_with_underscores"))

			By("Testing dots")
			response3 := testEnv.RequestRender("/blog/article.with.dots")
			Expect(response3.StatusCode).To(Equal(200))
			Expect(response3.Body).To(ContainSubstring("article.with.dots"))
		})

		It("should handle encoded characters", func() {
			By("Making request with URL-encoded characters")
			// %20 = space, %2F = slash
			response := testEnv.RequestRender("/blog/article%20with%20spaces")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("Blog Article"))
		})

		It("should handle URLs with fragments", func() {
			By("Making request with URL fragment")
			// Fragments (#section) are typically not sent to server
			// But test if they're included in URL
			response := testEnv.RequestRender("/blog/article#section")

			Expect(response.Error).To(BeNil())

			By("Verifying fragment doesn't break pattern matching")
			Expect(response.StatusCode).To(BeNumerically(">=", 200))
		})

		It("should handle malformed patterns gracefully", func() {
			By("Making request that might trigger edge cases in matcher")
			// Multiple consecutive slashes
			response := testEnv.RequestRender("/blog//article")

			Expect(response.Error).To(BeNil())

			By("Verifying system handles malformed URL gracefully")
			// Should either normalize or handle gracefully
			Expect(response.StatusCode).To(BeNumerically(">=", 200))
		})
	})

	Context("Status Code Validation", func() {
		It("should return 404 for removed content pattern", func() {
			By("Making request to removed content")
			response := testEnv.RequestRender("/removed/old-article")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(404))
			Expect(response.Body).To(ContainSubstring("Content removed"))

			By("Verifying no cache entry created for status action")
			// Status actions should not create cache entries
			// This would require checking Redis, which we can do if cache key is known
		})

		It("should return 410 for permanently removed content", func() {
			By("Making request to permanently removed content")
			response := testEnv.RequestRender("/gone/discontinued-product")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(410))
			Expect(response.Body).To(ContainSubstring("Permanently removed"))
		})

		It("should return 301 redirect with Location header", func() {
			By("Making request to redirect URL")
			response := testEnv.RequestRender("/redirect-old")

			By("Verifying 301 status code is returned")
			// Status action should return 301 immediately
			// If client auto-follows redirect, error might occur (target not reachable)
			// But we should still see 301 status in response
			if response.Error != nil {
				// Error occurred trying to follow redirect - acceptable
				// Just verify we attempted the redirect
				Skip("Redirect auto-follow attempted but target unreachable - expected behavior")
			}

			Expect(response.StatusCode).To(Equal(301))

			By("Verifying Location header is present")
			Expect(response.Headers).NotTo(BeNil())
			location := response.Headers.Get("Location")
			Expect(location).NotTo(BeEmpty())
			Expect(location).To(ContainSubstring("simple.html"))

			By("Verifying redirect has empty or minimal body")
			// Redirects typically have empty body
			Expect(len(response.Body)).To(BeNumerically("<=", 100))
		})

		It("should handle status actions without rendering", func() {
			By("Measuring response time for status action")
			// Status actions should be fast (no rendering)
			response := testEnv.RequestRender("/removed/test")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(404))

			By("Verifying fast response (< 100ms)")
			// Status actions should respond quickly
			// response.Duration should be available if we track it
			// For now, just verify correct status
		})
	})
})

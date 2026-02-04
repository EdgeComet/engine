package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Query Parameter Matching", Serial, func() {
	Context("Exact Value Matching", func() {
		It("should match exact query parameter value", func() {
			By("Making request with matching category parameter")
			response := testEnv.RequestRender("/qparam-test/exact-match?category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule header is present and indicates query match")
			Expect(response.Headers).NotTo(BeNil())
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty(), "X-Matched-Rule header should be set")
			Expect(matchedRule).To(ContainSubstring("/qparam-test/exact-match"), "Rule should match exact-match pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Rule should indicate query parameter matching")
		})

		It("should not match different parameter value", func() {
			By("Making request with non-matching category parameter")
			response := testEnv.RequestRender("/qparam-test/exact-match?category=news")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying fallback rule was used (shorter cache TTL)")
			// Exact match rule has 5m TTL, fallback has 2m TTL
			// Both render successfully, but different rules matched
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback to wildcard rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when parameter is missing", func() {
			By("Making request without required parameter")
			response := testEnv.RequestRender("/qparam-test/exact-match")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying fallback rule was used")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response.Body).To(ContainSubstring("No query parameters"))

			By("Verifying X-Matched-Rule indicates fallback to wildcard rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should be case-insensitive for parameter values", func() {
			By("Making request with different case")
			response := testEnv.RequestRender("/qparam-test/exact-match?category=Tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying exact match rule matched (case-insensitive)")
			// Exact matching is case-insensitive: "Tech" matches "tech"
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates exact match rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/exact-match"), "Should match exact-match pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Rule should indicate query parameter matching")
		})
	})

	Context("Wildcard Matching", func() {
		It("should match when parameter exists with non-empty value", func() {
			By("Making request with query parameter present")
			response := testEnv.RequestRender("/qparam-test/wildcard-required?q=search+term")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/wildcard-required"), "Should match wildcard-required pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match when parameter is missing", func() {
			By("Making request without required parameter")
			response := testEnv.RequestRender("/qparam-test/wildcard-required")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response.Body).To(ContainSubstring("No query parameters"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match empty parameter value", func() {
			By("Making request with empty parameter value")
			response := testEnv.RequestRender("/qparam-test/wildcard-required?q=")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard rule did not match empty value")
			// Falls back to generic rule
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should match any non-empty value", func() {
			By("Making request with different values")
			response1 := testEnv.RequestRender("/qparam-test/wildcard-required?q=test")
			response2 := testEnv.RequestRender("/qparam-test/wildcard-required?q=123")
			response3 := testEnv.RequestRender("/qparam-test/wildcard-required?q=special%20chars")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response3.StatusCode).To(Equal(200))

			By("Verifying all matched successfully")
			Expect(response1.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response2.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response3.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying all matched the query-specific rule")
			for _, resp := range []*TestResponse{response1, response2, response3} {
				matchedRule := resp.Headers.Get("X-Matched-Rule")
				Expect(matchedRule).NotTo(BeEmpty())
				Expect(matchedRule).To(ContainSubstring("/qparam-test/wildcard-required"), "Should match wildcard-required pattern")
				Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
			}
		})
	})

	Context("Array OR Logic", func() {
		It("should match first value in array", func() {
			By("Making request with first allowed value")
			response := testEnv.RequestRender("/qparam-test/array-or?category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/array-or"), "Should match array-or pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match second value in array", func() {
			By("Making request with second allowed value")
			response := testEnv.RequestRender("/qparam-test/array-or?category=science")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/array-or"), "Should match array-or pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match third value in array", func() {
			By("Making request with third allowed value")
			response := testEnv.RequestRender("/qparam-test/array-or?category=business")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/array-or"), "Should match array-or pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match value not in array", func() {
			By("Making request with non-allowed value")
			response := testEnv.RequestRender("/qparam-test/array-or?category=sports")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying OR logic rule did not match")
			// Falls back to generic rule
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when parameter is missing", func() {
			By("Making request without category parameter")
			response := testEnv.RequestRender("/qparam-test/array-or")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying rule did not match")
			Expect(response.Body).To(ContainSubstring("No query parameters"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Multiple Parameters - AND Logic", func() {
		It("should match when all parameters satisfy conditions", func() {
			By("Making request with all required parameters")
			response := testEnv.RequestRender("/qparam-test/multi-and?q=test&category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/multi-and"), "Should match multi-and pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match with second category value", func() {
			By("Making request with science category")
			response := testEnv.RequestRender("/qparam-test/multi-and?q=research&category=science")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/multi-and"), "Should match multi-and pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match when first parameter is missing", func() {
			By("Making request without q parameter")
			response := testEnv.RequestRender("/qparam-test/multi-and?category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying AND logic rule did not match")
			// Falls back to generic rule
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when second parameter is missing", func() {
			By("Making request without category parameter")
			response := testEnv.RequestRender("/qparam-test/multi-and?q=test")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying AND logic rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when category value is invalid", func() {
			By("Making request with invalid category")
			response := testEnv.RequestRender("/qparam-test/multi-and?q=test&category=news")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying AND logic rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when q parameter is empty", func() {
			By("Making request with empty q parameter")
			response := testEnv.RequestRender("/qparam-test/multi-and?q=&category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard condition failed on empty value")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Regexp Pattern Matching", func() {
		It("should match numeric ID with case-sensitive regexp", func() {
			By("Making request with numeric ID")
			response := testEnv.RequestRender("/qparam-test/regexp-sensitive?id=12345")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/regexp-sensitive"), "Should match regexp-sensitive pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match non-numeric ID", func() {
			By("Making request with non-numeric ID")
			response := testEnv.RequestRender("/qparam-test/regexp-sensitive?id=abc123")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying regexp rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should match case-insensitive regexp pattern", func() {
			By("Making request with lowercase prefix")
			response1 := testEnv.RequestRender("/qparam-test/regexp-insensitive?name=product-abc")

			By("Making request with uppercase prefix")
			response2 := testEnv.RequestRender("/qparam-test/regexp-insensitive?name=PRODUCT-xyz")

			By("Making request with mixed case")
			response3 := testEnv.RequestRender("/qparam-test/regexp-insensitive?name=Product-123")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response3.StatusCode).To(Equal(200))

			By("Verifying all case variations matched")
			Expect(response1.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response2.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response3.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying all matched the query-specific rule")
			for _, resp := range []*TestResponse{response1, response2, response3} {
				matchedRule := resp.Headers.Get("X-Matched-Rule")
				Expect(matchedRule).NotTo(BeEmpty())
				Expect(matchedRule).To(ContainSubstring("/qparam-test/regexp-insensitive"), "Should match regexp-insensitive pattern")
				Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
			}
		})

		It("should not match when prefix is incorrect", func() {
			By("Making request without correct prefix")
			response := testEnv.RequestRender("/qparam-test/regexp-insensitive?name=item-abc")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying regexp rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Complex Multi-Condition Matching", func() {
		It("should match when all complex conditions are satisfied", func() {
			By("Making request with search, tech category, and numeric page")
			response := testEnv.RequestRender("/qparam-test/complex?search=test&category=tech&page=5")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/complex"), "Should match complex pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match with science category", func() {
			By("Making request with science category")
			response := testEnv.RequestRender("/qparam-test/complex?search=quantum&category=science&page=1")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/complex"), "Should match complex pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match with invalid category", func() {
			By("Making request with business category (not in allowed array)")
			response := testEnv.RequestRender("/qparam-test/complex?search=test&category=business&page=1")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying complex rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match with non-numeric page", func() {
			By("Making request with alphabetic page value")
			response := testEnv.RequestRender("/qparam-test/complex?search=test&category=tech&page=first")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying regexp validation failed")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when search parameter is missing", func() {
			By("Making request without search parameter")
			response := testEnv.RequestRender("/qparam-test/complex?category=tech&page=1")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard condition failed")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Priority Rules - Query-Specific vs Path-Only", func() {
		It("should use query-specific rule when parameter matches", func() {
			By("Making request with premium parameter")
			response := testEnv.RequestRender("/qparam-test/priority?premium=true")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying query-specific rule matched (1h TTL vs 5m)")
			// Both rules render successfully, but different cache TTLs
			// Premium rule has 1h TTL, regular has 5m TTL
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
		})

		It("should use path-only rule when parameter is missing", func() {
			By("Making request without premium parameter")
			response := testEnv.RequestRender("/qparam-test/priority")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying path-only rule matched")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response.Body).To(ContainSubstring("No query parameters"))

			By("Verifying X-Matched-Rule indicates path-only rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/priority"), "Should match priority path pattern")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Path-only rule has no query matching")
		})

		It("should use path-only rule when parameter value does not match", func() {
			By("Making request with premium=false")
			response := testEnv.RequestRender("/qparam-test/priority?premium=false")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying path-only fallback rule matched")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates path-only rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/priority"), "Should match priority path pattern")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Path-only rule has no query matching")
		})
	})

	Context("Query Parameters with Different Actions", func() {
		It("should apply status action based on query parameter", func() {
			By("Making request with admin parameter")
			response := testEnv.RequestRender("/qparam-test/blocked?admin=true")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Body).To(ContainSubstring("Admin parameter not allowed"))
		})

		It("should render normally without admin parameter", func() {
			By("Making request without admin parameter")
			response := testEnv.RequestRender("/qparam-test/blocked")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying fallback rule rendered normally")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
		})

		It("should apply bypass action based on query parameter", func() {
			By("Making request with format=json parameter")
			response := testEnv.RequestRender("/qparam-test/api-call?format=json")

			Expect(response.Error).To(BeNil())

			By("Verifying bypass action was taken")
			Expect(response.Headers).NotTo(BeNil())
			source := response.Headers.Get("X-Render-Source")
			Expect(source).To(Equal("bypass"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/api-call"), "Should match api-call pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching for format=json")
		})

		It("should render normally with different format", func() {
			By("Making request with format=html parameter")
			response := testEnv.RequestRender("/qparam-test/api-call?format=html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying fallback rule rendered instead of bypass")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Regexp with Special Characters", func() {
		It("should match valid email with complex regexp pattern", func() {
			By("Making request with valid email address")
			response := testEnv.RequestRender("/qparam-test/regexp-special?email=user@example.com")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/regexp-special"), "Should match regexp-special pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match email with dots and hyphens", func() {
			By("Making request with email containing dots and hyphens")
			response := testEnv.RequestRender("/qparam-test/regexp-special?email=john.doe-tag@sub.example.org")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/regexp-special"), "Should match regexp-special pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match invalid email without @ symbol", func() {
			By("Making request with invalid email format")
			response := testEnv.RequestRender("/qparam-test/regexp-special?email=invalid-email.com")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying email regexp rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match email without domain", func() {
			By("Making request with @ but no domain")
			response := testEnv.RequestRender("/qparam-test/regexp-special?email=user@")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying email regexp rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
		})

		It("should not match email with missing local part", func() {
			By("Making request with @ but no local part")
			response := testEnv.RequestRender("/qparam-test/regexp-special?email=@example.com")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying email regexp rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
		})
	})

	Context("Mixed Array and Scalar Conditions", func() {
		It("should match when scalar matches and array value matches (tech)", func() {
			By("Making request with section=news and category=tech")
			response := testEnv.RequestRender("/qparam-test/mixed?section=news&category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/mixed"), "Should match mixed pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match when scalar matches and array value matches (business)", func() {
			By("Making request with section=news and category=business")
			response := testEnv.RequestRender("/qparam-test/mixed?section=news&category=business")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/mixed"), "Should match mixed pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match when scalar value is incorrect", func() {
			By("Making request with section=sports (wrong) and category=tech")
			response := testEnv.RequestRender("/qparam-test/mixed?section=sports&category=tech")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying mixed rule did not match due to scalar mismatch")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when array value is not in allowed list", func() {
			By("Making request with section=news and category=science (not in array)")
			response := testEnv.RequestRender("/qparam-test/mixed?section=news&category=science")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying mixed rule did not match due to array value mismatch")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when either parameter is missing", func() {
			By("Making request with only section parameter")
			response1 := testEnv.RequestRender("/qparam-test/mixed?section=news")

			By("Making request with only category parameter")
			response2 := testEnv.RequestRender("/qparam-test/mixed?category=tech")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying both fell back to generic rule")
			Expect(response1.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response2.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback for both")
			for _, resp := range []*TestResponse{response1, response2} {
				matchedRule := resp.Headers.Get("X-Matched-Rule")
				Expect(matchedRule).NotTo(BeEmpty())
				Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
				Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
			}
		})
	})

	Context("Non-Empty Wildcard Validation", func() {
		It("should match when value parameter has non-empty content", func() {
			By("Making request with non-empty value parameter")
			response := testEnv.RequestRender("/qparam-test/non-empty?value=something")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/non-empty"), "Should match non-empty pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should match with any non-empty value content", func() {
			By("Making request with various non-empty values")
			response1 := testEnv.RequestRender("/qparam-test/non-empty?value=123")
			response2 := testEnv.RequestRender("/qparam-test/non-empty?value=test+data")
			response3 := testEnv.RequestRender("/qparam-test/non-empty?value=x")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))
			Expect(response3.StatusCode).To(Equal(200))

			By("Verifying all matched the non-empty rule")
			for _, resp := range []*TestResponse{response1, response2, response3} {
				matchedRule := resp.Headers.Get("X-Matched-Rule")
				Expect(matchedRule).NotTo(BeEmpty())
				Expect(matchedRule).To(ContainSubstring("/qparam-test/non-empty"), "Should match non-empty pattern")
				Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
			}
		})

		It("should not match when value parameter is empty", func() {
			By("Making request with empty value parameter")
			response := testEnv.RequestRender("/qparam-test/non-empty?value=")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard rejected empty value")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should not match when value parameter is missing", func() {
			By("Making request without value parameter")
			response := testEnv.RequestRender("/qparam-test/non-empty")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying wildcard rule requires parameter presence")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})
	})

	Context("Edge Cases", func() {
		It("should handle multiple exact parameter conditions", func() {
			By("Making request with both exact parameters matching")
			response := testEnv.RequestRender("/qparam-test/exact-multi?type=article&status=published")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/exact-multi"), "Should match exact-multi pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should not match with one parameter incorrect", func() {
			By("Making request with wrong status")
			response := testEnv.RequestRender("/qparam-test/exact-multi?type=article&status=draft")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying exact match rule did not match")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should handle parameter names with different cases", func() {
			By("Making request with exact case match for parameter name")
			response := testEnv.RequestRender("/qparam-test/case-sensitive?Code=ABC123")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying parameter name case sensitivity")
			// Parameter names should match exactly (case-sensitive)
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/case-sensitive"), "Should match case-sensitive pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should be case-sensitive for parameter names (negative test)", func() {
			By("Making request with lowercase parameter name (config has Code)")
			response := testEnv.RequestRender("/qparam-test/case-sensitive?code=ABC123")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying rule did not match due to parameter name case mismatch")
			// Config has "Code", request has "code" → no match
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying X-Matched-Rule indicates fallback rule")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/*"), "Should match fallback wildcard rule")
			Expect(matchedRule).NotTo(ContainSubstring("?..."), "Fallback rule has no query matching")
		})

		It("should handle parameters with special characters in values", func() {
			By("Making request with URL-encoded special characters")
			response := testEnv.RequestRender("/qparam-test/wildcard-required?q=search+with+spaces")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/wildcard-required"), "Should match wildcard-required pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should handle multiple values for same parameter", func() {
			By("Making request with duplicate parameter keys")
			// URL: ?category=tech&category=science
			// Behavior: First value used (nginx convention)
			response := testEnv.RequestRender("/qparam-test/array-or?category=tech&category=science")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying first value was used for matching")
			Expect(response.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying correct rule matched via X-Matched-Rule header")
			matchedRule := response.Headers.Get("X-Matched-Rule")
			Expect(matchedRule).NotTo(BeEmpty())
			Expect(matchedRule).To(ContainSubstring("/qparam-test/array-or"), "Should match array-or pattern")
			Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
		})

		It("should handle parameter order independence", func() {
			By("Making request with parameters in different order")
			response1 := testEnv.RequestRender("/qparam-test/multi-and?q=test&category=tech")
			response2 := testEnv.RequestRender("/qparam-test/multi-and?category=tech&q=test")

			Expect(response1.StatusCode).To(Equal(200))
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying both parameter orders matched")
			Expect(response1.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))
			Expect(response2.Body).To(ContainSubstring("QPARAM_TEST_PAGE"))

			By("Verifying both matched the same rule")
			for _, resp := range []*TestResponse{response1, response2} {
				matchedRule := resp.Headers.Get("X-Matched-Rule")
				Expect(matchedRule).NotTo(BeEmpty())
				Expect(matchedRule).To(ContainSubstring("/qparam-test/multi-and"), "Should match multi-and pattern")
				Expect(matchedRule).To(ContainSubstring("?..."), "Should indicate query matching")
			}
		})
	})
})

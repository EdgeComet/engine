package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The X-Processed-URL response header used to expose the normalized URL, and these
// tests asserted stripping by inspecting it. That header was removed, so stripping is
// now verified through cache-key behavior instead: tracking parameters are stripped
// during URL normalization, which feeds the cache key. Two URLs that normalize to the
// same key share a cache entry, so a second request served from cache (EC-Source:
// render_cache) proves the differing parameters were stripped; a fresh render
// (EC-Source: render) proves they were preserved. The suite clears the cache before
// every test (suite_test.go BeforeEach), so each It starts from an empty cache.

var _ = Describe("Tracking Parameter Stripping", Serial, func() {

	// SCENARIO 1: Basic Stripping (Global/built-in defaults at host level)
	Context("Basic Stripping - Global Defaults", func() {

		It("should strip utm_source and preserve other parameters", func() {
			By("stripping utm_source yields the same cache key as the canonical URL")
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google&product=123",
				"/tracking-params/?product=123")

			By("changing the preserved product param yields a distinct cache key")
			expectDistinctCacheKeys(
				"/tracking-params/?product=alpha",
				"/tracking-params/?product=beta")
		})

		It("should strip utm_source when it's the only parameter", func() {
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google",
				"/tracking-params/")
		})

		It("should strip multiple UTM parameters", func() {
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google&utm_medium=cpc&utm_campaign=spring&product=123",
				"/tracking-params/?product=123")
		})

		It("should preserve non-tracking parameters unchanged", func() {
			expectDistinctCacheKeys(
				"/tracking-params/?product=123&category=tech&page=5",
				"/tracking-params/?product=123&category=tech&page=6")
		})
	})

	// SCENARIO 2: Custom Parameters (host params_add: custom_ref, custom_source)
	Context("Custom Parameters - Host Level", func() {

		It("should strip custom_ref parameter from host config", func() {
			expectSharedCacheKey(
				"/tracking-params/?custom_ref=twitter&product=123",
				"/tracking-params/?product=123")
		})

		It("should strip both built-in and custom parameters (global + host merge)", func() {
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google&custom_ref=twitter&custom_source=email&product=123",
				"/tracking-params/?product=123")
		})
	})

	// SCENARIO 3: Pattern Override - params replaces the list (/special/* strips only special_only)
	Context("Pattern Override - params replaces all", func() {

		It("should strip only the special_only parameter under the override", func() {
			expectSharedCacheKey(
				"/tracking-params/special/page?special_only=xyz&product=123",
				"/tracking-params/special/page?product=123")
		})

		It("should preserve utm_source under the override (replace mode drops built-in defaults)", func() {
			expectDistinctCacheKeys(
				"/tracking-params/special/page?utm_source=google&product=123",
				"/tracking-params/special/page?product=123")
		})
	})

	// SCENARIO 4: Disabled Stripping (/disabled/* strip: false). The path still caches,
	// so a distinct cache key genuinely proves nothing was stripped.
	Context("Disabled Stripping - strip: false", func() {

		It("should not strip any parameters when stripping is disabled", func() {
			expectDistinctCacheKeys(
				"/tracking-params/disabled/page?utm_source=google&product=123",
				"/tracking-params/disabled/page?product=123")
		})
	})

	// SCENARIO 5: Wildcard Patterns (/wildcard-test/* adds utm_*, ga_*, fb_*)
	Context("Wildcard Patterns", func() {

		It("should strip all utm_* parameters with wildcard", func() {
			expectSharedCacheKey(
				"/tracking-params/wildcard-test/page?utm_source=x&utm_medium=y&utm_campaign=z&utm_term=a&product=123",
				"/tracking-params/wildcard-test/page?product=123")
		})

		It("should strip all ga_* parameters with wildcard", func() {
			expectSharedCacheKey(
				"/tracking-params/wildcard-test/page?ga_session=abc&ga_client=def&product=123",
				"/tracking-params/wildcard-test/page?product=123")
		})

		It("should strip mixed wildcard patterns", func() {
			expectSharedCacheKey(
				"/tracking-params/wildcard-test/page?utm_source=x&ga_session=y&fb_source=z&product=123",
				"/tracking-params/wildcard-test/page?product=123")
		})
	})

	// SCENARIO 6: Three-Level Merge. On the wildcard-test path the strip list is the union of
	// global built-in defaults (utm_source), host params_add (custom_ref) and pattern params_add
	// (ga_*), so all three must be stripped to share the canonical cache key.
	Context("Three-Level Configuration Merge", func() {

		It("should strip parameters contributed by global, host, and pattern levels", func() {
			expectSharedCacheKey(
				"/tracking-params/wildcard-test/page?utm_source=builtin&custom_ref=host&ga_session=pattern&product=123",
				"/tracking-params/wildcard-test/page?product=123")
		})
	})

	// SCENARIO 7: Case Insensitivity (built-in exact matches are case-insensitive)
	Context("Case Insensitivity", func() {

		It("should strip UTM_SOURCE (uppercase) matching utm_source", func() {
			expectSharedCacheKey(
				"/tracking-params/?UTM_SOURCE=google&product=123",
				"/tracking-params/?product=123")
		})

		It("should strip Utm_Medium (mixed case) matching utm_medium", func() {
			expectSharedCacheKey(
				"/tracking-params/?Utm_Medium=cpc&product=123",
				"/tracking-params/?product=123")
		})
	})

	// SCENARIO 8: All Built-in Defaults
	Context("All Built-in Default Parameters", func() {

		It("should strip all built-in tracking parameters", func() {
			full := "/tracking-params/?utm_source=x&utm_content=y&utm_medium=z&utm_campaign=a&utm_term=b" +
				"&gclid=c&fbclid=d&msclkid=e&_ga=f&_gl=g&mc_cid=h&mc_eid=i&_ke=j&ref=k&referrer=l&product=m"
			expectSharedCacheKey(full, "/tracking-params/?product=m")
		})

		It("should strip tracking params while preserving interleaved product params", func() {
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google&product=123&category=tech&utm_medium=cpc&sort=desc&utm_campaign=spring",
				"/tracking-params/?product=123&category=tech&sort=desc")
		})
	})

	// SCENARIO 9: Cache Consistency across different tracking-param flavors
	Context("Cache Consistency", func() {

		It("should use the same cache entry regardless of which tracking params are present", func() {
			base := "/tracking-params/cache-test/page?product=xyz"

			By("Request 1: with utm_source renders fresh")
			r1 := testEnv.RequestRender(base + "&utm_source=google")
			Expect(r1.Error).To(BeNil())
			Expect(r1.StatusCode).To(Equal(200))
			Expect(r1.Headers.Get("EC-Source")).To(Equal("render"))

			By("Request 2: a different utm_source hits the same stripped cache key")
			r2 := testEnv.RequestRender(base + "&utm_source=facebook")
			Expect(r2.Error).To(BeNil())
			Expect(r2.Headers.Get("EC-Source")).To(Equal("render_cache"))

			By("Request 3: a different tracking type (gclid) also hits the cache")
			r3 := testEnv.RequestRender(base + "&gclid=abc123")
			Expect(r3.Error).To(BeNil())
			Expect(r3.Headers.Get("EC-Source")).To(Equal("render_cache"))

			By("Request 4: the canonical URL with no tracking params also hits the cache")
			r4 := testEnv.RequestRender(base)
			Expect(r4.Error).To(BeNil())
			Expect(r4.Headers.Get("EC-Source")).To(Equal("render_cache"))
		})
	})

	// SCENARIO 10: Parameter Order Independence (sorting + stripping converge to one key)
	Context("Parameter Order Independence", func() {

		It("should normalize parameter order consistently after stripping", func() {
			By("Request 1 renders fresh")
			r1 := testEnv.RequestRender("/tracking-params/?a=1&b=2&utm_source=google")
			Expect(r1.Error).To(BeNil())
			Expect(r1.StatusCode).To(Equal(200))
			Expect(r1.Headers.Get("EC-Source")).To(Equal("render"))

			By("Request 2 (reordered params, different utm_source) hits the same key")
			r2 := testEnv.RequestRender("/tracking-params/?b=2&a=1&utm_source=facebook")
			Expect(r2.Error).To(BeNil())
			Expect(r2.Headers.Get("EC-Source")).To(Equal("render_cache"))

			By("Request 3 (another order, different utm_source) also hits the same key")
			r3 := testEnv.RequestRender("/tracking-params/?b=2&utm_source=twitter&a=1")
			Expect(r3.Error).To(BeNil())
			Expect(r3.Headers.Get("EC-Source")).To(Equal("render_cache"))
		})
	})

	// SCENARIO 11: Edge cases
	Context("Edge Cases", func() {

		It("should serve a parameterless URL consistently from cache on repeat", func() {
			expectSharedCacheKey("/tracking-params/", "/tracking-params/")
		})

		It("should normalize a URL whose only params are tracking params to the bare path", func() {
			expectSharedCacheKey(
				"/tracking-params/?utm_source=google&utm_medium=cpc",
				"/tracking-params/")
		})

		It("should strip tracking params on deeper nested paths", func() {
			expectSharedCacheKey(
				"/tracking-params/a/b/c/page?utm_source=google&id=7",
				"/tracking-params/a/b/c/page?id=7")
		})
	})
})

// expectSharedCacheKey asserts urlB is served from urlA's render cache. A cache hit
// proves both URLs normalize to the same cache key, i.e. the tracking parameters that
// differ between them were stripped. The suite clears the cache before each test, so
// urlA always renders fresh first.
func expectSharedCacheKey(urlA, urlB string) {
	r1 := testEnv.RequestRender(urlA)
	ExpectWithOffset(1, r1.Error).To(BeNil())
	ExpectWithOffset(1, r1.StatusCode).To(Equal(200))
	ExpectWithOffset(1, r1.Headers.Get("EC-Source")).To(Equal("render"), "first request should render fresh")

	r2 := testEnv.RequestRender(urlB)
	ExpectWithOffset(1, r2.Error).To(BeNil())
	ExpectWithOffset(1, r2.StatusCode).To(Equal(200))
	ExpectWithOffset(1, r2.Headers.Get("EC-Source")).To(Equal("render_cache"), "second request should hit the shared (stripped) cache key")
}

// expectDistinctCacheKeys asserts urlB is not served from urlA's cache. Both requests
// render fresh, proving the URLs normalize to different cache keys, i.e. the differing
// parameter was preserved (not stripped).
func expectDistinctCacheKeys(urlA, urlB string) {
	r1 := testEnv.RequestRender(urlA)
	ExpectWithOffset(1, r1.Error).To(BeNil())
	ExpectWithOffset(1, r1.StatusCode).To(Equal(200))
	ExpectWithOffset(1, r1.Headers.Get("EC-Source")).To(Equal("render"))

	r2 := testEnv.RequestRender(urlB)
	ExpectWithOffset(1, r2.Error).To(BeNil())
	ExpectWithOffset(1, r2.StatusCode).To(Equal(200))
	ExpectWithOffset(1, r2.Headers.Get("EC-Source")).To(Equal("render"), "second request must render fresh (distinct cache key, param preserved)")
}

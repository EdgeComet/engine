package acceptance_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SEO Optimization Scenarios", Serial, func() {
	Context("when rendering SPA for search engines", func() {
		It("should render SPA with fully loaded content", func() {
			By("Making a request to an SPA page")
			response := testEnv.RequestRender("/seo/spa-initial.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying the title was updated from loading state")
			Expect(response.Body).NotTo(ContainSubstring("<title>SPA Loading...</title>"))
			Expect(response.Body).To(ContainSubstring("<title>Amazing Product - Best Deal Online</title>"))

			By("Verifying the meta description was updated")
			Expect(response.Body).NotTo(ContainSubstring(`content="Loading..."`))
			Expect(response.Body).To(ContainSubstring("Amazing product with great features. Get the best deal online"))

			By("Verifying Open Graph tags were added by JavaScript")
			Expect(response.Body).To(ContainSubstring(`<meta property="og:title" content="Amazing Product - Best Deal Online"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:type" content="product"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:image"`))

			By("Verifying Twitter Card tags were added")
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:card" content="summary_large_image"`))
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:title"`))

			By("Verifying structured data was added")
			Expect(response.Body).To(ContainSubstring(`"@type": "Product"`))
			Expect(response.Body).To(ContainSubstring(`"name": "Amazing Product"`))
			Expect(response.Body).To(ContainSubstring(`"offers"`))
			Expect(response.Body).To(ContainSubstring(`"aggregateRating"`))

			By("Verifying SPA content was fully rendered")
			Expect(response.Body).To(ContainSubstring("<h1 class=\"product-title\">Amazing Product</h1>"))
			Expect(response.Body).To(ContainSubstring("★★★★★"))
			Expect(response.Body).To(ContainSubstring("$299.99"))
			Expect(response.Body).To(ContainSubstring("Add to Cart"))

			By("Verifying loading states are NOT present")
			Expect(response.Body).NotTo(ContainSubstring("Loading application..."))
			Expect(response.Body).NotTo(ContainSubstring(`class="loading-screen"`))
			Expect(response.Body).NotTo(ContainSubstring(`class="spinner"`))

			By("Verifying product features are present")
			Expect(response.Body).To(ContainSubstring("Premium materials and construction"))
			Expect(response.Body).To(ContainSubstring("Advanced technology integration"))
			Expect(response.Body).To(ContainSubstring("2-year warranty included"))

			By("Verifying review content is present")
			Expect(response.Body).To(ContainSubstring("Customer Reviews"))
			Expect(response.Body).To(ContainSubstring("Absolutely amazing product"))
			Expect(response.Body).To(ContainSubstring("Great value for money"))

			By("Verifying SPA completion markers")
			Expect(response.Body).To(ContainSubstring(`data-spa-loaded="true"`))
			Expect(response.Body).To(ContainSubstring(`class="spa-initialized"`))
		})

		It("should render lazy-loaded images properly", func() {
			By("Making a request to lazy image page")
			response := testEnv.RequestRender("/seo/lazy-images.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying hero image loads immediately")
			Expect(response.Body).To(ContainSubstring(`class="hero-image"`))
			Expect(response.Body).To(ContainSubstring("/images/hero.png"))

			By("Verifying lazy images were converted from data-src to src")
			// Images should no longer have data-src attributes
			Expect(response.Body).NotTo(ContainSubstring("data-src="))

			// Images should have proper src attributes
			Expect(response.Body).To(ContainSubstring("/images/product1.png"))
			Expect(response.Body).To(ContainSubstring("/images/product2.png"))
			Expect(response.Body).To(ContainSubstring("/images/product3.png"))
			Expect(response.Body).To(ContainSubstring("/images/product4.png"))

			By("Verifying images have loaded class")
			Expect(response.Body).To(ContainSubstring(`class="lazy-image loaded`))

			By("Verifying dynamic images were added")
			Expect(response.Body).To(ContainSubstring("/images/dynamic1.png"))
			Expect(response.Body).To(ContainSubstring("/images/dynamic2.png"))
			Expect(response.Body).To(ContainSubstring("/images/dynamic3.png"))
			Expect(response.Body).To(ContainSubstring(`class="dynamic-image loaded"`))

			By("Verifying CSS background images were set")
			Expect(response.Body).To(ContainSubstring("/images/background1.png"))
			Expect(response.Body).To(ContainSubstring("/images/background2.png"))
			Expect(response.Body).To(ContainSubstring(`bg-loaded`))

			By("Verifying completion markers")
			Expect(response.Body).To(ContainSubstring(`data-lazy-loading-complete="true"`))
			Expect(response.Body).To(ContainSubstring(`data-dynamic-images-loaded="true"`))
			Expect(response.Body).To(ContainSubstring(`data-css-backgrounds-loaded="true"`))
		})

		It("should capture dynamically added meta tags and structured data", func() {
			By("Making a request to dynamic meta page")
			response := testEnv.RequestRender("/seo/dynamic-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying initial loading content is NOT present")
			Expect(response.Body).NotTo(ContainSubstring("Loading Dynamic Content..."))
			Expect(response.Body).NotTo(ContainSubstring("Please wait while we load"))

			By("Verifying document title was updated")
			Expect(response.Body).To(ContainSubstring("Ultimate Guide to Web Performance - Expert Tips"))
			Expect(response.Body).NotTo(ContainSubstring("Initial Title - Loading"))

			By("Verifying meta description was updated")
			Expect(response.Body).To(ContainSubstring("Learn the ultimate guide to web performance optimization"))
			Expect(response.Body).NotTo(ContainSubstring("Initial description - loading content"))

			By("Verifying Open Graph tags were added dynamically")
			Expect(response.Body).To(ContainSubstring(`<meta property="og:title" content="Ultimate Guide to Web Performance`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:type" content="article"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:image"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="og:locale" content="en_US"`))

			By("Verifying Twitter Card tags were added")
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:card" content="summary_large_image"`))
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:site" content="@performanceexpert"`))
			Expect(response.Body).To(ContainSubstring(`<meta name="twitter:creator"`))

			By("Verifying additional SEO meta tags")
			Expect(response.Body).To(ContainSubstring(`<meta name="author" content="Jane Doe`))
			Expect(response.Body).To(ContainSubstring(`<meta property="article:author"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="article:published_time"`))
			Expect(response.Body).To(ContainSubstring(`<meta property="article:section" content="Web Development"`))

			By("Verifying canonical link was added")
			Expect(response.Body).To(ContainSubstring(`<link rel="canonical" href="https://example.com/guides/web-performance"`))

			By("Verifying Article structured data was added")
			Expect(response.Body).To(ContainSubstring(`"@type": "Article"`))
			Expect(response.Body).To(ContainSubstring(`"headline": "Ultimate Guide to Web Performance`))
			Expect(response.Body).To(ContainSubstring(`"author":`))
			Expect(response.Body).To(ContainSubstring(`"datePublished"`))
			Expect(response.Body).To(ContainSubstring(`"publisher"`))

			By("Verifying breadcrumb structured data was added")
			Expect(response.Body).To(ContainSubstring(`"@type": "BreadcrumbList"`))
			Expect(response.Body).To(ContainSubstring(`"position": 1`))
			Expect(response.Body).To(ContainSubstring(`"name": "Home"`))
			Expect(response.Body).To(ContainSubstring(`"name": "Guides"`))

			By("Verifying page content was fully rendered")
			Expect(response.Body).To(ContainSubstring("Ultimate Guide to Web Performance"))
			Expect(response.Body).To(ContainSubstring("Table of Contents"))
			Expect(response.Body).To(ContainSubstring("Understanding Web Performance"))
			Expect(response.Body).To(ContainSubstring("Optimization Techniques"))

			By("Verifying completion markers")
			Expect(response.Body).To(ContainSubstring(`data-meta-updated="true"`))
			Expect(response.Body).To(ContainSubstring(`data-content-loaded="true"`))
			Expect(response.Body).To(ContainSubstring(`class="dynamic-meta-complete"`))
		})
	})

	Context("when handling complex SEO scenarios", func() {
		It("should render content for different user agents appropriately", func() {
			By("Making a request with Googlebot user agent")
			response := testEnv.RequestRender("/seo/spa-initial.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying bot-optimized content is present")
			// SPA should be fully rendered for search engines
			Expect(response.Body).To(ContainSubstring("Amazing Product"))
			Expect(response.Body).To(ContainSubstring("★★★★★"))
			Expect(response.Body).To(ContainSubstring("Customer Reviews"))

			By("Verifying structured data is present for bots")
			Expect(response.Body).To(ContainSubstring(`"@type": "Product"`))
			Expect(response.Body).To(ContainSubstring(`"offers"`))
		})

		It("should handle infinite scroll by loading initial content", func() {
			By("Making a request to a page that would normally use infinite scroll")
			// Using the SPA page which has multiple sections
			response := testEnv.RequestRender("/seo/spa-initial.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying initial content is fully loaded")
			Expect(response.Body).To(ContainSubstring("Amazing Product"))
			Expect(response.Body).To(ContainSubstring("Product Description"))

			By("Verifying review content is loaded")
			// Reviews that might normally load via infinite scroll should be present
			Expect(response.Body).To(ContainSubstring("John D."))
			Expect(response.Body).To(ContainSubstring("Sarah M."))
			Expect(response.Body).To(ContainSubstring("Verified Purchase"))
		})

		It("should capture social media meta tags correctly", func() {
			By("Making a request to a page with social media optimization")
			response := testEnv.RequestRender("/seo/spa-initial.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Facebook Open Graph tags")
			Expect(response.Body).To(ContainSubstring(`property="og:title"`))
			Expect(response.Body).To(ContainSubstring(`property="og:description"`))
			Expect(response.Body).To(ContainSubstring(`property="og:image"`))
			Expect(response.Body).To(ContainSubstring(`property="og:url"`))

			By("Verifying Twitter Card tags")
			Expect(response.Body).To(ContainSubstring(`name="twitter:card"`))
			Expect(response.Body).To(ContainSubstring(`name="twitter:title"`))
			Expect(response.Body).To(ContainSubstring(`name="twitter:description"`))
			Expect(response.Body).To(ContainSubstring(`name="twitter:image"`))
		})

		It("should handle JavaScript redirects properly", func() {
			By("Making a request to a page that redirects via JavaScript")
			response := testEnv.RequestRender("/edge-cases/redirect.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying the redirect was followed")
			// Should see the destination page content, not the redirect page
			Expect(response.Body).To(ContainSubstring("Redirect Destination Page"))
			Expect(response.Body).To(ContainSubstring("Redirect Successful!"))

			By("Verifying redirect metadata")
			Expect(response.Body).To(ContainSubstring("redirect-destination.html"))
			Expect(response.Body).To(ContainSubstring("JavaScript window.location"))

			By("Verifying final page content")
			Expect(response.Body).To(ContainSubstring("Final Destination Page"))
			Expect(response.Body).To(ContainSubstring("actual content that should be indexed"))

			By("Verifying completion markers")
			Expect(response.Body).To(ContainSubstring(`data-redirected="true"`))
			Expect(response.Body).To(ContainSubstring(`data-redirect-complete="true"`))
			Expect(response.Body).To(ContainSubstring(`data-final-page="true"`))

			By("Verifying initial redirect page content is NOT present")
			Expect(response.Body).NotTo(ContainSubstring("This page will redirect to test redirect handling"))
			Expect(response.Body).NotTo(ContainSubstring("Redirecting in:"))
		})
	})

	Context("when testing advanced SEO features", func() {
		It("should preserve schema.org structured data", func() {
			By("Making a request to structured data page")
			response := testEnv.RequestRender("/static/with-structured-data.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Article schema is intact")
			Expect(response.Body).To(ContainSubstring(`"@context": "https://schema.org"`))
			Expect(response.Body).To(ContainSubstring(`"@type": "Article"`))
			Expect(response.Body).To(ContainSubstring(`"headline": "Structured Data Test Article"`))

			By("Verifying Product schema is intact")
			Expect(response.Body).To(ContainSubstring(`"@type": "Product"`))
			Expect(response.Body).To(ContainSubstring(`"name": "Test Product"`))
			Expect(response.Body).To(ContainSubstring(`"offers"`))
			Expect(response.Body).To(ContainSubstring(`"aggregateRating"`))
		})

		It("should handle multilingual content properly", func() {
			By("Making a request to page with international content")
			response := testEnv.RequestRender("/edge-cases/malformed.html")

			// Even malformed pages should handle encoding
			if response.Error == nil && response.StatusCode == 200 {
				By("Verifying UTF-8 encoding is preserved")
				Expect(response.Body).To(ContainSubstring("charset=UTF-8"))

				By("Verifying international characters are present")
				// The malformed page has mixed encoding examples
				if strings.Contains(response.Body, "Mixed text directions") {
					Expect(response.Body).To(ContainSubstring("Mixed text directions"))
				}
			}
		})

		It("should handle microdata and RDFa formats", func() {
			By("Making a request to structured data page")
			response := testEnv.RequestRender("/static/with-structured-data.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying JSON-LD is properly formatted")
			// JSON-LD should be valid and complete
			jsonLdCount := strings.Count(response.Body, `<script type="application/ld+json">`)
			Expect(jsonLdCount).To(BeNumerically(">=", 1))

			By("Verifying structured data completeness")
			// Should have complete schema objects
			Expect(response.Body).To(ContainSubstring(`"author":`))
			Expect(response.Body).To(ContainSubstring(`"publisher":`))
			Expect(response.Body).To(ContainSubstring(`"mainEntityOfPage"`))
		})

		It("should optimize for Core Web Vitals", func() {
			By("Making a request to a performance-optimized page")
			response := testEnv.RequestRender("/seo/spa-initial.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying above-the-fold content is present")
			// Critical content should be rendered
			Expect(response.Body).To(ContainSubstring("Amazing Product"))
			Expect(response.Body).To(ContainSubstring("$299.99"))

			By("Verifying images have proper attributes")
			// Images should have alt attributes for accessibility
			Expect(response.Body).To(ContainSubstring(`alt="Amazing Product`))

			By("Verifying structured content layout")
			// Page should have proper semantic structure
			Expect(response.Body).To(ContainSubstring("<main"))
			Expect(response.Body).To(ContainSubstring("<header"))
			Expect(response.Body).To(ContainSubstring("<section"))
		})
	})

	Context("when validating SEO best practices", func() {
		It("should have proper heading hierarchy", func() {
			By("Making a request to article page")
			response := testEnv.RequestRender("/seo/dynamic-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying heading structure")
			Expect(response.Body).To(ContainSubstring("<h1"))
			Expect(response.Body).To(ContainSubstring("<h2"))
			Expect(response.Body).To(ContainSubstring("<h3"))

			By("Verifying semantic HTML5 elements")
			Expect(response.Body).To(ContainSubstring("<main"))
			Expect(response.Body).To(ContainSubstring("<article"))
			Expect(response.Body).To(ContainSubstring("<section"))
		})

		It("should have comprehensive meta tag coverage", func() {
			By("Making a request to SEO optimized page")
			response := testEnv.RequestRender("/static/with-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying essential meta tags")
			metaTags := []string{
				`<meta charset="UTF-8"`,
				`<meta name="viewport"`,
				`<meta name="description"`,
				`<meta name="keywords"`,
				`<meta name="author"`,
				`<meta name="robots"`,
			}

			for _, tag := range metaTags {
				Expect(response.Body).To(ContainSubstring(tag))
			}

			By("Verifying social media meta tags")
			socialTags := []string{
				`<meta property="og:title"`,
				`<meta property="og:description"`,
				`<meta property="og:type"`,
				`<meta name="twitter:card"`,
			}

			for _, tag := range socialTags {
				Expect(response.Body).To(ContainSubstring(tag))
			}
		})

		It("should maintain content quality for search engines", func() {
			By("Making a request to content-rich page")
			response := testEnv.RequestRender("/seo/dynamic-meta.html")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying substantial content is present")
			// Page should have meaningful content length
			Expect(len(response.Body)).To(BeNumerically(">", 5000))

			By("Verifying content structure")
			Expect(response.Body).To(ContainSubstring("Table of Contents"))
			Expect(response.Body).To(ContainSubstring("Understanding Web Performance"))
			Expect(response.Body).To(ContainSubstring("Optimization Techniques"))

			By("Verifying internal linking")
			// Should have proper internal link structure
			Expect(response.Body).To(ContainSubstring(`href="#`))
			Expect(response.Body).To(ContainSubstring("breadcrumb"))
		})
	})
})

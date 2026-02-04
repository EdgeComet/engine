package acceptance_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Script Cleaning", Serial, func() {

	Context("when strip_scripts is enabled (default)", func() {

		It("should remove executable scripts", func() {
			By("Requesting a page with mixed scripts")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying executable inline scripts are removed")
			Expect(resp.Body).NotTo(ContainSubstring("console.log('Executable script in head')"))
			Expect(resp.Body).NotTo(ContainSubstring("console.log('Executable script in body')"))
			Expect(resp.Body).NotTo(ContainSubstring("window.headScriptRan"))

			By("Verifying module scripts are removed")
			Expect(resp.Body).NotTo(ContainSubstring("console.log('Module script')"))
			Expect(resp.Body).NotTo(ContainSubstring("window.moduleScriptRan"))

			By("Verifying text/javascript scripts are removed")
			Expect(resp.Body).NotTo(ContainSubstring("console.log('text/javascript type script')"))

			By("Verifying application/javascript scripts are removed")
			Expect(resp.Body).NotTo(ContainSubstring("console.log('application/javascript type script')"))
		})

		It("should remove external scripts with src attribute", func() {
			By("Requesting a page with external scripts")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying external script tags are removed")
			Expect(resp.Body).NotTo(ContainSubstring(`src="/api/mock-data`))
		})

		It("should remove script-related link tags", func() {
			By("Requesting a page with script-related links")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying modulepreload link is removed")
			Expect(resp.Body).NotTo(ContainSubstring(`rel="modulepreload"`))

			By("Verifying preload as=script link is removed")
			// Check for absence of preload with as="script"
			hasPreloadScript := strings.Contains(resp.Body, `rel="preload"`) &&
				strings.Contains(resp.Body, `as="script"`)
			Expect(hasPreloadScript).To(BeFalse(), "preload as=script link should be removed")
		})

		It("should preserve JSON-LD structured data", func() {
			By("Requesting a page with JSON-LD")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying JSON-LD script is preserved")
			Expect(resp.Body).To(ContainSubstring(`type="application/ld+json"`))
			Expect(resp.Body).To(ContainSubstring(`"@context"`))
			Expect(resp.Body).To(ContainSubstring(`"@type"`))
			Expect(resp.Body).To(ContainSubstring(`"WebPage"`))
		})

		It("should preserve template scripts", func() {
			By("Requesting a page with template scripts")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying text/template script is preserved")
			Expect(resp.Body).To(ContainSubstring(`type="text/template"`))
			Expect(resp.Body).To(ContainSubstring(`id="item-template"`))
			Expect(resp.Body).To(ContainSubstring("{{title}}"))

			By("Verifying text/x-template (Vue.js style) script is preserved")
			Expect(resp.Body).To(ContainSubstring(`type="text/x-template"`))
			Expect(resp.Body).To(ContainSubstring(`id="vue-template"`))
		})

		It("should preserve JSON data scripts", func() {
			By("Requesting a page with JSON data scripts")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying application/json script is preserved")
			Expect(resp.Body).To(ContainSubstring(`type="application/json"`))
			Expect(resp.Body).To(ContainSubstring(`id="page-data"`))
			Expect(resp.Body).To(ContainSubstring(`"pageId"`))
		})

		It("should preserve import maps", func() {
			By("Requesting a page with import map")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying importmap script is preserved")
			Expect(resp.Body).To(ContainSubstring(`type="importmap"`))
			Expect(resp.Body).To(ContainSubstring(`"imports"`))
		})

		It("should preserve non-script link tags", func() {
			By("Requesting a page with various link tags")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying stylesheet link is preserved")
			Expect(resp.Body).To(ContainSubstring(`rel="stylesheet"`))

			By("Verifying canonical link is preserved")
			Expect(resp.Body).To(ContainSubstring(`rel="canonical"`))
		})

		It("should preserve noscript elements", func() {
			By("Requesting a page with noscript element")
			resp := testEnv.RequestRender("/script-cleaning/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))

			By("Verifying noscript element is preserved")
			Expect(resp.Body).To(ContainSubstring("<noscript>"))
			Expect(resp.Body).To(ContainSubstring("noscript-message"))
		})
	})

	Context("with cache interaction", func() {

		It("should serve script-cleaned content from cache", func() {
			url := "/script-cleaning/mixed-scripts.html"

			By("Step 1: First request - renders and cleans scripts")
			resp1 := testEnv.RequestRender(url)
			Expect(resp1.Error).To(BeNil())
			Expect(resp1.Headers.Get("X-Render-Source")).To(Equal("rendered"))
			Expect(resp1.Body).NotTo(ContainSubstring("console.log"))
			Expect(resp1.Body).To(ContainSubstring(`type="application/ld+json"`))

			By("Step 2: Second request - served from cache")
			resp2 := testEnv.RequestRender(url)
			Expect(resp2.Error).To(BeNil())
			Expect(resp2.Headers.Get("X-Render-Source")).To(Equal("cache"))
			Expect(resp2.Headers.Get("X-Render-Cache")).To(Equal("hit"))

			By("Step 3: Verify cached content still has scripts cleaned")
			Expect(resp2.Body).NotTo(ContainSubstring("console.log"))
			Expect(resp2.Body).To(ContainSubstring(`type="application/ld+json"`))
		})
	})

	Context("when strip_scripts is disabled", func() {

		It("should keep all scripts including executable ones", func() {
			By("Requesting a page with scripts disabled for stripping")
			resp := testEnv.RequestRender("/script-cleaning/keep-scripts/mixed-scripts.html")

			By("Verifying request succeeded")
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(200))
			Expect(resp.Headers.Get("X-Render-Source")).To(Equal("rendered"))

			By("Verifying executable scripts are preserved")
			Expect(resp.Body).To(ContainSubstring("console.log"))

			By("Verifying JSON-LD is still present")
			Expect(resp.Body).To(ContainSubstring(`type="application/ld+json"`))

			By("Verifying template scripts are still present")
			Expect(resp.Body).To(ContainSubstring(`type="text/template"`))

			By("Verifying script-related links are preserved")
			Expect(resp.Body).To(ContainSubstring(`rel="modulepreload"`))
		})
	})
})

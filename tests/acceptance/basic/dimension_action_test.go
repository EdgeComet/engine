package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dimension Block Action", Serial, func() {
	Context("Block Dimension", func() {
		It("should return 403 for User-Agent matching block dimension", func() {
			By("Making request with SemrushBot User-Agent")
			response := makeRequestWithCustomUA("/blog/test", "SemrushBot/7.0")

			By("Verifying 403 response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Should return HTTP 403 Forbidden for blocked UA")
		})

		It("should return 403 for AhrefsBot User-Agent", func() {
			response := makeRequestWithCustomUA("/blog/test", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Should return HTTP 403 Forbidden for AhrefsBot")
		})

		It("should return 403 for MJ12bot User-Agent", func() {
			response := makeRequestWithCustomUA("/blog/test", "Mozilla/5.0 (compatible; MJ12bot/v1.4.8; http://mj12bot.com/)")

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Should return HTTP 403 Forbidden for MJ12bot")
		})
	})

	Context("Block Overrides URL Rules", func() {
		It("should block even when URL rule has action=render", func() {
			By("Making request with SemrushBot to a render path")
			response := makeRequestWithCustomUA("/blog/test", "SemrushBot/7.0")

			By("Verifying block takes precedence over render action")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Block dimension should override URL rule action=render")
		})

		It("should block even when URL rule has action=bypass", func() {
			By("Making request with SemrushBot to a bypass path")
			response := makeRequestWithCustomUA("/api/test", "SemrushBot/7.0")

			By("Verifying block takes precedence over bypass action")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403), "Block dimension should override URL rule action=bypass")
		})
	})

	Context("Block Dimension Headers", func() {
		It("should NOT set X-Unmatched-Dimension header", func() {
			By("Making request with blocked User-Agent")
			response := makeRequestWithCustomUA("/blog/test", "SemrushBot/7.0")

			By("Verifying X-Unmatched-Dimension is not set")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(403))
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(BeEmpty(),
				"X-Unmatched-Dimension header should NOT be set for matched block dimension")
		})
	})

	Context("Non-Blocked User-Agent", func() {
		It("should serve normal response for Googlebot", func() {
			By("Making request with Googlebot User-Agent to same URL")
			response := makeRequestWithCustomUA("/blog/test", "Googlebot/2.1 (+http://www.google.com/bot.html)")

			By("Verifying normal response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Googlebot should not be blocked")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(BeEmpty(),
				"X-Unmatched-Dimension should not be set for matched render dimension")
		})
	})

	Context("Bypass Dimension as Default", func() {
		It("should bypass for unmatched User-Agent with no URL rule match", func() {
			By("Making request with unknown User-Agent to a path with no URL rule")
			response := makeRequestWithCustomUA("/static/simple.html", "UnknownBot/1.0")

			By("Verifying bypass response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"Unmatched UA should default to bypass dimension")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"X-Unmatched-Dimension header should be set for unmatched UA")
		})
	})

	Context("Backward Compatibility", func() {
		It("should default to render when dimension has no action field", func() {
			By("Making request with Googlebot UA matching desktop dimension (no action field in config)")
			response := makeRequestWithCustomUA("/static/simple.html", "Googlebot/2.1 (+http://www.google.com/bot.html)")

			By("Verifying dimension without action field renders normally")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Dimension without action field should default to render")
			Expect(response.Headers.Get("X-Unmatched-Dimension")).To(BeEmpty(),
				"Should not set X-Unmatched-Dimension for matched dimension")
		})
	})

	Context("Mixed Configurations", func() {
		It("should handle render, block, and bypass dimensions in a single host", func() {
			By("Requesting with Googlebot (desktop dimension, implicit action=render)")
			renderResp := makeRequestWithCustomUA("/static/simple.html", "Googlebot/2.1 (+http://www.google.com/bot.html)")
			Expect(renderResp.Error).To(BeNil())
			Expect(renderResp.StatusCode).To(Equal(200))
			Expect(renderResp.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Desktop dimension should render")

			By("Requesting with SemrushBot (scrapers dimension, action=block)")
			blockResp := makeRequestWithCustomUA("/static/simple.html", "SemrushBot/7.0")
			Expect(blockResp.Error).To(BeNil())
			Expect(blockResp.StatusCode).To(Equal(403),
				"Scrapers dimension should return 403")

			By("Requesting with unknown UA (bypass dimension, auto-injected)")
			bypassResp := makeRequestWithCustomUA("/static/simple.html", "UnknownBot/1.0")
			Expect(bypassResp.Error).To(BeNil())
			Expect(bypassResp.StatusCode).To(Equal(200))
			Expect(bypassResp.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"Unmatched UA should fall back to bypass dimension")
			Expect(bypassResp.Headers.Get("X-Unmatched-Dimension")).To(Equal("true"),
				"Unmatched UA should set X-Unmatched-Dimension header")
		})
	})

	Context("Dimension Action and URL Rule Interaction", func() {
		It("should render when bypass dimension hits URL rule with action=render", func() {
			By("Making request with unmatched UA to /blog/test (URL rule action=render)")
			response := makeRequestWithCustomUA("/blog/test", "UnknownBot/1.0")

			By("Verifying URL rule overrides bypass dimension to render")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"URL rule action=render should override bypass dimension default")
		})

		It("should bypass when render dimension hits URL rule with action=bypass", func() {
			By("Making request with Googlebot to /api/test (URL rule action=bypass)")
			response := makeRequestWithCustomUA("/api/test", "Googlebot/2.1 (+http://www.google.com/bot.html)")

			By("Verifying URL rule overrides render dimension to bypass")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"URL rule action=bypass should override render dimension default")
		})

		It("should bypass when bypass dimension has no matching URL rule", func() {
			By("Making request with unmatched UA to a path with no URL rule")
			response := makeRequestWithCustomUA("/static/simple.html", "UnknownBot/1.0")

			By("Verifying bypass dimension default applies")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("bypass"),
				"Bypass dimension default should apply when no URL rule matches")
		})

		It("should render when render dimension has no matching URL rule", func() {
			By("Making request with Googlebot to a path with no URL rule")
			response := makeRequestWithCustomUA("/static/simple.html", "Googlebot/2.1 (+http://www.google.com/bot.html)")

			By("Verifying render dimension default applies")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Headers.Get("X-Render-Source")).To(Equal("rendered"),
				"Render dimension default should apply when no URL rule matches")
		})
	})
})

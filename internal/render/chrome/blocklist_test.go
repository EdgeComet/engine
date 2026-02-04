package chrome

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBlocklist(t *testing.T) {
	t.Run("creates blocklist with global domains", func(t *testing.T) {
		bl := NewBlocklist(nil)
		require.NotNil(t, bl)

		// Verify global wildcard patterns work (single-pass matching)
		assert.True(t, bl.IsBlocked("https://google-analytics.com"))
		assert.True(t, bl.IsBlocked("https://www.google-analytics.com"))
		assert.True(t, bl.IsBlocked("https://facebook.com/page"))
		assert.True(t, bl.IsBlocked("https://www.facebook.com/page"))
		assert.True(t, bl.IsBlocked("https://doubleclick.net/ads"))
		assert.True(t, bl.IsBlocked("https://ad.doubleclick.net/ads"))
	})

	t.Run("creates blocklist with empty custom patterns", func(t *testing.T) {
		bl := NewBlocklist([]string{})
		require.NotNil(t, bl)

		// Global domains should still work
		assert.True(t, bl.IsBlocked("https://google-analytics.com"))
	})

	t.Run("creates blocklist with custom wildcard patterns", func(t *testing.T) {
		bl := NewBlocklist([]string{
			"*custom-tracker.io*",
			"*ads.example.com*",
		})
		require.NotNil(t, bl)

		// Global domains should work
		assert.True(t, bl.IsBlocked("https://google-analytics.com"))

		// Custom wildcard patterns should work
		assert.True(t, bl.IsBlocked("https://custom-tracker.io"))
		assert.True(t, bl.IsBlocked("https://api.custom-tracker.io/track"))
		assert.True(t, bl.IsBlocked("https://ads.example.com"))
		assert.True(t, bl.IsBlocked("https://cdn.ads.example.com/banner.js"))
	})
}

func TestBlocklist_GlobalDomains(t *testing.T) {
	// Test all 30 global domains (each with *domain* pattern)
	tests := []struct {
		domain string
	}{
		{"2mdn.net"},
		{"adobestats.com"},
		{"adsappier.com"},
		{"affirm.com"},
		{"ampproject.org"},
		{"braintree-api.com"},
		{"braintreegateway.com"},
		{"chatra.io"},
		{"convertexperiments.com"},
		{"doubleclick.net"},
		{"estorecontent.com"},
		{"google-analytics.com"},
		{"googleadservices.com"},
		{"googleapis.com"},
		{"googlesyndication.com"},
		{"googletagservices.com"},
		{"googletagmanager.com"},
		{"googlevideo.com"},
		{"gstatic.com"},
		{"facebook.com"},
		{"lexx.me"},
		{"paypal.com"},
		{"paypalobjects.com"},
		{"pointandplace.com"},
		{"typekit.net"},
		{"twitter.com"},
		{"hotjar.com"},
		{"clarity.ms"},
		{"analytics.google.com"},
		{"youtube.com"},
		{"listrakbi.com"},
		{"static.cloudflareinsights.com"},
	}

	bl := NewBlocklist(nil)

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			// Test base domain
			assert.True(t, bl.IsBlocked("https://"+tt.domain), "should block base domain")

			// Test with www subdomain
			assert.True(t, bl.IsBlocked("https://www."+tt.domain), "should block www subdomain")

			// Test with api subdomain
			assert.True(t, bl.IsBlocked("https://api."+tt.domain), "should block api subdomain")

			// Test with path
			assert.True(t, bl.IsBlocked("https://"+tt.domain+"/path/to/script.js"), "should block with path")

			// Test with port (now included in URL string)
			assert.True(t, bl.IsBlocked("https://"+tt.domain+":443/path"), "should block with port")

			// Test http scheme
			assert.True(t, bl.IsBlocked("http://"+tt.domain), "should block http scheme")
		})
	}
}

func TestBlocklist_WildcardPatterns(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		testURL     string
		shouldBlock bool
	}{
		// Wildcard patterns match substring in full URL
		{
			name:        "wildcard matches base domain",
			pattern:     "*example.com*",
			testURL:     "https://example.com/page",
			shouldBlock: true,
		},
		{
			name:        "wildcard matches www subdomain",
			pattern:     "*example.com*",
			testURL:     "https://www.example.com/page",
			shouldBlock: true,
		},
		{
			name:        "wildcard matches api subdomain",
			pattern:     "*example.com*",
			testURL:     "https://api.example.com/endpoint",
			shouldBlock: true,
		},
		{
			name:        "wildcard matches multi-level subdomain",
			pattern:     "*example.com*",
			testURL:     "https://api.v2.example.com/endpoint",
			shouldBlock: true,
		},
		{
			name:        "wildcard matches with port",
			pattern:     "*example.com*",
			testURL:     "https://example.com:8080/page",
			shouldBlock: true,
		},
		{
			name:        "wildcard matches with query params",
			pattern:     "*example.com*",
			testURL:     "https://example.com/page?foo=bar",
			shouldBlock: true,
		},
		{
			name:        "wildcard does not match different domain",
			pattern:     "*example.com*",
			testURL:     "https://other.net/page",
			shouldBlock: false,
		},
		{
			name:        "wildcard matches similar domain (substring matching limitation)",
			pattern:     "*example.com*",
			testURL:     "https://fakeexample.com.attacker.io/page",
			shouldBlock: true, // Tradeoff: substring matching can have false positives
		},
		// Case insensitive matching
		{
			name:        "case insensitive - uppercase pattern",
			pattern:     "*EXAMPLE.COM*",
			testURL:     "https://example.com/page",
			shouldBlock: true,
		},
		{
			name:        "case insensitive - uppercase URL",
			pattern:     "*example.com*",
			testURL:     "https://EXAMPLE.COM/page",
			shouldBlock: true,
		},
		{
			name:        "case insensitive - mixed case",
			pattern:     "*ExAmPlE.cOm*",
			testURL:     "https://eXaMpLe.CoM/page",
			shouldBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := NewBlocklist([]string{tt.pattern})
			assert.Equal(t, tt.shouldBlock, bl.IsBlocked(tt.testURL))
		})
	}
}

func TestBlocklist_PathPatterns(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		testURL     string
		shouldBlock bool
	}{
		{
			name:        "path pattern matches on any domain",
			pattern:     "*/tracking/*",
			testURL:     "https://example.com/tracking/pixel.gif",
			shouldBlock: true,
		},
		{
			name:        "path pattern matches on different domain",
			pattern:     "*/tracking/*",
			testURL:     "https://other.com/tracking/collect",
			shouldBlock: true,
		},
		{
			name:        "path pattern matches with subdomain",
			pattern:     "*/api/analytics/*",
			testURL:     "https://cdn.example.com/api/analytics/track.js",
			shouldBlock: true,
		},
		{
			name:        "path pattern does not match different path",
			pattern:     "*/tracking/*",
			testURL:     "https://example.com/other/path",
			shouldBlock: false,
		},
		{
			name:        "wildcard file extension pattern",
			pattern:     "*/pixel.gif",
			testURL:     "https://tracker.com/img/pixel.gif",
			shouldBlock: true,
		},
		{
			name:        "wildcard api pattern",
			pattern:     "*/api/track*",
			testURL:     "https://example.com/api/tracking",
			shouldBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := NewBlocklist([]string{tt.pattern})
			assert.Equal(t, tt.shouldBlock, bl.IsBlocked(tt.testURL))
		})
	}
}

func TestBlocklist_CombinedPatterns(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		testURL     string
		shouldBlock bool
	}{
		{
			name:        "combined domain and path pattern",
			pattern:     "*cdn.example.com/ads/*",
			testURL:     "https://cdn.example.com/ads/banner.js",
			shouldBlock: true,
		},
		{
			name:        "combined pattern with subdomain",
			pattern:     "*api.tracker.com/collect*",
			testURL:     "https://api.tracker.com/collect?id=123",
			shouldBlock: true,
		},
		{
			name:        "combined pattern does not match different domain",
			pattern:     "*cdn.example.com/ads/*",
			testURL:     "https://cdn.other.com/ads/banner.js",
			shouldBlock: false,
		},
		{
			name:        "combined pattern does not match different path",
			pattern:     "*cdn.example.com/ads/*",
			testURL:     "https://cdn.example.com/scripts/app.js",
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := NewBlocklist([]string{tt.pattern})
			assert.Equal(t, tt.shouldBlock, bl.IsBlocked(tt.testURL))
		})
	}
}

func TestBlocklist_RegexpPatterns(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		testURL     string
		shouldBlock bool
	}{
		{
			name:        "case-sensitive regexp - matches",
			pattern:     "~https?://.*\\.analytics\\..*",
			testURL:     "https://cdn.analytics.example.com/track.js",
			shouldBlock: true,
		},
		{
			name:        "case-sensitive regexp - does not match uppercase",
			pattern:     "~https?://.*\\.analytics\\..*",
			testURL:     "https://cdn.ANALYTICS.example.com/track.js",
			shouldBlock: false,
		},
		{
			name:        "case-insensitive regexp - matches lowercase",
			pattern:     "~*https?://.*\\.analytics\\..*",
			testURL:     "https://cdn.analytics.example.com/track.js",
			shouldBlock: true,
		},
		{
			name:        "case-insensitive regexp - matches uppercase",
			pattern:     "~*https?://.*\\.analytics\\..*",
			testURL:     "https://cdn.ANALYTICS.example.com/track.js",
			shouldBlock: true,
		},
		{
			name:        "regexp matches tracking or analytics",
			pattern:     "~*.*(tracking|analytics).*",
			testURL:     "https://example.com/tracking/pixel.gif",
			shouldBlock: true,
		},
		{
			name:        "regexp matches analytics case-insensitively",
			pattern:     "~*.*(tracking|analytics).*",
			testURL:     "https://example.com/ANALYTICS/collect",
			shouldBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := NewBlocklist([]string{tt.pattern})
			assert.Equal(t, tt.shouldBlock, bl.IsBlocked(tt.testURL))
		})
	}
}

func TestBlocklist_MultiplePatterns(t *testing.T) {
	bl := NewBlocklist([]string{
		"*tracker.com*",
		"*/api/analytics/*",
		"*cdn.example.com/ads/*",
		"~*.*(segment|mixpanel).*",
	})

	tests := []struct {
		name        string
		testURL     string
		shouldBlock bool
	}{
		{
			name:        "first pattern matches",
			testURL:     "https://tracker.com/pixel.gif",
			shouldBlock: true,
		},
		{
			name:        "second pattern matches",
			testURL:     "https://example.com/api/analytics/track",
			shouldBlock: true,
		},
		{
			name:        "third pattern matches",
			testURL:     "https://cdn.example.com/ads/banner.js",
			shouldBlock: true,
		},
		{
			name:        "fourth pattern matches (regexp)",
			testURL:     "https://segment.io/collect",
			shouldBlock: true,
		},
		{
			name:        "no pattern matches",
			testURL:     "https://safe.com/page",
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.shouldBlock, bl.IsBlocked(tt.testURL))
		})
	}
}

func TestBlocklist_ResourceTypeBlocking(t *testing.T) {
	t.Run("resource type blocking only", func(t *testing.T) {
		bl := NewBlocklistWithResourceTypes(nil, []string{"Image", "Media", "Font"})

		assert.True(t, bl.IsResourceTypeBlocked("Image"))
		assert.True(t, bl.IsResourceTypeBlocked("Media"))
		assert.True(t, bl.IsResourceTypeBlocked("Font"))
		assert.False(t, bl.IsResourceTypeBlocked("Script"))
		assert.False(t, bl.IsResourceTypeBlocked("Stylesheet"))
	})

	t.Run("URL and resource type blocking combined", func(t *testing.T) {
		bl := NewBlocklistWithResourceTypes(
			[]string{"*tracker.com*"},
			[]string{"Image"},
		)

		// URL should be blocked
		assert.True(t, bl.IsBlocked("https://tracker.com/pixel.gif"))

		// Resource type should be blocked
		assert.True(t, bl.IsResourceTypeBlocked("Image"))
		assert.False(t, bl.IsResourceTypeBlocked("Script"))
	})

	t.Run("empty resource types", func(t *testing.T) {
		bl := NewBlocklistWithResourceTypes([]string{"*tracker.com*"}, []string{})

		// URL blocking should work
		assert.True(t, bl.IsBlocked("https://tracker.com/pixel.gif"))

		// No resource types blocked
		assert.False(t, bl.IsResourceTypeBlocked("Image"))
		assert.False(t, bl.IsResourceTypeBlocked("Script"))
	})

	t.Run("whitespace in resource types", func(t *testing.T) {
		bl := NewBlocklistWithResourceTypes(nil, []string{" Image ", " Media "})

		assert.True(t, bl.IsResourceTypeBlocked("Image"))
		assert.True(t, bl.IsResourceTypeBlocked("Media"))
	})
}

func TestBlocklist_EdgeCases(t *testing.T) {
	t.Run("empty pattern ignored", func(t *testing.T) {
		bl := NewBlocklist([]string{"", "  ", "*tracker.com*"})
		require.NotNil(t, bl)

		// Non-empty pattern should work
		assert.True(t, bl.IsBlocked("https://tracker.com"))
	})

	t.Run("invalid regexp pattern skipped", func(t *testing.T) {
		bl := NewBlocklist([]string{"~[invalid(regex", "*tracker.com*"})
		require.NotNil(t, bl)

		// Valid pattern should still work
		assert.True(t, bl.IsBlocked("https://tracker.com"))
	})

	t.Run("query parameters in URL", func(t *testing.T) {
		bl := NewBlocklist([]string{"*tracker.com*"})

		assert.True(t, bl.IsBlocked("https://tracker.com/collect?id=123&ref=test"))
		assert.True(t, bl.IsBlocked("https://tracker.com?utm_source=google"))
	})

	t.Run("URL fragments", func(t *testing.T) {
		bl := NewBlocklist([]string{"*tracker.com*"})

		assert.True(t, bl.IsBlocked("https://tracker.com/page#section"))
		assert.True(t, bl.IsBlocked("https://tracker.com#top"))
	})

	t.Run("very long URL", func(t *testing.T) {
		bl := NewBlocklist([]string{"*tracker.com*"})

		longPath := "/very/long/path/with/many/segments/that/goes/on/and/on/and/on"
		assert.True(t, bl.IsBlocked("https://tracker.com"+longPath))
	})

	t.Run("special characters in URL", func(t *testing.T) {
		bl := NewBlocklist([]string{"*tracker.com*"})

		assert.True(t, bl.IsBlocked("https://tracker.com/path%20with%20spaces"))
		assert.True(t, bl.IsBlocked("https://tracker.com/path?q=search+query&x=1"))
	})
}

func TestBlocklist_RealWorldScenarios(t *testing.T) {
	t.Run("common analytics and tracking services", func(t *testing.T) {
		bl := NewBlocklist(nil) // Uses global blocklist

		// Google Analytics
		assert.True(t, bl.IsBlocked("https://www.google-analytics.com/analytics.js"))
		assert.True(t, bl.IsBlocked("https://ssl.google-analytics.com/ga.js"))

		// Google Tag Manager
		assert.True(t, bl.IsBlocked("https://www.googletagmanager.com/gtm.js?id=GTM-XXXX"))

		// Facebook
		assert.True(t, bl.IsBlocked("https://www.facebook.com/tr?id=123456"))
		assert.True(t, bl.IsBlocked("https://m.facebook.com/page"))

		// DoubleClick
		assert.True(t, bl.IsBlocked("https://googleads.g.doubleclick.net/pagead/id"))
		assert.True(t, bl.IsBlocked("https://stats.g.doubleclick.net/dc.js"))

		// Twitter
		assert.True(t, bl.IsBlocked("https://platform.twitter.com/widgets.js"))

		// Hotjar
		assert.True(t, bl.IsBlocked("https://static.hotjar.com/c/hotjar-123456.js"))
	})

	t.Run("custom tracking patterns", func(t *testing.T) {
		bl := NewBlocklist([]string{
			"*mixpanel.com*",
			"*segment.io*",
			"*/api/track*",
		})

		assert.True(t, bl.IsBlocked("https://api.mixpanel.com/track"))
		assert.True(t, bl.IsBlocked("https://cdn.segment.io/analytics.js"))
		assert.True(t, bl.IsBlocked("https://example.com/api/tracking"))
	})
}

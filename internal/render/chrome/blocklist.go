package chrome

import (
	"strings"

	"github.com/edgecomet/engine/pkg/pattern"
)

// globalBlockedPatterns is the hardcoded list of patterns to block across all requests
// These are primarily analytics, tracking, and third-party services
// Patterns use wildcards for substring matching against full URLs
var globalBlockedPatterns = []string{
	"*2mdn.net*",
	"*adobestats.com*",
	"*adsappier.com*",
	"*affirm.com*",
	"*ampproject.org*",
	"*braintree-api.com*",
	"*braintreegateway.com*",
	"*chatra.io*",
	"*convertexperiments.com*",
	"*doubleclick.net*",
	"*estorecontent.com*",
	"*google-analytics.com*",
	"*googleadservices.com*",
	"*googleapis.com*",
	"*googlesyndication.com*",
	"*googletagservices.com*",
	"*googletagmanager.com*",
	"*googlevideo.com*",
	"*gstatic.com*",
	"*facebook.com*",
	"*lexx.me*",
	"*paypal.com*",
	"*paypalobjects.com*",
	"*pointandplace.com*",
	"*typekit.net*",
	"*twitter.com*",
	"*hotjar.com*",
	"*clarity.ms*",
	"*analytics.google.com*",
	"*youtube.com*",
	"*listrakbi.com*",
	"*static.cloudflareinsights.com*",
}

// Blocklist holds compiled blocking rules for a render request
type Blocklist struct {
	compiledPatterns     []*pattern.Pattern  // Pre-compiled URL patterns (supports exact, wildcard, regexp)
	originalPatterns     []string            // Original pattern strings (for debugging)
	blockedResourceTypes map[string]struct{} // Resource types to block (Image, Media, Font, etc.)
}

// NewBlocklist creates a new blocklist combining global rules and custom patterns
func NewBlocklist(customPatterns []string) *Blocklist {
	return NewBlocklistWithResourceTypes(customPatterns, nil)
}

// NewBlocklistWithResourceTypes creates a new blocklist with URL patterns and resource type filtering
// Patterns are matched against full URLs (scheme://host/path) using single-pass matching
// Pattern types:
// - Exact: "google-analytics.com" (matches exact string in URL, case-insensitive)
// - Wildcard: "*google-analytics.com*" (matches substring in URL, case-insensitive)
// - Wildcard: "*/tracking/*" (matches path pattern in URL, case-insensitive)
// - Regexp: "~^https?://.*\\.ads\\..*" (case-sensitive regexp)
// - Regexp CI: "~*.*(tracking|analytics).*" (case-insensitive regexp)
func NewBlocklistWithResourceTypes(customPatterns []string, resourceTypes []string) *Blocklist {
	// Combine global and custom patterns
	allPatterns := make([]string, 0, len(globalBlockedPatterns)+len(customPatterns))
	allPatterns = append(allPatterns, globalBlockedPatterns...)
	allPatterns = append(allPatterns, customPatterns...)

	bl := &Blocklist{
		compiledPatterns:     make([]*pattern.Pattern, 0, len(allPatterns)),
		originalPatterns:     allPatterns,
		blockedResourceTypes: make(map[string]struct{}),
	}

	// Compile all patterns
	for _, pat := range allPatterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}

		// Only lowercase non-regexp patterns (exact and wildcard)
		// Regexp patterns (~, ~*) should preserve case for case-sensitive matching
		if !strings.HasPrefix(pat, "~") {
			pat = strings.ToLower(pat)
		}

		compiled, err := pattern.Compile(pat)
		if err != nil {
			// Skip invalid patterns (log in production)
			continue
		}
		bl.compiledPatterns = append(bl.compiledPatterns, compiled)
	}

	// Add blocked resource types
	for _, rt := range resourceTypes {
		bl.addResourceType(rt)
	}

	return bl
}

// IsBlocked checks if a URL should be blocked based on compiled pattern rules
// Uses single-pass matching against the full URL string
func (bl *Blocklist) IsBlocked(requestURL string) bool {
	// For non-regexp patterns, use lowercase URL for case-insensitive matching
	// For regexp patterns, preserve original case
	lowercaseURL := strings.ToLower(requestURL)

	// Test against each compiled pattern
	for _, compiledPattern := range bl.compiledPatterns {
		// Choose URL case based on pattern type
		url := lowercaseURL
		if compiledPattern.Type == pattern.PatternTypeRegexp {
			url = requestURL
		}

		// Single-pass match against full URL
		if compiledPattern.Match(url) {
			return true
		}
	}

	return false
}

// addResourceType adds a resource type to the blocklist
func (bl *Blocklist) addResourceType(resourceType string) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return
	}
	bl.blockedResourceTypes[resourceType] = struct{}{}
}

// IsResourceTypeBlocked checks if a resource type should be blocked
func (bl *Blocklist) IsResourceTypeBlocked(resourceType string) bool {
	if len(bl.blockedResourceTypes) == 0 {
		return false
	}
	_, blocked := bl.blockedResourceTypes[resourceType]
	return blocked
}

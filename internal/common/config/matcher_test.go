package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// TestPatternMatcher_ExactMatch tests exact URL pattern matching
func TestPatternMatcher_ExactMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		url      string
		expected bool
	}{
		{
			name:     "exact match - root path",
			pattern:  "/",
			url:      "https://example.com/",
			expected: true,
		},
		{
			name:     "exact match - simple path",
			pattern:  "/blog",
			url:      "https://example.com/blog",
			expected: true,
		},
		{
			name:     "exact no match - different path",
			pattern:  "/",
			url:      "https://example.com/blog",
			expected: false,
		},
		{
			name:     "exact match - path only (query params ignored)",
			pattern:  "/search",
			url:      "https://example.com/search?q=test",
			expected: true, // Patterns match path only, query params are ignored
		},
		{
			name:     "exact match - path matches despite query params",
			pattern:  "/search",
			url:      "https://example.com/search?b=2&a=1",
			expected: true, // Path-only matching ignores query parameters
		},
		{
			name:     "exact match - complex path",
			pattern:  "/api/v1/users",
			url:      "https://example.com/api/v1/users",
			expected: true,
		},
		{
			name:     "exact no match - partial path",
			pattern:  "/blog/post",
			url:      "https://example.com/blog",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:  tt.pattern,
				Action: types.ActionRender,
			}
			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expected {
				require.NotNil(t, matched, "Expected pattern to match")
				assert.Equal(t, types.ActionRender, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_SingleWildcard tests single wildcard (*) pattern matching
func TestPatternMatcher_SingleWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		url      string
		expected bool
	}{
		{
			name:     "single wildcard - match one level",
			pattern:  "/blog/*",
			url:      "https://example.com/blog/post1",
			expected: true,
		},
		{
			name:     "single wildcard - matches deeper path (recursive)",
			pattern:  "/blog/*",
			url:      "https://example.com/blog/2024/post1",
			expected: true, // Wildcard is recursive - matches multiple levels
		},
		{
			name:     "single wildcard - match with extension",
			pattern:  "*.pdf",
			url:      "https://example.com/document.pdf",
			expected: true,
		},
		{
			name:     "single wildcard - match extension with query params",
			pattern:  "*.pdf",
			url:      "https://example.com/document.pdf?download=true&version=2",
			expected: true,
		},
		{
			name:     "single wildcard - match extension nested path with query params",
			pattern:  "*.pdf",
			url:      "https://example.com/reports/2024/document.pdf?page=1",
			expected: true,
		},
		{
			name:     "single wildcard - no match wrong extension",
			pattern:  "*.pdf",
			url:      "https://example.com/document.docx",
			expected: false,
		},
		{
			name:     "single wildcard - no match wrong extension with query params",
			pattern:  "*.pdf",
			url:      "https://example.com/document.docx?download=true",
			expected: false,
		},
		{
			name:     "single wildcard - match at end",
			pattern:  "/search*",
			url:      "https://example.com/search",
			expected: true,
		},
		{
			name:     "single wildcard - match with query",
			pattern:  "/search*",
			url:      "https://example.com/search?q=test",
			expected: true,
		},
		{
			name:     "single wildcard - middle wildcard",
			pattern:  "/product/*/reviews",
			url:      "https://example.com/product/123/reviews",
			expected: true,
		},
		{
			name:     "single wildcard - middle wildcard matches deeper (recursive)",
			pattern:  "/product/*/reviews",
			url:      "https://example.com/product/123/details/reviews",
			expected: true, // Wildcard is recursive - matches multiple levels
		},
		{
			name:     "single wildcard - multiple wildcards",
			pattern:  "/api/*/users/*",
			url:      "https://example.com/api/v1/users/123",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:  tt.pattern,
				Action: types.ActionBypass,
			}
			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expected {
				require.NotNil(t, matched, "Expected pattern to match")
				assert.Equal(t, types.ActionBypass, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_MultiplePatterns tests matching with array of patterns
func TestPatternMatcher_MultiplePatterns(t *testing.T) {
	rule := types.URLRule{
		Match:  []interface{}{"/blog/*", "/articles/*", "/news/*"},
		Action: types.ActionRender,
	}
	matcher := NewPatternMatcher([]types.URLRule{rule})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"matches first pattern", "https://example.com/blog/post1", true},
		{"matches second pattern", "https://example.com/articles/story1", true},
		{"matches third pattern", "https://example.com/news/update1", true},
		{"no match", "https://example.com/events/meetup1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			if tt.expected {
				require.NotNil(t, matched, "Expected pattern to match")
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_FirstMatchWins tests that first matching rule is returned
func TestPatternMatcher_FirstMatchWins(t *testing.T) {
	rules := []types.URLRule{
		{
			Match:  "/blog/*",
			Action: types.ActionBlock,
			Status: &types.StatusRuleConfig{Reason: "first rule"},
		},
		{
			Match:  "/blog/post1",
			Action: types.ActionRender,
		},
		{
			Match:  "/*",
			Action: types.ActionBypass,
		},
	}
	matcher := NewPatternMatcher(rules)

	// Should match first rule, not more specific second rule
	matched, _ := matcher.FindMatchingRule("https://example.com/blog/post1")
	require.NotNil(t, matched)
	assert.Equal(t, types.ActionBlock, matched.Action)
	assert.Equal(t, "first rule", matched.Status.Reason)

	// Should match first rule
	matched, _ = matcher.FindMatchingRule("https://example.com/blog/post2")
	require.NotNil(t, matched)
	assert.Equal(t, types.ActionBlock, matched.Action)

	// Should match third rule (fallback)
	matched, _ = matcher.FindMatchingRule("https://example.com/other")
	require.NotNil(t, matched)
	assert.Equal(t, types.ActionBypass, matched.Action)
}

// TestPatternMatcher_PathOnlyMatching tests that patterns match path only, ignoring query params
func TestPatternMatcher_PathOnlyMatching(t *testing.T) {
	rule := types.URLRule{
		Match:  "/search",
		Action: types.ActionRender,
	}
	matcher := NewPatternMatcher([]types.URLRule{rule})

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "no query params",
			url:  "https://example.com/search",
		},
		{
			name: "with query params",
			url:  "https://example.com/search?a=1&b=2",
		},
		{
			name: "reverse order query params",
			url:  "https://example.com/search?b=2&a=1",
		},
		{
			name: "different query values",
			url:  "https://example.com/search?a=2&b=1",
		},
		{
			name: "many query params",
			url:  "https://example.com/search?a=1&b=2&c=3&d=4",
		},
		{
			name: "single query param",
			url:  "https://example.com/search?q=test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			require.NotNil(t, matched, "Path-only matching should match regardless of query params")
			assert.Equal(t, types.ActionRender, matched.Action)
		})
	}
}

// TestPatternMatcher_PathWildcardWithQuery tests path wildcard matching with query params
func TestPatternMatcher_PathWildcardWithQuery(t *testing.T) {
	rule := types.URLRule{
		Match:  "/search*",
		Action: types.ActionRender,
	}
	matcher := NewPatternMatcher([]types.URLRule{rule})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "no query params",
			url:      "https://example.com/search",
			expected: true,
		},
		{
			name:     "with query params",
			url:      "https://example.com/search?q=test",
			expected: true,
		},
		{
			name:     "with multiple query params",
			url:      "https://example.com/search?q=test&page=2",
			expected: true,
		},
		{
			name:     "query params normalized",
			url:      "https://example.com/search?page=2&q=test",
			expected: true,
		},
		{
			name:     "no match - different path",
			url:      "https://example.com/other?q=test",
			expected: false,
		},
		{
			name:     "no match - different path prefix",
			url:      "https://example.com/results?q=test",
			expected: false,
		},
		{
			name:     "no match - path doesn't start with pattern",
			url:      "https://example.com/api/search?q=test",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			if tt.expected {
				require.NotNil(t, matched, "Expected pattern to match")
				assert.Equal(t, types.ActionRender, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_SpecialCharsInQuery tests URLs with special characters
func TestPatternMatcher_SpecialCharsInQuery(t *testing.T) {
	rule := types.URLRule{
		Match:  "/search*",
		Action: types.ActionRender,
	}
	matcher := NewPatternMatcher([]types.URLRule{rule})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"url encoded space", "https://example.com/search?q=hello%20world", true},
		{"plus as space", "https://example.com/search?q=hello+world", true},
		{"special chars", "https://example.com/search?q=test%21%40%23", true},
		{"unicode", "https://example.com/search?q=%E2%9C%93", true},
		{"empty value", "https://example.com/search?q=", true},
		{"multiple equals", "https://example.com/search?q=a=b=c", true},
		{"no match - different path", "https://example.com/other?q=hello%20world", false},
		{"no match - api path with special chars", "https://example.com/api?q=test%21", false},
		{"no match - wrong prefix with unicode", "https://example.com/find?q=%E2%9C%93", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			if tt.expected {
				require.NotNil(t, matched, "Expected pattern to match")
				assert.Equal(t, types.ActionRender, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_EdgeCases tests edge cases and error handling
func TestPatternMatcher_EdgeCases(t *testing.T) {
	t.Run("malformed URL returns nil", func(t *testing.T) {
		rule := types.URLRule{
			Match:  "/test",
			Action: types.ActionRender,
		}
		matcher := NewPatternMatcher([]types.URLRule{rule})
		matched, _ := matcher.FindMatchingRule("://invalid-url")
		assert.Nil(t, matched, "Malformed URL should return nil")
	})

	t.Run("empty pattern never matches", func(t *testing.T) {
		rule := types.URLRule{
			Match:  "",
			Action: types.ActionRender,
		}
		matcher := NewPatternMatcher([]types.URLRule{rule})
		matched, _ := matcher.FindMatchingRule("https://example.com/test")
		assert.Nil(t, matched, "Empty pattern should never match")
	})

	t.Run("wildcard only pattern", func(t *testing.T) {
		rule := types.URLRule{
			Match:  "*",
			Action: types.ActionRender,
		}
		matcher := NewPatternMatcher([]types.URLRule{rule})
		matched, _ := matcher.FindMatchingRule("https://example.com/test")
		require.NotNil(t, matched, "Wildcard-only should match anything")
	})

	t.Run("no rules returns nil", func(t *testing.T) {
		matcher := NewPatternMatcher([]types.URLRule{})
		matched, _ := matcher.FindMatchingRule("https://example.com/test")
		assert.Nil(t, matched, "No rules should return nil")
	})

	t.Run("URL with explicit root path", func(t *testing.T) {
		rule := types.URLRule{
			Match:  "/",
			Action: types.ActionRender,
		}
		matcher := NewPatternMatcher([]types.URLRule{rule})
		// NOTE: URL without path ("https://example.com") has empty path, not "/"
		// So we need to use explicit "/" in URL
		matched, _ := matcher.FindMatchingRule("https://example.com/")
		require.NotNil(t, matched, "Should match root path")
	})

	t.Run("pattern with fragment ignored", func(t *testing.T) {
		rule := types.URLRule{
			Match:  "/page",
			Action: types.ActionRender,
		}
		matcher := NewPatternMatcher([]types.URLRule{rule})
		// Fragments are not included in matching
		matched, _ := matcher.FindMatchingRule("https://example.com/page#section")
		require.NotNil(t, matched, "Should match ignoring fragment")
	})
}

// TestPatternMatcher_ComplexScenarios tests realistic complex scenarios
func TestPatternMatcher_ComplexScenarios(t *testing.T) {
	rules := []types.URLRule{
		{
			Match:  "/admin/*",
			Action: types.ActionBlock,
			Status: &types.StatusRuleConfig{Reason: "Admin area"},
		},
		{
			Match:  "/api/**",
			Action: types.ActionBlock,
			Status: &types.StatusRuleConfig{Reason: "API endpoints"},
		},
		{
			Match:  []interface{}{"/blog/*", "/articles/*"},
			Action: types.ActionRender,
		},
		{
			Match:  "/",
			Action: types.ActionRender,
		},
		{
			Match:  "/*",
			Action: types.ActionBypass,
		},
	}
	matcher := NewPatternMatcher(rules)

	tests := []struct {
		name           string
		url            string
		expectedAction types.URLRuleAction
		expectedReason string
	}{
		{
			name:           "block admin",
			url:            "https://example.com/admin/users",
			expectedAction: types.ActionBlock,
			expectedReason: "Admin area",
		},
		{
			name:           "block api shallow",
			url:            "https://example.com/api/v1",
			expectedAction: types.ActionBlock,
			expectedReason: "API endpoints",
		},
		{
			name:           "block api deep",
			url:            "https://example.com/api/v1/users/123/posts",
			expectedAction: types.ActionBlock,
			expectedReason: "API endpoints",
		},
		{
			name:           "render blog",
			url:            "https://example.com/blog/post1",
			expectedAction: types.ActionRender,
			expectedReason: "",
		},
		{
			name:           "render articles",
			url:            "https://example.com/articles/story1",
			expectedAction: types.ActionRender,
			expectedReason: "",
		},
		{
			name:           "render homepage",
			url:            "https://example.com/",
			expectedAction: types.ActionRender,
			expectedReason: "",
		},
		{
			name:           "bypass other pages",
			url:            "https://example.com/about",
			expectedAction: types.ActionBypass,
			expectedReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			require.NotNil(t, matched, "Expected a rule to match")
			assert.Equal(t, tt.expectedAction, matched.Action)
			if tt.expectedReason != "" {
				require.NotNil(t, matched.Status, "Expected Status to be set for block action")
				assert.Equal(t, tt.expectedReason, matched.Status.Reason)
			}
		})
	}
}

// TestPatternMatcher_RealWorldPatterns tests patterns from example config
func TestPatternMatcher_RealWorldPatterns(t *testing.T) {
	rules := []types.URLRule{
		{
			Match:  []interface{}{"/admin/*", "/wp-admin/*", "/wp-login.php"},
			Action: types.ActionBlock,
		},
		{
			Match:  []interface{}{"/account/*", "/checkout/*", "/cart"},
			Action: types.ActionBypass,
		},
		{
			Match:  "/",
			Action: types.ActionRender,
		},
		{
			Match:  []interface{}{"/blog/*", "/articles/*", "/news/*"},
			Action: types.ActionRender,
		},
		{
			Match:  []interface{}{"/search*", "/results*"},
			Action: types.ActionRender,
		},
		{
			Match:  []interface{}{"/static/*", "/assets/*", "/public/*"},
			Action: types.ActionBypass,
		},
	}
	matcher := NewPatternMatcher(rules)

	tests := []struct {
		name           string
		url            string
		expectedAction types.URLRuleAction
	}{
		{"block wp-admin", "https://example.com/wp-admin/settings", types.ActionBlock},
		{"block admin", "https://example.com/admin/dashboard", types.ActionBlock},
		{"block wp-login exact", "https://example.com/wp-login.php", types.ActionBlock},
		{"bypass account", "https://example.com/account/profile", types.ActionBypass},
		{"bypass checkout", "https://example.com/checkout/payment", types.ActionBypass},
		{"bypass cart exact", "https://example.com/cart", types.ActionBypass},
		{"render homepage", "https://example.com/", types.ActionRender},
		{"render blog", "https://example.com/blog/my-post", types.ActionRender},
		{"render search", "https://example.com/search?q=test", types.ActionRender},
		{"render search no query", "https://example.com/search", types.ActionRender},
		{"bypass static", "https://example.com/static/css/main.css", types.ActionBypass},
		{"bypass assets", "https://example.com/assets/images/logo.png", types.ActionBypass},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)
			require.NotNil(t, matched, "Expected a rule to match")
			assert.Equal(t, tt.expectedAction, matched.Action)
		})
	}
}

// TestGetMatchPatterns tests URLRule.GetMatchPatterns() helper
func TestGetMatchPatterns(t *testing.T) {
	tests := []struct {
		name     string
		match    interface{}
		expected []string
	}{
		{
			name:     "single string",
			match:    "/test",
			expected: []string{"/test"},
		},
		{
			name:     "array of strings",
			match:    []string{"/a", "/b", "/c"},
			expected: []string{"/a", "/b", "/c"},
		},
		{
			name:     "array of interfaces",
			match:    []interface{}{"/x", "/y"},
			expected: []string{"/x", "/y"},
		},
		{
			name:     "empty array",
			match:    []string{},
			expected: []string{},
		},
		{
			name:     "invalid type",
			match:    123,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{Match: tt.match}
			patterns := rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns)
		})
	}
}

// TestPatternMatcher_QueryParamExact tests exact query parameter matching
func TestPatternMatcher_QueryParamExact(t *testing.T) {
	tests := []struct {
		name         string
		matchQuery   map[string]interface{}
		url          string
		expectMatch  bool
		expectAction types.URLRuleAction
	}{
		{
			name: "exact match single param",
			matchQuery: map[string]interface{}{
				"q": "test",
			},
			url:          "https://example.com/search?q=test",
			expectMatch:  true,
			expectAction: types.ActionRender,
		},
		{
			name: "exact no match different value",
			matchQuery: map[string]interface{}{
				"q": "test",
			},
			url:         "https://example.com/search?q=other",
			expectMatch: false,
		},
		{
			name: "exact match multiple params AND logic",
			matchQuery: map[string]interface{}{
				"q":    "test",
				"page": "1",
			},
			url:          "https://example.com/search?q=test&page=1",
			expectMatch:  true,
			expectAction: types.ActionRender,
		},
		{
			name: "no match - missing required param",
			matchQuery: map[string]interface{}{
				"q": "test",
			},
			url:         "https://example.com/search",
			expectMatch: false,
		},
		{
			name: "no match - partial AND logic failure",
			matchQuery: map[string]interface{}{
				"q":    "test",
				"page": "1",
			},
			url:         "https://example.com/search?q=test&page=2",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:      "/search",
				MatchQuery: tt.matchQuery,
				Action:     types.ActionRender,
			}
			// Compile patterns for programmatically created rules
			err := rule.CompilePatterns()
			require.NoError(t, err)

			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expectMatch {
				require.NotNil(t, matched, "Expected pattern to match")
				assert.Equal(t, tt.expectAction, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_QueryParamWildcard tests wildcard (*) query parameter matching
func TestPatternMatcher_QueryParamWildcard(t *testing.T) {
	tests := []struct {
		name        string
		matchQuery  map[string]interface{}
		url         string
		expectMatch bool
	}{
		{
			name: "wildcard matches non-empty value",
			matchQuery: map[string]interface{}{
				"q": "*",
			},
			url:         "https://example.com/search?q=test",
			expectMatch: true,
		},
		{
			name: "wildcard matches any non-empty value",
			matchQuery: map[string]interface{}{
				"q": "*",
			},
			url:         "https://example.com/search?q=anything",
			expectMatch: true,
		},
		{
			name: "wildcard no match - empty value",
			matchQuery: map[string]interface{}{
				"q": "*",
			},
			url:         "https://example.com/search?q=",
			expectMatch: false,
		},
		{
			name: "wildcard no match - missing param",
			matchQuery: map[string]interface{}{
				"q": "*",
			},
			url:         "https://example.com/search",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:      "/search",
				MatchQuery: tt.matchQuery,
				Action:     types.ActionRender,
			}
			err := rule.CompilePatterns()
			require.NoError(t, err)

			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expectMatch {
				require.NotNil(t, matched, "Expected pattern to match")
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_QueryParamRegexp tests regexp query parameter matching
func TestPatternMatcher_QueryParamRegexp(t *testing.T) {
	tests := []struct {
		name        string
		matchPath   string
		matchQuery  map[string]interface{}
		url         string
		expectMatch bool
	}{
		{
			name:      "case-sensitive regexp - match",
			matchPath: "/api",
			matchQuery: map[string]interface{}{
				"version": "~v[0-9]+",
			},
			url:         "https://example.com/api?version=v1",
			expectMatch: true,
		},
		{
			name:      "case-sensitive regexp - match v2",
			matchPath: "/api",
			matchQuery: map[string]interface{}{
				"version": "~v[0-9]+",
			},
			url:         "https://example.com/api?version=v10",
			expectMatch: true,
		},
		{
			name:      "case-sensitive regexp - no match",
			matchPath: "/api",
			matchQuery: map[string]interface{}{
				"version": "~v[0-9]+",
			},
			url:         "https://example.com/api?version=V1",
			expectMatch: false,
		},
		{
			name:      "case-insensitive regexp - match lowercase",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"lang": "~*en[-_]?(us|gb|au)?",
			},
			url:         "https://example.com/page?lang=en-us",
			expectMatch: true,
		},
		{
			name:      "case-insensitive regexp - match uppercase",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"lang": "~*en[-_]?(us|gb|au)?",
			},
			url:         "https://example.com/page?lang=EN_GB",
			expectMatch: true,
		},
		{
			name:      "case-insensitive regexp - match mixed case",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"lang": "~*en[-_]?(us|gb|au)?",
			},
			url:         "https://example.com/page?lang=En-AU",
			expectMatch: true,
		},
		{
			name:      "regexp matches empty value with .*",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"debug": "~.*",
			},
			url:         "https://example.com/page?debug=",
			expectMatch: true,
		},
		{
			name:      "regexp matches non-empty value with .*",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"debug": "~.*",
			},
			url:         "https://example.com/page?debug=true",
			expectMatch: true,
		},
		{
			name:      "regexp with .+ requires non-empty",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"token": "~.+",
			},
			url:         "https://example.com/page?token=abc123",
			expectMatch: true,
		},
		{
			name:      "regexp with .+ no match - empty value",
			matchPath: "/page",
			matchQuery: map[string]interface{}{
				"token": "~.+",
			},
			url:         "https://example.com/page?token=",
			expectMatch: false,
		},
		{
			name:      "complex regexp pattern",
			matchPath: "/reports",
			matchQuery: map[string]interface{}{
				"type": "~(annual|quarterly|monthly)_[0-9]{4}",
			},
			url:         "https://example.com/reports?type=annual_2024",
			expectMatch: true,
		},
		{
			name:      "complex regexp no match",
			matchPath: "/reports",
			matchQuery: map[string]interface{}{
				"type": "~(annual|quarterly|monthly)_[0-9]{4}",
			},
			url:         "https://example.com/reports?type=daily_2024",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:      tt.matchPath,
				MatchQuery: tt.matchQuery,
				Action:     types.ActionRender,
			}
			err := rule.CompilePatterns()
			require.NoError(t, err)

			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expectMatch {
				require.NotNil(t, matched, "Expected pattern to match")
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_QueryParamArrayOR tests array matching with OR logic
func TestPatternMatcher_QueryParamArrayOR(t *testing.T) {
	tests := []struct {
		name        string
		matchQuery  map[string]interface{}
		url         string
		expectMatch bool
	}{
		{
			name: "array OR - match first value",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"electronics", "computers", "phones"},
			},
			url:         "https://example.com/products?category=electronics",
			expectMatch: true,
		},
		{
			name: "array OR - match second value",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"electronics", "computers", "phones"},
			},
			url:         "https://example.com/products?category=computers",
			expectMatch: true,
		},
		{
			name: "array OR - match third value",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"electronics", "computers", "phones"},
			},
			url:         "https://example.com/products?category=phones",
			expectMatch: true,
		},
		{
			name: "array OR - no match",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"electronics", "computers", "phones"},
			},
			url:         "https://example.com/products?category=books",
			expectMatch: false,
		},
		{
			name: "mixed array - exact and regexp",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"~tech.*", "electronics", "~*mobile.*"},
			},
			url:         "https://example.com/products?category=technology",
			expectMatch: true,
		},
		{
			name: "mixed array - match exact value",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"~tech.*", "electronics", "~*mobile.*"},
			},
			url:         "https://example.com/products?category=electronics",
			expectMatch: true,
		},
		{
			name: "mixed array - match case-insensitive regexp",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"~tech.*", "electronics", "~*mobile.*"},
			},
			url:         "https://example.com/products?category=MOBILE-PHONES",
			expectMatch: true,
		},
		{
			name: "mixed array - no match",
			matchQuery: map[string]interface{}{
				"category": []interface{}{"~tech.*", "electronics", "~*mobile.*"},
			},
			url:         "https://example.com/products?category=books",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.URLRule{
				Match:      "/products",
				MatchQuery: tt.matchQuery,
				Action:     types.ActionRender,
			}
			err := rule.CompilePatterns()
			require.NoError(t, err)

			matcher := NewPatternMatcher([]types.URLRule{rule})
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expectMatch {
				require.NotNil(t, matched, "Expected pattern to match")
			} else {
				assert.Nil(t, matched, "Expected pattern not to match")
			}
		})
	}
}

// TestPatternMatcher_QueryParamCombined tests path + query parameter matching
func TestPatternMatcher_QueryParamCombined(t *testing.T) {
	rules := []types.URLRule{
		{
			Match: "/search",
			MatchQuery: map[string]interface{}{
				"q": "*",
			},
			Action: types.ActionRender,
		},
		{
			Match: "/search",
			// No match_query - matches all
			Action: types.ActionBypass,
		},
		{
			Match: "/products",
			MatchQuery: map[string]interface{}{
				"category": []interface{}{"electronics", "computers"},
				"sort":     "~(price|name|rating)",
			},
			Action: types.ActionRender,
		},
	}

	// Compile patterns
	for i := range rules {
		err := rules[i].CompilePatterns()
		require.NoError(t, err)
	}

	matcher := NewPatternMatcher(rules)

	tests := []struct {
		name           string
		url            string
		expectedAction types.URLRuleAction
		expectMatch    bool
	}{
		{
			name:           "first rule - path and query match",
			url:            "https://example.com/search?q=test",
			expectedAction: types.ActionRender,
			expectMatch:    true,
		},
		{
			name:           "second rule - path matches, no query required",
			url:            "https://example.com/search",
			expectedAction: types.ActionBypass,
			expectMatch:    true,
		},
		{
			name:           "second rule - path matches, query param empty",
			url:            "https://example.com/search?q=",
			expectedAction: types.ActionBypass,
			expectMatch:    true,
		},
		{
			name:           "third rule - both query params match",
			url:            "https://example.com/products?category=electronics&sort=price",
			expectedAction: types.ActionRender,
			expectMatch:    true,
		},
		{
			name:           "third rule - category matches, sort regexp matches",
			url:            "https://example.com/products?category=computers&sort=rating",
			expectedAction: types.ActionRender,
			expectMatch:    true,
		},
		{
			name:        "third rule - category matches but sort doesn't",
			url:         "https://example.com/products?category=electronics&sort=date",
			expectMatch: false,
		},
		{
			name:        "third rule - sort matches but category doesn't",
			url:         "https://example.com/products?category=books&sort=price",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := matcher.FindMatchingRule(tt.url)

			if tt.expectMatch {
				require.NotNil(t, matched, "Expected a rule to match")
				assert.Equal(t, tt.expectedAction, matched.Action)
			} else {
				assert.Nil(t, matched, "Expected no rule to match")
			}
		})
	}
}

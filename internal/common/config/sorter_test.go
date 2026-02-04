package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// Helper function to create a basic URLRule for testing
func makeRule(match interface{}, action types.URLRuleAction) types.URLRule {
	rule := types.URLRule{
		Match:  match,
		Action: action,
	}
	// Compile patterns to populate metadata
	_ = rule.CompilePatterns()
	return rule
}

// Helper function to create an INVALID URLRule for testing error handling
// This rule has a valid pattern but corrupted metadata
func makeInvalidRule() types.URLRule {
	rule := types.URLRule{
		Match:  "~/[invalid", // Invalid regex - missing closing bracket
		Action: types.ActionRender,
	}
	// Intentionally DO NOT compile patterns to simulate corruption
	return rule
}

// Helper function to create a URLRule with query matching
func makeRuleWithQuery(match string, action types.URLRuleAction, query map[string]interface{}) types.URLRule {
	rule := types.URLRule{
		Match:      match,
		Action:     action,
		MatchQuery: query,
	}
	_ = rule.CompilePatterns()
	return rule
}

// TestSortURLRules_ExactVsWildcardVsRegexp tests pattern type ordering
func TestSortURLRules_ExactVsWildcardVsRegexp(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/api/*", types.ActionBypass),           // Wildcard
		makeRule("/", types.ActionRender),                // Exact
		makeRule("~/api/v[0-9]+/.*", types.ActionBypass), // Regexp
		makeRule("/api/v1/users", types.ActionRender),    // Exact
		makeRule("*.pdf", types.ActionBypass),            // Wildcard
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	// Expected order: Exact patterns first, then Wildcard, then Regexp
	// Within each group, more slashes first
	require.Len(t, sorted, 5)

	// Exact patterns (2 slashes, then 1 slash)
	assert.Equal(t, "/api/v1/users", sorted[0].GetMatchPatterns()[0]) // Exact, 3 slashes
	assert.Equal(t, "/", sorted[1].GetMatchPatterns()[0])             // Exact, 1 slash

	// Wildcard patterns (2 slashes, then 0 slashes)
	assert.Equal(t, "/api/*", sorted[2].GetMatchPatterns()[0]) // Wildcard, 2 slashes
	assert.Equal(t, "*.pdf", sorted[3].GetMatchPatterns()[0])  // Wildcard, 0 slashes

	// Regexp patterns
	assert.Equal(t, "~/api/v[0-9]+/.*", sorted[4].GetMatchPatterns()[0]) // Regexp, 2 slashes
}

// TestSortURLRules_QueryMatchingPriority tests query parameter matching priority
func TestSortURLRules_QueryMatchingPriority(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/search", types.ActionRender),                                            // No query
		makeRuleWithQuery("/search", types.ActionRender, map[string]interface{}{"q": "*"}), // With query
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 2)

	// Rule with query matching should come first
	assert.NotNil(t, sorted[0].MatchQuery)
	assert.Contains(t, sorted[0].MatchQuery, "q")

	// Rule without query matching should come second
	assert.Nil(t, sorted[1].MatchQuery)
}

// TestSortURLRules_SlashCountOrdering tests slash count ordering
func TestSortURLRules_SlashCountOrdering(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/", types.ActionRender),             // 1 slash
		makeRule("/api/public", types.ActionBypass),   // 2 slashes
		makeRule("/api/v1/users", types.ActionRender), // 3 slashes
		makeRule("/blog/*", types.ActionRender),       // 2 slashes
		makeRule("*.pdf", types.ActionBypass),         // 0 slashes
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 5)

	// All are exact patterns except *.pdf (wildcard)
	// Within exact patterns: more slashes first
	assert.Equal(t, "/api/v1/users", sorted[0].GetMatchPatterns()[0]) // Exact, 3 slashes
	assert.Equal(t, "/api/public", sorted[1].GetMatchPatterns()[0])   // Exact, 2 slashes
	assert.Equal(t, "/", sorted[2].GetMatchPatterns()[0])             // Exact, 1 slash

	// Wildcard patterns
	assert.Equal(t, "/blog/*", sorted[3].GetMatchPatterns()[0]) // Wildcard, 2 slashes
	assert.Equal(t, "*.pdf", sorted[4].GetMatchPatterns()[0])   // Wildcard, 0 slashes
}

// TestSortURLRules_StableSortPreservesOrder tests that stable sort preserves declaration order
func TestSortURLRules_StableSortPreservesOrder(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/page1", types.ActionRender), // Exact, 1 slash
		makeRule("/page2", types.ActionRender), // Exact, 1 slash
		makeRule("/page3", types.ActionBypass), // Exact, 1 slash
		makeRule("/page4", types.ActionRender), // Exact, 1 slash
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 4)

	// All have same priority, so declaration order should be preserved
	assert.Equal(t, "/page1", sorted[0].GetMatchPatterns()[0])
	assert.Equal(t, "/page2", sorted[1].GetMatchPatterns()[0])
	assert.Equal(t, "/page3", sorted[2].GetMatchPatterns()[0])
	assert.Equal(t, "/page4", sorted[3].GetMatchPatterns()[0])
}

// TestExpandMultiPattern_SinglePattern tests expansion of single pattern rules
func TestExpandMultiPattern_SinglePattern(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/single", types.ActionRender),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 1)
	assert.Equal(t, "/single", sorted[0].GetMatchPatterns()[0])
}

// TestExpandMultiPattern_MultiplePatterns tests expansion of multi-pattern rules
func TestExpandMultiPattern_MultiplePatterns(t *testing.T) {
	rules := []types.URLRule{
		makeRule([]string{"/admin/*", "/wp-admin/*", "/wp-login.php"}, types.ActionStatus403),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 3)

	// Should be sorted: Exact first (1 slash), then Wildcards (2 slashes each)
	assert.Equal(t, "/wp-login.php", sorted[0].GetMatchPatterns()[0]) // Exact, 1 slash
	assert.Equal(t, "/admin/*", sorted[1].GetMatchPatterns()[0])      // Wildcard, 2 slashes (first in declaration)
	assert.Equal(t, "/wp-admin/*", sorted[2].GetMatchPatterns()[0])   // Wildcard, 2 slashes (second in declaration)

	// All should have the same action
	assert.Equal(t, types.ActionStatus403, sorted[0].Action)
	assert.Equal(t, types.ActionStatus403, sorted[1].Action)
	assert.Equal(t, types.ActionStatus403, sorted[2].Action)
}

// TestExpandMultiPattern_InheritsConfiguration tests that expanded rules inherit configuration
func TestExpandMultiPattern_InheritsConfiguration(t *testing.T) {
	rules := []types.URLRule{
		{
			Match:  []string{"/blog/*", "/articles/*"},
			Action: types.ActionRender,
			Render: &types.RenderRuleConfig{
				Timeout:   ptrDuration(60000000000), // 60s in nanoseconds
				Dimension: "desktop",
			},
		},
	}
	_ = rules[0].CompilePatterns()

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 2)

	// Both should inherit the render configuration
	assert.NotNil(t, sorted[0].Render)
	assert.Equal(t, "desktop", sorted[0].Render.Dimension)
	assert.NotNil(t, sorted[0].Render.Timeout)

	assert.NotNil(t, sorted[1].Render)
	assert.Equal(t, "desktop", sorted[1].Render.Dimension)
	assert.NotNil(t, sorted[1].Render.Timeout)
}

// TestCountSlashes_BasicPaths tests slash counting for basic paths
func TestCountSlashes_BasicPaths(t *testing.T) {
	tests := []struct {
		pattern  string
		expected int
	}{
		{"/", 1},
		{"/cart", 1},
		{"/api/public", 2},
		{"/api/v1/users", 3},
		{"/blog/2024/jan/post", 4},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.expected, countSlashes(tt.pattern))
		})
	}
}

// TestCountSlashes_WildcardPatterns tests slash counting for wildcard patterns
func TestCountSlashes_WildcardPatterns(t *testing.T) {
	tests := []struct {
		pattern  string
		expected int
	}{
		{"*", 0},
		{"*.pdf", 0},
		{"/blog/*", 2},
		{"/blog/*/posts", 3},
		{"/api/*", 2},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.expected, countSlashes(tt.pattern))
		})
	}
}

// TestCountSlashes_RegexpPatterns tests slash counting for regexp patterns
func TestCountSlashes_RegexpPatterns(t *testing.T) {
	tests := []struct {
		pattern  string
		expected int
	}{
		{"~/api/v[0-9]+/.*", 3},       // Case-sensitive regexp (3 slashes: /api, /v[0-9]+, /.*)
		{"~*/archive/[0-9]{4}/.*", 3}, // Case-insensitive regexp (3 slashes: /archive, /[0-9]{4}, /.*)
		{"~/.*/[0-9]+$", 2},           // Regexp with wildcards (2 slashes: /.*, /[0-9]+)
		{"~*.*\\.(jpg|png|gif)$", 0},  // Extension regexp (no slashes)
		{"~/blog/[a-z]+/[0-9]{4}", 3}, // Multiple path segments (3 slashes: /blog, /[a-z]+, /[0-9]{4})
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.expected, countSlashes(tt.pattern))
		})
	}
}

// TestSortURLRules_EmptyRules tests sorting empty rules
func TestSortURLRules_EmptyRules(t *testing.T) {
	rules := []types.URLRule{}
	sorted, err := SortURLRules(rules)
	require.NoError(t, err)
	assert.Empty(t, sorted)
}

// TestSortURLRules_SingleRule tests sorting a single rule
func TestSortURLRules_SingleRule(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/single", types.ActionRender),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 1)
	assert.Equal(t, "/single", sorted[0].GetMatchPatterns()[0])
}

// TestSortURLRules_IdenticalPatterns tests handling of identical patterns
func TestSortURLRules_IdenticalPatterns(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/page", types.ActionRender),
		makeRule("/page", types.ActionBypass),
		makeRule("/page", types.ActionStatus403),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 3)

	// All should have same pattern, order preserved
	assert.Equal(t, "/page", sorted[0].GetMatchPatterns()[0])
	assert.Equal(t, "/page", sorted[1].GetMatchPatterns()[0])
	assert.Equal(t, "/page", sorted[2].GetMatchPatterns()[0])

	// Actions should be preserved in declaration order
	assert.Equal(t, types.ActionRender, sorted[0].Action)
	assert.Equal(t, types.ActionBypass, sorted[1].Action)
	assert.Equal(t, types.ActionStatus403, sorted[2].Action)
}

// TestSortURLRules_CatchAllPattern tests catch-all pattern placement
func TestSortURLRules_CatchAllPattern(t *testing.T) {
	rules := []types.URLRule{
		makeRule("*", types.ActionRender),         // Catch-all
		makeRule("/api/*", types.ActionBypass),    // More specific wildcard
		makeRule("/specific", types.ActionRender), // Exact
		makeRule("*.pdf", types.ActionBypass),     // Extension wildcard
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 4)

	// Exact patterns first
	assert.Equal(t, "/specific", sorted[0].GetMatchPatterns()[0]) // Exact, 1 slash

	// Wildcard patterns by slash count (desc), then declaration order
	assert.Equal(t, "/api/*", sorted[1].GetMatchPatterns()[0]) // Wildcard, 2 slashes
	// Both * and *.pdf have 0 slashes, so declaration order is preserved
	assert.Equal(t, "*", sorted[2].GetMatchPatterns()[0])     // Wildcard, 0 slashes (first)
	assert.Equal(t, "*.pdf", sorted[3].GetMatchPatterns()[0]) // Wildcard, 0 slashes (second)
}

// TestSortURLRules_NoSlashPatterns tests patterns with no slashes
func TestSortURLRules_NoSlashPatterns(t *testing.T) {
	rules := []types.URLRule{
		makeRule("*", types.ActionRender),     // Wildcard, 0 slashes
		makeRule("*.pdf", types.ActionBypass), // Wildcard, 0 slashes
		makeRule("*.jpg", types.ActionBypass), // Wildcard, 0 slashes
		makeRule("/path", types.ActionRender), // Exact, 1 slash
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 4)

	// Exact pattern first
	assert.Equal(t, "/path", sorted[0].GetMatchPatterns()[0]) // Exact, 1 slash

	// Wildcard patterns in declaration order (all have 0 slashes)
	assert.Equal(t, "*", sorted[1].GetMatchPatterns()[0])     // Wildcard, 0 slashes (first)
	assert.Equal(t, "*.pdf", sorted[2].GetMatchPatterns()[0]) // Wildcard, 0 slashes (second)
	assert.Equal(t, "*.jpg", sorted[3].GetMatchPatterns()[0]) // Wildcard, 0 slashes (third)
}

// TestSortURLRules_RealWorldExample tests a realistic configuration
func TestSortURLRules_RealWorldExample(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/api/*", types.ActionBypass),                                             // Wildcard, 2 slashes
		makeRule("/", types.ActionRender),                                                  // Exact, 1 slash (position 1)
		makeRuleWithQuery("/search", types.ActionRender, map[string]interface{}{"q": "*"}), // Exact + query, 1 slash
		makeRule("/api/v1/users", types.ActionRender),                                      // Exact, 3 slashes
		makeRule("*.pdf", types.ActionBypass),                                              // Wildcard, 0 slashes (position 4)
		makeRule("~/api/v[0-9]+/.*", types.ActionBypass),                                   // Regexp, 2 slashes
		makeRule("/search", types.ActionRender),                                            // Exact, 1 slash (position 6)
		makeRule("*", types.ActionRender),                                                  // Catch-all, 0 slashes (position 7)
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 8)

	// Group 1: Exact patterns with query matching
	assert.Equal(t, "/search", sorted[0].GetMatchPatterns()[0])
	assert.NotNil(t, sorted[0].MatchQuery)

	// Group 2: Exact patterns without query (by slash count desc, then declaration order)
	assert.Equal(t, "/api/v1/users", sorted[1].GetMatchPatterns()[0]) // 3 slashes
	assert.Equal(t, "/", sorted[2].GetMatchPatterns()[0])             // 1 slash (declared before /search)
	assert.Equal(t, "/search", sorted[3].GetMatchPatterns()[0])       // 1 slash (declared after /)

	// Group 3: Wildcard patterns (by slash count desc, then declaration order)
	assert.Equal(t, "/api/*", sorted[4].GetMatchPatterns()[0]) // 2 slashes
	assert.Equal(t, "*.pdf", sorted[5].GetMatchPatterns()[0])  // 0 slashes (declared before *)
	assert.Equal(t, "*", sorted[6].GetMatchPatterns()[0])      // 0 slashes (declared after *.pdf)

	// Group 4: Regexp patterns
	assert.Equal(t, "~/api/v[0-9]+/.*", sorted[7].GetMatchPatterns()[0]) // 2 slashes
}

// TestSortURLRules_AllPatternTypes tests complex scenario with all pattern types
func TestSortURLRules_AllPatternTypes(t *testing.T) {
	rules := []types.URLRule{
		makeRule("~/blog/[0-9]{4}/.*", types.ActionRender),                                 // Regexp, 2 slashes
		makeRule("/blog/*", types.ActionRender),                                            // Wildcard, 2 slashes
		makeRule("/blog/2024/post", types.ActionRender),                                    // Exact, 3 slashes
		makeRuleWithQuery("/blog", types.ActionRender, map[string]interface{}{"tag": "*"}), // Exact + query, 1 slash
		makeRule("/blog", types.ActionRender),                                              // Exact, 1 slash
		makeRule("*.html", types.ActionBypass),                                             // Wildcard, 0 slashes
		makeRule("~*.*\\.(jpg|png)$", types.ActionBypass),                                  // Regexp, 0 slashes
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	require.Len(t, sorted, 7)

	// Exact with query
	assert.Equal(t, "/blog", sorted[0].GetMatchPatterns()[0])
	assert.NotNil(t, sorted[0].MatchQuery)

	// Exact without query (by slash count)
	assert.Equal(t, "/blog/2024/post", sorted[1].GetMatchPatterns()[0]) // 3 slashes
	assert.Equal(t, "/blog", sorted[2].GetMatchPatterns()[0])           // 1 slash

	// Wildcard patterns
	assert.Equal(t, "/blog/*", sorted[3].GetMatchPatterns()[0]) // 2 slashes
	assert.Equal(t, "*.html", sorted[4].GetMatchPatterns()[0])  // 0 slashes

	// Regexp patterns
	assert.Equal(t, "~/blog/[0-9]{4}/.*", sorted[5].GetMatchPatterns()[0]) // 2 slashes
	assert.Equal(t, "~*.*\\.(jpg|png)$", sorted[6].GetMatchPatterns()[0])  // 0 slashes
}

// TestSortURLRules_DoesNotModifyInput tests immutability of input
func TestSortURLRules_DoesNotModifyInput(t *testing.T) {
	rules := []types.URLRule{
		makeRule("*", types.ActionRender),             // Wildcard - should move to end
		makeRule("/api/v1/users", types.ActionBypass), // Exact - should move to start
		makeRule("/second", types.ActionRender),       // Exact - middle
	}

	// Create a copy of original patterns for comparison
	originalPatterns := make([]string, len(rules))
	for i, rule := range rules {
		originalPatterns[i] = rule.GetMatchPatterns()[0]
	}

	// Sort rules
	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	// Verify input was not modified
	require.Len(t, rules, 3)
	for i, rule := range rules {
		assert.Equal(t, originalPatterns[i], rule.GetMatchPatterns()[0],
			"Original input should not be modified")
	}

	// Verify sorted result is different from input
	require.Len(t, sorted, 3)
	// First item should be /api/v1/users (exact with most slashes), not * (wildcard)
	assert.Equal(t, "/api/v1/users", sorted[0].GetMatchPatterns()[0])
	assert.Equal(t, "*", rules[0].GetMatchPatterns()[0], "Input should not be modified")
}

// TestSortURLRules_ErrorHandling tests that invalid patterns cause errors instead of silent dropping
func TestSortURLRules_ErrorHandling(t *testing.T) {
	// Create a rule with an invalid regex pattern
	invalidRule := makeInvalidRule()

	rules := []types.URLRule{
		makeRule("/valid", types.ActionRender),
		invalidRule,
		makeRule("/another-valid", types.ActionBypass),
	}

	// SortURLRules should return an error instead of silently dropping the rule
	sorted, err := SortURLRules(rules)

	// Verify that we get an error
	require.Error(t, err, "Expected error for invalid regex pattern")
	assert.Contains(t, err.Error(), "failed to compile pattern")
	assert.Contains(t, err.Error(), "~/[invalid")
	assert.Nil(t, sorted, "Sorted result should be nil when error occurs")
}

// TestSortURLRules_LargeRuleSet tests performance with larger rule set
func TestSortURLRules_LargeRuleSet(t *testing.T) {
	// Create 100 rules with various patterns
	rules := make([]types.URLRule, 0, 100)
	for i := 0; i < 100; i++ {
		var pattern string
		switch i % 3 {
		case 0:
			pattern = "/exact/path" + string(rune('a'+i%26))
		case 1:
			pattern = "/wildcard/*/path" + string(rune('a'+i%26))
		case 2:
			pattern = "~/regexp/[0-9]+/" + string(rune('a'+i%26))
		}
		rules = append(rules, makeRule(pattern, types.ActionRender))
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	// Should have same number of rules
	assert.Len(t, sorted, 100)

	// Verify sorting: Exact patterns should come first, then Wildcards, then Regexp
	// Count each type in sorted result
	exactCount := 0
	wildcardCount := 0
	regexpCount := 0

	for _, rule := range sorted {
		pattern := rule.GetMatchPatterns()[0]
		if strings.HasPrefix(pattern, "~") {
			regexpCount++
		} else if strings.Contains(pattern, "*") {
			wildcardCount++
		} else {
			exactCount++
		}
	}

	// Should have roughly equal distribution
	assert.InDelta(t, 33, exactCount, 2)
	assert.InDelta(t, 34, wildcardCount, 2)
	assert.InDelta(t, 33, regexpCount, 2)

	// Verify exact patterns come first (first ~33 rules)
	for i := 0; i < exactCount; i++ {
		pattern := sorted[i].GetMatchPatterns()[0]
		assert.NotContains(t, pattern, "*", "Exact patterns should come first")
		assert.False(t, strings.HasPrefix(pattern, "~"), "Exact patterns should come first")
	}
}

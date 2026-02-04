package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// expandedRule represents a URL rule after multi-pattern expansion
// with metadata used for sorting
type expandedRule struct {
	rule          types.URLRule
	pattern       string
	patternType   pattern.PatternType
	hasQuery      bool
	slashCount    int
	originalIndex int
}

// SortURLRules sorts URL rules by specificity and expands multi-pattern rules.
// Returns a new slice with sorted rules (does not modify input).
//
// Sorting priority:
//  1. Pattern Type: Exact > Wildcard > Regexp
//  2. Query Matching: Has match_query > No match_query
//  3. Slash Count: Descending (more slashes first)
//  4. Declaration Order: Stable sort (preserve original order)
//
// Multi-pattern rules are expanded into separate rules before sorting.
// Returns error if pattern compilation fails during expansion.
func SortURLRules(rules []types.URLRule) ([]types.URLRule, error) {
	if len(rules) == 0 {
		return []types.URLRule{}, nil
	}

	// Step 1: Expand multi-pattern rules
	expanded, err := expandMultiPatternRules(rules)
	if err != nil {
		return nil, err
	}

	// Step 2: Sort by priority hierarchy (stable sort)
	sort.SliceStable(expanded, func(i, j int) bool {
		return compareExpandedRules(&expanded[i], &expanded[j])
	})

	// Step 3: Convert back to URLRule slice
	result := make([]types.URLRule, len(expanded))
	for i, er := range expanded {
		result[i] = er.rule
	}

	return result, nil
}

// expandMultiPatternRules expands rules with multiple patterns into separate rules
func expandMultiPatternRules(rules []types.URLRule) ([]expandedRule, error) {
	expanded := make([]expandedRule, 0, len(rules))
	originalIndex := 0

	for _, rule := range rules {
		patterns := rule.GetMatchPatterns()
		if len(patterns) == 0 {
			// Skip rules with no patterns (shouldn't happen after validation)
			continue
		}

		for _, pattern := range patterns {
			// Create a new rule for this pattern
			newRule := types.URLRule{
				Match:          pattern, // Single pattern string
				Action:         rule.Action,
				MatchQuery:     rule.MatchQuery,
				Render:         rule.Render,
				Bypass:         rule.Bypass,
				Status:         rule.Status,
				TrackingParams: rule.TrackingParams,
				CacheSharding:  rule.CacheSharding,
				BothitRecache:  rule.BothitRecache,
				Headers:        rule.Headers,
			}

			// Compile patterns for the new rule (sets matchPatterns and patternMetadata)
			// This is needed because we changed the Match field
			if err := newRule.CompilePatterns(); err != nil {
				// This should never happen since patterns were already compiled during YAML unmarshaling.
				// If it does, it indicates a bug in CompilePatterns() or data corruption.
				// Fail loudly rather than silently dropping rules (especially security rules).
				return nil, fmt.Errorf("failed to compile pattern '%s' during rule expansion (action=%s): %w", pattern, rule.Action, err)
			}

			// Create expanded rule with metadata
			expanded = append(expanded, expandedRule{
				rule:          newRule,
				pattern:       pattern,
				patternType:   newRule.GetCompiledPattern(0).Type,
				hasQuery:      len(rule.MatchQuery) > 0,
				slashCount:    countSlashes(pattern),
				originalIndex: originalIndex,
			})

			originalIndex++
		}
	}

	return expanded, nil
}

// compareExpandedRules compares two expanded rules for sorting
// Returns true if a should come before b
func compareExpandedRules(a, b *expandedRule) bool {
	// Priority 1: Pattern Type (Exact > Wildcard > Regexp)
	// PatternTypeWildcard = 0, PatternTypeRegexp = 1, PatternTypeExact = 2
	// Map to priority: Exact (2) → 3, Wildcard (0) → 2, Regexp (1) → 1
	if a.patternType != b.patternType {
		aPriority := getPatternTypePriority(a.patternType)
		bPriority := getPatternTypePriority(b.patternType)
		return aPriority > bPriority
	}

	// Priority 2: Query Matching (has match_query > no match_query)
	if a.hasQuery != b.hasQuery {
		return a.hasQuery // true before false
	}

	// Priority 3: Slash Count (more slashes = more specific)
	if a.slashCount != b.slashCount {
		return a.slashCount > b.slashCount
	}

	// Priority 4: Original Index (stable sort - preserve declaration order)
	return a.originalIndex < b.originalIndex
}

// getPatternTypePriority maps PatternType to sorting priority
// Higher value = higher priority (comes first)
func getPatternTypePriority(pt pattern.PatternType) int {
	switch pt {
	case pattern.PatternTypeExact:
		return 3 // Highest priority
	case pattern.PatternTypeWildcard:
		return 2 // Medium priority
	case pattern.PatternTypeRegexp:
		return 1 // Lowest priority
	default:
		return 0
	}
}

// countSlashes counts the number of slashes in a pattern
// Used to determine path specificity (more slashes = more specific)
func countSlashes(pattern string) int {
	// For regexp patterns, strip the prefix first
	if strings.HasPrefix(pattern, "~*") {
		pattern = pattern[2:]
	} else if strings.HasPrefix(pattern, "~") {
		pattern = pattern[1:]
	}

	return strings.Count(pattern, "/")
}

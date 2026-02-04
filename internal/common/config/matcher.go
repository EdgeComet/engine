package config

import (
	"net/url"

	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// PatternMatcher handles URL pattern matching for URL rules
type PatternMatcher struct {
	rules []types.URLRule
}

// NewPatternMatcher creates a new pattern matcher with the given URL rules
func NewPatternMatcher(rules []types.URLRule) *PatternMatcher {
	return &PatternMatcher{
		rules: rules,
	}
}

// FindMatchingRule finds the first matching URL rule for the given URL
// Returns the matched rule and its index, or (nil, -1) if no rule matches
// Top-to-bottom evaluation, first match wins
func (pm *PatternMatcher) FindMatchingRule(targetURL string) (*types.URLRule, int) {
	// Parse URL to extract path for matching
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		// If URL is malformed, no rule matches
		return nil, -1
	}

	// Match against path only (query parameters are ignored for pattern matching)
	matchPath := parsedURL.Path

	// Parse query for match_query matching (if needed)
	queryValues := parsedURL.Query()

	// Evaluate rules top-to-bottom
	for i := range pm.rules {
		rule := &pm.rules[i]
		patterns := rule.GetMatchPatterns()

		// Check if any pattern matches the path
		pathMatched := false
		for patternIdx, pattern := range patterns {
			if pm.matchPattern(matchPath, pattern, patternIdx, rule) {
				pathMatched = true
				break
			}
		}

		if !pathMatched {
			continue
		}

		// If path matched and match_query is specified, check query parameters
		if rule.MatchQuery != nil {
			if !matchQueryParams(queryValues, rule) {
				continue
			}
		}

		// All matchers passed
		return rule, i
	}

	return nil, -1
}

// matchPattern matches a path against a pattern using the unified pattern package
func (pm *PatternMatcher) matchPattern(path, pat string, patternIdx int, rule *types.URLRule) bool {
	compiled := rule.GetCompiledPattern(patternIdx)
	if compiled == nil {
		// Fallback: compile on the fly (shouldn't happen in normal operation)
		compiled, _ = pattern.Compile(pat)
		if compiled == nil {
			return false
		}
	}

	return compiled.Match(path)
}

// matchQueryParams checks if query parameters match the match_query conditions
// All conditions must match (AND logic)
func matchQueryParams(queryValues url.Values, rule *types.URLRule) bool {
	// Check if any query param conditions exist
	if rule.MatchQuery == nil || len(rule.QueryParamMetadata) == 0 {
		return true
	}

	// Validate all query param conditions (AND logic)
	for key, patterns := range rule.QueryParamMetadata {
		actualValues := queryValues[key]

		// Key not present in query string
		if len(actualValues) == 0 {
			return false
		}

		// Use first value (nginx behavior for multiple values with same key)
		actualValue := actualValues[0]

		// Match against patterns (OR logic within patterns array)
		if !matchQueryValue(actualValue, patterns) {
			return false
		}
	}

	return true
}

// matchQueryValue matches a single query parameter value against an array of patterns
// Returns true if ANY pattern matches (OR logic)
// Special case: Wildcard (*) in query params means "non-empty value required"
func matchQueryValue(actual string, patterns []*pattern.Pattern) bool {
	for _, pat := range patterns {
		// Special behavior for query parameter wildcards
		// Wildcard (*) means "parameter must exist with non-empty value"
		if pat.Type == pattern.PatternTypeWildcard && pat.Original == "*" {
			if actual != "" {
				return true
			}
			continue
		}

		// Use standard pattern matching for all other cases
		if pat.Match(actual) {
			return true
		}
	}

	return false
}

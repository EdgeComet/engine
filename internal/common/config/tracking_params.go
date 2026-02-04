package config

import (
	"fmt"

	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// Built-in default tracking parameters to strip
// These are common marketing and analytics parameters that should be removed
var defaultTrackingParams = []string{
	"utm_source",
	"utm_content",
	"utm_medium",
	"utm_campaign",
	"utm_term",
	"gclid",
	"fbclid",
	"msclkid",
	"_ga",
	"_gl",
	"mc_cid",
	"mc_eid",
	"_ke",
	"ref",
	"referrer",
}

// CompileStripPatterns compiles a list of pattern strings into CompiledStripPattern structures
// Pattern types:
//   - Exact ("utm_source"): case-insensitive exact match
//   - Wildcard ("utm_*"): case-insensitive pattern matching
//   - Regexp ("~^utm_.*$"): case-sensitive regular expression
//   - Regexp ("~*^ga_.*$"): case-insensitive regular expression
//
// Returns error if any pattern is invalid
func CompileStripPatterns(patterns []string) ([]CompiledStripPattern, error) {
	compiled := make([]CompiledStripPattern, 0, len(patterns))

	for _, pat := range patterns {
		if pat == "" {
			continue // Skip empty patterns
		}

		// Use unified pattern compilation
		p, err := pattern.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern '%s': %w", pat, err)
		}

		compiled = append(compiled, CompiledStripPattern{
			Compiled: p,
		})
	}

	return compiled, nil
}

// ShouldStripParam checks if a parameter name should be stripped based on compiled patterns
// Pattern matching behavior:
//   - Exact: case-insensitive ("utm_source" matches "utm_source", "UTM_SOURCE", etc.)
//   - Wildcard: case-insensitive ("utm_*" matches "utm_source", "UTM_SOURCE", etc.)
//   - Regexp: respects pattern's case sensitivity flag (~, ~*)
//
// Exported for use by normalizer package
func ShouldStripParam(paramName string, patterns []CompiledStripPattern) bool {
	for _, pat := range patterns {
		if pat.Compiled != nil && pat.Compiled.Match(paramName) {
			return true
		}
	}

	return false
}

// ValidateTrackingParams validates tracking parameter configuration
// Returns an error if any patterns are invalid (e.g., invalid regex)
func ValidateTrackingParams(tp *types.TrackingParamsConfig, level string) error {
	if tp == nil {
		return nil
	}

	// Validate and pre-compile all patterns
	compiled, err := CompileStripPatterns(tp.Params)
	if err != nil {
		return fmt.Errorf("invalid tracking_params at %s level: %w", level, err)
	}

	// Check for redundant patterns (warn but don't fail)
	redundant := findRedundantPatterns(compiled)
	for _, pattern := range redundant {
		// Note: Warnings will be logged by the caller (config loader)
		// We return the pattern info for structured logging
		_ = pattern // Keep for future structured logging
	}

	return nil
}

// findRedundantPatterns detects patterns that are covered by other patterns
// Returns a list of redundant pattern strings for logging
func findRedundantPatterns(patterns []CompiledStripPattern) []string {
	redundant := []string{}

	// Check each pattern against all others
	for i, pat := range patterns {
		for j, other := range patterns {
			if i == j {
				continue
			}

			// Check if pattern is covered by other
			if isPatternRedundant(pat, other) {
				if pat.Compiled != nil {
					redundant = append(redundant, pat.Compiled.Original)
				}
				break
			}
		}
	}

	return redundant
}

// isPatternRedundant checks if a pattern is fully covered by another pattern
func isPatternRedundant(pat, other CompiledStripPattern) bool {
	if pat.Compiled == nil || other.Compiled == nil {
		return false
	}

	// Exact patterns covered by wildcard
	// Example: "utm_source" is redundant if "utm_*" exists
	if pat.Compiled.Type == pattern.PatternTypeExact && other.Compiled.Type == pattern.PatternTypeWildcard {
		if other.Compiled.Match(pat.Compiled.CleanPattern) {
			return true
		}
	}

	// Wildcard covered by broader wildcard
	// Example: "utm_*" is redundant if "*" exists
	if pat.Compiled.Type == pattern.PatternTypeWildcard && other.Compiled.Type == pattern.PatternTypeWildcard {
		// Check if pat.Original matches other.Original wildcard
		// This handles cases like "utm_source_*" being covered by "utm_*"
		if other.Compiled.Match(pat.Compiled.Original) && pat.Compiled.Original != other.Compiled.Original {
			return true
		}
	}

	// Exact/Wildcard covered by regex (simplified check)
	// Example: "utm_source" is redundant if "~^utm_.*$" exists
	if other.Compiled.Type == pattern.PatternTypeRegexp {
		if pat.Compiled.Type == pattern.PatternTypeExact {
			if other.Compiled.Match(pat.Compiled.CleanPattern) {
				return true
			}
		}
	}

	return false
}

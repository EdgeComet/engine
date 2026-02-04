// Package pattern provides unified pattern matching across the codebase.
//
// Pattern Matching Behavior:
//
//   - Exact (no prefix): Case-insensitive exact match
//     Example: "Googlebot" matches "Googlebot", "GOOGLEBOT", "googlebot"
//
//   - Wildcard (*): Case-insensitive pattern with * matching any characters
//     Example: "*googlebot*" matches "Googlebot", "GOOGLEBOT/2.1", "my-googlebot"
//
//   - Regexp (~): Case-sensitive regular expression
//     Example: "~^Googlebot/[0-9.]+$" matches "Googlebot/2.1" but not "googlebot/2.1"
//
//   - Regexp (~*): Case-insensitive regular expression
//     Example: "~*googlebot|bingbot" matches "Googlebot", "BINGBOT", "GoogleBot"
package pattern

import (
	"fmt"
	"regexp"
	"strings"
)

// PatternType defines the type of pattern matching
type PatternType int

const (
	PatternTypeWildcard PatternType = iota
	PatternTypeRegexp
	PatternTypeExact
)

// Pattern represents a compiled pattern ready for matching
type Pattern struct {
	Original        string         // Original pattern string
	Type            PatternType    // Pattern type: Exact, Wildcard, or Regexp
	CleanPattern    string         // Pattern with prefix removed (for regexp)
	CaseInsensitive bool           // For ~* prefix
	compiledRegexp  *regexp.Regexp // Pre-compiled regexp (nil for exact/wildcard)
}

// DetectPatternType determines the pattern matching type
// Returns: PatternType, clean pattern (prefix removed), case-insensitive flag
func DetectPatternType(pattern string) (PatternType, string, bool) {
	// Check for regexp prefix
	if strings.HasPrefix(pattern, "~*") {
		return PatternTypeRegexp, pattern[2:], true // case-insensitive
	}
	if strings.HasPrefix(pattern, "~") {
		return PatternTypeRegexp, pattern[1:], false // case-sensitive
	}

	// Check for wildcard
	if strings.Contains(pattern, "*") {
		return PatternTypeWildcard, pattern, false
	}

	// Default to exact match
	return PatternTypeExact, pattern, false
}

// Compile pre-compiles a pattern for efficient matching
// This function should be called once during configuration loading
func Compile(pattern string) (*Pattern, error) {
	if pattern == "" {
		return nil, fmt.Errorf("pattern cannot be empty")
	}

	patternType, cleanPattern, caseInsensitive := DetectPatternType(pattern)

	p := &Pattern{
		Original:        pattern,
		Type:            patternType,
		CleanPattern:    cleanPattern,
		CaseInsensitive: caseInsensitive,
	}

	// Pre-compile regexp if needed
	if patternType == PatternTypeRegexp {
		var re *regexp.Regexp
		var err error

		if caseInsensitive {
			re, err = regexp.Compile("(?i)" + cleanPattern)
		} else {
			re, err = regexp.Compile(cleanPattern)
		}

		if err != nil {
			return nil, fmt.Errorf("invalid regexp pattern '%s': %w", pattern, err)
		}

		p.compiledRegexp = re
	}

	return p, nil
}

// Match tests if input matches the compiled pattern
// This is a method on Pattern, similar to regexp.Regexp.MatchString()
func (p *Pattern) Match(input string) bool {
	if p == nil {
		return false
	}

	switch p.Type {
	case PatternTypeRegexp:
		if p.compiledRegexp == nil {
			return false
		}
		return p.compiledRegexp.MatchString(input)

	case PatternTypeWildcard:
		// Wildcard matching is case-insensitive
		return MatchWildcard(strings.ToLower(input), strings.ToLower(p.CleanPattern))

	case PatternTypeExact:
		// Exact matching is case-insensitive
		return strings.EqualFold(input, p.CleanPattern)

	default:
		return false
	}
}

// MatchWildcard performs wildcard pattern matching on raw strings (utility function)
// This is a low-level utility for special cases where you need direct wildcard
// matching without compiling a Pattern. For normal use, prefer Compile() + Match().
//
// The wildcard * matches any sequence of characters (including none)
// Multiple wildcards are supported
//
// Examples:
//   - MatchWildcard("/blog/post", "/blog/*") → true
//   - MatchWildcard("/blog/2024/post", "/blog/*") → true (recursive matching)
//   - MatchWildcard("document.pdf", "*.pdf") → true
//   - MatchWildcard("anything", "*") → true (catch-all)
//
// Note: The wildcard * is always recursive and matches multiple path segments
func MatchWildcard(text, pattern string) bool {
	// If no wildcard, do exact match
	if !strings.Contains(pattern, "*") {
		return text == pattern
	}

	// Split pattern by wildcards
	parts := strings.Split(pattern, "*")

	// Text must start with first part
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]

	// Text must end with last part
	if !strings.HasSuffix(text, parts[len(parts)-1]) {
		return false
	}
	text = text[:len(text)-len(parts[len(parts)-1])]

	// Check middle parts exist in order
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		idx := strings.Index(text, parts[i])
		if idx == -1 {
			return false
		}
		text = text[idx+len(parts[i]):]
	}

	return true
}

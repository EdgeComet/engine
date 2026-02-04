package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

// TestCompileStripPatterns tests pattern compilation
func TestCompileStripPatterns(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		expectError bool
		expectCount int
	}{
		{
			name:        "empty patterns",
			patterns:    []string{},
			expectError: false,
			expectCount: 0,
		},
		{
			name:        "exact patterns",
			patterns:    []string{"utm_source", "utm_medium", "gclid"},
			expectError: false,
			expectCount: 3,
		},
		{
			name:        "wildcard patterns",
			patterns:    []string{"utm_*", "ga_*", "mc_*"},
			expectError: false,
			expectCount: 3,
		},
		{
			name:        "case-sensitive regexp",
			patterns:    []string{"~^utm_.*$", "~^fb.*$"},
			expectError: false,
			expectCount: 2,
		},
		{
			name:        "case-insensitive regexp",
			patterns:    []string{"~*^utm_.*$", "~*^ga_.*$"},
			expectError: false,
			expectCount: 2,
		},
		{
			name:        "mixed pattern types",
			patterns:    []string{"utm_source", "ga_*", "~^fb.*$"},
			expectError: false,
			expectCount: 3,
		},
		{
			name:        "invalid regexp",
			patterns:    []string{"~^[invalid(regexp"},
			expectError: true,
		},
		{
			name:        "skip empty strings",
			patterns:    []string{"utm_source", "", "gclid"},
			expectError: false,
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := CompileStripPatterns(tt.patterns)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, compiled, tt.expectCount)
		})
	}
}

// TestShouldStripParam_ExactMatch tests exact pattern matching
func TestShouldStripParam_ExactMatch(t *testing.T) {
	patterns, err := CompileStripPatterns([]string{"utm_source", "gclid", "fbclid"})
	require.NoError(t, err)

	tests := []struct {
		paramName string
		expected  bool
	}{
		{"utm_source", true},
		{"UTM_SOURCE", true}, // Case-insensitive: exact match is case-insensitive
		{"Utm_Source", true}, // Case-insensitive: exact match is case-insensitive
		{"gclid", true},
		{"fbclid", true},
		{"utm_medium", false}, // Not in list
		{"ref", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.paramName, func(t *testing.T) {
			result := ShouldStripParam(tt.paramName, patterns)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestShouldStripParam_WildcardMatch tests wildcard pattern matching
func TestShouldStripParam_WildcardMatch(t *testing.T) {
	patterns, err := CompileStripPatterns([]string{"utm_*", "ga_*", "_ke"})
	require.NoError(t, err)

	tests := []struct {
		paramName string
		expected  bool
	}{
		// utm_* matches
		{"utm_source", true},
		{"utm_medium", true},
		{"utm_campaign", true},
		{"UTM_SOURCE", true},   // Case-insensitive
		{"UTM_CAMPAIGN", true}, // Case-insensitive
		{"utm_anything", true},

		// ga_* matches
		{"ga_session", true},
		{"ga_client", true},
		{"GA_SESSION", true}, // Case-insensitive

		// _ke exact match
		{"_ke", true},

		// Non-matches
		{"gclid", false},
		{"fbclid", false},
		{"ref", false},
		{"utm", false}, // Doesn't match utm_*
		{"ga", false},  // Doesn't match ga_*
	}

	for _, tt := range tests {
		t.Run(tt.paramName, func(t *testing.T) {
			result := ShouldStripParam(tt.paramName, patterns)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestShouldStripParam_RegexpMatch tests regexp pattern matching
func TestShouldStripParam_RegexpMatch(t *testing.T) {
	patterns, err := CompileStripPatterns([]string{
		"~^utm_.*$",        // Case-sensitive: utm_*
		"~*^ga_.*$",        // Case-insensitive: ga_*
		"~^custom_[0-9]+$", // Case-sensitive: custom_<digits>
	})
	require.NoError(t, err)

	tests := []struct {
		paramName string
		expected  bool
	}{
		// ~^utm_.*$ (case-sensitive)
		{"utm_source", true},
		{"utm_medium", true},
		{"UTM_SOURCE", false}, // Case-sensitive regexp fails

		// ~*^ga_.*$ (case-insensitive)
		{"ga_session", true},
		{"GA_SESSION", true}, // Case-insensitive regexp passes
		{"ga_client", true},

		// ~^custom_[0-9]+$
		{"custom_123", true},
		{"custom_456", true},
		{"custom_abc", false}, // Not digits
		{"CUSTOM_123", false}, // Case-sensitive

		// Non-matches
		{"gclid", false},
		{"fbclid", false},
	}

	for _, tt := range tests {
		t.Run(tt.paramName, func(t *testing.T) {
			result := ShouldStripParam(tt.paramName, patterns)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestValidateTrackingParams tests configuration validation
func TestValidateTrackingParams(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.TrackingParamsConfig
		level       string
		expectError bool
	}{
		{
			name:        "nil config",
			config:      nil,
			level:       "global",
			expectError: false,
		},
		{
			name: "valid exact patterns",
			config: &types.TrackingParamsConfig{
				Params: []string{"utm_source", "gclid", "fbclid"},
			},
			level:       "host",
			expectError: false,
		},
		{
			name: "valid wildcard patterns",
			config: &types.TrackingParamsConfig{
				Params: []string{"utm_*", "ga_*"},
			},
			level:       "host",
			expectError: false,
		},
		{
			name: "valid regexp patterns",
			config: &types.TrackingParamsConfig{
				Params: []string{"~^utm_.*$", "~*^ga_.*$"},
			},
			level:       "host",
			expectError: false,
		},
		{
			name: "invalid regexp pattern",
			config: &types.TrackingParamsConfig{
				Params: []string{"~^[invalid(regexp"},
			},
			level:       "global",
			expectError: true,
		},
		{
			name: "mixed valid patterns",
			config: &types.TrackingParamsConfig{
				Params: []string{"utm_source", "ga_*", "~^fb.*$"},
			},
			level:       "pattern",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrackingParams(tt.config, tt.level)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.level)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestFindRedundantPatterns tests redundant pattern detection
func TestFindRedundantPatterns(t *testing.T) {
	tests := []struct {
		name              string
		patterns          []string
		expectedRedundant []string
		checkContains     bool // If true, check Contains instead of exact match
	}{
		{
			name:              "no redundancy",
			patterns:          []string{"utm_source", "gclid", "fbclid"},
			expectedRedundant: []string{},
		},
		{
			name:              "exact covered by wildcard",
			patterns:          []string{"utm_source", "utm_*"},
			expectedRedundant: []string{"utm_source"},
		},
		{
			name:              "multiple exact covered by wildcard",
			patterns:          []string{"utm_source", "utm_medium", "utm_*"},
			expectedRedundant: []string{"utm_source", "utm_medium"},
			checkContains:     true,
		},
		{
			name:              "wildcard covered by broader wildcard",
			patterns:          []string{"utm_*", "*"},
			expectedRedundant: []string{"utm_*"},
		},
		{
			name:              "exact covered by regexp",
			patterns:          []string{"utm_source", "~^utm_.*$"},
			expectedRedundant: []string{"utm_source"},
		},
		{
			name:              "no redundancy with different prefixes",
			patterns:          []string{"utm_*", "ga_*", "fb_*"},
			expectedRedundant: []string{},
		},
		{
			name:              "case-insensitive wildcard redundancy",
			patterns:          []string{"utm_*", "UTM_*"},
			expectedRedundant: []string{"utm_*", "UTM_*"}, // Both are redundant (duplicates)
		},
		{
			name:              "case-insensitive broader wildcard coverage",
			patterns:          []string{"utm_source_*", "UTM_*"},
			expectedRedundant: []string{"utm_source_*"},
		},
		{
			name:              "mixed case wildcard variants",
			patterns:          []string{"GA_*", "ga_session_*", "ga_*"},
			expectedRedundant: []string{"ga_session_*"},
			checkContains:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := CompileStripPatterns(tt.patterns)
			require.NoError(t, err)

			redundant := findRedundantPatterns(compiled)

			if len(tt.expectedRedundant) == 0 {
				assert.Empty(t, redundant)
			} else if tt.checkContains {
				// Check that all expected patterns are in redundant list
				for _, expected := range tt.expectedRedundant {
					assert.Contains(t, redundant, expected)
				}
			} else {
				assert.ElementsMatch(t, tt.expectedRedundant, redundant)
			}
		})
	}
}

// TestMatchWildcard tests wildcard matching helper
func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		text     string
		pattern  string
		expected bool
	}{
		// Exact matches (no wildcard)
		{"utm_source", "utm_source", true},
		{"utm_source", "utm_medium", false},

		// Simple prefix wildcard
		{"utm_source", "utm_*", true},
		{"utm_medium", "utm_*", true},
		{"ga_session", "utm_*", false},

		// Simple suffix wildcard
		{"test.pdf", "*.pdf", true},
		{"test.jpg", "*.pdf", false},

		// Middle wildcard
		{"/product/123/reviews", "/product/*/reviews", true},
		{"/product/456/reviews", "/product/*/reviews", true},
		{"/product/123/details", "/product/*/reviews", false},

		// Match all
		{"anything", "*", true},
		{"/any/path", "*", true},

		// Empty cases
		{"", "", true},
		{"test", "", false},
		{"", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.text+"_vs_"+tt.pattern, func(t *testing.T) {
			result := pattern.MatchWildcard(tt.text, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

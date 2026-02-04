package bot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

func TestIsBotRequest(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		patterns  []string
		expectBot bool
	}{
		// Wildcard matching tests for substring behavior
		{
			name:      "wildcard match - Googlebot",
			userAgent: "Mozilla/5.0 (compatible; Googlebot/2.1)",
			patterns:  []string{"*Googlebot*"},
			expectBot: true,
		},
		{
			name:      "wildcard match - case insensitive",
			userAgent: "Googlebot",
			patterns:  []string{"*googlebot*"},
			expectBot: true, // Wildcards are case-insensitive
		},
		{
			name:      "wildcard match - partial substring",
			userAgent: "NotAGooglebot",
			patterns:  []string{"*Googlebot*"},
			expectBot: true,
		},

		// Wildcard matching tests
		{
			name:      "wildcard prefix - Google*",
			userAgent: "Googlebot",
			patterns:  []string{"Google*"},
			expectBot: true,
		},
		{
			name:      "wildcard prefix - Google-Mobile",
			userAgent: "Google-Mobile",
			patterns:  []string{"Google*"},
			expectBot: true,
		},
		{
			name:      "wildcard suffix - *bot",
			userAgent: "Googlebot",
			patterns:  []string{"*bot"},
			expectBot: true,
		},
		{
			name:      "wildcard suffix - Slackbot",
			userAgent: "Slackbot",
			patterns:  []string{"*bot"},
			expectBot: true,
		},
		{
			name:      "wildcard middle - *Googlebot*",
			userAgent: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			patterns:  []string{"*Googlebot*"},
			expectBot: true,
		},
		{
			name:      "match-all wildcard",
			userAgent: "AnyUserAgent/1.0",
			patterns:  []string{"*"},
			expectBot: true,
		},

		// Case-sensitive regexp tests (~)
		{
			name:      "case-sensitive regexp - exact match",
			userAgent: "Googlebot",
			patterns:  []string{"~^Googlebot$"},
			expectBot: true,
		},
		{
			name:      "case-sensitive regexp - case mismatch",
			userAgent: "googlebot",
			patterns:  []string{"~^Googlebot$"},
			expectBot: false,
		},
		{
			name:      "case-sensitive regexp - pattern match",
			userAgent: "Mozilla/5.0 Googlebot/2.1",
			patterns:  []string{"~.*Googlebot.*"},
			expectBot: true,
		},
		{
			name:      "case-sensitive regexp - complex pattern",
			userAgent: "Googlebot/2.1",
			patterns:  []string{"~^Googlebot/[0-9.]+$"},
			expectBot: true,
		},

		// Case-insensitive regexp tests (~*)
		{
			name:      "case-insensitive regexp - lowercase input",
			userAgent: "googlebot",
			patterns:  []string{"~*^Googlebot$"},
			expectBot: true,
		},
		{
			name:      "case-insensitive regexp - uppercase input",
			userAgent: "GOOGLEBOT",
			patterns:  []string{"~*^googlebot$"},
			expectBot: true,
		},
		{
			name:      "case-insensitive regexp - mixed case",
			userAgent: "GoOgLeBoT",
			patterns:  []string{"~*^googlebot$"},
			expectBot: true,
		},
		{
			name:      "case-insensitive regexp - pattern match",
			userAgent: "Mozilla/5.0 googlebot/2.1",
			patterns:  []string{"~*.*GOOGLEBOT.*"},
			expectBot: true,
		},

		// Multiple patterns tests
		{
			name:      "first match wins",
			userAgent: "Googlebot",
			patterns:  []string{"*Bingbot*", "*Googlebot*", "*Slurp*"},
			expectBot: true,
		},
		{
			name:      "multiple patterns - match second",
			userAgent: "Bingbot",
			patterns:  []string{"*Googlebot*", "*Bingbot*", "*Slurp*"},
			expectBot: true,
		},
		{
			name:      "multiple patterns - mixed types",
			userAgent: "Googlebot/2.1",
			patterns:  []string{"~^Bingbot.*", "~^Googlebot.*", "*Slurp*"},
			expectBot: true,
		},

		// Edge cases
		{
			name:      "no match",
			userAgent: "Chrome/100.0",
			patterns:  []string{"*Googlebot*", "*Bingbot*"},
			expectBot: false,
		},
		{
			name:      "empty patterns",
			userAgent: "Googlebot",
			patterns:  []string{},
			expectBot: false,
		},
		{
			name:      "nil patterns",
			userAgent: "Googlebot",
			patterns:  nil,
			expectBot: false,
		},
		{
			name:      "empty user agent",
			userAgent: "",
			patterns:  []string{"*Googlebot*"},
			expectBot: false,
		},

		// Real-world user agents
		{
			name:      "real Googlebot user agent",
			userAgent: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			patterns:  []string{"*Googlebot*"},
			expectBot: true,
		},
		{
			name:      "real Bingbot user agent",
			userAgent: "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			patterns:  []string{"*bingbot*"},
			expectBot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create BothitRecacheConfig and compile patterns
			enabled := true
			interval := types.Duration(time.Hour)
			bothitConfig := &types.BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval,
				MatchUA:  tt.patterns,
			}

			err := bothitConfig.CompileMatchUAPatterns()
			require.NoError(t, err, "pattern compilation should succeed")

			// Convert to ResolvedBothitRecache for the function call
			resolvedConfig := &config.ResolvedBothitRecache{
				Enabled:          *bothitConfig.Enabled,
				Interval:         time.Duration(*bothitConfig.Interval),
				MatchUA:          bothitConfig.MatchUA,
				CompiledPatterns: bothitConfig.CompiledPatterns,
			}

			result := IsBotRequest(tt.userAgent, resolvedConfig)
			assert.Equal(t, tt.expectBot, result)
		})
	}
}

// TestIsBotRequest_InvalidRegexp tests that invalid regexp patterns fail during compilation
func TestIsBotRequest_InvalidRegexp(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{
			name:    "invalid case-sensitive regexp",
			pattern: "~[invalid(",
		},
		{
			name:    "invalid case-insensitive regexp",
			pattern: "~*[invalid(",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := true
			interval := types.Duration(time.Hour)
			config := &types.BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval,
				MatchUA:  []string{tt.pattern},
			}

			err := config.CompileMatchUAPatterns()
			assert.Error(t, err, "invalid regexp pattern should fail compilation")
		})
	}
}

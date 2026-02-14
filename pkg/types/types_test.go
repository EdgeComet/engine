package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/edgecomet/engine/pkg/pattern"
)

// TestDuration_UnmarshalYAML tests YAML unmarshaling for Duration type
func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected time.Duration
		wantErr  bool
	}{
		// Standard Go duration formats
		{
			name:     "nanoseconds",
			yaml:     "duration: 100ns",
			expected: 100 * time.Nanosecond,
			wantErr:  false,
		},
		{
			name:     "microseconds",
			yaml:     "duration: 500us",
			expected: 500 * time.Microsecond,
			wantErr:  false,
		},
		{
			name:     "milliseconds",
			yaml:     "duration: 250ms",
			expected: 250 * time.Millisecond,
			wantErr:  false,
		},
		{
			name:     "seconds",
			yaml:     "duration: 30s",
			expected: 30 * time.Second,
			wantErr:  false,
		},
		{
			name:     "minutes",
			yaml:     "duration: 15m",
			expected: 15 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "hours",
			yaml:     "duration: 2h",
			expected: 2 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "combined format",
			yaml:     "duration: 1h30m45s",
			expected: 1*time.Hour + 30*time.Minute + 45*time.Second,
			wantErr:  false,
		},

		// Extended formats - days
		{
			name:     "days integer",
			yaml:     "duration: 7d",
			expected: 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "days float",
			yaml:     "duration: 1.5d",
			expected: time.Duration(1.5 * float64(24*time.Hour)),
			wantErr:  false,
		},
		{
			name:     "30 days",
			yaml:     "duration: 30d",
			expected: 30 * 24 * time.Hour,
			wantErr:  false,
		},

		// Extended formats - weeks
		{
			name:     "weeks integer",
			yaml:     "duration: 2w",
			expected: 2 * 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "weeks float",
			yaml:     "duration: 1.5w",
			expected: time.Duration(1.5 * float64(7*24*time.Hour)),
			wantErr:  false,
		},
		{
			name:     "one week",
			yaml:     "duration: 1w",
			expected: 7 * 24 * time.Hour,
			wantErr:  false,
		},

		// Negative values
		{
			name:     "negative seconds",
			yaml:     "duration: -10s",
			expected: -10 * time.Second,
			wantErr:  false,
		},
		{
			name:     "negative days",
			yaml:     "duration: -3d",
			expected: -3 * 24 * time.Hour,
			wantErr:  false,
		},

		// Zero values
		{
			name:     "zero seconds",
			yaml:     "duration: 0s",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "zero days",
			yaml:     "duration: 0d",
			expected: 0,
			wantErr:  false,
		},

		// Invalid formats
		{
			name:     "invalid suffix",
			yaml:     "duration: 10y",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid format",
			yaml:     "duration: invalid",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "empty string",
			yaml:     "duration: \"\"",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "just number no suffix",
			yaml:     "duration: 30",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config struct {
				Duration Duration `yaml:"duration"`
			}

			err := yaml.Unmarshal([]byte(tt.yaml), &config)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, time.Duration(config.Duration))
			}
		})
	}
}

// TestDuration_MarshalYAML tests YAML marshaling for Duration type
func TestDuration_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected string
	}{
		{
			name:     "seconds",
			duration: Duration(30 * time.Second),
			expected: "30s",
		},
		{
			name:     "minutes",
			duration: Duration(15 * time.Minute),
			expected: "15m0s",
		},
		{
			name:     "hours",
			duration: Duration(2 * time.Hour),
			expected: "2h0m0s",
		},
		{
			name:     "days converted to hours",
			duration: Duration(24 * time.Hour),
			expected: "24h0m0s",
		},
		{
			name:     "zero",
			duration: Duration(0),
			expected: "0s",
		},
		{
			name:     "negative",
			duration: Duration(-10 * time.Second),
			expected: "-10s",
		},
		{
			name:     "milliseconds",
			duration: Duration(500 * time.Millisecond),
			expected: "500ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := struct {
				Duration Duration `yaml:"duration"`
			}{
				Duration: tt.duration,
			}

			data, err := yaml.Marshal(&config)
			require.NoError(t, err)

			// Parse the marshaled YAML to check the duration value
			var result struct {
				Duration string `yaml:"duration"`
			}
			err = yaml.Unmarshal(data, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expected, result.Duration)
		})
	}
}

// TestDuration_ToDuration tests the ToDuration conversion method
func TestDuration_ToDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected time.Duration
	}{
		{
			name:     "zero",
			duration: Duration(0),
			expected: 0,
		},
		{
			name:     "positive",
			duration: Duration(30 * time.Second),
			expected: 30 * time.Second,
		},
		{
			name:     "negative",
			duration: Duration(-10 * time.Minute),
			expected: -10 * time.Minute,
		},
		{
			name:     "large value",
			duration: Duration(365 * 24 * time.Hour),
			expected: 365 * 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.duration.ToDuration()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDuration_String tests the String method
func TestDuration_String(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected string
	}{
		{
			name:     "zero",
			duration: Duration(0),
			expected: "0s",
		},
		{
			name:     "seconds",
			duration: Duration(45 * time.Second),
			expected: "45s",
		},
		{
			name:     "minutes",
			duration: Duration(10 * time.Minute),
			expected: "10m0s",
		},
		{
			name:     "hours",
			duration: Duration(3 * time.Hour),
			expected: "3h0m0s",
		},
		{
			name:     "negative",
			duration: Duration(-30 * time.Second),
			expected: "-30s",
		},
		{
			name:     "milliseconds",
			duration: Duration(250 * time.Millisecond),
			expected: "250ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.duration.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseExtendedDuration tests the parseExtendedDuration function indirectly
// through UnmarshalYAML since parseExtendedDuration is not exported
func TestDuration_ExtendedFormats(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    time.Duration
		description string
	}{
		{
			name:        "standard 7 days",
			input:       "7d",
			expected:    7 * 24 * time.Hour,
			description: "one week in days",
		},
		{
			name:        "fractional days",
			input:       "0.5d",
			expected:    12 * time.Hour,
			description: "half a day is 12 hours",
		},
		{
			name:        "multiple weeks",
			input:       "4w",
			expected:    28 * 24 * time.Hour,
			description: "four weeks is 28 days",
		},
		{
			name:        "fractional weeks",
			input:       "0.5w",
			expected:    time.Duration(0.5 * float64(7*24*time.Hour)),
			description: "half a week is 3.5 days",
		},
		{
			name:        "large day value",
			input:       "365d",
			expected:    365 * 24 * time.Hour,
			description: "approximately one year",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlStr := "duration: " + tt.input
			var config struct {
				Duration Duration `yaml:"duration"`
			}

			err := yaml.Unmarshal([]byte(yamlStr), &config)
			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expected, time.Duration(config.Duration), tt.description)
		})
	}
}

// TestDuration_RealWorldExamples tests realistic configuration values
func TestDuration_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected time.Duration
	}{
		{
			name:     "cache TTL 30 days",
			yaml:     "ttl: 30d",
			expected: 30 * 24 * time.Hour,
		},
		{
			name:     "render timeout 30 seconds",
			yaml:     "timeout: 30s",
			expected: 30 * time.Second,
		},
		{
			name:     "stale ttl 5 minutes",
			yaml:     "stale_ttl: 5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "stale ttl 1 hour",
			yaml:     "stale_ttl: 1h",
			expected: 1 * time.Hour,
		},
		{
			name:     "retry delay 30 seconds",
			yaml:     "retry_delay: 30s",
			expected: 30 * time.Second,
		},
		{
			name:     "heartbeat interval 10 seconds",
			yaml:     "heartbeat_interval: 10s",
			expected: 10 * time.Second,
		},
		{
			name:     "stale threshold 5 minutes",
			yaml:     "stale_threshold: 5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "cleanup interval 1 hour",
			yaml:     "cleanup_interval: 1h",
			expected: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the YAML field name and value
			var config map[string]Duration
			err := yaml.Unmarshal([]byte(tt.yaml), &config)
			require.NoError(t, err)

			// Get the first (and only) value from the map
			for _, duration := range config {
				assert.Equal(t, tt.expected, time.Duration(duration))
				break
			}
		})
	}
}

// TestCacheKey_String tests the CacheKey String method
func TestCacheKey_String(t *testing.T) {
	tests := []struct {
		name     string
		key      CacheKey
		expected string
	}{
		{
			name: "basic cache key",
			key: CacheKey{
				HostID:      1,
				DimensionID: 2,
				URLHash:     "abc123def456",
			},
			expected: "cache:1:2:abc123def456",
		},
		{
			name: "cache key with zeros",
			key: CacheKey{
				HostID:      0,
				DimensionID: 0,
				URLHash:     "hash",
			},
			expected: "cache:0:0:hash",
		},
		{
			name: "cache key with large IDs",
			key: CacheKey{
				HostID:      9999,
				DimensionID: 1234,
				URLHash:     "longhashvalue123",
			},
			expected: "cache:9999:1234:longhashvalue123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.key.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDimension_CompileMatchUAPatterns tests user agent pattern compilation
func TestDimension_CompileMatchUAPatterns(t *testing.T) {
	tests := []struct {
		name        string
		matchUA     []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "exact match patterns",
			matchUA:     []string{"Googlebot", "bingbot", "Mobile Safari"},
			expectError: false,
		},
		{
			name:        "wildcard patterns (for substring matching)",
			matchUA:     []string{"*Googlebot*", "*bingbot*", "*Mobile Safari*"},
			expectError: false,
		},
		{
			name:        "case-sensitive regexp",
			matchUA:     []string{"~Mobile.*Googlebot", "~.*bingbot.*"},
			expectError: false,
		},
		{
			name:        "case-insensitive regexp",
			matchUA:     []string{"~*googlebot", "~*mobile.*safari"},
			expectError: false,
		},
		{
			name:        "wildcard patterns",
			matchUA:     []string{"*Googlebot*", "Mobile*"},
			expectError: false,
		},
		{
			name:        "mixed pattern types",
			matchUA:     []string{"*Googlebot*", "~Mobile.*Safari", "~*iphone", "*Android*"},
			expectError: false,
		},
		{
			name:        "invalid regexp - unclosed group",
			matchUA:     []string{"~(Googlebot"},
			expectError: true,
			errorMsg:    "invalid user agent pattern",
		},
		{
			name:        "invalid regexp - bad syntax",
			matchUA:     []string{"~[a-"},
			expectError: true,
			errorMsg:    "invalid user agent pattern",
		},
		{
			name:        "empty patterns list",
			matchUA:     []string{},
			expectError: false,
		},
		{
			name:        "nil patterns list",
			matchUA:     nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim := &Dimension{
				ID:       1,
				Width:    1920,
				Height:   1080,
				RenderUA: "Mozilla/5.0...",
				MatchUA:  tt.matchUA,
			}

			err := dim.CompileMatchUAPatterns()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)

				// Verify compiled patterns were created for non-empty patterns
				if len(tt.matchUA) > 0 {
					assert.Equal(t, len(tt.matchUA), len(dim.CompiledPatterns))

					// Verify pattern types were correctly detected
					for i, pat := range tt.matchUA {
						compiled := dim.CompiledPatterns[i]
						assert.NotNil(t, compiled)

						if len(pat) >= 2 && pat[:2] == "~*" {
							assert.Equal(t, pattern.PatternTypeRegexp, compiled.Type)
							assert.True(t, compiled.CaseInsensitive)
						} else if len(pat) >= 1 && pat[0] == '~' {
							assert.Equal(t, pattern.PatternTypeRegexp, compiled.Type)
							assert.False(t, compiled.CaseInsensitive)
						}
					}
				}
			}
		})
	}
}

// TestDimension_CompileMatchUAPatterns_RegexpMatching tests that compiled regexps work correctly
func TestDimension_CompileMatchUAPatterns_RegexpMatching(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		userAgent   string
		shouldMatch bool
	}{
		{
			name:        "case-sensitive regexp matches",
			pattern:     "~Mobile.*Googlebot",
			userAgent:   "Mozilla/5.0 (Linux) Mobile Safari (compatible; Googlebot/2.1)",
			shouldMatch: true,
		},
		{
			name:        "case-sensitive regexp no match - wrong case",
			pattern:     "~Mobile.*Googlebot",
			userAgent:   "Mozilla/5.0 (Linux) mobile safari (compatible; googlebot/2.1)",
			shouldMatch: false,
		},
		{
			name:        "case-insensitive regexp matches",
			pattern:     "~*mobile.*googlebot",
			userAgent:   "Mozilla/5.0 (Linux) Mobile Safari (compatible; Googlebot/2.1)",
			shouldMatch: true,
		},
		{
			name:        "case-insensitive regexp matches lowercase",
			pattern:     "~*mobile.*googlebot",
			userAgent:   "mozilla/5.0 (linux) mobile safari (compatible; googlebot/2.1)",
			shouldMatch: true,
		},
		{
			name:        "regexp with character class",
			pattern:     "~Googlebot/[0-9]+\\.[0-9]+",
			userAgent:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: true,
		},
		{
			name:        "regexp with anchors",
			pattern:     "~^Mozilla.*Googlebot",
			userAgent:   "Mozilla/5.0 (compatible; Googlebot/2.1)",
			shouldMatch: true,
		},
		{
			name:        "regexp with anchors no match",
			pattern:     "~^Mozilla.*Googlebot$",
			userAgent:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim := &Dimension{
				ID:       1,
				Width:    1920,
				Height:   1080,
				RenderUA: "Mozilla/5.0...",
				MatchUA:  []string{tt.pattern},
			}

			err := dim.CompileMatchUAPatterns()
			require.NoError(t, err)
			require.Len(t, dim.CompiledPatterns, 1)
			require.NotNil(t, dim.CompiledPatterns[0])

			matched := dim.CompiledPatterns[0].Match(tt.userAgent)
			assert.Equal(t, tt.shouldMatch, matched,
				"Pattern: %s, UserAgent: %s", tt.pattern, tt.userAgent)
		})
	}
}

// TestHost_UnmarshalYAML tests custom YAML unmarshaling for Host struct
func TestHost_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		expectedDomains []string
		expectedDomain  string
		wantErr         bool
	}{
		{
			name: "single domain as string",
			yaml: `
id: 1
domain: example.com
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "multiple domains as array",
			yaml: `
id: 2
domain:
  - example.com
  - www.example.com
  - cdn.example.com
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com", "www.example.com", "cdn.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "multiple domains as inline array",
			yaml: `
id: 3
domain: [example.com, www.example.com]
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "trailing dot stripped from string",
			yaml: `
id: 4
domain: example.com.
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "trailing dots stripped from array",
			yaml: `
id: 5
domain:
  - example.com.
  - www.example.com.
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "empty domain field",
			yaml: `
id: 6
render_key: test-key
enabled: true
`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name: "empty string domain",
			yaml: `
id: 7
domain: ""
render_key: test-key
enabled: true
`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name: "empty array domain",
			yaml: `
id: 8
domain: []
render_key: test-key
enabled: true
`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name: "whitespace-only string domain filtered",
			yaml: `
id: 9
domain: "   "
render_key: test-key
enabled: true
`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name: "whitespace-only domains in array filtered",
			yaml: `
id: 10
domain: ["  ", "example.com", "   "]
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name: "domains with leading/trailing whitespace trimmed",
			yaml: `
id: 11
domain: ["  example.com  ", "  www.example.com  "]
render_key: test-key
enabled: true
`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var host Host
			err := yaml.Unmarshal([]byte(tt.yaml), &host)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedDomains, host.Domains)
			assert.Equal(t, tt.expectedDomain, host.Domain)
		})
	}
}

// TestHost_UnmarshalYAML_PreservesOtherFields verifies other Host fields are preserved
func TestHost_UnmarshalYAML_PreservesOtherFields(t *testing.T) {
	yamlStr := `
id: 42
domain: example.com
render_key: my-secret-key
enabled: true
render:
  timeout: 30s
  events:
    wait_for: networkIdle
`
	var host Host
	err := yaml.Unmarshal([]byte(yamlStr), &host)
	require.NoError(t, err)

	assert.Equal(t, 42, host.ID)
	assert.Equal(t, []string{"example.com"}, host.Domains)
	assert.Equal(t, "example.com", host.Domain)
	assert.Equal(t, "my-secret-key", host.RenderKey)
	assert.True(t, host.Enabled)
	assert.Equal(t, 30*time.Second, host.Render.Timeout.ToDuration())
	assert.Equal(t, "networkIdle", host.Render.Events.WaitFor)
}

// TestHost_UnmarshalYAML_MultiDomainScenarios tests real-world multi-domain use cases
func TestHost_UnmarshalYAML_MultiDomainScenarios(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		expectedDomains []string
		expectedDomain  string
	}{
		{
			name: "www and non-www variants",
			yaml: `
id: 1
domain: [shop.example.com, www.shop.example.com]
render_key: key
enabled: true
`,
			expectedDomains: []string{"shop.example.com", "www.shop.example.com"},
			expectedDomain:  "shop.example.com",
		},
		{
			name: "subdomain variants",
			yaml: `
id: 2
domain:
  - api.example.com
  - api-v2.example.com
  - staging-api.example.com
render_key: key
enabled: true
`,
			expectedDomains: []string{"api.example.com", "api-v2.example.com", "staging-api.example.com"},
			expectedDomain:  "api.example.com",
		},
		{
			name: "mixed trailing dots",
			yaml: `
id: 3
domain:
  - clean.example.com
  - trailing.example.com.
render_key: key
enabled: true
`,
			expectedDomains: []string{"clean.example.com", "trailing.example.com"},
			expectedDomain:  "clean.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var host Host
			err := yaml.Unmarshal([]byte(tt.yaml), &host)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedDomains, host.Domains)
			assert.Equal(t, tt.expectedDomain, host.Domain)
		})
	}
}

// TestHost_MarshalYAML tests YAML marshaling for Host struct
func TestHost_MarshalYAML(t *testing.T) {
	tests := []struct {
		name           string
		host           Host
		expectedDomain interface{}
	}{
		{
			name: "single domain marshals as string",
			host: Host{
				ID:        1,
				Domains:   []string{"example.com"},
				Domain:    "example.com",
				RenderKey: "test-key",
				Enabled:   true,
			},
			expectedDomain: "example.com",
		},
		{
			name: "multiple domains marshal as array",
			host: Host{
				ID:        2,
				Domains:   []string{"example.com", "www.example.com"},
				Domain:    "example.com",
				RenderKey: "test-key",
				Enabled:   true,
			},
			expectedDomain: []interface{}{"example.com", "www.example.com"},
		},
		{
			name: "empty domains omits field",
			host: Host{
				ID:        3,
				Domains:   []string{},
				RenderKey: "test-key",
				Enabled:   true,
			},
			expectedDomain: nil,
		},
		{
			name: "nil domains omits field",
			host: Host{
				ID:        4,
				RenderKey: "test-key",
				Enabled:   true,
			},
			expectedDomain: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.host)
			require.NoError(t, err)

			// Parse marshaled YAML to verify domain field
			var result map[string]interface{}
			err = yaml.Unmarshal(data, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedDomain, result["domain"])
		})
	}
}

// TestHost_MarshalUnmarshalRoundTrip tests round-trip marshal/unmarshal consistency
func TestHost_MarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		host Host
	}{
		{
			name: "single domain round-trip",
			host: Host{
				ID:        1,
				Domains:   []string{"example.com"},
				Domain:    "example.com",
				RenderKey: "secret-key",
				Enabled:   true,
			},
		},
		{
			name: "multiple domains round-trip",
			host: Host{
				ID:        2,
				Domains:   []string{"api.example.com", "www.api.example.com", "staging.api.example.com"},
				Domain:    "api.example.com",
				RenderKey: "api-key",
				Enabled:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := yaml.Marshal(&tt.host)
			require.NoError(t, err)

			// Unmarshal
			var result Host
			err = yaml.Unmarshal(data, &result)
			require.NoError(t, err)

			// Verify key fields match
			assert.Equal(t, tt.host.ID, result.ID)
			assert.Equal(t, tt.host.Domains, result.Domains)
			assert.Equal(t, tt.host.Domain, result.Domain)
			assert.Equal(t, tt.host.RenderKey, result.RenderKey)
			assert.Equal(t, tt.host.Enabled, result.Enabled)
		})
	}
}

// TestDuration_UnmarshalJSON tests JSON unmarshaling for Duration type
func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected time.Duration
		wantErr  bool
	}{
		// String inputs
		{
			name:     "seconds string",
			json:     `{"duration":"15s"}`,
			expected: 15 * time.Second,
		},
		{
			name:     "hours string",
			json:     `{"duration":"24h"}`,
			expected: 24 * time.Hour,
		},
		{
			name:     "days string",
			json:     `{"duration":"30d"}`,
			expected: 30 * 24 * time.Hour,
		},
		{
			name:     "weeks string",
			json:     `{"duration":"2w"}`,
			expected: 2 * 7 * 24 * time.Hour,
		},
		{
			name:     "zero string",
			json:     `{"duration":"0s"}`,
			expected: 0,
		},
		{
			name:     "combined string",
			json:     `{"duration":"1h30m"}`,
			expected: 1*time.Hour + 30*time.Minute,
		},
		// Number inputs (nanoseconds)
		{
			name:     "nanoseconds number",
			json:     `{"duration":30000000000}`,
			expected: 30 * time.Second,
		},
		{
			name:     "zero number",
			json:     `{"duration":0}`,
			expected: 0,
		},
		// Invalid inputs
		{
			name:    "boolean",
			json:    `{"duration":true}`,
			wantErr: true,
		},
		{
			name:    "array",
			json:    `{"duration":[]}`,
			wantErr: true,
		},
		{
			name:    "invalid string",
			json:    `{"duration":"invalid"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config struct {
				Duration Duration `json:"duration"`
			}

			err := json.Unmarshal([]byte(tt.json), &config)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, time.Duration(config.Duration))
			}
		})
	}
}

// TestDuration_MarshalJSON tests JSON marshaling for Duration type
func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected string
	}{
		{
			name:     "seconds",
			duration: Duration(30 * time.Second),
			expected: `"30s"`,
		},
		{
			name:     "hours",
			duration: Duration(24 * time.Hour),
			expected: `"24h0m0s"`,
		},
		{
			name:     "zero",
			duration: Duration(0),
			expected: `"0s"`,
		},
		{
			name:     "milliseconds",
			duration: Duration(500 * time.Millisecond),
			expected: `"500ms"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.duration)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

// TestDuration_JSON_RoundTrip tests JSON marshal then unmarshal consistency
func TestDuration_JSON_RoundTrip(t *testing.T) {
	original := Duration(45 * time.Second)

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Duration
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}

// TestHost_UnmarshalJSON tests JSON unmarshaling for Host struct
func TestHost_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		expectedDomains []string
		expectedDomain  string
		wantErr         bool
	}{
		{
			name:            "single domain as string",
			json:            `{"id": 1, "domain": "example.com", "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name:            "multiple domains as array",
			json:            `{"id": 2, "domain": ["example.com", "www.example.com"], "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name:            "trailing dot stripped from string",
			json:            `{"id": 3, "domain": "example.com.", "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name:            "trailing dots stripped from array",
			json:            `{"id": 4, "domain": ["example.com.", "www.example.com."], "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name:            "empty domain field",
			json:            `{"id": 5, "render_key": "key", "enabled": true}`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name:            "null domain",
			json:            `{"id": 6, "domain": null, "render_key": "key", "enabled": true}`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name:            "empty string domain",
			json:            `{"id": 7, "domain": "", "render_key": "key", "enabled": true}`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name:            "empty array domain",
			json:            `{"id": 8, "domain": [], "render_key": "key", "enabled": true}`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name:            "whitespace-only string domain filtered",
			json:            `{"id": 9, "domain": "   ", "render_key": "key", "enabled": true}`,
			expectedDomains: nil,
			expectedDomain:  "",
			wantErr:         false,
		},
		{
			name:            "whitespace-only domains in array filtered",
			json:            `{"id": 10, "domain": ["  ", "example.com", "   "], "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
		{
			name:            "domains with leading/trailing whitespace trimmed",
			json:            `{"id": 11, "domain": ["  example.com  ", "  www.example.com  "], "render_key": "key", "enabled": true}`,
			expectedDomains: []string{"example.com", "www.example.com"},
			expectedDomain:  "example.com",
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var host Host
			err := json.Unmarshal([]byte(tt.json), &host)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedDomains, host.Domains)
			assert.Equal(t, tt.expectedDomain, host.Domain)
		})
	}
}

// TestHost_UnmarshalJSON_PreservesOtherFields verifies other Host fields are preserved
func TestHost_UnmarshalJSON_PreservesOtherFields(t *testing.T) {
	jsonStr := `{
		"id": 42,
		"domain": "example.com",
		"render_key": "my-secret-key",
		"enabled": true
	}`

	var host Host
	err := json.Unmarshal([]byte(jsonStr), &host)
	require.NoError(t, err)

	assert.Equal(t, 42, host.ID)
	assert.Equal(t, []string{"example.com"}, host.Domains)
	assert.Equal(t, "example.com", host.Domain)
	assert.Equal(t, "my-secret-key", host.RenderKey)
	assert.True(t, host.Enabled)
}

package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestURLRule_GetMatchPatterns_Cached tests that GetMatchPatterns returns cached patterns
func TestURLRule_GetMatchPatterns_Cached(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "single string pattern",
			yaml: `
match: "/blog/*"
action: render
`,
			expected: []string{"/blog/*"},
		},
		{
			name: "array of string patterns",
			yaml: `
match:
  - "/blog/*"
  - "/articles/*"
  - "/news/*"
action: render
`,
			expected: []string{"/blog/*", "/articles/*", "/news/*"},
		},
		{
			name: "single pattern with exact path",
			yaml: `
match: "/admin/login"
action: block
`,
			expected: []string{"/admin/login"},
		},
		{
			name: "wildcard patterns",
			yaml: `
match:
  - "*.pdf"
  - "*.docx"
  - "/downloads/*"
action: bypass
`,
			expected: []string{"*.pdf", "*.docx", "/downloads/*"},
		},
		{
			name: "empty string filtered out",
			yaml: `
match: ""
action: render
`,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule URLRule
			err := yaml.Unmarshal([]byte(tt.yaml), &rule)
			require.NoError(t, err)

			// First call - should return cached patterns
			patterns1 := rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns1)

			// Second call - should return same cached slice (pointer equality)
			patterns2 := rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns2)

			// Verify it's the same slice (not a new allocation)
			if len(patterns1) > 0 {
				assert.True(t, &patterns1[0] == &patterns2[0], "Should return same cached slice")
			}
		})
	}
}

// TestURLRule_UnmarshalYAML_PrecomputesPatterns tests that patterns are pre-computed during unmarshal
func TestURLRule_UnmarshalYAML_PrecomputesPatterns(t *testing.T) {
	yamlData := `
match:
  - "/api/*"
  - "/admin/*"
action: block
reason: "Protected endpoints"
`
	var rule URLRule
	err := yaml.Unmarshal([]byte(yamlData), &rule)
	require.NoError(t, err)

	// Verify matchPatterns is pre-populated
	assert.NotNil(t, rule.matchPatterns, "matchPatterns should be pre-computed")
	assert.Equal(t, []string{"/api/*", "/admin/*"}, rule.matchPatterns)

	// Verify GetMatchPatterns returns the cached slice
	patterns := rule.GetMatchPatterns()
	assert.Equal(t, []string{"/api/*", "/admin/*"}, patterns)
}

// TestURLRule_GetMatchPatterns_ProgrammaticCreation tests backward compatibility
func TestURLRule_GetMatchPatterns_ProgrammaticCreation(t *testing.T) {
	tests := []struct {
		name     string
		rule     URLRule
		expected []string
	}{
		{
			name: "string match - programmatic",
			rule: URLRule{
				Match:  "/blog/*",
				Action: ActionRender,
			},
			expected: []string{"/blog/*"},
		},
		{
			name: "[]string match - programmatic",
			rule: URLRule{
				Match:  []string{"/a", "/b", "/c"},
				Action: ActionRender,
			},
			expected: []string{"/a", "/b", "/c"},
		},
		{
			name: "[]interface{} match - programmatic",
			rule: URLRule{
				Match:  []interface{}{"/x", "/y"},
				Action: ActionRender,
			},
			expected: []string{"/x", "/y"},
		},
		{
			name: "invalid type - programmatic",
			rule: URLRule{
				Match:  123,
				Action: ActionRender,
			},
			expected: []string{},
		},
		{
			name: "nil match - programmatic",
			rule: URLRule{
				Match:  nil,
				Action: ActionRender,
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := tt.rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns)
		})
	}
}

// TestURLRule_UnmarshalYAML_ComplexPatterns tests unmarshaling with complex patterns
func TestURLRule_UnmarshalYAML_ComplexPatterns(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "query param patterns",
			yaml: `
match:
  - "/search*"
  - "/results*"
action: render
`,
			expected: []string{"/search*", "/results*"},
		},
		{
			name: "extension patterns",
			yaml: `
match:
  - "*.pdf"
  - "*.jpg"
  - "*.png"
action: bypass
`,
			expected: []string{"*.pdf", "*.jpg", "*.png"},
		},
		{
			name: "middle wildcard patterns",
			yaml: `
match:
  - "/product/*/reviews"
  - "/user/*/profile"
action: render
`,
			expected: []string{"/product/*/reviews", "/user/*/profile"},
		},
		{
			name: "catch-all pattern",
			yaml: `
match: "*"
action: bypass
`,
			expected: []string{"*"},
		},
		{
			name: "root path",
			yaml: `
match: "/"
action: render
`,
			expected: []string{"/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule URLRule
			err := yaml.Unmarshal([]byte(tt.yaml), &rule)
			require.NoError(t, err)

			patterns := rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns)
		})
	}
}

// TestURLRule_UnmarshalYAML_EmptyPatterns tests handling of empty patterns
func TestURLRule_UnmarshalYAML_EmptyPatterns(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "empty string pattern",
			yaml: `
match: ""
action: render
`,
			expected: []string{},
		},
		{
			name: "array with empty strings filtered",
			yaml: `
match:
  - "/blog/*"
  - ""
  - "/news/*"
action: render
`,
			expected: []string{"/blog/*", "/news/*"},
		},
		{
			name: "array with only empty strings",
			yaml: `
match:
  - ""
  - ""
action: render
`,
			expected: []string{},
		},
		{
			name: "empty array",
			yaml: `
match: []
action: render
`,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule URLRule
			err := yaml.Unmarshal([]byte(tt.yaml), &rule)
			require.NoError(t, err)

			patterns := rule.GetMatchPatterns()
			assert.Equal(t, tt.expected, patterns)
		})
	}
}

// TestURLRule_UnmarshalYAML_WithOverrides tests unmarshaling with cache/render/bypass overrides
func TestURLRule_UnmarshalYAML_WithOverrides(t *testing.T) {
	yamlData := `
match: "/api/*"
action: render
render:
  timeout: 30s
  dimension: mobile
  cache:
    ttl: 1h
`
	var rule URLRule
	err := yaml.Unmarshal([]byte(yamlData), &rule)
	require.NoError(t, err)

	// Verify patterns are cached
	patterns := rule.GetMatchPatterns()
	assert.Equal(t, []string{"/api/*"}, patterns)

	// Verify overrides are preserved
	require.NotNil(t, rule.Render)
	assert.Equal(t, "mobile", rule.Render.Dimension)
	require.NotNil(t, rule.Render.Cache)
	require.NotNil(t, rule.Render.Cache.TTL)
}

// TestURLRule_UnmarshalYAML_MixedTypes tests unmarshaling with mixed valid/invalid pattern types
func TestURLRule_UnmarshalYAML_MixedTypes(t *testing.T) {
	// YAML unmarshaling will convert all array elements to the same type
	// This test ensures we handle []interface{} correctly
	yamlData := `
match:
  - "/blog/*"
  - "/news/*"
  - "/articles/*"
action: render
`
	var rule URLRule
	err := yaml.Unmarshal([]byte(yamlData), &rule)
	require.NoError(t, err)

	patterns := rule.GetMatchPatterns()
	assert.Equal(t, []string{"/blog/*", "/news/*", "/articles/*"}, patterns)
}

// TestURLRule_JSONMarshaling tests that JSON marshaling/unmarshaling works correctly
func TestURLRule_JSONMarshaling(t *testing.T) {
	// Create a rule from YAML first (to populate cache)
	yamlData := `
match:
  - "/api/*"
  - "/admin/*"
action: block
reason: "Protected"
`
	var rule URLRule
	err := yaml.Unmarshal([]byte(yamlData), &rule)
	require.NoError(t, err)

	// Marshal to JSON (should not include matchPatterns due to json:"-" tag)
	jsonData, err := json.Marshal(rule)
	require.NoError(t, err)

	// Verify matchPatterns is not in JSON
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)
	_, exists := jsonMap["matchPatterns"]
	assert.False(t, exists, "matchPatterns should not be in JSON output")

	// Unmarshal back from JSON
	var rule2 URLRule
	err = json.Unmarshal(jsonData, &rule2)
	require.NoError(t, err)

	// Verify GetMatchPatterns still works (uses fallback logic)
	patterns := rule2.GetMatchPatterns()
	assert.Equal(t, []string{"/api/*", "/admin/*"}, patterns)
}

// TestURLRule_UnmarshalYAML_Actions tests all action types with cached patterns
func TestURLRule_UnmarshalYAML_Actions(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		action   URLRuleAction
		patterns []string
	}{
		{
			name: "render action",
			yaml: `
match: "/blog/*"
action: render
`,
			action:   ActionRender,
			patterns: []string{"/blog/*"},
		},
		{
			name: "bypass action",
			yaml: `
match: "/static/*"
action: bypass
`,
			action:   ActionBypass,
			patterns: []string{"/static/*"},
		},
		{
			name: "block action (alias)",
			yaml: `
match: "/admin/*"
action: block
reason: "Restricted"
`,
			action:   ActionBlock,
			patterns: []string{"/admin/*"},
		},
		{
			name: "status_403 action (explicit)",
			yaml: `
match: "/api/*"
action: status_403
reason: "API blocked"
`,
			action:   ActionStatus403,
			patterns: []string{"/api/*"},
		},
		{
			name: "status_404 action",
			yaml: `
match: "/removed/*"
action: status_404
reason: "Content removed"
`,
			action:   ActionStatus404,
			patterns: []string{"/removed/*"},
		},
		{
			name: "status_410 action",
			yaml: `
match: "/discontinued/*"
action: status_410
reason: "Permanently gone"
`,
			action:   ActionStatus410,
			patterns: []string{"/discontinued/*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule URLRule
			err := yaml.Unmarshal([]byte(tt.yaml), &rule)
			require.NoError(t, err)

			assert.Equal(t, tt.action, rule.Action)
			patterns := rule.GetMatchPatterns()
			assert.Equal(t, tt.patterns, patterns)
		})
	}
}

// TestURLRule_GetMatchPatterns_ZeroAllocation tests that cached patterns have zero allocations
func TestURLRule_GetMatchPatterns_ZeroAllocation(t *testing.T) {
	// Create rule from YAML (populates cache)
	yamlData := `
match:
  - "/blog/*"
  - "/news/*"
action: render
`
	var rule URLRule
	err := yaml.Unmarshal([]byte(yamlData), &rule)
	require.NoError(t, err)

	// Call GetMatchPatterns multiple times - should return same slice
	patterns1 := rule.GetMatchPatterns()
	patterns2 := rule.GetMatchPatterns()
	patterns3 := rule.GetMatchPatterns()

	// Verify all return the same patterns
	assert.Equal(t, patterns1, patterns2)
	assert.Equal(t, patterns2, patterns3)

	// Verify it's the exact same slice (pointer comparison)
	// This proves zero allocation on subsequent calls
	if len(patterns1) > 0 && len(patterns2) > 0 {
		assert.True(t, &patterns1[0] == &patterns2[0], "Should return same slice pointer")
		assert.True(t, &patterns2[0] == &patterns3[0], "Should return same slice pointer")
	}
}

// TestURLRule_ComputeMatchPatterns_Coverage tests computeMatchPatterns with all types
func TestURLRule_ComputeMatchPatterns_Coverage(t *testing.T) {
	tests := []struct {
		name     string
		match    interface{}
		expected []string
	}{
		{
			name:     "string - non-empty",
			match:    "/test",
			expected: []string{"/test"},
		},
		{
			name:     "string - empty",
			match:    "",
			expected: []string{},
		},
		{
			name:     "[]interface{} - mixed",
			match:    []interface{}{"/a", "/b", ""},
			expected: []string{"/a", "/b"},
		},
		{
			name:     "[]interface{} - empty",
			match:    []interface{}{},
			expected: []string{},
		},
		{
			name:     "[]interface{} - non-string elements ignored",
			match:    []interface{}{"/a", 123, "/b", nil, "/c"},
			expected: []string{"/a", "/b", "/c"},
		},
		{
			name:     "[]string - non-empty",
			match:    []string{"/x", "/y", "/z"},
			expected: []string{"/x", "/y", "/z"},
		},
		{
			name:     "[]string - with empty strings",
			match:    []string{"/x", "", "/y", ""},
			expected: []string{"/x", "/y"},
		},
		{
			name:     "[]string - all empty",
			match:    []string{"", "", ""},
			expected: []string{},
		},
		{
			name:     "invalid type - int",
			match:    42,
			expected: []string{},
		},
		{
			name:     "invalid type - nil",
			match:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := URLRule{Match: tt.match}
			result := rule.computeMatchPatterns()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestURLRuleAction_NormalizeBlockAction tests action normalization
func TestURLRuleAction_NormalizeBlockAction(t *testing.T) {
	tests := []struct {
		name     string
		action   URLRuleAction
		expected URLRuleAction
	}{
		{
			name:     "block normalizes to status_403",
			action:   ActionBlock,
			expected: ActionStatus403,
		},
		{
			name:     "status_403 stays as status_403",
			action:   ActionStatus403,
			expected: ActionStatus403,
		},
		{
			name:     "render stays as render",
			action:   ActionRender,
			expected: ActionRender,
		},
		{
			name:     "bypass stays as bypass",
			action:   ActionBypass,
			expected: ActionBypass,
		},
		{
			name:     "status_404 stays as status_404",
			action:   ActionStatus404,
			expected: ActionStatus404,
		},
		{
			name:     "status_410 stays as status_410",
			action:   ActionStatus410,
			expected: ActionStatus410,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.action.NormalizeBlockAction()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBothitRecacheConfig_Validate tests BothitRecacheConfig validation
func TestBothitRecacheConfig_Validate(t *testing.T) {
	enabled := true
	disabled := false
	interval30m := Duration(30 * time.Minute)
	interval1h := Duration(1 * time.Hour)
	interval24h := Duration(24 * time.Hour)
	interval25h := Duration(25 * time.Hour)
	interval29m := Duration(29 * time.Minute)

	tests := []struct {
		name    string
		config  *BothitRecacheConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled config with empty match_ua is valid",
			config: &BothitRecacheConfig{
				Enabled: &disabled,
				MatchUA: []string{},
			},
			wantErr: false,
		},
		{
			name: "enabled with valid interval and match_ua",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval1h,
				MatchUA:  []string{"Googlebot", "Bingbot"},
			},
			wantErr: false,
		},
		{
			name: "enabled with interval = 30m (minimum)",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval30m,
				MatchUA:  []string{"Googlebot"},
			},
			wantErr: false,
		},
		{
			name: "enabled with interval = 24h (maximum)",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval24h,
				MatchUA:  []string{"Googlebot"},
			},
			wantErr: false,
		},
		{
			name: "enabled with interval < 30m should fail",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval29m,
				MatchUA:  []string{"Googlebot"},
			},
			wantErr: true,
			errMsg:  "must be >= 30m",
		},
		{
			name: "enabled with interval > 24h should fail",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval25h,
				MatchUA:  []string{"Googlebot"},
			},
			wantErr: true,
			errMsg:  "must be <= 24h",
		},
		{
			name: "enabled with empty match_ua should fail",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval1h,
				MatchUA:  []string{},
			},
			wantErr: true,
			errMsg:  "must be non-empty when enabled=true",
		},
		{
			name: "enabled with nil match_ua should fail",
			config: &BothitRecacheConfig{
				Enabled:  &enabled,
				Interval: &interval1h,
				MatchUA:  nil,
			},
			wantErr: true,
			errMsg:  "must be non-empty when enabled=true",
		},
		{
			name: "enabled without interval is valid (can use parent config)",
			config: &BothitRecacheConfig{
				Enabled: &enabled,
				MatchUA: []string{"Googlebot"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

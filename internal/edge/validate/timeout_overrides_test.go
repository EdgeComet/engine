package validate

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func durationPtr(d time.Duration) *types.Duration {
	v := types.Duration(d)
	return &v
}

func TestValidateHostTimeouts(t *testing.T) {
	tests := []struct {
		name    string
		host    *types.Host
		wantErr string
	}{
		{name: "nil host", host: nil},
		{name: "no overrides", host: &types.Host{}},
		{
			name: "positive overrides at every level",
			host: &types.Host{
				Bypass: &types.BypassConfig{Timeout: durationPtr(10 * time.Second)},
				URLRules: []types.URLRule{{
					Match:  "/slow/*",
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{Timeout: durationPtr(45 * time.Second)},
					Bypass: &types.BypassRuleConfig{Timeout: durationPtr(20 * time.Second)},
				}},
			},
		},
		{
			name:    "zero host bypass timeout",
			host:    &types.Host{Bypass: &types.BypassConfig{Timeout: durationPtr(0)}},
			wantErr: "bypass.timeout must be positive",
		},
		{
			name:    "negative host bypass timeout",
			host:    &types.Host{Bypass: &types.BypassConfig{Timeout: durationPtr(-time.Second)}},
			wantErr: "bypass.timeout must be positive",
		},
		{
			name: "zero rule bypass timeout names the rule",
			host: &types.Host{URLRules: []types.URLRule{
				{Match: "/a", Action: types.ActionBypass},
				{Match: "/b", Action: types.ActionBypass, Bypass: &types.BypassRuleConfig{Timeout: durationPtr(0)}},
			}},
			wantErr: "url_rules[1].bypass.timeout must be positive",
		},
		{
			name: "zero rule render timeout",
			host: &types.Host{URLRules: []types.URLRule{
				{Match: "/a", Action: types.ActionRender, Render: &types.RenderRuleConfig{Timeout: durationPtr(0)}},
			}},
			wantErr: "url_rules[0].render.timeout must be positive",
		},
		{
			name: "a render rule's fallback bypass timeout is checked too",
			host: &types.Host{URLRules: []types.URLRule{
				{Match: "/a", Action: types.ActionRender, Bypass: &types.BypassRuleConfig{Timeout: durationPtr(0)}},
			}},
			wantErr: "url_rules[0].bypass.timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostTimeouts(tt.host)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// The file validator applies the same timeout checks, plus the cross-checks against server.timeout
// (120s in writeValidationTestConfig) that only it can run.
func TestValidateConfiguration_TimeoutOverrides(t *testing.T) {
	const dimensions = `      desktop:
        id: 1
        width: 1920
        height: 1080
        render_ua: "Mozilla/5.0"`

	tests := []struct {
		name     string
		hostYAML string
		wantErr  string
	}{
		{
			name: "valid overrides",
			hostYAML: `    bypass:
      timeout: 20s
    url_rules:
      - match: "/slow/*"
        action: "render"
        render:
          timeout: 40s
      - match: "/api/*"
        action: "bypass"
        bypass:
          timeout: 10s`,
		},
		{
			name: "zero host bypass timeout",
			hostYAML: `    bypass:
      timeout: 0s
    url_rules:
      - match: "/*"
        action: "render"`,
			wantErr: "bypass.timeout must be positive",
		},
		{
			name: "zero rule bypass timeout",
			hostYAML: `    url_rules:
      - match: "/api/*"
        action: "bypass"
        bypass:
          timeout: 0s`,
			wantErr: "url_rules[0].bypass.timeout must be positive",
		},
		{
			name: "zero rule render timeout",
			hostYAML: `    url_rules:
      - match: "/slow/*"
        action: "render"
        render:
          timeout: 0s`,
			wantErr: "url_rules[0].render.timeout must be positive",
		},
		{
			name: "host bypass timeout above server timeout",
			hostYAML: `    bypass:
      timeout: 150s
    url_rules:
      - match: "/*"
        action: "render"`,
			wantErr: "bypass.timeout (2m30s) exceeds server.timeout (2m0s)",
		},
		{
			name: "rule bypass timeout above server timeout",
			hostYAML: `    url_rules:
      - match: "/api/*"
        action: "bypass"
        bypass:
          timeout: 150s`,
			wantErr: "url_rules[0]: bypass.timeout (2m30s) exceeds server.timeout (2m0s)",
		},
		{
			// 60s concurrent wait (80% of 100s, capped) + 100s + 10s overhead = 170s > 120s
			name: "rule render timeout counts toward the server timeout minimum",
			hostYAML: `    url_rules:
      - match: "/slow/*"
        action: "render"
        render:
          timeout: 100s`,
			wantErr: "server.timeout (2m0s) is too small",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeValidationTestConfig(t, dimensions, tt.hostYAML)
			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr == "" {
				assert.True(t, result.Valid, "expected valid configuration, got errors: %v", result.Errors)
				return
			}

			assert.False(t, result.Valid, "expected configuration to be invalid")
			found := false
			for _, e := range result.Errors {
				if strings.Contains(e.Message, tt.wantErr) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected an error containing %q, got: %v", tt.wantErr, result.Errors)
		})
	}
}

// Both settings were accepted and silently ignored; with the fields removed, strict YAML rejects them.
func TestValidateConfiguration_RemovedHostOverrides(t *testing.T) {
	const dimensions = `      desktop:
        id: 1
        width: 1920
        height: 1080
        render_ua: "Mozilla/5.0"`

	tests := []struct {
		name     string
		hostYAML string
		field    string
	}{
		{
			name:  "bypass.enabled",
			field: "field enabled not found",
			hostYAML: `    bypass:
      enabled: false
    url_rules:
      - match: "/*"
        action: "render"`,
		},
		{
			name:  "host-level cache_sharding.distribution_strategy",
			field: "field distribution_strategy not found",
			hostYAML: `    cache_sharding:
      distribution_strategy: "random"
    url_rules:
      - match: "/*"
        action: "render"`,
		},
		{
			name:  "rule-level cache_sharding.distribution_strategy",
			field: "field distribution_strategy not found",
			hostYAML: `    url_rules:
      - match: "/*"
        action: "render"
        cache_sharding:
          distribution_strategy: "random"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeValidationTestConfig(t, dimensions, tt.hostYAML)
			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)
			assert.False(t, result.Valid, "expected the removed field to be rejected")
			found := false
			for _, e := range result.Errors {
				if strings.Contains(e.Message, tt.field) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected an error containing %q, got: %v", tt.field, result.Errors)
		})
	}
}

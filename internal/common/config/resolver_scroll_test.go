package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/pkg/types"
)

const scrollTestURL = "https://example.com/app/page"

func scrollSection(enabled *bool) *types.RenderScroll {
	if enabled == nil {
		return nil
	}
	return &types.RenderScroll{Enabled: enabled}
}

// TestResolver_ScrollResolution tests scroll configuration resolution across the three levels
func TestResolver_ScrollResolution(t *testing.T) {
	globalBypass := buildTestGlobalBypass()

	tests := []struct {
		name     string
		global   *bool
		host     *bool
		rule     *bool
		expected bool
		reason   string
	}{
		{
			name:     "unset at every level defaults to disabled",
			expected: false,
			reason:   "Scroll must stay off unless a level asks for it",
		},
		{
			name:     "global enables",
			global:   ptrBool(true),
			expected: true,
			reason:   "Global scroll should apply when host and rule are unset",
		},
		{
			name:     "host enables",
			host:     ptrBool(true),
			expected: true,
			reason:   "Host scroll should apply when global and rule are unset",
		},
		{
			name:     "host disables over enabled global",
			global:   ptrBool(true),
			host:     ptrBool(false),
			expected: false,
			reason:   "Host should override global",
		},
		{
			name:     "rule disables over enabled host",
			global:   ptrBool(true),
			host:     ptrBool(true),
			rule:     ptrBool(false),
			expected: false,
			reason:   "Rule should override host",
		},
		{
			name:     "rule enables over disabled host",
			host:     ptrBool(false),
			rule:     ptrBool(true),
			expected: true,
			reason:   "Rule should be able to enable what host disabled",
		},
		{
			name:     "rule enables over disabled global with host unset",
			global:   ptrBool(false),
			rule:     ptrBool(true),
			expected: true,
			reason:   "An unset host level must not block a rule override",
		},
		{
			name:     "rule disables an inherited global with host unset",
			global:   ptrBool(true),
			rule:     ptrBool(false),
			expected: false,
			reason:   "Rule false wins over a global true the host did not restate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalRender := buildTestGlobalRender()
			globalRender.Scroll = scrollSection(tt.global)

			host := buildTestHost()
			host.Render.Scroll = scrollSection(tt.host)
			host.URLRules = []types.URLRule{
				{
					Match:  "/app/*",
					Action: types.ActionRender,
					Render: &types.RenderRuleConfig{Scroll: scrollSection(tt.rule)},
				},
			}

			resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
			resolved := resolver.ResolveForURL(scrollTestURL)

			assert.Equal(t, tt.expected, resolved.Render.Scroll, tt.reason)
		})
	}

	t.Run("unmatched URL uses host value", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.Scroll = scrollSection(ptrBool(true))
		host.URLRules = []types.URLRule{
			{
				Match:  "/app/*",
				Action: types.ActionRender,
				Render: &types.RenderRuleConfig{Scroll: scrollSection(ptrBool(false))},
			},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL("https://example.com/other/page")

		assert.True(t, resolved.Render.Scroll, "A rule that does not match must not change the host value")
	})

	t.Run("rule without a render section uses host value", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.Scroll = scrollSection(ptrBool(true))
		host.URLRules = []types.URLRule{
			{Match: "/app/*", Action: types.ActionRender, Render: nil},
		}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL(scrollTestURL)

		assert.True(t, resolved.Render.Scroll, "A rule with no render overrides must not change the host value")
	})

	t.Run("scroll section without enabled inherits", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		globalRender.Scroll = &types.RenderScroll{Enabled: ptrBool(true)}
		host := buildTestHost()
		host.Render.Scroll = &types.RenderScroll{}

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL(scrollTestURL)

		assert.True(t, resolved.Render.Scroll, "An empty scroll section is not an override")
	})

	t.Run("scroll and strip_scripts resolve independently", func(t *testing.T) {
		globalRender := buildTestGlobalRender()
		host := buildTestHost()
		host.Render.Scroll = scrollSection(ptrBool(true))
		host.Render.StripScripts = ptrBool(false)

		resolver := NewConfigResolver(globalRender, globalBypass, nil, nil, nil, nil, types.CompressionSnappy, host)
		resolved := resolver.ResolveForURL(scrollTestURL)

		assert.True(t, resolved.Render.Scroll)
		assert.False(t, resolved.Render.StripScripts)
	})
}

// TestResolveThreeLevelBool tests the precedence shared by strip_scripts and scroll
func TestResolveThreeLevelBool(t *testing.T) {
	tests := []struct {
		name     string
		global   *bool
		host     *bool
		rule     *bool
		def      bool
		expected bool
	}{
		{name: "all unset keeps a true default", def: true, expected: true},
		{name: "all unset keeps a false default", def: false, expected: false},
		{name: "global overrides a true default", global: ptrBool(false), def: true, expected: false},
		{name: "global overrides a false default", global: ptrBool(true), def: false, expected: true},
		{name: "host overrides global", global: ptrBool(true), host: ptrBool(false), def: true, expected: false},
		{name: "rule overrides host", global: ptrBool(false), host: ptrBool(false), rule: ptrBool(true), def: false, expected: true},
		{name: "rule overrides a default with global and host unset", rule: ptrBool(false), def: true, expected: false},
		{name: "host overrides a default with global unset", host: ptrBool(true), def: false, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, resolveThreeLevelBool(tt.global, tt.host, tt.rule, tt.def))
		})
	}
}

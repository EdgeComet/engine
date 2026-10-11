package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

const matchUATestURL = "https://example.com/page"

func TestFindMatchingRule_MatchUA(t *testing.T) {
	rules := []types.URLRule{
		uaRule("*", types.ActionStatus403, "*ClaudeBot*"),
		uaRule("/page", types.ActionRender),
	}
	matcher := NewPatternMatcher(rules)

	t.Run("UA hit", func(t *testing.T) {
		matched, idx := matcher.FindMatchingRule(matchUATestURL, claudeBotUA)
		require.NotNil(t, matched)
		assert.Equal(t, 0, idx)
		assert.Equal(t, types.ActionStatus403, matched.Action)
	})

	t.Run("UA miss falls through to the next rule", func(t *testing.T) {
		matched, idx := matcher.FindMatchingRule(matchUATestURL, googlebotUA)
		require.NotNil(t, matched)
		assert.Equal(t, 1, idx)
		assert.Equal(t, types.ActionRender, matched.Action)
	})

	t.Run("no client never matches", func(t *testing.T) {
		_, idx := matcher.FindMatchingRule(matchUATestURL, NoClientUserAgent)
		assert.Equal(t, 1, idx)
	})
}

func TestFindMatchingRule_EmptyUserAgentNeverMatchesEmptyRegexp(t *testing.T) {
	matcher := NewPatternMatcher([]types.URLRule{
		uaRule("*", types.ActionStatus403, "~^$"),
		uaRule("*", types.ActionRender),
	})

	_, idx := matcher.FindMatchingRule(matchUATestURL, NoClientUserAgent)
	assert.Equal(t, 1, idx, "~^$ matches the empty string, so only the short-circuit keeps it from matching")

	_, idx = matcher.FindMatchingRule(matchUATestURL, "AnyBot/1.0")
	assert.Equal(t, 1, idx)
}

func TestFindMatchingRule_MatchUAPatternTypes(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		ua      string
		want    bool
	}{
		// Exact matching in pkg/pattern is case-insensitive, the same as for dimension match_ua.
		{name: "exact same case", pattern: "ClaudeBot/1.0", ua: "ClaudeBot/1.0", want: true},
		{name: "exact other case", pattern: "ClaudeBot/1.0", ua: "claudebot/1.0", want: true},
		{name: "exact is a whole-string match", pattern: "ClaudeBot/1.0", ua: claudeBotUA, want: false},
		{name: "wildcard same case", pattern: "*ClaudeBot*", ua: claudeBotUA, want: true},
		{name: "wildcard other case", pattern: "*claudebot*", ua: claudeBotUA, want: true},
		{name: "regexp same case", pattern: `~ClaudeBot/\d`, ua: claudeBotUA, want: true},
		{name: "regexp other case", pattern: `~claudebot/\d`, ua: claudeBotUA, want: false},
		{name: "case-insensitive regexp", pattern: `~*claudebot/\d`, ua: claudeBotUA, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := NewPatternMatcher([]types.URLRule{uaRule("*", types.ActionStatus403, tt.pattern)})
			matched, _ := matcher.FindMatchingRule(matchUATestURL, tt.ua)
			assert.Equal(t, tt.want, matched != nil)
		})
	}
}

func TestFindMatchingRule_MatchUAAndMatchQuery(t *testing.T) {
	uaQueryRule := uaRule("/search", types.ActionStatus, "*ClaudeBot*")
	uaQueryRule.MatchQuery = map[string]interface{}{"q": "*"}
	require.NoError(t, uaQueryRule.CompilePatterns())

	matcher := NewPatternMatcher([]types.URLRule{
		uaQueryRule,
		uaRule("/search", types.ActionRender),
	})

	tests := []struct {
		name    string
		url     string
		ua      string
		wantIdx int
	}{
		{name: "path, query and UA hit", url: "https://example.com/search?q=x", ua: claudeBotUA, wantIdx: 0},
		{name: "path and query hit, UA miss", url: "https://example.com/search?q=x", ua: googlebotUA, wantIdx: 1},
		{name: "path and UA hit, query miss", url: "https://example.com/search", ua: claudeBotUA, wantIdx: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, idx := matcher.FindMatchingRule(tt.url, tt.ua)
			assert.Equal(t, tt.wantIdx, idx)
		})
	}
}

func TestFindMatchingRule_UncompiledMatchUANeverMatches(t *testing.T) {
	rule := types.URLRule{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"*ClaudeBot*"}}
	require.NoError(t, rule.CompilePatterns())

	matcher := NewPatternMatcher([]types.URLRule{rule})
	matched, idx := matcher.FindMatchingRule(matchUATestURL, claudeBotUA)
	assert.Nil(t, matched)
	assert.Equal(t, -1, idx)
}

func TestResolver_ResolveForURLIgnoresMatchUARules(t *testing.T) {
	host := buildTestHost()
	host.URLRules = []types.URLRule{
		{Match: "*", MatchUA: []string{"$AnthropicBot"}, Action: types.ActionStatus403},
	}
	require.NoError(t, PrepareHost(host, nil, "test", testLogger()))
	resolver := NewConfigResolver(buildTestGlobalRender(), buildTestGlobalBypass(), nil, nil, nil, nil, types.CompressionSnappy, host)

	noClient := resolver.ResolveForURL(matchUATestURL)
	assert.Equal(t, types.ActionRender, noClient.Action)
	assert.Empty(t, noClient.MatchedRuleID)

	bot := resolver.ResolveForRequest(matchUATestURL, claudeBotUA)
	assert.Equal(t, types.ActionStatus403, bot.Action)
	assert.Equal(t, "rule_0:*+ua", bot.MatchedRuleID)
	assert.Equal(t, "*", bot.MatchedPattern, "matched_rule in events keeps the bare pattern")

	other := resolver.ResolveForRequest(matchUATestURL, googlebotUA)
	assert.Equal(t, types.ActionRender, other.Action)
	assert.Empty(t, other.MatchedRuleID)
}

func TestResolver_ResolveRenderForURLIgnoresMatchUARenderOverrides(t *testing.T) {
	host := buildTestHost()
	host.URLRules = []types.URLRule{
		{
			Match:   "/products/*",
			MatchUA: []string{"$AnthropicBot"},
			Action:  types.ActionRender,
			Render:  &types.RenderRuleConfig{Dimension: "mobile", Timeout: ptrDuration(20 * time.Second)},
		},
	}
	require.NoError(t, PrepareHost(host, nil, "test", testLogger()))
	resolver := NewConfigResolver(buildTestGlobalRender(), buildTestGlobalBypass(), nil, nil, nil, nil, types.CompressionSnappy, host)

	const productURL = "https://example.com/products/1"

	render := resolver.ResolveRenderForURL(productURL)
	assert.Empty(t, render.Render.Dimension)
	assert.Equal(t, 30*time.Second, render.Render.Timeout, "host timeout, not the UA rule's")

	bot := resolver.ResolveForRequest(productURL, claudeBotUA)
	assert.Equal(t, "mobile", bot.Render.Dimension)
	assert.Equal(t, 20*time.Second, bot.Render.Timeout)
}

func TestResolver_MatchUARuleIDCarriesQueryAndUAMarkers(t *testing.T) {
	retryCode := 429
	host := buildTestHost()
	host.URLRules = []types.URLRule{
		{Match: "/", Action: types.ActionRender},
		{
			Match:      "*",
			MatchQuery: map[string]interface{}{"inches": "*"},
			MatchUA:    []string{"$MetaExternalAgent"},
			Action:     types.ActionStatus,
			Status:     &types.StatusRuleConfig{Code: &retryCode, Headers: map[string]string{"Retry-After": "3600"}},
		},
	}
	require.NoError(t, PrepareHost(host, nil, "test", testLogger()))
	resolver := NewConfigResolver(buildTestGlobalRender(), buildTestGlobalBypass(), nil, nil, nil, nil, types.CompressionSnappy, host)

	resolved := resolver.ResolveForRequest("https://example.com/list?inches=55", metaExternalAgentUA)
	assert.Equal(t, "rule_0:*?...+ua", resolved.MatchedRuleID)
	assert.Equal(t, retryCode, resolved.Status.Code)

	plain := resolver.ResolveForRequest("https://example.com/", metaExternalAgentUA)
	assert.Equal(t, "rule_1:/", plain.MatchedRuleID)
}

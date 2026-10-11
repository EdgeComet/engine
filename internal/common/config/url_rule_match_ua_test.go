package config

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/edgecomet/engine/internal/common/yamlutil"
	"github.com/edgecomet/engine/internal/edge/validate"
	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	claudeBotUA         = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
	googlebotUA         = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	googlebotMobileUA   = "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.6778.204 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	storebotDesktopUA   = "Mozilla/5.0 (X11; Linux x86_64; Storebot-Google/1.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.88 Safari/537.36"
	storebotMobileUA    = "Mozilla/5.0 (Linux; Android 8.0; Pixel 2 Build/OPD3.170816.012; Storebot-Google/1.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.138 Mobile Safari/537.36"
	metaExternalAgentUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36 (compatible; meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler))"
	metaWebIndexerUA    = "meta-webindexer/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)"
	facebookHitUA       = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"
)

func matchesAny(patterns []*pattern.Pattern, ua string) bool {
	for _, p := range patterns {
		if p.Match(ua) {
			return true
		}
	}
	return false
}

func uaRule(match interface{}, action types.URLRuleAction, matchUA ...string) types.URLRule {
	rule := types.URLRule{Match: match, Action: action, MatchUA: matchUA}
	_ = rule.CompilePatterns()
	_ = rule.CompileMatchUAPatterns()
	return rule
}

func sortedPatterns(rules []types.URLRule) []string {
	out := make([]string, len(rules))
	for i := range rules {
		out[i] = rules[i].GetMatchPatterns()[0]
	}
	return out
}

func TestPrepareHost_MatchUAValidationNamesSubmittedIndex(t *testing.T) {
	stripOff := false
	tests := []struct {
		name    string
		bad     types.URLRule
		wantErr string
	}{
		{
			name:    "catch-all pattern",
			bad:     types.URLRule{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"*"}},
			wantErr: "url_rule[1] match_ua[0] '*' matches every client; omit match_ua instead",
		},
		{
			name:    "tracking_params",
			bad:     types.URLRule{Match: "*", Action: types.ActionBypass, MatchUA: []string{"$AnthropicBot"}, TrackingParams: &types.TrackingParamsConfig{Strip: &stripOff}},
			wantErr: "url_rule[1] tracking_params is not allowed on a rule with match_ua; set it at host level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sorting would move the match_ua rule to index 0.
			host := &types.Host{URLRules: []types.URLRule{
				{Match: "/", Action: types.ActionRender},
				tt.bad,
			}}

			err := PrepareHost(host, nil, "test", testLogger())
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

func TestPrepareHost_MatchUADecodeParity(t *testing.T) {
	decodeYAML := func(t *testing.T, ruleYAML string) *types.Host {
		data := "hosts:\n  - id: 1\n    domain: example.com\n    render_key: key\n    url_rules:\n      - match: \"*\"\n        action: status_403\n" + ruleYAML
		var cfg HostsConfig
		require.NoError(t, yamlutil.UnmarshalStrict([]byte(data), &cfg))
		require.Len(t, cfg.Hosts, 1)
		return &cfg.Hosts[0]
	}
	decodeJSON := func(t *testing.T, ruleJSON string) *types.Host {
		data := `{"id":1,"domain":"example.com","render_key":"key","url_rules":[{"match":"*","action":"status_403"` + ruleJSON + `}]}`
		var host types.Host
		require.NoError(t, json.Unmarshal([]byte(data), &host))
		return &host
	}

	cases := []struct {
		name     string
		yamlRule string
		jsonRule string
		check    func(t *testing.T, host *types.Host, err error)
	}{
		{
			name:     "empty list rejected",
			yamlRule: "        match_ua: []\n",
			jsonRule: `,"match_ua":[]`,
			check: func(t *testing.T, host *types.Host, err error) {
				require.Error(t, err)
				assert.Equal(t, "url_rule[0] match_ua must list at least one pattern; omit it to match every client", err.Error())
			},
		},
		{
			name: "omitted accepted",
			check: func(t *testing.T, host *types.Host, err error) {
				require.NoError(t, err)
				require.Len(t, host.URLRules, 1)
				assert.Nil(t, host.URLRules[0].MatchUA)
				assert.Nil(t, host.URLRules[0].UAPatterns())
			},
		},
		{
			// The decoder cannot tell null from an omitted key; the cluster manager rejects JSON null itself.
			name:     "null decodes as omitted",
			yamlRule: "        match_ua: ~\n",
			jsonRule: `,"match_ua":null`,
			check: func(t *testing.T, host *types.Host, err error) {
				require.NoError(t, err)
				assert.Nil(t, host.URLRules[0].MatchUA)
			},
		},
		{
			name:     "alias expanded before compile",
			yamlRule: "        match_ua: [\"$AnthropicBot\", \"*CustomBot*\"]\n",
			jsonRule: `,"match_ua":["$AnthropicBot","*CustomBot*"]`,
			check: func(t *testing.T, host *types.Host, err error) {
				require.NoError(t, err)
				rule := host.URLRules[0]
				want := append(append([]string{}, BotAliases["AnthropicBot"]...), "*CustomBot*")
				assert.Equal(t, want, rule.MatchUA)
				require.Len(t, rule.UAPatterns(), len(want))
				for i, p := range rule.UAPatterns() {
					assert.Equal(t, want[i], p.Original)
				}
				assert.True(t, matchesAny(rule.UAPatterns(), claudeBotUA))
				assert.False(t, matchesAny(rule.UAPatterns(), googlebotUA))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/yaml", func(t *testing.T) {
			host := decodeYAML(t, tc.yamlRule)
			tc.check(t, host, PrepareHost(host, nil, "test", testLogger()))
		})
		t.Run(tc.name+"/json", func(t *testing.T) {
			host := decodeJSON(t, tc.jsonRule)
			tc.check(t, host, PrepareHost(host, nil, "test", testLogger()))
		})
	}
}

func TestPrepareHost_MatchUAUnknownAliasNamesSubmittedIndex(t *testing.T) {
	host := &types.Host{URLRules: []types.URLRule{
		{Match: "/", Action: types.ActionRender},
		{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"$AnthropicBot", "$NoSuchBot"}},
	}}

	err := PrepareHost(host, nil, "hosts.d/test.yaml:host_id=1", testLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "url_rule[1] match_ua: failed to expand bot aliases: unknown bot alias \"$NoSuchBot\"")
	assert.Contains(t, err.Error(), "at hosts.d/test.yaml:host_id=1:url_rule[1]")
}

func TestPrepareHost_CompilesRuleBothitRecache(t *testing.T) {
	enabled := true
	host := &types.Host{
		BothitRecache: &types.BothitRecacheConfig{Enabled: &enabled, MatchUA: []string{"$Amazonbot"}},
		URLRules: []types.URLRule{{
			Match:         "/products/*",
			Action:        types.ActionRender,
			BothitRecache: &types.BothitRecacheConfig{Enabled: &enabled, MatchUA: []string{"$AnthropicBot", "*CustomBot*"}},
		}},
	}

	require.NoError(t, PrepareHost(host, nil, "test", testLogger()))

	assert.Equal(t, BotAliases["Amazonbot"], host.BothitRecache.MatchUA)
	require.Len(t, host.BothitRecache.CompiledPatterns, len(host.BothitRecache.MatchUA))

	ruleBothit := host.URLRules[0].BothitRecache
	assert.Equal(t, append(append([]string{}, BotAliases["AnthropicBot"]...), "*CustomBot*"), ruleBothit.MatchUA)
	require.Len(t, ruleBothit.CompiledPatterns, len(ruleBothit.MatchUA))
	assert.True(t, matchesAny(ruleBothit.CompiledPatterns, claudeBotUA))
}

// Store-loaded hosts reach PrepareHost without file validation, so these are the messages an
// operator sees for a bad bothit_recache regexp.
func TestPrepareHost_BothitRecacheCompileErrorKeepsPrefix(t *testing.T) {
	bad := []string{"*ok*", "~(unclosed"}

	t.Run("host", func(t *testing.T) {
		host := &types.Host{BothitRecache: &types.BothitRecacheConfig{MatchUA: bad}}
		err := PrepareHost(host, nil, "test", testLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bothit_recache: invalid bothit_recache user agent pattern[1] '~(unclosed'")
	})

	t.Run("url rule", func(t *testing.T) {
		host := &types.Host{URLRules: []types.URLRule{
			{Match: "/", Action: types.ActionRender},
			{Match: "/products/*", Action: types.ActionRender, BothitRecache: &types.BothitRecacheConfig{MatchUA: bad}},
		}}
		err := PrepareHost(host, nil, "test", testLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url_rule[1] bothit_recache: invalid bothit_recache user agent pattern[1] '~(unclosed'")
	})
}

func TestGlobalBothitRecache_ExpandsAndCompiles(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, zaptest.NewLogger(t))
	require.NoError(t, err)

	global := cm.GetConfig().BothitRecache
	require.NotNil(t, global)
	require.NotEmpty(t, global.MatchUA)
	require.Len(t, global.CompiledPatterns, len(global.MatchUA))
	for i, p := range global.CompiledPatterns {
		assert.Equal(t, global.MatchUA[i], p.Original)
		assert.NotEqual(t, '$', rune(global.MatchUA[i][0]), "alias left unexpanded: %s", global.MatchUA[i])
	}
	assert.True(t, matchesAny(global.CompiledPatterns, googlebotUA))
}

func TestPrepareHost_MultiPatternRuleSharesCompiledMatchUA(t *testing.T) {
	host := &types.Host{URLRules: []types.URLRule{
		{Match: []string{"/a/*", "/b/*"}, Action: types.ActionBypass, MatchUA: []string{"$AnthropicBot", "~*custombot"}},
	}}

	require.NoError(t, PrepareHost(host, nil, "test", testLogger()))

	require.Len(t, host.URLRules, 2)
	first, second := host.URLRules[0], host.URLRules[1]
	assert.Equal(t, []string{"/a/*", "/b/*"}, sortedPatterns(host.URLRules))
	assert.Equal(t, first.MatchUA, second.MatchUA)
	assert.NotEmpty(t, first.MatchUA)

	require.Len(t, first.UAPatterns(), 2)
	require.Len(t, second.UAPatterns(), 2)
	assert.Same(t, &first.UAPatterns()[0], &second.UAPatterns()[0], "expanded copies must share one compiled slice")
	for i := range first.UAPatterns() {
		assert.Same(t, first.UAPatterns()[i], second.UAPatterns()[i])
	}
}

func TestSortURLRules_MatchUAFirst(t *testing.T) {
	rules := []types.URLRule{
		makeRule("/", types.ActionRender),
		makeRule("/products/*", types.ActionRender),
		uaRule("*", types.ActionStatus403, "*ClaudeBot*"),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	assert.Equal(t, []string{"*", "/", "/products/*"}, sortedPatterns(sorted))
	assert.Equal(t, []string{"*ClaudeBot*"}, sorted[0].MatchUA)
	assert.Equal(t, types.ActionStatus403, sorted[0].Action)
}

func TestSortURLRules_MatchUAGroupKeepsExistingPriorities(t *testing.T) {
	queryRule := uaRule("*", types.ActionBypass, "*ClaudeBot*")
	queryRule.MatchQuery = map[string]interface{}{"inches": "*"}
	require.NoError(t, queryRule.CompilePatterns())

	rules := []types.URLRule{
		makeRule("/plain", types.ActionRender),
		uaRule("~/api/.*", types.ActionBypass, "*ClaudeBot*"),
		uaRule("*", types.ActionStatus403, "*ClaudeBot*"),
		uaRule("/products/*", types.ActionRender, "*ClaudeBot*"),
		queryRule,
		uaRule("/", types.ActionBypass, "*ClaudeBot*"),
		uaRule("/products/shoes/*", types.ActionRender, "*ClaudeBot*"),
		uaRule("/b/*", types.ActionRender, "*Amazonbot*"),
		uaRule("/a/*", types.ActionRender, "*Amazonbot*"),
	}

	sorted, err := SortURLRules(rules)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"/",                 // exact
		"*",                 // wildcard with match_query
		"/products/shoes/*", // wildcard, 3 slashes
		"/products/*",       // wildcard, 2 slashes, declared before /b/* and /a/*
		"/b/*",              // declaration order
		"/a/*",
		"*",        // wildcard, 0 slashes
		"~/api/.*", // regexp
		"/plain",   // no match_ua
	}, sortedPatterns(sorted))
	assert.NotNil(t, sorted[1].MatchQuery)
	assert.Nil(t, sorted[6].MatchQuery)
	assert.Nil(t, sorted[8].MatchUA)
}

// legacySortURLRules is the field-by-field expansion and four-priority comparator that
// SortURLRules used before match_ua.
func legacySortURLRules(t *testing.T, rules []types.URLRule) []types.URLRule {
	type legacyRule struct {
		rule        types.URLRule
		patternType pattern.PatternType
		hasQuery    bool
		slashes     int
		index       int
	}

	var expanded []legacyRule
	for _, rule := range rules {
		for _, p := range rule.GetMatchPatterns() {
			newRule := types.URLRule{
				Match:          p,
				Action:         rule.Action,
				MatchQuery:     rule.MatchQuery,
				Render:         rule.Render,
				Bypass:         rule.Bypass,
				Status:         rule.Status,
				TrackingParams: rule.TrackingParams,
				CacheSharding:  rule.CacheSharding,
				BothitRecache:  rule.BothitRecache,
				Headers:        rule.Headers,
			}
			require.NoError(t, newRule.CompilePatterns())
			expanded = append(expanded, legacyRule{
				rule:        newRule,
				patternType: newRule.GetCompiledPattern(0).Type,
				hasQuery:    len(rule.MatchQuery) > 0,
				slashes:     countSlashes(p),
				index:       len(expanded),
			})
		}
	}

	sort.SliceStable(expanded, func(i, j int) bool {
		a, b := expanded[i], expanded[j]
		if a.patternType != b.patternType {
			return pattern.TypePriority(a.patternType) > pattern.TypePriority(b.patternType)
		}
		if a.hasQuery != b.hasQuery {
			return a.hasQuery
		}
		if a.slashes != b.slashes {
			return a.slashes > b.slashes
		}
		return a.index < b.index
	})

	out := make([]types.URLRule, len(expanded))
	for i := range expanded {
		out[i] = expanded[i].rule
	}
	return out
}

func TestSortURLRules_NoMatchUAUnchangedFromLegacy(t *testing.T) {
	enabled := true
	code := 301
	ttl := types.Duration(3600_000_000_000)
	newRules := func() []types.URLRule {
		rules := []types.URLRule{
			{Match: "*", Action: types.ActionRender},
			{Match: []string{"/admin/*", "/api/*", "/"}, Action: types.ActionStatus404},
			{Match: "/old", Action: types.ActionStatus, Status: &types.StatusRuleConfig{Code: &code, Headers: map[string]string{"Location": "/new"}}},
			{Match: "*", Action: types.ActionBypass, MatchQuery: map[string]interface{}{"inches": "*", "color": []interface{}{"red", "blue"}},
				Bypass: &types.BypassRuleConfig{Cache: &types.BypassCacheConfig{Enabled: &enabled}}},
			{Match: "~*/.*\\.(jpg|png)$", Action: types.ActionBypass},
			{Match: "/products/*", Action: types.ActionRender,
				Render:         &types.RenderRuleConfig{Dimension: "mobile", Cache: &types.RenderCacheOverride{TTL: &ttl}},
				TrackingParams: &types.TrackingParamsConfig{ParamsAdd: []string{"ref"}},
				CacheSharding:  &types.CacheShardingBehaviorConfig{Enabled: &enabled},
				BothitRecache:  &types.BothitRecacheConfig{Enabled: &enabled, MatchUA: []string{"*Googlebot*"}},
				Headers:        &types.HeadersConfig{SafeRequestAdd: []string{"X-Tenant"}}},
			{Match: "/blog/2024/*", Action: types.ActionRender},
		}
		for i := range rules {
			require.NoError(t, rules[i].CompilePatterns())
		}
		return rules
	}

	got, err := SortURLRules(newRules())
	require.NoError(t, err)

	assert.Equal(t, legacySortURLRules(t, newRules()), got)
}

func TestBotAliases_MatchProdUserAgents(t *testing.T) {
	tests := []struct {
		alias   string
		matches []string
		misses  []string
	}{
		{
			alias:   "StorebotGoogle",
			matches: []string{storebotDesktopUA, storebotMobileUA},
			misses:  []string{googlebotUA, googlebotMobileUA},
		},
		{
			alias:   "MetaExternalAgent",
			matches: []string{metaExternalAgentUA, "Mozilla/5.0 (compatible; Meta-ExternalAgent/1.1; +https://developers.facebook.com/docs/sharing/webmasters/crawler)"},
			misses:  []string{metaWebIndexerUA, facebookHitUA},
		},
		{
			alias:   "MetaWebIndexer",
			matches: []string{metaWebIndexerUA},
			misses:  []string{metaExternalAgentUA, facebookHitUA},
		},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			expanded, err := ExpandBotAliases([]string{"$" + tt.alias}, "test")
			require.NoError(t, err)
			compiled, err := pattern.CompileAll(expanded)
			require.NoError(t, err)
			for _, ua := range tt.matches {
				assert.True(t, matchesAny(compiled, ua), "should match %q", ua)
			}
			for _, ua := range tt.misses {
				assert.False(t, matchesAny(compiled, ua), "should not match %q", ua)
			}
		})
	}
}

func TestBotAliases_CompositesUnchangedByNewAliases(t *testing.T) {
	for _, composite := range []string{"AIBots", "Socials", "SearchBots"} {
		for _, p := range BotAliases[composite] {
			assert.NotContains(t, []string{"$MetaExternalAgent", "$MetaWebIndexer", "$StorebotGoogle"}, p, composite)
		}
	}
}

// PrepareHost validates match_ua before alias expansion; every alias must also pass once
// expanded, or re-preparing an already prepared host would fail.
func TestBotAliases_ExpandedPatternsPassMatchUAValidation(t *testing.T) {
	for _, alias := range GetAvailableAliases() {
		t.Run(alias, func(t *testing.T) {
			expanded, err := ExpandBotAliases([]string{"$" + alias}, "test")
			require.NoError(t, err)
			host := &types.Host{URLRules: []types.URLRule{{Match: "*", Action: types.ActionStatus403, MatchUA: expanded}}}
			assert.NoError(t, validate.ValidateURLRuleMatchUA(host))
		})
	}
}

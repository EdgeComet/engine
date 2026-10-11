package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func TestValidateMatchUAList(t *testing.T) {
	stripOff := false
	tracking := &types.TrackingParamsConfig{Strip: &stripOff}

	tests := []struct {
		name     string
		rule     types.URLRule
		wantErr  string
		errorsIs error
		// regexp errors end with the Go regexp parser's message
		prefixOnly bool
	}{
		{name: "no match_ua", rule: types.URLRule{Match: "*"}},
		{name: "no match_ua with tracking_params is untouched", rule: types.URLRule{Match: "*", TrackingParams: tracking}},
		{name: "wildcard pattern", rule: types.URLRule{Match: "*", MatchUA: []string{"*ClaudeBot*"}}},
		{name: "alias left for expansion", rule: types.URLRule{Match: "*", MatchUA: []string{"$AnthropicBot", "$UnknownAlias"}}},
		{name: "regexp catch-all is not detected", rule: types.URLRule{Match: "*", MatchUA: []string{"~.*"}}},
		{
			name:     "empty list",
			rule:     types.URLRule{Match: "*", MatchUA: []string{}},
			wantErr:  "match_ua must list at least one pattern; omit it to match every client",
			errorsIs: errMatchUAEmptyList,
		},
		{
			name:    "empty element gets the empty message, not the catch-all one",
			rule:    types.URLRule{Match: "*", MatchUA: []string{""}},
			wantErr: "match_ua[0] must not be empty",
		},
		{
			name:    "empty element names its index",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"*ClaudeBot*", ""}},
			wantErr: "match_ua[1] must not be empty",
		},
		{
			name:    "bare wildcard",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"*"}},
			wantErr: "match_ua[0] '*' matches every client; omit match_ua instead",
		},
		{
			name:    "double wildcard gets the catch-all message",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"*ClaudeBot*", "**"}},
			wantErr: "match_ua[1] '**' matches every client; omit match_ua instead",
		},
		{
			name:    "consecutive wildcards inside a pattern",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"*ClaudeBot**"}},
			wantErr: "match_ua[0]: pattern contains consecutive wildcards '**' - use single '*' for recursive matching",
		},
		{
			name:       "invalid regexp",
			rule:       types.URLRule{Match: "*", MatchUA: []string{"~(ClaudeBot"}},
			wantErr:    "match_ua[0]: invalid user-agent case-sensitive regexp '~(ClaudeBot': ",
			prefixOnly: true,
		},
		{
			name:    "empty case-insensitive regexp",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"~*"}},
			wantErr: "match_ua[0]: user-agent pattern '~*' is empty",
		},
		{
			name:     "tracking_params on a match_ua rule",
			rule:     types.URLRule{Match: "*", MatchUA: []string{"*ClaudeBot*"}, TrackingParams: tracking},
			wantErr:  "tracking_params is not allowed on a rule with match_ua; set it at host level",
			errorsIs: errMatchUATrackingParams,
		},
		{
			name:    "pattern errors are reported before tracking_params",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"*"}, TrackingParams: tracking},
			wantErr: "match_ua[0] '*' matches every client; omit match_ua instead",
		},
		{
			name:    "empty element is reported before a later catch-all",
			rule:    types.URLRule{Match: "*", MatchUA: []string{"", "*"}},
			wantErr: "match_ua[0] must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMatchUAList(&tt.rule)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tt.prefixOnly {
				assert.True(t, strings.HasPrefix(err.Error(), tt.wantErr), "error %q", err.Error())
			} else {
				assert.Equal(t, tt.wantErr, err.Error())
			}
			if tt.errorsIs != nil {
				assert.ErrorIs(t, err, tt.errorsIs)
			}
		})
	}
}

func TestValidateURLRuleMatchUA(t *testing.T) {
	t.Run("nil host", func(t *testing.T) {
		require.NoError(t, ValidateURLRuleMatchUA(nil))
	})

	t.Run("valid rules", func(t *testing.T) {
		host := &types.Host{URLRules: []types.URLRule{
			{Match: "/", Action: types.ActionRender},
			{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"$AnthropicBot"}},
		}}
		require.NoError(t, ValidateURLRuleMatchUA(host))
	})

	t.Run("error names the submitted index", func(t *testing.T) {
		host := &types.Host{URLRules: []types.URLRule{
			{Match: "/", Action: types.ActionRender},
			{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"*"}},
		}}
		err := ValidateURLRuleMatchUA(host)
		require.Error(t, err)
		assert.Equal(t, "url_rule[1] match_ua[0] '*' matches every client; omit match_ua instead", err.Error())
	})

	t.Run("empty list", func(t *testing.T) {
		host := &types.Host{URLRules: []types.URLRule{
			{Match: "*", Action: types.ActionStatus403, MatchUA: []string{}},
		}}
		err := ValidateURLRuleMatchUA(host)
		require.Error(t, err)
		assert.Equal(t, "url_rule[0] match_ua must list at least one pattern; omit it to match every client", err.Error())
	})
}

func TestValidateURLRules_ReportsMatchUA(t *testing.T) {
	collector := NewErrorCollector()
	host := &types.Host{
		Domain: "example.com",
		URLRules: []types.URLRule{
			{Match: "/", Action: types.ActionRender},
			{Match: "*", Action: types.ActionStatus403, MatchUA: []string{"**"}},
		},
	}

	validateURLRules(0, host, "hosts.yaml", nil, collector)

	require.Equal(t, 1, collector.Count())
	assert.Equal(t, "host[0] (example.com): url_rules[1]: match_ua[0] '**' matches every client; omit match_ua instead",
		collector.Errors()[0].Message)
}

func TestUserAgentPatternErrors_SameWordingForEveryUAList(t *testing.T) {
	const badRegexp = "~(ClaudeBot"
	const wording = "invalid user-agent case-sensitive regexp '~(ClaudeBot'"

	ruleErr := validateMatchUAList(&types.URLRule{Match: "*", MatchUA: []string{badRegexp}})
	require.Error(t, ruleErr)
	assert.Contains(t, ruleErr.Error(), wording)

	enabled := true
	bothitErr := validateBothitRecacheInternal(&types.BothitRecacheConfig{Enabled: &enabled, MatchUA: []string{badRegexp}}, "host")
	require.Error(t, bothitErr)
	assert.Contains(t, bothitErr.Error(), wording)

	collector := NewErrorCollector()
	validateGlobalDimensions(&configtypes.EgConfig{
		Dimensions: map[string]types.Dimension{
			"desktop": {ID: 1, Width: 1920, Height: 1080, MatchUA: []string{badRegexp}},
		},
	}, "edge-gateway.yaml", collector)
	require.Equal(t, 1, collector.Count())
	assert.Contains(t, collector.Errors()[0].Message, wording)
}

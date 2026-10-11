package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileMatchUAPatterns_ErrorPrefixes(t *testing.T) {
	bad := []string{"*ok*", "~(unclosed"}

	dim := &Dimension{MatchUA: bad}
	err := dim.CompileMatchUAPatterns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid user agent pattern[1] '~(unclosed'")

	bothit := &BothitRecacheConfig{MatchUA: bad}
	err = bothit.CompileMatchUAPatterns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bothit_recache user agent pattern[1] '~(unclosed'")

	rule := &URLRule{Match: "*", MatchUA: bad}
	err = rule.CompileMatchUAPatterns()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "match_ua pattern[1] '~(unclosed'")
	assert.Nil(t, rule.UAPatterns())
}

func TestURLRule_CompileMatchUAPatterns(t *testing.T) {
	rule := &URLRule{Match: "/products/*", Action: ActionRender, MatchUA: []string{"*ClaudeBot*", "~*amazonbot"}}

	require.NoError(t, rule.CompileMatchUAPatterns())

	compiled := rule.UAPatterns()
	require.Len(t, compiled, 2)
	assert.True(t, compiled[0].Match("Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"))
	assert.True(t, compiled[1].Match("Mozilla/5.0 (compatible; AmazonBot/0.1)"))
	assert.False(t, compiled[0].Match("Googlebot/2.1"))
}

func TestURLRule_CompilePatternsLeavesMatchUAUncompiled(t *testing.T) {
	rule := &URLRule{Match: "*", Action: ActionStatus403, MatchUA: []string{"*ClaudeBot*"}}

	require.NoError(t, rule.CompilePatterns())

	assert.Nil(t, rule.UAPatterns())
	assert.NotNil(t, rule.GetCompiledPattern(0))
}

func TestURLRule_CompileMatchUAPatterns_NoMatchUA(t *testing.T) {
	rule := &URLRule{Match: "*", Action: ActionRender}

	require.NoError(t, rule.CompileMatchUAPatterns())

	assert.Nil(t, rule.UAPatterns())
}

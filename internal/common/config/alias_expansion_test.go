package config

import (
	"testing"

	"github.com/edgecomet/engine/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestExpandDimensionAliases_SingleAlias(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: []string{"$GooglebotSearchDesktop"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["desktop"].MatchUA, 5)
	assert.Contains(t, dimensions["desktop"].MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, dimensions["desktop"].MatchUA, "Googlebot/2.1 (+http://www.google.com/bot.html)")
}

func TestExpandDimensionAliases_MultipleAliases(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"bots": {
			ID:      1,
			MatchUA: []string{"$GooglebotSearchDesktop", "$BingbotDesktop"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["bots"].MatchUA, 8)

	assert.Contains(t, dimensions["bots"].MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, dimensions["bots"].MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
}

func TestExpandDimensionAliases_MixedAliasAndCustom(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"mixed": {
			ID:      1,
			MatchUA: []string{"*CustomBot*", "$GoogleBotAds", "Mozilla/5.0 (custom pattern)"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["mixed"].MatchUA, 3)

	assert.Equal(t, "*CustomBot*", dimensions["mixed"].MatchUA[0])
	assert.Equal(t, "AdsBot-Google (+http://www.google.com/adsbot.html)", dimensions["mixed"].MatchUA[1])
	assert.Equal(t, "Mozilla/5.0 (custom pattern)", dimensions["mixed"].MatchUA[2])
}

func TestExpandDimensionAliases_UnknownAlias(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: []string{"$UnknownBot"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/etc/edge-gateway.yaml", zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown bot alias")
	assert.Contains(t, err.Error(), "$UnknownBot")
	assert.Contains(t, err.Error(), "desktop")
	assert.Contains(t, err.Error(), "/etc/edge-gateway.yaml")
}

func TestExpandDimensionAliases_EmptyMap(t *testing.T) {
	dimensions := map[string]types.Dimension{}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)
}

func TestExpandDimensionAliases_NilMap(t *testing.T) {
	err := ExpandDimensionAliases(nil, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)
}

func TestExpandDimensionAliases_EmptyMatchUA(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: []string{},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)
	assert.Len(t, dimensions["desktop"].MatchUA, 0)
}

func TestExpandDimensionAliases_NilMatchUA(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: nil,
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)
	assert.Nil(t, dimensions["desktop"].MatchUA)
}

func TestExpandDimensionAliases_NonAliasUnchanged(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID: 1,
			MatchUA: []string{
				"Mozilla/5.0 (custom)",
				"*wildcard*",
				"~^regexp$",
			},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["desktop"].MatchUA, 3)
	assert.Equal(t, "Mozilla/5.0 (custom)", dimensions["desktop"].MatchUA[0])
	assert.Equal(t, "*wildcard*", dimensions["desktop"].MatchUA[1])
	assert.Equal(t, "~^regexp$", dimensions["desktop"].MatchUA[2])
}

func TestExpandDimensionAliases_CaseSensitivity(t *testing.T) {
	tests := []struct {
		name        string
		aliasRef    string
		shouldError bool
	}{
		{
			name:        "correct case with $",
			aliasRef:    "$GooglebotSearchDesktop",
			shouldError: false,
		},
		{
			name:        "wrong case",
			aliasRef:    "$googlebotSearchDesktop",
			shouldError: true,
		},
		{
			name:        "no $ prefix",
			aliasRef:    "GooglebotSearchDesktop",
			shouldError: false,
		},
		{
			name:        "uppercase",
			aliasRef:    "$GOOGLEBOTSEARCHDESKTOP",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dimensions := map[string]types.Dimension{
				"desktop": {
					ID:      1,
					MatchUA: []string{tt.aliasRef},
				},
			}

			err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExpandDimensionAliases_MultipleDimensions(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: []string{"$GooglebotSearchDesktop"},
		},
		"mobile": {
			ID:      2,
			MatchUA: []string{"$GooglebotSearchMobile"},
		},
		"custom": {
			ID:      3,
			MatchUA: []string{"*CustomBot*"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["desktop"].MatchUA, 5)
	assert.Len(t, dimensions["mobile"].MatchUA, 4)
	assert.Len(t, dimensions["custom"].MatchUA, 1)
	assert.Equal(t, "*CustomBot*", dimensions["custom"].MatchUA[0])
}

func TestExpandDimensionAliases_AllAIBots(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"ai_bots": {
			ID: 1,
			MatchUA: []string{
				"$ChatGPTUserBot",
				"$OpenAISearchBot",
				"$PerplexityBot",
				"$AnthropicBot",
			},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["ai_bots"].MatchUA, 4)
	assert.Contains(t, dimensions["ai_bots"].MatchUA, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot")
	assert.Contains(t, dimensions["ai_bots"].MatchUA, "*OAI-SearchBot/1.0; +https://openai.com/searchbot*")
	assert.Contains(t, dimensions["ai_bots"].MatchUA, "*ClaudeBot/1.0; +claudebot@anthropic.com*")
}

func TestExpandDimensionAliases_OrderPreserved(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"ordered": {
			ID: 1,
			MatchUA: []string{
				"pattern1",
				"$GoogleBotAds",
				"pattern2",
				"$AnthropicBot",
				"pattern3",
			},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	assert.Len(t, dimensions["ordered"].MatchUA, 5)
	assert.Equal(t, "pattern1", dimensions["ordered"].MatchUA[0])
	assert.Equal(t, "AdsBot-Google (+http://www.google.com/adsbot.html)", dimensions["ordered"].MatchUA[1])
	assert.Equal(t, "pattern2", dimensions["ordered"].MatchUA[2])
	assert.Equal(t, "*ClaudeBot/1.0; +claudebot@anthropic.com*", dimensions["ordered"].MatchUA[3])
	assert.Equal(t, "pattern3", dimensions["ordered"].MatchUA[4])
}

func TestExpandDimensionAliases_ErrorMessageFormat(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"bot_dimension": {
			ID:      1,
			MatchUA: []string{"$UnknownBotAlias"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/etc/edge-gateway/config.yaml", zap.NewNop())
	require.Error(t, err)

	errorMsg := err.Error()
	assert.Contains(t, errorMsg, "unknown bot alias")
	assert.Contains(t, errorMsg, "$UnknownBotAlias")
	assert.Contains(t, errorMsg, "bot_dimension")
	assert.Contains(t, errorMsg, "/etc/edge-gateway/config.yaml")
	assert.Contains(t, errorMsg, "Available aliases:")
}

func TestExpandDimensionAliases_ErrorMessageHint(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"desktop": {
			ID:      1,
			MatchUA: []string{"$InvalidAlias"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/hosts.yaml", zap.NewNop())
	require.Error(t, err)

	errorMsg := err.Error()
	// First 5 aliases alphabetically: AIBots, AmazonUser, Amazonbot, AnthropicBot, AnthropicSearchBot
	assert.Contains(t, errorMsg, "$AIBots")
	assert.Contains(t, errorMsg, "$AmazonUser")
	assert.Contains(t, errorMsg, "$Amazonbot")
	assert.Contains(t, errorMsg, "$AnthropicBot")
	assert.Contains(t, errorMsg, "$AnthropicSearchBot")
	assert.Contains(t, errorMsg, "and 15 more") // 20 total - 5 displayed = 15 more
}

func TestExpandDimensionAliases_MultipleUnknownAliases(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"multi": {
			ID: 1,
			MatchUA: []string{
				"$FirstUnknown",
				"$SecondUnknown",
				"$ThirdUnknown",
			},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/tmp/test.yaml", zap.NewNop())
	require.Error(t, err)

	errorMsg := err.Error()
	// Now uses expandPatternsWithNesting which collects ALL unknown aliases
	assert.Contains(t, errorMsg, "$FirstUnknown")
	assert.Contains(t, errorMsg, "$SecondUnknown")
	assert.Contains(t, errorMsg, "$ThirdUnknown")
}

func TestExpandDimensionAliases_ErrorMessageAllFields(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"search_bots": {
			ID:      5,
			MatchUA: []string{"$MissingBot"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/var/config/edge.yaml", zap.NewNop())
	require.Error(t, err)

	errorMsg := err.Error()
	assert.Contains(t, errorMsg, "unknown bot alias \"$MissingBot\"")
	assert.Contains(t, errorMsg, "dimension \"search_bots\"")
	assert.Contains(t, errorMsg, "at /var/config/edge.yaml")
	assert.Contains(t, errorMsg, "\n\nAvailable aliases:")
	assert.Contains(t, errorMsg, "$")
}

func TestExpandDimensionAliases_NoLogsForNonAliasPatterns(t *testing.T) {
	observedZapCore, observedLogs := observer.New(zap.DebugLevel)
	logger := zap.New(observedZapCore)

	dimensions := map[string]types.Dimension{
		"custom": {
			ID:      1,
			MatchUA: []string{"*CustomBot*", "Mozilla/5.0 (custom)", "~^regexp$"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/etc/edge-gateway.yaml", logger)
	require.NoError(t, err)

	logs := observedLogs.All()
	assert.Len(t, logs, 0)
}

func TestExpandBotAliases_SingleAlias(t *testing.T) {
	patterns := []string{"$GooglebotSearchDesktop"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	assert.Len(t, expanded, 5)
	assert.Contains(t, expanded, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, expanded, "Googlebot/2.1 (+http://www.google.com/bot.html)")
}

func TestExpandBotAliases_MultipleAliases(t *testing.T) {
	patterns := []string{"$GooglebotSearchDesktop", "$BingbotDesktop"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	assert.Len(t, expanded, 8)
	assert.Contains(t, expanded, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, expanded, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
}

func TestExpandBotAliases_MixedAliasAndCustom(t *testing.T) {
	patterns := []string{"*CustomBot*", "$GoogleBotAds", "Mozilla/5.0 (custom pattern)"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	assert.Len(t, expanded, 3)
	assert.Equal(t, "*CustomBot*", expanded[0])
	assert.Equal(t, "AdsBot-Google (+http://www.google.com/adsbot.html)", expanded[1])
	assert.Equal(t, "Mozilla/5.0 (custom pattern)", expanded[2])
}

func TestExpandBotAliases_UnknownAlias(t *testing.T) {
	patterns := []string{"$UnknownBot"}

	expanded, err := ExpandBotAliases(patterns, "global bothit_recache")
	require.Error(t, err)
	assert.Nil(t, expanded)
	assert.Contains(t, err.Error(), "unknown bot alias")
	assert.Contains(t, err.Error(), "$UnknownBot")
	assert.Contains(t, err.Error(), "global bothit_recache")
}

func TestExpandBotAliases_EmptyArray(t *testing.T) {
	patterns := []string{}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)
	assert.Len(t, expanded, 0)
}

func TestExpandBotAliases_NilArray(t *testing.T) {
	var patterns []string

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)
	assert.Nil(t, expanded)
}

func TestExpandBotAliases_NonAliasUnchanged(t *testing.T) {
	patterns := []string{
		"Mozilla/5.0 (custom)",
		"*wildcard*",
		"~^regexp$",
	}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	assert.Len(t, expanded, 3)
	assert.Equal(t, "Mozilla/5.0 (custom)", expanded[0])
	assert.Equal(t, "*wildcard*", expanded[1])
	assert.Equal(t, "~^regexp$", expanded[2])
}

func TestExpandBotAliases_OrderPreserved(t *testing.T) {
	patterns := []string{"pattern1", "$GoogleBotAds", "pattern2", "$AnthropicBot"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	assert.Len(t, expanded, 4)
	assert.Equal(t, "pattern1", expanded[0])
	assert.Equal(t, "AdsBot-Google (+http://www.google.com/adsbot.html)", expanded[1])
	assert.Equal(t, "pattern2", expanded[2])
	assert.Equal(t, "*ClaudeBot/1.0; +claudebot@anthropic.com*", expanded[3])
}

func TestExpandBotAliases_ErrorMessageFormat(t *testing.T) {
	patterns := []string{"$NonExistentBot"}

	_, err := ExpandBotAliases(patterns, "host 'example.com', url_rule[0]")
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "unknown bot alias \"$NonExistentBot\"")
	assert.Contains(t, errMsg, "at host 'example.com', url_rule[0]")
	assert.Contains(t, errMsg, "Available aliases:")
}

func TestExpandBotAliases_CompositeSearchBots(t *testing.T) {
	patterns := []string{"$SearchBots"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	// SearchBots: GooglebotSearchDesktop(5) + GooglebotSearchMobile(4) + BingbotDesktop(3) + BingbotMobile(4) = 16
	assert.Len(t, expanded, 16)

	// Verify patterns from GooglebotSearchDesktop
	assert.Contains(t, expanded, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, expanded, "Googlebot/2.1 (+http://www.google.com/bot.html)")

	// Verify patterns from GooglebotSearchMobile
	assert.Contains(t, expanded, "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")

	// Verify patterns from BingbotDesktop
	assert.Contains(t, expanded, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

	// Verify patterns from BingbotMobile
	assert.Contains(t, expanded, "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
}

func TestExpandBotAliases_CompositeAIBots(t *testing.T) {
	patterns := []string{"$AIBots"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	// AIBots: ChatGPTUserBot(1) + ChatGPTTrainingBot(2) + OpenAISearchBot(1) + PerplexityBot(1) +
	//         PerplexityUserBot(1) + AnthropicBot(1) + AnthropicUserBot(1) + AnthropicSearchBot(1) +
	//         Amazonbot(1) + AmazonUser(1) = 11
	assert.Len(t, expanded, 11)

	// Verify patterns from various AI bots
	assert.Contains(t, expanded, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot")
	assert.Contains(t, expanded, "*OAI-SearchBot/1.0; +https://openai.com/searchbot*")
	assert.Contains(t, expanded, "*ClaudeBot/1.0; +claudebot@anthropic.com*")
	assert.Contains(t, expanded, "*Claude-User*")
	assert.Contains(t, expanded, "*Claude-SearchBot*")
	assert.Contains(t, expanded, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)")
	assert.Contains(t, expanded, "*Amazonbot/*")
	assert.Contains(t, expanded, "*AMZN-User/*")
}

func TestExpandBotAliases_CompositeWithCustomPatterns(t *testing.T) {
	patterns := []string{"*CustomBot*", "$SearchBots", "Mozilla/5.0 (my bot)"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	// 1 custom + 16 SearchBots + 1 custom = 18
	assert.Len(t, expanded, 18)

	// Verify order: custom pattern first
	assert.Equal(t, "*CustomBot*", expanded[0])
	// Last pattern should be custom
	assert.Equal(t, "Mozilla/5.0 (my bot)", expanded[17])
}

func TestExpandBotAliases_MultipleCompositeAliases(t *testing.T) {
	patterns := []string{"$SearchBots", "$AIBots"}

	expanded, err := ExpandBotAliases(patterns, "global config")
	require.NoError(t, err)

	// 16 SearchBots + 11 AIBots = 27
	assert.Len(t, expanded, 27)
}

func TestExpandBotAliases_CompositeUnknownNestedAlias(t *testing.T) {
	// Save original and restore after test
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	// Add a composite alias that references an unknown alias
	BotAliases = map[string][]string{
		"TestComposite": {"$UnknownAlias", "$GooglebotSearchDesktop"},
	}

	patterns := []string{"$TestComposite"}

	_, err := ExpandBotAliases(patterns, "test context")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown bot alias")
	assert.Contains(t, err.Error(), "$UnknownAlias")
}

func TestExpandBotAliases_ExceedsMaxNestingDepth(t *testing.T) {
	// Save original and restore after test
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	// Create a chain: Level2 -> Level1 -> Base
	BotAliases = map[string][]string{
		"BaseAlias":   {"pattern1", "pattern2"},
		"Level1Alias": {"$BaseAlias"},
		"Level2Alias": {"$Level1Alias"}, // This is 2 levels deep
	}

	patterns := []string{"$Level2Alias"}

	_, err := ExpandBotAliases(patterns, "test context")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alias nesting exceeds maximum depth")
}

func TestExpandDimensionAliases_CompositeSearchBots(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"search_bots": {
			ID:      1,
			MatchUA: []string{"$SearchBots"},
		},
	}

	err := ExpandDimensionAliases(dimensions, "/path/to/config.yaml", zap.NewNop())
	require.NoError(t, err)

	// SearchBots expands to 16 patterns
	assert.Len(t, dimensions["search_bots"].MatchUA, 16)
	assert.Contains(t, dimensions["search_bots"].MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, dimensions["search_bots"].MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
}

func TestContainsAliasReferences(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		expected bool
	}{
		{
			name:     "empty slice",
			patterns: []string{},
			expected: false,
		},
		{
			name:     "no alias references",
			patterns: []string{"pattern1", "*wildcard*", "~^regexp$"},
			expected: false,
		},
		{
			name:     "single alias reference",
			patterns: []string{"$GooglebotSearchDesktop"},
			expected: true,
		},
		{
			name:     "mixed patterns with alias",
			patterns: []string{"pattern1", "$SomeAlias", "pattern2"},
			expected: true,
		},
		{
			name:     "multiple alias references",
			patterns: []string{"$Alias1", "$Alias2"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAliasReferences(tt.patterns)
			assert.Equal(t, tt.expected, result)
		})
	}
}

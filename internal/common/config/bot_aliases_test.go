package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBotAlias_EmptyMap(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{}

	patterns, exists := GetBotAlias("$GooglebotSearchDesktop")
	assert.False(t, exists)
	assert.Nil(t, patterns)
}

func TestGetBotAlias_EdgeCases(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{}

	tests := []struct {
		name           string
		aliasName      string
		expectedExists bool
		expectedNil    bool
	}{
		{
			name:           "empty string",
			aliasName:      "",
			expectedExists: false,
			expectedNil:    true,
		},
		{
			name:           "whitespace only",
			aliasName:      "   ",
			expectedExists: false,
			expectedNil:    true,
		},
		{
			name:           "non-existent alias",
			aliasName:      "$NonExistentBot",
			expectedExists: false,
			expectedNil:    true,
		},
		{
			name:           "special characters",
			aliasName:      "$Test@Bot#123",
			expectedExists: false,
			expectedNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns, exists := GetBotAlias(tt.aliasName)
			assert.Equal(t, tt.expectedExists, exists)
			if tt.expectedNil {
				assert.Nil(t, patterns)
			}
		})
	}
}

func TestGetBotAlias_WithData(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{
		"$GooglebotSearchDesktop": {"Mozilla/5.0 (compatible; Googlebot/2.1)", "Googlebot/2.1"},
		"$BingBot":                {"Mozilla/5.0 (compatible; bingbot/2.0)"},
	}

	tests := []struct {
		name            string
		aliasName       string
		expectedExists  bool
		expectedCount   int
		expectedPattern string
	}{
		{
			name:            "existing alias with multiple patterns",
			aliasName:       "$GooglebotSearchDesktop",
			expectedExists:  true,
			expectedCount:   2,
			expectedPattern: "Mozilla/5.0 (compatible; Googlebot/2.1)",
		},
		{
			name:            "existing alias with single pattern",
			aliasName:       "$BingBot",
			expectedExists:  true,
			expectedCount:   1,
			expectedPattern: "Mozilla/5.0 (compatible; bingbot/2.0)",
		},
		{
			name:           "non-existent alias",
			aliasName:      "$NonExistent",
			expectedExists: false,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns, exists := GetBotAlias(tt.aliasName)
			assert.Equal(t, tt.expectedExists, exists)
			if tt.expectedExists {
				assert.Len(t, patterns, tt.expectedCount)
				if tt.expectedCount > 0 {
					assert.Contains(t, patterns, tt.expectedPattern)
				}
			} else {
				assert.Nil(t, patterns)
			}
		})
	}
}

func TestGetAvailableAliases_EmptyMap(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{}

	aliases := GetAvailableAliases()
	assert.NotNil(t, aliases)
	assert.Len(t, aliases, 0)
	assert.Equal(t, []string{}, aliases)
}

func TestGetAvailableAliases_WithData(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{
		"$Zebra":     {"pattern1"},
		"$Alpha":     {"pattern2"},
		"$MiddleBot": {"pattern3"},
		"$BetaBot":   {"pattern4"},
	}

	aliases := GetAvailableAliases()
	assert.Len(t, aliases, 4)
	assert.Equal(t, []string{"$Alpha", "$BetaBot", "$MiddleBot", "$Zebra"}, aliases)
}

func TestGetAvailableAliases_Immutability(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{
		"$TestBot1": {"pattern1"},
		"$TestBot2": {"pattern2"},
	}

	aliases1 := GetAvailableAliases()
	aliases2 := GetAvailableAliases()

	assert.Equal(t, aliases1, aliases2)

	aliases1[0] = "Modified"
	aliases2Second := GetAvailableAliases()
	assert.NotEqual(t, aliases1[0], aliases2Second[0])
	assert.Equal(t, "$TestBot1", aliases2Second[0])
}

func TestGetAvailableAliases_SingleAlias(t *testing.T) {
	originalAliases := BotAliases
	defer func() { BotAliases = originalAliases }()

	BotAliases = map[string][]string{
		"$OnlyBot": {"pattern1"},
	}

	aliases := GetAvailableAliases()
	assert.Len(t, aliases, 1)
	assert.Equal(t, []string{"$OnlyBot"}, aliases)
}

func TestGetBotAlias_GooglebotSearchDesktop(t *testing.T) {
	patterns, exists := GetBotAlias("GooglebotSearchDesktop")
	assert.True(t, exists)
	assert.Len(t, patterns, 5)
	assert.Contains(t, patterns, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, patterns, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Safari/537.36")
	assert.Contains(t, patterns, "Googlebot/2.1 (+http://www.google.com/bot.html)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML like Gecko\\; compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36")
}

func TestGetBotAlias_GooglebotSearchMobile(t *testing.T) {
	patterns, exists := GetBotAlias("GooglebotSearchMobile")
	assert.True(t, exists)
	assert.Len(t, patterns, 4)
	assert.Contains(t, patterns, "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\; like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)")
}

func TestGetBotAlias_GoogleBotAds(t *testing.T) {
	patterns, exists := GetBotAlias("GoogleBotAds")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "AdsBot-Google (+http://www.google.com/adsbot.html)")
}

func TestGetBotAlias_GoogleBotAdsMobileWeb(t *testing.T) {
	patterns, exists := GetBotAlias("GoogleBotAdsMobileWeb")
	assert.True(t, exists)
	assert.Len(t, patterns, 5)
	assert.Contains(t, patterns, "Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(iPhone; CPU iPhone OS 14_7_1 like Mac OS X\\) AppleWebKit\\/605\\.1\\.15 \\(KHTML, like Gecko\\) Version\\/14\\.1\\.2 Mobile\\/15E148 Safari\\/604\\.1 \\(compatible; AdsBot-Google-Mobile; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot\\.html\\)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; AdsBot-Google-Mobile\\; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot\\.html\\)")
	assert.Contains(t, patterns, "Mozilla/5.0 (Linux; Android 5.0; SM-G920A) AppleWebKit (KHTML, like Gecko) Chrome Mobile Safari (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux; Android 6\\.0\\.1; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+.\\d+.\\d+.\\d+ Mobile Safari\\/537\\.36 \\(compatible; AdsBot-Google-Mobile; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot.html\\)")
}

func TestGetBotAlias_BingbotDesktop(t *testing.T) {
	patterns, exists := GetBotAlias("BingbotDesktop")
	assert.True(t, exists)
	assert.Len(t, patterns, 3)
	assert.Contains(t, patterns, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\; compatible\\; bingbot\\/2\\.0\\; \\+http:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36 Edg\\/\\d+\\.\\d+\\.\\d+\\.\\d+")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\; compatible\\; bingbot\\/2\\.0\\; \\+http:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36")
}

func TestGetBotAlias_BingbotMobile(t *testing.T) {
	patterns, exists := GetBotAlias("BingbotMobile")
	assert.True(t, exists)
	assert.Len(t, patterns, 4)
	assert.Contains(t, patterns, "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.Contains(t, patterns, "Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 530) like Gecko (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 Edg/\\d+\\.\\d+\\.\\d+\\.\\d+ \\(compatible\\; bingbot\\/2\\.0; \\+http:\\/\\/www.bing.com\\/bingbot.htm\\)")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; bingbot\\/2\\.0\\; \\+http\\:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\)")
}

func TestGetBotAlias_ChatGPTUserBot(t *testing.T) {
	patterns, exists := GetBotAlias("ChatGPTUserBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot")
}

func TestGetBotAlias_OpenAISearchBot(t *testing.T) {
	patterns, exists := GetBotAlias("OpenAISearchBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*OAI-SearchBot/1.0; +https://openai.com/searchbot*")
}

func TestGetBotAlias_ChatGPTTrainingBot(t *testing.T) {
	patterns, exists := GetBotAlias("ChatGPTTrainingBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 2)
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\)\\; compatible\\; GPTBot\\/\\d\\.\\d\\; \\+https:\\/\\/openai\\.com\\/gptbot")
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; GPTBot\\/\\d\\.\\d\\; \\+https:\\/\\/openai\\.com\\/gptbot\\)")
}

func TestGetBotAlias_PerplexityBot(t *testing.T) {
	patterns, exists := GetBotAlias("PerplexityBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; PerplexityBot\\/\\d\\.\\d\\; \\+https:\\/\\/perplexity\\.ai\\/perplexitybot\\)")
}

func TestGetBotAlias_PerplexityUserBot(t *testing.T) {
	patterns, exists := GetBotAlias("PerplexityUserBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)")
}

func TestGetBotAlias_AnthropicBot(t *testing.T) {
	patterns, exists := GetBotAlias("AnthropicBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*ClaudeBot/1.0; +claudebot@anthropic.com*")
}

func TestGetBotAlias_AnthropicUserBot(t *testing.T) {
	patterns, exists := GetBotAlias("AnthropicUserBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*Claude-User*")
}

func TestGetBotAlias_AnthropicSearchBot(t *testing.T) {
	patterns, exists := GetBotAlias("AnthropicSearchBot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*Claude-SearchBot*")
}

func TestGetBotAlias_Messengers(t *testing.T) {
	patterns, exists := GetBotAlias("Messengers")
	assert.True(t, exists)
	assert.Len(t, patterns, 6)
	assert.Contains(t, patterns, "*WhatsApp/*")
	assert.Contains(t, patterns, "*ViberBot*")
	assert.Contains(t, patterns, "*Telegrambot*")
	assert.Contains(t, patterns, "*Snapchat*")
	assert.Contains(t, patterns, "*Discordbot*")
	assert.Contains(t, patterns, "*Slackbot*")
}

func TestGetBotAlias_Socials(t *testing.T) {
	patterns, exists := GetBotAlias("Socials")
	assert.True(t, exists)
	assert.Len(t, patterns, 4)
	assert.Contains(t, patterns, "*facebookexternalhit*")
	assert.Contains(t, patterns, "*twitterbot*")
	assert.Contains(t, patterns, "*Pinterestbot/*")
	assert.Contains(t, patterns, "*Applebot/*")
}

func TestGetBotAlias_Amazonbot(t *testing.T) {
	patterns, exists := GetBotAlias("Amazonbot")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*Amazonbot/*")
}

func TestGetBotAlias_AmazonUser(t *testing.T) {
	patterns, exists := GetBotAlias("AmazonUser")
	assert.True(t, exists)
	assert.Len(t, patterns, 1)
	assert.Contains(t, patterns, "*AMZN-User/*")
}

func TestGetBotAlias_SearchBots(t *testing.T) {
	patterns, exists := GetBotAlias("SearchBots")
	assert.True(t, exists)
	assert.Len(t, patterns, 4)
	assert.Contains(t, patterns, "$GooglebotSearchDesktop")
	assert.Contains(t, patterns, "$GooglebotSearchMobile")
	assert.Contains(t, patterns, "$BingbotDesktop")
	assert.Contains(t, patterns, "$BingbotMobile")
}

func TestGetBotAlias_AIBots(t *testing.T) {
	patterns, exists := GetBotAlias("AIBots")
	assert.True(t, exists)
	assert.Len(t, patterns, 10)
	assert.Contains(t, patterns, "$ChatGPTUserBot")
	assert.Contains(t, patterns, "$ChatGPTTrainingBot")
	assert.Contains(t, patterns, "$OpenAISearchBot")
	assert.Contains(t, patterns, "$PerplexityBot")
	assert.Contains(t, patterns, "$PerplexityUserBot")
	assert.Contains(t, patterns, "$AnthropicBot")
	assert.Contains(t, patterns, "$AnthropicUserBot")
	assert.Contains(t, patterns, "$AnthropicSearchBot")
	assert.Contains(t, patterns, "$Amazonbot")
	assert.Contains(t, patterns, "$AmazonUser")
}

func TestGetAvailableAliases_AllBots(t *testing.T) {
	aliases := GetAvailableAliases()
	assert.Len(t, aliases, 20)

	assert.Contains(t, aliases, "GooglebotSearchDesktop")
	assert.Contains(t, aliases, "GooglebotSearchMobile")
	assert.Contains(t, aliases, "GoogleBotAds")
	assert.Contains(t, aliases, "GoogleBotAdsMobileWeb")
	assert.Contains(t, aliases, "BingbotDesktop")
	assert.Contains(t, aliases, "BingbotMobile")
	assert.Contains(t, aliases, "ChatGPTUserBot")
	assert.Contains(t, aliases, "OpenAISearchBot")
	assert.Contains(t, aliases, "ChatGPTTrainingBot")
	assert.Contains(t, aliases, "PerplexityBot")
	assert.Contains(t, aliases, "PerplexityUserBot")
	assert.Contains(t, aliases, "AnthropicBot")
	assert.Contains(t, aliases, "AnthropicUserBot")
	assert.Contains(t, aliases, "AnthropicSearchBot")
	assert.Contains(t, aliases, "Amazonbot")
	assert.Contains(t, aliases, "AmazonUser")
	assert.Contains(t, aliases, "Messengers")
	assert.Contains(t, aliases, "Socials")
	assert.Contains(t, aliases, "SearchBots")
	assert.Contains(t, aliases, "AIBots")

	assert.Equal(t, []string{
		"AIBots",
		"AmazonUser",
		"Amazonbot",
		"AnthropicBot",
		"AnthropicSearchBot",
		"AnthropicUserBot",
		"BingbotDesktop",
		"BingbotMobile",
		"ChatGPTTrainingBot",
		"ChatGPTUserBot",
		"GoogleBotAds",
		"GoogleBotAdsMobileWeb",
		"GooglebotSearchDesktop",
		"GooglebotSearchMobile",
		"Messengers",
		"OpenAISearchBot",
		"PerplexityBot",
		"PerplexityUserBot",
		"SearchBots",
		"Socials",
	}, aliases)
}

func TestGetAvailableAliases_GoogleBots(t *testing.T) {
	aliases := GetAvailableAliases()
	assert.GreaterOrEqual(t, len(aliases), 4)
	assert.Contains(t, aliases, "GooglebotSearchDesktop")
	assert.Contains(t, aliases, "GooglebotSearchMobile")
	assert.Contains(t, aliases, "GoogleBotAds")
	assert.Contains(t, aliases, "GoogleBotAdsMobileWeb")
}

func TestGetBotAlias_CaseSensitivity(t *testing.T) {
	tests := []struct {
		name           string
		aliasName      string
		expectedExists bool
	}{
		{
			name:           "correct case - GooglebotSearchDesktop",
			aliasName:      "GooglebotSearchDesktop",
			expectedExists: true,
		},
		{
			name:           "lowercase first letter",
			aliasName:      "googlebotSearchDesktop",
			expectedExists: false,
		},
		{
			name:           "all lowercase",
			aliasName:      "googlebotsearchdesktop",
			expectedExists: false,
		},
		{
			name:           "all uppercase",
			aliasName:      "GOOGLEBOTSEARCHDESKTOP",
			expectedExists: false,
		},
		{
			name:           "correct case - GooglebotSearchMobile",
			aliasName:      "GooglebotSearchMobile",
			expectedExists: true,
		},
		{
			name:           "wrong case mobile",
			aliasName:      "googlebotSearchMobile",
			expectedExists: false,
		},
		{
			name:           "correct case - BingbotDesktop",
			aliasName:      "BingbotDesktop",
			expectedExists: true,
		},
		{
			name:           "wrong case - bingbotDesktop",
			aliasName:      "bingbotDesktop",
			expectedExists: false,
		},
		{
			name:           "correct case - BingbotMobile",
			aliasName:      "BingbotMobile",
			expectedExists: true,
		},
		{
			name:           "wrong case - bingbotMobile",
			aliasName:      "bingbotMobile",
			expectedExists: false,
		},
		{
			name:           "correct case - ChatGPTUserBot",
			aliasName:      "ChatGPTUserBot",
			expectedExists: true,
		},
		{
			name:           "wrong case - chatGPTUserBot",
			aliasName:      "chatGPTUserBot",
			expectedExists: false,
		},
		{
			name:           "correct case - AnthropicBot",
			aliasName:      "AnthropicBot",
			expectedExists: true,
		},
		{
			name:           "wrong case - anthropicBot",
			aliasName:      "anthropicBot",
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns, exists := GetBotAlias(tt.aliasName)
			assert.Equal(t, tt.expectedExists, exists)
			if !tt.expectedExists {
				assert.Nil(t, patterns)
			}
		})
	}
}

func TestBotAliases_MapStructure(t *testing.T) {
	aliases := GetAvailableAliases()
	assert.NotEmpty(t, aliases, "BotAliases map should not be empty")

	for _, aliasName := range aliases {
		t.Run(aliasName, func(t *testing.T) {
			patterns, exists := GetBotAlias(aliasName)
			assert.True(t, exists, "Alias %s should exist", aliasName)
			assert.NotNil(t, patterns, "Patterns for %s should not be nil", aliasName)
			assert.NotEmpty(t, patterns, "Patterns for %s should not be empty", aliasName)

			for i, pattern := range patterns {
				assert.NotEmpty(t, pattern, "Pattern %d for %s should not be empty", i, aliasName)
			}
		})
	}
}

func TestBotAliases_NamingConvention(t *testing.T) {
	aliases := GetAvailableAliases()

	for _, aliasName := range aliases {
		t.Run(aliasName, func(t *testing.T) {
			assert.NotEmpty(t, aliasName, "Alias name should not be empty")

			firstChar := aliasName[0]
			assert.True(t, firstChar >= 'A' && firstChar <= 'Z',
				"Alias %s should start with uppercase letter (PascalCase)", aliasName)

			assert.NotContains(t, aliasName, " ", "Alias %s should not contain spaces", aliasName)
			assert.NotContains(t, aliasName, "-", "Alias %s should not contain hyphens", aliasName)
			assert.NotContains(t, aliasName, "_", "Alias %s should not contain underscores", aliasName)
		})
	}
}

func TestBotAliases_VendorGrouping(t *testing.T) {
	aliases := GetAvailableAliases()

	googleAliases := []string{}
	bingAliases := []string{}
	openAIAliases := []string{}
	perplexityAliases := []string{}
	anthropicAliases := []string{}
	amazonAliases := []string{}
	messengerAliases := []string{}
	socialAliases := []string{}
	compositeAliases := []string{}

	for _, alias := range aliases {
		switch {
		case len(alias) >= 6 && alias[:6] == "Google":
			googleAliases = append(googleAliases, alias)
		case len(alias) >= 7 && alias[:7] == "Bingbot":
			bingAliases = append(bingAliases, alias)
		case len(alias) >= 7 && (alias[:7] == "ChatGPT" || alias[:6] == "OpenAI"):
			openAIAliases = append(openAIAliases, alias)
		case len(alias) >= 10 && alias[:10] == "Perplexity":
			perplexityAliases = append(perplexityAliases, alias)
		case len(alias) >= 9 && alias[:9] == "Anthropic":
			anthropicAliases = append(anthropicAliases, alias)
		case len(alias) >= 6 && alias[:6] == "Amazon":
			amazonAliases = append(amazonAliases, alias)
		case alias == "Messengers":
			messengerAliases = append(messengerAliases, alias)
		case alias == "Socials":
			socialAliases = append(socialAliases, alias)
		case alias == "SearchBots" || alias == "AIBots":
			compositeAliases = append(compositeAliases, alias)
		}
	}

	assert.Len(t, googleAliases, 4, "Should have 4 Google aliases")
	assert.Len(t, bingAliases, 2, "Should have 2 Bing aliases")
	assert.Len(t, openAIAliases, 3, "Should have 3 OpenAI aliases")
	assert.Len(t, perplexityAliases, 2, "Should have 2 Perplexity aliases")
	assert.Len(t, anthropicAliases, 3, "Should have 3 Anthropic aliases")
	assert.Len(t, amazonAliases, 2, "Should have 2 Amazon aliases")
	assert.Len(t, messengerAliases, 1, "Should have 1 Messengers alias")
	assert.Len(t, socialAliases, 1, "Should have 1 Socials alias")
	assert.Len(t, compositeAliases, 2, "Should have 2 composite aliases")
}

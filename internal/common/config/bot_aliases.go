package config

import "sort"

// BotAliases maps alias names to user agent patterns.
// Aliases allow grouping common bot user agent patterns under memorable names.
// Example: "$GooglebotSearchDesktop" -> ["Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", ...]
// Alias names typically start with "$" to distinguish them from literal patterns.
var BotAliases = map[string][]string{
	"GooglebotSearchDesktop": {
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Safari/537.36",
		"Googlebot/2.1 (+http://www.google.com/bot.html)",
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36",
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML like Gecko\\; compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36",
	},
	"GooglebotSearchMobile": {
		"Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\; like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; Googlebot\\/2\\.1\\; \\+http:\\/\\/www\\.google\\.com\\/bot\\.html\\)",
	},
	"GoogleBotAds": {
		"AdsBot-Google (+http://www.google.com/adsbot.html)",
	},
	"GoogleBotAdsMobileWeb": {
		"Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)",
		"~^Mozilla\\/5\\.0 \\(iPhone; CPU iPhone OS 14_7_1 like Mac OS X\\) AppleWebKit\\/605\\.1\\.15 \\(KHTML, like Gecko\\) Version\\/14\\.1\\.2 Mobile\\/15E148 Safari\\/604\\.1 \\(compatible; AdsBot-Google-Mobile; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot\\.html\\)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; AdsBot-Google-Mobile\\; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot\\.html\\)",
		"Mozilla/5.0 (Linux; Android 5.0; SM-G920A) AppleWebKit (KHTML, like Gecko) Chrome Mobile Safari (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)",
		"~^Mozilla\\/5\\.0 \\(Linux; Android 6\\.0\\.1; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+.\\d+.\\d+.\\d+ Mobile Safari\\/537\\.36 \\(compatible; AdsBot-Google-Mobile; \\+http\\:\\/\\/www\\.google\\.com\\/mobile\\/adsbot.html\\)",
	},
	"BingbotDesktop": {
		"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\; compatible\\; bingbot\\/2\\.0\\; \\+http:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36 Edg\\/\\d+\\.\\d+\\.\\d+\\.\\d+",
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\; compatible\\; bingbot\\/2\\.0\\; \\+http:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Safari\\/537\\.36",
	},
	"BingbotMobile": {
		"Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 530) like Gecko (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 Edg/\\d+\\.\\d+\\.\\d+\\.\\d+ \\(compatible\\; bingbot\\/2\\.0; \\+http:\\/\\/www.bing.com\\/bingbot.htm\\)",
		"~^Mozilla\\/5\\.0 \\(Linux\\; Android 6\\.0\\.1\\; Nexus 5X Build\\/MMB29P\\) AppleWebKit\\/537\\.36 \\(KHTML, like Gecko\\) Chrome\\/\\d+\\.\\d+\\.\\d+\\.\\d+ Mobile Safari\\/537\\.36 \\(compatible\\; bingbot\\/2\\.0\\; \\+http\\:\\/\\/www\\.bing\\.com\\/bingbot\\.htm\\)",
	},
	"ChatGPTUserBot": {
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
	},
	"OpenAISearchBot": {
		"*OAI-SearchBot/1.0; +https://openai.com/searchbot*",
	},
	"ChatGPTTrainingBot": {
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\)\\; compatible\\; GPTBot\\/\\d\\.\\d\\; \\+https:\\/\\/openai\\.com\\/gptbot",
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; GPTBot\\/\\d\\.\\d\\; \\+https:\\/\\/openai\\.com\\/gptbot\\)",
	},
	"PerplexityBot": {
		"~^Mozilla\\/5\\.0 AppleWebKit\\/537\\.36 \\(KHTML\\, like Gecko\\; compatible\\; PerplexityBot\\/\\d\\.\\d\\; \\+https:\\/\\/perplexity\\.ai\\/perplexitybot\\)",
	},
	"PerplexityUserBot": {
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)",
	},
	"AnthropicBot": {
		"*ClaudeBot/1.0; +claudebot@anthropic.com*",
	},
	"AnthropicUserBot": {
		"*Claude-User*",
	},
	"AnthropicSearchBot": {
		"*Claude-SearchBot*",
	},
	"Amazonbot": {
		"*Amazonbot/*",
	},
	"AmazonUser": {
		"*AMZN-User/*",
	},

	"Messengers": {
		"*WhatsApp/*",
		"*ViberBot*",
		"*Telegrambot*",
		"*Snapchat*",
		"*Discordbot*",
		"*Slackbot*",
	},
	"Socials": {
		"*facebookexternalhit*",
		"*twitterbot*",
		"*Pinterestbot/*",
		"*Applebot/*",
	},

	// Composite aliases - reference other aliases with $ prefix (single level nesting)
	"SearchBots": {
		"$GooglebotSearchDesktop",
		"$GooglebotSearchMobile",
		"$BingbotDesktop",
		"$BingbotMobile",
	},
	"AIBots": {
		"$ChatGPTUserBot",
		"$ChatGPTTrainingBot",
		"$OpenAISearchBot",
		"$PerplexityBot",
		"$PerplexityUserBot",
		"$AnthropicBot",
		"$AnthropicUserBot",
		"$AnthropicSearchBot",
		"$Amazonbot",
		"$AmazonUser",
	},
}

// GetBotAlias returns the user agent patterns for a given alias name.
// Returns the patterns and true if the alias exists, nil and false otherwise.
func GetBotAlias(name string) ([]string, bool) {
	patterns, exists := BotAliases[name]
	return patterns, exists
}

// GetAvailableAliases returns a sorted list of all available alias names.
func GetAvailableAliases() []string {
	if len(BotAliases) == 0 {
		return []string{}
	}

	aliases := make([]string, 0, len(BotAliases))
	for name := range BotAliases {
		aliases = append(aliases, name)
	}
	sort.Strings(aliases)
	return aliases
}

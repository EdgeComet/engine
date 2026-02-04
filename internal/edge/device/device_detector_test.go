package device

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/pattern"
	"github.com/edgecomet/engine/pkg/types"
)

func TestDeviceDetector_DetectDimension(t *testing.T) {
	detector := NewDeviceDetector()
	logger := zap.NewNop()

	host := &types.Host{
		Render: types.RenderConfig{
			UnmatchedDimension: "desktop",
			Dimensions: map[string]types.Dimension{
				"mobile": {
					ID:      2,
					MatchUA: []string{"*Googlebot-Mobile*", "*iPhone*", "*Android*"},
				},
				"desktop": {
					ID:      1,
					MatchUA: []string{"*Googlebot*", "*bingbot*"},
				},
			},
		},
	}

	// Compile patterns for all dimensions
	for name, dim := range host.Render.Dimensions {
		err := dim.CompileMatchUAPatterns()
		if err != nil {
			t.Fatalf("Failed to compile patterns for dimension %s: %v", name, err)
		}
		host.Render.Dimensions[name] = dim
	}

	tests := []struct {
		name              string
		userAgent         string
		expectedDimension string
		expectedMatched   bool
	}{
		{
			name:              "Googlebot desktop",
			userAgent:         "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			expectedDimension: "desktop",
			expectedMatched:   true,
		},
		{
			name:              "Googlebot mobile",
			userAgent:         "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/W.X.Y.Z Mobile Safari/537.36 (compatible; Googlebot-Mobile)",
			expectedDimension: "mobile",
			expectedMatched:   true,
		},
		{
			name:              "iPhone user agent",
			userAgent:         "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15",
			expectedDimension: "mobile",
			expectedMatched:   true,
		},
		{
			name:              "Unknown user agent - no match",
			userAgent:         "Unknown/1.0",
			expectedDimension: "",
			expectedMatched:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetUserAgent(tt.userAgent)

			renderCtx := edgectx.NewRenderContext("test-request", ctx, logger, 30*time.Second).WithHost(host)
			dimension, matched := detector.DetectDimension(renderCtx)
			assert.Equal(t, tt.expectedDimension, dimension)
			assert.Equal(t, tt.expectedMatched, matched)
		})
	}
}

func TestDeviceDetector_DetectDimensionNoMatch(t *testing.T) {
	detector := NewDeviceDetector()
	logger := zap.NewNop()

	host := &types.Host{
		Render: types.RenderConfig{
			UnmatchedDimension: "mobile",
			Dimensions: map[string]types.Dimension{
				"mobile": {
					ID:      1,
					MatchUA: []string{"*Googlebot-Mobile*"},
				},
			},
		},
	}

	// Compile patterns for all dimensions
	for name, dim := range host.Render.Dimensions {
		err := dim.CompileMatchUAPatterns()
		if err != nil {
			t.Fatalf("Failed to compile patterns for dimension %s: %v", name, err)
		}
		host.Render.Dimensions[name] = dim
	}

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetUserAgent("Unknown/1.0")

	renderCtx := edgectx.NewRenderContext("test-request", ctx, logger, 30*time.Second).WithHost(host)
	dimension, matched := detector.DetectDimension(renderCtx)
	assert.Equal(t, "", dimension, "DeviceDetector should return empty string for unmatched User-Agent")
	assert.False(t, matched, "Unknown User-Agent should not match any pattern")
}

func TestDeviceDetector_DetectDimension_RegexpPatterns(t *testing.T) {
	detector := NewDeviceDetector()
	logger := zap.NewNop()

	// Create dimensions with regexp patterns
	mobileDim := types.Dimension{
		ID:      2,
		MatchUA: []string{"~Mobile.*Googlebot"},
	}
	desktopDim := types.Dimension{
		ID:      1,
		MatchUA: []string{"~*googlebot"},
	}

	// Compile patterns
	err := mobileDim.CompileMatchUAPatterns()
	assert.NoError(t, err)
	err = desktopDim.CompileMatchUAPatterns()
	assert.NoError(t, err)

	host := &types.Host{
		Render: types.RenderConfig{
			Dimensions: map[string]types.Dimension{
				"mobile":  mobileDim,
				"desktop": desktopDim,
			},
		},
	}

	tests := []struct {
		name              string
		userAgent         string
		expectedDimension string
		expectedMatched   bool
	}{
		{
			name:              "Mobile Googlebot matches mobile regexp",
			userAgent:         "Mozilla/5.0 (Linux; Android 6.0.1) Mobile Safari/537.36 (compatible; Googlebot/2.1)",
			expectedDimension: "mobile",
			expectedMatched:   true,
		},
		{
			name:              "Desktop Googlebot matches case-insensitive regexp",
			userAgent:         "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			expectedDimension: "desktop",
			expectedMatched:   true,
		},
		{
			name:              "Case variations match case-insensitive regexp",
			userAgent:         "Mozilla/5.0 (compatible; GOOGLEBOT/2.1)",
			expectedDimension: "desktop",
			expectedMatched:   true,
		},
		{
			name:              "Unknown user agent - no match",
			userAgent:         "Unknown/1.0",
			expectedDimension: "",
			expectedMatched:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetUserAgent(tt.userAgent)

			renderCtx := edgectx.NewRenderContext("test-request", ctx, logger, 30*time.Second).WithHost(host)
			dimension, matched := detector.DetectDimension(renderCtx)
			assert.Equal(t, tt.expectedDimension, dimension)
			assert.Equal(t, tt.expectedMatched, matched)
		})
	}
}

func TestDeviceDetector_DetectDimension_PatternPriority(t *testing.T) {
	detector := NewDeviceDetector()
	logger := zap.NewNop()

	// Create dimensions with different pattern types
	// mobile uses regexp (should be checked first)
	mobileDim := types.Dimension{
		ID:      2,
		MatchUA: []string{"~Mobile.*Googlebot"},
	}
	// desktop uses wildcard (should be checked after regexp)
	desktopDim := types.Dimension{
		ID:      1,
		MatchUA: []string{"*Googlebot*"},
	}

	// Compile patterns
	err := mobileDim.CompileMatchUAPatterns()
	assert.NoError(t, err)
	err = desktopDim.CompileMatchUAPatterns()
	assert.NoError(t, err)

	host := &types.Host{
		Render: types.RenderConfig{
			Dimensions: map[string]types.Dimension{
				"mobile":  mobileDim,
				"desktop": desktopDim,
			},
		},
	}

	tests := []struct {
		name              string
		userAgent         string
		expectedDimension string
		reason            string
	}{
		{
			name:              "Mobile Googlebot matches mobile regexp (priority)",
			userAgent:         "Mozilla/5.0 (Linux) Mobile Safari (compatible; Googlebot/2.1)",
			expectedDimension: "mobile",
			reason:            "Regexp patterns have higher priority than wildcard",
		},
		{
			name:              "Desktop Googlebot matches desktop wildcard",
			userAgent:         "Mozilla/5.0 (compatible; Googlebot/2.1)",
			expectedDimension: "desktop",
			reason:            "Only desktop pattern matches (no Mobile in UA)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetUserAgent(tt.userAgent)

			renderCtx := edgectx.NewRenderContext("test-request", ctx, logger, 30*time.Second).WithHost(host)
			dimension, matched := detector.DetectDimension(renderCtx)
			assert.Equal(t, tt.expectedDimension, dimension, tt.reason)
			assert.True(t, matched)
		})
	}
}

// TestGoogleBotUserAgentMatching validates that bot alias patterns correctly match real Google bot user agents
// User agent strings are from Google's official documentation:
// https://developers.google.com/search/docs/crawling-indexing/overview-google-crawlers
func TestGoogleBotUserAgentMatching(t *testing.T) {
	tests := []struct {
		aliasName   string
		userAgent   string
		shouldMatch bool
		description string
	}{
		// GooglebotSearchDesktop - exact match patterns
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: true,
			description: "Googlebot desktop - common format",
		},
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Safari/537.36",
			shouldMatch: true,
			description: "Googlebot desktop - WebKit format",
		},
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Googlebot/2.1 (+http://www.google.com/bot.html)",
			shouldMatch: true,
			description: "Googlebot desktop - minimal format",
		},
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: true,
			description: "Googlebot desktop - Chrome version format (regexp pattern)",
		},
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/115.0.5790.170 Safari/537.36",
			shouldMatch: true,
			description: "Googlebot desktop - Chrome without comma after KHTML (regexp pattern)",
		},

		// GooglebotSearchMobile - mobile patterns
		{
			aliasName:   "GooglebotSearchMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: true,
			description: "Googlebot mobile - Nexus 5X with comma after KHTML (regexp pattern)",
		},
		{
			aliasName:   "GooglebotSearchMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML like Gecko) Chrome/118.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: true,
			description: "Googlebot mobile - Nexus 5X without comma after KHTML (regexp pattern)",
		},
		{
			aliasName:   "GooglebotSearchMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			shouldMatch: true,
			description: "Googlebot mobile - exact match pattern",
		},

		// GoogleBotAds - desktop ads bot
		{
			aliasName:   "GoogleBotAds",
			userAgent:   "AdsBot-Google (+http://www.google.com/adsbot.html)",
			shouldMatch: true,
			description: "AdsBot-Google - desktop format",
		},

		// GoogleBotAdsMobileWeb - mobile ads bot
		{
			aliasName:   "GoogleBotAdsMobileWeb",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)",
			shouldMatch: true,
			description: "AdsBot-Google-Mobile - iPhone format (exact match)",
		},
		{
			aliasName:   "GoogleBotAdsMobileWeb",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)",
			shouldMatch: true,
			description: "AdsBot-Google-Mobile - Android format (regexp pattern)",
		},

		// Negative tests - regular browsers should NOT match
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: false,
			description: "Regular Chrome browser - should NOT match Googlebot patterns",
		},
		{
			aliasName:   "GooglebotSearchDesktop",
			userAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: false,
			description: "Chrome on macOS - should NOT match Googlebot patterns",
		},
		{
			aliasName:   "GooglebotSearchMobile",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			shouldMatch: false,
			description: "Safari on iPhone - should NOT match Googlebot patterns",
		},
		{
			aliasName:   "GooglebotSearchMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			shouldMatch: false,
			description: "Chrome on Android - should NOT match Googlebot patterns",
		},
		{
			aliasName:   "GoogleBotAds",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
			shouldMatch: false,
			description: "Firefox - should NOT match AdsBot patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			patterns, exists := config.GetBotAlias(tt.aliasName)
			require.True(t, exists, "Bot alias %s should exist", tt.aliasName)
			require.NotEmpty(t, patterns, "Bot alias %s should have patterns", tt.aliasName)

			// Compile all patterns for this alias
			var compiledPatterns []*pattern.Pattern
			for _, patternStr := range patterns {
				compiled, err := pattern.Compile(patternStr)
				require.NoError(t, err, "Pattern should compile: %s", patternStr)
				compiledPatterns = append(compiledPatterns, compiled)
			}

			// Check if user agent matches any pattern
			matched := false
			for _, compiledPattern := range compiledPatterns {
				if compiledPattern.Match(tt.userAgent) {
					matched = true
					break
				}
			}

			if tt.shouldMatch {
				assert.True(t, matched, "User agent should match at least one pattern in %s: %s", tt.aliasName, tt.userAgent)
			} else {
				assert.False(t, matched, "User agent should NOT match any pattern in %s: %s", tt.aliasName, tt.userAgent)
			}
		})
	}
}

// TestBingBotUserAgentMatching validates that Bing bot alias patterns correctly match real user agents
// User agent strings are from Microsoft's official documentation:
// https://www.bing.com/webmasters/help/which-crawlers-does-bing-use-8c184ec0
func TestBingBotUserAgentMatching(t *testing.T) {
	tests := []struct {
		aliasName   string
		userAgent   string
		shouldMatch bool
		description string
	}{
		// BingbotDesktop - desktop crawler patterns
		{
			aliasName:   "BingbotDesktop",
			userAgent:   "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			shouldMatch: true,
			description: "Bingbot desktop - common format",
		},
		{
			aliasName:   "BingbotDesktop",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: true,
			description: "Bingbot desktop - Chrome format (regexp pattern)",
		},
		{
			aliasName:   "BingbotDesktop",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/115.0.5790.170 Safari/537.36 Edg/115.0.1901.203",
			shouldMatch: true,
			description: "Bingbot desktop - Chrome with Edge format (regexp pattern)",
		},

		// BingbotMobile - mobile crawler patterns
		{
			aliasName:   "BingbotMobile",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			shouldMatch: true,
			description: "Bingbot mobile - iPhone format (exact match)",
		},
		{
			aliasName:   "BingbotMobile",
			userAgent:   "Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 530) like Gecko (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			shouldMatch: true,
			description: "Bingbot mobile - Windows Phone format (exact match)",
		},
		{
			aliasName:   "BingbotMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			shouldMatch: true,
			description: "Bingbot mobile - Android format (regexp pattern)",
		},
		{
			aliasName:   "BingbotMobile",
			userAgent:   "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Mobile Safari/537.36 Edg/118.0.2088.46 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			shouldMatch: true,
			description: "Bingbot mobile - Android with Edge format (regexp pattern)",
		},

		// Negative tests - regular browsers should NOT match
		{
			aliasName:   "BingbotDesktop",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: false,
			description: "Regular Chrome browser - should NOT match Bingbot patterns",
		},
		{
			aliasName:   "BingbotDesktop",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			shouldMatch: false,
			description: "Microsoft Edge browser - should NOT match Bingbot patterns",
		},
		{
			aliasName:   "BingbotMobile",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			shouldMatch: false,
			description: "Safari on iPhone - should NOT match Bingbot patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			patterns, exists := config.GetBotAlias(tt.aliasName)
			require.True(t, exists, "Bot alias %s should exist", tt.aliasName)
			require.NotEmpty(t, patterns, "Bot alias %s should have patterns", tt.aliasName)

			// Compile all patterns for this alias
			var compiledPatterns []*pattern.Pattern
			for _, patternStr := range patterns {
				compiled, err := pattern.Compile(patternStr)
				require.NoError(t, err, "Pattern should compile: %s", patternStr)
				compiledPatterns = append(compiledPatterns, compiled)
			}

			// Check if user agent matches any pattern
			matched := false
			for _, compiledPattern := range compiledPatterns {
				if compiledPattern.Match(tt.userAgent) {
					matched = true
					break
				}
			}

			if tt.shouldMatch {
				assert.True(t, matched, "User agent should match at least one pattern in %s: %s", tt.aliasName, tt.userAgent)
			} else {
				assert.False(t, matched, "User agent should NOT match any pattern in %s: %s", tt.aliasName, tt.userAgent)
			}
		})
	}
}

// TestAIBotUserAgentMatching validates that AI bot alias patterns correctly match real user agents
// User agent strings are from respective companies' official documentation:
// - OpenAI: https://platform.openai.com/docs/bots
// - Perplexity: https://docs.perplexity.ai/docs/perplexitybot
// - Anthropic: https://docs.anthropic.com/en/api/bot
func TestAIBotUserAgentMatching(t *testing.T) {
	tests := []struct {
		aliasName   string
		userAgent   string
		shouldMatch bool
		description string
	}{
		// ChatGPTUserBot - OpenAI chat user bot
		{
			aliasName:   "ChatGPTUserBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
			shouldMatch: true,
			description: "ChatGPT-User bot - exact match",
		},

		// OpenAISearchBot - OpenAI search bot
		{
			aliasName:   "OpenAISearchBot",
			userAgent:   "OAI-SearchBot/1.0; +https://openai.com/searchbot",
			shouldMatch: true,
			description: "OAI-SearchBot - wildcard match",
		},
		{
			aliasName:   "OpenAISearchBot",
			userAgent:   "Mozilla/5.0 (compatible; OAI-SearchBot/1.0; +https://openai.com/searchbot)",
			shouldMatch: true,
			description: "OAI-SearchBot - with prefix (wildcard match)",
		},

		// ChatGPTTrainingBot - OpenAI training bot
		{
			aliasName:   "ChatGPTTrainingBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.0; +https://openai.com/gptbot",
			shouldMatch: true,
			description: "GPTBot - version 1.0 (regexp pattern)",
		},
		{
			aliasName:   "ChatGPTTrainingBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)",
			shouldMatch: true,
			description: "GPTBot - version 1.2 with parenthesis (regexp pattern)",
		},
		{
			aliasName:   "ChatGPTTrainingBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/2.5; +https://openai.com/gptbot",
			shouldMatch: true,
			description: "GPTBot - version 2.5 without closing paren (regexp pattern)",
		},

		// PerplexityBot - Perplexity search bot
		{
			aliasName:   "PerplexityBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)",
			shouldMatch: true,
			description: "PerplexityBot - version 1.0 (regexp pattern)",
		},
		{
			aliasName:   "PerplexityBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/2.3; +https://perplexity.ai/perplexitybot)",
			shouldMatch: true,
			description: "PerplexityBot - version 2.3 (regexp pattern)",
		},

		// PerplexityUserBot - Perplexity user bot
		{
			aliasName:   "PerplexityUserBot",
			userAgent:   "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)",
			shouldMatch: true,
			description: "Perplexity-User bot - exact match",
		},

		// AnthropicBot - Anthropic Claude bot
		{
			aliasName:   "AnthropicBot",
			userAgent:   "ClaudeBot/1.0; +claudebot@anthropic.com",
			shouldMatch: true,
			description: "ClaudeBot - wildcard match",
		},
		{
			aliasName:   "AnthropicBot",
			userAgent:   "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)",
			shouldMatch: true,
			description: "ClaudeBot - with prefix (wildcard match)",
		},

		// AnthropicUserBot - Anthropic user bot
		{
			aliasName:   "AnthropicUserBot",
			userAgent:   "Claude-User",
			shouldMatch: true,
			description: "Claude-User bot - wildcard match",
		},
		{
			aliasName:   "AnthropicUserBot",
			userAgent:   "Mozilla/5.0 (compatible; Claude-User)",
			shouldMatch: true,
			description: "Claude-User bot - with prefix (wildcard match)",
		},

		// AnthropicSearchBot - Anthropic search bot
		{
			aliasName:   "AnthropicSearchBot",
			userAgent:   "Claude-SearchBot",
			shouldMatch: true,
			description: "Claude-SearchBot - wildcard match",
		},
		{
			aliasName:   "AnthropicSearchBot",
			userAgent:   "Mozilla/5.0 (compatible; Claude-SearchBot/1.0)",
			shouldMatch: true,
			description: "Claude-SearchBot - with prefix (wildcard match)",
		},

		// Negative tests - regular browsers should NOT match
		{
			aliasName:   "ChatGPTUserBot",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: false,
			description: "Regular Chrome browser - should NOT match ChatGPT patterns",
		},
		{
			aliasName:   "OpenAISearchBot",
			userAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			shouldMatch: false,
			description: "Chrome on macOS - should NOT match OpenAI patterns",
		},
		{
			aliasName:   "PerplexityBot",
			userAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
			shouldMatch: false,
			description: "Firefox - should NOT match Perplexity patterns",
		},
		{
			aliasName:   "AnthropicBot",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			shouldMatch: false,
			description: "Safari on iPhone - should NOT match Anthropic patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			patterns, exists := config.GetBotAlias(tt.aliasName)
			require.True(t, exists, "Bot alias %s should exist", tt.aliasName)
			require.NotEmpty(t, patterns, "Bot alias %s should have patterns", tt.aliasName)

			// Compile all patterns for this alias
			var compiledPatterns []*pattern.Pattern
			for _, patternStr := range patterns {
				compiled, err := pattern.Compile(patternStr)
				require.NoError(t, err, "Pattern should compile: %s", patternStr)
				compiledPatterns = append(compiledPatterns, compiled)
			}

			// Check if user agent matches any pattern
			matched := false
			for _, compiledPattern := range compiledPatterns {
				if compiledPattern.Match(tt.userAgent) {
					matched = true
					break
				}
			}

			if tt.shouldMatch {
				assert.True(t, matched, "User agent should match at least one pattern in %s: %s", tt.aliasName, tt.userAgent)
			} else {
				assert.False(t, matched, "User agent should NOT match any pattern in %s: %s", tt.aliasName, tt.userAgent)
			}
		})
	}
}

// TestGoogleBotUserAgentMatchingComprehensive validates all bot aliases have valid patterns
func TestGoogleBotUserAgentMatchingComprehensive(t *testing.T) {
	// Ensure all bot aliases are accessible and have valid patterns
	aliases := config.GetAvailableAliases()
	require.NotEmpty(t, aliases, "Should have bot aliases defined")

	for _, aliasName := range aliases {
		t.Run(aliasName, func(t *testing.T) {
			patterns, exists := config.GetBotAlias(aliasName)
			require.True(t, exists, "Alias %s should exist", aliasName)
			require.NotEmpty(t, patterns, "Alias %s should have at least one pattern", aliasName)

			// All patterns should compile without errors
			for _, patternStr := range patterns {
				compiled, err := pattern.Compile(patternStr)
				require.NoError(t, err, "Pattern should compile for alias %s: %s", aliasName, patternStr)
				require.NotNil(t, compiled, "Compiled pattern should not be nil for %s", patternStr)
			}
		})
	}
}

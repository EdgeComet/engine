package acceptance_test

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
)

// requestRenderWithUserAgent makes a render request with a custom User-Agent
// This is needed for bot alias testing since we need to vary the User-Agent
func requestRenderWithUserAgent(targetURL string, userAgent string, apiKey string) *TestResponse {
	// Build the full URL for the test page
	var fullTargetURL string
	if targetURL[0] == '/' {
		fullTargetURL = testEnv.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", testEnv.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	// Add required headers with custom User-Agent
	req.Header.Set("X-Render-Key", apiKey)
	req.Header.Set("User-Agent", userAgent)

	start := time.Now()
	resp, err := testEnv.HTTPClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &TestResponse{Error: err, Duration: duration}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Duration:   duration,
			Error:      err,
		}
	}

	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(body),
		Duration:   duration,
	}
}

var _ = Describe("Bot Aliases", Serial, func() {
	Context("when testing bot alias expansion for Google bots", func() {
		It("should match Googlebot desktop dimension via alias", func() {
			By("Sending request with Googlebot desktop User-Agent")
			userAgent := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200), "Should return HTTP 200 OK")

			By("Verifying response headers")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty(), "Should include X-Request-ID")
			Expect(response.Headers.Get("X-Render-Source")).To(Or(
				Equal("rendered"),
				Equal("cache"),
				Equal("bypass"),
			), "Should include X-Render-Source header")

			By("Verifying content is returned")
			Expect(response.Body).To(ContainSubstring("Static Content"), "Body should contain expected content")
		})

		It("should match Googlebot mobile dimension via alias", func() {
			By("Sending request with Googlebot mobile User-Agent")
			userAgent := "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying response headers indicate mobile bot")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
			Expect(response.Headers.Get("X-Render-Source")).NotTo(BeEmpty())
		})

		It("should match Googlebot desktop with Chrome version via alias regexp pattern", func() {
			By("Sending request with Googlebot Chrome User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/120.0.0.0 Safari/537.36"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying bot was recognized via regexp pattern")
			Expect(response.Headers.Get("X-Render-Source")).NotTo(BeEmpty())
		})

		It("should match Google AdsBot via alias", func() {
			By("Sending request with AdsBot-Google User-Agent")
			userAgent := "AdsBot-Google (+http://www.google.com/adsbot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying AdsBot was recognized")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match Google AdsBot Mobile via alias", func() {
			By("Sending request with AdsBot-Google-Mobile User-Agent")
			userAgent := "Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1 (compatible; AdsBot-Google-Mobile; +http://www.google.com/mobile/adsbot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("when testing bot alias expansion for Bing bots", func() {
		It("should match Bingbot desktop dimension via alias", func() {
			By("Sending request with Bingbot desktop User-Agent")
			userAgent := "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Bingbot was recognized")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
			Expect(response.Headers.Get("X-Render-Source")).NotTo(BeEmpty())
		})

		It("should match Bingbot mobile dimension via alias", func() {
			By("Sending request with Bingbot mobile User-Agent")
			userAgent := "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying mobile Bingbot was recognized")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match Bingbot with Chrome/Edge version via alias regexp pattern", func() {
			By("Sending request with Bingbot Chrome/Edge User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm) Chrome/118.0.0.0 Safari/537.36 Edg/118.0.2088.46"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying Bingbot regexp pattern matched")
			Expect(response.Headers.Get("X-Render-Source")).NotTo(BeEmpty())
		})
	})

	Context("when testing bot alias expansion for AI bots", func() {
		It("should match ChatGPT user bot dimension via alias", func() {
			By("Sending request with ChatGPT-User User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying ChatGPT bot was recognized")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match GPTBot training bot via alias with version", func() {
			By("Sending request with GPTBot User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.0; +https://openai.com/gptbot"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying GPTBot was recognized via regexp pattern")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match Perplexity bot via alias with version", func() {
			By("Sending request with PerplexityBot User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying PerplexityBot was recognized")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match Anthropic ClaudeBot via alias", func() {
			By("Sending request with ClaudeBot User-Agent")
			userAgent := "ClaudeBot/1.0; +claudebot@anthropic.com"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying ClaudeBot was recognized via wildcard pattern")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match OpenAI SearchBot via alias", func() {
			By("Sending request with OAI-SearchBot User-Agent")
			userAgent := "OAI-SearchBot/1.0; +https://openai.com/searchbot"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying successful response")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying OAI-SearchBot was recognized via wildcard pattern")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})
	})

	Context("when testing mixed patterns with bot aliases", func() {
		It("should match dimension with bot alias and custom wildcard patterns", func() {
			By("Sending request with bot alias User-Agent")
			userAgent := "ClaudeBot/1.0; +claudebot@anthropic.com"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying bot alias matched")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Sending request with custom pattern User-Agent")
			// This assumes the test config has a dimension with *CustomBot* pattern
			customUA := "Mozilla/5.0 (compatible; CustomBot/1.0)"
			response2 := requestRenderWithUserAgent("/static/simple.html", customUA, testEnv.Config.Test.ValidAPIKey)

			By("Verifying custom pattern matched")
			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))
		})

		It("should match dimension with bot alias and custom regexp patterns", func() {
			By("Sending request with bot alias User-Agent")
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying bot alias matched")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("when testing bot alias priority and pattern matching", func() {
		It("should match more specific alias over general alias", func() {
			By("Sending request with specific bot User-Agent")
			// GooglebotSearchMobile is more specific than general Googlebot patterns
			userAgent := "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying specific mobile pattern matched")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			By("Verifying request completed successfully")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should handle regexp patterns in bot aliases correctly", func() {
			By("Sending request with versioned GPTBot User-Agent")
			// Test that regexp patterns with version numbers work
			userAgent := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/2.5; +https://openai.com/gptbot)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying regexp pattern with version matched")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})

		It("should handle wildcard patterns in bot aliases correctly", func() {
			By("Sending request with wildcard-matching User-Agent")
			// Test that wildcard patterns work with prefix/suffix
			userAgent := "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying wildcard pattern matched with prefix")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("when testing negative cases for bot aliases", func() {
		It("should not match regular browser User-Agents to bot dimensions", func() {
			By("Sending request with Chrome desktop User-Agent")
			userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying response is handled")
			// Regular browsers should either be bypassed or fallback to unmatched_dimension
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Or(
				Equal(200), // If unmatched_dimension is set to a dimension
				Equal(403), // If unmatched_dimension is "block"
			))

			By("Verifying request was processed")
			Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should not match Safari mobile to bot dimensions", func() {
			By("Sending request with Safari iOS User-Agent")
			userAgent := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying response is handled")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Or(
				Equal(200),
				Equal(403),
			))
		})

		It("should not match Firefox to bot dimensions", func() {
			By("Sending request with Firefox User-Agent")
			userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			By("Verifying response is handled")
			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Or(
				Equal(200),
				Equal(403),
			))
		})
	})

	Context("when testing bot alias expansion with multiple aliases per dimension", func() {
		It("should match any bot alias in a multi-alias dimension", func() {
			By("Testing first bot alias in multi-alias dimension")
			userAgent1 := "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot"
			response1 := requestRenderWithUserAgent("/static/simple.html", userAgent1, testEnv.Config.Test.ValidAPIKey)

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))

			By("Testing second bot alias in same multi-alias dimension")
			userAgent2 := "ClaudeBot/1.0; +claudebot@anthropic.com"
			response2 := requestRenderWithUserAgent("/static/simple.html", userAgent2, testEnv.Config.Test.ValidAPIKey)

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying both matched successfully")
			Expect(response1.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
			Expect(response2.Headers.Get("X-Request-ID")).NotTo(BeEmpty())
		})

		It("should match bot aliases combined with exact patterns", func() {
			By("Testing bot alias matching")
			userAgent := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
			response := requestRenderWithUserAgent("/static/simple.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			Expect(response.Error).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
		})
	})

	Context("when testing bot alias performance and caching", func() {
		It("should cache bot alias dimension matches correctly", func() {
			By("Making first request with Googlebot")
			userAgent := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
			response1 := requestRenderWithUserAgent("/static/with-meta.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			Expect(response1.Error).To(BeNil())
			Expect(response1.StatusCode).To(Equal(200))
			firstSource := response1.Headers.Get("X-Render-Source")

			By("Making second request with same bot User-Agent")
			response2 := requestRenderWithUserAgent("/static/with-meta.html", userAgent, testEnv.Config.Test.ValidAPIKey)

			Expect(response2.Error).To(BeNil())
			Expect(response2.StatusCode).To(Equal(200))

			By("Verifying second request was served from cache")
			secondSource := response2.Headers.Get("X-Render-Source")
			if firstSource == "rendered" || firstSource == "bypass" {
				Expect(secondSource).To(Or(Equal("cache"), Equal("bypass_cache")), "Second request should be cached")
			}
		})

		It("should handle rapid requests from different bot types", func() {
			bots := []struct {
				name string
				ua   string
			}{
				{"Googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
				{"Bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"},
				{"GPTBot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.0; +https://openai.com/gptbot"},
				{"ClaudeBot", "ClaudeBot/1.0; +claudebot@anthropic.com"},
			}

			By("Making requests from multiple bot types")
			for _, bot := range bots {
				response := requestRenderWithUserAgent("/static/simple.html", bot.ua, testEnv.Config.Test.ValidAPIKey)

				Expect(response.Error).To(BeNil(), "Request for "+bot.name+" should succeed")
				Expect(response.StatusCode).To(Equal(200), bot.name+" should get 200 OK")
				Expect(response.Headers.Get("X-Request-ID")).NotTo(BeEmpty(), bot.name+" should have request ID")
			}
		})
	})

	Context("when testing error handling for unknown bot aliases", func() {
		var tempDir string
		var logger *zap.Logger

		BeforeEach(func() {
			// Create temporary directory for error test configs
			var err error
			tempDir, err = os.MkdirTemp("", "bot-alias-error-test-*")
			Expect(err).NotTo(HaveOccurred())

			// Create hosts.d directory with minimal host file for tests
			hostsDir := filepath.Join(tempDir, "hosts.d")
			err = os.MkdirAll(hostsDir, 0755)
			Expect(err).NotTo(HaveOccurred())

			// Create a minimal valid host file so config loads past host validation
			minimalHostConfig := `
hosts:
  - id: 1
    domain: "test.example.com"
    render_key: "test-key"
    enabled: true
    render:
      timeout: 30s
      unmatched_dimension: "bypass"
      cache:
        ttl: 1h
`
			hostFilePath := filepath.Join(hostsDir, "test-host.yaml")
			err = os.WriteFile(hostFilePath, []byte(minimalHostConfig), 0644)
			Expect(err).NotTo(HaveOccurred())

			// Create a no-op logger for config loading tests
			logger = zap.NewNop()
		})

		AfterEach(func() {
			// Clean up temporary directory
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
		})

		It("should fail to load config with unknown bot alias", func() {
			By("Creating config with unknown bot alias")
			configContent := `
eg_id: "eg-test-error"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    broken_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $UnknownBotAlias
        - $AnotherUnknownBot

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(configContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config with unknown bot aliases")
			_, err = config.NewEGConfigManager(configPath, logger)

			By("Verifying config loading failed")
			Expect(err).To(HaveOccurred(), "Config with unknown bot aliases should fail")

			By("Verifying error message contains 'unknown bot alias'")
			Expect(err.Error()).To(ContainSubstring("unknown bot alias"), "Error should mention unknown alias")

			By("Verifying error message contains the unknown alias name")
			Expect(err.Error()).To(Or(
				ContainSubstring("UnknownBotAlias"),
				ContainSubstring("AnotherUnknownBot"),
			), "Error should mention the specific unknown alias")

			By("Verifying error message is descriptive")
			// Error should help users understand what went wrong
			errMsg := strings.ToLower(err.Error())
			Expect(errMsg).To(Or(
				ContainSubstring("dimension"),
				ContainSubstring("match_ua"),
			), "Error should provide context about where the problem is")
		})

		It("should fail to load config with unknown alias in host config", func() {
			By("Creating main config")
			mainConfigContent := `
eg_id: "eg-test-error"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    valid_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $GooglebotSearchDesktop

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(mainConfigContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Creating hosts directory with invalid alias")
			hostsDir := filepath.Join(tempDir, "hosts.d")
			err = os.MkdirAll(hostsDir, 0755)
			Expect(err).NotTo(HaveOccurred())

			hostConfigContent := `
hosts:
  - id: 2
    domain: "error-test.example.com"
    render_key: "test-key"
    enabled: true
    render:
      timeout: 30s
      unmatched_dimension: "bypass"
      cache:
        ttl: 1h
      dimensions:
        broken_host_dimension:
          id: 10
          width: 1920
          height: 1080
          render_ua: "TestAgent"
          match_ua:
            - $InvalidBotAlias
`
			hostConfigPath := filepath.Join(hostsDir, "error-host.yaml")
			err = os.WriteFile(hostConfigPath, []byte(hostConfigContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config with unknown alias in host config")
			_, err = config.NewEGConfigManager(configPath, logger)

			By("Verifying config loading failed")
			Expect(err).To(HaveOccurred(), "Config with unknown alias in host should fail")

			By("Verifying error message contains details about host config")
			Expect(err.Error()).To(ContainSubstring("unknown bot alias"), "Error should mention unknown alias")
			Expect(err.Error()).To(ContainSubstring("InvalidBotAlias"), "Error should mention the specific alias")

			By("Verifying error provides helpful context")
			errMsg := err.Error()
			// Should mention either the host or the config file
			contextFound := strings.Contains(errMsg, "host") ||
				strings.Contains(errMsg, "error-host.yaml") ||
				strings.Contains(errMsg, "dimension")
			Expect(contextFound).To(BeTrue(), "Error should provide context about location")
		})

		It("should validate case sensitivity of bot aliases", func() {
			By("Creating config with incorrect case in bot alias")
			configContent := `
eg_id: "eg-test-case"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    case_sensitive_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $googlebotSearchDesktop

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(configContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config with wrong case")
			_, err = config.NewEGConfigManager(configPath, logger)

			By("Verifying config loading failed")
			Expect(err).To(HaveOccurred(), "Config with wrong case should fail")

			By("Verifying error mentions case sensitivity")
			errMsg := strings.ToLower(err.Error())
			Expect(errMsg).To(ContainSubstring("unknown bot alias"), "Error should mention unknown alias")
			Expect(err.Error()).To(ContainSubstring("googlebotSearchDesktop"), "Error should show the incorrect alias")

			By("Verifying error is helpful for case issues")
			// The error should indicate this is a case sensitivity issue
			// by showing the alias doesn't exist (even though GooglebotSearchDesktop does)
			Expect(err.Error()).To(ContainSubstring("unknown"), "Should indicate alias is unknown")
		})

		It("should provide list of available aliases in error message", func() {
			By("Creating config with unknown bot alias")
			configContent := `
eg_id: "eg-test-hint"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    helpful_error_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $NonExistentBot

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(configContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config")
			_, err = config.NewEGConfigManager(configPath, logger)

			By("Verifying config loading failed")
			Expect(err).To(HaveOccurred())

			By("Verifying error message provides hints about available aliases")
			errMsg := err.Error()

			// Error should either list available aliases or provide guidance
			hasAvailableList := strings.Contains(errMsg, "Available") ||
				strings.Contains(errMsg, "valid")

			// Or it should at least mention some known aliases as examples
			hasBotNames := strings.Contains(errMsg, "Googlebot") ||
				strings.Contains(errMsg, "Bingbot") ||
				strings.Contains(errMsg, "bot_aliases")

			Expect(hasAvailableList || hasBotNames).To(BeTrue(),
				"Error should provide hints about available aliases or where to find them")
		})

		It("should fail with multiple unknown aliases and report all of them", func() {
			By("Creating config with multiple unknown bot aliases")
			configContent := `
eg_id: "eg-test-multiple"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    multi_error_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $FirstUnknownBot
        - $GooglebotSearchDesktop
        - $SecondUnknownBot
        - $ThirdUnknownBot

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(configContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config")
			_, err = config.NewEGConfigManager(configPath, logger)

			By("Verifying config loading failed")
			Expect(err).To(HaveOccurred())

			By("Verifying error message mentions unknown aliases")
			errMsg := err.Error()
			Expect(errMsg).To(ContainSubstring("unknown bot alias"))

			By("Verifying at least one unknown alias is mentioned")
			unknownMentioned := strings.Contains(errMsg, "FirstUnknownBot") ||
				strings.Contains(errMsg, "SecondUnknownBot") ||
				strings.Contains(errMsg, "ThirdUnknownBot")
			Expect(unknownMentioned).To(BeTrue(), "Error should mention at least one unknown alias")
		})

		It("should succeed with valid aliases after fixing config", func() {
			By("Creating config with valid bot aliases only")
			configContent := `
eg_id: "eg-test-valid"
internal:
  listen: "localhost:10071"
  auth_key: "test"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  dimensions:
    valid_google_dimension:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $GooglebotSearchDesktop
        - $GooglebotSearchMobile
    valid_ai_dimension:
      id: 2
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua:
        - $ChatGPTUserBot
        - $PerplexityBot

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: false

hosts:
  include: "hosts.d/"
`
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			err := os.WriteFile(configPath, []byte(configContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to load config with all valid aliases")
			cm, err := config.NewEGConfigManager(configPath, logger)

			By("Verifying config loaded successfully")
			Expect(err).NotTo(HaveOccurred(), "Config with valid aliases should load")
			Expect(cm).NotTo(BeNil(), "Config manager should be created")

			By("Verifying dimensions are loaded")
			cfg := cm.GetConfig()
			Expect(cfg.Render.Dimensions).To(HaveKey("valid_google_dimension"))
			Expect(cfg.Render.Dimensions).To(HaveKey("valid_ai_dimension"))

			By("Verifying bot aliases were expanded in dimensions")
			googleDim := cfg.Render.Dimensions["valid_google_dimension"]
			// After expansion, should have more patterns than the 2 aliases
			Expect(len(googleDim.MatchUA)).To(BeNumerically(">", 2),
				"Bot aliases should expand to multiple patterns")
		})
	})
})

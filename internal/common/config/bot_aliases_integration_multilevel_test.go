package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBothitRecache_Integration_AllThreeLevels tests all three configuration levels with different aliases
func TestBothitRecache_Integration_AllThreeLevels(t *testing.T) {
	tmpDir := t.TempDir()

	// Global config with BingbotDesktop
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $BingbotDesktop
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Host config with GooglebotSearchDesktop at host level
	// and ChatGPTUserBot at pattern level
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $GooglebotSearchDesktop
    url_rules:
      - match: "/blog/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $ChatGPTUserBot
`
	hostConfigPath := filepath.Join(hostsDir, "01-example.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify global has Bingbot patterns
	globalConfig := manager.GetConfig()
	require.NotNil(t, globalConfig.BothitRecache)
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 3, "Global should have 3 Bingbot patterns")
	assert.Contains(t, globalConfig.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

	// Verify host has Google patterns (overrides global)
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)
	host := hosts[0]
	require.NotNil(t, host.BothitRecache)
	assert.Len(t, host.BothitRecache.MatchUA, 5, "Host should have 5 Googlebot patterns")
	assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.NotContains(t, host.BothitRecache.MatchUA[0], "bingbot", "Host should not have Bingbot patterns")

	// Verify pattern has ChatGPT patterns (overrides host)
	require.Len(t, host.URLRules, 1)
	blogRule := host.URLRules[0]
	require.NotNil(t, blogRule.BothitRecache)
	assert.Len(t, blogRule.BothitRecache.MatchUA, 1, "Pattern should have 1 ChatGPT pattern")
	assert.Contains(t, blogRule.BothitRecache.MatchUA[0], "ChatGPT-User")
	assert.NotContains(t, blogRule.BothitRecache.MatchUA[0], "Googlebot", "Pattern should not have Googlebot patterns")
	assert.NotContains(t, blogRule.BothitRecache.MatchUA[0], "bingbot", "Pattern should not have Bingbot patterns")
}

// TestBothitRecache_Integration_MixedPatternsAllLevels tests mixing aliases and custom patterns at all levels
func TestBothitRecache_Integration_MixedPatternsAllLevels(t *testing.T) {
	tmpDir := t.TempDir()

	// Global: alias + custom pattern
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $GooglebotSearchDesktop
    - "*Slurp*"
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Host: alias + custom pattern, Pattern: alias + custom pattern
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $BingbotDesktop
        - "*DuckDuckBot*"
    url_rules:
      - match: "/api/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $ChatGPTUserBot
            - "*CustomBot*"
`
	hostConfigPath := filepath.Join(hostsDir, "01-example.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify global: 5 Googlebot + 1 custom = 6
	globalConfig := manager.GetConfig()
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 6)
	assert.Equal(t, "*Slurp*", globalConfig.BothitRecache.MatchUA[5], "Custom pattern should be last")

	// Verify host: 3 Bingbot + 1 custom = 4
	hosts := manager.GetHosts()
	host := hosts[0]
	assert.Len(t, host.BothitRecache.MatchUA, 4)
	assert.Equal(t, "*DuckDuckBot*", host.BothitRecache.MatchUA[3], "Custom pattern should be last")

	// Verify pattern: 1 ChatGPT + 1 custom = 2
	apiRule := host.URLRules[0]
	assert.Len(t, apiRule.BothitRecache.MatchUA, 2)
	assert.Contains(t, apiRule.BothitRecache.MatchUA[0], "ChatGPT-User")
	assert.Equal(t, "*CustomBot*", apiRule.BothitRecache.MatchUA[1], "Custom pattern should be last")
}

// TestBothitRecache_Integration_RealWorldScenario tests a realistic e-commerce site configuration
func TestBothitRecache_Integration_RealWorldScenario(t *testing.T) {
	tmpDir := t.TempDir()

	// Realistic global config for e-commerce
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 24h
  match_ua:
    - $GooglebotSearchDesktop
    - $GooglebotSearchMobile
    - $BingbotDesktop
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// E-commerce shop with specific bot requirements
	hostYAML := `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "shop-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 12h
      match_ua:
        - $GoogleBotAds
        - $GoogleBotAdsMobileWeb
    url_rules:
      - match: "/admin/*"
        action: "status_403"
`
	hostConfigPath := filepath.Join(hostsDir, "01-shop.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify global has multiple bot types
	globalConfig := manager.GetConfig()
	require.NotNil(t, globalConfig.BothitRecache)
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 5+4+3) // 5 GoogleDesktop + 4 GoogleMobile + 3 Bing = 12

	// Count bot types in global
	googlebotCount := 0
	bingbotCount := 0
	for _, pattern := range globalConfig.BothitRecache.MatchUA {
		if contains(pattern, "Googlebot") || contains(pattern, "googlebot") {
			googlebotCount++
		}
		if contains(pattern, "bingbot") {
			bingbotCount++
		}
	}
	assert.Equal(t, 9, googlebotCount, "Global should have 9 Googlebot patterns (5 desktop + 4 mobile)")
	assert.Equal(t, 3, bingbotCount, "Global should have 3 Bingbot patterns")

	// Verify host overrides with Ads bots only
	hosts := manager.GetHosts()
	host := hosts[0]
	require.NotNil(t, host.BothitRecache)
	assert.Len(t, host.BothitRecache.MatchUA, 1+5) // 1 GoogleBotAds + 5 GoogleBotAdsMobileWeb = 6

	// Verify admin pattern has no bothit_recache (status action)
	require.Len(t, host.URLRules, 1)
	adminRule := host.URLRules[0]
	assert.Nil(t, adminRule.BothitRecache, "Admin pattern should not have bothit_recache")
}

// TestBothitRecache_Integration_InheritanceChain tests the inheritance chain from global to host to pattern
func TestBothitRecache_Integration_InheritanceChain(t *testing.T) {
	tmpDir := t.TempDir()

	// Global with GooglebotSearchDesktop
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $GooglebotSearchDesktop
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Host1: no bothit_recache (inherits global)
	// Pattern in Host1: no bothit_recache (inherits from host/global)
	host1YAML := `
hosts:
  - id: 1
    domain: "blog.example.com"
    render_key: "blog-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/posts/*"
        action: "render"
`
	err = os.WriteFile(filepath.Join(hostsDir, "01-blog.yaml"), []byte(host1YAML), 0o644)
	require.NoError(t, err)

	// Host2: bothit_recache with BingbotDesktop
	// Pattern in Host2: bothit_recache with ChatGPTUserBot
	host2YAML := `
hosts:
  - id: 2
    domain: "shop.example.com"
    render_key: "shop-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $BingbotDesktop
    url_rules:
      - match: "/products/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $ChatGPTUserBot
`
	err = os.WriteFile(filepath.Join(hostsDir, "02-shop.yaml"), []byte(host2YAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify global
	globalConfig := manager.GetConfig()
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 5)

	hosts := manager.GetHosts()
	require.Len(t, hosts, 2)

	// Verify Host1 does not have bothit_recache (inherits at runtime)
	host1 := hosts[0]
	assert.Equal(t, "blog.example.com", host1.Domain)
	assert.Nil(t, host1.BothitRecache, "Host1 should not have bothit_recache - inherits from global at runtime")

	// Verify Host1 pattern does not have bothit_recache (inherits at runtime)
	require.Len(t, host1.URLRules, 1)
	host1Pattern := host1.URLRules[0]
	assert.Nil(t, host1Pattern.BothitRecache, "Host1 pattern should not have bothit_recache - inherits from host/global")

	// Verify Host2 has Bingbot (expanded at host level)
	host2 := hosts[1]
	assert.Equal(t, "shop.example.com", host2.Domain)
	require.NotNil(t, host2.BothitRecache)
	assert.Len(t, host2.BothitRecache.MatchUA, 3)
	assert.Contains(t, host2.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

	// Verify Host2 pattern has ChatGPT (expanded at pattern level)
	require.Len(t, host2.URLRules, 1)
	host2Pattern := host2.URLRules[0]
	require.NotNil(t, host2Pattern.BothitRecache)
	assert.Len(t, host2Pattern.BothitRecache.MatchUA, 1)
	assert.Contains(t, host2Pattern.BothitRecache.MatchUA[0], "ChatGPT-User")
}

// TestBothitRecache_Integration_ComplexMultiHost tests complex scenario with multiple hosts and patterns
func TestBothitRecache_Integration_ComplexMultiHost(t *testing.T) {
	tmpDir := t.TempDir()

	// Global with GooglebotSearchDesktop
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $GooglebotSearchDesktop
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Host1: blog with multiple patterns
	host1YAML := `
hosts:
  - id: 1
    domain: "blog.example.com"
    render_key: "blog-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $BingbotDesktop
    url_rules:
      - match: "/posts/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $ChatGPTUserBot
      - match: "/api/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $PerplexityBot
`
	err = os.WriteFile(filepath.Join(hostsDir, "01-blog.yaml"), []byte(host1YAML), 0o644)
	require.NoError(t, err)

	// Host2: shop with multiple patterns
	host2YAML := `
hosts:
  - id: 2
    domain: "shop.example.com"
    render_key: "shop-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $GoogleBotAds
    url_rules:
      - match: "/products/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $GooglebotSearchDesktop
      - match: "/checkout/*"
        action: "render"
`
	err = os.WriteFile(filepath.Join(hostsDir, "02-shop.yaml"), []byte(host2YAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify global
	globalConfig := manager.GetConfig()
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 5, "Global should have 5 Googlebot patterns")

	hosts := manager.GetHosts()
	require.Len(t, hosts, 2)

	// Verify Host1 (blog)
	host1 := hosts[0]
	assert.Equal(t, "blog.example.com", host1.Domain)
	require.NotNil(t, host1.BothitRecache)
	assert.Len(t, host1.BothitRecache.MatchUA, 3, "Host1 should have 3 Bingbot patterns")

	require.Len(t, host1.URLRules, 2)

	// Host1 Pattern 1: /posts/* with ChatGPT
	postsRule := host1.URLRules[0]
	require.NotNil(t, postsRule.BothitRecache)
	assert.Len(t, postsRule.BothitRecache.MatchUA, 1, "Posts pattern should have 1 ChatGPT pattern")
	assert.Contains(t, postsRule.BothitRecache.MatchUA[0], "ChatGPT-User")

	// Host1 Pattern 2: /api/* with Perplexity
	apiRule := host1.URLRules[1]
	require.NotNil(t, apiRule.BothitRecache)
	assert.Len(t, apiRule.BothitRecache.MatchUA, 1, "API pattern should have 1 Perplexity pattern")
	assert.Contains(t, apiRule.BothitRecache.MatchUA[0], "PerplexityBot")

	// Verify Host2 (shop)
	host2 := hosts[1]
	assert.Equal(t, "shop.example.com", host2.Domain)
	require.NotNil(t, host2.BothitRecache)
	assert.Len(t, host2.BothitRecache.MatchUA, 1, "Host2 should have 1 GoogleBotAds pattern")

	require.Len(t, host2.URLRules, 2)

	// Host2 Pattern 1: /products/* with GooglebotSearchDesktop
	productsRule := host2.URLRules[0]
	require.NotNil(t, productsRule.BothitRecache)
	assert.Len(t, productsRule.BothitRecache.MatchUA, 5, "Products pattern should have 5 Googlebot patterns")

	// Host2 Pattern 2: /checkout/* inherits from host
	checkoutRule := host2.URLRules[1]
	assert.Nil(t, checkoutRule.BothitRecache, "Checkout pattern should inherit from host")

	// Verify no cross-contamination between hosts
	for _, pattern := range host1.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "GoogleBotAds", "Host1 should not have GoogleBotAds patterns")
	}

	for _, pattern := range host2.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "bingbot", "Host2 should not have Bingbot patterns")
	}

	// Verify no cross-contamination between patterns
	for _, pattern := range postsRule.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "PerplexityBot", "Posts pattern should not have Perplexity")
	}

	for _, pattern := range apiRule.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "ChatGPT", "API pattern should not have ChatGPT")
	}
}

// TestBothitRecache_Integration_PrecedenceVerification tests that pattern-level config takes precedence
func TestBothitRecache_Integration_PrecedenceVerification(t *testing.T) {
	tmpDir := t.TempDir()

	// All three levels with different aliases to test precedence
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - "*GlobalBot*"
    - $BingbotDesktop
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - "*HostBot*"
        - $GooglebotSearchDesktop
    url_rules:
      - match: "/special/*"
        action: "render"
        bothit_recache:
          match_ua:
            - "*PatternBot*"
            - $ChatGPTUserBot
      - match: "/normal/*"
        action: "render"
`
	hostConfigPath := filepath.Join(hostsDir, "01-example.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify precedence at each level
	globalConfig := manager.GetConfig()
	hosts := manager.GetHosts()
	host := hosts[0]

	// Global should have GlobalBot + Bingbot
	assert.Contains(t, globalConfig.BothitRecache.MatchUA, "*GlobalBot*")
	assert.Contains(t, globalConfig.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.NotContains(t, globalConfig.BothitRecache.MatchUA, "*HostBot*")
	assert.NotContains(t, globalConfig.BothitRecache.MatchUA, "*PatternBot*")

	// Host should have HostBot + Googlebot (NOT GlobalBot or Bingbot)
	assert.Contains(t, host.BothitRecache.MatchUA, "*HostBot*")
	assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.NotContains(t, host.BothitRecache.MatchUA, "*GlobalBot*")
	assert.NotContains(t, host.BothitRecache.MatchUA, "*PatternBot*")

	// Pattern /special/* should have PatternBot + ChatGPT (NOT HostBot or GlobalBot)
	specialRule := host.URLRules[0]
	require.NotNil(t, specialRule.BothitRecache)
	assert.Contains(t, specialRule.BothitRecache.MatchUA, "*PatternBot*")
	assert.Contains(t, specialRule.BothitRecache.MatchUA[1], "ChatGPT-User")
	assert.NotContains(t, specialRule.BothitRecache.MatchUA, "*HostBot*")
	assert.NotContains(t, specialRule.BothitRecache.MatchUA, "*GlobalBot*")

	// Pattern /normal/* should inherit from host (at runtime)
	normalRule := host.URLRules[1]
	assert.Nil(t, normalRule.BothitRecache, "Normal pattern should inherit from host")
}

// TestBothitRecache_Integration_AllAliasesAcrossLevels tests using all available bot aliases across different levels
func TestBothitRecache_Integration_AllAliasesAcrossLevels(t *testing.T) {
	tmpDir := t.TempDir()

	// Global with AI bots
	globalYAML := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
hosts:
  include: "hosts.d"
redis:
  addr: "localhost:6379"
storage:
  base_path: "cache/"
render:
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*"]
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $ChatGPTUserBot
    - $ChatGPTTrainingBot
    - $AnthropicBot
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Host with search engine bots
	hostYAML := `
hosts:
  - id: 1
    domain: "docs.example.com"
    render_key: "docs-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua:
        - $GooglebotSearchDesktop
        - $GooglebotSearchMobile
        - $BingbotDesktop
        - $BingbotMobile
    url_rules:
      - match: "/guides/*"
        action: "render"
        bothit_recache:
          match_ua:
            - $PerplexityBot
            - $PerplexityUserBot
            - $OpenAISearchBot
`
	hostConfigPath := filepath.Join(hostsDir, "01-docs.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)

	// Verify global has AI bot patterns
	globalConfig := manager.GetConfig()
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 1+2+1) // ChatGPT(1) + Training(2) + Anthropic(1) = 4

	// Verify host has search engine patterns
	hosts := manager.GetHosts()
	host := hosts[0]
	assert.Len(t, host.BothitRecache.MatchUA, 5+4+3+4) // 5 Google Desktop + 4 Google Mobile + 3 Bing Desktop + 4 Bing Mobile = 16

	// Verify pattern has Perplexity and OpenAI patterns
	guidesRule := host.URLRules[0]
	assert.Len(t, guidesRule.BothitRecache.MatchUA, 1+1+1) // Perplexity(1) + PerplexityUser(1) + OpenAI(1) = 3

	// Verify specific patterns exist (PerplexityBot is a regex pattern)
	assert.Contains(t, guidesRule.BothitRecache.MatchUA[0], "PerplexityBot", "Should contain PerplexityBot regex pattern")
	assert.Contains(t, guidesRule.BothitRecache.MatchUA[1], "Perplexity-User", "Should contain PerplexityUser exact pattern")
	assert.Contains(t, guidesRule.BothitRecache.MatchUA[2], "OAI-SearchBot", "Should contain OpenAI SearchBot pattern")
}

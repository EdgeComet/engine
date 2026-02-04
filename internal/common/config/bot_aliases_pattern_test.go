package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBothitRecache_PatternLevel_BasicExpansion tests pattern-level bot alias expansion
func TestBothitRecache_PatternLevel_BasicExpansion(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create global config
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with URL rule that has bothit_recache with alias
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/blog/*"
        action: "render"
        bothit_recache:
          match_ua: ["$GooglebotSearchDesktop"]
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify pattern-level bothit_recache was expanded
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	require.Len(t, host.URLRules, 1)

	rule := host.URLRules[0]
	require.NotNil(t, rule.BothitRecache)

	// Verify aliases were expanded (should have 5 patterns for GooglebotSearchDesktop)
	assert.Len(t, rule.BothitRecache.MatchUA, 5)
	assert.Contains(t, rule.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, rule.BothitRecache.MatchUA, "Googlebot/2.1 (+http://www.google.com/bot.html)")

	// Verify patterns were compiled
	assert.NotNil(t, rule.BothitRecache.CompiledPatterns)
	assert.Len(t, rule.BothitRecache.CompiledPatterns, 5)
}

// TestBothitRecache_PatternLevel_MultipleRules tests multiple URL rules with different aliases
func TestBothitRecache_PatternLevel_MultipleRules(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create global config
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with multiple URL rules with different aliases
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/blog/*"
        action: "render"
        bothit_recache:
          match_ua: ["$GooglebotSearchDesktop"]
      - match: "/api/*"
        action: "render"
        bothit_recache:
          match_ua: ["$BingbotDesktop"]
      - match: "/news/*"
        action: "render"
        bothit_recache:
          match_ua: ["*CustomBot*", "$AnthropicBot"]
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify all pattern-level bothit_recache were expanded independently
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	require.Len(t, host.URLRules, 3)

	// Verify first rule (/blog/*) has GooglebotSearchDesktop (5 patterns)
	rule1 := host.URLRules[0]
	require.NotNil(t, rule1.BothitRecache)
	assert.Len(t, rule1.BothitRecache.MatchUA, 5)
	assert.Contains(t, rule1.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")

	// Verify second rule (/api/*) has BingbotDesktop (3 patterns)
	rule2 := host.URLRules[1]
	require.NotNil(t, rule2.BothitRecache)
	assert.Len(t, rule2.BothitRecache.MatchUA, 3)
	assert.Contains(t, rule2.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

	// Verify third rule (/news/*) has mixed custom and alias (2 patterns: custom + AnthropicBot)
	rule3 := host.URLRules[2]
	require.NotNil(t, rule3.BothitRecache)
	assert.Len(t, rule3.BothitRecache.MatchUA, 2)
	assert.Equal(t, "*CustomBot*", rule3.BothitRecache.MatchUA[0])
	assert.Contains(t, rule3.BothitRecache.MatchUA[1], "ClaudeBot")

	// Verify no cross-contamination
	assert.NotContains(t, rule1.BothitRecache.MatchUA, "bingbot")
	assert.NotContains(t, rule2.BothitRecache.MatchUA, "Googlebot")
}

// TestBothitRecache_PatternLevel_UnknownAlias tests error handling for unknown aliases in patterns
func TestBothitRecache_PatternLevel_UnknownAlias(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create global config
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with URL rule that has invalid alias
	hostYAML := `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/products/*"
        action: "render"
        bothit_recache:
          match_ua: ["$UnknownBot"]
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config - should fail
	logger := zap.NewNop()
	_, err = NewEGConfigManager(globalConfigPath, logger)

	// Verify error includes pattern context
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url_rule[0]", "Error should include URL rule index")
	assert.Contains(t, err.Error(), "shop.example.com", "Error should include host domain")
	assert.Contains(t, err.Error(), "$UnknownBot", "Error should include unknown alias")
	assert.Contains(t, err.Error(), "Available aliases:", "Error should list available aliases")
}

// TestBothitRecache_PatternLevel_NoExpansionWhenEmpty tests that empty bothit_recache is handled correctly
func TestBothitRecache_PatternLevel_NoExpansionWhenEmpty(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create global config
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with URL rules - some with bothit_recache, some without
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/with-alias/*"
        action: "render"
        bothit_recache:
          match_ua: ["$GooglebotSearchDesktop"]
      - match: "/no-bothit/*"
        action: "render"
      - match: "/empty-bothit/*"
        action: "render"
        bothit_recache: {}
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify expansion only happened where bothit_recache had match_ua
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	require.Len(t, host.URLRules, 3)

	// Rule 1: Has alias, should be expanded
	rule1 := host.URLRules[0]
	require.NotNil(t, rule1.BothitRecache)
	assert.Len(t, rule1.BothitRecache.MatchUA, 5)

	// Rule 2: No bothit_recache (inherits from host/global at runtime)
	rule2 := host.URLRules[1]
	assert.Nil(t, rule2.BothitRecache)

	// Rule 3: Empty bothit_recache
	rule3 := host.URLRules[2]
	require.NotNil(t, rule3.BothitRecache)
	assert.Len(t, rule3.BothitRecache.MatchUA, 0)
}

// TestBothitRecache_PatternLevel_OverridesHostAndGlobal tests that pattern-level bothit_recache overrides host and global
func TestBothitRecache_PatternLevel_OverridesHostAndGlobal(t *testing.T) {
	tmpDir := t.TempDir()

	// Create global config with BingbotDesktop alias
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
  match_ua: ["$BingbotDesktop"]
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with GooglebotSearchDesktop at host level
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
      match_ua: ["$GooglebotSearchDesktop"]
    url_rules:
      - match: "/special/*"
        action: "render"
        bothit_recache:
          match_ua: ["$ChatGPTUserBot"]
      - match: "/normal/*"
        action: "render"
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify global has BingbotDesktop (3 patterns)
	globalConfig := manager.GetConfig()
	require.NotNil(t, globalConfig.BothitRecache)
	assert.Len(t, globalConfig.BothitRecache.MatchUA, 3)
	assert.Contains(t, globalConfig.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.NotContains(t, globalConfig.BothitRecache.MatchUA, "Googlebot")
	assert.NotContains(t, globalConfig.BothitRecache.MatchUA, "ChatGPT")

	// Verify host has GooglebotSearchDesktop (5 patterns)
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)
	host := hosts[0]
	require.NotNil(t, host.BothitRecache)
	assert.Len(t, host.BothitRecache.MatchUA, 5)
	assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.NotContains(t, host.BothitRecache.MatchUA, "bingbot")
	assert.NotContains(t, host.BothitRecache.MatchUA, "ChatGPT")

	// Verify pattern "/special/*" has ChatGPTUserBot only (1 pattern)
	require.Len(t, host.URLRules, 2)
	specialRule := host.URLRules[0]
	require.NotNil(t, specialRule.BothitRecache)
	assert.Len(t, specialRule.BothitRecache.MatchUA, 1)
	assert.Contains(t, specialRule.BothitRecache.MatchUA[0], "ChatGPT-User")
	assert.NotContains(t, specialRule.BothitRecache.MatchUA[0], "Googlebot")
	assert.NotContains(t, specialRule.BothitRecache.MatchUA[0], "bingbot")

	// Verify pattern "/normal/*" has no bothit_recache (inherits at runtime)
	normalRule := host.URLRules[1]
	assert.Nil(t, normalRule.BothitRecache)
}

// TestBothitRecache_PatternLevel_InheritsFromHost tests that pattern inherits host bothit_recache when not defined
func TestBothitRecache_PatternLevel_InheritsFromHost(t *testing.T) {
	tmpDir := t.TempDir()

	// Create global config without bothit_recache
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with bothit_recache at host level
	// Pattern does NOT define its own bothit_recache
	hostYAML := `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua: ["$GooglebotSearchDesktop", "*ShopBot*"]
    url_rules:
      - match: "/products/*"
        action: "render"
      - match: "/checkout/*"
        action: "render"
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify host has expanded bothit_recache
	hosts := manager.GetHosts()
	require.Len(t, hosts, 1)
	host := hosts[0]
	require.NotNil(t, host.BothitRecache)
	assert.Len(t, host.BothitRecache.MatchUA, 6) // 5 Googlebot + 1 custom
	assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Contains(t, host.BothitRecache.MatchUA, "*ShopBot*")

	// Verify patterns do NOT have their own bothit_recache (inheritance happens at runtime)
	require.Len(t, host.URLRules, 2)

	productsRule := host.URLRules[0]
	assert.Nil(t, productsRule.BothitRecache, "Pattern should not have bothit_recache - inherits from host at runtime")

	checkoutRule := host.URLRules[1]
	assert.Nil(t, checkoutRule.BothitRecache, "Pattern should not have bothit_recache - inherits from host at runtime")
}

// TestBothitRecache_PatternLevel_MultipleHostsWithPatterns tests multiple hosts with pattern-level aliases
func TestBothitRecache_PatternLevel_MultipleHostsWithPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create global config
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
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create first host with GooglebotSearchDesktop in pattern
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
        bothit_recache:
          match_ua: ["$GooglebotSearchDesktop"]
`
	host1ConfigPath := filepath.Join(hostsDir, "01-blog.yaml")
	err = os.WriteFile(host1ConfigPath, []byte(host1YAML), 0644)
	require.NoError(t, err)

	// Create second host with BingbotDesktop in pattern
	host2YAML := `
hosts:
  - id: 2
    domain: "shop.example.com"
    render_key: "shop-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/products/*"
        action: "render"
        bothit_recache:
          match_ua: ["$BingbotDesktop"]
`
	host2ConfigPath := filepath.Join(hostsDir, "02-shop.yaml")
	err = os.WriteFile(host2ConfigPath, []byte(host2YAML), 0644)
	require.NoError(t, err)

	// Create third host with ChatGPTUserBot and AnthropicBot in pattern
	host3YAML := `
hosts:
  - id: 3
    domain: "docs.example.com"
    render_key: "docs-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/guides/*"
        action: "render"
        bothit_recache:
          match_ua: ["$ChatGPTUserBot", "$AnthropicBot"]
`
	host3ConfigPath := filepath.Join(hostsDir, "03-docs.yaml")
	err = os.WriteFile(host3ConfigPath, []byte(host3YAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify all hosts loaded
	hosts := manager.GetHosts()
	require.Len(t, hosts, 3)

	// Verify Host 1 (blog.example.com) has GooglebotSearchDesktop
	host1 := hosts[0]
	assert.Equal(t, "blog.example.com", host1.Domain)
	require.Len(t, host1.URLRules, 1)
	blog1Rule := host1.URLRules[0]
	require.NotNil(t, blog1Rule.BothitRecache)
	assert.Len(t, blog1Rule.BothitRecache.MatchUA, 5)
	assert.Contains(t, blog1Rule.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.NotContains(t, blog1Rule.BothitRecache.MatchUA[0], "bingbot")
	assert.NotContains(t, blog1Rule.BothitRecache.MatchUA[0], "ChatGPT")

	// Verify Host 2 (shop.example.com) has BingbotDesktop
	host2 := hosts[1]
	assert.Equal(t, "shop.example.com", host2.Domain)
	require.Len(t, host2.URLRules, 1)
	shop2Rule := host2.URLRules[0]
	require.NotNil(t, shop2Rule.BothitRecache)
	assert.Len(t, shop2Rule.BothitRecache.MatchUA, 3)
	assert.Contains(t, shop2Rule.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
	assert.NotContains(t, shop2Rule.BothitRecache.MatchUA[0], "Googlebot")
	assert.NotContains(t, shop2Rule.BothitRecache.MatchUA[0], "ChatGPT")

	// Verify Host 3 (docs.example.com) has ChatGPTUserBot and AnthropicBot
	host3 := hosts[2]
	assert.Equal(t, "docs.example.com", host3.Domain)
	require.Len(t, host3.URLRules, 1)
	docs3Rule := host3.URLRules[0]
	require.NotNil(t, docs3Rule.BothitRecache)
	assert.Len(t, docs3Rule.BothitRecache.MatchUA, 2) // 1 ChatGPT + 1 Anthropic

	// Verify ChatGPT pattern
	hasChatGPT := false
	hasAnthropic := false
	for _, pattern := range docs3Rule.BothitRecache.MatchUA {
		if contains(pattern, "ChatGPT-User") {
			hasChatGPT = true
		}
		if contains(pattern, "ClaudeBot") {
			hasAnthropic = true
		}
	}
	assert.True(t, hasChatGPT, "Should have ChatGPT pattern")
	assert.True(t, hasAnthropic, "Should have Anthropic pattern")

	// Verify no cross-contamination
	for _, pattern := range docs3Rule.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "Googlebot")
		assert.NotContains(t, pattern, "bingbot")
	}

	// Verify no cross-contamination between hosts
	// Host 1 should only have Googlebot
	for _, pattern := range blog1Rule.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "bingbot")
		assert.NotContains(t, pattern, "ChatGPT")
		assert.NotContains(t, pattern, "ClaudeBot")
	}

	// Host 2 should only have Bingbot
	for _, pattern := range shop2Rule.BothitRecache.MatchUA {
		assert.NotContains(t, pattern, "Googlebot")
		assert.NotContains(t, pattern, "ChatGPT")
		assert.NotContains(t, pattern, "ClaudeBot")
	}
}

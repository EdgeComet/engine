package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestBotAliasIntegration_FullConfigLoad(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bot-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err, "Config loading should succeed")
	require.NotNil(t, cm, "Config manager should not be nil")

	config := cm.GetConfig()
	require.NotNil(t, config, "Config should not be nil")

	t.Run("global dimensions expanded", func(t *testing.T) {
		require.NotNil(t, config.Render.Dimensions, "Global dimensions should exist")

		desktop, exists := config.Render.Dimensions["desktop"]
		require.True(t, exists, "Desktop dimension should exist")

		assert.Greater(t, len(desktop.MatchUA), 3, "Desktop should have more than 3 patterns (2 aliases + 1 custom)")
		assert.NotContains(t, desktop.MatchUA, "$GooglebotSearchDesktop", "Alias should be expanded, not present")
		assert.NotContains(t, desktop.MatchUA, "$BingbotDesktop", "Alias should be expanded, not present")

		assert.Contains(t, desktop.MatchUA, "*DesktopCustomBot*", "Custom pattern should be preserved")
		assert.Contains(t, desktop.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			"Googlebot pattern should be expanded")
		assert.Contains(t, desktop.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			"Bingbot pattern should be expanded")

		assert.NotNil(t, desktop.CompiledPatterns, "Patterns should be compiled")
		assert.Greater(t, len(desktop.CompiledPatterns), 0, "Compiled patterns should exist")
	})

	hosts := cm.GetHosts()
	require.Len(t, hosts, 1, "Should have exactly one host")

	host := hosts[0]
	assert.Equal(t, "test.example.com", host.Domain)

	t.Run("host dimensions expanded", func(t *testing.T) {
		require.NotNil(t, host.Render.Dimensions, "Host dimensions should exist")

		mobile, exists := host.Render.Dimensions["mobile"]
		require.True(t, exists, "Mobile dimension should exist")

		assert.Greater(t, len(mobile.MatchUA), 3, "Mobile should have multiple expanded patterns")
		assert.NotContains(t, mobile.MatchUA, "$GooglebotSearchMobile", "Mobile alias should be expanded")
		assert.NotContains(t, mobile.MatchUA, "$BingbotMobile", "Mobile alias should be expanded")

		assert.Contains(t, mobile.MatchUA, "*CustomBot*", "Custom pattern should be preserved in mobile")

		expectedGooglebotMobile := "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
		assert.Contains(t, mobile.MatchUA, expectedGooglebotMobile, "Googlebot mobile pattern should be present")

		assert.NotNil(t, mobile.CompiledPatterns, "Mobile patterns should be compiled")
		assert.Greater(t, len(mobile.CompiledPatterns), 0, "Mobile compiled patterns should exist")

		tablet, exists := host.Render.Dimensions["tablet"]
		require.True(t, exists, "Tablet dimension should exist")

		assert.Greater(t, len(tablet.MatchUA), 1, "Tablet should have expanded patterns")
		assert.NotContains(t, tablet.MatchUA, "$AnthropicBot", "Anthropic alias should be expanded")
		assert.NotContains(t, tablet.MatchUA, "$ChatGPTUserBot", "ChatGPT alias should be expanded")

		assert.Contains(t, tablet.MatchUA, "*ClaudeBot/1.0; +claudebot@anthropic.com*",
			"AnthropicBot should expand to ClaudeBot pattern")
		assert.Contains(t, tablet.MatchUA, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
			"ChatGPTUserBot should expand correctly")

		assert.NotNil(t, tablet.CompiledPatterns, "Tablet patterns should be compiled")
	})

	t.Run("pattern counts validate expansion", func(t *testing.T) {
		desktop := config.Render.Dimensions["desktop"]
		mobile := host.Render.Dimensions["mobile"]
		tablet := host.Render.Dimensions["tablet"]

		assert.Equal(t, 5+3+1, len(desktop.MatchUA),
			"Desktop should have 5 Googlebot + 3 Bingbot + 1 custom = 9 patterns")

		assert.Equal(t, 4+4+1, len(mobile.MatchUA),
			"Mobile should have 4 Googlebot + 4 Bingbot + 1 custom = 9 patterns")

		assert.Equal(t, 1+1, len(tablet.MatchUA),
			"Tablet should have 1 AnthropicBot + 1 ChatGPTUserBot = 2 patterns")
	})

	t.Run("order preserved during expansion", func(t *testing.T) {
		desktop := config.Render.Dimensions["desktop"]

		assert.Contains(t, desktop.MatchUA[len(desktop.MatchUA)-1], "*DesktopCustomBot*",
			"Custom pattern should remain at the end")
	})
}

func TestBotAliasIntegration_InvalidAlias(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bot-aliases-invalid", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)

	require.Error(t, err, "Config loading should fail with unknown alias")
	assert.Nil(t, cm, "Config manager should be nil on error")

	assert.Contains(t, err.Error(), "failed to expand bot aliases", "Error should mention alias expansion failure")
	assert.Contains(t, err.Error(), "$UnknownBotAlias", "Error should mention the unknown alias")
	assert.Contains(t, err.Error(), "Available aliases:", "Error should list available aliases")
	assert.Contains(t, err.Error(), "$AnthropicBot", "Error should include valid alias examples")
}

func TestBotAliasIntegration_AliasExpansionVsInheritance(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bot-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]

	t.Run("host does not inherit global dimensions", func(t *testing.T) {
		_, hasDesktop := host.Render.Dimensions["desktop"]
		assert.False(t, hasDesktop, "Host should not inherit desktop dimension (has own dimensions)")

		_, hasMobile := host.Render.Dimensions["mobile"]
		assert.True(t, hasMobile, "Host should have its own mobile dimension")

		_, hasTablet := host.Render.Dimensions["tablet"]
		assert.True(t, hasTablet, "Host should have its own tablet dimension")
	})

	t.Run("global dimensions only in global config", func(t *testing.T) {
		globalConfig := cm.GetConfig()

		globalDesktop, exists := globalConfig.Render.Dimensions["desktop"]
		require.True(t, exists, "Global desktop dimension should exist")

		assert.Contains(t, globalDesktop.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			"Global desktop should have expanded Googlebot patterns")
	})
}

func TestBotAliasIntegration_CompiledPatternsWork(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bot-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	config := cm.GetConfig()
	desktop := config.Render.Dimensions["desktop"]

	t.Run("all patterns are compiled", func(t *testing.T) {
		assert.Greater(t, len(desktop.CompiledPatterns), 0, "Should have compiled patterns")
		assert.Equal(t, len(desktop.MatchUA), len(desktop.CompiledPatterns),
			"All MatchUA patterns should be compiled")

		for i, cp := range desktop.CompiledPatterns {
			assert.NotEmpty(t, cp.Original, "Pattern %d should have original string", i)
		}
	})

	hosts := cm.GetHosts()
	mobile := hosts[0].Render.Dimensions["mobile"]
	tablet := hosts[0].Render.Dimensions["tablet"]

	t.Run("host dimensions are compiled", func(t *testing.T) {
		assert.Equal(t, len(mobile.MatchUA), len(mobile.CompiledPatterns),
			"All mobile patterns should be compiled")
		assert.Equal(t, len(tablet.MatchUA), len(tablet.CompiledPatterns),
			"All tablet patterns should be compiled")
	})
}

func TestBotAliasIntegration_MixedPatternsPreserved(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bot-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	hosts := cm.GetHosts()
	mobile := hosts[0].Render.Dimensions["mobile"]

	t.Run("aliases expand and custom patterns preserve", func(t *testing.T) {
		aliasPatternCount := 0
		customPatternCount := 0

		for _, pattern := range mobile.MatchUA {
			if pattern == "*CustomBot*" {
				customPatternCount++
			} else {
				aliasPatternCount++
			}
		}

		assert.Equal(t, 1, customPatternCount, "Should have exactly 1 custom pattern")
		assert.Equal(t, 8, aliasPatternCount, "Should have 8 expanded alias patterns (4 Googlebot + 4 Bingbot)")
	})

	t.Run("order maintains aliases then custom", func(t *testing.T) {
		lastPattern := mobile.MatchUA[len(mobile.MatchUA)-1]
		assert.Equal(t, "*CustomBot*", lastPattern, "Custom pattern should be last (preserving order)")
	})
}

func TestBothitRecache_GlobalAlias_SingleValid(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err, "Config loading should succeed with valid bot aliases")
	require.NotNil(t, cm, "Config manager should not be nil")

	config := cm.GetConfig()
	require.NotNil(t, config, "Config should not be nil")
	require.NotNil(t, config.BothitRecache, "BothitRecache should be configured")

	t.Run("aliases are expanded not preserved", func(t *testing.T) {
		assert.NotContains(t, config.BothitRecache.MatchUA, "$GooglebotSearchDesktop",
			"Alias should be expanded, not preserved")
		assert.NotContains(t, config.BothitRecache.MatchUA, "$BingbotDesktop",
			"Alias should be expanded, not preserved")
	})

	t.Run("expanded patterns are present", func(t *testing.T) {
		assert.Contains(t, config.BothitRecache.MatchUA,
			"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			"Googlebot pattern should be expanded")
		assert.Contains(t, config.BothitRecache.MatchUA,
			"Googlebot/2.1 (+http://www.google.com/bot.html)",
			"Googlebot pattern variant should be expanded")
		assert.Contains(t, config.BothitRecache.MatchUA,
			"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			"Bingbot pattern should be expanded")
	})

	t.Run("custom pattern is preserved", func(t *testing.T) {
		assert.Contains(t, config.BothitRecache.MatchUA, "*CustomRecacheBot*",
			"Custom pattern should be preserved unchanged")
	})

	t.Run("pattern count is correct", func(t *testing.T) {
		expectedCount := 5 + 3 + 1
		assert.Equal(t, expectedCount, len(config.BothitRecache.MatchUA),
			"Should have 5 Googlebot + 3 Bingbot + 1 custom = 9 patterns")
	})

	t.Run("patterns are compiled", func(t *testing.T) {
		assert.NotNil(t, config.BothitRecache.CompiledPatterns,
			"Patterns should be compiled")
		assert.Equal(t, len(config.BothitRecache.MatchUA), len(config.BothitRecache.CompiledPatterns),
			"All patterns should be compiled")
	})
}

func TestBothitRecache_GlobalAlias_Multiple(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	config := cm.GetConfig()
	require.NotNil(t, config.BothitRecache)

	t.Run("multiple aliases expand correctly", func(t *testing.T) {
		googlebotCount := 0
		bingbotCount := 0
		customCount := 0

		for _, pattern := range config.BothitRecache.MatchUA {
			if pattern == "*CustomRecacheBot*" {
				customCount++
			} else if contains(pattern, "Googlebot") || contains(pattern, "googlebot") {
				googlebotCount++
			} else if contains(pattern, "bingbot") {
				bingbotCount++
			}
		}

		assert.Equal(t, 5, googlebotCount, "Should have 5 Googlebot patterns")
		assert.Equal(t, 3, bingbotCount, "Should have 3 Bingbot patterns")
		assert.Equal(t, 1, customCount, "Should have 1 custom pattern")
	})

	t.Run("total pattern count validates all expansions", func(t *testing.T) {
		assert.Equal(t, 9, len(config.BothitRecache.MatchUA),
			"Total should be 5 + 3 + 1 = 9 patterns")
	})
}

func TestBothitRecache_GlobalAlias_MixedPatterns(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-aliases-test", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	config := cm.GetConfig()

	t.Run("aliases expand while custom patterns preserve", func(t *testing.T) {
		hasExpandedPattern := false
		hasCustomPattern := false

		for _, pattern := range config.BothitRecache.MatchUA {
			if pattern == "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" {
				hasExpandedPattern = true
			}
			if pattern == "*CustomRecacheBot*" {
				hasCustomPattern = true
			}
		}

		assert.True(t, hasExpandedPattern, "Should have expanded Googlebot pattern")
		assert.True(t, hasCustomPattern, "Should preserve custom pattern")
	})

	t.Run("order is preserved during expansion", func(t *testing.T) {
		lastPattern := config.BothitRecache.MatchUA[len(config.BothitRecache.MatchUA)-1]
		assert.Equal(t, "*CustomRecacheBot*", lastPattern,
			"Custom pattern should remain last (order preserved)")
	})
}

func TestBothitRecache_GlobalAlias_UnknownFails(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-invalid-alias", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)

	t.Run("config loading fails", func(t *testing.T) {
		require.Error(t, err, "Config loading should fail with unknown alias")
		assert.Nil(t, cm, "Config manager should be nil on error")
	})

	t.Run("error message is descriptive", func(t *testing.T) {
		assert.Contains(t, err.Error(), "failed to expand bot aliases",
			"Error should mention alias expansion failure")
		assert.Contains(t, err.Error(), "global bothit_recache",
			"Error should mention global bothit_recache context")
		assert.Contains(t, err.Error(), "unknown bot alias",
			"Error should explicitly say unknown bot alias")
		assert.Contains(t, err.Error(), "$UnknownBotAlias",
			"Error should mention the specific unknown alias")
	})

	t.Run("error provides helpful hints", func(t *testing.T) {
		assert.Contains(t, err.Error(), "Available aliases:",
			"Error should list available aliases")
		assert.Contains(t, err.Error(), "$AnthropicBot",
			"Error should include valid alias examples")
	})
}

func TestBothitRecache_GlobalAlias_CaseSensitive(t *testing.T) {
	configYAML := `server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
redis:
  addr: "localhost:6379"
storage:
  base_path: "/tmp/cache"
render:
  cache:
    ttl: 1h
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
      match_ua: ["*Bot*"]
  events:
    wait_for: "networkIdle"
bothit_recache:
  enabled: true
  interval: 30m
  match_ua:
    - $googlebotSearchDesktop
bypass:
  timeout: 20s
registry:
  selection_strategy: "least_loaded"
log:
  level: "info"
metrics:
  enabled: false
hosts:
  include: "hosts.d/"`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o644))

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))
	hostsYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key"
    render:
      timeout: 60s
      dimensions:
        mobile:
          id: 2
          width: 375
          height: 667
          render_ua: "Mozilla/5.0"
          match_ua: ["*Bot*"]`
	require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "test.yaml"), []byte(hostsYAML), 0o644))

	logger := zaptest.NewLogger(t)
	cm, err := NewEGConfigManager(configPath, logger)

	t.Run("wrong case causes error", func(t *testing.T) {
		require.Error(t, err, "Should fail with wrong case alias")
		assert.Nil(t, cm, "Config manager should be nil")
	})

	t.Run("error is about unknown alias", func(t *testing.T) {
		assert.Contains(t, err.Error(), "unknown bot alias",
			"Error should be about unknown alias, not silent failure")
		assert.Contains(t, err.Error(), "$googlebotSearchDesktop",
			"Error should mention the wrong-case alias")
	})
}

func TestBothitRecache_GlobalAlias_EmptyMatchUA(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-empty-matchua", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)

	t.Run("empty match_ua loads successfully", func(t *testing.T) {
		require.NoError(t, err, "Config should load with empty match_ua")
		require.NotNil(t, cm, "Config manager should not be nil")
	})

	config := cm.GetConfig()
	require.NotNil(t, config)

	t.Run("match_ua remains empty", func(t *testing.T) {
		require.NotNil(t, config.BothitRecache, "BothitRecache should exist")
		assert.Empty(t, config.BothitRecache.MatchUA, "MatchUA should be empty")
		assert.NotNil(t, config.BothitRecache.MatchUA, "MatchUA should not be nil")
	})

	t.Run("compiled patterns is empty", func(t *testing.T) {
		assert.Empty(t, config.BothitRecache.CompiledPatterns,
			"CompiledPatterns should be empty for empty MatchUA")
	})
}

func TestBothitRecache_HostAlias_SingleValid(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-host-aliases", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err, "Config loading should succeed with valid bot aliases")
	require.NotNil(t, cm, "Config manager should not be nil")

	hosts := cm.GetHosts()
	require.Len(t, hosts, 1, "Should have exactly one host")

	host := hosts[0]
	require.Equal(t, "shop.example.com", host.Domain)
	require.NotNil(t, host.BothitRecache, "Host BothitRecache should be configured")

	t.Run("aliases are expanded not preserved", func(t *testing.T) {
		assert.NotContains(t, host.BothitRecache.MatchUA, "$GooglebotSearchMobile",
			"Alias should be expanded, not preserved")
		assert.NotContains(t, host.BothitRecache.MatchUA, "$BingbotMobile",
			"Alias should be expanded, not preserved")
	})

	t.Run("expanded patterns are present", func(t *testing.T) {
		expectedGooglebotMobile := "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
		assert.Contains(t, host.BothitRecache.MatchUA, expectedGooglebotMobile,
			"Googlebot mobile pattern should be expanded")

		expectedBingbotMobile := "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"
		assert.Contains(t, host.BothitRecache.MatchUA, expectedBingbotMobile,
			"Bingbot mobile pattern should be expanded")
	})

	t.Run("custom pattern is preserved", func(t *testing.T) {
		assert.Contains(t, host.BothitRecache.MatchUA, "*ShopBot*",
			"Custom pattern should be preserved unchanged")
	})

	t.Run("pattern count is correct", func(t *testing.T) {
		expectedCount := 4 + 4 + 1
		assert.Equal(t, expectedCount, len(host.BothitRecache.MatchUA),
			"Should have 4 Googlebot + 4 Bingbot + 1 custom = 9 patterns")
	})

	t.Run("patterns are compiled", func(t *testing.T) {
		assert.NotNil(t, host.BothitRecache.CompiledPatterns,
			"Patterns should be compiled")
		assert.Equal(t, len(host.BothitRecache.MatchUA), len(host.BothitRecache.CompiledPatterns),
			"All patterns should be compiled")
	})
}

func TestBothitRecache_HostAlias_Multiple(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-host-aliases", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	hosts := cm.GetHosts()
	host := hosts[0]
	require.NotNil(t, host.BothitRecache)

	t.Run("multiple aliases expand correctly", func(t *testing.T) {
		googlebotCount := 0
		bingbotCount := 0
		customCount := 0

		for _, pattern := range host.BothitRecache.MatchUA {
			if pattern == "*ShopBot*" {
				customCount++
			} else if contains(pattern, "Googlebot") || contains(pattern, "googlebot") {
				googlebotCount++
			} else if contains(pattern, "bingbot") {
				bingbotCount++
			}
		}

		assert.Equal(t, 4, googlebotCount, "Should have 4 Googlebot patterns")
		assert.Equal(t, 4, bingbotCount, "Should have 4 Bingbot patterns")
		assert.Equal(t, 1, customCount, "Should have 1 custom pattern")
	})

	t.Run("total pattern count validates all expansions", func(t *testing.T) {
		assert.Equal(t, 9, len(host.BothitRecache.MatchUA),
			"Total should be 4 + 4 + 1 = 9 patterns")
	})
}

func TestBothitRecache_HostAlias_MixedPatterns(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-host-aliases", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)
	require.NoError(t, err)

	hosts := cm.GetHosts()
	host := hosts[0]

	t.Run("aliases expand while custom patterns preserve", func(t *testing.T) {
		hasExpandedPattern := false
		hasCustomPattern := false

		expectedGooglebotMobile := "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2272.96 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"

		for _, pattern := range host.BothitRecache.MatchUA {
			if pattern == expectedGooglebotMobile {
				hasExpandedPattern = true
			}
			if pattern == "*ShopBot*" {
				hasCustomPattern = true
			}
		}

		assert.True(t, hasExpandedPattern, "Should have expanded Googlebot pattern")
		assert.True(t, hasCustomPattern, "Should preserve custom pattern")
	})

	t.Run("order is preserved during expansion", func(t *testing.T) {
		lastPattern := host.BothitRecache.MatchUA[len(host.BothitRecache.MatchUA)-1]
		assert.Equal(t, "*ShopBot*", lastPattern,
			"Custom pattern should remain last (order preserved)")
	})
}

func TestBothitRecache_HostAlias_UnknownFails(t *testing.T) {
	logger := zaptest.NewLogger(t)
	configPath := filepath.Join("..", "..", "..", "tests", "integration", "fixtures", "bothit-recache-host-invalid", "edge-gateway.yaml")

	cm, err := NewEGConfigManager(configPath, logger)

	t.Run("config loading fails", func(t *testing.T) {
		require.Error(t, err, "Config loading should fail with unknown alias")
		assert.Nil(t, cm, "Config manager should be nil on error")
	})

	t.Run("error message is descriptive", func(t *testing.T) {
		assert.Contains(t, err.Error(), "failed to expand bothit_recache aliases",
			"Error should mention alias expansion failure")
		assert.Contains(t, err.Error(), "host 'invalid.example.com'",
			"Error should mention host domain")
		assert.Contains(t, err.Error(), "hosts.d/invalid.yaml",
			"Error should mention config file path")
		assert.Contains(t, err.Error(), "unknown bot alias",
			"Error should explicitly say unknown bot alias")
		assert.Contains(t, err.Error(), "$InvalidBotAlias",
			"Error should mention the specific unknown alias")
	})

	t.Run("error provides helpful hints", func(t *testing.T) {
		assert.Contains(t, err.Error(), "Available aliases:",
			"Error should list available aliases")
		assert.Contains(t, err.Error(), "$AnthropicBot",
			"Error should include valid alias examples")
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

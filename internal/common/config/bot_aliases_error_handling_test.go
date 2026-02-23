package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBothitRecache_ErrorHandling_MultipleUnknownAliases tests that all unknown aliases are reported
func TestBothitRecache_ErrorHandling_MultipleUnknownAliases(t *testing.T) {
	t.Run("global level with multiple unknown", func(t *testing.T) {
		tmpDir := t.TempDir()

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
  match_ua: ["$UnknownBot1", "$GooglebotSearchDesktop", "$UnknownBot2", "$InvalidAlias"]
`
		globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

		// Create a minimal valid host file to pass validation
		hostYAML := `
hosts:
  - id: 1
    domain: "test.com"
    render_key: "key"
    render:
      timeout: 30s
`
		err = os.WriteFile(filepath.Join(hostsDir, "test.yaml"), []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err, "Config loading should fail with multiple unknown aliases")

		errMsg := err.Error()
		assert.Contains(t, errMsg, "$UnknownBot1", "Error should mention first unknown alias")
		assert.Contains(t, errMsg, "$UnknownBot2", "Error should mention second unknown alias")
		assert.Contains(t, errMsg, "$InvalidAlias", "Error should mention third unknown alias")
		assert.Contains(t, errMsg, "unknown bot aliases", "Error should use plural 'aliases'")
		assert.Contains(t, errMsg, "Available aliases:", "Error should list available aliases")
	})

	t.Run("host level with multiple unknown", func(t *testing.T) {
		tmpDir := t.TempDir()

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
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

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
      match_ua: ["$FakeBot", "$NonExistentBot"]
`
		hostConfigPath := filepath.Join(hostsDir, "01-shop.yaml")
		err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err, "Config loading should fail")

		errMsg := err.Error()
		assert.Contains(t, errMsg, "$FakeBot", "Error should mention first unknown alias")
		assert.Contains(t, errMsg, "$NonExistentBot", "Error should mention second unknown alias")
		assert.Contains(t, errMsg, "shop.example.com", "Error should mention host domain")
		assert.Contains(t, errMsg, "unknown bot aliases", "Error should use plural")
	})

	t.Run("pattern level with multiple unknown", func(t *testing.T) {
		tmpDir := t.TempDir()

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
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

		hostYAML := `
hosts:
  - id: 1
    domain: "blog.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/articles/*"
        action: "render"
        bothit_recache:
          match_ua: ["$WrongBot1", "$WrongBot2", "$WrongBot3"]
`
		hostConfigPath := filepath.Join(hostsDir, "01-blog.yaml")
		err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err, "Config loading should fail")

		errMsg := err.Error()
		assert.Contains(t, errMsg, "$WrongBot1", "Error should mention all unknown aliases")
		assert.Contains(t, errMsg, "$WrongBot2", "Error should mention all unknown aliases")
		assert.Contains(t, errMsg, "$WrongBot3", "Error should mention all unknown aliases")
		assert.Contains(t, errMsg, "url_rule[0]", "Error should mention URL rule index")
		assert.Contains(t, errMsg, "blog.example.com", "Error should mention host")
	})
}

// TestBothitRecache_ErrorHandling_IncludesContext tests that error messages include helpful context
func TestBothitRecache_ErrorHandling_IncludesContext(t *testing.T) {
	t.Run("global config context", func(t *testing.T) {
		tmpDir := t.TempDir()

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
  match_ua: ["$GlobalUnknown"]
`
		globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

		// Create a minimal valid host file
		hostYAML := `
hosts:
  - id: 1
    domain: "test.com"
    render_key: "key"
    render:
      timeout: 30s
`
		err = os.WriteFile(filepath.Join(hostsDir, "test.yaml"), []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err)

		errMsg := err.Error()
		assert.Contains(t, errMsg, "global config", "Error should mention global config context")
		assert.Contains(t, errMsg, "$GlobalUnknown", "Error should mention unknown alias")
		assert.Contains(t, errMsg, "Available aliases:", "Error should list available aliases")
	})

	t.Run("host config context", func(t *testing.T) {
		tmpDir := t.TempDir()

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
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

		hostYAML := `
hosts:
  - id: 1
    domain: "test.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      enabled: true
      interval: 30m
      match_ua: ["$HostUnknown"]
`
		hostConfigPath := filepath.Join(hostsDir, "02-test.yaml")
		err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err)

		errMsg := err.Error()
		assert.Contains(t, errMsg, "test.example.com", "Error should mention host domain")
		assert.Contains(t, errMsg, "02-test.yaml", "Error should mention config file")
		assert.Contains(t, errMsg, "$HostUnknown", "Error should mention unknown alias")
	})

	t.Run("pattern config context", func(t *testing.T) {
		tmpDir := t.TempDir()

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
		err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
		require.NoError(t, err)

		hostsDir := filepath.Join(tmpDir, "hosts.d")
		err = os.Mkdir(hostsDir, 0o755)
		require.NoError(t, err)

		hostYAML := `
hosts:
  - id: 1
    domain: "api.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    url_rules:
      - match: "/v2/products/*"
        action: "render"
        bothit_recache:
          match_ua: ["$PatternUnknown"]
`
		hostConfigPath := filepath.Join(hostsDir, "api.yaml")
		err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0o644)
		require.NoError(t, err)

		logger := zap.NewNop()
		_, err = NewEGConfigManager(globalConfigPath, logger)

		require.Error(t, err)

		errMsg := err.Error()
		assert.Contains(t, errMsg, "url_rule[0]", "Error should mention URL rule index")
		assert.Contains(t, errMsg, "api.example.com", "Error should mention host domain")
		assert.Contains(t, errMsg, "api.yaml", "Error should mention config file")
		assert.Contains(t, errMsg, "$PatternUnknown", "Error should mention unknown alias")
	})
}

// TestBothitRecache_ErrorHandling_ListsAvailableAliases tests that errors list available aliases
func TestBothitRecache_ErrorHandling_ListsAvailableAliases(t *testing.T) {
	tmpDir := t.TempDir()

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
  match_ua: ["$UnknownAlias"]
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Create a minimal valid host file
	hostYAML := `
hosts:
  - id: 1
    domain: "test.com"
    render_key: "key"
    render:
      timeout: 30s
`
	err = os.WriteFile(filepath.Join(hostsDir, "test.yaml"), []byte(hostYAML), 0o644)
	require.NoError(t, err)

	logger := zap.NewNop()
	_, err = NewEGConfigManager(globalConfigPath, logger)

	require.Error(t, err, "Config loading should fail")

	errMsg := err.Error()

	t.Run("lists available aliases", func(t *testing.T) {
		assert.Contains(t, errMsg, "Available aliases:", "Error should include 'Available aliases:' label")

		// Check that at least some aliases are listed (first 5 alphabetically by default)
		// With 20 aliases, first 5 are: AIBots, AmazonUser, Amazonbot, AnthropicBot, AnthropicSearchBot
		assert.Contains(t, errMsg, "$AIBots", "Should list AIBots")
		assert.Contains(t, errMsg, "$AnthropicBot", "Should list AnthropicBot")
		assert.Contains(t, errMsg, "$Amazonbot", "Should list Amazonbot")
	})

	t.Run("shows truncation when many aliases", func(t *testing.T) {
		if strings.Contains(errMsg, "... and") {
			assert.Contains(t, errMsg, "more", "Should indicate there are more aliases")
		}
	})
}

// TestBothitRecache_ErrorHandling_IsFatal tests that unknown aliases prevent service startup
func TestBothitRecache_ErrorHandling_IsFatal(t *testing.T) {
	tmpDir := t.TempDir()

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
  match_ua: ["$InvalidAlias"]
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0o644)
	require.NoError(t, err)

	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0o755)
	require.NoError(t, err)

	// Create a minimal valid host file
	hostYAML := `
hosts:
  - id: 1
    domain: "test.com"
    render_key: "key"
    render:
      timeout: 30s
`
	err = os.WriteFile(filepath.Join(hostsDir, "test.yaml"), []byte(hostYAML), 0o644)
	require.NoError(t, err)

	logger := zap.NewNop()
	configManager, err := NewEGConfigManager(globalConfigPath, logger)

	t.Run("config manager creation fails", func(t *testing.T) {
		require.Error(t, err, "NewEGConfigManager should return error")
		assert.Nil(t, configManager, "Config manager should be nil when creation fails")
	})

	t.Run("error is wrapped correctly", func(t *testing.T) {
		assert.Contains(t, err.Error(), "failed to expand bot aliases", "Error should be wrapped with context")
		assert.Contains(t, err.Error(), "$InvalidAlias", "Error should include the invalid alias")
	})
}

// TestBothitRecache_ErrorHandling_SingleVsMultiple tests singular vs plural error messages
func TestBothitRecache_ErrorHandling_SingleVsMultiple(t *testing.T) {
	t.Run("single unknown alias uses singular", func(t *testing.T) {
		patterns := []string{"$UnknownBot"}
		_, err := ExpandBotAliases(patterns, "test context")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown bot alias", "Should use singular 'alias'")
		assert.NotContains(t, err.Error(), "unknown bot aliases", "Should not use plural")
	})

	t.Run("multiple unknown aliases use plural", func(t *testing.T) {
		patterns := []string{"$UnknownBot1", "$UnknownBot2"}
		_, err := ExpandBotAliases(patterns, "test context")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown bot aliases", "Should use plural 'aliases'")
		assert.Contains(t, err.Error(), "$UnknownBot1", "Should mention first alias")
		assert.Contains(t, err.Error(), "$UnknownBot2", "Should mention second alias")
	})

	t.Run("mixed valid and invalid reports only invalid", func(t *testing.T) {
		patterns := []string{"$GooglebotSearchDesktop", "$Invalid1", "$BingbotDesktop", "$Invalid2"}
		_, err := ExpandBotAliases(patterns, "test context")

		require.Error(t, err)
		errMsg := err.Error()
		assert.Contains(t, errMsg, "$Invalid1", "Should mention first invalid alias")
		assert.Contains(t, errMsg, "$Invalid2", "Should mention second invalid alias")

		// Check that valid aliases are not mentioned in the "unknown bot aliases" part
		// (they will appear in "Available aliases:" section which is correct)
		assert.Contains(t, errMsg, `"$Invalid1", "$Invalid2"`, "Unknown aliases should be listed together")
		assert.NotContains(t, errMsg, `"$GooglebotSearchDesktop"`, "Valid aliases should not be in the unknown list")
	})
}

// TestBothitRecache_ErrorHandling_ConsistentFormat tests error format consistency across levels
func TestBothitRecache_ErrorHandling_ConsistentFormat(t *testing.T) {
	t.Run("all levels have consistent structure", func(t *testing.T) {
		// Test ExpandBotAliases function (used for all levels)
		patterns := []string{"$InvalidBot"}

		contexts := []string{
			"global config",
			"host 'example.com' (hosts.d/01-host.yaml)",
			"URL pattern '/api/*' in host 'example.com' (hosts.d/01-host.yaml)",
		}

		for _, context := range contexts {
			_, err := ExpandBotAliases(patterns, context)
			require.Error(t, err, "Should error for context: %s", context)

			errMsg := err.Error()
			assert.Contains(t, errMsg, "unknown bot alias", "Should have 'unknown bot alias'")
			assert.Contains(t, errMsg, "$InvalidBot", "Should mention the invalid alias")
			assert.Contains(t, errMsg, context, "Should include the context")
			assert.Contains(t, errMsg, "Available aliases:", "Should list available aliases")
		}
	})
}

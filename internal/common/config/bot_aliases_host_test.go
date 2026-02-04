package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBothitRecache_HostLevel tests host-level bot alias expansion with various scenarios
func TestBothitRecache_HostLevel(t *testing.T) {
	tests := []struct {
		name          string
		globalYAML    string
		hostYAML      string
		expectError   bool
		errorContains []string
		validateFunc  func(t *testing.T, manager *EGConfigManager)
	}{
		{
			name: "host alias expands correctly",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$GooglebotSearchDesktop"]
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]

				require.NotNil(t, host.BothitRecache)
				assert.Len(t, host.BothitRecache.MatchUA, 5)
				assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
				assert.Contains(t, host.BothitRecache.MatchUA, "Googlebot/2.1 (+http://www.google.com/bot.html)")
			},
		},
		{
			name: "host overrides global bothit_recache",
			globalYAML: `
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
  match_ua: ["$BingbotDesktop"]
  interval: 12h
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$GooglebotSearchDesktop"]
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				// Verify global has Bing patterns
				require.NotNil(t, manager.config.BothitRecache)
				assert.Len(t, manager.config.BothitRecache.MatchUA, 3)
				assert.Contains(t, manager.config.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

				// Verify host has Google patterns (not Bing)
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]

				require.NotNil(t, host.BothitRecache)
				assert.Len(t, host.BothitRecache.MatchUA, 5)
				assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
				assert.NotContains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
			},
		},
		{
			name: "multiple hosts with different aliases",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "site1.example.com"
    render_key: "test-key-1"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$GooglebotSearchDesktop"]
  - id: 2
    domain: "site2.example.com"
    render_key: "test-key-2"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$BingbotDesktop"]
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				require.Len(t, manager.hosts.Hosts, 2)

				// Verify host 1 has Google patterns
				host1 := manager.hosts.Hosts[0]
				require.NotNil(t, host1.BothitRecache)
				assert.Len(t, host1.BothitRecache.MatchUA, 5)
				assert.Contains(t, host1.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
				assert.NotContains(t, host1.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")

				// Verify host 2 has Bing patterns
				host2 := manager.hosts.Hosts[1]
				require.NotNil(t, host2.BothitRecache)
				assert.Len(t, host2.BothitRecache.MatchUA, 3)
				assert.Contains(t, host2.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
				assert.NotContains(t, host2.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
			},
		},
		{
			name: "unknown alias in host fails with host context",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$UnknownBot"]
`,
			expectError:   true,
			errorContains: []string{"shop.example.com", "$UnknownBot", "Available aliases:", "hosts.d/01-hosts.yaml"},
		},
		{
			name: "host inherits global when no local bothit_recache",
			globalYAML: `
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
  match_ua: ["$GooglebotSearchDesktop"]
  interval: 12h
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				// Verify global has expanded patterns
				require.NotNil(t, manager.config.BothitRecache)
				assert.Len(t, manager.config.BothitRecache.MatchUA, 5)

				// Verify host has NO bothit_recache (will inherit at runtime)
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]
				assert.Nil(t, host.BothitRecache)
			},
		},
		{
			name: "host with empty bothit_recache",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache: {}
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]

				require.NotNil(t, host.BothitRecache)
				assert.Len(t, host.BothitRecache.MatchUA, 0)
			},
		},
		{
			name: "mixed alias and custom patterns",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["*CustomBot*", "$GoogleBotAds", "Mozilla/5.0 (custom)"]
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]

				require.NotNil(t, host.BothitRecache)
				assert.Len(t, host.BothitRecache.MatchUA, 3)
				assert.Equal(t, "*CustomBot*", host.BothitRecache.MatchUA[0])
				assert.Equal(t, "AdsBot-Google (+http://www.google.com/adsbot.html)", host.BothitRecache.MatchUA[1])
				assert.Equal(t, "Mozilla/5.0 (custom)", host.BothitRecache.MatchUA[2])
			},
		},
		{
			name: "multiple aliases in host expand correctly",
			globalYAML: `
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
`,
			hostYAML: `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$GooglebotSearchDesktop", "$BingbotDesktop"]
`,
			expectError: false,
			validateFunc: func(t *testing.T, manager *EGConfigManager) {
				require.Len(t, manager.hosts.Hosts, 1)
				host := manager.hosts.Hosts[0]

				require.NotNil(t, host.BothitRecache)
				assert.Len(t, host.BothitRecache.MatchUA, 8)
				assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
				assert.Contains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()

			// Create global config file
			globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			err := os.WriteFile(globalConfigPath, []byte(tt.globalYAML), 0644)
			require.NoError(t, err)

			// Create hosts directory
			hostsDir := filepath.Join(tmpDir, "hosts.d")
			err = os.Mkdir(hostsDir, 0755)
			require.NoError(t, err)

			// Create host config file
			hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
			err = os.WriteFile(hostConfigPath, []byte(tt.hostYAML), 0644)
			require.NoError(t, err)

			// Create config manager and load config
			logger := zap.NewNop()
			manager, err := NewEGConfigManager(globalConfigPath, logger)

			if tt.expectError {
				require.Error(t, err)
				errMsg := err.Error()
				for _, expected := range tt.errorContains {
					assert.Contains(t, errMsg, expected, fmt.Sprintf("Error should contain '%s'", expected))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, manager)

				if tt.validateFunc != nil {
					tt.validateFunc(t, manager)
				}
			}
		})
	}
}

// TestBothitRecache_HostLevel_ErrorIncludesFilePath verifies error messages include config file path
func TestBothitRecache_HostLevel_ErrorIncludesFilePath(t *testing.T) {
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

	// Create host config with specific filename
	hostYAML := `
hosts:
  - id: 1
    domain: "shop.example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$UnknownBot"]
`
	hostConfigPath := filepath.Join(hostsDir, "02-shop.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	_, err = NewEGConfigManager(globalConfigPath, logger)

	// Verify error includes file path
	require.Error(t, err)
	assert.Contains(t, err.Error(), "02-shop.yaml")
	assert.Contains(t, err.Error(), "shop.example.com")
	assert.Contains(t, err.Error(), "$UnknownBot")
}

// TestBothitRecache_HostLevel_MultipleFiles tests loading hosts from multiple files
func TestBothitRecache_HostLevel_MultipleFiles(t *testing.T) {
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

	// Create first host file
	host1YAML := `
hosts:
  - id: 1
    domain: "site1.example.com"
    render_key: "test-key-1"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$GooglebotSearchDesktop"]
`
	host1Path := filepath.Join(hostsDir, "01-site1.yaml")
	err = os.WriteFile(host1Path, []byte(host1YAML), 0644)
	require.NoError(t, err)

	// Create second host file
	host2YAML := `
hosts:
  - id: 2
    domain: "site2.example.com"
    render_key: "test-key-2"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$BingbotMobile"]
`
	host2Path := filepath.Join(hostsDir, "02-site2.yaml")
	err = os.WriteFile(host2Path, []byte(host2YAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify both hosts loaded with correct expansions
	require.Len(t, manager.hosts.Hosts, 2)

	// Verify host 1
	host1 := manager.hosts.Hosts[0]
	assert.Equal(t, "site1.example.com", host1.Domain)
	require.NotNil(t, host1.BothitRecache)
	assert.Len(t, host1.BothitRecache.MatchUA, 5)
	assert.Contains(t, host1.BothitRecache.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")

	// Verify host 2
	host2 := manager.hosts.Hosts[1]
	assert.Equal(t, "site2.example.com", host2.Domain)
	require.NotNil(t, host2.BothitRecache)
	assert.Len(t, host2.BothitRecache.MatchUA, 4)
	assert.Contains(t, host2.BothitRecache.MatchUA, "Mozilla/5.0 (iPhone; CPU iPhone OS 7_0 like Mac OS X) AppleWebKit/537.51.1 (KHTML, like Gecko) Version/7.0 Mobile/11A465 Safari/9537.53 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)")
}

// TestBothitRecache_HostLevel_GlobalAndHostBothExpanded tests both global and host-level expansions
func TestBothitRecache_HostLevel_GlobalAndHostBothExpanded(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create global config with bothit_recache
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
  match_ua: ["$ChatGPTUserBot"]
  interval: 6h
`
	globalConfigPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	err := os.WriteFile(globalConfigPath, []byte(globalYAML), 0644)
	require.NoError(t, err)

	// Create hosts directory
	hostsDir := filepath.Join(tmpDir, "hosts.d")
	err = os.Mkdir(hostsDir, 0755)
	require.NoError(t, err)

	// Create host config with different alias
	hostYAML := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["$AnthropicBot"]
`
	hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
	require.NoError(t, err)

	// Load config
	logger := zap.NewNop()
	manager, err := NewEGConfigManager(globalConfigPath, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify global bothit_recache expanded
	require.NotNil(t, manager.config.BothitRecache)
	assert.Len(t, manager.config.BothitRecache.MatchUA, 1)
	assert.Contains(t, manager.config.BothitRecache.MatchUA, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot")

	// Verify host bothit_recache expanded independently
	require.Len(t, manager.hosts.Hosts, 1)
	host := manager.hosts.Hosts[0]
	require.NotNil(t, host.BothitRecache)
	assert.Len(t, host.BothitRecache.MatchUA, 1)
	assert.Contains(t, host.BothitRecache.MatchUA, "*ClaudeBot/1.0; +claudebot@anthropic.com*")

	// Verify no cross-contamination
	assert.NotContains(t, manager.config.BothitRecache.MatchUA, "*ClaudeBot/1.0; +claudebot@anthropic.com*")
	assert.NotContains(t, host.BothitRecache.MatchUA, "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot")
}

// TestBothitRecache_HostLevel_CaseSensitivity tests case-sensitive alias matching at host level
func TestBothitRecache_HostLevel_CaseSensitivity(t *testing.T) {
	tests := []struct {
		name        string
		alias       string
		shouldError bool
	}{
		{
			name:        "correct case",
			alias:       "$GooglebotSearchDesktop",
			shouldError: false,
		},
		{
			name:        "lowercase",
			alias:       "$googlebotSearchDesktop",
			shouldError: true,
		},
		{
			name:        "uppercase",
			alias:       "$GOOGLEBOTSEARCHDESKTOP",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			// Create host config with test alias
			hostYAML := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
    bothit_recache:
      match_ua: ["%s"]
`, tt.alias)
			hostConfigPath := filepath.Join(hostsDir, "01-hosts.yaml")
			err = os.WriteFile(hostConfigPath, []byte(hostYAML), 0644)
			require.NoError(t, err)

			// Load config
			logger := zap.NewNop()
			_, err = NewEGConfigManager(globalConfigPath, logger)

			if tt.shouldError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown bot alias")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

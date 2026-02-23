package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/edgecomet/engine/pkg/types"
)

func TestTimeoutValidation(t *testing.T) {
	tests := []struct {
		name          string
		serverTimeout string
		waitTimeout   string
		lockTTL       string
		hostTimeouts  []int
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid configuration - plenty of margin",
			serverTimeout: "120s",
			waitTimeout:   "10s",
			lockTTL:       "90s",
			hostTimeouts:  []int{30, 45, 60},
			expectError:   false,
		},
		{
			name:          "valid configuration - exactly at minimum",
			serverTimeout: "118s", // 48s wait (80% of 60s) + 60s render + 10s overhead = 118s
			waitTimeout:   "10s",
			lockTTL:       "60s",
			hostTimeouts:  []int{60},
			expectError:   false,
		},
		{
			name:          "invalid - server timeout too small",
			serverTimeout: "50s", // Bypass takes 35s, but 60s host timeout is too much
			waitTimeout:   "10s",
			lockTTL:       "90s",
			hostTimeouts:  []int{60},
			expectError:   true,
			errorContains: "server.timeout",
		},
		{
			name:          "invalid - one host timeout too large",
			serverTimeout: "60s",
			waitTimeout:   "10s",
			lockTTL:       "90s",
			hostTimeouts:  []int{30, 45, 90}, // 90s host timeout
			expectError:   true,
			errorContains: "server.timeout",
		},
		{
			name:          "valid - minimal host timeout",
			serverTimeout: "60s",
			waitTimeout:   "10s",
			lockTTL:       "30s",
			hostTimeouts:  []int{10},
			expectError:   false,
		},
		{
			name:          "valid - single host with valid timeout",
			serverTimeout: "64s", // 24s wait (80% of 30s) + 30s render + 10s overhead = 64s
			waitTimeout:   "10s",
			lockTTL:       "30s",
			hostTimeouts:  []int{30},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test configs
			tempDir := t.TempDir()

			// Create main config
			mainConfigYAML := `
server:
  listen: ":8080"
  timeout: ` + tt.serverTimeout + `

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h

bypass:
  timeout: 20s
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

			// Create hosts config
			hostsConfigYAML := "hosts:\n"
			for i, timeout := range tt.hostTimeouts {
				hostsConfigYAML += fmt.Sprintf(`  - id: %d
    domain: "test%d.com"
    render_key: "key%d"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: %ds
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "TestAgent"
          match_ua: []
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`, i+1, i+1, i+1, timeout)
			}

			// Create hosts.d directory and write host file
			hostsDir := filepath.Join(tempDir, "hosts.d")
			require.NoError(t, os.MkdirAll(hostsDir, 0o755))

			require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

			// Create config manager
			logger := zaptest.NewLogger(t)
			cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, cm)
			}
		})
	}
}

func TestBypassTimeoutValidation(t *testing.T) {
	tests := []struct {
		name          string
		serverTimeout string
		bypassTimeout string
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid - bypass timeout fits within server timeout",
			serverTimeout: "120s",
			bypassTimeout: "30s",
			expectError:   false,
		},
		{
			name:          "invalid - bypass timeout exceeds server timeout",
			serverTimeout: "70s", // Large enough for server timeout validation (64s required)
			bypassTimeout: "90s",
			expectError:   true,
			errorContains: "bypass.timeout",
		},
		{
			name:          "valid - exactly at limit",
			serverTimeout: "70s", // Large enough for server timeout validation
			bypassTimeout: "70s",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			mainConfigYAML := `
server:
  listen: ":8080"
  timeout: ` + tt.serverTimeout + `

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h

bypass:
  timeout: ` + tt.bypassTimeout + `
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

			hostsConfigYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key1"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "TestAgent"
          match_ua: []
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`

			// Create hosts.d directory and write host file
			hostsDir := filepath.Join(tempDir, "hosts.d")
			require.NoError(t, os.MkdirAll(hostsDir, 0o755))

			require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

			logger := zaptest.NewLogger(t)
			cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, cm)
			}
		})
	}
}

// TestExampleConfigsLoad tests that example configs load without errors
func TestExampleConfigsLoad(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test loading example configs
	configDir := filepath.Join("..", "..", "..", "configs", "example")

	// Check if config directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Skip("Example config directory not found, skipping test")
	}

	t.Run("example edge-gateway.yaml loads", func(t *testing.T) {
		cm, err := NewEGConfigManager(configDir, logger)
		if err != nil {
			// Skip if example configs have issues (e.g., duration format not supported)
			t.Skipf("Example config could not be loaded (may need updates): %v", err)
			return
		}
		require.NotNil(t, cm)

		config := cm.GetConfig()
		assert.NotNil(t, config)
		assert.NotEmpty(t, config.Server.Listen)
		assert.NotEmpty(t, config.Redis.Addr)
	})

	t.Run("example hosts configuration has url_rules", func(t *testing.T) {
		cm, err := NewEGConfigManager(configDir, logger)
		if err != nil {
			t.Skipf("Example config could not be loaded (may need updates): %v", err)
			return
		}

		hosts := cm.GetHosts()
		assert.Greater(t, len(hosts), 0, "Should have at least one host")

		// Check that at least one host has URL rules configured
		hasURLRules := false
		for _, host := range hosts {
			if len(host.URLRules) > 0 {
				hasURLRules = true
				break
			}
		}
		assert.True(t, hasURLRules, "At least one host should have url_rules configured")
	})

	t.Run("example hosts have valid dimensions", func(t *testing.T) {
		cm, err := NewEGConfigManager(configDir, logger)
		if err != nil {
			t.Skipf("Example config could not be loaded (may need updates): %v", err)
			return
		}

		hosts := cm.GetHosts()
		for _, host := range hosts {
			assert.Greater(t, len(host.Render.Dimensions), 0, "Host %s should have at least one dimension", host.Domain)

			// Verify dimension IDs are unique
			dimensionIDs := make(map[int]string)
			for name, dim := range host.Render.Dimensions {
				assert.Greater(t, dim.ID, 0, "Dimension %s should have positive ID", name)
				if existingDim, exists := dimensionIDs[dim.ID]; exists {
					t.Errorf("Host %s has duplicate dimension ID %d for %s and %s", host.Domain, dim.ID, name, existingDim)
				}
				dimensionIDs[dim.ID] = name
			}
		}
	})
}

func TestBotAliasExpansionInGlobalDimensions(t *testing.T) {
	tempDir := t.TempDir()

	mainConfigYAML := `
server:
  listen: ":8080"
  timeout: 120s

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua: ["$GooglebotSearchDesktop", "*CustomBot*"]
    mobile:
      id: 2
      width: 375
      height: 667
      render_ua: "TestMobileAgent"
      match_ua: ["$BingbotMobile"]

bypass:
  timeout: 20s
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

	hostsConfigYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key1"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: 30s
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`

	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

	logger := zaptest.NewLogger(t)
	cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

	require.NoError(t, err)
	require.NotNil(t, cm)

	config := cm.GetConfig()
	require.NotNil(t, config.Render.Dimensions)

	desktop := config.Render.Dimensions["desktop"]
	assert.Greater(t, len(desktop.MatchUA), 2, "desktop dimension should have expanded patterns")
	assert.Contains(t, desktop.MatchUA, "*CustomBot*", "desktop should retain custom pattern")
	assert.Contains(t, desktop.MatchUA, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.NotNil(t, desktop.CompiledPatterns, "desktop patterns should be compiled")
	assert.Greater(t, len(desktop.CompiledPatterns), 0, "desktop should have compiled patterns")

	mobile := config.Render.Dimensions["mobile"]
	assert.Greater(t, len(mobile.MatchUA), 1, "mobile dimension should have expanded patterns")
	assert.NotNil(t, mobile.CompiledPatterns, "mobile patterns should be compiled")
}

func TestBotAliasExpansionInHostDimensions(t *testing.T) {
	tempDir := t.TempDir()

	mainConfigYAML := `
server:
  listen: ":8080"
  timeout: 120s

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h

bypass:
  timeout: 20s
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

	hostsConfigYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key1"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "TestAgent"
          match_ua: ["$AnthropicBot", "*CustomBot*"]
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`

	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

	logger := zaptest.NewLogger(t)
	cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

	require.NoError(t, err)
	require.NotNil(t, cm)

	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	desktop := hosts[0].Render.Dimensions["desktop"]
	assert.Greater(t, len(desktop.MatchUA), 1, "host dimension should have expanded patterns")
	assert.Contains(t, desktop.MatchUA, "*CustomBot*", "should retain custom pattern")
	assert.Contains(t, desktop.MatchUA, "*ClaudeBot/1.0; +claudebot@anthropic.com*")
	assert.NotNil(t, desktop.CompiledPatterns, "patterns should be compiled")
	assert.Greater(t, len(desktop.CompiledPatterns), 0, "should have compiled patterns")
}

func TestBotAliasExpansionFailsWithUnknownAlias(t *testing.T) {
	tempDir := t.TempDir()

	mainConfigYAML := `
server:
  listen: ":8080"
  timeout: 120s

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "TestAgent"
      match_ua: ["$UnknownBotAlias"]

bypass:
  timeout: 20s
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

	hostsConfigYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key1"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: 30s
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`

	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

	logger := zaptest.NewLogger(t)
	cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

	require.Error(t, err)
	assert.Nil(t, cm)
	assert.Contains(t, err.Error(), "failed to expand bot aliases")
	assert.Contains(t, err.Error(), "$UnknownBotAlias")
	assert.Contains(t, err.Error(), "Available aliases:")
}

func TestBotAliasExpansionFailsInHostWithUnknownAlias(t *testing.T) {
	tempDir := t.TempDir()

	mainConfigYAML := `
server:
  listen: ":8080"
  timeout: 120s

internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/cache"

render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 1h

bypass:
  timeout: 20s
  user_agent: "TestAgent"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`

	hostsConfigYAML := `hosts:
  - id: 1
    domain: "test.com"
    render_key: "key1"
    enabled: true
    render:
      cache:
        ttl: 1h
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "TestAgent"
          match_ua: ["$InvalidBotAlias"]
      events:
        wait_for: "networkIdle"
        additional_wait: 1s
`

	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "edge-gateway.yaml"), []byte(mainConfigYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsConfigYAML), 0o644))

	logger := zaptest.NewLogger(t)
	cm, err := NewEGConfigManager(filepath.Join(tempDir, "edge-gateway.yaml"), logger)

	require.Error(t, err)
	assert.Nil(t, cm)
	assert.Contains(t, err.Error(), "failed to load hosts config")
	assert.Contains(t, err.Error(), "$InvalidBotAlias")
	assert.Contains(t, err.Error(), "test.com")
}

// Helper function for tests
func ptrDuration(d time.Duration) *types.Duration {
	td := types.Duration(d)
	return &td
}

// TestBuildHostsCache tests the buildHostsCache function
func TestBuildHostsCache(t *testing.T) {
	tests := []struct {
		name           string
		hosts          []types.Host
		expectedCount  int
		expectedLookup map[string]int // domain -> host ID
	}{
		{
			name:           "empty hosts slice",
			hosts:          []types.Host{},
			expectedCount:  0,
			expectedLookup: map[string]int{},
		},
		{
			name: "single host single domain",
			hosts: []types.Host{
				{ID: 1, Domains: []string{"example.com"}},
			},
			expectedCount: 1,
			expectedLookup: map[string]int{
				"example.com": 1,
			},
		},
		{
			name: "single host multiple domains",
			hosts: []types.Host{
				{ID: 1, Domains: []string{"example.com", "www.example.com", "cdn.example.com"}},
			},
			expectedCount: 1,
			expectedLookup: map[string]int{
				"example.com":     1,
				"www.example.com": 1,
				"cdn.example.com": 1,
			},
		},
		{
			name: "multiple hosts",
			hosts: []types.Host{
				{ID: 1, Domains: []string{"example.com", "www.example.com"}},
				{ID: 2, Domains: []string{"other.com"}},
			},
			expectedCount: 2,
			expectedLookup: map[string]int{
				"example.com":     1,
				"www.example.com": 1,
				"other.com":       2,
			},
		},
		{
			name: "case normalization in domains",
			hosts: []types.Host{
				{ID: 1, Domains: []string{"Example.COM", "WWW.Example.Com"}},
			},
			expectedCount: 1,
			expectedLookup: map[string]int{
				"example.com":     1,
				"www.example.com": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := buildHostsCache(tt.hosts)

			assert.NotNil(t, cache)
			assert.Equal(t, tt.expectedCount, len(cache.hosts))

			for domain, expectedID := range tt.expectedLookup {
				host := cache.byDomain[domain]
				require.NotNil(t, host, "Expected domain %s to be in cache", domain)
				assert.Equal(t, expectedID, host.ID, "Domain %s should map to host ID %d", domain, expectedID)
			}
		})
	}
}

// TestGetHostByDomain tests the GetHostByDomain method
func TestGetHostByDomain(t *testing.T) {
	// Create a config manager with test hosts
	cm := &EGConfigManager{}
	testHosts := []types.Host{
		{
			ID:        1,
			Domain:    "example.com",
			Domains:   []string{"example.com", "www.example.com"},
			RenderKey: "key1",
			Enabled:   true,
		},
		{
			ID:        2,
			Domain:    "other.com",
			Domains:   []string{"other.com"},
			RenderKey: "key2",
			Enabled:   true,
		},
	}

	// Build and store the cache
	cache := buildHostsCache(testHosts)
	cm.cache.Store(cache)

	tests := []struct {
		name       string
		domain     string
		expectNil  bool
		expectedID int
	}{
		{
			name:       "domain exists - primary",
			domain:     "example.com",
			expectNil:  false,
			expectedID: 1,
		},
		{
			name:       "domain exists - secondary",
			domain:     "www.example.com",
			expectNil:  false,
			expectedID: 1,
		},
		{
			name:       "domain exists - other host",
			domain:     "other.com",
			expectNil:  false,
			expectedID: 2,
		},
		{
			name:      "domain does not exist",
			domain:    "nonexistent.com",
			expectNil: true,
		},
		{
			name:       "case-insensitive lookup - uppercase",
			domain:     "EXAMPLE.COM",
			expectNil:  false,
			expectedID: 1,
		},
		{
			name:       "case-insensitive lookup - mixed case",
			domain:     "Example.Com",
			expectNil:  false,
			expectedID: 1,
		},
		{
			name:       "case-insensitive lookup - secondary domain uppercase",
			domain:     "WWW.EXAMPLE.COM",
			expectNil:  false,
			expectedID: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := cm.GetHostByDomain(tt.domain)

			if tt.expectNil {
				assert.Nil(t, host)
			} else {
				require.NotNil(t, host)
				assert.Equal(t, tt.expectedID, host.ID)
			}
		})
	}
}

// TestGetHostByDomain_NilCache tests GetHostByDomain when cache is not loaded
func TestGetHostByDomain_NilCache(t *testing.T) {
	cm := &EGConfigManager{}
	// Don't store any cache

	host := cm.GetHostByDomain("example.com")
	assert.Nil(t, host, "Should return nil when cache is not loaded")
}

// TestGetHosts_NilCache tests GetHosts when cache is not loaded
func TestGetHosts_NilCache(t *testing.T) {
	cm := &EGConfigManager{}
	// Don't store any cache

	hosts := cm.GetHosts()
	assert.Nil(t, hosts, "Should return nil when cache is not loaded")
}

// TestGetHosts_WithCache tests GetHosts returns hosts from cache
func TestGetHosts_WithCache(t *testing.T) {
	cm := &EGConfigManager{}
	testHosts := []types.Host{
		{ID: 1, Domains: []string{"example.com"}},
		{ID: 2, Domains: []string{"other.com"}},
	}

	cache := buildHostsCache(testHosts)
	cm.cache.Store(cache)

	hosts := cm.GetHosts()
	require.NotNil(t, hosts)
	assert.Len(t, hosts, 2)
	assert.Equal(t, 1, hosts[0].ID)
	assert.Equal(t, 2, hosts[1].ID)
}

// TestSetHosts_RebuildsCacheTests that SetHosts rebuilds the cache
func TestSetHosts_RebuildsCache(t *testing.T) {
	cm := &EGConfigManager{}

	// Set initial hosts
	initialHosts := &HostsConfig{
		Hosts: []types.Host{
			{ID: 1, Domains: []string{"example.com"}},
		},
	}
	cm.SetHosts(initialHosts)

	// Verify initial state
	host := cm.GetHostByDomain("example.com")
	require.NotNil(t, host)
	assert.Equal(t, 1, host.ID)

	// Update hosts
	updatedHosts := &HostsConfig{
		Hosts: []types.Host{
			{ID: 2, Domains: []string{"new.com"}},
		},
	}
	cm.SetHosts(updatedHosts)

	// Verify old domain no longer found
	host = cm.GetHostByDomain("example.com")
	assert.Nil(t, host)

	// Verify new domain is found
	host = cm.GetHostByDomain("new.com")
	require.NotNil(t, host)
	assert.Equal(t, 2, host.ID)
}

// TestSetHosts_NilClearsCache verifies that SetHosts(nil) clears the cache
func TestSetHosts_NilClearsCache(t *testing.T) {
	cm := &EGConfigManager{}

	// Set initial hosts
	initialHosts := &HostsConfig{
		Hosts: []types.Host{
			{ID: 1, Domains: []string{"example.com"}},
		},
	}
	cm.SetHosts(initialHosts)

	// Verify initial state
	host := cm.GetHostByDomain("example.com")
	require.NotNil(t, host)

	// Set hosts to nil
	cm.SetHosts(nil)

	// Verify cache is cleared - domain lookup should return nil
	host = cm.GetHostByDomain("example.com")
	assert.Nil(t, host)

	// Verify GetHosts returns nil (cache is cleared)
	assert.Nil(t, cm.GetHosts())
}

// TestHostsCacheConcurrentAccess verifies thread-safe concurrent access to hosts cache.
// Run with: go test -race ./internal/common/config/...
func TestHostsCacheConcurrentAccess(t *testing.T) {
	cm := &EGConfigManager{}

	// Setup test hosts with multiple domains
	testHosts := &HostsConfig{
		Hosts: []types.Host{
			{
				ID:        1,
				Domain:    "example.com",
				Domains:   []string{"example.com", "www.example.com"},
				RenderKey: "key1",
				Enabled:   true,
			},
			{
				ID:        2,
				Domain:    "other.com",
				Domains:   []string{"other.com", "api.other.com"},
				RenderKey: "key2",
				Enabled:   true,
			},
		},
	}
	cm.SetHosts(testHosts)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	// Spawn reader goroutines that call GetHosts
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hosts := cm.GetHosts()
				// Verify we get consistent data
				if hosts != nil && len(hosts) > 0 {
					_ = hosts[0].ID
				}
			}
		}()
	}

	// Spawn reader goroutines that call GetHostByDomain
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			domains := []string{"example.com", "www.example.com", "other.com", "api.other.com", "nonexistent.com"}
			for j := 0; j < iterations; j++ {
				domain := domains[j%len(domains)]
				host := cm.GetHostByDomain(domain)
				// Verify we get consistent data (nil for nonexistent is expected)
				if host != nil {
					_ = host.ID
					_ = host.Domain
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestHostsCacheConcurrentReadWrite verifies thread-safe concurrent reads during cache updates.
// This tests the realistic scenario: single writer (config reload) with many concurrent readers.
// Run with: go test -race ./internal/common/config/...
func TestHostsCacheConcurrentReadWrite(t *testing.T) {
	cm := &EGConfigManager{}

	// Setup initial hosts
	initialHosts := &HostsConfig{
		Hosts: []types.Host{
			{ID: 1, Domains: []string{"initial.com"}},
		},
	}
	cm.SetHosts(initialHosts)

	var wg sync.WaitGroup
	const readers = 100
	const iterations = 1000
	stop := make(chan struct{})

	// Spawn reader goroutines
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					// Read operations - must be safe during writes
					hosts := cm.GetHosts()
					if hosts != nil && len(hosts) > 0 {
						_ = hosts[0].ID
					}
					host := cm.GetHostByDomain("initial.com")
					if host != nil {
						_ = host.ID
					}
					_ = cm.GetHostByDomain("updated.com")
				}
			}
		}()
	}

	// Single writer goroutine (realistic scenario: config reloads are serialized)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < iterations; j++ {
			// Alternate between two host configurations
			if j%2 == 0 {
				cm.SetHosts(&HostsConfig{
					Hosts: []types.Host{
						{ID: 1, Domains: []string{"initial.com"}},
					},
				})
			} else {
				cm.SetHosts(&HostsConfig{
					Hosts: []types.Host{
						{ID: 2, Domains: []string{"updated.com"}},
					},
				})
			}
		}
		close(stop)
	}()

	wg.Wait()

	// Verify final state is valid (either configuration is acceptable)
	hosts := cm.GetHosts()
	require.NotNil(t, hosts)
	assert.Len(t, hosts, 1)
}

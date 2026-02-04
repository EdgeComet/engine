package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func TestGlobalDimensions_Loading(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global dimensions
	mainConfig := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
redis:
  addr: "localhost:6379"
  db: 0
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
      render_ua: "Mozilla/5.0 Desktop"
      match_ua: ["*Googlebot*", "*bingbot*"]
    mobile:
      id: 2
      width: 375
      height: 812
      render_ua: "Mozilla/5.0 Mobile"
      match_ua: ["*Mobile*"]
bypass:
  user_agent: "EdgeComet"
  cache:
    enabled: false
log:
  level: "info"
  console:
    enabled: false
metrics:
  enabled: false
hosts:
  include: "hosts.d/"
`

	mainConfigPath := filepath.Join(tempDir, "edge-gateway.yaml")
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0644))

	// Create hosts directory with single host (no dimensions - should inherit)
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0755))

	hostConfig := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify global dimensions were loaded
	cfg := cm.GetConfig()
	require.NotNil(t, cfg.Render.Dimensions)
	assert.Len(t, cfg.Render.Dimensions, 2)

	// Verify desktop dimension
	desktop, exists := cfg.Render.Dimensions["desktop"]
	assert.True(t, exists)
	assert.Equal(t, 1, desktop.ID)
	assert.Equal(t, 1920, desktop.Width)
	assert.Equal(t, 1080, desktop.Height)
	assert.Equal(t, "Mozilla/5.0 Desktop", desktop.RenderUA)
	assert.Len(t, desktop.MatchUA, 2)

	// Verify mobile dimension
	mobile, exists := cfg.Render.Dimensions["mobile"]
	assert.True(t, exists)
	assert.Equal(t, 2, mobile.ID)
	assert.Equal(t, 375, mobile.Width)
	assert.Equal(t, 812, mobile.Height)

	// Verify patterns were compiled
	assert.NotNil(t, desktop.CompiledPatterns)
	assert.NotNil(t, mobile.CompiledPatterns)

	// Verify host inherited dimensions
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "example.com", host.Domain)
	require.NotNil(t, host.Render.Dimensions)
	assert.Len(t, host.Render.Dimensions, 2)

	// Verify inherited desktop dimension
	inheritedDesktop, exists := host.Render.Dimensions["desktop"]
	assert.True(t, exists)
	assert.Equal(t, 1, inheritedDesktop.ID)
	assert.Equal(t, 1920, inheritedDesktop.Width)
	assert.Equal(t, 1080, inheritedDesktop.Height)

	// Verify inherited mobile dimension
	inheritedMobile, exists := host.Render.Dimensions["mobile"]
	assert.True(t, exists)
	assert.Equal(t, 2, inheritedMobile.ID)
	assert.Equal(t, 375, inheritedMobile.Width)
	assert.Equal(t, 812, inheritedMobile.Height)
}

func TestGlobalDimensions_HostOverride(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global dimensions
	mainConfig := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
redis:
  addr: "localhost:6379"
  db: 0
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
      render_ua: "Mozilla/5.0 Global Desktop"
      match_ua: ["*Googlebot*"]
bypass:
  user_agent: "EdgeComet"
  cache:
    enabled: false
log:
  level: "info"
  console:
    enabled: false
metrics:
  enabled: false
hosts:
  include: "hosts.d/"
`

	mainConfigPath := filepath.Join(tempDir, "edge-gateway.yaml")
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0644))

	// Create hosts directory with host that has custom dimensions
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0755))

	hostConfig := `
hosts:
  - id: 1
    domain: "custom.com"
    render_key: "test-key"
    render:
      timeout: 30s
      dimensions:
        custom:
          id: 10
          width: 1440
          height: 900
          render_ua: "Mozilla/5.0 Custom"
          match_ua: ["*Custom*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host used its own dimensions (not inherited)
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "custom.com", host.Domain)
	require.NotNil(t, host.Render.Dimensions)
	assert.Len(t, host.Render.Dimensions, 1)

	// Verify custom dimension (not global)
	custom, exists := host.Render.Dimensions["custom"]
	assert.True(t, exists)
	assert.Equal(t, 10, custom.ID)
	assert.Equal(t, 1440, custom.Width)
	assert.Equal(t, 900, custom.Height)
	assert.Equal(t, "Mozilla/5.0 Custom", custom.RenderUA)

	// Verify global dimension NOT inherited
	_, exists = host.Render.Dimensions["desktop"]
	assert.False(t, exists)
}

func TestGlobalDimensions_EmptyMapInherits(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global dimensions
	mainConfig := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
redis:
  addr: "localhost:6379"
  db: 0
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
      render_ua: "Mozilla/5.0 Desktop"
      match_ua: ["*Googlebot*"]
bypass:
  user_agent: "EdgeComet"
  cache:
    enabled: false
log:
  level: "info"
  console:
    enabled: false
metrics:
  enabled: false
hosts:
  include: "hosts.d/"
`

	mainConfigPath := filepath.Join(tempDir, "edge-gateway.yaml")
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0644))

	// Create hosts directory with host that has empty dimensions map
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0755))

	hostConfig := `
hosts:
  - id: 1
    domain: "inherit.com"
    render_key: "test-key"
    render:
      timeout: 30s
      dimensions: {}
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host inherited global dimensions (empty map = not set)
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "inherit.com", host.Domain)
	require.NotNil(t, host.Render.Dimensions)
	assert.Len(t, host.Render.Dimensions, 1)

	// Verify inherited dimension
	desktop, exists := host.Render.Dimensions["desktop"]
	assert.True(t, exists)
	assert.Equal(t, 1, desktop.ID)
	assert.Equal(t, 1920, desktop.Width)
}

func TestGlobalDimensions_NoGlobalDimensions(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config WITHOUT global dimensions
	mainConfig := `
server:
  listen: ":10070"
  timeout: 120s
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key"
redis:
  addr: "localhost:6379"
  db: 0
storage:
  base_path: "/tmp/cache"
render:
  cache:
    ttl: 1h
bypass:
  user_agent: "EdgeComet"
  cache:
    enabled: false
log:
  level: "info"
  console:
    enabled: false
metrics:
  enabled: false
hosts:
  include: "hosts.d/"
`

	mainConfigPath := filepath.Join(tempDir, "edge-gateway.yaml")
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0644))

	// Create hosts directory with host that has its own dimensions
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0755))

	hostConfig := `
hosts:
  - id: 1
    domain: "nodims.com"
    render_key: "test-key"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
          match_ua: ["*Googlebot*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify global dimensions are nil/empty
	cfg := cm.GetConfig()
	assert.Len(t, cfg.Render.Dimensions, 0)

	// Verify host has its own dimensions
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Len(t, host.Render.Dimensions, 1)
}

func TestCompileGlobalDimensions_InvalidPattern(t *testing.T) {
	logger := zap.NewNop()
	cm := &EGConfigManager{
		logger: logger,
		config: &configtypes.EgConfig{
			Render: configtypes.GlobalRenderConfig{
				Dimensions: map[string]types.Dimension{
					"invalid": {
						ID:       1,
						Width:    1920,
						Height:   1080,
						RenderUA: "Test",
						MatchUA:  []string{"~[invalid"},
					},
				},
			},
		},
	}

	err := cm.compileGlobalDimensions()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "global dimension 'invalid'")
}

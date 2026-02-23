package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

func TestGlobalEvents_Loading(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global events
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
  events:
    wait_for: "load"
    additional_wait: 2s
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that has no events (should inherit)
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "example.com"
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
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify global events were loaded
	cfg := cm.GetConfig()
	require.NotNil(t, cfg.Render.Events)
	assert.Equal(t, "load", cfg.Render.Events.WaitFor)
	assert.NotNil(t, cfg.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(2*time.Second), time.Duration(*cfg.Render.Events.AdditionalWait))

	// Verify host inherited events
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "example.com", host.Domain)
	assert.Equal(t, "load", host.Render.Events.WaitFor)
	assert.NotNil(t, host.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(2*time.Second), time.Duration(*host.Render.Events.AdditionalWait))
}

func TestGlobalEvents_HostOverride(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global events
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
  events:
    wait_for: "networkIdle"
    additional_wait: 1s
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that has custom events
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "custom.com"
    render_key: "test-key"
    render:
      timeout: 30s
      events:
        wait_for: "DOMContentLoaded"
        additional_wait: 500ms
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
          match_ua: ["*Googlebot*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host used its own events (not inherited)
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "custom.com", host.Domain)
	assert.Equal(t, "DOMContentLoaded", host.Render.Events.WaitFor)
	assert.NotNil(t, host.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(500*time.Millisecond), time.Duration(*host.Render.Events.AdditionalWait))
}

func TestGlobalEvents_EmptyInherits(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global events
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
  events:
    wait_for: "networkAlmostIdle"
    additional_wait: 3s
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that has empty events
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "inherit.com"
    render_key: "test-key"
    render:
      timeout: 30s
      events: {}
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
          match_ua: ["*Googlebot*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host inherited global events (empty = not set)
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "inherit.com", host.Domain)
	assert.Equal(t, "networkAlmostIdle", host.Render.Events.WaitFor)
	assert.NotNil(t, host.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(3*time.Second), time.Duration(*host.Render.Events.AdditionalWait))
}

func TestGlobalEvents_DefaultValues(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config WITHOUT global events
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
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that has no events
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "example.com"
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
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify defaults were applied
	cfg := cm.GetConfig()
	assert.Equal(t, types.LifecycleEventNetworkIdle, cfg.Render.Events.WaitFor)
	assert.NotNil(t, cfg.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(0), time.Duration(*cfg.Render.Events.AdditionalWait))

	// Verify host inherited defaults
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, types.LifecycleEventNetworkIdle, host.Render.Events.WaitFor)
	assert.NotNil(t, host.Render.Events.AdditionalWait)
	assert.Equal(t, time.Duration(0), time.Duration(*host.Render.Events.AdditionalWait))
}

func TestGlobalEvents_PartialHostOverride(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global events
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
  events:
    wait_for: "networkIdle"
    additional_wait: 2s
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that overrides only wait_for
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "partial.com"
    render_key: "test-key"
    render:
      timeout: 30s
      events:
        wait_for: "load"
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
          match_ua: ["*Googlebot*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host overrode wait_for but NOT additional_wait
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "partial.com", host.Domain)

	// Verify partial override with field-level merge
	assert.Equal(t, "load", host.Render.Events.WaitFor) // Overridden by host
	assert.NotNil(t, host.Render.Events.AdditionalWait) // Inherited from global
	assert.Equal(t, time.Duration(2*time.Second),
		time.Duration(*host.Render.Events.AdditionalWait)) // Inherited value
}

func TestGlobalEvents_PartialOverrideAdditionalWait(t *testing.T) {
	tempDir := t.TempDir()

	// Create main config with global events
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
  events:
    wait_for: "networkIdle"
    additional_wait: 3s
  dimensions:
    desktop:
      id: 1
      width: 1920
      height: 1080
      render_ua: "Mozilla/5.0"
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
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0o644))

	// Create hosts directory with host that overrides only additional_wait
	hostsDir := filepath.Join(tempDir, "hosts.d")
	require.NoError(t, os.MkdirAll(hostsDir, 0o755))

	hostConfig := `
hosts:
  - id: 1
    domain: "partial2.com"
    render_key: "test-key"
    render:
      timeout: 30s
      events:
        additional_wait: 500ms
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
          match_ua: ["*Googlebot*"]
`

	hostConfigPath := filepath.Join(hostsDir, "01-host.yaml")
	require.NoError(t, os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644))

	// Load config
	logger := zap.NewNop()
	cm, err := NewEGConfigManager(mainConfigPath, logger)
	require.NoError(t, err)

	// Verify host overrode additional_wait but inherited wait_for
	hosts := cm.GetHosts()
	require.Len(t, hosts, 1)

	host := hosts[0]
	assert.Equal(t, "partial2.com", host.Domain)

	// Verify partial override with field-level merge (opposite direction)
	assert.Equal(t, "networkIdle", host.Render.Events.WaitFor) // Inherited from global
	assert.NotNil(t, host.Render.Events.AdditionalWait)        // Overridden by host
	assert.Equal(t, time.Duration(500*time.Millisecond),
		time.Duration(*host.Render.Events.AdditionalWait)) // Overridden value
}

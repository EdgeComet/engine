package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/edgecomet/engine/pkg/types"
)

func TestSelectionStrategyDefault(t *testing.T) {
	tempDir := t.TempDir()

	// Config WITHOUT selection_strategy specified
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
  # selection_strategy not specified - should default to "least_loaded"

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

	require.NoError(t, err)
	require.NotNil(t, cm)

	// Verify default was applied
	config := cm.GetConfig()
	assert.Equal(t, types.SelectionStrategyLeastLoaded, config.Registry.SelectionStrategy,
		"selection_strategy should default to 'least_loaded' when not specified")
}

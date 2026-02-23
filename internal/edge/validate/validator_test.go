package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func TestValidateConfiguration_ValidConfig(t *testing.T) {
	// Test with valid test config
	result, err := ValidateConfiguration("../../../tests/integration/fixtures/validator-test/valid_config.yaml")
	require.NoError(t, err)
	assert.True(t, result.Valid, "Expected configuration to be valid")
	assert.Empty(t, result.Errors)
}

func TestValidateConfiguration_InvalidConfig(t *testing.T) {
	// Test with invalid config
	result, err := ValidateConfiguration("../../../tests/integration/fixtures/validator-test/invalid_config.yaml")
	require.NoError(t, err)
	assert.False(t, result.Valid, "Expected configuration to be invalid")
	assert.NotEmpty(t, result.Errors)

	// Check that we collected multiple errors
	assert.Greater(t, len(result.Errors), 2, "Expected multiple validation errors")

	// Check specific errors
	errorMessages := make(map[string]bool)
	for _, e := range result.Errors {
		errorMessages[e.Message] = true
	}

	assert.Contains(t, errorMessages, "invalid server.listen: port must be between 1 and 65535, got 70000")
	assert.Contains(t, errorMessages, "redis.addr is required")
}

func TestValidateConfiguration_FileNotFound(t *testing.T) {
	_, err := ValidateConfiguration("/nonexistent/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestErrorCollector(t *testing.T) {
	collector := NewErrorCollector()

	assert.False(t, collector.HasErrors())
	assert.Equal(t, 0, collector.Count())

	collector.Add("test.yaml", 10, "error message %d", 1)
	assert.True(t, collector.HasErrors())
	assert.Equal(t, 1, collector.Count())

	collector.Add("test.yaml", 20, "another error")
	assert.Equal(t, 2, collector.Count())

	errors := collector.Errors()
	assert.Len(t, errors, 2)
	assert.Equal(t, "test.yaml", errors[0].File)
	assert.Equal(t, 10, errors[0].Line)
	assert.Contains(t, errors[0].Message, "error message 1")
}

func TestValidateConfiguration_WithInvalidYAML(t *testing.T) {
	// Create a temp file with invalid YAML
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write invalid YAML (unclosed bracket)
	err := os.WriteFile(configPath, []byte(`server:
  listen: ":8080"
  invalid: [unclosed
`), 0o644)
	require.NoError(t, err)

	// Create hosts.d directory with minimal valid host config
	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// Should have YAML syntax error
	found := false
	for _, e := range result.Errors {
		if e.Message != "" {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected YAML syntax error")
}

func TestValidateConfiguration_NegativeValues(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Config with negative values
	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 60s

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "cache/"

render:
  cache:
    ttl: -1h

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Create hosts.d directory
	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	require.NoError(t, err)
	assert.False(t, result.Valid)

	// Check for negative value errors
	errorMessages := make([]string, 0)
	for _, e := range result.Errors {
		errorMessages = append(errorMessages, e.Message)
	}

	assert.Contains(t, errorMessages, "render.cache.ttl cannot be negative")
}

func TestValidateConfiguration_InvalidPort(t *testing.T) {
	tests := []struct {
		name           string
		port           int
		wantErr        bool
		errorSubstring string
	}{
		{"zero port", 0, true, "port must be between 1 and 65535, got 0"},
		{"negative port", -1, true, "port must be between 1 and 65535, got -1"},
		{"max valid port", 65535, false, ""},
		{"too large port", 65536, true, "port must be between 1 and 65535, got 65536"},
		{"normal port", 8080, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			configContent := fmt.Sprintf(`
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":%d"
  timeout: 60s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`, tt.port)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.errorSubstring) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing: %s, got %d errors", tt.errorSubstring, len(result.Errors))
			}
		})
	}
}

func TestValidateHosts_EmptyHosts(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write minimal valid main config
	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 60s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Create hosts.d directory
	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts: []`), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	// Empty hosts file returns error from validator (not validation error)
	if err != nil {
		assert.Contains(t, err.Error(), "no hosts loaded", "Expected 'no hosts loaded' error")
		return
	}

	// Or validation might succeed but find no hosts configured
	assert.False(t, result.Valid)
	found := false
	for _, e := range result.Errors {
		if e.Message == "no hosts configured" {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected 'no hosts configured' error")
}

func TestValidateHosts_HostIDValidation(t *testing.T) {
	tests := []struct {
		name          string
		hostsContent  string
		wantErr       bool
		expectedError string
	}{
		{
			name: "valid unique positive IDs",
			hostsContent: `
hosts:
  - id: 1
    domain: "example1.com"
    render_key: "key-1"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
  - id: 2
    domain: "example2.com"
    render_key: "key-2"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`,
			wantErr: false,
		},
		{
			name: "zero host ID",
			hostsContent: `
hosts:
  - id: 0
    domain: "example.com"
    render_key: "key-1"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`,
			wantErr:       true,
			expectedError: "id must be positive (got 0)",
		},
		{
			name: "negative host ID",
			hostsContent: `
hosts:
  - id: -1
    domain: "example.com"
    render_key: "key-1"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`,
			wantErr:       true,
			expectedError: "id must be positive (got -1)",
		},
		{
			name: "duplicate host IDs",
			hostsContent: `
hosts:
  - id: 1
    domain: "example1.com"
    render_key: "key-1"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
  - id: 1
    domain: "example2.com"
    render_key: "key-2"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`,
			wantErr:       true,
			expectedError: "duplicate host id 1",
		},
		{
			name: "missing ID field defaults to zero",
			hostsContent: `
hosts:
  - domain: "example.com"
    render_key: "key-1"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`,
			wantErr:       true,
			expectedError: "id must be positive (got 0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(tt.hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidatePatternSyntax(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
		errMsg  string
	}{
		// Valid patterns
		{"match-all wildcard", "*", false, ""},
		{"simple wildcard", "/blog/*", false, ""},
		{"extension wildcard", "*.pdf", false, ""},
		{"exact match", "/exact/path", false, ""},
		{"valid case-sensitive regexp", "~/api/v[0-9]+", false, ""},
		{"valid case-insensitive regexp", "~*/\\.(jpg|png|gif)$", false, ""},
		{"complex regexp", "~/blog/[0-9]{4}/[0-9]{2}/.*", false, ""},

		// Invalid patterns - consecutive wildcards
		{"consecutive wildcards", "/path/**", true, "consecutive wildcards"},
		{"triple wildcards", "***", true, "consecutive wildcards"},

		// Invalid patterns - empty regexp
		{"empty case-sensitive regexp", "~", true, "empty"},
		{"empty case-insensitive regexp", "~*", true, "empty"},

		// Invalid patterns - malformed regexp
		{"unclosed bracket case-sensitive", "~/[unclosed", true, "case-sensitive regexp"},
		{"unclosed bracket case-insensitive", "~*[unclosed", true, "case-insensitive regexp"},
		{"unclosed paren case-sensitive", "~/(unclosed", true, "case-sensitive regexp"},
		{"unclosed paren case-insensitive", "~*(unclosed", true, "case-insensitive regexp"},
		{"invalid backreference", "~/\\k<invalid>", true, "case-sensitive regexp"},
		{"invalid escape sequence", "~/\\xZZ", true, "case-sensitive regexp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePatternSyntax(tt.pattern, "test")
			if tt.wantErr {
				require.Error(t, err, "Expected error for pattern: %s", tt.pattern)
				assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain: %s", tt.errMsg)
			} else {
				require.NoError(t, err, "Expected no error for pattern: %s", tt.pattern)
			}
		})
	}
}

func TestValidateConfiguration_InvalidRegexpPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write minimal valid main config
	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 60s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	// Hosts config with invalid regexp pattern
	hostsContent := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
    url_rules:
      - match: "~/api/v[unclosed"
        action: "render"
`

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Create hosts.d directory
	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	// Validator might return error if YAML is completely invalid
	// Or it might return validation errors in result
	if err != nil {
		// Validator returned error - check if it's the expected regexp error type
		assert.Contains(t, err.Error(), "no hosts loaded", "Expected error about hosts not loaded or regexp validation")
		return
	}

	// Otherwise validation should have collected errors
	require.NoError(t, err)
	assert.False(t, result.Valid)

	// Check for regexp validation error
	// Error can come from either YAML unmarshaling or pattern validation
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "invalid regexp pattern") && strings.Contains(e.Message, "[unclosed") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected invalid regexp pattern error, got errors: %v", result.Errors)
}

func TestValidateConfiguration_InvalidDimensionUserAgentPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 60s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	hostsContent := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      dimensions:
        mobile:
          id: 1
          width: 375
          height: 812
          render_ua: "Mozilla/5.0"
          match_ua:
            - "~(Mobile"
    url_rules:
      - match: "/api/*"
        action: "render"
`

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	if err != nil {
		assert.Contains(t, err.Error(), "no hosts loaded")
		return
	}

	require.NoError(t, err)
	assert.False(t, result.Valid)

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "invalid match_ua pattern") && strings.Contains(e.Message, "~(Mobile") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected invalid dimension match_ua pattern error, got errors: %v", result.Errors)
}

func TestValidateConfiguration_InvalidBothitRecacheUserAgentPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 60s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	hostsContent := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
    bothit_recache:
      enabled: true
      interval: 1h
      match_ua:
        - "~[a-"
    url_rules:
      - match: "/api/*"
        action: "render"
`

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	if err != nil {
		assert.Contains(t, err.Error(), "no hosts loaded")
		return
	}

	require.NoError(t, err)
	assert.False(t, result.Valid)

	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "bothit_recache") && strings.Contains(e.Message, "invalid match_ua pattern") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected invalid bothit_recache match_ua pattern error, got errors: %v", result.Errors)
}

func TestValidateConfiguration_ValidRegexpPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write minimal valid main config
	configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

	// Hosts config with valid regexp patterns
	hostsContent := `
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
    url_rules:
      - match:
          - "~/api/v[0-9]+"
          - "~*/\\.(jpg|png|gif)$"
          - "/blog/*"
        action: "render"
`

	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Create hosts.d directory
	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	require.NoError(t, err)
	assert.True(t, result.Valid, "Expected configuration to be valid, got errors: %v", result.Errors)
	assert.Empty(t, result.Errors)
}

func TestValidateConfiguration_UnmatchedDimension(t *testing.T) {
	tests := []struct {
		name              string
		unmatchedDimValue string
		wantErr           bool
		expectedError     string
	}{
		{
			name:              "valid block constant",
			unmatchedDimValue: `unmatched_dimension: "block"`,
			wantErr:           false,
		},
		{
			name:              "valid bypass constant",
			unmatchedDimValue: `unmatched_dimension: "bypass"`,
			wantErr:           false,
		},
		{
			name:              "valid dimension name",
			unmatchedDimValue: `unmatched_dimension: "desktop"`,
			wantErr:           false,
		},
		{
			name:              "valid dimension name - mobile",
			unmatchedDimValue: `unmatched_dimension: "mobile"`,
			wantErr:           false,
		},
		{
			name:              "empty value (will be defaulted)",
			unmatchedDimValue: `# unmatched_dimension not specified`,
			wantErr:           false,
		},
		{
			name:              "invalid constant value",
			unmatchedDimValue: `unmatched_dimension: "invalid"`,
			wantErr:           true,
			expectedError:     "unmatched_dimension 'invalid' is invalid",
		},
		{
			name:              "nonexistent dimension name",
			unmatchedDimValue: `unmatched_dimension: "tablet"`,
			wantErr:           true,
			expectedError:     "unmatched_dimension 'tablet' is invalid",
		},
		{
			name:              "typo in block",
			unmatchedDimValue: `unmatched_dimension: "blcok"`,
			wantErr:           true,
			expectedError:     "unmatched_dimension 'blcok' is invalid",
		},
		{
			name:              "typo in bypass",
			unmatchedDimValue: `unmatched_dimension: "bypas"`,
			wantErr:           true,
			expectedError:     "unmatched_dimension 'bypas' is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			// Hosts config with unmatched_dimension value
			hostsContent := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      %s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
        mobile:
          id: 2
          width: 375
          height: 667
          render_ua: "Mobile/5.0"
`, tt.unmatchedDimValue)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_UnmatchedDimensionEmptyMap(t *testing.T) {
	tests := []struct {
		name              string
		unmatchedDimValue string
		dimensionsConfig  string
		wantErr           bool
		expectedError     string
	}{
		{
			name:              "empty dimensions map with dimension reference",
			unmatchedDimValue: `unmatched_dimension: "desktop"`,
			dimensionsConfig:  `dimensions: {}`,
			wantErr:           true,
			expectedError:     "at least one dimension must be configured",
		},
		{
			name:              "empty dimensions map with block constant",
			unmatchedDimValue: `unmatched_dimension: "block"`,
			dimensionsConfig:  `dimensions: {}`,
			wantErr:           true,
			expectedError:     "at least one dimension must be configured",
		},
		{
			name:              "empty dimensions map with bypass constant",
			unmatchedDimValue: `unmatched_dimension: "bypass"`,
			dimensionsConfig:  `dimensions: {}`,
			wantErr:           true,
			expectedError:     "at least one dimension must be configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			// Hosts config with empty dimensions map and unmatched_dimension value
			hostsContent := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      %s
      %s
`, tt.unmatchedDimValue, tt.dimensionsConfig)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_RenderEvents(t *testing.T) {
	tests := []struct {
		name          string
		eventsConfig  string
		wantErr       bool
		expectedError string
	}{
		// Valid wait_for values
		{
			name: "valid DOMContentLoaded",
			eventsConfig: `
      events:
        wait_for: "DOMContentLoaded"`,
			wantErr: false,
		},
		{
			name: "valid load",
			eventsConfig: `
      events:
        wait_for: "load"`,
			wantErr: false,
		},
		{
			name: "valid networkIdle",
			eventsConfig: `
      events:
        wait_for: "networkIdle"`,
			wantErr: false,
		},
		{
			name: "valid networkAlmostIdle",
			eventsConfig: `
      events:
        wait_for: "networkAlmostIdle"`,
			wantErr: false,
		},
		// Invalid wait_for values
		{
			name: "invalid networkidle0 (old Puppeteer value)",
			eventsConfig: `
      events:
        wait_for: "networkidle0"`,
			wantErr:       true,
			expectedError: "events.wait_for 'networkidle0' is invalid",
		},
		{
			name: "invalid networkidle2 (old Puppeteer value)",
			eventsConfig: `
      events:
        wait_for: "networkidle2"`,
			wantErr:       true,
			expectedError: "events.wait_for 'networkidle2' is invalid",
		},
		{
			name: "invalid typo in DOMContentLoaded",
			eventsConfig: `
      events:
        wait_for: "DomContentLoaded"`,
			wantErr:       true,
			expectedError: "events.wait_for 'DomContentLoaded' is invalid",
		},
		{
			name: "invalid random value",
			eventsConfig: `
      events:
        wait_for: "ready"`,
			wantErr:       true,
			expectedError: "events.wait_for 'ready' is invalid",
		},
		// Valid additional_wait values
		{
			name: "valid additional_wait 1s",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: 1s`,
			wantErr: false,
		},
		{
			name: "valid additional_wait 30s (max)",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: 30s`,
			wantErr: false,
		},
		{
			name: "valid additional_wait 500ms",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: 500ms`,
			wantErr: false,
		},
		// Invalid additional_wait values
		{
			name: "negative additional_wait",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: -1s`,
			wantErr:       true,
			expectedError: "events.additional_wait cannot be negative",
		},
		{
			name: "additional_wait exceeds 30s",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: 31s`,
			wantErr:       true,
			expectedError: "events.additional_wait cannot exceed 30s",
		},
		{
			name: "additional_wait 1 minute (60s)",
			eventsConfig: `
      events:
        wait_for: "networkIdle"
        additional_wait: 1m`,
			wantErr:       true,
			expectedError: "events.additional_wait cannot exceed 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			// Hosts config with events configuration
			hostsContent := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s%s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`, tt.eventsConfig)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_RenderEventsInURLRule(t *testing.T) {
	tests := []struct {
		name          string
		eventsConfig  string
		wantErr       bool
		expectedError string
	}{
		{
			name: "valid events override in URL rule",
			eventsConfig: `
          events:
            wait_for: "DOMContentLoaded"
            additional_wait: 2s`,
			wantErr: false,
		},
		{
			name: "invalid wait_for in URL rule",
			eventsConfig: `
          events:
            wait_for: "domready"`,
			wantErr:       true,
			expectedError: "url_rules[0]: events.wait_for 'domready' is invalid",
		},
		{
			name: "additional_wait exceeds limit in URL rule",
			eventsConfig: `
          events:
            wait_for: "load"
            additional_wait: 45s`,
			wantErr:       true,
			expectedError: "url_rules[0]: events.additional_wait cannot exceed 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			// Hosts config with URL rule events override
			hostsContent := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s
      events:
        wait_for: "networkIdle"
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
    url_rules:
      - match: "/special/*"
        action: "render"
        render:%s
`, tt.eventsConfig)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_HostRenderCache(t *testing.T) {
	tests := []struct {
		name          string
		cacheConfig   string
		wantErr       bool
		expectedError string
	}{
		// Valid configurations
		{
			name: "valid positive default_ttl",
			cacheConfig: `
      cache:
        ttl: 1h`,
			wantErr: false,
		},
		{
			name: "valid cache with expired config",
			cacheConfig: `
      cache:
        ttl: 30m
        expired:
          strategy: "serve_stale"
          stale_ttl: 10m`,
			wantErr: false,
		},
		{
			name: "valid complete cache configuration",
			cacheConfig: `
      cache:
        ttl: 2h
        expired:
          strategy: "delete"
          stale_ttl: 15m`,
			wantErr: false,
		},
		// Invalid default_ttl
		{
			name: "negative default_ttl",
			cacheConfig: `
      cache:
        ttl: -1h`,
			wantErr:       true,
			expectedError: "render.cache.ttl cannot be negative",
		},
		// Invalid expired.strategy
		{
			name: "invalid expired strategy",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "invalid_strategy"`,
			wantErr:       true,
			expectedError: "invalid expired.strategy 'invalid_strategy'",
		},
		// Invalid expired.stale_ttl
		{
			name: "negative stale_ttl",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "serve_stale"
          stale_ttl: -5m`,
			wantErr:       true,
			expectedError: "stale_ttl must be positive when strategy is 'serve_stale'",
		},
		// Duplicate test removed (only one stale field now)
		{
			name: "negative stale_ttl with different value",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "serve_stale"
          stale_ttl: -10m`,
			wantErr:       true,
			expectedError: "stale_ttl must be positive when strategy is 'serve_stale'",
		},
		// Missing stale_ttl when strategy is serve_stale
		{
			name: "missing stale_ttl with serve_stale strategy",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "serve_stale"`,
			wantErr:       true,
			expectedError: "stale_ttl is required when strategy is 'serve_stale'",
		},
		// Zero stale_ttl when strategy is serve_stale
		{
			name: "zero stale_ttl with serve_stale strategy",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "serve_stale"
          stale_ttl: 0m`,
			wantErr:       true,
			expectedError: "stale_ttl must be positive when strategy is 'serve_stale'",
		},
		// Missing stale_ttl with delete strategy (should be valid)
		{
			name: "missing stale_ttl with delete strategy",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "delete"`,
			wantErr: false,
		},
		// stale_ttl provided with delete strategy (should be valid, just ignored)
		{
			name: "stale_ttl with delete strategy",
			cacheConfig: `
      cache:
        ttl: 1h
        expired:
          strategy: "delete"
          stale_ttl: 10m`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write minimal valid main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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

			// Hosts config with render cache configuration
			hostsContent := fmt.Sprintf(`
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    render:
      timeout: 30s%s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`, tt.cacheConfig)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_RegistrySelectionStrategy(t *testing.T) {
	tests := []struct {
		name              string
		selectionStrategy string
		wantErr           bool
		expectedError     string
	}{
		// Valid strategies
		{
			name:              "valid least_loaded strategy",
			selectionStrategy: "least_loaded",
			wantErr:           false,
		},
		{
			name:              "valid most_available strategy",
			selectionStrategy: "most_available",
			wantErr:           false,
		},
		// Invalid strategies
		{
			name:              "invalid round_robin strategy (not implemented)",
			selectionStrategy: "round_robin",
			wantErr:           true,
			expectedError:     "invalid registry.selection_strategy 'round_robin'",
		},
		{
			name:              "invalid random strategy (not implemented)",
			selectionStrategy: "random",
			wantErr:           true,
			expectedError:     "invalid registry.selection_strategy 'random'",
		},
		{
			name:              "invalid strategy value",
			selectionStrategy: "invalid_strategy",
			wantErr:           true,
			expectedError:     "invalid registry.selection_strategy 'invalid_strategy'",
		},
		{
			name:              "empty strategy value",
			selectionStrategy: "",
			wantErr:           false, // Empty is allowed, will use default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write main config with specific selection_strategy
			configContent := fmt.Sprintf(`
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "%s"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`, tt.selectionStrategy)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory with minimal valid host config
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateConfiguration_TimeoutWarnings(t *testing.T) {
	tests := []struct {
		name              string
		serverTimeout     string
		bypassTimeout     string
		hostRenderTimeout string
		expectWarnings    []string
		expectErrors      []string
	}{
		{
			name:              "all timeouts in good range",
			serverTimeout:     "120s",
			bypassTimeout:     "30s",
			hostRenderTimeout: "30s",
			expectWarnings:    []string{},
			expectErrors:      []string{},
		},
		{
			name:              "low server timeout",
			serverTimeout:     "30s",
			bypassTimeout:     "10s",
			hostRenderTimeout: "10s",
			expectWarnings:    []string{"server.timeout (30s) is low"},
			expectErrors:      []string{},
		},
		{
			name:              "high server timeout",
			serverTimeout:     "400s",
			bypassTimeout:     "30s",
			hostRenderTimeout: "30s",
			expectWarnings:    []string{"server.timeout (6m40s) is very high"},
			expectErrors:      []string{},
		},
		{
			name:              "low bypass timeout",
			serverTimeout:     "120s",
			bypassTimeout:     "2s",
			hostRenderTimeout: "30s",
			expectWarnings:    []string{"bypass.timeout (2s) is low"},
			expectErrors:      []string{},
		},
		{
			name:              "high bypass timeout",
			serverTimeout:     "120s",
			bypassTimeout:     "90s",
			hostRenderTimeout: "30s",
			expectWarnings:    []string{"bypass.timeout (1m30s) is high"},
			expectErrors:      []string{},
		},
		{
			name:              "critically low render timeout - should error",
			serverTimeout:     "120s",
			bypassTimeout:     "30s",
			hostRenderTimeout: "3s",
			expectWarnings:    []string{},
			expectErrors:      []string{"render.timeout (3s) is too low"},
		},
		{
			name:              "low render timeout - should warn",
			serverTimeout:     "120s",
			bypassTimeout:     "30s",
			hostRenderTimeout: "7s",
			expectWarnings:    []string{"render.timeout (7s) is very low"},
			expectErrors:      []string{},
		},
		{
			name:              "very high render timeout",
			serverTimeout:     "240s",
			bypassTimeout:     "30s",
			hostRenderTimeout: "150s",
			expectWarnings:    []string{"render.timeout (2m30s) is very high"},
			expectErrors:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write main config
			configContent := fmt.Sprintf(`
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: %s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: %s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`, tt.serverTimeout, tt.bypassTimeout)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Write hosts config
			hostsContent := fmt.Sprintf(`hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: %s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"`, tt.hostRenderTimeout)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			// Check errors
			if len(tt.expectErrors) > 0 {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				for _, expectedErr := range tt.expectErrors {
					found := false
					for _, e := range result.Errors {
						if strings.Contains(e.Message, expectedErr) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected error containing '%s', got errors: %v", expectedErr, result.Errors)
				}
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}

			// Check warnings
			if len(tt.expectWarnings) > 0 {
				for _, expectedWarn := range tt.expectWarnings {
					found := false
					for _, w := range result.Warnings {
						if strings.Contains(w.Message, expectedWarn) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected warning containing '%s', got warnings: %v", expectedWarn, result.Warnings)
				}
			} else {
				assert.Empty(t, result.Warnings, "Expected no warnings for test: %s, got: %v", tt.name, result.Warnings)
			}
		})
	}
}

func TestValidateConfiguration_StaleTTLWarnings(t *testing.T) {
	tests := []struct {
		name           string
		staleTTL       string
		expectWarnings []string
		expectErrors   []string
	}{
		{
			name:           "very short stale_ttl (30m)",
			staleTTL:       "30m",
			expectWarnings: []string{"stale_ttl is very short (30m0s), recommended minimum is 1h0m0s"},
			expectErrors:   []string{},
		},
		{
			name:           "minimum acceptable stale_ttl (1h)",
			staleTTL:       "1h",
			expectWarnings: []string{},
			expectErrors:   []string{},
		},
		{
			name:           "moderate stale_ttl (24h)",
			staleTTL:       "24h",
			expectWarnings: []string{},
			expectErrors:   []string{},
		},
		{
			name:           "maximum acceptable stale_ttl (7d)",
			staleTTL:       "168h",
			expectWarnings: []string{},
			expectErrors:   []string{},
		},
		{
			name:           "very large stale_ttl (10d)",
			staleTTL:       "240h",
			expectWarnings: []string{"stale_ttl is very large (240h0m0s), recommended maximum is 168h0m0s"},
			expectErrors:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write main config
			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
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
			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)

			// Write hosts config with stale_ttl
			hostsContent := fmt.Sprintf(`hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
      cache:
        ttl: 1h
        expired:
          strategy: "serve_stale"
          stale_ttl: %s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"`, tt.staleTTL)

			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			// Check errors
			if len(tt.expectErrors) > 0 {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				for _, expectedErr := range tt.expectErrors {
					found := false
					for _, e := range result.Errors {
						if strings.Contains(e.Message, expectedErr) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected error containing '%s', got errors: %v", expectedErr, result.Errors)
				}
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}

			// Check warnings
			if len(tt.expectWarnings) > 0 {
				for _, expectedWarn := range tt.expectWarnings {
					found := false
					for _, w := range result.Warnings {
						if strings.Contains(w.Message, expectedWarn) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected warning containing '%s', got warnings: %v", expectedWarn, result.Warnings)
				}
			} else {
				assert.Empty(t, result.Warnings, "Expected no warnings for test: %s, got: %v", tt.name, result.Warnings)
			}
		})
	}
}

func TestValidateConfiguration_LogConfig(t *testing.T) {
	tests := []struct {
		name          string
		logLevel      string
		logFormat     string
		wantErr       bool
		expectedError string
	}{
		// Valid log levels
		{"valid debug level", "debug", "console", false, ""},
		{"valid info level", "info", "console", false, ""},
		{"valid warn level", "warn", "console", false, ""},
		{"valid error level", "error", "console", false, ""},
		{"valid dpanic level", "dpanic", "json", false, ""},
		{"valid panic level", "panic", "json", false, ""},
		{"valid fatal level", "fatal", "json", false, ""},

		// Valid log formats
		{"valid json format", "info", "json", false, ""},
		{"valid console format", "info", "console", false, ""},

		// Invalid log levels
		{"invalid trace level", "trace", "console", true, "invalid log.level 'trace'"},
		{"invalid debugging level", "debugging", "console", true, "invalid log.level 'debugging'"},
		{"invalid verbose level", "verbose", "console", true, "invalid log.level 'verbose'"},

		// Invalid log formats
		{"invalid xml format", "info", "xml", true, "invalid log.console.format 'xml'"},
		{"invalid text format", "info", "text", true, "invalid log.console.format 'text'"},
		{"invalid yaml format", "info", "yaml", true, "invalid log.console.format 'yaml'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			configContent := fmt.Sprintf(`
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache: {}

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

registry:
  selection_strategy: "least_loaded"

log:
  level: "%s"
  console:
    enabled: true
    format: "%s"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`, tt.logLevel, tt.logFormat)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Create hosts.d directory
			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid for test: %s", tt.name)
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectedError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing '%s', got errors: %v", tt.expectedError, result.Errors)
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid for test: %s, got errors: %v", tt.name, result.Errors)
			}
		})
	}
}

func TestValidateHTTPHeaderName(t *testing.T) {
	tests := []struct {
		name        string
		headerName  string
		expectError bool
		errorSubstr string
	}{
		// Valid header names
		{name: "valid header - Content-Type", headerName: "Content-Type", expectError: false},
		{name: "valid header - Cache-Control", headerName: "Cache-Control", expectError: false},
		{name: "valid header - X-Custom-Header", headerName: "X-Custom-Header", expectError: false},
		{name: "valid header - Accept", headerName: "Accept", expectError: false},
		{name: "valid header - ETag", headerName: "ETag", expectError: false},
		{name: "valid header with underscore", headerName: "Cache_Control", expectError: false},
		{name: "valid header with dot", headerName: "X-Custom.Header", expectError: false},
		{name: "valid header with exclamation", headerName: "X!Header", expectError: false},
		{name: "valid header with multiple special chars", headerName: "X-Custom_Header.v1!test", expectError: false},
		{name: "valid header with tilde", headerName: "X~Custom", expectError: false},
		{name: "valid header with caret", headerName: "X^Custom", expectError: false},
		{name: "valid header with pipe", headerName: "X|Custom", expectError: false},
		{name: "valid header with backtick", headerName: "X`Custom", expectError: false},
		{name: "valid header with hash", headerName: "X#Custom", expectError: false},
		{name: "valid header with dollar sign", headerName: "X$Custom", expectError: false},
		{name: "valid header with percent", headerName: "X%Custom", expectError: false},
		{name: "valid header with ampersand", headerName: "X&Custom", expectError: false},
		{name: "valid header with single quote", headerName: "X'Custom", expectError: false},
		{name: "valid header with asterisk", headerName: "X*Custom", expectError: false},
		{name: "valid header with plus", headerName: "X+Custom", expectError: false},

		// Invalid header names
		{name: "empty string", headerName: "", expectError: true, errorSubstr: "header name cannot be empty"},
		{name: "contains space", headerName: "Content Type", expectError: true, errorSubstr: "contains invalid space"},
		{name: "leading space", headerName: " Content-Type", expectError: true, errorSubstr: "contains invalid space"},
		{name: "trailing space", headerName: "Content-Type ", expectError: true, errorSubstr: "contains invalid space"},
		{name: "contains colon", headerName: "Content:Type", expectError: true, errorSubstr: "contains invalid colon"},
		{name: "trailing colon", headerName: "Content-Type:", expectError: true, errorSubstr: "contains invalid colon"},
		{name: "contains newline", headerName: "Content\nType", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "contains carriage return", headerName: "Content\rType", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "contains tab", headerName: "Content\tType", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "contains null byte", headerName: "Content\x00Type", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "contains DEL character", headerName: "Content\x7FType", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "contains @ symbol", headerName: "Content@Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains [ bracket", headerName: "Content[Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains ] bracket", headerName: "Content]Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains ( paren", headerName: "Content(Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains ) paren", headerName: "Content)Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains comma", headerName: "Content,Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains semicolon", headerName: "Content;Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains less than", headerName: "Content<Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains greater than", headerName: "Content>Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains equals", headerName: "Content=Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains question mark", headerName: "Content?Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains slash", headerName: "Content/Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains backslash", headerName: "Content\\Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains curly brace", headerName: "Content{Type", expectError: true, errorSubstr: "contains invalid character"},
		{name: "contains double quote", headerName: "Content\"Type", expectError: true, errorSubstr: "contains invalid character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTTPHeaderName(tt.headerName)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstr != "" {
					assert.Contains(t, err.Error(), tt.errorSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSafeHeadersInternal(t *testing.T) {
	tests := []struct {
		name        string
		headers     []string
		level       string
		expectError bool
		errorSubstr string
	}{
		{name: "valid headers list", headers: []string{"Content-Type", "Cache-Control", "ETag"}, level: "global", expectError: false},
		{name: "empty list is valid", headers: []string{}, level: "global", expectError: false},
		{name: "nil list is valid", headers: nil, level: "global", expectError: false},
		{name: "single valid header", headers: []string{"Content-Type"}, level: "host", expectError: false},
		{name: "mixed valid and invalid - invalid at position 1", headers: []string{"Content-Type", "Invalid Header", "Cache-Control"}, level: "global", expectError: true, errorSubstr: "global safe_headers[1]"},
		{name: "invalid header at beginning", headers: []string{"Invalid Header", "Content-Type"}, level: "host", expectError: true, errorSubstr: "host safe_headers[0]"},
		{name: "invalid header at end", headers: []string{"Content-Type", "Cache-Control", "Invalid:Header"}, level: "pattern", expectError: true, errorSubstr: "pattern safe_headers[2]"},
		{name: "multiple invalid headers - reports first", headers: []string{"Content-Type", "Invalid Header", "Another Bad"}, level: "global", expectError: true, errorSubstr: "safe_headers[1]"},
		{name: "header with space", headers: []string{"Content-Type", "X Custom Header"}, level: "host", expectError: true, errorSubstr: "contains invalid space"},
		{name: "header with colon", headers: []string{"Content-Type", "X-Custom:Value"}, level: "global", expectError: true, errorSubstr: "contains invalid colon"},
		{name: "header with newline", headers: []string{"Content-Type", "X-Custom\nHeader"}, level: "pattern", expectError: true, errorSubstr: "contains invalid control character"},
		{name: "empty string in list", headers: []string{"Content-Type", "", "Cache-Control"}, level: "global", expectError: true, errorSubstr: "cannot be empty"},
		{name: "all valid special characters", headers: []string{"X!Header", "X#Header", "X$Header", "X%Header", "X&Header", "X'Header"}, level: "global", expectError: false},
		{name: "custom header with all valid punctuation", headers: []string{"X-Custom_Header.v1!test#2$value%end&more'quote*star+plus"}, level: "host", expectError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSafeHeadersInternal(tt.headers, tt.level)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstr != "" {
					assert.Contains(t, err.Error(), tt.errorSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsValidHTTPHeaderChar(t *testing.T) {
	validChars := []rune{'A', 'Z', 'a', 'z', 'M', 'n', '0', '9', '5', '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~'}
	for _, char := range validChars {
		t.Run(string(char), func(t *testing.T) {
			assert.True(t, isValidHTTPHeaderChar(char), "Character %q (%d) should be valid", char, char)
		})
	}

	invalidChars := []rune{'\x00', '\x1F', '\x7F', ' ', '\t', '\n', '\r', ':', '/', '\\', '"', ',', ';', '(', ')', '<', '>', '[', ']', '{', '}', '=', '?', '@'}
	for _, char := range invalidChars {
		t.Run(string(char), func(t *testing.T) {
			assert.False(t, isValidHTTPHeaderChar(char), "Character %q (%d) should be invalid", char, char)
		})
	}
}

func TestValidateStorageConfig_EmptyBasePath(t *testing.T) {
	tests := []struct {
		name      string
		basePath  string
		wantError bool
	}{
		{
			name:      "empty string",
			basePath:  "",
			wantError: true,
		},
		{
			name:      "whitespace only",
			basePath:  "   ",
			wantError: true,
		},
		{
			name:      "tabs only",
			basePath:  "\t\t",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			configContent := fmt.Sprintf(`server:
  listen: ":10070"
redis:
  addr: "localhost:6379"
storage:
  base_path: "%s"
hosts:
  include: "hosts.d/"
`, tt.basePath)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantError {
				assert.False(t, result.Valid, "Expected configuration to be invalid")
				assert.NotEmpty(t, result.Errors, "Expected validation errors")

				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, "storage.base_path is required") {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected 'storage.base_path is required' error message")
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid")
			}
		})
	}
}

func TestValidateStorageConfig_RelativePath(t *testing.T) {
	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: "./cache",
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")
	assert.True(t, filepath.IsAbs(cfg.Storage.BasePath), "Expected path to be converted to absolute, got: %s", cfg.Storage.BasePath)
}

func TestValidateStorageConfig_AbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()
	absoluteCachePath := filepath.Join(tmpDir, "absolute-cache")

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: absoluteCachePath,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")
	assert.True(t, filepath.IsAbs(cfg.Storage.BasePath), "Expected path to be absolute")
	assert.Equal(t, absoluteCachePath, cfg.Storage.BasePath, "Expected absolute path to remain unchanged")
}

func TestValidateStorageConfig_ExistingDirectory(t *testing.T) {
	existingDir := t.TempDir()

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: existingDir,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")

	fileInfo, err := os.Stat(existingDir)
	require.NoError(t, err)
	assert.True(t, fileInfo.IsDir(), "Expected directory to still exist")
}

func TestValidateStorageConfig_NonExistentDirectory_AutoCreate(t *testing.T) {
	tempDir := t.TempDir()
	newCacheDir := filepath.Join(tempDir, "cache", "storage")

	_, err := os.Stat(newCacheDir)
	require.True(t, os.IsNotExist(err), "Directory should not exist yet")

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: newCacheDir,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")

	fileInfo, err := os.Stat(newCacheDir)
	require.NoError(t, err, "Directory should have been created")
	assert.True(t, fileInfo.IsDir(), "Expected path to be a directory")
}

func TestValidateStorageConfig_PathIsFile_NotDirectory(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "notadirectory")

	err := os.WriteFile(tempFile, []byte("test"), 0o644)
	require.NoError(t, err)

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: tempFile,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.True(t, collector.HasErrors(), "Expected validation error")

	errors := collector.Errors()
	found := false
	for _, e := range errors {
		if strings.Contains(e.Message, "not a directory") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected 'not a directory' error message")
}

func TestValidateStorageConfig_WritableDirectory(t *testing.T) {
	writableDir := t.TempDir()

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: writableDir,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")

	matches, err := filepath.Glob(filepath.Join(writableDir, ".edgecomet-write-test-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "Expected no test files left behind")
}

func TestValidateStorageConfig_ReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Read-only directory test not applicable on Windows")
	}

	tempDir := t.TempDir()
	roDir := filepath.Join(tempDir, "readonly")
	require.NoError(t, os.MkdirAll(roDir, 0o755))

	require.NoError(t, os.Chmod(roDir, 0o555))
	defer os.Chmod(roDir, 0o755)

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: roDir,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.True(t, collector.HasErrors(), "Expected validation error")

	errors := collector.Errors()
	found := false
	for _, e := range errors {
		if strings.Contains(e.Message, "not writable") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected 'not writable' error message")
}

func TestValidateStorageConfig_FullFileSystemTest(t *testing.T) {
	testDir := t.TempDir()

	cfg := &configtypes.EgConfig{
		Storage: configtypes.GlobalStorageConfig{
			BasePath: testDir,
		},
	}
	collector := NewErrorCollector()

	validateStorageConfig(cfg, nil, "test.yaml", collector)

	assert.False(t, collector.HasErrors(), "Expected no validation errors")

	matches, err := filepath.Glob(filepath.Join(testDir, ".edgecomet-write-test-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "Expected all test files to be cleaned up")
}

func TestValidateConfiguration_WithStorageBasePath(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		setupFunc   func(t *testing.T, basePath string)
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid relative path",
			basePath: "./cache",
			wantErr:  false,
		},
		{
			name: "valid absolute path",
			basePath: func() string {
				tmpDir := t.TempDir()
				return filepath.Join(tmpDir, "cache")
			}(),
			wantErr: false,
		},
		{
			name:        "missing base_path",
			basePath:    "",
			wantErr:     true,
			errContains: "storage.base_path is required",
		},
		{
			name: "path is file not directory",
			basePath: func() string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "notadirectory")
				os.WriteFile(filePath, []byte("test"), 0o644)
				return filePath
			}(),
			setupFunc: func(t *testing.T, basePath string) {
			},
			wantErr:     true,
			errContains: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			if tt.setupFunc != nil {
				tt.setupFunc(t, tt.basePath)
			}

			configContent := fmt.Sprintf(`internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"
server:
  listen: ":10070"
  timeout: 120s
redis:
  addr: "localhost:6379"
storage:
  base_path: "%s"
hosts:
  include: "hosts.d/"
`, tt.basePath)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected configuration to be invalid")
				assert.NotEmpty(t, result.Errors, "Expected validation errors")

				if tt.errContains != "" {
					found := false
					for _, e := range result.Errors {
						if strings.Contains(e.Message, tt.errContains) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected error message to contain '%s'", tt.errContains)
				}
			} else {
				assert.True(t, result.Valid, "Expected configuration to be valid")
				assert.Empty(t, result.Errors, "Expected no validation errors")
			}
		})
	}
}

func TestValidateConfiguration_DurationUnitWarnings(t *testing.T) {
	tests := []struct {
		name           string
		serverTimeout  string
		bypassTimeout  string
		renderTimeout  string
		renderCacheTTL string
		bypassCacheTTL string
		staleTTL       string
		expectWarnings []string
	}{
		{
			name:           "all durations with proper units",
			serverTimeout:  "120s",
			bypassTimeout:  "30s",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{},
		},
		{
			name:           "server.timeout suspiciously small (30ns)",
			serverTimeout:  "30ns",
			bypassTimeout:  "30s",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{"server.timeout value 30ns is suspiciously small"},
		},
		{
			name:           "bypass.timeout suspiciously small (500µs)",
			serverTimeout:  "120s",
			bypassTimeout:  "500µs",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{"bypass.timeout value 500"},
		},
		{
			name:           "render.timeout suspiciously small (100ns)",
			serverTimeout:  "120s",
			bypassTimeout:  "30s",
			renderTimeout:  "100ns",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{"render.timeout value 100ns is suspiciously small"},
		},
		{
			name:           "render.cache.ttl suspiciously small (500ns)",
			serverTimeout:  "120s",
			bypassTimeout:  "30s",
			renderTimeout:  "30s",
			renderCacheTTL: "500ns",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{"render.cache.ttl value 500ns is suspiciously small"},
		},
		{
			name:           "bypass.cache.ttl suspiciously small (800ns)",
			serverTimeout:  "120s",
			bypassTimeout:  "30s",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "800ns",
			staleTTL:       "24h",
			expectWarnings: []string{"bypass.cache.ttl value 800ns is suspiciously small"},
		},
		{
			name:           "stale_ttl suspiciously small (600ns)",
			serverTimeout:  "120s",
			bypassTimeout:  "30s",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "600ns",
			expectWarnings: []string{"stale_ttl value 600ns is suspiciously small"},
		},
		{
			name:           "multiple suspiciously small durations",
			serverTimeout:  "200ns",
			bypassTimeout:  "150ns",
			renderTimeout:  "30s",
			renderCacheTTL: "1h",
			bypassCacheTTL: "30m",
			staleTTL:       "24h",
			expectWarnings: []string{
				"server.timeout value 200ns is suspiciously small",
				"bypass.timeout value 150ns is suspiciously small",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			configContent := fmt.Sprintf(`
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: %s

redis:
  addr: "localhost:6379"
  db: 0

storage:
  base_path: "cache/"

render:
  cache:
    ttl: %s
    expired:
      strategy: "serve_stale"
      stale_ttl: %s

bypass:
  timeout: %s
  user_agent: "test"
  cache:
    enabled: true
    ttl: %s
    status_codes: [200]

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "console"
  file:
    enabled: false

metrics:
  enabled: true
  listen: ":9090"
  path: "/metrics"
  namespace: "edgecomet"

hosts:
  include: "hosts.d/"
`, tt.serverTimeout, tt.renderCacheTTL, tt.staleTTL, tt.bypassTimeout, tt.bypassCacheTTL)

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			hostsContent := fmt.Sprintf(`hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: %s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"`, tt.renderTimeout)

			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if len(tt.expectWarnings) > 0 {
				for _, expectedWarn := range tt.expectWarnings {
					found := false
					for _, w := range result.Warnings {
						if strings.Contains(w.Message, expectedWarn) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected warning containing '%s', got warnings: %v", expectedWarn, result.Warnings)
				}
			} else {
				warningsWithoutOthers := []ValidationError{}
				for _, w := range result.Warnings {
					if !strings.Contains(w.Message, "is low") && !strings.Contains(w.Message, "is high") {
						warningsWithoutOthers = append(warningsWithoutOthers, w)
					}
				}
				assert.Empty(t, warningsWithoutOthers, "Expected no duration unit warnings for test: %s, got: %v", tt.name, warningsWithoutOthers)
			}
		})
	}
}

// TestValidateConfiguration_DomainsArray tests validation of Domains array
func TestValidateConfiguration_DomainsArray(t *testing.T) {
	tests := []struct {
		name        string
		domains     string // YAML for domain field
		expectValid bool
		expectError string // substring to find in error message
	}{
		{
			name:        "valid single domain",
			domains:     `domain: example.com`,
			expectValid: true,
		},
		{
			name:        "valid multiple domains",
			domains:     `domain: [example.com, www.example.com]`,
			expectValid: true,
		},
		{
			name:        "empty domains array",
			domains:     `domain: []`,
			expectValid: false,
			expectError: "domain is required",
		},
		{
			name:        "whitespace-only domain",
			domains:     `domain: ["  "]`,
			expectValid: false,
			expectError: "domain is required",
		},
		{
			name:        "domain with protocol",
			domains:     `domain: "https://example.com"`,
			expectValid: false,
			expectError: "must not contain protocol",
		},
		{
			name:        "domain with path",
			domains:     `domain: "example.com/path"`,
			expectValid: false,
			expectError: "must not contain path",
		},
		{
			name:        "domain with port",
			domains:     `domain: "example.com:8080"`,
			expectValid: false,
			expectError: "must not contain port",
		},
		{
			name:        "domain with wildcard",
			domains:     `domain: "*.example.com"`,
			expectValid: false,
			expectError: "wildcards not allowed",
		},
		{
			name:        "uppercase domain",
			domains:     `domain: "Example.COM"`,
			expectValid: false,
			expectError: "must be lowercase",
		},
		{
			name:        "duplicate within host",
			domains:     `domain: [example.com, example.com]`,
			expectValid: false,
			expectError: "duplicate domain within same host",
		},
		{
			name:        "duplicate within host case-insensitive",
			domains:     `domain: [example.com, EXAMPLE.COM]`,
			expectValid: false,
			expectError: "must be lowercase", // uppercase check fires first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			// Write main config
			configContent := `
server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"

internal:
  listen: ":10071"
  auth_key: "test-internal-auth-key-32chars!"

storage:
  base_path: "/tmp/test-cache"

render:
  cache:
    ttl: 30d

bypass:
  timeout: 30s

log:
  level: info
  console:
    enabled: true
    format: "console"

metrics:
  enabled: true
  listen: ":9090"

hosts:
  include: "hosts.d/"
`
			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			// Write host config with test domain
			hostsContent := fmt.Sprintf(`hosts:
  - id: 1
    %s
    render_key: "test-key"
    enabled: true
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`, tt.domains)

			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostsContent), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.expectValid {
				assert.True(t, result.Valid, "Expected valid config, got errors: %v", result.Errors)
			} else {
				assert.False(t, result.Valid, "Expected invalid config")
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.expectError) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

// TestValidateConfiguration_CrossHostDuplicateDomain tests duplicate domain across hosts
func TestValidateConfiguration_CrossHostDuplicateDomain(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write main config
	configContent := `
server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"

internal:
  listen: ":10071"
  auth_key: "test-internal-auth-key-32chars!"

storage:
  base_path: "/tmp/test-cache"

render:
  cache:
    ttl: 30d

bypass:
  timeout: 30s

log:
  level: info
  console:
    enabled: true

metrics:
  enabled: true
  listen: ":9090"

hosts:
  include: "hosts.d/"
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Write first host
	host1Content := `hosts:
  - id: 1
    domain: example.com
    render_key: "key1"
    enabled: true
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`
	// Write second host with same domain
	host2Content := `hosts:
  - id: 2
    domain: example.com
    render_key: "key2"
    enabled: true
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`

	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-host1.yaml"), []byte(host1Content), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "02-host2.yaml"), []byte(host2Content), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	require.NoError(t, err)

	assert.False(t, result.Valid, "Expected invalid config due to duplicate domain")
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "duplicate domain") && strings.Contains(e.Message, "already defined in") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected error about cross-host duplicate domain, got: %v", result.Errors)
}

// TestValidateConfiguration_MultiDomainCrossHostDuplicate tests duplicate in multi-domain array across hosts
func TestValidateConfiguration_MultiDomainCrossHostDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(tmpDir, "hosts.d")

	// Write main config
	configContent := `
server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"

internal:
  listen: ":10071"
  auth_key: "test-internal-auth-key-32chars!"

storage:
  base_path: "/tmp/test-cache"

render:
  cache:
    ttl: 30d

bypass:
  timeout: 30s

log:
  level: info
  console:
    enabled: true

metrics:
  enabled: true
  listen: ":9090"

hosts:
  include: "hosts.d/"
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// First host with multiple domains
	host1Content := `hosts:
  - id: 1
    domain: [example.com, www.example.com]
    render_key: "key1"
    enabled: true
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`
	// Second host with one domain that overlaps
	host2Content := `hosts:
  - id: 2
    domain: [other.com, www.example.com]
    render_key: "key2"
    enabled: true
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`

	err = os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "01-host1.yaml"), []byte(host1Content), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(hostsDir, "02-host2.yaml"), []byte(host2Content), 0o644)
	require.NoError(t, err)

	result, err := ValidateConfiguration(configPath)
	require.NoError(t, err)

	assert.False(t, result.Valid, "Expected invalid config due to duplicate domain in multi-domain array")
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "www.example.com") && strings.Contains(e.Message, "duplicate domain") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected error about www.example.com duplicate, got: %v", result.Errors)
}

func TestValidateConfiguration_EventLogging(t *testing.T) {
	tests := []struct {
		name           string
		eventLogging   string
		wantErr        bool
		errorSubstring string
	}{
		{
			name: "valid config",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: "{timestamp} {url}"
    rotation:
      max_size: 100
      max_age: 30
`,
			wantErr: false,
		},
		{
			name: "disabled config with empty fields passes",
			eventLogging: `event_logging:
  file:
    enabled: false
`,
			wantErr: false,
		},
		{
			name: "enabled with empty path fails",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: ""
    template: "{url}"
`,
			wantErr:        true,
			errorSubstring: "event_logging.file.path is required",
		},
		{
			name: "enabled with empty template passes (default applied at runtime)",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: ""
`,
			wantErr: false,
		},
		{
			name: "negative max_size fails",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: "{url}"
    rotation:
      max_size: -1
`,
			wantErr:        true,
			errorSubstring: "event_logging.file.rotation.max_size must be >= 0",
		},
		{
			name: "negative max_age fails",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: "{url}"
    rotation:
      max_age: -1
`,
			wantErr:        true,
			errorSubstring: "event_logging.file.rotation.max_age must be >= 0",
		},
		{
			name: "negative max_backups fails",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: "{url}"
    rotation:
      max_backups: -1
`,
			wantErr:        true,
			errorSubstring: "event_logging.file.rotation.max_backups must be >= 0",
		},
		{
			name: "zero rotation values are valid",
			eventLogging: `event_logging:
  file:
    enabled: true
    path: "/tmp/access.log"
    template: "{url}"
    rotation:
      max_size: 0
      max_age: 0
      max_backups: 0
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tmpDir, "hosts.d")

			configContent := `
internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"

server:
  listen: ":8080"
  timeout: 120s

redis:
  addr: "localhost:6379"

storage:
  base_path: "` + tmpDir + `/cache"

render:
  cache:
    ttl: 1h

bypass:
  timeout: 30s
  user_agent: "test"
  cache:
    enabled: false

log:
  level: "info"
  console:
    enabled: true

metrics:
  enabled: true
  listen: ":9090"

hosts:
  include: "hosts.d/"

` + tt.eventLogging

			err := os.WriteFile(configPath, []byte(configContent), 0o644)
			require.NoError(t, err)

			err = os.MkdirAll(hostsDir, 0o755)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(`hosts:
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
          render_ua: "Mozilla/5.0"`), 0o644)
			require.NoError(t, err)

			result, err := ValidateConfiguration(configPath)
			require.NoError(t, err)

			if tt.wantErr {
				assert.False(t, result.Valid, "Expected validation to fail")
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.errorSubstring) {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error containing %q, got: %v", tt.errorSubstring, result.Errors)
			} else {
				// Filter out storage-related warnings that might appear
				validationErrors := make([]ValidationError, 0)
				for _, e := range result.Errors {
					validationErrors = append(validationErrors, e)
				}
				assert.True(t, result.Valid, "Expected validation to pass, got errors: %v", validationErrors)
			}
		})
	}
}

func TestValidateClientIPConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.ClientIPConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "valid config with headers",
			config:  &types.ClientIPConfig{Headers: []string{"X-Real-IP", "X-Forwarded-For"}},
			wantErr: false,
		},
		{
			name:        "empty headers slice",
			config:      &types.ClientIPConfig{Headers: []string{}},
			wantErr:     true,
			errContains: "must not be empty",
		},
		{
			name:        "invalid header name",
			config:      &types.ClientIPConfig{Headers: []string{"Valid-Header", "invalid header"}},
			wantErr:     true,
			errContains: "invalid header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClientIPConfig(tt.config, "test")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

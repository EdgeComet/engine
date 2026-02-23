package validate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to create a config with TLS settings
func makeTLSConfig(tls configtypes.TLSConfig, serverListen string, metricsEnabled bool, metricsListen string, internalListen string) *configtypes.EgConfig {
	return &configtypes.EgConfig{
		Server: configtypes.ServerConfig{
			Listen: serverListen,
			TLS:    tls,
		},
		Metrics: configtypes.MetricsConfig{
			Enabled: metricsEnabled,
			Listen:  metricsListen,
		},
		Internal: configtypes.InternalConfig{
			Listen: internalListen,
		},
	}
}

func TestValidateTLSConfig_RequiredFields(t *testing.T) {
	t.Run("disabled TLS returns no errors", func(t *testing.T) {
		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{Enabled: false},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/config", "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("enabled with all fields returns no errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")
		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("enabled but missing listen returns error", func(t *testing.T) {
		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   "",
				CertFile: "/path/to/cert.crt",
				KeyFile:  "/path/to/key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/config", "test.yaml", collector)
		require.True(t, collector.HasErrors())
		assert.Contains(t, collector.Errors()[0].Message, "TLS enabled but tls.listen not specified")
	})

	t.Run("enabled but missing cert_file returns error", func(t *testing.T) {
		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "",
				KeyFile:  "/path/to/key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/config", "test.yaml", collector)
		require.True(t, collector.HasErrors())
		assert.Contains(t, collector.Errors()[0].Message, "TLS enabled but tls.cert_file not specified")
	})

	t.Run("enabled but missing key_file returns error", func(t *testing.T) {
		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "/path/to/cert.crt",
				KeyFile:  "",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/config", "test.yaml", collector)
		require.True(t, collector.HasErrors())
		assert.Contains(t, collector.Errors()[0].Message, "TLS enabled but tls.key_file not specified")
	})

	t.Run("multiple missing fields returns multiple errors", func(t *testing.T) {
		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   "",
				CertFile: "",
				KeyFile:  "",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/config", "test.yaml", collector)
		require.True(t, collector.HasErrors())
		// Should have 3 errors: missing listen, cert_file, and key_file
		assert.Len(t, collector.Errors(), 3)
	})
}

func TestValidateTLSConfig_ListenAddress(t *testing.T) {
	tests := []struct {
		name        string
		listen      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid port only",
			listen:  ":10443",
			wantErr: false,
		},
		{
			name:    "valid all interfaces",
			listen:  "0.0.0.0:10443",
			wantErr: false,
		},
		{
			name:    "valid specific IP",
			listen:  "192.168.1.1:10443",
			wantErr: false,
		},
		{
			name:    "valid localhost",
			listen:  "localhost:10443",
			wantErr: false,
		},
		{
			name:        "invalid missing colon",
			listen:      "10443",
			wantErr:     true,
			errContains: "TLS listen address invalid",
		},
		{
			name:        "invalid port 0",
			listen:      ":0",
			wantErr:     true,
			errContains: "TLS listen address invalid",
		},
		{
			name:        "invalid port too high",
			listen:      ":70000",
			wantErr:     true,
			errContains: "TLS listen address invalid",
		},
		{
			name:        "invalid non-numeric port",
			listen:      ":abc",
			wantErr:     true,
			errContains: "TLS listen address invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp files for valid cases so file validation passes
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "cert.crt")
			keyPath := filepath.Join(tmpDir, "key.key")
			require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
			require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

			collector := NewErrorCollector()
			cfg := makeTLSConfig(
				configtypes.TLSConfig{
					Enabled:  true,
					Listen:   tt.listen,
					CertFile: certPath,
					KeyFile:  keyPath,
				},
				":10070", false, "", "",
			)
			validateTLSConfig(cfg, tmpDir, "test.yaml", collector)

			if tt.wantErr {
				require.True(t, collector.HasErrors())
				found := false
				for _, err := range collector.Errors() {
					if assert.Contains(t, err.Message, tt.errContains) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected error containing %q", tt.errContains)
			} else {
				require.False(t, collector.HasErrors())
			}
		})
	}
}

func TestValidateTLSConfig_FileValidation(t *testing.T) {
	t.Run("relative path resolution", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "cert.crt",
				KeyFile:  "key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("absolute path used as-is", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, "/different/config/dir", "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("path with parent traversal", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.Mkdir(subDir, 0o755))

		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "../cert.crt",
				KeyFile:  "../key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, subDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("symlink resolution", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks require elevated privileges on Windows")
		}

		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")
		certLink := filepath.Join(tmpDir, "cert_link.crt")
		keyLink := filepath.Join(tmpDir, "key_link.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))
		require.NoError(t, os.Symlink(certPath, certLink))
		require.NoError(t, os.Symlink(keyPath, keyLink))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "cert_link.crt",
				KeyFile:  "key_link.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("missing cert file returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyPath := filepath.Join(tmpDir, "key.key")
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "nonexistent.crt",
				KeyFile:  "key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS cert_file not found") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("missing key file returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "cert.crt",
				KeyFile:  "nonexistent.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS key_file not found") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("unreadable cert file returns error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod not reliable on Windows")
		}

		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o000))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "cert.crt",
				KeyFile:  "key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS cert_file not readable") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("both files exist returns no errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")

		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: "cert.crt",
				KeyFile:  "key.key",
			},
			":10070", false, "", "",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})
}

func TestValidateTLSConfig_PortConflicts(t *testing.T) {
	// Helper to create temp files for all tests
	setupTempFiles := func(t *testing.T) (string, string, string) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.crt")
		keyPath := filepath.Join(tmpDir, "key.key")
		require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0o644))
		require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o644))
		return tmpDir, certPath, keyPath
	}

	t.Run("no conflict with different ports", func(t *testing.T) {
		tmpDir, certPath, keyPath := setupTempFiles(t)

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10443",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", true, ":10079", ":10071",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.False(t, collector.HasErrors())
	})

	t.Run("conflict with HTTP port", func(t *testing.T) {
		tmpDir, certPath, keyPath := setupTempFiles(t)

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10070",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", true, ":10079", ":10071",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS listen port conflicts with server.listen: both use port 10070") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("conflict with metrics port", func(t *testing.T) {
		tmpDir, certPath, keyPath := setupTempFiles(t)

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10079",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", true, ":10079", ":10071",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS listen port 10079 conflicts with metrics.port") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("conflict with internal server port", func(t *testing.T) {
		tmpDir, certPath, keyPath := setupTempFiles(t)

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   ":10071",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			":10070", true, ":10079", ":10071",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS listen port 10071 conflicts with internal_server.listen") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("different IPs same port still conflicts", func(t *testing.T) {
		tmpDir, certPath, keyPath := setupTempFiles(t)

		collector := NewErrorCollector()
		cfg := makeTLSConfig(
			configtypes.TLSConfig{
				Enabled:  true,
				Listen:   "192.168.1.1:10070",
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			"0.0.0.0:10070", true, ":10079", ":10071",
		)
		validateTLSConfig(cfg, tmpDir, "test.yaml", collector)
		require.True(t, collector.HasErrors())
		found := false
		for _, err := range collector.Errors() {
			if assert.Contains(t, err.Message, "TLS listen port conflicts with server.listen: both use port 10070") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})
}

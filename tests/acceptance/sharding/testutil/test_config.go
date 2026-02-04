package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// EdgeGatewayConfig represents configuration for a single Edge Gateway instance
type EdgeGatewayConfig struct {
	Port     int    `yaml:"port"`
	Address  string `yaml:"address"`
	Internal struct {
		Listen string `yaml:"listen"`
	} `yaml:"internal"`
	EgID    string `yaml:"eg_id"`
	Timeout string `yaml:"timeout"`

	Storage struct {
		BasePath string `yaml:"base_path"`
	} `yaml:"storage"`

	Bypass struct {
		Enabled   bool   `yaml:"enabled"`
		Timeout   string `yaml:"timeout"`
		UserAgent string `yaml:"user_agent"`
	} `yaml:"bypass"`

	Registry struct {
		SelectionStrategy string `yaml:"selection_strategy"`
	} `yaml:"registry"`

	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

// TestEnvironmentConfig represents the unified test environment configuration
type TestEnvironmentConfig struct {
	TestServer struct {
		Port    int    `yaml:"port"`
		Address string `yaml:"address"`
	} `yaml:"test_server"`

	EdgeGateway1 EdgeGatewayConfig `yaml:"edge_gateway1"`
	EdgeGateway2 EdgeGatewayConfig `yaml:"edge_gateway2"`
	EdgeGateway3 EdgeGatewayConfig `yaml:"edge_gateway3"`

	RenderService struct {
		ID      string `yaml:"id"`
		Port    int    `yaml:"port"`
		Address string `yaml:"address"`

		Chrome struct {
			PoolSize int `yaml:"pool_size"`

			Warmup struct {
				URL     string `yaml:"url"`
				Timeout string `yaml:"timeout"`
			} `yaml:"warmup"`

			Restart struct {
				AfterCount int    `yaml:"after_count"`
				AfterTime  string `yaml:"after_time"`
			} `yaml:"restart"`

			Render struct {
				MaxTimeout string `yaml:"max_timeout"`
			} `yaml:"render"`
		} `yaml:"chrome"`

		Log struct {
			Level  string `yaml:"level"`
			Format string `yaml:"format"`
		} `yaml:"log"`
	} `yaml:"render_service"`

	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`

	Test struct {
		ValidAPIKey        string `yaml:"valid_api_key"`
		InvalidAPIKey      string `yaml:"invalid_api_key"`
		HostID             int    `yaml:"host_id"`
		StartupTimeout     string `yaml:"startup_timeout"`
		HealthCheckTimeout string `yaml:"health_check_timeout"`
		HTTPClientTimeout  string `yaml:"http_client_timeout"`
	} `yaml:"test"`
}

// LoadTestConfig loads the test configuration from test_config.yaml
func LoadTestConfig() (*TestEnvironmentConfig, error) {
	// Find test_config.yaml relative to the test module root
	configPath := filepath.Join("fixtures", "test_config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read test config: %w", err)
	}

	var config TestEnvironmentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse test config: %w", err)
	}

	return &config, nil
}

// EG1BaseURL returns the Edge Gateway 1 base URL (client port)
func (c *TestEnvironmentConfig) EG1BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", c.EdgeGateway1.Port)
}

// EG2BaseURL returns the Edge Gateway 2 base URL (client port)
func (c *TestEnvironmentConfig) EG2BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", c.EdgeGateway2.Port)
}

// EG3BaseURL returns the Edge Gateway 3 base URL (client port)
func (c *TestEnvironmentConfig) EG3BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", c.EdgeGateway3.Port)
}

// EG1InternalBaseURL returns the Edge Gateway 1 internal API URL
func (c *TestEnvironmentConfig) EG1InternalBaseURL() string {
	return "http://" + c.EdgeGateway1.Internal.Listen
}

// EG2InternalBaseURL returns the Edge Gateway 2 internal API URL
func (c *TestEnvironmentConfig) EG2InternalBaseURL() string {
	return "http://" + c.EdgeGateway2.Internal.Listen
}

// EG3InternalBaseURL returns the Edge Gateway 3 internal API URL
func (c *TestEnvironmentConfig) EG3InternalBaseURL() string {
	return "http://" + c.EdgeGateway3.Internal.Listen
}

// EGBaseURL returns the Edge Gateway 1 base URL (for backward compatibility)
func (c *TestEnvironmentConfig) EGBaseURL() string {
	return c.EG1BaseURL()
}

// RSBaseURL returns the Render Service base URL
func (c *TestEnvironmentConfig) RSBaseURL() string {
	return fmt.Sprintf("http://localhost:%d", c.RenderService.Port)
}

// TestPagesURL returns the test pages server base URL
func (c *TestEnvironmentConfig) TestPagesURL() string {
	return fmt.Sprintf("http://localhost:%d", c.TestServer.Port)
}

// StartupTimeout returns the startup timeout as duration
func (c *TestEnvironmentConfig) StartupTimeout() time.Duration {
	d, _ := time.ParseDuration(c.Test.StartupTimeout)
	if d == 0 {
		return 30 * time.Second
	}
	return d
}

// HealthCheckTimeout returns the health check timeout as duration
func (c *TestEnvironmentConfig) HealthCheckTimeout() time.Duration {
	d, _ := time.ParseDuration(c.Test.HealthCheckTimeout)
	if d == 0 {
		return 30 * time.Second
	}
	return d
}

// HTTPClientTimeout returns the HTTP client timeout as duration
func (c *TestEnvironmentConfig) HTTPClientTimeout() time.Duration {
	d, _ := time.ParseDuration(c.Test.HTTPClientTimeout)
	if d == 0 {
		return 30 * time.Second
	}
	return d
}

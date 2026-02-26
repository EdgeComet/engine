package chrome

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_CalculatePoolSize(t *testing.T) {
	config := DefaultConfig()

	// Test with explicit pool size
	config.PoolSize = "10"
	assert.Equal(t, 10, config.CalculatePoolSize())

	// Test with auto-sizing (PoolSize = 0)
	config.PoolSize = "auto"
	autoSize := config.CalculatePoolSize()
	assert.GreaterOrEqual(t, autoSize, 2, "Should have at least 2 instances")
	assert.LessOrEqual(t, autoSize, 50, "Should not exceed 50 instances")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		modifyFn  func(*Config)
		expectErr bool
	}{
		{
			name:      "valid config",
			modifyFn:  func(c *Config) {},
			expectErr: false,
		},
		{
			name: "negative pool size",
			modifyFn: func(c *Config) {
				c.PoolSize = "-1"
			},
			expectErr: true,
		},
		{
			name: "zero restart count",
			modifyFn: func(c *Config) {
				c.RestartAfterCount = 0
			},
			expectErr: true,
		},
		{
			name: "empty warmup URL",
			modifyFn: func(c *Config) {
				c.WarmupURL = ""
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.modifyFn(config)

			err := config.Validate()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

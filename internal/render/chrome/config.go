package chrome

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// Config holds the configuration for Chrome pool and instances
type Config struct {
	// Pool configuration
	PoolSize        string        // "auto" or integer string
	WarmupURL       string        // URL to navigate during warmup
	WarmupTimeout   time.Duration // Warmup navigation timeout
	ShutdownTimeout time.Duration // Graceful shutdown timeout

	// Restart policies
	RestartAfterCount int           // Restart after N renders
	RestartAfterTime  time.Duration // Restart after duration
}

// NewConfigFromYAML creates a Config from ChromeYAMLConfig
// This is used to convert the YAML config structure to internal Config
func NewConfigFromYAML(poolSize string, warmupURL string, warmupTimeout time.Duration,
	restartAfterCount int, restartAfterTime time.Duration, shutdownTimeout time.Duration,
) *Config {
	return &Config{
		PoolSize:          poolSize,
		WarmupURL:         warmupURL,
		WarmupTimeout:     warmupTimeout,
		ShutdownTimeout:   shutdownTimeout,
		RestartAfterCount: restartAfterCount,
		RestartAfterTime:  restartAfterTime,
	}
}

// DefaultConfig is used in tests to avoid constructing full Config structs
func DefaultConfig() *Config {
	return &Config{
		PoolSize:          "auto",
		WarmupURL:         "https://example.com/",
		WarmupTimeout:     10 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		RestartAfterCount: 100,
		RestartAfterTime:  60 * time.Minute,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate pool size (must be "auto" or positive integer string)
	if c.PoolSize != "auto" {
		size, err := strconv.Atoi(c.PoolSize)
		if err != nil {
			return fmt.Errorf("pool size must be 'auto' or valid integer")
		}
		if size <= 0 {
			return fmt.Errorf("pool size must be positive")
		}
	}

	if c.RestartAfterCount <= 0 {
		return fmt.Errorf("restart after count must be positive")
	}

	if c.RestartAfterTime <= 0 {
		return fmt.Errorf("restart after time must be positive")
	}

	if c.WarmupURL == "" {
		return fmt.Errorf("warmup URL cannot be empty")
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	return nil
}

// CalculatePoolSize determines the optimal pool size based on available RAM
// Formula: (Available RAM - 2GB) / 500MB per Chrome
func (c *Config) CalculatePoolSize() int {
	if c.PoolSize == "auto" {
		// Auto-calculate based on system RAM
		return c.calculateAutoPoolSize()
	}

	// Parse as integer
	size, err := strconv.Atoi(c.PoolSize)
	if err != nil || size <= 0 {
		// Fallback to auto if invalid
		return c.calculateAutoPoolSize()
	}

	return size
}

// calculateAutoPoolSize calculates pool size based on available RAM
func (c *Config) calculateAutoPoolSize() int {
	// Get actual system memory using gopsutil
	v, err := mem.VirtualMemory()
	var totalRAMBytes int64

	if err != nil {
		// Fallback to conservative estimate if we can't read system memory
		totalRAMBytes = int64(8 * 1024 * 1024 * 1024) // 8GB fallback
	} else {
		totalRAMBytes = int64(v.Total)
	}

	// Reserve 2GB for system and other processes
	reservedBytes := int64(2 * 1024 * 1024 * 1024)
	availableBytes := totalRAMBytes - reservedBytes

	// Each Chrome instance uses approximately 500MB
	chromeInstanceBytes := int64(500 * 1024 * 1024)

	poolSize := int(availableBytes / chromeInstanceBytes)

	// Safety limits
	if poolSize < 2 {
		poolSize = 2 // Minimum 2 instances
	}
	if poolSize > 50 {
		poolSize = 50 // Maximum 50 instances
	}

	return poolSize
}

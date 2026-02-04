package chrome

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewChromePool(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "3"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Shutdown()

	assert.Equal(t, 3, pool.PoolSize())
	assert.Equal(t, 3, pool.AvailableInstances())
}

func TestChromePool_AcquireRelease(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "2"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)
	defer pool.Shutdown()

	// Acquire instance
	instance, err := pool.AcquireChrome("test-request-1")
	require.NoError(t, err)
	require.NotNil(t, instance)

	assert.Equal(t, 1, pool.AvailableInstances())
	assert.Equal(t, ChromeStatusRendering, instance.GetStatus())

	// Release instance
	pool.ReleaseChrome(instance)
	assert.Equal(t, 2, pool.AvailableInstances())
	assert.Equal(t, ChromeStatusIdle, instance.GetStatus())
}

func TestChromePool_ConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "5"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)
	defer pool.Shutdown()

	// Spawn multiple goroutines that acquire and release instances
	var wg sync.WaitGroup
	numGoroutines := 20
	acquisitionsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < acquisitionsPerGoroutine; j++ {
				instance, err := pool.AcquireChrome("test-request")
				if err != nil {
					t.Logf("Failed to acquire: %v", err)
					continue
				}

				// Simulate some work
				time.Sleep(10 * time.Millisecond)

				pool.ReleaseChrome(instance)
			}
		}(i)
	}

	wg.Wait()

	// All instances should be back in the pool
	assert.Equal(t, 5, pool.AvailableInstances())

	// Check stats
	stats := pool.GetStats()
	assert.Equal(t, int64(numGoroutines*acquisitionsPerGoroutine), stats.TotalRenders)
}

func TestChromePool_GetStats(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "3"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)
	defer pool.Shutdown()

	stats := pool.GetStats()
	assert.Equal(t, 3, stats.TotalInstances)
	assert.Equal(t, 3, stats.AvailableInstances)
	assert.Equal(t, 0, stats.ActiveInstances)
	assert.Equal(t, int64(0), stats.TotalRenders)

	// Acquire an instance
	instance, err := pool.AcquireChrome("test-request-stats")
	require.NoError(t, err)

	stats = pool.GetStats()
	assert.Equal(t, 2, stats.AvailableInstances)
	assert.Equal(t, 1, stats.ActiveInstances)

	// Release
	pool.ReleaseChrome(instance)

	stats = pool.GetStats()
	assert.Equal(t, 3, stats.AvailableInstances)
	assert.Equal(t, 0, stats.ActiveInstances)
	assert.Equal(t, int64(1), stats.TotalRenders)
}

func TestChromePool_AutoRestart(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "1"
	config.WarmupURL = "about:blank"
	config.RestartAfterCount = 3
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)
	defer pool.Shutdown()

	// Acquire and release multiple times to trigger restart
	for i := 0; i < 4; i++ {
		instance, err := pool.AcquireChrome("test-request-restart")
		require.NoError(t, err)

		// On the 4th acquisition, instance should be restarted
		if i == 3 {
			assert.Equal(t, int32(0), instance.GetRequestsDone(), "Instance should have been restarted")
		}

		pool.ReleaseChrome(instance)
	}

	stats := pool.GetStats()
	assert.Greater(t, stats.TotalRestarts, int64(0), "Should have at least one restart")
}

func TestChromePool_Shutdown(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "3"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Get initial stats
	stats := pool.GetStats()
	assert.Equal(t, 3, stats.TotalInstances)
	assert.Equal(t, 3, pool.AvailableInstances())

	// Shutdown
	err = pool.Shutdown()
	assert.NoError(t, err)

	// Verify context is cancelled
	select {
	case <-pool.ctx.Done():
		// Context should be done
	default:
		t.Fatal("Pool context should be cancelled after shutdown")
	}

	// Verify all instances in the pool are terminated
	pool.mu.RLock()
	for i, instance := range pool.instances {
		assert.Equal(t, ChromeStatusDead, instance.GetStatus(), "Instance %d should be dead", i)
	}
	pool.mu.RUnlock()
}

func TestChromePool_ShutdownWithActiveRenders(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "2"
	config.WarmupURL = "about:blank"
	config.ShutdownTimeout = 2 * time.Second
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Acquire instances
	instance1, err := pool.AcquireChrome("test-request-shutdown-1")
	require.NoError(t, err)
	instance2, err := pool.AcquireChrome("test-request-shutdown-2")
	require.NoError(t, err)

	// Verify active renders
	assert.Equal(t, int32(2), pool.activeTabs.Load())

	// Start shutdown in background
	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- pool.Shutdown()
	}()

	// Give shutdown time to initiate
	time.Sleep(100 * time.Millisecond)

	// Try to acquire - should fail
	_, err = pool.AcquireChrome("test-request-shutdown-fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutting down")

	// Release instances
	pool.ReleaseChrome(instance1)
	pool.ReleaseChrome(instance2)

	// Wait for shutdown to complete
	err = <-shutdownDone
	assert.NoError(t, err)

	// Verify all instances terminated
	assert.Equal(t, int32(0), pool.activeTabs.Load())
}

func TestChromePool_ShutdownTimeout(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "1"
	config.WarmupURL = "about:blank"
	config.ShutdownTimeout = 500 * time.Millisecond
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Acquire instance and don't release it
	instance, err := pool.AcquireChrome("test-request-timeout")
	require.NoError(t, err)
	_ = instance

	// Shutdown should timeout
	start := time.Now()
	err = pool.Shutdown()
	duration := time.Since(start)

	// Should complete around the timeout duration
	assert.InDelta(t, config.ShutdownTimeout.Seconds(), duration.Seconds(), 0.2)
	assert.NoError(t, err) // No error, just forced termination

	// Instance should be terminated despite not being released
	pool.mu.RLock()
	assert.Equal(t, ChromeStatusDead, pool.instances[0].GetStatus())
	pool.mu.RUnlock()
}

func TestChromePool_AcquireAfterShutdown(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "2"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Shutdown pool
	err = pool.Shutdown()
	require.NoError(t, err)

	// Try to acquire - should fail
	_, err = pool.AcquireChrome("test-request-after-shutdown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutting down")
}

func TestChromePool_ReleaseDuringShutdown(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "2"
	config.WarmupURL = "about:blank"
	config.ShutdownTimeout = 1 * time.Second
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Acquire instance
	instance, err := pool.AcquireChrome("test-request-release-during-shutdown")
	require.NoError(t, err)

	// Start shutdown
	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- pool.Shutdown()
	}()

	// Give shutdown time to initiate
	time.Sleep(100 * time.Millisecond)

	// Release should not panic
	pool.ReleaseChrome(instance)

	// Wait for shutdown
	err = <-shutdownDone
	assert.NoError(t, err)
}

func TestChromePool_ConcurrentShutdown(t *testing.T) {
	config := DefaultConfig()
	config.PoolSize = "2"
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	pool, err := NewChromePool(config, nil, nil, nil, nil, "", logger)
	require.NoError(t, err)

	// Call shutdown multiple times concurrently
	var wg sync.WaitGroup
	errors := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = pool.Shutdown()
		}(i)
	}

	wg.Wait()

	// At least one should succeed, others might timeout or succeed
	// The important thing is no panics occurred
	successCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		}
	}
	assert.GreaterOrEqual(t, successCount, 1, "At least one shutdown should succeed")
}

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

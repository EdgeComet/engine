package chrome

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewChromeInstance(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank" // Use about:blank for faster tests
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	require.NotNil(t, instance)

	defer instance.Terminate()

	assert.Equal(t, 0, instance.ID)
	assert.Equal(t, ChromeStatusIdle, instance.GetStatus())
	assert.Equal(t, int32(0), instance.GetRequestsDone())
	assert.False(t, instance.createdAt.IsZero())
}

func TestChromeInstance_IsAlive(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	// Instance should be alive after creation
	assert.True(t, instance.IsAlive())

	// Terminate and check it's dead
	instance.Terminate()
	assert.False(t, instance.IsAlive())
}

func TestChromeInstance_Age(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	// Age should be small initially
	age := instance.Age()
	assert.Greater(t, age, time.Duration(0))
	assert.Less(t, age, 5*time.Second)

	// Wait and check age increased
	time.Sleep(100 * time.Millisecond)
	newAge := instance.Age()
	assert.Greater(t, newAge, age)
}

func TestChromeInstance_ShouldRestart(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	config.RestartAfterCount = 5
	config.RestartAfterTime = 1 * time.Second
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	// Should not need restart initially
	assert.False(t, instance.ShouldRestart(config))

	// Should need restart after request count
	instance.requestsDone = 5
	assert.True(t, instance.ShouldRestart(config))

	// Reset and test time-based restart
	instance.requestsDone = 0
	instance.createdAt = time.Now() // Reset creation time
	assert.False(t, instance.ShouldRestart(config))

	// Set creation time to past to trigger time-based restart
	instance.createdAt = time.Now().Add(-2 * time.Second)
	assert.True(t, instance.ShouldRestart(config))
}

func TestChromeInstance_Restart(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	// Set some state
	instance.requestsDone = 10
	oldCreatedAt := instance.createdAt

	// Wait a bit so we can verify createdAt changes
	time.Sleep(50 * time.Millisecond)

	// Restart
	err = instance.Restart(config)
	require.NoError(t, err)

	// Verify state was reset
	assert.Equal(t, int32(0), instance.GetRequestsDone())
	assert.True(t, instance.createdAt.After(oldCreatedAt))
	assert.Equal(t, ChromeStatusIdle, instance.GetStatus())
	assert.True(t, instance.IsAlive())
}

func TestChromeInstance_IncrementRequests(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	assert.Equal(t, int32(0), instance.GetRequestsDone())

	instance.IncrementRequests()
	assert.Equal(t, int32(1), instance.GetRequestsDone())

	instance.IncrementRequests()
	assert.Equal(t, int32(2), instance.GetRequestsDone())
}

func TestChromeInstance_StatusManagement(t *testing.T) {
	config := DefaultConfig()
	config.WarmupURL = "about:blank"
	logger := zaptest.NewLogger(t)

	instance, err := NewChromeInstance(0, "test-rs", config, logger)
	require.NoError(t, err)
	defer instance.Terminate()

	assert.Equal(t, ChromeStatusIdle, instance.GetStatus())

	instance.SetStatus(ChromeStatusRendering)
	assert.Equal(t, ChromeStatusRendering, instance.GetStatus())

	instance.SetStatus(ChromeStatusRestarting)
	assert.Equal(t, ChromeStatusRestarting, instance.GetStatus())
}

func TestChromeStatus_String(t *testing.T) {
	tests := []struct {
		status   ChromeStatus
		expected string
	}{
		{ChromeStatusIdle, "idle"},
		{ChromeStatusRendering, "rendering"},
		{ChromeStatusRestarting, "restarting"},
		{ChromeStatusDead, "dead"},
		{ChromeStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

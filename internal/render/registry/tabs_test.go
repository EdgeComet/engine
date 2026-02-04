package registry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/redis"
)

func TestTabManager_RegisterTabs(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	redisClient := &redis.Client{} // Mock or real Redis client needed
	poolSize := 5
	serviceID := "test-service-1"

	tm := NewTabManager(redisClient, serviceID, poolSize, logger)

	// Verify initial state
	assert.Equal(t, "tabs:test-service-1", tm.GetTabsKey())
	assert.Equal(t, "test-service-1", tm.GetServiceID())
	assert.Equal(t, 5, tm.GetPoolSize())
}

func TestTabManager_GettersReturnCorrectValues(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	redisClient := &redis.Client{}

	tm := NewTabManager(redisClient, "rs-42", 10, logger)

	assert.Equal(t, "tabs:rs-42", tm.GetTabsKey())
	assert.Equal(t, "rs-42", tm.GetServiceID())
	assert.Equal(t, 10, tm.GetPoolSize())
}

// Integration test - requires real Redis
func TestTabManager_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Setup real Redis connection
	logger, _ := zap.NewDevelopment()
	cfg := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       1, // Use test DB
	}

	redisClient, err := redis.NewClient(cfg, logger)
	require.NoError(t, err)
	defer redisClient.Close()

	ctx := context.Background()
	serviceID := "test-service-integration"
	poolSize := 3

	tm := NewTabManager(redisClient, serviceID, poolSize, logger)

	// Clean up before test
	_ = tm.DeleteTabs(ctx)

	// Test RegisterTabs
	err = tm.RegisterTabs(ctx)
	require.NoError(t, err)

	// Verify tabs exist in Redis
	exists, err := redisClient.Exists(ctx, "tabs:test-service-integration")
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify all tabs are available (empty string)
	for i := 0; i < poolSize; i++ {
		value, err := redisClient.HGet(ctx, tm.tabsKey, string(rune('0'+i)))
		require.NoError(t, err)
		assert.Equal(t, "", value)
	}

	// Test CountReservations with no reservations
	count := tm.CountReservations(ctx)
	assert.Equal(t, 0, count)

	// Simulate reservation
	err = redisClient.HSet(ctx, tm.tabsKey, "0", "request-123")
	require.NoError(t, err)

	// Test CountReservations with one reservation
	count = tm.CountReservations(ctx)
	assert.Equal(t, 1, count)

	// Test ClearReservation
	err = tm.ClearReservation(ctx, 0)
	require.NoError(t, err)

	// Verify reservation cleared
	count = tm.CountReservations(ctx)
	assert.Equal(t, 0, count)

	// Test ExtendTTL
	err = tm.ExtendTTL(ctx, 10*time.Second)
	require.NoError(t, err)

	// Verify TTL was set
	ttl, err := redisClient.TTL(ctx, tm.tabsKey)
	require.NoError(t, err)
	assert.Greater(t, ttl, 5*time.Second) // Should be around 10s

	// Test DeleteTabs
	err = tm.DeleteTabs(ctx)
	require.NoError(t, err)

	// Verify tabs deleted
	exists, err = redisClient.Exists(ctx, "tabs:test-service-integration")
	require.NoError(t, err)
	assert.False(t, exists)
}

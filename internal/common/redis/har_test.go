package redis

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreHAR(t *testing.T) {
	client := setupTestClient(t)
	if client == nil {
		t.Skip("Redis not available")
	}

	ctx := context.Background()
	data := []byte(`{"log":{"version":"1.2"}}`)

	err := client.StoreHAR(ctx, "host-1", "req-123", data, 5*time.Minute)
	require.NoError(t, err)

	// Clean up
	defer client.DeleteHAR(ctx, "host-1", "req-123")
}

func TestGetHAR(t *testing.T) {
	client := setupTestClient(t)
	if client == nil {
		t.Skip("Redis not available")
	}

	ctx := context.Background()
	data := []byte(`{"log":{"version":"1.2"}}`)

	err := client.StoreHAR(ctx, "host-1", "req-get", data, 5*time.Minute)
	require.NoError(t, err)
	defer client.DeleteHAR(ctx, "host-1", "req-get")

	result, err := client.GetHAR(ctx, "host-1", "req-get")
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestGetHARNotFound(t *testing.T) {
	client := setupTestClient(t)
	if client == nil {
		t.Skip("Redis not available")
	}

	ctx := context.Background()

	result, err := client.GetHAR(ctx, "host-1", "non-existent-har")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestDeleteHAR(t *testing.T) {
	client := setupTestClient(t)
	if client == nil {
		t.Skip("Redis not available")
	}

	ctx := context.Background()
	data := []byte(`{"log":{"version":"1.2"}}`)

	err := client.StoreHAR(ctx, "host-1", "req-del", data, 5*time.Minute)
	require.NoError(t, err)

	err = client.DeleteHAR(ctx, "host-1", "req-del")
	require.NoError(t, err)

	result, err := client.GetHAR(ctx, "host-1", "req-del")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHarKey(t *testing.T) {
	key := harKey("host-1", "req-123")
	assert.Equal(t, "debug:har:host-1:req-123", key)
}

func TestDefaultHARTTL(t *testing.T) {
	assert.Equal(t, 5*time.Minute, defaultHARTTL)
}

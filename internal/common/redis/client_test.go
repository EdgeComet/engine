package redis

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/logger"
)

func TestNewClient(t *testing.T) {
	log, err := logger.NewDefaultLogger()
	require.NoError(t, err)

	tests := []struct {
		name        string
		config      *config.RedisConfig
		expectError bool
		errorText   string
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
			errorText:   "redis config is required",
		},
		{
			name: "invalid Redis address",
			config: &config.RedisConfig{
				Addr:     "invalid:99999",
				Password: "",
				DB:       0,
			},
			expectError: true,
			errorText:   "failed to connect to Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config, log.Logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorText)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				if client != nil {
					client.Close()
				}
			}
		})
	}
}

func TestNewClientNilLogger(t *testing.T) {
	cfg := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	}

	client, err := NewClient(cfg, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger is required")
	assert.Nil(t, client)
}

func TestClientBasicOperations(t *testing.T) {
	client := setupTestClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer client.Close()

	ctx := context.Background()

	t.Run("ping", func(t *testing.T) {
		err := client.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("health check", func(t *testing.T) {
		err := client.HealthCheck(ctx)
		assert.NoError(t, err)
	})

	t.Run("set and get", func(t *testing.T) {
		key := "test:key"
		value := "test_value"

		err := client.Set(ctx, key, value, time.Minute)
		require.NoError(t, err)

		retrieved, err := client.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		err = client.Del(ctx, key)
		assert.NoError(t, err)
	})

	t.Run("get non-existent key", func(t *testing.T) {
		value, err := client.Get(ctx, "non:existent:key")
		assert.NoError(t, err)
		assert.Empty(t, value)
	})

	t.Run("setnx", func(t *testing.T) {
		key := "test:setnx"
		value := "test_value"

		acquired, err := client.SetNX(ctx, key, value, time.Minute)
		require.NoError(t, err)
		assert.True(t, acquired)

		acquired, err = client.SetNX(ctx, key, "different_value", time.Minute)
		require.NoError(t, err)
		assert.False(t, acquired)

		err = client.Del(ctx, key)
		assert.NoError(t, err)
	})

	t.Run("exists", func(t *testing.T) {
		key := "test:exists"

		exists, err := client.Exists(ctx, key)
		require.NoError(t, err)
		assert.False(t, exists)

		err = client.Set(ctx, key, "value", time.Minute)
		require.NoError(t, err)

		exists, err = client.Exists(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists)

		err = client.Del(ctx, key)
		assert.NoError(t, err)
	})

	t.Run("hash operations", func(t *testing.T) {
		key := "test:hash"

		err := client.HSet(ctx, key, "field1", "value1", "field2", "value2")
		require.NoError(t, err)

		value, err := client.HGet(ctx, key, "field1")
		require.NoError(t, err)
		assert.Equal(t, "value1", value)

		value, err = client.HGet(ctx, key, "non_existent_field")
		require.NoError(t, err)
		assert.Empty(t, value)

		allFields, err := client.HGetAll(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"field1": "value1",
			"field2": "value2",
		}, allFields)

		err = client.Del(ctx, key)
		assert.NoError(t, err)
	})

	t.Run("keys pattern matching", func(t *testing.T) {
		testKeys := []string{"test:pattern:1", "test:pattern:2", "test:other:1"}

		for _, key := range testKeys {
			err := client.Set(ctx, key, "value", time.Minute)
			require.NoError(t, err)
		}

		keys, err := client.Keys(ctx, "test:pattern:*")
		require.NoError(t, err)
		assert.Len(t, keys, 2)

		keys, err = client.Keys(ctx, "test:*")
		require.NoError(t, err)
		assert.Len(t, keys, 3)

		err = client.Del(ctx, testKeys...)
		assert.NoError(t, err)
	})

	t.Run("delete multiple keys", func(t *testing.T) {
		keys := []string{"test:del:1", "test:del:2", "test:del:3"}

		for _, key := range keys {
			err := client.Set(ctx, key, "value", time.Minute)
			require.NoError(t, err)
		}

		err := client.Del(ctx, keys...)
		assert.NoError(t, err)

		for _, key := range keys {
			exists, err := client.Exists(ctx, key)
			require.NoError(t, err)
			assert.False(t, exists)
		}
	})

	t.Run("delete no keys", func(t *testing.T) {
		err := client.Del(ctx)
		assert.NoError(t, err)
	})
}

func setupTestClient(t *testing.T) *Client {
	log, err := logger.NewDefaultLogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	cfg := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       1,
	}

	client, err := NewClient(cfg, log.Logger)
	if err != nil {
		t.Logf("Redis not available for testing: %v", err)
		return nil
	}

	return client
}

func BenchmarkClientOperations(b *testing.B) {
	client := setupBenchmarkClient(b)
	if client == nil {
		b.Skip("Redis not available for benchmarking")
		return
	}
	defer client.Close()

	ctx := context.Background()

	b.Run("set", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := "bench:set:" + string(rune(i))
			_ = client.Set(ctx, key, "value", time.Minute)
		}
	})

	b.Run("get", func(b *testing.B) {
		key := "bench:get:key"
		client.Set(ctx, key, "value", time.Minute)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = client.Get(ctx, key)
		}
	})

	b.Run("ping", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = client.Ping(ctx)
		}
	})
}

func setupBenchmarkClient(b *testing.B) *Client {
	log := zap.NewNop()

	cfg := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       1,
	}

	client, err := NewClient(cfg, log)
	if err != nil {
		b.Logf("Redis not available for benchmarking: %v", err)
		return nil
	}

	return client
}

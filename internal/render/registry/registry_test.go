package registry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/internal/common/redis"
)

func TestServiceInfo(t *testing.T) {
	info := &ServiceInfo{
		ID:       "test-service-1",
		Address:  "192.168.1.100",
		Port:     8080,
		Capacity: 100,
		Load:     25,
		LastSeen: time.Now(),
	}

	t.Run("URL generation", func(t *testing.T) {
		expected := "http://192.168.1.100:8080"
		assert.Equal(t, expected, info.URL())
	})

	t.Run("is healthy", func(t *testing.T) {
		assert.True(t, info.IsHealthy())

		info.LastSeen = time.Now().Add(-2 * time.Minute)
		assert.False(t, info.IsHealthy())
	})

	t.Run("load percentage", func(t *testing.T) {
		info.Capacity = 100
		info.Load = 25
		assert.Equal(t, 25.0, info.LoadPercentage())

		info.Capacity = 0
		assert.Equal(t, 100.0, info.LoadPercentage())

		info.Capacity = 50
		info.Load = 75
		assert.Equal(t, 150.0, info.LoadPercentage())
	})
}

func TestServiceRegistry(t *testing.T) {
	client := setupTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer client.Close()

	log, err := logger.NewDefaultLogger()
	require.NoError(t, err)
	registry := NewServiceRegistry(client, log.Logger)
	ctx := context.Background()

	cleanup := func() {
		services, _ := registry.ListServices(ctx)
		for _, service := range services {
			registry.UnregisterService(ctx, service.ID)
		}
	}

	t.Run("register service", func(t *testing.T) {
		defer cleanup()

		info := &ServiceInfo{
			ID:       "test-service-1",
			Address:  "192.168.1.100",
			Port:     8080,
			Capacity: 100,
		}

		err := registry.RegisterService(ctx, info)
		require.NoError(t, err)

		retrieved, err := registry.GetService(ctx, info.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		assert.Equal(t, info.ID, retrieved.ID)
		assert.Equal(t, info.Address, retrieved.Address)
		assert.Equal(t, info.Port, retrieved.Port)
		assert.Equal(t, info.Capacity, retrieved.Capacity)
		assert.WithinDuration(t, time.Now(), retrieved.LastSeen, time.Second)
	})

	t.Run("register service validation", func(t *testing.T) {
		tests := []struct {
			name      string
			info      *ServiceInfo
			errorText string
		}{
			{
				name:      "missing ID",
				info:      &ServiceInfo{Address: "localhost", Port: 8080},
				errorText: "service ID is required",
			},
			{
				name:      "missing address",
				info:      &ServiceInfo{ID: "test", Port: 8080},
				errorText: "service address is required",
			},
			{
				name:      "invalid port",
				info:      &ServiceInfo{ID: "test", Address: "localhost", Port: 0},
				errorText: "service port must be positive",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := registry.RegisterService(ctx, tt.info)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorText)
			})
		}
	})

	t.Run("get non-existent service", func(t *testing.T) {
		service, err := registry.GetService(ctx, "non-existent")
		assert.NoError(t, err)
		assert.Nil(t, service)
	})

	t.Run("unregister service", func(t *testing.T) {
		defer cleanup()

		info := &ServiceInfo{
			ID:      "test-service-2",
			Address: "192.168.1.101",
			Port:    8081,
		}

		err := registry.RegisterService(ctx, info)
		require.NoError(t, err)

		err = registry.UnregisterService(ctx, info.ID)
		assert.NoError(t, err)

		service, err := registry.GetService(ctx, info.ID)
		assert.NoError(t, err)
		assert.Nil(t, service)
	})

	t.Run("unregister non-existent service", func(t *testing.T) {
		err := registry.UnregisterService(ctx, "non-existent")
		assert.NoError(t, err)
	})

	t.Run("list services", func(t *testing.T) {
		defer cleanup()

		services := []*ServiceInfo{
			{ID: "service-1", Address: "192.168.1.100", Port: 8080},
			{ID: "service-2", Address: "192.168.1.101", Port: 8081},
			{ID: "service-3", Address: "192.168.1.102", Port: 8082},
		}

		for _, service := range services {
			err := registry.RegisterService(ctx, service)
			require.NoError(t, err)
		}

		retrieved, err := registry.ListServices(ctx)
		require.NoError(t, err)
		assert.Len(t, retrieved, 3)

		ids := make([]string, len(retrieved))
		for i, service := range retrieved {
			ids[i] = service.ID
		}
		assert.Contains(t, ids, "service-1")
		assert.Contains(t, ids, "service-2")
		assert.Contains(t, ids, "service-3")
	})

	t.Run("list healthy services", func(t *testing.T) {
		defer cleanup()

		services := []*ServiceInfo{
			{ID: "healthy-1", Address: "192.168.1.100", Port: 8080},
			{ID: "healthy-2", Address: "192.168.1.101", Port: 8081},
		}

		for _, service := range services {
			err := registry.RegisterService(ctx, service)
			require.NoError(t, err)
		}

		// Create a stale service manually (RegisterService always sets LastSeen to now)
		staleService := &ServiceInfo{
			ID:       "unhealthy-1",
			Address:  "192.168.1.102",
			Port:     8082,
			LastSeen: time.Now().Add(-5 * time.Minute),
		}
		data, err := json.Marshal(staleService)
		require.NoError(t, err)
		err = client.Set(ctx, "service:render:unhealthy-1", data, 0)
		require.NoError(t, err)

		healthy, err := registry.ListHealthyServices(ctx)
		require.NoError(t, err)
		assert.Len(t, healthy, 2)

		for _, service := range healthy {
			assert.Contains(t, []string{"healthy-1", "healthy-2"}, service.ID)
		}
	})

	t.Run("heartbeat", func(t *testing.T) {
		defer cleanup()

		info := &ServiceInfo{
			ID:       "heartbeat-test",
			Address:  "192.168.1.100",
			Port:     8080,
			Capacity: 100,
			Load:     10,
		}

		err := registry.RegisterService(ctx, info)
		require.NoError(t, err)

		err = registry.Heartbeat(ctx, info.ID, 50)
		require.NoError(t, err)

		updated, err := registry.GetService(ctx, info.ID)
		require.NoError(t, err)
		require.NotNil(t, updated)

		assert.Equal(t, 50, updated.Load)
		assert.WithinDuration(t, time.Now(), updated.LastSeen, time.Second)
	})

	t.Run("heartbeat non-existent service", func(t *testing.T) {
		err := registry.Heartbeat(ctx, "non-existent", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service not found")
	})
}

func TestCleanupStaleServices(t *testing.T) {
	client := setupTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer client.Close()

	log, err := logger.NewDefaultLogger()
	require.NoError(t, err)
	registry := NewServiceRegistry(client, log.Logger)
	ctx := context.Background()

	cleanup := func() {
		services, _ := registry.ListServices(ctx)
		for _, service := range services {
			registry.UnregisterService(ctx, service.ID)
		}
	}
	defer cleanup()

	info := &ServiceInfo{
		ID:       "stale-service",
		Address:  "192.168.1.100",
		Port:     8080,
		LastSeen: time.Now().Add(-5 * time.Minute),
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	serviceKey := "service:render:" + info.ID
	err = client.Set(ctx, serviceKey, data, 5*time.Minute)
	require.NoError(t, err)

	err = registry.CleanupStaleServices(ctx)
	assert.NoError(t, err)

	service, err := registry.GetService(ctx, info.ID)
	assert.NoError(t, err)
	assert.Nil(t, service)
}

func setupTestRedisClient(t *testing.T) *redis.Client {
	log, err := logger.NewDefaultLogger()
	require.NoError(t, err)

	cfg := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       2,
	}

	client, err := redis.NewClient(cfg, log.Logger)
	if err != nil {
		t.Logf("Redis not available for testing: %v", err)
		return nil
	}

	return client
}

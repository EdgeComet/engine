package registry

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/redis"
)

// orderingProbeRuns re-reads the registry enough times that a missing sort cannot
// hide behind a lucky map-iteration order
const orderingProbeRuns = 20

func setupIndexTestRegistry(t *testing.T) (*ServiceRegistry, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client, err := redis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return NewServiceRegistry(client, zap.NewNop()), mr
}

func registerTestService(t *testing.T, sr *ServiceRegistry, id string, port int) {
	t.Helper()

	require.NoError(t, sr.RegisterService(context.Background(), &ServiceInfo{
		ID:       id,
		Address:  "127.0.0.1",
		Port:     port,
		Capacity: 10,
	}))
}

func serviceIndexFields(t *testing.T, sr *ServiceRegistry) map[string]string {
	t.Helper()

	fields, err := sr.redis.HGetAll(context.Background(), serviceListKey)
	require.NoError(t, err)
	return fields
}

func TestListServices_ViaIndex(t *testing.T) {
	sr, _ := setupIndexTestRegistry(t)

	// Seeded out of order so sorted output cannot come from the seeding
	registerTestService(t, sr, "rs-4", 10083)
	registerTestService(t, sr, "rs-1", 10080)
	registerTestService(t, sr, "rs-5", 10084)
	registerTestService(t, sr, "rs-2", 10081)
	registerTestService(t, sr, "rs-3", 10082)

	// Re-read repeatedly: Go iterates a small map as a random rotation of
	// insertion order, so one call has roughly even odds of looking sorted even
	// with the sort deleted
	for i := 0; i < orderingProbeRuns; i++ {
		services, err := sr.ListServices(context.Background())
		require.NoError(t, err)

		ids := make([]string, 0, len(services))
		for _, service := range services {
			ids = append(ids, service.ID)
		}
		require.Equal(t, []string{"rs-1", "rs-2", "rs-3", "rs-4", "rs-5"}, ids)
	}

	assert.Len(t, serviceIndexFields(t, sr), 5)
	assert.Equal(t, "http://127.0.0.1:10080", serviceIndexFields(t, sr)["rs-1"])
}

func TestListServices_EmptyIndex(t *testing.T) {
	sr, _ := setupIndexTestRegistry(t)

	services, err := sr.ListServices(context.Background())
	require.NoError(t, err)
	assert.Empty(t, services)
}

func TestListServices_SoftDeletedFieldPruned(t *testing.T) {
	sr, mr := setupIndexTestRegistry(t)

	registerTestService(t, sr, "rs-1", 10080)

	// An older build unregistered rs-2 by blanking its URL rather than
	// removing the field
	mr.HSet(serviceListKey, "rs-2", "")

	services, err := sr.ListServices(context.Background())
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "rs-1", services[0].ID)

	assert.NotContains(t, serviceIndexFields(t, sr), "rs-2")
}

func TestListServices_ExpiredServicePruned(t *testing.T) {
	sr, mr := setupIndexTestRegistry(t)

	registerTestService(t, sr, "rs-1", 10080)
	registerTestService(t, sr, "rs-2", 10081)

	// miniredis needs an explicit clock nudge to expire the per-service keys
	mr.FastForward(RegistryTTL + time.Second)
	registerTestService(t, sr, "rs-1", 10080)

	services, err := sr.ListServices(context.Background())
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "rs-1", services[0].ID)

	assert.Equal(t, map[string]string{"rs-1": "http://127.0.0.1:10080"}, serviceIndexFields(t, sr))
}

func TestListServices_CorruptDataSkippedNotPruned(t *testing.T) {
	sr, mr := setupIndexTestRegistry(t)

	registerTestService(t, sr, "rs-1", 10080)
	registerTestService(t, sr, "rs-2", 10081)
	require.NoError(t, mr.Set(serviceKeyPrefix+"rs-2", "{not-json"))

	services, err := sr.ListServices(context.Background())
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "rs-1", services[0].ID)

	// The key exists, so rs-2 is alive with half-written data: its next
	// heartbeat repairs the value and it must still be in the index
	assert.Contains(t, serviceIndexFields(t, sr), "rs-2")
}

func TestListServices_ReadErrorSkippedNotPruned(t *testing.T) {
	sr, mr := setupIndexTestRegistry(t)

	registerTestService(t, sr, "rs-1", 10080)

	// Wrong value type makes GET fail with an error rather than report absence
	mr.HSet(serviceKeyPrefix+"rs-2", "unexpected", "value")
	mr.HSet(serviceListKey, "rs-2", "http://127.0.0.1:10081")

	services, err := sr.ListServices(context.Background())
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "rs-1", services[0].ID)

	// A failed read is not proof of death: pruning here would drop live services
	// from the index during a transient Redis failure
	assert.Contains(t, serviceIndexFields(t, sr), "rs-2")
}

func TestUnregisterService_RemovesIndexField(t *testing.T) {
	sr, mr := setupIndexTestRegistry(t)
	ctx := context.Background()

	registerTestService(t, sr, "rs-1", 10080)
	require.NoError(t, sr.UnregisterService(ctx, "rs-1"))

	assert.False(t, mr.Exists(serviceKeyPrefix+"rs-1"))
	assert.Empty(t, serviceIndexFields(t, sr))

	services, err := sr.ListServices(ctx)
	require.NoError(t, err)
	assert.Empty(t, services)
}

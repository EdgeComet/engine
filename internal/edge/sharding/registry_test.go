package sharding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/redis"
)

func setupTestRegistry(t *testing.T) (*RedisRegistry, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client, err := redis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return NewRedisRegistry(client, zap.NewNop()), mr
}

// seedEG writes a registry entry plus its index field the way a live EG would,
// bypassing Register() so the test controls eg_id, address and sharding flag.
func seedEG(t *testing.T, mr *miniredis.Miniredis, egID, address string, shardingEnabled bool) {
	t.Helper()

	data, err := json.Marshal(EGInfo{
		EgID:            egID,
		Address:         address,
		LastHeartbeat:   time.Now().UTC(),
		ShardingEnabled: shardingEnabled,
	})
	require.NoError(t, err)

	require.NoError(t, mr.Set(registryKeyPrefix+egID, string(data)))
	mr.SetTTL(registryKeyPrefix+egID, defaultRegistryTTL)
	mr.HSet(registryIndexKey, egID, address)
}

func indexFields(t *testing.T, r *RedisRegistry) map[string]string {
	t.Helper()

	fields, err := r.redis.HGetAll(context.Background(), registryIndexKey)
	require.NoError(t, err)
	return fields
}

func TestRegistry_RegisterMaintainsIndex(t *testing.T) {
	registry, _ := setupTestRegistry(t)
	ctx := context.Background()

	require.NoError(t, registry.Register(ctx, "eg1", "10.0.0.1:10070"))

	assert.Equal(t, map[string]string{"eg1": "10.0.0.1:10070"}, indexFields(t, registry))
}

func TestRegistry_HeartbeatReAddsPrunedIndexField(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	require.NoError(t, registry.Register(ctx, "eg1", "10.0.0.1:10070"))

	// Another instance pruned this EG while its Redis GET was failing
	mr.HDel(registryIndexKey, "eg1")
	require.Empty(t, indexFields(t, registry))

	require.NoError(t, registry.Heartbeat(ctx))

	assert.Equal(t, map[string]string{"eg1": "10.0.0.1:10070"}, indexFields(t, registry))
}

func TestRegistry_DeregisterRemovesIndexField(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	require.NoError(t, registry.Register(ctx, "eg1", "10.0.0.1:10070"))
	require.NoError(t, registry.Deregister(ctx, "eg1"))

	assert.Empty(t, indexFields(t, registry))
	assert.False(t, mr.Exists(registryKeyPrefix+"eg1"))
}

func TestRegistry_IndexKeyOutsideRegistryPrefix(t *testing.T) {
	registry, _ := setupTestRegistry(t)
	ctx := context.Background()

	require.NoError(t, registry.Register(ctx, "eg1", "10.0.0.1:10070"))

	// Older builds still scanning KEYS "registry:eg:*" must not match the hash
	keys, err := registry.redis.GetClient().Keys(ctx, registryKeyPrefix+"*").Result()
	require.NoError(t, err)
	assert.Equal(t, []string{registryKeyPrefix + "eg1"}, keys)
}

// orderingProbeRuns re-reads the registry enough times that a missing sort cannot
// hide. Go iterates a small map as a random rotation of insertion order, and
// miniredis returns HGETALL already sorted, so any single call has roughly even
// odds of looking sorted by accident.
const orderingProbeRuns = 20

// egIDsOf flattens the reader's output so ordering can be asserted in one shot
func egIDsOf(egs []EGInfo) []string {
	ids := make([]string, 0, len(egs))
	for _, eg := range egs {
		ids = append(ids, eg.EgID)
	}
	return ids
}

func TestGetHealthyEGs_OnlyLiveShardingEnabled(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	// Seeded out of order so sorted output cannot come from the seeding
	seedEG(t, mr, "eg4", "10.0.0.4:10070", true)
	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)
	seedEG(t, mr, "eg5", "10.0.0.5:10070", true)
	seedEG(t, mr, "eg2", "10.0.0.2:10070", true)
	seedEG(t, mr, "eg3", "10.0.0.3:10070", true)
	seedEG(t, mr, "eg6", "10.0.0.6:10070", false)

	// The hash-modulo distributor needs every EG to agree on member ordering, so
	// a silent sort regression splits cache ownership across the cluster
	for i := 0; i < orderingProbeRuns; i++ {
		healthy, err := registry.GetHealthyEGs(ctx)
		require.NoError(t, err)

		require.Equal(t, []string{"eg1", "eg2", "eg3", "eg4", "eg5"}, egIDsOf(healthy))
		require.Equal(t, "10.0.0.1:10070", healthy[0].Address)
	}

	// eg6 is registered, just not sharding: it stays in the index
	assert.Len(t, indexFields(t, registry), 6)
}

func TestGetHealthyEGs_EmptyIndex(t *testing.T) {
	registry, _ := setupTestRegistry(t)

	healthy, err := registry.GetHealthyEGs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, healthy)
}

func TestGetHealthyEGs_PrunesExpiredMember(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)
	seedEG(t, mr, "eg2", "10.0.0.2:10070", true)

	// miniredis needs an explicit clock nudge to expire the per-EG keys
	mr.FastForward(defaultRegistryTTL + time.Second)
	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)

	healthy, err := registry.GetHealthyEGs(ctx)
	require.NoError(t, err)
	require.Len(t, healthy, 1)
	assert.Equal(t, "eg1", healthy[0].EgID)

	assert.Equal(t, map[string]string{"eg1": "10.0.0.1:10070"}, indexFields(t, registry))
}

func TestGetHealthyEGs_CorruptDataSkippedNotPruned(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)

	seedEG(t, mr, "eg2", "10.0.0.2:10070", true)
	require.NoError(t, mr.Set(registryKeyPrefix+"eg2", "{not-json"))

	healthy, err := registry.GetHealthyEGs(ctx)
	require.NoError(t, err)
	require.Len(t, healthy, 1)
	assert.Equal(t, "eg1", healthy[0].EgID)

	// The key exists, so eg2 is alive with half-written data: its next
	// heartbeat repairs the value and it must still be in the index
	assert.Contains(t, indexFields(t, registry), "eg2")
}

func TestGetHealthyEGs_ReadErrorSkippedNotPruned(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)

	// Wrong value type makes GET fail with an error rather than report absence
	mr.HSet(registryKeyPrefix+"eg2", "unexpected", "value")
	mr.HSet(registryIndexKey, "eg2", "10.0.0.2:10070")

	healthy, err := registry.GetHealthyEGs(ctx)
	require.NoError(t, err)
	require.Len(t, healthy, 1)
	assert.Equal(t, "eg1", healthy[0].EgID)

	// A failed read is not proof of death: pruning here would drop live members
	// from the index during a transient Redis failure
	assert.Contains(t, indexFields(t, registry), "eg2")
}

func TestGetClusterMembers_DetectsClusterWithoutConfig(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	// Seeded out of order so sorted output cannot come from the seeding
	seedEG(t, mr, "eg4", "10.0.0.4:10070", true)
	seedEG(t, mr, "eg2", "10.0.0.2:10070", true)
	seedEG(t, mr, "eg5", "10.0.0.5:10070", true)
	seedEG(t, mr, "eg1", "10.0.0.1:10070", false)
	seedEG(t, mr, "eg3", "10.0.0.3:10070", true)

	// Sharding-disabled EGs still count as cluster members, and the order is sorted
	for i := 0; i < orderingProbeRuns; i++ {
		members, err := registry.GetClusterMembers(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"eg1", "eg2", "eg3", "eg4", "eg5"}, members)
	}
}

func TestGetClusterMembers_PrunesExpiredMember(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)
	seedEG(t, mr, "eg2", "10.0.0.2:10070", true)

	mr.FastForward(defaultRegistryTTL + time.Second)
	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)

	members, err := registry.GetClusterMembers(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"eg1"}, members)

	assert.Equal(t, map[string]string{"eg1": "10.0.0.1:10070"}, indexFields(t, registry))
}

func TestGetClusterMembers_EmptyIndex(t *testing.T) {
	registry, _ := setupTestRegistry(t)

	members, err := registry.GetClusterMembers(context.Background())
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestPruneIndexEntry_KeepsFieldWhenEGIsLive(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)

	// Stands in for the lost update: a reader decided eg1 was dead, and eg1
	// heartbeated before the prune landed. The conditional delete must no-op,
	// or a healthy EG drops out of the ring until its next heartbeat.
	registry.pruneIndexEntry(ctx, "eg1")

	assert.Contains(t, indexFields(t, registry), "eg1", "a live EG must survive a racing prune")
}

func TestPruneIndexEntry_RemovesFieldWhenEGIsGone(t *testing.T) {
	registry, mr := setupTestRegistry(t)
	ctx := context.Background()

	seedEG(t, mr, "eg1", "10.0.0.1:10070", true)
	mr.FastForward(defaultRegistryTTL + time.Second)

	registry.pruneIndexEntry(ctx, "eg1")

	assert.NotContains(t, indexFields(t, registry), "eg1")
}

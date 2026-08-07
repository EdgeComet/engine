package registry

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

func setupTestSelector(t *testing.T) (*TabSelector, *ServiceRegistry, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client, err := redis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return NewTabSelector(client, zap.NewNop()), NewServiceRegistry(client, zap.NewNop()), mr
}

// seedService registers a render service and its tabs hash the way a live
// instance would: capacity/load on the service key, one field per tab.
func seedService(t *testing.T, sr *ServiceRegistry, mr *miniredis.Miniredis, id string, port, capacity, load, poolSize int) {
	t.Helper()

	require.NoError(t, sr.RegisterService(context.Background(), &ServiceInfo{
		ID:       id,
		Address:  "127.0.0.1",
		Port:     port,
		Capacity: capacity,
		Load:     load,
	}))

	for i := 0; i < poolSize; i++ {
		mr.HSet(tabsKeyPrefix+id, strconv.Itoa(i), availableTab)
	}
	mr.SetTTL(tabsKeyPrefix+id, RegistryTTL)
}

// seedRawService writes a hand-crafted service value, bypassing RegisterService's
// validation, so tests can exercise values only a corrupt writer would produce.
func seedRawService(t *testing.T, mr *miniredis.Miniredis, id, rawJSON string, poolSize int) {
	t.Helper()

	require.NoError(t, mr.Set(serviceKeyPrefix+id, rawJSON))
	mr.SetTTL(serviceKeyPrefix+id, RegistryTTL)
	mr.HSet(serviceListKey, id, "http://127.0.0.1:9999")

	for i := 0; i < poolSize; i++ {
		mr.HSet(tabsKeyPrefix+id, strconv.Itoa(i), availableTab)
	}
	mr.SetTTL(tabsKeyPrefix+id, RegistryTTL)
}

func reserveTab(t *testing.T, mr *miniredis.Miniredis, id string, tabID int, requestID string) {
	t.Helper()
	mr.HSet(tabsKeyPrefix+id, strconv.Itoa(tabID), requestID)
}

func indexFieldNames(t *testing.T, ts *TabSelector) map[string]string {
	t.Helper()

	fields, err := ts.redis.HGetAll(context.Background(), serviceListKey)
	require.NoError(t, err)
	return fields
}

func TestSelectAndReserve_LeastLoaded(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-busy", 10080, 10, 8, 4)
	seedService(t, sr, mr, "rs-idle", 10081, 10, 1, 4)

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)

	assert.Equal(t, "rs-idle", reservation.ServiceID)
	assert.Equal(t, 10081, reservation.Port)
	assert.Equal(t, "127.0.0.1", reservation.Address)
}

func TestSelectAndReserve_MostAvailable(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	// rs-few is the least loaded, so this only passes if the strategy is honoured
	seedService(t, sr, mr, "rs-few", 10080, 10, 1, 4)
	seedService(t, sr, mr, "rs-many", 10081, 10, 5, 8)
	for i := 0; i < 3; i++ {
		reserveTab(t, mr, "rs-few", i, "held")
	}

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyMostAvailable)
	require.NoError(t, err)

	assert.Equal(t, "rs-many", reservation.ServiceID)
}

func TestSelectAndReserve_WritesRequestIDIntoTab(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)
	reserveTab(t, mr, "rs-1", 0, "other-request")

	reservation, err := ts.SelectAndReserve(context.Background(), "req-42", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)

	// Tab 0 is taken, so the first free one is tab 1
	assert.Equal(t, 1, reservation.TabID)
	assert.Equal(t, "req-42", mr.HGet(tabsKeyPrefix+"rs-1", "1"))
	assert.Equal(t, "other-request", mr.HGet(tabsKeyPrefix+"rs-1", "0"))

	ttl := mr.TTL(tabsKeyPrefix + "rs-1")
	assert.Equal(t, defaultReservationTTL, ttl)
}

func TestSelectAndReserve_EmptyIndexReturnsNoServices(t *testing.T) {
	ts, _, _ := setupTestSelector(t)

	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoServices)
}

func TestSelectAndReserve_AllServicesDeadReturnsNoServicesAndPrunes(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)
	seedService(t, sr, mr, "rs-2", 10081, 10, 0, 3)

	// miniredis needs an explicit clock nudge to expire the per-service keys
	mr.FastForward(RegistryTTL + time.Second)

	// The index still lists both, but neither key survives. The live counter has
	// to distinguish this from saturation, exactly as the KEYS version did.
	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoServices)

	assert.Empty(t, indexFieldNames(t, ts), "dead services pruned from the index")
}

func TestSelectAndReserve_DeadServiceSkippedLiveOneSelected(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-dead", 10080, 10, 0, 3)
	mr.FastForward(RegistryTTL + time.Second)
	seedService(t, sr, mr, "rs-live", 10081, 10, 0, 3)

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	assert.Equal(t, "rs-live", reservation.ServiceID)

	assert.NotContains(t, indexFieldNames(t, ts), "rs-dead")
	assert.Contains(t, indexFieldNames(t, ts), "rs-live")
}

func TestSelectAndReserve_SoftDeletedFieldSkippedAndPruned(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)

	// An older build unregistered rs-2 by blanking its URL rather than removing it
	mr.HSet(serviceListKey, "rs-2", "")

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	assert.Equal(t, "rs-1", reservation.ServiceID)

	assert.NotContains(t, indexFieldNames(t, ts), "rs-2")
}

func TestSelectAndReserve_AtCapacityReturnsNoCapacity(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-1", 10080, 4, 4, 4)

	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestSelectAndReserve_AllTabsBusyReturnsNoCapacity(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)
	for i := 0; i < 3; i++ {
		reserveTab(t, mr, "rs-1", i, "held")
	}

	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestSelectAndReserve_ServiceWithoutTabsHashSkipped(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	// Registered and alive, but never called RegisterTabs: HGETALL returns empty,
	// which is what replaced the dropped EXISTS check
	require.NoError(t, sr.RegisterService(context.Background(), &ServiceInfo{
		ID: "rs-notabs", Address: "127.0.0.1", Port: 10080, Capacity: 10,
	}))
	seedService(t, sr, mr, "rs-ok", 10081, 10, 0, 2)

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	assert.Equal(t, "rs-ok", reservation.ServiceID)
}

func TestSelectAndReserve_ZeroCapacitySkipped(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	seedService(t, sr, mr, "rs-zero", 10080, 0, 0, 3)

	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestRelease_ClearsTab(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)
	ctx := context.Background()

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)

	reservation, err := ts.SelectAndReserve(ctx, "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	require.Equal(t, "req-1", mr.HGet(tabsKeyPrefix+"rs-1", strconv.Itoa(reservation.TabID)))

	require.NoError(t, ts.Release(ctx, reservation))
	assert.Equal(t, availableTab, mr.HGet(tabsKeyPrefix+"rs-1", strconv.Itoa(reservation.TabID)))

	// The freed tab is handed out again
	next, err := ts.SelectAndReserve(ctx, "req-2", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	assert.Equal(t, reservation.TabID, next.TabID)
}

func TestRelease_NilReservationIsNoOp(t *testing.T) {
	ts, _, _ := setupTestSelector(t)

	assert.NoError(t, ts.Release(context.Background(), nil))
}

func TestSelectAndReserve_CorruptServiceSkippedNotFatal(t *testing.T) {
	ts, sr, mr := setupTestSelector(t)

	// "rs-bad" sorts before "rs-good", so miniredis iterates it first: a decode
	// that aborted the script would never reach the healthy service behind it
	seedRawService(t, mr, "rs-bad", "{not-json", 2)
	seedService(t, sr, mr, "rs-good", 10081, 10, 0, 2)

	reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	require.NoError(t, err)
	assert.Equal(t, "rs-good", reservation.ServiceID)

	// The key exists, so rs-bad is alive with a bad value: skip it, keep the
	// index field, let its next heartbeat repair the value
	assert.Contains(t, indexFieldNames(t, ts), "rs-bad")
}

func TestSelectAndReserve_OnlyCorruptServiceReturnsNoCapacity(t *testing.T) {
	ts, _, mr := setupTestSelector(t)

	seedRawService(t, mr, "rs-bad", "{not-json", 2)

	// Alive but unusable is saturation, not an empty registry
	_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
	assert.ErrorIs(t, err, ErrNoCapacity)
	assert.Contains(t, indexFieldNames(t, ts), "rs-bad")
}

func TestSelectAndReserve_NullNumericFieldsSkipped(t *testing.T) {
	// JSON null decodes to a truthy userdata: a bare comparison would raise and
	// abort the script rather than skipping the record
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"null capacity", `{"id":"rs-1","address":"127.0.0.1","port":10080,"capacity":null,"load":0}`},
		{"null port", `{"id":"rs-1","address":"127.0.0.1","port":null,"capacity":10,"load":0}`},
		{"null address", `{"id":"rs-1","address":null,"port":10080,"capacity":10,"load":0}`},
		{"empty address", `{"id":"rs-1","address":"","port":10080,"capacity":10,"load":0}`},
		{"json array", `[1,2,3]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, _, mr := setupTestSelector(t)
			seedRawService(t, mr, "rs-1", tc.raw, 3)

			_, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
			assert.ErrorIs(t, err, ErrNoCapacity)

			// Nothing was selected, so no tab may be held: a truncated reply would
			// have reserved one that no caller can ever release
			for i := 0; i < 3; i++ {
				assert.Equal(t, availableTab, mr.HGet(tabsKeyPrefix+"rs-1", strconv.Itoa(i)))
			}
		})
	}
}

func TestSelectAndReserve_AbsentLoadDefaultsToZero(t *testing.T) {
	// A missing or null load has always meant zero, so the service stays usable.
	// Only the comparison had to be made null-safe, not the semantics.
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"null load", `{"id":"rs-1","address":"127.0.0.1","port":10080,"capacity":10,"load":null}`},
		{"omitted load", `{"id":"rs-1","address":"127.0.0.1","port":10080,"capacity":10}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, _, mr := setupTestSelector(t)
			seedRawService(t, mr, "rs-1", tc.raw, 3)

			reservation, err := ts.SelectAndReserve(context.Background(), "req-1", types.SelectionStrategyLeastLoaded)
			require.NoError(t, err)
			assert.Equal(t, "rs-1", reservation.ServiceID)
			assert.Equal(t, 10080, reservation.Port)
		})
	}
}

func TestPruneIndexEntry_KeepsFieldWhenServiceIsLive(t *testing.T) {
	_, sr, mr := setupTestSelector(t)
	ctx := context.Background()

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)

	// Stands in for the lost update: a reader decided rs-1 was dead, and rs-1
	// re-registered before the prune landed. The conditional delete must no-op.
	sr.pruneIndexEntry(ctx, "rs-1")

	fields, err := sr.redis.HGetAll(ctx, serviceListKey)
	require.NoError(t, err)
	assert.Contains(t, fields, "rs-1", "a live service must survive a racing prune")
}

func TestPruneIndexEntry_RemovesFieldWhenServiceIsGone(t *testing.T) {
	_, sr, mr := setupTestSelector(t)
	ctx := context.Background()

	seedService(t, sr, mr, "rs-1", 10080, 10, 0, 3)
	mr.FastForward(RegistryTTL + time.Second)

	sr.pruneIndexEntry(ctx, "rs-1")

	fields, err := sr.redis.HGetAll(ctx, serviceListKey)
	require.NoError(t, err)
	assert.NotContains(t, fields, "rs-1")
}

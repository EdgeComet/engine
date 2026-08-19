package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/sharding"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

// schedulerTestEnv is a minimal CacheDaemon harness for drain tests.
// dispatchHook records each batch in dispatchOrder (preserving inter-iter
// ordering) and releases the per-host concurrency slot for every entry —
// the same release contract DistributeToEGs honours in production.
type schedulerTestEnv struct {
	daemon        *CacheDaemon
	mr            *miniredis.Miniredis
	mu            sync.Mutex
	dispatchOrder []InternalQueueEntry
	dispatchBatch [][]InternalQueueEntry // one slot per iter that fired the hook
	dispatchedBy  map[int]int            // hostID -> count
}

// schedulerTestHost is the input shape for newSchedulerEnv. dimensionID is
// the dimension all queued members reference; action defaults to Bypass so
// tests skip the RS budget gate (it's checked elsewhere).
type schedulerTestHost struct {
	id            int
	domain        string
	maxConcurrent int
	dimensionID   int
	action        types.URLRuleAction
}

// newTestHost builds a types.Host from the test input shape. Shared by
// newSchedulerEnv and resync tests that swap the config manager's host set.
func newTestHost(h schedulerTestHost) types.Host {
	action := h.action
	if action == "" {
		action = types.ActionBypass
	}
	return types.Host{
		ID:     h.id,
		Domain: h.domain,
		Dimensions: map[string]types.Dimension{
			"dim": {ID: h.dimensionID, Action: action},
		},
		Recache: &types.RecacheLimitConfig{MaxConcurrent: h.maxConcurrent},
	}
}

func newSchedulerEnv(t *testing.T, iqMaxSize int, hostsIn []schedulerTestHost) *schedulerTestEnv {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)

	hosts := make([]types.Host, 0, len(hostsIn))
	for _, h := range hostsIn {
		hosts = append(hosts, newTestHost(h))
	}

	configMgr := &mockConfigManager{hosts: hosts}
	keyGen := redis.NewKeyGenerator()
	iq := NewInternalQueue(iqMaxSize)
	limiter := NewHostConcurrencyLimiter(configMgr.GetConfig(), hosts)

	daemonCfg := &configtypes.CacheDaemonConfig{
		Scheduler:     configtypes.CacheDaemonScheduler{TickInterval: types.Duration(time.Second)},
		InternalQueue: configtypes.CacheDaemonInternalQueue{MaxSize: iqMaxSize, MaxRetries: 3},
		Recache:       configtypes.CacheDaemonRecache{RSCapacityReserved: 0.0, TimeoutPerURL: types.Duration(time.Second)},
	}

	d := &CacheDaemon{
		daemonConfig:       daemonCfg,
		configManager:      configMgr,
		redis:              redisClient,
		logger:             logger,
		internalQueue:      iq,
		rsRegistry:         registry.NewServiceRegistry(redisClient, logger),
		egRegistry:         sharding.NewRedisRegistry(redisClient, logger),
		normalizer:         hash.NewURLNormalizer(),
		keyGenerator:       keyGen,
		retryBaseDelay:     10 * time.Millisecond,
		concurrencyLimiter: limiter,
		schedulerDone:      make(chan struct{}),
		httpClient: &fasthttp.Client{
			ReadTimeout:     time.Duration(daemonCfg.Recache.TimeoutPerURL),
			WriteTimeout:    time.Duration(daemonCfg.Recache.TimeoutPerURL),
			MaxConnsPerHost: 256,
		},
	}
	d.reloadMu.Lock()
	d.rebuildHostByIDLocked()
	d.reloadMu.Unlock()

	env := &schedulerTestEnv{
		daemon:       d,
		mr:           mr,
		dispatchedBy: map[int]int{},
	}
	d.dispatchHook = func(batch []readyItem) {
		env.mu.Lock()
		copied := make([]InternalQueueEntry, 0, len(batch))
		for _, it := range batch {
			copied = append(copied, it.entry)
			env.dispatchOrder = append(env.dispatchOrder, it.entry)
			env.dispatchedBy[it.entry.HostID]++
		}
		env.dispatchBatch = append(env.dispatchBatch, copied)
		env.mu.Unlock()
		// Match DistributeToEGs' slot-release contract.
		for _, it := range batch {
			d.concurrencyLimiter.Release(it.slot)
		}
	}
	return env
}

// enqueueZSet pushes a RecacheMember onto recache:{hostID}:{priority}.
// score controls ZPopMin order and (for autorecache) due-time filtering.
func (env *schedulerTestEnv) enqueueZSet(t *testing.T, hostID int, priority string, dimensionID int, urls []string, baseScore float64) {
	t.Helper()
	ctx := context.Background()
	key := env.daemon.keyGenerator.RecacheQueueKey(hostID, priority)
	for i, u := range urls {
		m := types.RecacheMember{URL: u, DimensionID: dimensionID}
		raw, err := json.Marshal(m)
		require.NoError(t, err)
		require.NoError(t, env.daemon.redis.ZAdd(ctx, key, baseScore+float64(i), string(raw)))
	}
}

func (env *schedulerTestEnv) zcard(t *testing.T, hostID int, priority string) int64 {
	t.Helper()
	key := env.daemon.keyGenerator.RecacheQueueKey(hostID, priority)
	n, err := env.daemon.redis.ZCard(context.Background(), key)
	require.NoError(t, err)
	return n
}

func (env *schedulerTestEnv) totalDispatched() int {
	env.mu.Lock()
	defer env.mu.Unlock()
	return len(env.dispatchOrder)
}

func (env *schedulerTestEnv) dispatchedFor(hostID int) int {
	env.mu.Lock()
	defer env.mu.Unlock()
	return env.dispatchedBy[hostID]
}

// TestScheduler_DrainHighOnly (#1): a single host with only high-priority
// entries drains in ceil(N/max_concurrent) iters, and no Redis read touches
// the normal/autorecache keys (we assert by checking those keys were never
// created in miniredis).
func TestScheduler_DrainHighOnly(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxConcurrent = 5
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h1.test", maxConcurrent: maxConcurrent, dimensionID: dimID},
	})

	urls := make([]string, 25)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h1.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 25, env.totalDispatched(), "all 25 high-priority entries should have dispatched")
	assert.Equal(t, int64(0), env.zcard(t, hostID, redis.PriorityHigh), "high ZSET should be drained")

	// Normal/autorecache keys must never have been written to, since the host
	// never had a non-empty high *and* a normal at the same iter.
	assert.False(t, env.mr.Exists(env.daemon.keyGenerator.RecacheQueueKey(hostID, redis.PriorityNormal)))
	assert.False(t, env.mr.Exists(env.daemon.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)))

	// Iter count: pulls happen in batches of maxConcurrent. After each batch
	// dispatchHook releases slots, so the next iter can pull another batch.
	// Final iter pulls 0 (high empty) and the drain loop breaks. So we expect
	// ceil(25/5) batches = 5.
	assert.Equal(t, 5, len(env.dispatchBatch))
}

// TestScheduler_PriorityOrderWithinHost (#2): 5 each of high/normal/autorecache
// for one host drain in strict priority order: all-high, then all-normal,
// then all-autorecache.
func TestScheduler_PriorityOrderWithinHost(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h1.test", maxConcurrent: 5, dimensionID: dimID},
	})

	high := []string{"H0", "H1", "H2", "H3", "H4"}
	normal := []string{"N0", "N1", "N2", "N3", "N4"}
	auto := []string{"A0", "A1", "A2", "A3", "A4"}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, high, 0)
	env.enqueueZSet(t, hostID, redis.PriorityNormal, dimID, normal, 0)
	// All autorecache entries are due at score 0 (epoch); time.Now() is far ahead.
	env.enqueueZSet(t, hostID, redis.PriorityAutorecache, dimID, auto, 0)

	env.daemon.runOneTick(context.Background(), 1)

	require.Equal(t, 15, env.totalDispatched())
	for i := 0; i < 5; i++ {
		assert.Equal(t, high[i], env.dispatchOrder[i].URL, "first 5 should be high")
	}
	for i := 0; i < 5; i++ {
		assert.Equal(t, normal[i], env.dispatchOrder[5+i].URL, "next 5 should be normal")
	}
	for i := 0; i < 5; i++ {
		assert.Equal(t, auto[i], env.dispatchOrder[10+i].URL, "last 5 should be autorecache")
	}
}

// TestScheduler_NoCrossHostStarvation (#3): host A has 100 high, host B has
// 100 normal, both max_concurrent=5. Run several ticks and assert B drains
// at the same rate as A.
func TestScheduler_NoCrossHostStarvation(t *testing.T) {
	const hostA, hostB = 1, 2
	const dimID = 1
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: hostA, domain: "a.test", maxConcurrent: 5, dimensionID: dimID},
		{id: hostB, domain: "b.test", maxConcurrent: 5, dimensionID: dimID},
	})
	urlsA := make([]string, 100)
	urlsB := make([]string, 100)
	for i := 0; i < 100; i++ {
		urlsA[i] = fmt.Sprintf("https://a.test/p%d", i)
		urlsB[i] = fmt.Sprintf("https://b.test/p%d", i)
	}
	env.enqueueZSet(t, hostA, redis.PriorityHigh, dimID, urlsA, 0)
	env.enqueueZSet(t, hostB, redis.PriorityNormal, dimID, urlsB, 0)

	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 100, env.dispatchedBy[hostA], "host A high should fully drain")
	assert.Equal(t, 100, env.dispatchedBy[hostB], "host B normal should drain at the same cadence (no 60s gate)")
}

// TestScheduler_AtMostOnePriorityPerHostPerIter (#4): pullForHost returns
// after the first non-empty priority, even if the cap permits more.
func TestScheduler_AtMostOnePriorityPerHostPerIter(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, []string{"H0", "H1"}, 0)
	normal := make([]string, 100)
	for i := range normal {
		normal[i] = fmt.Sprintf("N%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityNormal, dimID, normal, 0)

	// Drive pullForHost directly so we observe iter-level pull behaviour
	// without ProcessInternalQueue's dispatch resetting iq.
	ctx := context.Background()
	nowUnix := time.Now().UTC().Unix()
	n, prio := env.daemon.pullForHost(ctx, hostID, 200, nowUnix, nil)
	assert.Equal(t, 2, n, "iter 1 should pull the 2 high entries")
	assert.Equal(t, redis.PriorityHigh, prio)
	assert.Equal(t, int64(100), env.zcard(t, hostID, redis.PriorityNormal), "normal must remain untouched")
	assert.Equal(t, 2, env.daemon.internalQueue.Size())

	// Drain iq so the next pull can take fresh concurrency.
	env.daemon.internalQueue.Dequeue(2)

	n, prio = env.daemon.pullForHost(ctx, hostID, 200, nowUnix, nil)
	assert.Equal(t, 5, n, "iter 2 should pull normal up to max_concurrent")
	assert.Equal(t, redis.PriorityNormal, prio)
}

// TestScheduler_AutorecacheDueFilter (#5): only entries with score <= now() pop.
func TestScheduler_AutorecacheDueFilter(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 10, dimensionID: dimID},
	})

	now := float64(time.Now().UTC().Unix())
	due := []string{"D0", "D1", "D2", "D3", "D4"}
	future := []string{"F0", "F1", "F2", "F3", "F4"}
	env.enqueueZSet(t, hostID, redis.PriorityAutorecache, dimID, due, now-60)
	env.enqueueZSet(t, hostID, redis.PriorityAutorecache, dimID, future, now+3600)

	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 5, env.totalDispatched(), "only 5 due entries should pop")
	assert.Equal(t, int64(5), env.zcard(t, hostID, redis.PriorityAutorecache), "5 future entries must remain in ZSET")
}

// TestScheduler_EmptyEverywhereProcessIQ (#6): zero pulls, but a backoff item
// seeded in iq still gets a chance via the tick-end ProcessInternalQueue.
func TestScheduler_EmptyEverywhereProcessIQ(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/seed",
		DimensionID: dimID,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 1, env.totalDispatched(), "tick-end ProcessInternalQueue must dispatch the seeded entry")
}

// TestScheduler_BackoffWakeUp (#7): an entry with NextRetryAfter in the past
// dispatches on this tick (no other queues active).
func TestScheduler_BackoffWakeUp(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	now := time.Now().UTC()
	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:         hostID,
		URL:            "https://h.test/retry",
		DimensionID:    dimID,
		RetryCount:     1,
		NextRetryAfter: now.Add(-time.Second), // past
	}))

	env.daemon.runOneTick(context.Background(), 1)
	assert.Equal(t, 1, env.totalDispatched(), "due backoff entry must dispatch")
}

// TestScheduler_ContextCancellationMidDrain (#8): pre-cancel ctx, runOneTick
// returns without entering the drain loop.
func TestScheduler_ContextCancellationMidDrain(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	urls := make([]string, 50)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		env.daemon.runOneTick(ctx, 1)
		close(done)
	}()

	select {
	case <-done:
		// Cancelled before drain executed any iter; iq must be empty.
		assert.Equal(t, 0, env.totalDispatched())
		assert.Equal(t, int64(50), env.zcard(t, hostID, redis.PriorityHigh), "no pulls; ZSET unchanged")
	case <-time.After(2 * time.Second):
		t.Fatal("runOneTick blocked despite cancelled context")
	}
}

// TestScheduler_DurabilityPreCheck (#9): a host fully saturated on concurrency
// keeps its Redis backlog untouched. After releasing slots, the next iter
// pulls.
func TestScheduler_DurabilityPreCheck(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 5
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID},
	})
	urls := make([]string, 100)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	// Saturate all 5 slots before calling pullForHost.
	heldSlots := make([]Slot, 0, maxC)
	for i := 0; i < maxC; i++ {
		s, ok := env.daemon.concurrencyLimiter.TryAcquire(hostID)
		require.True(t, ok)
		heldSlots = append(heldSlots, s)
	}

	nowUnix := time.Now().UTC().Unix()
	n, prio := env.daemon.pullForHost(context.Background(), hostID, 200, nowUnix, nil)
	assert.Equal(t, 0, n, "no slot free → 0 pulled (entries stay durable in Redis)")
	assert.Equal(t, "", prio)
	assert.Equal(t, int64(100), env.zcard(t, hostID, redis.PriorityHigh), "ZSET unchanged")
	assert.Equal(t, 0, env.daemon.internalQueue.Size())

	// Release the saturating slots; next pull succeeds.
	for _, s := range heldSlots {
		env.daemon.concurrencyLimiter.Release(s)
	}
	n, prio = env.daemon.pullForHost(context.Background(), hostID, 200, nowUnix, nil)
	assert.Equal(t, maxC, n, "after release, next iter pulls one batch")
	assert.Equal(t, redis.PriorityHigh, prio)
}

// TestScheduler_RotatingCursorNoStarvation (#10): with iq capped tight, the
// cursor must rotate so all hosts eventually dispatch.
func TestScheduler_RotatingCursorNoStarvation(t *testing.T) {
	const numHosts = 20
	const dimID = 1
	hosts := make([]schedulerTestHost, numHosts)
	for i := 0; i < numHosts; i++ {
		hosts[i] = schedulerTestHost{
			id:            i + 1,
			domain:        fmt.Sprintf("h%d.test", i+1),
			maxConcurrent: 5,
			dimensionID:   dimID,
		}
	}
	env := newSchedulerEnv(t, 10, hosts)

	for _, h := range hosts {
		urls := make([]string, 100)
		for j := range urls {
			urls[j] = fmt.Sprintf("https://%s/p%d", h.domain, j)
		}
		env.enqueueZSet(t, h.id, redis.PriorityHigh, dimID, urls, 0)
	}

	for tick := 1; tick <= 20; tick++ {
		env.daemon.runOneTick(context.Background(), tick)
	}

	for _, h := range hosts {
		assert.Greater(t, env.dispatchedBy[h.id], 0, "host %d should have dispatched within 20 ticks", h.id)
	}
}

// TestScheduler_CursorNoOpWhenSlack (#11): 5 hosts × 5 entries, iq has lots
// of slack — one iter visits all 5 hosts, so cursor advances by 5 mod 5 = 0.
func TestScheduler_CursorNoOpWhenSlack(t *testing.T) {
	const numHosts = 5
	const dimID = 1
	hosts := make([]schedulerTestHost, numHosts)
	for i := 0; i < numHosts; i++ {
		hosts[i] = schedulerTestHost{
			id:            i + 1,
			domain:        fmt.Sprintf("h%d.test", i+1),
			maxConcurrent: 5,
			dimensionID:   dimID,
		}
	}
	env := newSchedulerEnv(t, 100, hosts)
	for _, h := range hosts {
		urls := make([]string, 5)
		for j := range urls {
			urls[j] = fmt.Sprintf("https://%s/p%d", h.domain, j)
		}
		env.enqueueZSet(t, h.id, redis.PriorityHigh, dimID, urls, 0)
	}

	cursorBefore := env.daemon.hostCursor
	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, cursorBefore, env.daemon.hostCursor, "cursor should be unchanged: 5 visited mod 5 = 0")
	assert.Equal(t, 25, env.totalDispatched())
}

// TestScheduler_NoHostsPanicGuard (#12): empty host list does not panic; the
// tick-end ProcessInternalQueue still dispatches a seeded iq entry.
func TestScheduler_NoHostsPanicGuard(t *testing.T) {
	env := newSchedulerEnv(t, 100, nil)

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      99, // No matching host — would be discarded by actionForEntry
		URL:         "https://orphan.test/p",
		DimensionID: 1,
		QueuedAt:    time.Now().UTC(),
	}))

	cursorBefore := env.daemon.hostCursor

	require.NotPanics(t, func() {
		for tick := 1; tick <= 3; tick++ {
			env.daemon.runOneTick(context.Background(), tick)
		}
	})

	assert.Equal(t, cursorBefore, env.daemon.hostCursor)
	// The orphan entry is discarded by actionForEntry (unresolved host),
	// not dispatched. That's correct: tick-end ProcessInternalQueue ran.
	assert.Equal(t, 0, env.totalDispatched())
	assert.Equal(t, 0, env.daemon.internalQueue.Size(), "orphan entry should be drained out (discarded) by ProcessInternalQueue")
}

// TestScheduler_RSBudgetStall_NoOverPull (#13): regression for the
// "drain keeps pulling under RS-budget saturation" bug. With render-mode
// dimensions and no healthy RS instances, every dispatch attempt is
// deferred via the RS-budget gate (slots released, entries re-enqueued).
// The drain loop must detect zero forward progress after a non-empty pull
// and break — otherwise it would keep moving durable Redis entries into
// volatile iq until iq fills. Acceptable leakage per tick: a single iter's
// worth of pulls (≤ MaxConcurrent per host).
func TestScheduler_RSBudgetStall_NoOverPull(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 5
	const iqMax = 1000
	const seeded = 200

	env := newSchedulerEnv(t, iqMax, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID, action: types.ActionRender},
	})
	urls := make([]string, seeded)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	// rsRegistry is empty in the test env -> CalculateAvailableCapacity = 0
	// -> every render entry is RS-budget-deferred.
	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 0, env.totalDispatched(), "no entries should reach the dispatchHook when RS budget is 0")

	// Per-iter leakage bound: each pullForHost call pulls up to
	// MaxConcurrent items per host (durability pre-check sees free slots
	// after the prior iter's release-on-RS-defer). Before the drain breaks
	// it executes the first iter that pulled (and dispatched 0), so iq holds
	// at most that one iter's worth of entries. Tight bound: ≤ maxC.
	assert.LessOrEqual(t, env.daemon.internalQueue.Size(), maxC,
		"iq should hold at most one iter's worth of RS-deferred entries (got %d)", env.daemon.internalQueue.Size())

	// Most of the Redis backlog must still be durable.
	assert.GreaterOrEqual(t, env.zcard(t, hostID, redis.PriorityHigh), int64(seeded-maxC),
		"durable Redis backlog should be largely intact under RS saturation")
}

// TestScheduler_MixedBatchRSBudgetStall (#14): regression for two related
// guarantees:
//
//  1. Durability under RS saturation: host A (render, RS-gated by an empty
//     RS registry) must not leak its durable Redis backlog into volatile iq
//     beyond one iter's MaxConcurrent. After A's first iter defers, the
//     per-host skip set keeps A out of subsequent iters this tick.
//
//  2. Per-host independence: host B (bypass) shares the same tick as A but
//     does not depend on RS budget. B must fully drain its Redis backlog —
//     it should not be tick-throttled by A's RS-gated state. A global
//     "break on any defer" check would weaken this; the per-host skip keeps
//     it intact.
func TestScheduler_MixedBatchRSBudgetStall(t *testing.T) {
	const hostA, hostB = 1, 2
	const dimRender, dimBypass = 1, 2
	const maxC = 5
	const iqMax = 1000
	const seeded = 100

	env := newSchedulerEnv(t, iqMax, []schedulerTestHost{
		{id: hostA, domain: "a.test", maxConcurrent: maxC, dimensionID: dimRender, action: types.ActionRender},
		{id: hostB, domain: "b.test", maxConcurrent: maxC, dimensionID: dimBypass, action: types.ActionBypass},
	})

	urlsA := make([]string, seeded)
	urlsB := make([]string, seeded)
	for i := 0; i < seeded; i++ {
		urlsA[i] = fmt.Sprintf("https://a.test/p%d", i)
		urlsB[i] = fmt.Sprintf("https://b.test/p%d", i)
	}
	env.enqueueZSet(t, hostA, redis.PriorityHigh, dimRender, urlsA, 0)
	env.enqueueZSet(t, hostB, redis.PriorityHigh, dimBypass, urlsB, 0)

	// rsRegistry is empty → CalculateAvailableCapacity = 0 → A's render
	// entries hit the RS-budget defer path; B's bypass entries skip the
	// gate entirely.
	env.daemon.runOneTick(context.Background(), 1)

	// (1) Durability: A's render entries never dispatch and at most one
	// iter's worth (MaxConcurrent) leaks from Redis into iq.
	assert.Equal(t, 0, env.dispatchedBy[hostA], "host A render entries must not dispatch with RS budget 0")
	assert.LessOrEqual(t, env.daemon.internalQueue.Size(), maxC,
		"iq leak must be bounded to one iter's pull (got %d)", env.daemon.internalQueue.Size())
	assert.GreaterOrEqual(t, env.zcard(t, hostA, redis.PriorityHigh), int64(seeded-maxC),
		"host A render backlog should be largely intact (got %d in Redis)", env.zcard(t, hostA, redis.PriorityHigh))

	// (2) Per-host independence: B fully drains. A's RS-gated state must
	// not throttle B's bypass throughput to one iter per tick.
	assert.Equal(t, seeded, env.dispatchedBy[hostB],
		"host B bypass entries should fully drain across iters within a tick (got %d / %d)", env.dispatchedBy[hostB], seeded)
	assert.Equal(t, int64(0), env.zcard(t, hostB, redis.PriorityHigh),
		"host B Redis queue should be empty after the tick")
}

// TestScheduler_CtxCancelSkipsTickEnd (#15): regression for the
// "post-cancel work" bug. After ctx is cancelled inside the drain loop the
// tick-end ProcessInternalQueue must NOT run. Dispatch is detached from the
// tick now, so the concern is no longer a synchronous wg.Wait stretching
// shutdown latency; it is that a cancelled tick must not move durable Redis
// state into the volatile iq (or spawn fresh dispatch work) after cancel —
// Shutdown joins the scheduler and drains exactly the in-flight set, so the
// tick must stop cleanly at the cancel checkpoint.
//
// We seed iq with a directly-dispatchable entry, then cancel ctx and call
// runOneTick. If the early return is honoured, the seeded entry stays in iq
// (not dispatched). Without the fix, tick-end ProcessInternalQueue would
// dispatch it via the dispatchHook.
func TestScheduler_CtxCancelSkipsTickEnd(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/seed",
		DimensionID: dimID,
		QueuedAt:    time.Now().UTC(),
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env.daemon.runOneTick(ctx, 1)

	assert.Equal(t, 0, env.totalDispatched(), "cancelled tick must not dispatch anything")
	assert.Equal(t, 1, env.daemon.internalQueue.Size(), "seeded entry must remain in iq, not be drained by tick-end ProcessInternalQueue")
}

// TestScheduler_RuntimeHostAdd_Resyncs: a host added to the config manager's host
// set out-of-band (no POST /internal/reload) must be picked up by the next tick,
// so its recache entries dispatch instead of being discarded as "unresolved host".
// Regression for the stale-hostByID silent-drop bug. The first tick seeds the
// change-detection marker with the {host1} set (the harness skips NewCacheDaemon's
// marker init); the assertion then proves dispatch happens on the SECOND tick via
// genuine change-detection, not an unconditional cold-start resync.
func TestScheduler_RuntimeHostAdd_Resyncs(t *testing.T) {
	const (
		host1 = 1
		host2 = 2
		dim1  = 1
		dim2  = 2
	)
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: host1, domain: "h1.test", maxConcurrent: 5, dimensionID: dim1},
	})

	env.daemon.runOneTick(context.Background(), 1)
	require.Equal(t, 0, env.totalDispatched(), "no host1 work queued yet")

	// Add host2 out-of-band: a brand-new slice, no reload call.
	cm := env.daemon.configManager.(*mockConfigManager)
	cm.hosts = []types.Host{
		newTestHost(schedulerTestHost{id: host1, domain: "h1.test", maxConcurrent: 5, dimensionID: dim1}),
		newTestHost(schedulerTestHost{id: host2, domain: "h2.test", maxConcurrent: 5, dimensionID: dim2}),
	}

	urls := []string{"https://h2.test/a", "https://h2.test/b", "https://h2.test/c"}
	env.enqueueZSet(t, host2, redis.PriorityNormal, dim2, urls, 0)

	env.daemon.runOneTick(context.Background(), 2)

	assert.Equal(t, len(urls), env.dispatchedFor(host2), "host2 entries must dispatch after the out-of-band add")
	assert.Equal(t, int64(0), env.zcard(t, host2, redis.PriorityNormal), "host2 queue must be drained as work, not discarded")
}

// TestScheduler_RuntimeHostRemove_StillDiscards: the resync also applies removals.
// An entry already in the internal queue for a host dropped from config out-of-band
// is discarded (not dispatched), and the discard path still fires after the resync.
func TestScheduler_RuntimeHostRemove_StillDiscards(t *testing.T) {
	const (
		host1 = 1
		dim1  = 1
	)
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: host1, domain: "h1.test", maxConcurrent: 5, dimensionID: dim1},
	})

	env.daemon.runOneTick(context.Background(), 1) // seed marker with {host1}

	// Drop host1 out-of-band and seed an iq entry for it directly (the pull path
	// would no longer pull a now-unconfigured host from Redis).
	cm := env.daemon.configManager.(*mockConfigManager)
	cm.hosts = nil
	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      host1,
		URL:         "https://h1.test/stale",
		DimensionID: dim1,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.runOneTick(context.Background(), 2)

	assert.Equal(t, 0, env.totalDispatched(), "entry for a removed host must be discarded, not dispatched")
	assert.Equal(t, 0, env.daemon.internalQueue.Size(), "discarded entry must be drained out of iq")
}

// TestDaemon_ActionForEntry_AfterResync: the resync helper makes a newly added
// host resolvable by actionForEntry and applies its concurrency override (proving
// the paired limiter resync ran, not just the hostByID rebuild).
func TestDaemon_ActionForEntry_AfterResync(t *testing.T) {
	const (
		host2        = 2
		dim2         = 2
		host2MaxConc = 7
	)
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: 1, domain: "h1.test", maxConcurrent: 5, dimensionID: 1},
	})

	entry := InternalQueueEntry{HostID: host2, DimensionID: dim2}

	env.daemon.reloadMu.RLock()
	before := env.daemon.actionForEntry(entry)
	env.daemon.reloadMu.RUnlock()
	require.Equal(t, types.URLRuleAction(""), before, "host2 unknown before resync")

	cm := env.daemon.configManager.(*mockConfigManager)
	cm.hosts = []types.Host{
		newTestHost(schedulerTestHost{id: 1, domain: "h1.test", maxConcurrent: 5, dimensionID: 1}),
		newTestHost(schedulerTestHost{id: host2, domain: "h2.test", maxConcurrent: host2MaxConc, dimensionID: dim2, action: types.ActionRender}),
	}

	env.daemon.maybeResyncDerivedCaches()

	env.daemon.reloadMu.RLock()
	after := env.daemon.actionForEntry(entry)
	env.daemon.reloadMu.RUnlock()
	assert.Equal(t, types.ActionRender, after, "host2 resolves to its dimension action after resync")
	assert.Equal(t, host2MaxConc, env.daemon.concurrencyLimiter.MaxConcurrent(host2), "host2 concurrency override applied by the paired limiter resync")
}

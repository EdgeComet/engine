package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/cachedaemon/metrics"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

// testEGRegistryKeyPrefix and testEGRegistryIndexKey mirror
// sharding.registryKeyPrefix and sharding.registryIndexKey. Duplicated here
// because those consts are unexported; GetHealthyEGs reads the EG IDs from the
// index hash and then loads each EG's key under this prefix.
const (
	testEGRegistryKeyPrefix = "registry:eg:"
	testEGRegistryIndexKey  = "registry:eg-index"
)

// fakeEG is an in-process stand-in for an Edge Gateway's
// /internal/cache/recache endpoint. Each request records its URL; a request
// whose URL satisfies the stuck predicate blocks until release is closed,
// modelling a straggler that holds its concurrency slot.
type fakeEG struct {
	srv     *httptest.Server
	address string

	mu       sync.Mutex
	received []string

	release chan struct{}
	relOnce sync.Once
}

func newFakeEG(t *testing.T, stuck func(url string) bool) *fakeEG {
	t.Helper()
	f := &fakeEG{release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(body, &req)

		f.mu.Lock()
		f.received = append(f.received, req.URL)
		f.mu.Unlock()

		if stuck != nil && stuck(req.URL) {
			<-f.release
		}
		w.WriteHeader(http.StatusOK)
	}))
	f.address = strings.TrimPrefix(f.srv.URL, "http://")
	t.Cleanup(func() {
		f.releaseAll()
		f.srv.Close()
	})
	return f
}

func (f *fakeEG) releaseAll() { f.relOnce.Do(func() { close(f.release) }) }

func (f *fakeEG) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

func (f *fakeEG) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.received))
	copy(out, f.received)
	return out
}

// enableAsyncDispatch removes the synchronous test hook so ProcessInternalQueue
// takes the real detached DistributeToEGs path.
func (env *schedulerTestEnv) enableAsyncDispatch() { env.daemon.dispatchHook = nil }

// registerEG writes a healthy EG registry entry pointing at address (host:port).
func (env *schedulerTestEnv) registerEG(t *testing.T, egID, address string) {
	t.Helper()
	info := types.EGInfo{
		EgID:            egID,
		Address:         address,
		ShardingEnabled: true,
		LastHeartbeat:   time.Now().UTC(),
	}
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	require.NoError(t, env.daemon.redis.Set(context.Background(), testEGRegistryKeyPrefix+egID, string(raw), time.Minute))
	require.NoError(t, env.daemon.redis.HSet(context.Background(), testEGRegistryIndexKey, egID, address))
}

// registerRS registers a healthy render service so CalculateAvailableCapacity
// returns a non-zero budget.
func (env *schedulerTestEnv) registerRS(t *testing.T, id string, capacity, load int) {
	t.Helper()
	require.NoError(t, env.daemon.rsRegistry.RegisterService(context.Background(), &registry.ServiceInfo{
		ID:       id,
		Address:  "127.0.0.1",
		Port:     1,
		Capacity: capacity,
		Load:     load,
	}))
}

func (env *schedulerTestEnv) attachMetrics() *metrics.MetricsCollector {
	mc := metrics.NewMetricsCollector("test", env.daemon.logger)
	env.daemon.metricsCollector = mc
	return mc
}

// inFlightRenders reads the atomic render counter.
func (env *schedulerTestEnv) inFlightRenders() int64 {
	return atomic.LoadInt64(&env.daemon.inFlightRenders)
}

// waitFor polls cond until true or the deadline, failing the test on timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timeout after %s waiting for: %s", timeout, msg)
}

func scrapeMetrics(t *testing.T, mc *metrics.MetricsCollector) string {
	t.Helper()
	var ctx fasthttp.RequestCtx
	ctx.Request.Header.SetMethod(http.MethodGet)
	ctx.Request.SetRequestURI("/metrics")
	mc.ServeHTTP(&ctx)
	return string(ctx.Response.Body())
}

// drainDispatches releases any stuck EG and waits for all detached dispatch
// goroutines to finish, so no goroutine outlives the test and races teardown.
func (env *schedulerTestEnv) drainDispatches(eg *fakeEG) {
	if eg != nil {
		eg.releaseAll()
	}
	env.daemon.dispatchWG.Wait()
}

// --- async dispatch tests ---

// TestAsyncDispatch_HOLRegression (core proof): one straggler URL on host A
// holds exactly one of its three slots for the whole test while the other two
// slots keep cycling, and host B keeps dispatching the entire time.
func TestAsyncDispatch_HOLRegression(t *testing.T) {
	const hostA, hostB = 1, 2
	const dimID = 1
	const maxC = 3
	env := newSchedulerEnv(t, 1000, []schedulerTestHost{
		{id: hostA, domain: "a.test", maxConcurrent: maxC, dimensionID: dimID},
		{id: hostB, domain: "b.test", maxConcurrent: maxC, dimensionID: dimID},
	})
	env.enableAsyncDispatch()

	eg := newFakeEG(t, func(url string) bool { return strings.Contains(url, "STUCK") })
	env.registerEG(t, "eg1", eg.address)

	// Host A: the straggler first (lowest score, pulled in the first batch),
	// then many fast URLs behind it.
	aURLs := []string{"https://a.test/STUCK"}
	for i := 0; i < 90; i++ {
		aURLs = append(aURLs, fmt.Sprintf("https://a.test/fast%d", i))
	}
	env.enqueueZSet(t, hostA, redis.PriorityHigh, dimID, aURLs, 0)

	bURLs := make([]string, 90)
	for i := range bURLs {
		bURLs[i] = fmt.Sprintf("https://b.test/fast%d", i)
	}
	env.enqueueZSet(t, hostB, redis.PriorityHigh, dimID, bURLs, 0)

	ctx := context.Background()
	for tick := 1; tick <= 12; tick++ {
		env.daemon.runOneTick(ctx, tick)
		// Let this tick's fast dispatches complete: host A settles back to the
		// single stuck slot, and host B fully releases its slots.
		waitFor(t, 3*time.Second, func() bool {
			return env.daemon.concurrencyLimiter.Stats(hostA).InFlight <= 1 &&
				env.daemon.concurrencyLimiter.Stats(hostB).InFlight == 0
		}, "fast dispatches should drain between ticks")
	}

	// The straggler occupies exactly one of host A's three slots.
	assert.Equal(t, int64(1), env.daemon.concurrencyLimiter.Stats(hostA).InFlight,
		"straggler holds exactly one slot")

	var aFast, bHits int
	for _, u := range eg.snapshot() {
		switch {
		case strings.Contains(u, "a.test/fast"):
			aFast++
		case strings.Contains(u, "b.test/fast"):
			bHits++
		}
	}
	// With one slot stuck, the other two cycled across 12 ticks (>= 2 per tick).
	assert.GreaterOrEqual(t, aFast, 2*maxC,
		"host A's non-straggler slots kept cycling despite the stuck slot (got %d)", aFast)
	assert.GreaterOrEqual(t, bHits, 2*maxC,
		"host B kept dispatching while host A had a stuck slot (got %d)", bHits)

	env.drainDispatches(eg)
}

// TestAsyncDispatch_ConcurrencyCapUnderRace: under sustained overlapping async
// dispatch, per-host in-flight never exceeds max_concurrent. Run with -race to
// also exercise the concurrent Stats reads vs the atomic slot counters.
func TestAsyncDispatch_ConcurrencyCapUnderRace(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 4
	env := newSchedulerEnv(t, 2000, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID},
	})
	env.enableAsyncDispatch()

	// Fast EG but each request holds briefly so dispatches from successive ticks
	// overlap in flight.
	eg := newFakeEG(t, nil)
	env.registerEG(t, "eg1", eg.address)

	const total = 300
	urls := make([]string, total)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	stop := make(chan struct{})
	var maxObserved int64
	var sampler sync.WaitGroup
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		for {
			select {
			case <-stop:
				return
			default:
				inf := env.daemon.concurrencyLimiter.Stats(hostID).InFlight
				for {
					cur := atomic.LoadInt64(&maxObserved)
					if inf <= cur || atomic.CompareAndSwapInt64(&maxObserved, cur, inf) {
						break
					}
				}
			}
		}
	}()

	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for tick := 1; eg.count() < total && time.Now().Before(deadline); tick++ {
		env.daemon.runOneTick(ctx, tick)
		time.Sleep(3 * time.Millisecond)
	}

	close(stop)
	sampler.Wait()
	env.drainDispatches(eg)

	assert.LessOrEqual(t, atomic.LoadInt64(&maxObserved), int64(maxC),
		"per-host in-flight must never exceed max_concurrent")
	assert.Equal(t, total, eg.count(), "all URLs eventually dispatched")
}

// TestAsyncDispatch_ShutdownDrainCancelMidTick: a live scheduler with an
// in-flight blocked dispatch is shut down; Shutdown joins Run, drains the
// dispatch within budget, and returns cleanly with no WaitGroup race.
func TestAsyncDispatch_ShutdownDrainCancelMidTick(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 5
	env := newSchedulerEnv(t, 1000, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	env.daemon.daemonConfig.Scheduler.TickInterval = types.Duration(10 * time.Millisecond)

	eg := newFakeEG(t, func(string) bool { return true }) // every request blocks
	env.registerEG(t, "eg1", eg.address)

	urls := make([]string, 20)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	require.NoError(t, env.daemon.Start(context.Background()))

	// Wait until the scheduler has dispatched a full batch that is now stuck.
	waitFor(t, 3*time.Second, func() bool {
		return env.daemon.concurrencyLimiter.Stats(hostID).InFlight == int64(maxC)
	}, "scheduler should fill the host's slots with stuck dispatches")

	// Release the stuck EG shortly after Shutdown starts so the drain completes
	// within budget; this exercises the join-then-drain path while ticks may
	// still be firing (so dispatchWG.Add can race dispatchWG.Wait unless the
	// scheduler is joined first).
	go func() {
		time.Sleep(30 * time.Millisecond)
		eg.releaseAll()
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, env.daemon.Shutdown(shutdownCtx))
	assert.Less(t, time.Since(start), 5*time.Second, "drain completed within budget")

	// Run returned (schedulerDone closed) and all dispatches drained.
	select {
	case <-env.daemon.schedulerDone:
	default:
		t.Fatal("schedulerDone should be closed after Shutdown")
	}
	assert.Equal(t, int64(0), env.daemon.concurrencyLimiter.Stats(hostID).InFlight,
		"all slots released after drain")
}

// TestAsyncDispatch_ShutdownExpiredCtxStillJoinsAndFlushes: entering Shutdown
// with an already-expired ctx must still join the scheduler via the safety
// timer (not fall through to a racy Wait), then drain (ctx already done ->
// immediate) and still flush the internal queue to Redis.
func TestAsyncDispatch_ShutdownExpiredCtxStillJoinsAndFlushes(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 5
	env := newSchedulerEnv(t, 1000, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	env.daemon.daemonConfig.Scheduler.TickInterval = types.Duration(10 * time.Millisecond)

	eg := newFakeEG(t, func(string) bool { return true })
	env.registerEG(t, "eg1", eg.address)

	urls := make([]string, maxC)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	// Seed iq with deferred (backoff) entries that the tick keeps re-enqueuing;
	// these must survive shutdown by being flushed to Redis.
	const flushN = 3
	for i := 0; i < flushN; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:         hostID,
			URL:            fmt.Sprintf("https://h.test/deferred%d", i),
			DimensionID:    dimID,
			Priority:       redis.PriorityNormal,
			QueuedAt:       time.Now().UTC(),
			NextRetryAfter: time.Now().UTC().Add(time.Hour), // never due during the test
		}))
	}

	require.NoError(t, env.daemon.Start(context.Background()))
	waitFor(t, 3*time.Second, func() bool {
		return env.daemon.concurrencyLimiter.Stats(hostID).InFlight == int64(maxC)
	}, "scheduler should fill slots with stuck dispatches")

	expired, cancel := context.WithCancel(context.Background())
	cancel() // already expired

	require.NoError(t, env.daemon.Shutdown(expired))

	// Joined the scheduler despite the expired ctx.
	select {
	case <-env.daemon.schedulerDone:
	default:
		t.Fatal("schedulerDone should be closed: join must not fall through on expired ctx")
	}

	// The deferred iq entries were flushed to Redis even though the drain timed
	// out immediately on the expired ctx.
	assert.Equal(t, int64(flushN), env.zcard(t, hostID, redis.PriorityNormal),
		"deferred internal-queue entries must be flushed to Redis on shutdown")

	env.drainDispatches(eg)
}

// TestAsyncDispatch_ShutdownJoinTimeoutAbortsFlush: a Run that never returns
// must be abandoned within schedulerJoinTimeout, returning an error and
// skipping the drain and flush (no WaitGroup race, no lossy flush).
func TestAsyncDispatch_ShutdownJoinTimeoutAbortsFlush(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	// Simulate a pathological non-returning Run: schedulerDone is never closed,
	// and the join timer is tiny so the test is fast.
	env.daemon.schedulerDone = make(chan struct{})
	env.daemon.schedulerJoinTimeout = 50 * time.Millisecond

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/stuck",
		DimensionID: dimID,
		Priority:    redis.PriorityNormal,
		QueuedAt:    time.Now().UTC(),
	}))

	expired, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := env.daemon.Shutdown(expired)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler did not stop")
	assert.Less(t, elapsed, 2*time.Second, "abort happens promptly on the join timer")
	// Flush was skipped: iq untouched, Redis empty.
	assert.Equal(t, 1, env.daemon.internalQueue.Size(), "iq must be left intact when the flush is abandoned")
	assert.Equal(t, int64(0), env.zcard(t, hostID, redis.PriorityNormal), "no flush on the join-timeout path")
}

// TestShutdownFlush_ReenqueuesIqToRedis: entries left in iq at shutdown are
// re-pushed to their priority ZSET.
func TestShutdownFlush_ReenqueuesIqToRedis(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	const n = 7
	for i := 0; i < n; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/p%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	env.daemon.flushInternalQueueToRedis()

	assert.Equal(t, int64(n), env.zcard(t, hostID, redis.PriorityHigh), "all iq entries flushed to Redis")
	assert.Equal(t, 0, env.daemon.internalQueue.Size(), "iq drained by flush")
}

// TestShutdownFlush_FailedDispatchDuringDrainFlushed: a dispatch that fails
// (no healthy EGs) re-enqueues to iq during the detached path; a subsequent
// flush re-pushes it to Redis rather than losing it.
func TestShutdownFlush_FailedDispatchDuringDrainFlushed(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	// No EG registered -> GetHealthyEGs empty -> releaseAndReenqueue.

	const n = 5
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	env.daemon.runOneTick(context.Background(), 1)
	env.daemon.dispatchWG.Wait() // let the failed dispatch re-enqueue to iq

	require.Equal(t, n, env.daemon.internalQueue.Size(), "failed dispatch re-enqueued all entries to iq")

	env.daemon.flushInternalQueueToRedis()
	assert.Equal(t, int64(n), env.zcard(t, hostID, redis.PriorityHigh), "re-enqueued entries flushed back to Redis, not lost")
}

// TestRenderCounter_PairsAndZeroes: render dispatches increment the
// in-flight render counter while blocked and return it to zero once they
// complete.
func TestRenderCounter_PairsAndZeroes(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 10, dimensionID: dimID, action: types.ActionRender},
	})
	env.enableAsyncDispatch()
	env.registerRS(t, "rs1", 100, 0) // ample render budget
	eg := newFakeEG(t, func(string) bool { return true })
	env.registerEG(t, "eg1", eg.address)

	const n = 5
	for i := 0; i < n; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/r%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	env.daemon.ProcessInternalQueue()
	waitFor(t, 3*time.Second, func() bool { return env.inFlightRenders() == int64(n) },
		"in-flight render counter should rise to the dispatched count")

	eg.releaseAll()
	env.daemon.dispatchWG.Wait()
	assert.Equal(t, int64(0), env.inFlightRenders(), "counter returns to zero once renders complete")
}

// TestRenderCounter_IsRenderCapturedAtGateAcrossReload: isRender is captured at gate
// time, so a config reload that flips the dimension render->bypass while the
// dispatch is in flight cannot unbalance the in-flight render counter. If the
// decrement re-read config instead of the captured flag, the counter would leak
// (stuck above 0) after the flip; asserting it returns to 0 proves capture-at-gate.
func TestRenderCounter_IsRenderCapturedAtGateAcrossReload(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 10, dimensionID: dimID, action: types.ActionRender},
	})
	env.enableAsyncDispatch()
	env.registerRS(t, "rs1", 100, 0)
	eg := newFakeEG(t, func(string) bool { return true }) // hold renders in flight
	env.registerEG(t, "eg1", eg.address)

	const n = 4
	for i := 0; i < n; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/r%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	// Gate the batch as RENDER (isRender=true captured on each readyItem).
	env.daemon.ProcessInternalQueue()
	waitFor(t, 3*time.Second, func() bool { return env.inFlightRenders() == int64(n) },
		"render counter rises to the dispatched count")

	// Flip the dimension render -> bypass mid-flight, as a reload would.
	hosts := env.daemon.configManager.GetHosts()
	hosts[0].Dimensions["dim"] = types.Dimension{ID: dimID, Action: types.ActionBypass}
	env.daemon.reloadMu.Lock()
	env.daemon.rebuildHostByIDLocked()
	env.daemon.reloadMu.Unlock()
	env.daemon.reloadMu.RLock()
	gotAction := env.daemon.actionForEntry(InternalQueueEntry{HostID: hostID, DimensionID: dimID})
	env.daemon.reloadMu.RUnlock()
	require.Equal(t, types.ActionBypass, gotAction,
		"sanity: the reload took effect for new resolutions")

	// Complete the in-flight renders. The decrement must use the captured flag.
	eg.releaseAll()
	env.daemon.dispatchWG.Wait()
	assert.Equal(t, int64(0), env.inFlightRenders(),
		"counter must return to 0: decrement keys off gate-captured isRender, not reloaded config")
}

// TestAsyncDispatch_PerURLPanicContained: a panic inside the per-URL dispatch
// goroutine (here forced by a nil HTTP client) is contained by the recover, so
// the process does not crash, the concurrency slot is still released, and the
// entry is re-enqueued for retry rather than lost.
func TestAsyncDispatch_PerURLPanicContained(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	env.registerEG(t, "eg1", "127.0.0.1:1") // address never dialed; client panics first
	env.daemon.httpClient = nil             // force a nil-deref panic inside SendRecacheRequest

	const n = 3
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h.test/p%d", i)
	}
	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, urls, 0)

	require.NotPanics(t, func() {
		env.daemon.runOneTick(context.Background(), 1)
		env.daemon.dispatchWG.Wait()
	}, "a per-URL dispatch panic must be contained, not crash the process")

	assert.Equal(t, int64(0), env.daemon.concurrencyLimiter.Stats(hostID).InFlight,
		"slots released even though every dispatch panicked")
	assert.Equal(t, n, env.daemon.internalQueue.Size(),
		"panicked dispatches re-enqueue for retry instead of being lost")
}

// TestRenderCounter_NoHealthyEGNoIncrement: the no-healthy-EG batch path releases
// slots without spawning the per-URL goroutine, so the render counter is never
// touched (no leak, no spurious decrement).
func TestRenderCounter_NoHealthyEGNoIncrement(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 10, dimensionID: dimID, action: types.ActionRender},
	})
	env.enableAsyncDispatch()
	env.registerRS(t, "rs1", 100, 0) // budget available; no EG registered

	for i := 0; i < 5; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/r%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	assert.Equal(t, int64(0), env.inFlightRenders(),
		"no-healthy-EG path must not touch the render counter")
}

// TestRenderCounter_BypassDoesNotIncrement: bypass dispatches never touch the render
// counter.
func TestRenderCounter_BypassDoesNotIncrement(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 10, dimensionID: dimID, action: types.ActionBypass},
	})
	env.enableAsyncDispatch()
	eg := newFakeEG(t, nil)
	env.registerEG(t, "eg1", eg.address)

	for i := 0; i < 5; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/b%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	assert.Equal(t, int64(0), env.inFlightRenders(), "bypass must not increment the render counter")
	assert.Equal(t, 5, eg.count(), "bypass entries still dispatched")
}

// TestRSReserve_NeverViolatedUnderAsync: with budget=7 (RS capacity 10,
// 30% reserved) and max_concurrent well above it, sustained async render
// dispatch settles at the reserve budget instead of climbing toward
// max_concurrent. Driven through ProcessInternalQueue directly with a settle
// wait between calls so the counter has caught up (the one-tick gate-to-counter
// lag is out of scope here).
func TestRSReserve_NeverViolatedUnderAsync(t *testing.T) {
	const hostID = 1
	const dimID = 1
	const maxC = 20
	env := newSchedulerEnv(t, 1000, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: maxC, dimensionID: dimID, action: types.ActionRender},
	})
	env.enableAsyncDispatch()
	env.daemon.daemonConfig.Recache.RSCapacityReserved = 0.30
	env.registerRS(t, "rs1", 10, 0) // free=10, reserved=3 -> budget=7
	const budget = 7

	eg := newFakeEG(t, func(string) bool { return true }) // hold every render in flight
	env.registerEG(t, "eg1", eg.address)

	const total = 100
	for i := 0; i < total; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/r%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	// First gate pass dispatches up to the budget.
	env.daemon.ProcessInternalQueue()
	waitFor(t, 3*time.Second, func() bool { return env.inFlightRenders() == int64(budget) },
		"first pass should dispatch exactly the reserve budget")

	// Further passes (counter settled) must dispatch nothing more: budget -
	// in-flight = 0.
	for i := 0; i < 5; i++ {
		env.daemon.ProcessInternalQueue()
		require.Equal(t, int64(budget), env.inFlightRenders(),
			"in-flight renders must not exceed the reserve budget under sustained dispatch")
	}
	assert.Equal(t, budget, eg.count(), "no dispatch beyond the reserve budget")

	eg.releaseAll()
	env.daemon.dispatchWG.Wait()
	assert.Equal(t, int64(0), env.inFlightRenders(), "counter returns to zero after drain")
}

// TestMetrics_DurationHistogramPopulated: a successful recache run populates the
// previously-dead recache_duration_seconds histogram and records a success.
func TestMetrics_DurationHistogramPopulated(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	mc := env.attachMetrics()
	eg := newFakeEG(t, nil)
	env.registerEG(t, "eg1", eg.address)

	for i := 0; i < 3; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/p%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	out := scrapeMetrics(t, mc)
	assert.Contains(t, out, "test_cd_recache_duration_seconds_count")
	assert.NotContains(t, out, "test_cd_recache_duration_seconds_count 0\n",
		"duration histogram must be non-zero after a recache run")
	assert.Contains(t, out, `status="success"`, "successful dispatches recorded")
}

// TestMetrics_TimeoutStatusRecorded: a URL that hits timeout_per_url is recorded
// with status="timeout" so operators see the straggler rate directly.
func TestMetrics_TimeoutStatusRecorded(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 100, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	env.daemon.daemonConfig.Recache.TimeoutPerURL = types.Duration(200 * time.Millisecond)
	mc := env.attachMetrics()
	eg := newFakeEG(t, func(string) bool { return true }) // blocks past the per-URL timeout
	env.registerEG(t, "eg1", eg.address)

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/slow",
		DimensionID: dimID,
		Priority:    redis.PriorityHigh,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()
	// The per-URL goroutine returns once DoTimeout fires (~200ms).
	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(scrapeMetrics(t, mc), `status="timeout"`)
	}, "a stuck URL should be recorded with status=timeout")

	env.drainDispatches(eg)
}

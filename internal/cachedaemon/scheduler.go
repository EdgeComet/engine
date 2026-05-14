package cachedaemon

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// maxDrainIterationsPerTick caps the unified drain loop so a single host with
// a very large queue cannot block other tick work indefinitely. Each iter is
// gated by free internal-queue space and the per-host concurrency cap, so the
// real ceiling is Sum(MaxConcurrent) * 100 URLs/tick.
const maxDrainIterationsPerTick = 100

// Run is the main scheduler loop that processes recache queues.
// Runs in a separate goroutine and respects context cancellation.
//
// On each tick the drain walks GetConfiguredHosts() starting at d.hostCursor,
// pulling at most one priority (high > normal > due autorecache) per host
// per iter. Strict priority within a host is preserved; cross-host fairness
// is preserved by the rotating cursor so a busy host at the front of the
// list cannot starve later hosts when iq fills before everyone has been
// visited. Each iter ends with ProcessInternalQueue() so concurrency slots
// recycle synchronously into the next iter.
//
// Tick-end ProcessInternalQueue() is always called, even when the drain loop
// is skipped because the host list is empty — backoff items and entries
// awaiting retry must still get a chance to dispatch.
//
// The actual tick body lives in runOneTick so unit tests can fire ticks
// deterministically without a ticker goroutine. Shutdown latency is bounded
// by one drain iter plus any in-flight DistributeToEGs wg.Wait() — the
// ctx.Done() check at iter top does not cancel renders mid-dispatch.
func (d *CacheDaemon) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(d.daemonConfig.Scheduler.TickInterval))
	defer ticker.Stop()

	d.logger.Info("Scheduler started",
		zap.Duration("tick_interval", time.Duration(d.daemonConfig.Scheduler.TickInterval)))

	tickCount := 0

	for {
		select {
		case <-ticker.C:
			tickCount++
			now := time.Now().UTC()
			d.lastTickMu.Lock()
			d.lastTickTime = now
			d.lastTickMu.Unlock()

			d.logger.Debug("Scheduler tick",
				zap.Int("tick", tickCount),
				zap.Time("time", now))

			if d.IsSchedulerPaused() {
				d.logger.Debug("Scheduler paused, skipping processing", zap.Int("tick", tickCount))
				continue
			}

			d.runOneTick(ctx, tickCount)

		case <-ctx.Done():
			d.logger.Info("Scheduler shutdown requested")
			return
		}
	}
}

// runOneTick executes a single scheduler tick: unified drain across all
// configured hosts, followed by tick-end housekeeping. Extracted from Run()
// so tests can drive ticks deterministically without a ticker goroutine.
//
// nowUnix is captured once per tick so all hosts share the same autorecache
// due-time reference — avoids sub-millisecond drift within a tick and keeps
// test assertions deterministic.
//
// Shutdown behaviour: a cancelled ctx returns immediately from the drain
// loop without running tick-end housekeeping, so DistributeToEGs.wg.Wait()
// is never started on a cancelled context. Worst-case shutdown latency is
// one render_time (the longest in-flight dispatch from the last successful
// iter).
//
// Per-host skip on defer: after each iter's ProcessInternalQueue, compare
// per-host iq counts before and after. Any host whose iq count grew (its
// pulled entries got deferred — RS budget, concurrency, or downstream gate)
// is added to a tick-local skip set and not pulled from again for the rest
// of this tick. Healthy hosts in the same iter (e.g., bypass hosts when
// render hosts RS-defer) keep draining across subsequent iters; only the
// deferring hosts are throttled. This preserves the spec's per-host
// independence goal: one host's pathology does not throttle other hosts.
// The drain naturally terminates when all hosts are either drained or
// skipped, signalled by `pulled == 0` in an iter.
func (d *CacheDaemon) runOneTick(ctx context.Context, tickCount int) {
	if err := ctx.Err(); err != nil {
		return
	}

	hosts := d.GetConfiguredHosts()
	n := len(hosts)
	nowUnix := time.Now().UTC().Unix()
	var pulledHigh, pulledNormal, pulledAuto int
	iters := 0

	// Guard: empty host list. Without it, the `% n` below would panic.
	// We still fall through to the tick-end ProcessInternalQueue so iq
	// backoff/retry items get a chance to dispatch.
	if n > 0 {
		// Tick-local set of hosts whose pulled entries got deferred this
		// tick. Skipped in subsequent iters to keep durable Redis state in
		// place while still letting other hosts drain. Reset every tick so
		// the next tick re-evaluates with fresh RS budget / concurrency.
		skipForRest := make(map[int]bool, n)

		for iter := 0; iter < maxDrainIterationsPerTick; iter++ {
			if err := ctx.Err(); err != nil {
				return
			}

			spaceRemaining := d.daemonConfig.InternalQueue.MaxSize - d.internalQueue.Size()
			if spaceRemaining <= 0 {
				break
			}

			// Per-host iq counts BEFORE this iter's pull. After
			// ProcessInternalQueue, any host whose count grew has new
			// entries sitting deferred — that host gets added to skipForRest.
			iqBefore := d.internalQueue.CountsByHostID()

			pulled, visited := 0, 0
			pulledThisIter := make(map[int]int, n)
			for i := 0; i < n && spaceRemaining > 0; i++ {
				h := hosts[(d.hostCursor+i)%n]
				visited++
				if skipForRest[h] {
					continue
				}
				p, prio := d.pullForHost(ctx, h, spaceRemaining, nowUnix)
				if p > 0 {
					pulledThisIter[h] = p
				}
				pulled += p
				spaceRemaining -= p
				switch prio {
				case redis.PriorityHigh:
					pulledHigh += p
				case redis.PriorityNormal:
					pulledNormal += p
				case redis.PriorityAutorecache:
					pulledAuto += p
				}
			}
			d.hostCursor = (d.hostCursor + visited) % n
			iters++

			if pulled == 0 {
				// All non-skipped host queues empty (within the durability /
				// space caps). Nothing more to pull this tick.
				break
			}

			// Dispatch this iter's pull so concurrency slots recycle before
			// the next iter calls pullForHost (which gates on free slots).
			d.ProcessInternalQueue()

			// Skip hosts whose pulled entries didn't dispatch. Compare iq
			// count per host vs the pre-pull baseline: a host whose count
			// grew has deferred entries from this iter (RS-gated render,
			// concurrency saturation, or another defer reason). Pulling more
			// from that host this tick would just re-inflate iq.
			iqAfter := d.internalQueue.CountsByHostID()
			for h := range pulledThisIter {
				if iqAfter[h] > iqBefore[h] {
					skipForRest[h] = true
				}
			}
		}
	}

	// Tick-end housekeeping. ProcessInternalQueue dispatches any backoff/retry
	// entries that became due during this tick, plus anything the final drain
	// iter pulled but did not yet dispatch. Skipped on ctx cancellation via
	// the early return above.
	d.ProcessInternalQueue()
	d.publishConcurrencyMetrics()

	totalPulled := pulledHigh + pulledNormal + pulledAuto
	if totalPulled > 0 {
		// Per-tick summary at DEBUG: per-priority counters are already
		// exported via edgecomet_cd_recache_pulled_total{priority,host_id},
		// which is the operator's primary observability channel. Logging
		// every active tick at INFO floods aggregators on busy daemons.
		d.logger.Debug("Scheduler tick drain summary",
			zap.Int("tick", tickCount),
			zap.Int("pulled_high", pulledHigh),
			zap.Int("pulled_normal", pulledNormal),
			zap.Int("pulled_autorecache", pulledAuto),
			zap.Int("total_pulled", totalPulled),
			zap.Int("iterations", iters),
			zap.Bool("hit_iteration_cap", iters == maxDrainIterationsPerTick),
			zap.Int("host_count", n),
			zap.Int("internal_queue_size", d.internalQueue.Size()))
	} else if tickCount%10 == 0 || d.internalQueue.Size() > 0 {
		d.logger.Debug("Scheduler status",
			zap.Int("tick", tickCount),
			zap.Int("internal_queue_size", d.internalQueue.Size()))
	}
}

// CalculateAvailableCapacity calculates how many recache operations can be performed
// based on available RS capacity and configured reservation percentage
func (d *CacheDaemon) CalculateAvailableCapacity() int {
	ctx := context.Background()

	// Query RS registry for healthy services
	rsInstances, err := d.rsRegistry.ListHealthyServices(ctx)
	if err != nil {
		d.logger.Error("Failed to query RS registry", zap.Error(err))
		return 0
	}

	if len(rsInstances) == 0 {
		d.logger.Debug("No render services available")
		return 0
	}

	// Calculate total free tabs across all RS instances
	totalFreeTabs := 0
	for _, rs := range rsInstances {
		freeTabs := rs.Capacity - rs.Load
		if freeTabs > 0 {
			totalFreeTabs += freeTabs
		}
	}

	if totalFreeTabs == 0 {
		d.logger.Debug("All render services at capacity")
		return 0
	}

	// Apply reservation (keep percentage reserved for online traffic)
	reservedForOnline := int(float64(totalFreeTabs) * d.daemonConfig.Recache.RSCapacityReserved)
	availableForRecache := totalFreeTabs - reservedForOnline

	if availableForRecache < 0 {
		return 0
	}

	d.logger.Debug("Calculated available capacity",
		zap.Int("total_free_tabs", totalFreeTabs),
		zap.Int("reserved_for_online", reservedForOnline),
		zap.Int("available_for_recache", availableForRecache),
		zap.Int("rs_count", len(rsInstances)))

	return availableForRecache
}

// ProcessInternalQueue processes entries from the internal queue through two
// composed gates and dispatches the survivors to Edge Gateways:
//
//  1. Per-host concurrency gate (origin protection): every entry must acquire
//     a slot on its host's semaphore. Applies to render and bypass alike.
//  2. RS capacity gate (online traffic protection): only render entries
//     decrement the per-tick RS budget; bypass entries skip this gate.
//
// Entries that fail either gate are re-enqueued for the next tick. Slots
// acquired in this function travel with the entry into DistributeToEGs and
// are released by the path that finishes the entry's work.
//
// Returns the number of entries that passed both gates and were handed off
// to dispatch ("ready" count). runOneTick uses this as a "made progress"
// signal: when a non-zero pull yields zero ready, the drain loop breaks to
// avoid moving more durable Redis state into volatile iq under a stalled
// dispatch path (e.g., RS budget exhausted with all-render entries).
func (d *CacheDaemon) ProcessInternalQueue() int {
	queueSize := d.internalQueue.Size()
	if queueSize == 0 {
		return 0
	}

	batchSize := queueSize
	if batchSize > d.daemonConfig.InternalQueue.MaxSize {
		batchSize = d.daemonConfig.InternalQueue.MaxSize
	}

	batch := d.internalQueue.Dequeue(batchSize)
	if len(batch) == 0 {
		return 0
	}

	rsBudget := d.CalculateAvailableCapacity()
	rsBudgetInitial := rsBudget
	now := time.Now().UTC()
	skipHosts := map[int]bool{}

	var deferredBackoff, deferredConcurrency, deferredRSBudget, droppedQueueFull, discardedUnknown int
	ready := make([]readyItem, 0, len(batch))

	// Hold reloadMu for read across the entire gate loop so hostByID
	// (consumed by actionForEntry) and concurrencyLimiter cannot be swapped
	// mid-batch. DistributeToEGs runs after the lock is released — its slots
	// have captured channel refs and don't depend on the current limiter state.
	d.reloadMu.RLock()
	for _, entry := range batch {
		if !entry.NextRetryAfter.IsZero() && now.Before(entry.NextRetryAfter) {
			if !d.internalQueue.Enqueue(entry) {
				d.logQueueFullDrop(entry, "backoff")
				droppedQueueFull++
				continue
			}
			deferredBackoff++
			continue
		}

		// Discard entries with unresolved host or dimension before acquiring
		// a slot. Such entries would fail at the EG with "host/dimension not
		// found" and consume an origin slot for the round-trip duration. The
		// usual cause is a queue entry persisted in Redis after the host or
		// dimension was removed from config.
		action := d.actionForEntry(entry)
		if action == "" {
			d.logger.Warn("Discarding recache entry with unresolved host or dimension",
				zap.Int("host_id", entry.HostID),
				zap.Int("dimension_id", entry.DimensionID),
				zap.String("url", entry.URL))
			discardedUnknown++
			continue
		}

		if skipHosts[entry.HostID] {
			if !d.internalQueue.Enqueue(entry) {
				d.logQueueFullDrop(entry, "concurrency")
				droppedQueueFull++
				continue
			}
			deferredConcurrency++
			continue
		}

		slot, ok := d.concurrencyLimiter.TryAcquire(entry.HostID)
		if !ok {
			skipHosts[entry.HostID] = true
			if !d.internalQueue.Enqueue(entry) {
				d.logQueueFullDrop(entry, "concurrency")
				droppedQueueFull++
				continue
			}
			deferredConcurrency++
			continue
		}

		// Only ActionRender consumes RS budget. Bypass and status_* actions
		// do not reserve render-service tabs.
		if action == types.ActionRender {
			if rsBudget <= 0 {
				d.concurrencyLimiter.Release(slot)
				if !d.internalQueue.Enqueue(entry) {
					d.logQueueFullDrop(entry, "rs_budget")
					droppedQueueFull++
					continue
				}
				deferredRSBudget++
				continue
			}
			rsBudget--
		}

		ready = append(ready, readyItem{entry: entry, slot: slot})
	}
	d.reloadMu.RUnlock()

	if deferredBackoff+deferredConcurrency+deferredRSBudget+discardedUnknown+droppedQueueFull > 0 {
		d.logger.Debug("Internal queue entries deferred",
			zap.Int("deferred_backoff", deferredBackoff),
			zap.Int("deferred_concurrency", deferredConcurrency),
			zap.Int("deferred_rs_budget", deferredRSBudget),
			zap.Int("discarded_unknown", discardedUnknown),
			zap.Int("dropped_queue_full", droppedQueueFull))
	}

	if len(ready) == 0 {
		return 0
	}

	d.logger.Debug("Processing internal queue batch",
		zap.Int("batch_size", len(ready)),
		zap.Int("rs_budget_initial", rsBudgetInitial),
		zap.Int("rs_budget_remaining", rsBudget),
		zap.Int("deferred_backoff", deferredBackoff),
		zap.Int("deferred_concurrency", deferredConcurrency),
		zap.Int("deferred_rs_budget", deferredRSBudget),
		zap.Int("discarded_unknown", discardedUnknown),
		zap.Int("dropped_queue_full", droppedQueueFull))

	dispatched := len(ready)
	if d.dispatchHook != nil {
		d.dispatchHook(ready)
		return dispatched
	}
	d.DistributeToEGs(ready)
	return dispatched
}

// logQueueFullDrop records an internal-queue overflow. The entry is dropped
// because the queue is at MaxSize when we tried to re-enqueue it after a
// gate rejection. Operators see one error log per dropped entry so the loss
// is observable.
func (d *CacheDaemon) logQueueFullDrop(entry InternalQueueEntry, reason string) {
	d.logger.Error("Internal queue full while re-enqueueing deferred entry; entry dropped",
		zap.Int("host_id", entry.HostID),
		zap.String("url", entry.URL),
		zap.Int("dimension_id", entry.DimensionID),
		zap.String("reason", reason))
}

// zpopAndEnqueue pops up to pullCap entries from the Redis ZSET for
// (hostID, priority) and enqueues them on the internal queue. Returns the
// count actually pushed into iq.
//
// Failure modes (unchanged from the per-priority processors that this
// replaces):
//   - ZPopMin error: log and return 0.
//   - Unmarshal error per entry: log and skip (the ZPopMin already removed it
//     so the poison entry doesn't linger).
//   - iq.Enqueue returns false (queue full): ZAdd the entry back to Redis
//     with the original score so FIFO ordering is preserved on the next
//     iter. If the ZAdd itself fails, log CRITICAL and accept the loss —
//     same behaviour as the old code path. Stops pulling this priority.
func (d *CacheDaemon) zpopAndEnqueue(ctx context.Context, hostID int, priority string, pullCap int) int {
	if pullCap <= 0 {
		return 0
	}
	zsetKey := d.keyGenerator.RecacheQueueKey(hostID, priority)

	result, err := d.redis.ZPopMin(ctx, zsetKey, int64(pullCap))
	if err != nil {
		d.logger.Error("Failed to pop from recache queue",
			zap.Int("host_id", hostID),
			zap.String("priority", priority),
			zap.String("key", zsetKey),
			zap.Error(err))
		return 0
	}
	if len(result) == 0 {
		return 0
	}

	pulled := 0
	for _, r := range result {
		memberJSON := r.Member.(string)
		score := r.Score
		var member types.RecacheMember
		if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
			d.logger.Error("Failed to unmarshal RecacheMember",
				zap.Int("host_id", hostID),
				zap.String("priority", priority),
				zap.String("member_json", memberJSON),
				zap.Error(err))
			continue
		}

		entry := InternalQueueEntry{
			HostID:      hostID,
			URL:         member.URL,
			DimensionID: member.DimensionID,
			RetryCount:  0,
			QueuedAt:    time.Now().UTC(),
		}

		if d.internalQueue.Enqueue(entry) {
			pulled++
			d.logger.Debug("Pulled from recache queue",
				zap.Int("host_id", hostID),
				zap.String("priority", priority),
				zap.String("url", member.URL),
				zap.Int("dimension_id", member.DimensionID))
			continue
		}

		// iq full mid-pull. Re-add to Redis with the original score so this
		// entry keeps its FIFO position relative to siblings that ZPopMin
		// hasn't reached yet. Best-effort: a Redis failure here is logged
		// CRITICAL and the entry is lost (matches the prior code paths).
		d.logger.Warn("Failed to enqueue entry (queue full)",
			zap.Int("host_id", hostID),
			zap.String("priority", priority),
			zap.String("url", member.URL))
		if err := d.redis.ZAdd(ctx, zsetKey, score, memberJSON); err != nil {
			d.logger.Error("CRITICAL: Failed to re-add dropped entry to ZSET",
				zap.Int("host_id", hostID),
				zap.String("priority", priority),
				zap.String("url", member.URL),
				zap.String("key", zsetKey),
				zap.Error(err))
		} else {
			d.logger.Info("Re-added entry to ZSET after enqueue failure",
				zap.Int("host_id", hostID),
				zap.String("priority", priority),
				zap.String("url", member.URL))
		}
		break
	}

	if pulled > 0 && d.metricsCollector != nil {
		d.metricsCollector.RecordRecachePulled(priority, hostID, pulled)
	}
	return pulled
}

// pullForHost drains at most one priority for hostID, in strict priority
// order: high -> normal -> due autorecache. Returns the count actually
// pushed into the internal queue along with the priority that was pulled
// (empty string if nothing was pulled).
//
// Durability pre-check: if the host has no free concurrency slots, items
// popped here would be re-enqueued onto the volatile internal queue (or
// dropped on iq overflow) — keep them in durable Redis instead until slots
// free up.
//
// The pre-check is best-effort: Stats is sampled without holding reloadMu,
// so a concurrent Reload that shrinks max_concurrent can land between this
// read and ProcessInternalQueue's TryAcquire. In that case excess entries
// fall through to ProcessInternalQueue's concurrency-gate path and get
// re-enqueued on iq with a skipHosts mark — they do not get dispatched
// over-cap. The window is short (one tick) and operationally bounded.
//
// At most one priority is pulled per host per iter even if `high` returns
// fewer than the cap, so iq never holds an iter-N normal entry ahead of an
// iter-N+1 high entry (would invert priority on dispatch).
//
// nowUnix is the shared "now" for autorecache due-time filtering, captured
// once per tick by runOneTick.
func (d *CacheDaemon) pullForHost(ctx context.Context, hostID int, spaceRemaining int, nowUnix int64) (int, string) {
	if spaceRemaining <= 0 {
		return 0, ""
	}

	stats := d.concurrencyLimiter.Stats(hostID)
	free := stats.MaxConcurrent - int(stats.InFlight)
	if free <= 0 {
		return 0, ""
	}

	pullCap := free
	if spaceRemaining < pullCap {
		pullCap = spaceRemaining
	}
	if pullCap <= 0 {
		return 0, ""
	}

	if n := d.zpopAndEnqueue(ctx, hostID, redis.PriorityHigh, pullCap); n > 0 {
		return n, redis.PriorityHigh
	}
	if n := d.zpopAndEnqueue(ctx, hostID, redis.PriorityNormal, pullCap); n > 0 {
		return n, redis.PriorityNormal
	}

	// Autorecache: only pop entries that are due (score <= now). ZPopMin
	// returns the N lowest scores unconditionally, so we clamp pullCap to
	// dueCount or the contract breaks (URLs dispatched ahead of schedule).
	zsetKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)
	nowStr := strconv.FormatInt(nowUnix, 10)
	dueCount, err := d.redis.ZCount(ctx, zsetKey, "-inf", nowStr)
	if err != nil {
		d.logger.Error("Failed to count due autorecache entries",
			zap.Int("host_id", hostID),
			zap.String("key", zsetKey),
			zap.Error(err))
		return 0, ""
	}
	if dueCount == 0 {
		return 0, ""
	}
	if int64(pullCap) > dueCount {
		pullCap = int(dueCount)
	}
	n := d.zpopAndEnqueue(ctx, hostID, redis.PriorityAutorecache, pullCap)
	if n == 0 {
		return 0, ""
	}
	return n, redis.PriorityAutorecache
}

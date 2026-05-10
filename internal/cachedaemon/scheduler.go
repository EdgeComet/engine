package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// maxHighPriorityDrainIterations bounds the per-tick drain loop in Run() so a
// single host with a very large queue cannot block other tick work indefinitely.
// Each iteration pulls up to Sum(MaxConcurrent) across all hosts, so the cap
// gives ~Sum(MaxConcurrent) * 100 URLs/tick as a safety ceiling.
const maxHighPriorityDrainIterations = 100

// Run is the main scheduler loop that processes recache queues
// This runs in a separate goroutine and respects context cancellation
func (d *CacheDaemon) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(d.daemonConfig.Scheduler.TickInterval))
	defer ticker.Stop()

	d.logger.Info("Scheduler started",
		zap.Duration("tick_interval", time.Duration(d.daemonConfig.Scheduler.TickInterval)),
		zap.Duration("normal_check_interval", time.Duration(d.daemonConfig.Scheduler.NormalCheckInterval)))

	// Calculate how many ticks between normal/autorecache queue processing
	normalCheckTicks := int(time.Duration(d.daemonConfig.Scheduler.NormalCheckInterval) / time.Duration(d.daemonConfig.Scheduler.TickInterval))
	if normalCheckTicks < 1 {
		normalCheckTicks = 1
	}

	d.logger.Info("Scheduler configuration",
		zap.Int("normal_check_ticks", normalCheckTicks))

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

			// Skip processing if paused
			if d.IsSchedulerPaused() {
				d.logger.Debug("Scheduler paused, skipping processing", zap.Int("tick", tickCount))
				continue
			}

			// Drain runs FIRST so high-priority entries acquire concurrency
			// slots before normal/auto on Nth ticks. The original code pulled
			// high before normal/auto; preserve that precedence here. Each
			// iteration pulls up to Sum(MaxConcurrent) URLs and dispatches
			// them; slots are released synchronously inside
			// ProcessInternalQueue -> DistributeToEGs, so the next iteration
			// can re-acquire them. Bounded by the constant.
			totalPulled, drainIterations := 0, 0
			for i := 0; i < maxHighPriorityDrainIterations; i++ {
				pulled := d.ProcessHighPriorityQueues()
				if pulled == 0 {
					break
				}
				totalPulled += pulled
				drainIterations++
				d.ProcessInternalQueue()
			}
			if totalPulled > 0 {
				d.logger.Info("High priority drain completed",
					zap.Int("pulled", totalPulled),
					zap.Int("iterations", drainIterations),
					zap.Bool("hit_iteration_cap", drainIterations == maxHighPriorityDrainIterations))
			}

			// Every Nth tick: pull normal + autorecache. Their entries land
			// in the (now-empty) internal queue and dispatch via the
			// post-loop ProcessInternalQueue below.
			if tickCount%normalCheckTicks == 0 {
				d.ProcessNormalPriorityQueues()
				d.ProcessAutoRecacheQueues()
			}

			// Every tick: dispatch normal/auto entries pulled above and any
			// backoff-deferred entries. No-op when the queue is empty.
			d.ProcessInternalQueue()

			// Publish per-host concurrency stats to metrics
			d.publishConcurrencyMetrics()

			// Log queue status periodically (every 10 ticks or when non-empty)
			if tickCount%10 == 0 || d.internalQueue.Size() > 0 {
				d.logger.Debug("Scheduler status",
					zap.Int("tick", tickCount),
					zap.Int("internal_queue_size", d.internalQueue.Size()))
			}

		case <-ctx.Done():
			d.logger.Info("Scheduler shutdown requested")
			return
		}
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

// ProcessHighPriorityQueues pulls entries from high priority ZSETs and enqueues them.
// Returns the number of entries pulled (used by Run() to drive the drain loop).
func (d *CacheDaemon) ProcessHighPriorityQueues() int {
	ctx := context.Background()

	// Check internal queue space
	internalQueueSpace := d.daemonConfig.InternalQueue.MaxSize - d.internalQueue.Size()
	if internalQueueSpace <= 0 {
		d.logger.Debug("Internal queue full, skipping high priority queue processing")
		return 0
	}

	hosts := d.GetConfiguredHosts()
	pulledCount := 0

	for _, hostID := range hosts {
		if pulledCount >= internalQueueSpace {
			break
		}

		zsetKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityHigh)

		pullCount := int64(d.concurrencyLimiter.MaxConcurrent(hostID))
		if pullCount < 1 {
			pullCount = 1
		}
		if remaining := int64(internalQueueSpace - pulledCount); pullCount > remaining {
			pullCount = remaining
		}

		result, err := d.redis.ZPopMin(ctx, zsetKey, pullCount)
		if err != nil {
			d.logger.Error("Failed to pop from high priority queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			continue
		}

		for _, r := range result {
			memberJSON := r.Member.(string)
			score := r.Score
			var member types.RecacheMember
			if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
				d.logger.Error("Failed to unmarshal RecacheMember",
					zap.Int("host_id", hostID),
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
				pulledCount++
				d.logger.Debug("Pulled from high priority queue",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL),
					zap.Int("dimension_id", member.DimensionID))
			} else {
				d.logger.Warn("Failed to enqueue entry (queue full)",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL))

				if err := d.redis.ZAdd(ctx, zsetKey, score, memberJSON); err != nil {
					d.logger.Error("CRITICAL: Failed to re-add dropped entry to ZSET",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL),
						zap.String("key", zsetKey),
						zap.Error(err))
				} else {
					d.logger.Info("Re-added entry to ZSET after enqueue failure",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL))
				}
			}
		}
	}

	if pulledCount > 0 {
		d.logger.Debug("Processed high priority queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
	return pulledCount
}

// ProcessNormalPriorityQueues pulls entries from normal priority ZSETs and enqueues them
func (d *CacheDaemon) ProcessNormalPriorityQueues() {
	ctx := context.Background()

	// Check internal queue space
	internalQueueSpace := d.daemonConfig.InternalQueue.MaxSize - d.internalQueue.Size()
	if internalQueueSpace <= 0 {
		d.logger.Debug("Internal queue full, skipping normal priority queue processing")
		return
	}

	hosts := d.GetConfiguredHosts()
	pulledCount := 0

	for _, hostID := range hosts {
		if pulledCount >= internalQueueSpace {
			break
		}

		zsetKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityNormal)

		pullCount := int64(d.concurrencyLimiter.MaxConcurrent(hostID))
		if pullCount < 1 {
			pullCount = 1
		}
		if remaining := int64(internalQueueSpace - pulledCount); pullCount > remaining {
			pullCount = remaining
		}

		result, err := d.redis.ZPopMin(ctx, zsetKey, pullCount)
		if err != nil {
			d.logger.Error("Failed to pop from normal priority queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			continue
		}

		for _, r := range result {
			memberJSON := r.Member.(string)
			score := r.Score
			var member types.RecacheMember
			if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
				d.logger.Error("Failed to unmarshal RecacheMember",
					zap.Int("host_id", hostID),
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
				pulledCount++
				d.logger.Debug("Pulled from normal priority queue",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL),
					zap.Int("dimension_id", member.DimensionID))
			} else {
				d.logger.Warn("Failed to enqueue entry (queue full)",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL))

				if err := d.redis.ZAdd(ctx, zsetKey, score, memberJSON); err != nil {
					d.logger.Error("CRITICAL: Failed to re-add dropped entry to ZSET",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL),
						zap.String("key", zsetKey),
						zap.Error(err))
				} else {
					d.logger.Info("Re-added entry to ZSET after enqueue failure",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL))
				}
			}
		}
	}

	if pulledCount > 0 {
		d.logger.Debug("Processed normal priority queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
}

// ProcessAutoRecacheQueues pulls entries from autorecache ZSETs (only due entries)
func (d *CacheDaemon) ProcessAutoRecacheQueues() {
	ctx := context.Background()

	// Check internal queue space
	internalQueueSpace := d.daemonConfig.InternalQueue.MaxSize - d.internalQueue.Size()
	if internalQueueSpace <= 0 {
		d.logger.Debug("Internal queue full, skipping autorecache queue processing")
		return
	}

	hosts := d.GetConfiguredHosts()
	pulledCount := 0
	now := time.Now().UTC().Unix()
	nowStr := fmt.Sprintf("%d", now)

	for _, hostID := range hosts {
		if pulledCount >= internalQueueSpace {
			break
		}

		zsetKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)

		// Check how many entries are due (score <= now)
		dueCount, err := d.redis.ZCount(ctx, zsetKey, "-inf", nowStr)
		if err != nil {
			d.logger.Error("Failed to count due autorecache entries",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if dueCount == 0 {
			// No entries due for this host
			continue
		}

		// Get total count for logging
		totalCount, err := d.redis.ZCard(ctx, zsetKey)
		if err != nil {
			d.logger.Debug("Failed to get total autorecache count",
				zap.Int("host_id", hostID),
				zap.Error(err))
			totalCount = -1 // Mark as unknown
		}

		d.logger.Debug("Autorecache queue status",
			zap.Int("host_id", hostID),
			zap.Int64("due_count", dueCount),
			zap.Int64("total_count", totalCount))

		pullCount := int64(d.concurrencyLimiter.MaxConcurrent(hostID))
		if pullCount < 1 {
			pullCount = 1
		}
		if remaining := int64(internalQueueSpace - pulledCount); pullCount > remaining {
			pullCount = remaining
		}
		// Critical: clamp to dueCount so ZPOPMIN does not pop entries scheduled
		// for the future. ZPOPMIN returns the N lowest scores unconditionally;
		// without this clamp, batched pulls would dispatch URLs ahead of their
		// scheduled time and silently break the autorecache contract.
		if pullCount > dueCount {
			pullCount = dueCount
		}

		result, err := d.redis.ZPopMin(ctx, zsetKey, pullCount)
		if err != nil {
			d.logger.Error("Failed to pop from autorecache queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			continue
		}

		for _, r := range result {
			memberJSON := r.Member.(string)
			score := int64(r.Score)
			var member types.RecacheMember
			if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
				d.logger.Error("Failed to unmarshal RecacheMember",
					zap.Int("host_id", hostID),
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
				pulledCount++
				d.logger.Debug("Pulled from autorecache queue",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL),
					zap.Int("dimension_id", member.DimensionID),
					zap.Int64("scheduled_at", score),
					zap.Int64("now", now))
			} else {
				d.logger.Warn("Failed to enqueue entry (queue full)",
					zap.Int("host_id", hostID),
					zap.String("url", member.URL))

				if err := d.redis.ZAdd(ctx, zsetKey, float64(score), memberJSON); err != nil {
					d.logger.Error("CRITICAL: Failed to re-add dropped entry to ZSET",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL),
						zap.String("key", zsetKey),
						zap.Error(err))
				} else {
					d.logger.Info("Re-added entry to ZSET after enqueue failure",
						zap.Int("host_id", hostID),
						zap.String("url", member.URL),
						zap.Int64("scheduled_at", score))
				}
			}
		}
	}

	if pulledCount > 0 {
		d.logger.Debug("Processed autorecache queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
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
func (d *CacheDaemon) ProcessInternalQueue() {
	queueSize := d.internalQueue.Size()
	if queueSize == 0 {
		return
	}

	batchSize := queueSize
	if batchSize > d.daemonConfig.InternalQueue.MaxSize {
		batchSize = d.daemonConfig.InternalQueue.MaxSize
	}

	batch := d.internalQueue.Dequeue(batchSize)
	if len(batch) == 0 {
		return
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
		return
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

	d.DistributeToEGs(ready)
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

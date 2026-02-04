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

			// Calculate available RS capacity
			availableCapacity := d.CalculateAvailableCapacity()

			// Every tick: Process high priority queues
			d.ProcessHighPriorityQueues(availableCapacity)

			// Every Nth tick: Process normal + autorecache queues
			if tickCount%normalCheckTicks == 0 {
				d.ProcessNormalPriorityQueues(availableCapacity)
				d.ProcessAutoRecacheQueues(availableCapacity)
			}

			// Every tick: Process internal queue
			d.ProcessInternalQueue(availableCapacity)

			// Log queue status periodically (every 10 ticks or when non-empty)
			if tickCount%10 == 0 || d.internalQueue.Size() > 0 {
				d.logger.Info("Scheduler status",
					zap.Int("tick", tickCount),
					zap.Int("internal_queue_size", d.internalQueue.Size()),
					zap.Int("available_capacity", availableCapacity))
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

// ProcessHighPriorityQueues pulls entries from high priority ZSETs and enqueues them
func (d *CacheDaemon) ProcessHighPriorityQueues(availableCapacity int) {
	ctx := context.Background()

	// Check internal queue space
	internalQueueSpace := d.daemonConfig.InternalQueue.MaxSize - d.internalQueue.Size()
	if internalQueueSpace <= 0 {
		d.logger.Debug("Internal queue full, skipping high priority queue processing")
		return
	}

	hosts := d.GetConfiguredHosts()
	pulledCount := 0

	for _, hostID := range hosts {
		if pulledCount >= internalQueueSpace {
			break
		}

		zsetKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityHigh)

		// ZPOPMIN to get and remove lowest score entry (FIFO by score)
		result, err := d.redis.ZPopMin(ctx, zsetKey, 1)
		if err != nil {
			d.logger.Error("Failed to pop from high priority queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			// Queue empty for this host
			continue
		}

		// Parse RecacheMember from JSON
		memberJSON := result[0].Member.(string)
		score := result[0].Score
		var member types.RecacheMember
		if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
			d.logger.Error("Failed to unmarshal RecacheMember",
				zap.Int("host_id", hostID),
				zap.String("member_json", memberJSON),
				zap.Error(err))
			continue
		}

		// Create internal queue entry
		entry := InternalQueueEntry{
			HostID:      hostID,
			URL:         member.URL,
			DimensionID: member.DimensionID,
			RetryCount:  0,
			QueuedAt:    time.Now().UTC(),
		}

		// Enqueue
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

			// Re-add to ZSET since we couldn't enqueue
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

	if pulledCount > 0 {
		d.logger.Info("Processed high priority queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
}

// ProcessNormalPriorityQueues pulls entries from normal priority ZSETs and enqueues them
func (d *CacheDaemon) ProcessNormalPriorityQueues(availableCapacity int) {
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

		// ZPOPMIN to get and remove lowest score entry (FIFO by score)
		result, err := d.redis.ZPopMin(ctx, zsetKey, 1)
		if err != nil {
			d.logger.Error("Failed to pop from normal priority queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			// Queue empty for this host
			continue
		}

		// Parse RecacheMember from JSON
		memberJSON := result[0].Member.(string)
		score := result[0].Score
		var member types.RecacheMember
		if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
			d.logger.Error("Failed to unmarshal RecacheMember",
				zap.Int("host_id", hostID),
				zap.String("member_json", memberJSON),
				zap.Error(err))
			continue
		}

		// Create internal queue entry
		entry := InternalQueueEntry{
			HostID:      hostID,
			URL:         member.URL,
			DimensionID: member.DimensionID,
			RetryCount:  0,
			QueuedAt:    time.Now().UTC(),
		}

		// Enqueue
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

			// Re-add to ZSET since we couldn't enqueue
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

	if pulledCount > 0 {
		d.logger.Info("Processed normal priority queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
}

// ProcessAutoRecacheQueues pulls entries from autorecache ZSETs (only due entries)
func (d *CacheDaemon) ProcessAutoRecacheQueues(availableCapacity int) {
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

		// ZPOPMIN to get and remove lowest score entry (earliest due)
		result, err := d.redis.ZPopMin(ctx, zsetKey, 1)
		if err != nil {
			d.logger.Error("Failed to pop from autorecache queue",
				zap.Int("host_id", hostID),
				zap.String("key", zsetKey),
				zap.Error(err))
			continue
		}

		if len(result) == 0 {
			// Queue empty (race condition - entry was taken between ZCOUNT and ZPOPMIN)
			continue
		}

		// Parse RecacheMember from JSON
		memberJSON := result[0].Member.(string)
		score := int64(result[0].Score)
		var member types.RecacheMember
		if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
			d.logger.Error("Failed to unmarshal RecacheMember",
				zap.Int("host_id", hostID),
				zap.String("member_json", memberJSON),
				zap.Error(err))
			continue
		}

		// Create internal queue entry
		entry := InternalQueueEntry{
			HostID:      hostID,
			URL:         member.URL,
			DimensionID: member.DimensionID,
			RetryCount:  0,
			QueuedAt:    time.Now().UTC(),
		}

		// Enqueue
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

			// Re-add to ZSET since we couldn't enqueue
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

	if pulledCount > 0 {
		d.logger.Info("Processed autorecache queues",
			zap.Int("entries_pulled", pulledCount),
			zap.Int("hosts_checked", len(hosts)))
	}
}

// ProcessInternalQueue processes entries from internal queue and sends to EGs
func (d *CacheDaemon) ProcessInternalQueue(availableCapacity int) {
	if availableCapacity <= 0 {
		d.logger.Debug("No available capacity for processing internal queue")
		return
	}

	// Dequeue batch (up to availableCapacity)
	batchSize := availableCapacity
	if batchSize > d.internalQueue.Size() {
		batchSize = d.internalQueue.Size()
	}

	if batchSize == 0 {
		// Queue empty, nothing to process
		return
	}

	batch := d.internalQueue.Dequeue(batchSize)

	// Filter entries based on retry backoff
	now := time.Now().UTC()
	readyBatch := []InternalQueueEntry{}
	notReadyCount := 0

	for _, entry := range batch {
		if entry.NextRetryAfter.IsZero() || !now.Before(entry.NextRetryAfter) {
			readyBatch = append(readyBatch, entry)
		} else {
			// Re-enqueue entries not yet ready for retry
			d.internalQueue.Enqueue(entry)
			notReadyCount++
		}
	}

	if notReadyCount > 0 {
		d.logger.Debug("Re-enqueued entries waiting for retry backoff",
			zap.Int("not_ready_count", notReadyCount))
	}

	if len(readyBatch) == 0 {
		d.logger.Debug("No entries ready for processing after backoff filter")
		return
	}

	d.logger.Info("Processing internal queue batch",
		zap.Int("batch_size", len(readyBatch)),
		zap.Int("deferred", notReadyCount),
		zap.Int("available_capacity", availableCapacity))

	// Distribute to EGs with retry logic
	d.DistributeToEGs(readyBatch)
}

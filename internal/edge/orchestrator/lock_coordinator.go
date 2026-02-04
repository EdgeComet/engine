package orchestrator

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/metrics"
)

// LockCoordinator handles distributed locking for render coordination
type LockCoordinator struct {
	metadata *cache.MetadataStore
	logger   *zap.Logger
}

// NewLockCoordinator creates a new LockCoordinator instance
func NewLockCoordinator(
	metadata *cache.MetadataStore,
	logger *zap.Logger,
) *LockCoordinator {
	return &LockCoordinator{
		metadata: metadata,
		logger:   logger,
	}
}

// AcquireLock attempts to acquire a render lock for the given cache key
func (lc *LockCoordinator) AcquireLock(renderCtx *edgectx.RenderContext) (bool, error) {
	// Use independent timeout to prevent race condition from request cancellation
	// This ensures lock acquisition always completes or fails atomically
	lockCtx, cancel := context.WithTimeout(context.Background(), redisLockOperationTimeout)
	defer cancel()

	// Calculate lock TTL based on this host's render timeout
	lockTTL := lc.CalculateLockTTL(time.Duration(renderCtx.Host.Render.Timeout))

	renderCtx.Logger.Debug("Attempting to acquire render lock",
		zap.Duration("lock_ttl", lockTTL),
		zap.Duration("host_render_timeout", time.Duration(renderCtx.Host.Render.Timeout)))

	acquired, err := lc.metadata.AcquireLock(lockCtx, renderCtx.LockKey, lockTTL)
	// Uses independent context prevents inconsistent lock state
	if err != nil {
		renderCtx.Logger.Error("Failed to acquire render lock", zap.Error(err))
		return false, fmt.Errorf("failed to acquire render lock: %w", err)
	}

	if acquired {
		renderCtx.Logger.Debug("Render lock acquired successfully")
	}

	return acquired, nil
}

// ReleaseLock releases the render lock using background context for reliable cleanup
func (lc *LockCoordinator) ReleaseLock(renderCtx *edgectx.RenderContext) {
	// Use background context for cleanup - must always complete
	if err := lc.metadata.ReleaseLock(context.Background(), renderCtx.LockKey); err != nil {
		renderCtx.Logger.Error("Failed to release render lock", zap.Error(err))
	}
}

// WaitForConcurrentRender waits for another render to complete and tries to serve from cache
func (lc *LockCoordinator) WaitForConcurrentRender(
	renderCtx *edgectx.RenderContext,
	cacheCoord *CacheCoordinator,
	metricsCollector *metrics.MetricsCollector,
) (WaitResult, error) {
	// Calculate wait timeout as 80% of host's render timeout
	baseTimeout := time.Duration(renderCtx.Host.Render.Timeout)
	waitTimeout := time.Duration(float64(baseTimeout) * concurrentRenderWaitPercent)

	// Apply min/max constraints
	if waitTimeout < minConcurrentWait {
		waitTimeout = minConcurrentWait
	}
	if waitTimeout > maxConcurrentWait {
		waitTimeout = maxConcurrentWait
	}

	renderCtx.Logger.Info("Lock not acquired, waiting for concurrent render to complete",
		zap.Duration("wait_timeout", waitTimeout),
		zap.Duration("host_render_timeout", baseTimeout),
		zap.Duration("poll_interval", concurrentRenderPollInterval))

	// Poll for cache availability within timeout window
	startTime := time.Now().UTC()
	deadline := startTime.Add(waitTimeout)
	pollAttempt := 0

	for time.Now().UTC().Before(deadline) {
		// Check if request has timed out before continuing
		if renderCtx.IsTimedOut() {
			waitTime := time.Now().UTC().Sub(startTime)
			renderCtx.Logger.Warn("Request timeout during concurrent render wait",
				zap.Int("poll_attempts", pollAttempt),
				zap.Duration("wait_time", waitTime),
				zap.Duration("time_remaining", renderCtx.TimeRemaining()))
			metricsCollector.RecordWaitTimeout(renderCtx.Host.Domain, renderCtx.Dimension, waitTime)
			return WaitRequestTimeout, nil
		}

		time.Sleep(concurrentRenderPollInterval)
		pollAttempt++

		renderCtx.Logger.Debug("Polling for cache availability",
			zap.Int("attempt", pollAttempt),
			zap.Duration("elapsed", time.Now().UTC().Sub(startTime)),
			zap.Duration("remaining", deadline.Sub(time.Now().UTC())),
			zap.Duration("timeout_remaining", renderCtx.TimeRemaining()))

		if _, exists := cacheCoord.LookupCache(renderCtx); exists {
			waitTime := time.Now().UTC().Sub(startTime)
			renderCtx.Logger.Info("Cache became available after waiting",
				zap.Int("poll_attempts", pollAttempt),
				zap.Duration("wait_time", waitTime))
			metricsCollector.RecordWaitSuccess(renderCtx.Host.Domain, renderCtx.Dimension, waitTime)
			return WaitCacheAvailable, nil
		}
	}

	waitTime := time.Now().UTC().Sub(startTime)
	renderCtx.Logger.Warn("Timeout waiting for concurrent render, using bypass mode",
		zap.Int("total_attempts", pollAttempt),
		zap.Duration("wait_time", waitTime))
	metricsCollector.RecordWaitTimeout(renderCtx.Host.Domain, renderCtx.Dimension, waitTime)
	return WaitTimeout, nil
}

// CalculateLockTTL calculates the lock TTL based on render timeout
func (lc *LockCoordinator) CalculateLockTTL(renderTimeout time.Duration) time.Duration {
	lockTTL := renderTimeout + lockTTLBuffer
	if lockTTL < minLockTTL {
		lockTTL = minLockTTL
	}
	return lockTTL
}

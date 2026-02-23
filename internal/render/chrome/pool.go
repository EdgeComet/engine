package chrome

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/render/metrics"
	"github.com/edgecomet/engine/internal/render/registry"
)

// ChromePool manages a pool of Chrome instances with a simple FIFO queue
type ChromePool struct {
	config           *Config
	logger           *zap.Logger
	instances        []*ChromeInstance
	queue            chan int           // FIFO queue of available instance IDs
	mu               sync.RWMutex       // Protects instances slice
	activeTabs       atomic.Int32       // Number of currently active renders
	totalRenders     atomic.Int64       // Total renders processed
	totalRestarts    atomic.Int64       // Total instance restarts
	createdAt        time.Time          // Pool creation time
	ctx              context.Context    // Pool context
	cancel           context.CancelFunc // Cancel function
	registry         *registry.ServiceRegistry
	serviceInfo      *registry.ServiceInfo
	metricsCollector *metrics.MetricsCollector
	hostname         string
	poolSize         int // Store pool size for health calculation

	// Tab management (for counting reservations in heartbeat)
	tabManager *registry.TabManager

	// Tab occupancy tracking
	acquiredTabs   map[int]string // tab ID -> request ID
	acquiredTabsMu sync.Mutex     // Protects acquiredTabs map

	// Heartbeat goroutine tracking
	heartbeatWg      sync.WaitGroup
	heartbeatStopped atomic.Bool // Tracks if heartbeat has been stopped
	serviceInfoMu    sync.Mutex  // Protects serviceInfo during heartbeat operations
}

// NewChromePool creates a new Chrome pool with the specified configuration
func NewChromePool(config *Config, registry *registry.ServiceRegistry, serviceInfo *registry.ServiceInfo,
	metricsCollector *metrics.MetricsCollector, tabManager *registry.TabManager, hostname string, logger *zap.Logger,
) (*ChromePool, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	poolSize := config.CalculatePoolSize()
	logger.Info("Initializing Chrome pool",
		zap.Int("pool_size", poolSize))

	ctx, cancel := context.WithCancel(context.Background())

	pool := &ChromePool{
		config:           config,
		logger:           logger,
		instances:        make([]*ChromeInstance, poolSize),
		queue:            make(chan int, poolSize),
		createdAt:        time.Now().UTC(),
		ctx:              ctx,
		cancel:           cancel,
		registry:         registry,
		serviceInfo:      serviceInfo,
		metricsCollector: metricsCollector,
		hostname:         hostname,
		poolSize:         poolSize,
		tabManager:       tabManager,
		acquiredTabs:     make(map[int]string),
	}

	// Create all Chrome instances
	serviceID := ""
	if serviceInfo != nil {
		serviceID = serviceInfo.ID
	}
	for i := 0; i < poolSize; i++ {
		instance, err := NewChromeInstance(i, serviceID, config, logger)
		if err != nil {
			// Cleanup already created instances
			pool.Shutdown()
			return nil, fmt.Errorf("failed to create Chrome instance %d: %w", i, err)
		}

		pool.instances[i] = instance
		pool.queue <- i // Add to available queue
	}

	logger.Info("Chrome pool initialized successfully",
		zap.Int("instances", poolSize))

	return pool, nil
}

// AcquireChrome acquires a Chrome instance from the pool (blocking)
func (p *ChromePool) AcquireChrome(requestID string) (*ChromeInstance, error) {
	select {
	case <-p.ctx.Done():
		return nil, ErrPoolShutdown
	case instanceID := <-p.queue:
		// Double-check if shutdown happened while we were waiting on queue
		select {
		case <-p.ctx.Done():
			// Return instance to queue and fail
			select {
			case p.queue <- instanceID:
			default:
			}
			return nil, ErrPoolShutdown
		default:
		}

		p.activeTabs.Add(1)

		// Track acquired tab
		p.acquiredTabsMu.Lock()
		p.acquiredTabs[instanceID] = requestID
		p.acquiredTabsMu.Unlock()

		p.mu.RLock()
		instance := p.instances[instanceID]
		p.mu.RUnlock()

		// Check if instance is alive
		if !instance.IsAlive() {
			p.logger.Warn("Chrome instance is dead, restarting",
				zap.String("request_id", requestID),
				zap.Int("instance_id", instanceID),
				zap.Int32("requests_done", instance.GetRequestsDone()))

			// Attempt restart
			if err := instance.Restart(p.config); err != nil {
				p.logger.Error("Failed to restart dead instance",
					zap.String("request_id", requestID),
					zap.Int("instance_id", instanceID),
					zap.Error(err))
				// Return to queue with select to avoid panic during shutdown
				select {
				case p.queue <- instanceID:
				case <-p.ctx.Done():
					// Shutting down, don't return to queue
				}
				p.activeTabs.Add(-1)
				return nil, fmt.Errorf("%w: instance %d", ErrInstanceDead, instanceID)
			}
			p.totalRestarts.Add(1)
		}

		// Check if instance should be restarted based on policies
		if instance.ShouldRestart(p.config) {
			p.logger.Info("Chrome instance needs restart based on policy",
				zap.String("request_id", requestID),
				zap.Int("instance_id", instanceID),
				zap.Int32("requests_done", instance.GetRequestsDone()),
				zap.Duration("age", instance.Age()))

			if err := instance.Restart(p.config); err != nil {
				p.logger.Error("Failed to restart instance",
					zap.String("request_id", requestID),
					zap.Int("instance_id", instanceID),
					zap.Error(err))
				// Continue with current instance despite restart failure
			} else {
				p.totalRestarts.Add(1)
			}
		}

		instance.SetStatus(ChromeStatusRendering)
		instance.currentRequestID = requestID

		p.logger.Debug("Chrome instance acquired",
			zap.String("request_id", requestID),
			zap.Int("instance_id", instanceID),
			zap.Int32("active_tabs", p.activeTabs.Load()),
			zap.Int("pool_size", p.poolSize))

		// Send immediate heartbeat to update registry with current pool state
		p.sendHeartbeat()

		return instance, nil
	}
}

// ReleaseChrome returns a Chrome instance back to the pool
func (p *ChromePool) ReleaseChrome(instance *ChromeInstance) {
	requestID := instance.currentRequestID
	instance.SetStatus(ChromeStatusIdle)
	instance.IncrementRequests()
	p.totalRenders.Add(1)

	// Clear request ID BEFORE returning to queue to avoid race condition
	instance.currentRequestID = ""

	// Release tab tracking and decrement counter atomically (before heartbeat)
	p.acquiredTabsMu.Lock()
	delete(p.acquiredTabs, instance.ID)
	p.acquiredTabsMu.Unlock()

	p.activeTabs.Add(-1)

	// Return to queue with select to avoid panic if shutting down
	select {
	case p.queue <- instance.ID:
		p.logger.Debug("Chrome instance released",
			zap.String("request_id", requestID),
			zap.Int("instance_id", instance.ID),
			zap.Int32("requests_done", instance.GetRequestsDone()),
			zap.Int32("active_tabs", p.activeTabs.Load()))
	case <-p.ctx.Done():
		// Pool shutting down, discard instance
		p.logger.Debug("Discarding instance during shutdown",
			zap.String("request_id", requestID),
			zap.Int("instance_id", instance.ID))
	default:
		// Queue full - should never happen, indicates bug
		p.logger.Error("Queue full when returning instance - possible leak",
			zap.String("request_id", requestID),
			zap.Int("instance_id", instance.ID),
			zap.Int("queue_len", len(p.queue)))
	}

	// Send immediate heartbeat to update registry with current pool state
	p.sendHeartbeat()
}

// GetStats returns current pool statistics
func (p *ChromePool) GetStats() PoolStats {
	p.mu.RLock()
	totalInstances := len(p.instances)
	p.mu.RUnlock()

	return PoolStats{
		TotalInstances:     totalInstances,
		AvailableInstances: len(p.queue),
		ActiveInstances:    int(p.activeTabs.Load()),
		QueueDepth:         totalInstances - len(p.queue),
		TotalRenders:       p.totalRenders.Load(),
		TotalRestarts:      p.totalRestarts.Load(),
		Uptime:             time.Since(p.createdAt),
	}
}

// sendHeartbeat sends an immediate heartbeat update to the service registry
// This is called after every acquire/release to keep Edge Gateway informed
func (p *ChromePool) sendHeartbeat() {
	// Skip if registry not configured (e.g., in tests)
	if p.registry == nil || p.serviceInfo == nil {
		return
	}

	// Serialize heartbeat operations to prevent concurrent map writes
	p.serviceInfoMu.Lock()
	defer p.serviceInfoMu.Unlock()

	// Early exit if context is cancelled (during shutdown)
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	ctx := context.Background()

	// Sync tabs hash with current pool state (recreates if missing)
	p.acquiredTabsMu.Lock()
	acquiredSnapshot := make(map[int]string, len(p.acquiredTabs))
	for tabID, reqID := range p.acquiredTabs {
		acquiredSnapshot[tabID] = reqID
	}
	p.acquiredTabsMu.Unlock()

	// Update Redis tabs hash (efficient: only refresh TTL if exists, full rebuild if missing)
	if err := p.tabManager.SyncTabs(ctx, acquiredSnapshot, p.poolSize); err != nil {
		p.logger.Error("Failed to sync tabs", zap.Error(err))
	}

	// Check again before expensive operations
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	stats := p.GetStats()

	// Calculate availability
	available := stats.TotalInstances - stats.ActiveInstances

	// Update service info with current pool state
	p.serviceInfo.Load = stats.ActiveInstances
	p.serviceInfo.Capacity = stats.TotalInstances

	// Update metadata
	p.serviceInfo.SetMetadata(stats.TotalInstances, available, p.hostname)

	// Update Prometheus metrics
	if p.metricsCollector != nil {
		p.metricsCollector.UpdateChromePoolSize(stats.TotalInstances)
		p.metricsCollector.UpdateChromeAvailable(available)
	}

	// Log heartbeat state for debugging
	p.logger.Debug("Sending heartbeat to registry",
		zap.Int("available", available),
		zap.Int("active", stats.ActiveInstances),
		zap.Int("total", stats.TotalInstances),
		zap.Int("load", p.serviceInfo.Load),
		zap.Int("capacity", p.serviceInfo.Capacity))

	// Send to registry
	if err := p.registry.RegisterService(ctx, p.serviceInfo); err != nil {
		p.logger.Error("Failed to send heartbeat",
			zap.Error(err),
			zap.Int("available", available))
	}
}

// StartPeriodicHeartbeat starts a background goroutine that sends periodic heartbeats
// This ensures the service registry is updated even when the pool is idle
func (p *ChromePool) StartPeriodicHeartbeat(interval time.Duration) {
	// Skip if registry not configured
	if p.registry == nil {
		return
	}

	p.logger.Info("Starting periodic heartbeat",
		zap.Duration("interval", interval))

	// Send initial heartbeat
	p.sendHeartbeat()

	ticker := time.NewTicker(interval)
	p.heartbeatWg.Add(1)
	go func() {
		defer p.heartbeatWg.Done()
		for {
			select {
			case <-ticker.C:
				p.sendHeartbeat()
			case <-p.ctx.Done():
				ticker.Stop()
				p.logger.Info("Stopping periodic heartbeat")
				return
			}
		}
	}()
}

// StopHeartbeat stops the periodic heartbeat goroutine without shutting down Chrome instances
// This should be called early in the shutdown sequence before deleting tabs from Redis
func (p *ChromePool) StopHeartbeat() {
	// Skip if registry not configured or already stopped
	if p.registry == nil || p.heartbeatStopped.Load() {
		return
	}

	p.logger.Info("Stopping heartbeat goroutine")

	// Signal heartbeat to stop
	p.cancel()

	// Wait for heartbeat goroutine to exit cleanly
	p.heartbeatWg.Wait()

	// Mark as stopped
	p.heartbeatStopped.Store(true)

	p.logger.Info("Heartbeat goroutine stopped")
}

// Shutdown gracefully shuts down all Chrome instances with default timeout
func (p *ChromePool) Shutdown() error {
	return p.ShutdownWithTimeout(p.config.ShutdownTimeout)
}

// ShutdownWithTimeout gracefully shuts down all Chrome instances with custom timeout
func (p *ChromePool) ShutdownWithTimeout(timeout time.Duration) error {
	p.logger.Info("Initiating Chrome pool shutdown",
		zap.Duration("timeout", timeout),
		zap.Int32("active_renders", p.activeTabs.Load()))

	// === PHASE 0: Stop heartbeat (if not already stopped by StopHeartbeat()) ===
	if !p.heartbeatStopped.Load() {
		p.cancel()

		// Wait for heartbeat goroutine to exit cleanly
		p.logger.Info("Waiting for heartbeat goroutine to stop")
		p.heartbeatWg.Wait()
		p.heartbeatStopped.Store(true)
		p.logger.Info("Heartbeat goroutine stopped")
	} else {
		// Heartbeat already stopped externally (via StopHeartbeat)
		// Still need to cancel context for other operations
		p.cancel()
	}

	stats := p.GetStats()
	p.logger.Info("Shutdown initiated - waiting for active renders to complete",
		zap.Int("active_renders", stats.ActiveInstances),
		zap.Int("total_instances", stats.TotalInstances))

	// === PHASE 2: Graceful drain with timeout ===
	gracefulComplete := p.waitForActiveRenders(timeout)

	if gracefulComplete {
		p.logger.Info("All active renders completed gracefully")
	} else {
		p.logger.Warn("Shutdown timeout exceeded, forcing termination",
			zap.Int32("stuck_renders", p.activeTabs.Load()))
	}

	// === PHASE 3: Terminate all instances ===
	p.mu.Lock()
	var errors []error
	for i, instance := range p.instances {
		if instance == nil {
			continue
		}

		if err := instance.Terminate(); err != nil {
			p.logger.Error("Error terminating instance",
				zap.Int("instance_id", i),
				zap.Error(err))
			errors = append(errors, err)
		}
	}
	p.mu.Unlock()

	// Note: We don't close the queue to avoid panics on send
	// The queue becomes irrelevant after context cancellation

	finalStats := p.GetStats()
	p.logger.Info("Chrome pool shut down",
		zap.Int64("total_renders", finalStats.TotalRenders),
		zap.Int64("total_restarts", finalStats.TotalRestarts),
		zap.Duration("uptime", finalStats.Uptime))

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during shutdown", len(errors))
	}

	return nil
}

// waitForActiveRenders waits for all active renders to complete with timeout
// Returns true if all renders completed, false if timeout was reached
func (p *ChromePool) waitForActiveRenders(timeout time.Duration) bool {
	deadline := time.Now().UTC().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if p.activeTabs.Load() == 0 {
			return true
		}

		<-ticker.C
		if time.Now().UTC().After(deadline) {
			return false
		}
	}
}

// PoolSize returns the total number of Chrome instances in the pool
func (p *ChromePool) PoolSize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.instances)
}

// AvailableInstances returns the number of available Chrome instances
func (p *ChromePool) AvailableInstances() int {
	return len(p.queue)
}

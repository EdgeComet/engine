package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/cachedaemon/metrics"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/common/metricsserver"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/sharding"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

// CacheDaemon is the main cache daemon service
type CacheDaemon struct {
	daemonConfig    *configtypes.CacheDaemonConfig
	configManager   configtypes.EGConfigManager
	redis           *redis.Client
	logger          *zap.Logger
	internalAuthKey string // Internal auth key from EG config (cache_sharding.internal_auth_key)
	internalQueue   *InternalQueue
	rsRegistry      *registry.ServiceRegistry
	egRegistry      sharding.Registry
	normalizer      *hash.URLNormalizer
	keyGenerator    *redis.KeyGenerator
	httpClient      *fasthttp.Client
	retryBaseDelay  time.Duration // Override for testing (0 = use default from distributor.go)
	startTime       time.Time
	lastTickMu      sync.RWMutex
	lastTickTime    time.Time

	// Per-host recache concurrency limiter (origin protection gate)
	concurrencyLimiter *HostConcurrencyLimiter

	// hostCursor rotates the per-tick host scan so a backlog at the front of
	// GetConfiguredHosts() cannot starve hosts later in the list when the
	// internal queue fills before every host has been visited. Read/written
	// only by Run() (single goroutine), so no synchronisation is needed.
	hostCursor int

	// hostSetPtr/hostSetLen identify the host slice last applied to the derived
	// caches (hostByID + concurrencyLimiter). The config manager swaps its host
	// slice wholesale on every reload, so a changed first-element address or
	// length means the live host set diverged from the derived caches and they
	// must be rebuilt (see maybeResyncDerivedCaches). Read/written only by the
	// Run goroutine, like hostCursor, so no synchronisation is needed.
	hostSetPtr *types.Host
	hostSetLen int

	// dispatchHook, when non-nil, replaces the EG dispatch path inside
	// ProcessInternalQueue. Used by scheduler unit tests to observe gated
	// entries without standing up a real EG. The hook owns slot release for
	// the entries it receives (matches DistributeToEGs' contract).
	dispatchHook func(batch []readyItem)

	// reloadMu serialises configuration reloads against scheduler ticks so that
	// hostByID and concurrencyLimiter are always observed in a consistent
	// state from a single tick. Reload takes the write lock; the scheduler's
	// gate loop takes the read lock.
	reloadMu sync.RWMutex

	// Local host_id -> *types.Host cache; rebuilt from configManager on init/reload.
	// Read under reloadMu.RLock; written only by rebuildHostByIDLocked while
	// reloadMu.Lock is held.
	hostByID map[int]*types.Host

	// Readers
	cacheReader *CacheReader
	queueReader *QueueReader

	// Metrics
	metricsCollector *metrics.MetricsCollector
	metricsServer    *fasthttp.Server

	// Scheduler control
	schedulerCtx     context.Context
	schedulerCancel  context.CancelFunc
	schedulerPaused  bool
	schedulerPauseMu sync.RWMutex

	// dispatchWG tracks detached DistributeToEGs goroutines so Shutdown can
	// drain in-flight dispatches before the process exits. Add is called only
	// on the Run goroutine; Wait is called only by Shutdown after Run returns.
	dispatchWG sync.WaitGroup

	// schedulerDone is closed by Run() on every return path so Shutdown can
	// join the scheduler before draining dispatchWG (closes the Add-during-Wait
	// race). Initialised in NewCacheDaemon.
	schedulerDone chan struct{}

	// schedulerJoinTimeout bounds the wait for Run() to return after cancel.
	// Zero means defaultSchedulerJoinTimeout. Overridable only so tests can
	// exercise the pathological non-returning-Run abort path quickly.
	schedulerJoinTimeout time.Duration

	// inFlightRenders counts render-mode recache requests the daemon currently
	// has in flight across detached dispatch goroutines. Accessed via
	// sync/atomic. Subtracted from the per-tick RS budget so async dispatch
	// cannot over-commit render-service capacity past the reserve. Incremented
	// and decremented 1:1 inside the per-URL goroutine (distributor.go).
	inFlightRenders int64

	// Reload hook (optional, set by enterprise version)
	reloadFunc func(ctx context.Context) error
}

// NewCacheDaemon creates a new cache daemon instance
func NewCacheDaemon(
	daemonCfg *configtypes.CacheDaemonConfig,
	configManager configtypes.EGConfigManager,
	redisClient *redis.Client,
	logger *zap.Logger,
) (*CacheDaemon, error) {
	if daemonCfg == nil {
		return nil, fmt.Errorf("daemon config is required")
	}
	if configManager == nil {
		return nil, fmt.Errorf("config manager is required")
	}
	if redisClient == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Get internal auth key from EG config (internal.auth_key)
	egConfig := configManager.GetConfig()
	if egConfig.Internal.AuthKey == "" {
		return nil, fmt.Errorf("internal.auth_key in EG config is required for daemon API authentication")
	}
	internalAuthKey := egConfig.Internal.AuthKey

	// Initialize internal queue
	internalQueue := NewInternalQueue(daemonCfg.InternalQueue.MaxSize)

	// Initialize RS registry
	rsRegistry := registry.NewServiceRegistry(redisClient, logger)

	// Initialize EG registry
	egRegistry := sharding.NewRedisRegistry(redisClient, logger)

	// Initialize URL normalizer
	normalizer := hash.NewURLNormalizer()

	// Initialize key generator
	keyGenerator := redis.NewKeyGenerator()

	// Initialize HTTP client for recache requests to EGs.
	// MaxConnsPerHost caps the daemon-side connection storm to any single EG.
	const maxConnsPerEG = 256
	httpClient := &fasthttp.Client{
		ReadTimeout:         time.Duration(daemonCfg.Recache.TimeoutPerURL),
		WriteTimeout:        time.Duration(daemonCfg.Recache.TimeoutPerURL),
		MaxIdleConnDuration: 500 * time.Millisecond,
		MaxConnsPerHost:     maxConnsPerEG,
	}

	// Get retry base delay from config (default: 5s)
	const defaultRetryBaseDelay = 5 * time.Second
	retryBaseDelay := daemonCfg.InternalQueue.RetryBaseDelay.ToDuration()
	if retryBaseDelay == 0 {
		retryBaseDelay = defaultRetryBaseDelay
		logger.Info("Using default retry base delay",
			zap.Duration("retry_base_delay", retryBaseDelay))
	} else {
		logger.Info("Using configured retry base delay",
			zap.Duration("retry_base_delay", retryBaseDelay))
	}

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(daemonCfg.Metrics.Namespace, logger)

	// Start separate metrics server if needed
	metricsServer, err := metricsserver.StartMetricsServer(
		daemonCfg.Metrics.Enabled,
		daemonCfg.Metrics.Listen,
		daemonCfg.Metrics.Path,
		metricsCollector,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start metrics server: %w", err)
	}

	// Shared between the daemon's concurrency gate and the queue reader so the
	// "processing" figure reported by the queue-summary endpoint derives from
	// the same in-flight counters as /status and the recache_inflight gauge.
	concurrencyLimiter := NewHostConcurrencyLimiter(egConfig, configManager.GetHosts())

	daemon := &CacheDaemon{
		daemonConfig:       daemonCfg,
		configManager:      configManager,
		redis:              redisClient,
		logger:             logger,
		internalAuthKey:    internalAuthKey,
		internalQueue:      internalQueue,
		rsRegistry:         rsRegistry,
		egRegistry:         egRegistry,
		normalizer:         normalizer,
		keyGenerator:       keyGenerator,
		httpClient:         httpClient,
		retryBaseDelay:     retryBaseDelay,
		startTime:          time.Now().UTC(),
		concurrencyLimiter: concurrencyLimiter,
		hostCursor:         0,
		metricsCollector:   metricsCollector,
		metricsServer:      metricsServer,
		cacheReader:        NewCacheReader(redisClient, keyGenerator, logger),
		queueReader:        NewQueueReader(redisClient, keyGenerator, internalQueue, concurrencyLimiter, logger),
		schedulerDone:      make(chan struct{}),
	}

	daemon.reloadMu.Lock()
	daemon.rebuildHostByIDLocked()
	daemon.reloadMu.Unlock()

	// Seed the change-detection marker with the host set hostByID was just built
	// from, so the first scheduler tick doesn't trigger a redundant resync.
	if h := configManager.GetHosts(); len(h) > 0 {
		daemon.hostSetPtr = &h[0]
		daemon.hostSetLen = len(h)
	}

	return daemon, nil
}

// rebuildHostByIDLocked refreshes the local host_id -> *types.Host lookup
// from the config manager. Caller must hold d.reloadMu (write lock).
func (d *CacheDaemon) rebuildHostByIDLocked() {
	hosts := d.configManager.GetHosts()
	m := make(map[int]*types.Host, len(hosts))
	for i := range hosts {
		m[hosts[i].ID] = &hosts[i]
	}
	d.hostByID = m
}

// resyncDerivedCaches rebuilds hostByID and the per-host concurrency limiter
// from the config manager's current host set, atomically under reloadMu so a
// concurrent scheduler gate loop never observes one new and one old. Shared by
// the reload hook (POST /internal/reload) and the per-tick self-resync.
func (d *CacheDaemon) resyncDerivedCaches() {
	eg := d.configManager.GetConfig()
	d.reloadMu.Lock()
	d.rebuildHostByIDLocked()
	d.concurrencyLimiter.Reload(eg, d.configManager.GetHosts())
	d.reloadMu.Unlock()
}

// maybeResyncDerivedCaches rebuilds the derived caches when the config manager's
// host set was swapped out-of-band (e.g. a hot-reload poll) without going through
// POST /internal/reload. Called at the top of each tick on the Run goroutine. The
// manager swaps its host slice wholesale, so a different first-element address or
// length signals a change. The stored pointer is used only as an identity token
// (never dereferenced).
//
// Keep hostSetPtr typed as *types.Host (not uintptr): retaining a real pointer
// into the previous backing array keeps that array alive, so the allocator cannot
// reuse its address for the next host slice -- which is what makes the address
// comparison sound (prevents an ABA false-negative on a same-length swap).
//
// On a no-change tick this takes no lock: GetHosts() is an atomic load and the
// check is a pointer/int comparison; reloadMu is taken only when a change fires.
func (d *CacheDaemon) maybeResyncDerivedCaches() {
	hosts := d.configManager.GetHosts()
	var ptr *types.Host
	if len(hosts) > 0 {
		ptr = &hosts[0]
	}
	if ptr == d.hostSetPtr && len(hosts) == d.hostSetLen {
		return
	}
	d.resyncDerivedCaches()
	d.hostSetPtr = ptr
	d.hostSetLen = len(hosts)
}

// publishConcurrencyMetrics snapshots the per-host concurrency limiter state
// and exports it to Prometheus. Called once per scheduler tick.
// Domain labels are looked up under reloadMu.RLock so they stay consistent
// with the snapshot the limiter just produced.
func (d *CacheDaemon) publishConcurrencyMetrics() {
	if d.metricsCollector == nil {
		return
	}
	stats := d.concurrencyLimiter.AllStats()
	d.reloadMu.RLock()
	defer d.reloadMu.RUnlock()
	for hostID, s := range stats {
		var domain string
		if h, ok := d.hostByID[hostID]; ok && h != nil {
			domain = h.Domain
		}
		d.metricsCollector.SetHostConcurrency(hostID, domain, s.InFlight, int64(s.MaxConcurrent), s.AcquiredTotal, s.DeniedTotal)
	}
}

// actionForEntry resolves the effective action for a queue entry. A per-request
// mode (entry.Mode) overrides the dimension's action so the scheduler budgets RS
// capacity correctly (a render override consumes render budget; a bypass override
// does not). Returns an empty URLRuleAction when host or dimension are missing —
// the scheduler treats these entries as unresolved and discards them before
// dispatch. Caller must hold d.reloadMu.RLock so hostByID stays stable for the
// duration of the lookup.
func (d *CacheDaemon) actionForEntry(entry InternalQueueEntry) types.URLRuleAction {
	host := d.hostByID[entry.HostID]
	if host == nil {
		return ""
	}
	for _, dim := range host.Dimensions {
		if dim.ID == entry.DimensionID {
			switch entry.Mode {
			case types.RecacheModeRender:
				return types.ActionRender
			case types.RecacheModeBypass:
				return types.ActionBypass
			default:
				return dim.EffectiveAction()
			}
		}
	}
	return ""
}

// Start starts the cache daemon components (scheduler, etc.)
func (d *CacheDaemon) Start(ctx context.Context) error {
	d.logger.Info("Starting cache daemon components")

	// Create scheduler context
	d.schedulerCtx, d.schedulerCancel = context.WithCancel(ctx)

	// Start scheduler in separate goroutine
	go d.Run(d.schedulerCtx)

	d.logger.Info("Cache daemon components started")
	return nil
}

// defaultSchedulerJoinTimeout bounds the wait for Run() to return after cancel.
// It is INDEPENDENT of the shutdown ctx on purpose: the common trigger is a
// shutdown ctx already expired on entry, and we must still wait the (now
// non-blocking) tick for the scheduler to stop so the drain and flush below are
// race-free. The tick body never blocks on dispatch, so Run returns well
// within this bound; the timer only guards a pathological non-returning Run.
const defaultSchedulerJoinTimeout = 30 * time.Second

const metricsServerShutdownTimeout = 5 * time.Second

// Shutdown gracefully shuts down the cache daemon. It accepts the process
// shutdown context, which bounds the in-flight dispatch drain. The scheduler
// join is bounded only by schedulerJoinTimeout (NOT by ctx) so an already
// expired ctx cannot let dispatchWG.Add race dispatchWG.Wait.
func (d *CacheDaemon) Shutdown(ctx context.Context) error {
	d.logger.Info("Shutting down cache daemon")

	// 1. Shutdown separate metrics server if exists.
	if d.metricsServer != nil {
		d.logger.Info("Shutting down separate metrics server")
		mctx, cancel := context.WithTimeout(context.Background(), metricsServerShutdownTimeout)
		if err := d.metricsServer.ShutdownWithContext(mctx); err != nil {
			d.logger.Error("Metrics server shutdown error", zap.Error(err))
		} else {
			d.logger.Info("Metrics server shutdown complete")
		}
		cancel()
	}

	// 2. Cancel the scheduler and join Run UNCONDITIONALLY, bounded only by the
	//    safety timer (NOT by ctx). dispatchWG.Add is called only on the Run
	//    goroutine, so the Wait in step 3 is safe only once Run has provably
	//    returned.
	if d.schedulerCancel != nil {
		d.schedulerCancel()
	}
	joinTimeout := d.schedulerJoinTimeout
	if joinTimeout == 0 {
		joinTimeout = defaultSchedulerJoinTimeout
	}
	joinTimer := time.NewTimer(joinTimeout)
	defer joinTimer.Stop()
	select {
	case <-d.schedulerDone:
		// Run returned: no further dispatchWG.Add can happen.
	case <-joinTimer.C:
		// Pathological: Run did not return. We cannot safely Wait on dispatchWG
		// (Add may still race) nor flush (the scheduler may still mutate the
		// iq), so abandon both and let the process exit. Loud log so residual
		// loss is observable, not silent.
		d.logger.Error("Scheduler did not stop; abandoning dispatch drain and queue flush to avoid WaitGroup race and lossy flush",
			zap.Int("internal_queue_remaining", d.internalQueue.Size()))
		return fmt.Errorf("scheduler did not stop within %s", joinTimeout)
	}

	// 3. Run has returned. Drain in-flight dispatches, bounded by the shutdown ctx.
	drained := make(chan struct{})
	go func() { d.dispatchWG.Wait(); close(drained) }()
	select {
	case <-drained:
		d.logger.Info("In-flight recache dispatches drained")
	case <-ctx.Done():
		d.logger.Warn("Timed out draining in-flight recache dispatches",
			zap.Int("internal_queue_remaining", d.internalQueue.Size()))
	}

	// 4. Flush the internal queue back to Redis. Safe: the scheduler has
	//    stopped, so the only possible producers are in-flight dispatch
	//    goroutines. If the drain completed, none remain and the flush is a
	//    clean final snapshot; on drain timeout it is best-effort.
	d.flushInternalQueueToRedis()

	d.logger.Info("Cache daemon shutdown complete")
	return nil
}

// flushInternalQueueToRedis re-pushes every entry left in the volatile internal
// queue back to its durable Redis ZSET on shutdown so deferred / retry-pending
// entries are not silently lost. FIFO position is preserved by reusing each
// entry's QueuedAt as the ZSET score. Best-effort: a ZAdd failure is logged at
// ERROR so any residual loss is observable instead of silent.
func (d *CacheDaemon) flushInternalQueueToRedis() {
	// Drain the whole queue in one shot. The scheduler has stopped, so no new
	// pulls race this; only in-flight dispatch goroutines might still enqueue
	// retry entries, and those are picked up if they land before this Dequeue.
	entries := d.internalQueue.Dequeue(d.internalQueue.Size())
	if len(entries) == 0 {
		return
	}

	ctx := context.Background()
	flushed := 0
	lost := 0
	for _, entry := range entries {
		priority := entry.Priority
		if priority == "" {
			// Entries always carry their source priority once pulled from
			// Redis; fall back to normal for any that predate that (e.g. a
			// directly seeded entry) so they are not lost to a malformed key.
			priority = redis.PriorityNormal
		}
		zsetKey := d.keyGenerator.RecacheQueueKey(entry.HostID, priority)
		member := types.RecacheMember{
			URL:         entry.URL,
			DimensionID: entry.DimensionID,
			Mode:        entry.Mode,
		}
		memberJSON, err := json.Marshal(member)
		if err != nil {
			d.logger.Error("Failed to marshal recache member during shutdown flush; entry lost",
				zap.Int("host_id", entry.HostID),
				zap.String("url", entry.URL),
				zap.Error(err))
			lost++
			continue
		}
		score := float64(entry.QueuedAt.UTC().Unix())
		if err := d.redis.ZAdd(ctx, zsetKey, score, string(memberJSON)); err != nil {
			d.logger.Error("Failed to flush internal queue entry back to Redis; entry lost",
				zap.Int("host_id", entry.HostID),
				zap.String("url", entry.URL),
				zap.String("priority", priority),
				zap.String("key", zsetKey),
				zap.Error(err))
			lost++
			continue
		}
		flushed++
	}

	d.logger.Info("Flushed internal queue to Redis on shutdown",
		zap.Int("flushed", flushed),
		zap.Int("lost", lost))
}

// GetConfiguredHosts returns a list of host IDs from the hosts configuration
func (d *CacheDaemon) GetConfiguredHosts() []int {
	hosts := d.configManager.GetHosts()
	hostIDs := make([]int, 0, len(hosts))

	for _, host := range hosts {
		hostIDs = append(hostIDs, host.ID)
	}

	return hostIDs
}

// GetHost returns a host configuration by ID
func (d *CacheDaemon) GetHost(hostID int) *types.Host {
	hosts := d.configManager.GetHosts()

	for i := range hosts {
		if hosts[i].ID == hostID {
			return &hosts[i]
		}
	}

	return nil
}

// GetRSCapacityStatus returns current render service capacity status
func (d *CacheDaemon) GetRSCapacityStatus() RSCapacityStatus {
	ctx := context.Background()

	rsInstances, err := d.rsRegistry.ListHealthyServices(ctx)
	if err != nil || len(rsInstances) == 0 {
		return RSCapacityStatus{
			TotalFreeTabs:       0,
			ReservedForOnline:   0,
			AvailableForRecache: 0,
			ReservationPercent:  d.daemonConfig.Recache.RSCapacityReserved * 100,
		}
	}

	totalFreeTabs := 0
	for _, rs := range rsInstances {
		freeTabs := rs.Capacity - rs.Load
		if freeTabs > 0 {
			totalFreeTabs += freeTabs
		}
	}

	reservedForOnline := int(float64(totalFreeTabs) * d.daemonConfig.Recache.RSCapacityReserved)
	availableForRecache := totalFreeTabs - reservedForOnline
	if availableForRecache < 0 {
		availableForRecache = 0
	}

	return RSCapacityStatus{
		TotalFreeTabs:       totalFreeTabs,
		ReservedForOnline:   reservedForOnline,
		AvailableForRecache: availableForRecache,
		ReservationPercent:  d.daemonConfig.Recache.RSCapacityReserved * 100,
	}
}

// PauseScheduler pauses the scheduler processing loop
func (d *CacheDaemon) PauseScheduler() {
	d.schedulerPauseMu.Lock()
	defer d.schedulerPauseMu.Unlock()
	d.schedulerPaused = true
	d.logger.Info("Scheduler paused")
}

// ResumeScheduler resumes the scheduler processing loop
func (d *CacheDaemon) ResumeScheduler() {
	d.schedulerPauseMu.Lock()
	defer d.schedulerPauseMu.Unlock()
	d.schedulerPaused = false
	d.logger.Info("Scheduler resumed")
}

// IsSchedulerPaused returns true if scheduler is paused
func (d *CacheDaemon) IsSchedulerPaused() bool {
	d.schedulerPauseMu.RLock()
	defer d.schedulerPauseMu.RUnlock()
	return d.schedulerPaused
}

// SetReloadFunc sets the reload hook called by POST /internal/reload.
// The supplied fn is wrapped so that, on success, the daemon refreshes its
// host lookup cache and the per-host concurrency limiter from the new config
// atomically — both updates happen while reloadMu is held for write so a
// concurrent scheduler tick can never observe one new and one old.
func (d *CacheDaemon) SetReloadFunc(fn func(ctx context.Context) error) {
	if fn == nil {
		d.reloadFunc = nil
		return
	}
	d.reloadFunc = func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		d.resyncDerivedCaches()
		return nil
	}
}

// getStaleTTL resolves the stale TTL in seconds from host config -> global config -> 0
func (d *CacheDaemon) getStaleTTL(host *types.Host) int64 {
	if host.Render.Cache != nil && host.Render.Cache.Expired != nil && host.Render.Cache.Expired.StaleTTL != nil {
		return int64(host.Render.Cache.Expired.StaleTTL.ToDuration().Seconds())
	}
	egConfig := d.configManager.GetConfig()
	if egConfig.Render.Cache.Expired != nil && egConfig.Render.Cache.Expired.StaleTTL != nil {
		return int64(egConfig.Render.Cache.Expired.StaleTTL.ToDuration().Seconds())
	}
	return 0
}

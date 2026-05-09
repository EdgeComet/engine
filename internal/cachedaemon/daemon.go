package cachedaemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/cachedaemon/metrics"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/metricsserver"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/hash"
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
		concurrencyLimiter: NewHostConcurrencyLimiter(egConfig, configManager.GetHosts()),
		metricsCollector:   metricsCollector,
		metricsServer:      metricsServer,
		cacheReader:        NewCacheReader(redisClient, keyGenerator, logger),
		queueReader:        NewQueueReader(redisClient, keyGenerator, internalQueue, logger),
	}

	daemon.reloadMu.Lock()
	daemon.rebuildHostByIDLocked()
	daemon.reloadMu.Unlock()

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

// actionForEntry resolves the dimension's effective action for a queue entry.
// Returns an empty URLRuleAction when host or dimension are missing — the
// scheduler treats these entries as unresolved and discards them before
// dispatch. Caller must hold d.reloadMu.RLock so hostByID stays stable for
// the duration of the lookup.
func (d *CacheDaemon) actionForEntry(entry InternalQueueEntry) types.URLRuleAction {
	host := d.hostByID[entry.HostID]
	if host == nil {
		return ""
	}
	for _, dim := range host.Dimensions {
		if dim.ID == entry.DimensionID {
			return dim.EffectiveAction()
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

// Shutdown gracefully shuts down the cache daemon
func (d *CacheDaemon) Shutdown() error {
	d.logger.Info("Shutting down cache daemon")

	// Shutdown separate metrics server if exists
	if d.metricsServer != nil {
		d.logger.Info("Shutting down separate metrics server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := d.metricsServer.ShutdownWithContext(ctx); err != nil {
			d.logger.Error("Metrics server shutdown error", zap.Error(err))
		} else {
			d.logger.Info("Metrics server shutdown complete")
		}
		cancel()
	}

	// Cancel scheduler context
	if d.schedulerCancel != nil {
		d.schedulerCancel()
	}

	d.logger.Info("Cache daemon shutdown complete")
	return nil
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
		eg := d.configManager.GetConfig()
		hosts := d.configManager.GetHosts()
		d.reloadMu.Lock()
		d.rebuildHostByIDLocked()
		d.concurrencyLimiter.Reload(eg, hosts)
		d.reloadMu.Unlock()
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

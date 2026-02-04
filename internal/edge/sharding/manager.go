package sharding

import (
	"context"
	"fmt"
	"time"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	interEgTimeout = 3 * time.Second // Timeout for inter-EG operations (pull/push/status)
)

// Manager coordinates all sharding operations
type Manager struct {
	config          *types.CacheShardingConfig
	egID            string
	internalAuthKey string
	registry        Registry
	distributor     Distributor
	client          Client
	metrics         *Metrics
	cacheService    *cache.CacheService
	redisClient     *redis.Client
	logger          *zap.Logger
	startTime       time.Time

	heartbeatCtx    context.Context
	heartbeatCancel context.CancelFunc
}

// NewManager creates a new sharding manager
func NewManager(
	config *types.CacheShardingConfig,
	egID string,
	internalAuthKey string,
	redisClient *redis.Client,
	cacheService *cache.CacheService,
	metricsNamespace string,
	logger *zap.Logger,
) (*Manager, error) {
	registry := NewRedisRegistry(redisClient, logger)

	// If sharding disabled, check for active cluster before allowing standalone startup
	if config == nil || config.Enabled == nil || !*config.Enabled {
		// Check for active cluster in Redis
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		clusterMembers, err := registry.GetClusterMembers(ctx)
		if err != nil {
			logger.Warn("Failed to check for active cluster, proceeding with standalone startup",
				zap.Error(err))
			// Fail-open on Redis errors - don't block startup if Redis is unreachable
		} else if len(clusterMembers) > 0 {
			// Active cluster detected - block startup
			return nil, fmt.Errorf(
				"cannot start with sharding disabled: active sharding cluster detected with %d members %v. "+
					"To proceed: (1) enable sharding in your config (cache_sharding.enabled: true), OR "+
					"(2) stop all cluster members and wait for their registry entries to expire (TTL: 3s)",
				len(clusterMembers), clusterMembers)
		}

		logger.Info("Sharding disabled, no active cluster detected - starting in standalone mode")

		return &Manager{
			config:          config,
			egID:            egID,
			internalAuthKey: internalAuthKey,
			logger:          logger,
			startTime:       time.Now().UTC(),
		}, nil
	}

	// Create distributor based on strategy
	strategy := config.DistributionStrategy
	if strategy == "" {
		strategy = "hash_modulo" // Default
	}
	distributor, err := DistributorFactory(strategy, registry, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create distributor: %w", err)
	}

	// Create client (uses HTTP for inter-EG communication)
	client := NewFastHTTPClient(registry, internalAuthKey, "http", interEgTimeout, logger)

	// Create metrics
	metrics := NewMetrics(metricsNamespace)

	return &Manager{
		config:          config,
		egID:            egID,
		internalAuthKey: internalAuthKey,
		registry:        registry,
		distributor:     distributor,
		client:          client,
		metrics:         metrics,
		cacheService:    cacheService,
		redisClient:     redisClient,
		logger:          logger,
		startTime:       time.Now().UTC(),
	}, nil
}

// Start initializes the sharding system (cluster registration and heartbeat)
func (m *Manager) Start(ctx context.Context, internalAddr string) error {
	// Skip startup if sharding disabled
	if !m.IsEnabled() {
		m.logger.Info("Sharding disabled, skipping cluster initialization")
		return nil
	}

	// Register in cluster
	if err := m.registry.Register(ctx, m.egID, internalAddr); err != nil {
		return fmt.Errorf("failed to register EG: %w", err)
	}

	m.logger.Info("Edge Gateway registered in cluster",
		zap.String("internal_address", internalAddr))

	// Start heartbeat loop
	m.heartbeatCtx, m.heartbeatCancel = context.WithCancel(ctx)
	go m.registry.(*RedisRegistry).HeartbeatLoop(m.heartbeatCtx)

	// Update cluster size metric
	healthyEGs, err := m.registry.GetHealthyEGs(ctx)
	if err == nil {
		m.metrics.UpdateClusterSize(len(healthyEGs))
	}

	return nil
}

// Shutdown gracefully stops the sharding system
func (m *Manager) Shutdown(ctx context.Context) error {
	// Skip shutdown if sharding disabled
	if !m.IsEnabled() {
		return nil
	}

	m.logger.Info("Shutting down sharding system")

	// Stop heartbeat
	if m.heartbeatCancel != nil {
		m.heartbeatCancel()
	}

	// Deregister from cluster
	if err := m.registry.Deregister(ctx, m.egID); err != nil {
		m.logger.Warn("Failed to deregister from cluster", zap.Error(err))
	}

	m.logger.Info("Sharding system shutdown complete")
	return nil
}

// ComputeTargets computes which EGs should store a cache entry
func (m *Manager) ComputeTargets(ctx context.Context, cacheKey string) ([]string, error) {
	replicationFactor := 2 // Default
	if m.config.ReplicationFactor != nil {
		replicationFactor = *m.config.ReplicationFactor
	}

	return m.distributor.ComputeTargets(ctx, cacheKey, m.egID, replicationFactor)
}

// IsTargetForCache checks if the current EG should store this cache based on hash distribution
// Returns true if current EG is in the computed target list for this cache key
// Uses pure hash-based distribution (no rendering EG override) for pull operations
func (m *Manager) IsTargetForCache(ctx context.Context, cacheKey string) (bool, error) {
	// If sharding disabled, always store locally
	if !m.IsEnabled() {
		return true, nil
	}

	// Get replication factor
	replicationFactor := 2 // Default
	if m.config.ReplicationFactor != nil {
		replicationFactor = *m.config.ReplicationFactor
	}

	// Compute pure hash-based targets (no rendering EG override)
	targets, err := m.distributor.ComputeHashTargets(ctx, cacheKey, replicationFactor)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash targets: %w", err)
	}

	// Check if self is in target list
	for _, target := range targets {
		if target == m.egID {
			return true, nil
		}
	}

	return false, nil
}

// PushToTargets pushes cache to target EGs
func (m *Manager) PushToTargets(ctx context.Context, cacheKey *types.CacheKey, content []byte, metadata *cache.CacheMetadata, targetEgIDs []string, requestID string) ([]string, error) {
	// Start with self - cache is already stored locally
	successfulEgIDs := []string{m.egID}

	// Filter out self from targets (already stored locally)
	targets := []string{}
	for _, egID := range targetEgIDs {
		if egID != m.egID {
			targets = append(targets, egID)
		}
	}

	if len(targets) == 0 {
		return successfulEgIDs, nil // No remote targets, only self
	}

	req := &PushRequest{
		HostID:      cacheKey.HostID,
		DimensionID: cacheKey.DimensionID,
		URLHash:     cacheKey.URLHash,
		Content:     content,
		CreatedAt:   metadata.CreatedAt,
		ExpiresAt:   metadata.ExpiresAt,
		RequestID:   requestID,
		FilePath:    metadata.FilePath, // Includes compression extension
	}

	// Push in parallel
	results := m.client.(*FastHTTPClient).PushParallel(ctx, targets, req)

	// Track metrics and collect successful EGs
	successCount := 0
	for egID, err := range results {
		if err == nil {
			successCount++
			successfulEgIDs = append(successfulEgIDs, egID)
			m.metrics.RecordPushRequest(egID, true, 0) // Duration tracked by client
			m.metrics.RecordBytesTransferred("push", "sent", len(content))
		} else {
			m.logger.Warn("Failed to push cache to target EG",
				zap.String("target_eg", egID),
				zap.String("cache_key", cacheKey.String()),
				zap.String("request_id", requestID),
				zap.Error(err))
			m.metrics.RecordPushRequest(egID, false, 0)
			m.metrics.RecordError("push_failed")
		}
	}

	if successCount == 0 && len(targets) > 0 {
		m.logger.Warn("Failed to push cache to all remote target EGs (stored locally only)",
			zap.String("cache_key", cacheKey.String()),
			zap.String("request_id", requestID),
			zap.Int("target_count", len(targets)))
		return successfulEgIDs, fmt.Errorf("failed to push to all %d remote targets (replication under-satisfied)", len(targets))
	} else if successCount < len(targets) {
		m.logger.Warn("Partial push success - some remote targets failed",
			zap.String("cache_key", cacheKey.String()),
			zap.String("request_id", requestID),
			zap.Int("success_count", successCount),
			zap.Int("target_count", len(targets)),
			zap.Int("failed_count", len(targets)-successCount))
	}

	return successfulEgIDs, nil
}

// PullFromRemote attempts to pull cache from remote EGs
// Uses hash-based peer selection to distribute load across replicas
func (m *Manager) PullFromRemote(ctx context.Context, cacheKey *types.CacheKey, egIDs []string) ([]byte, error) {
	// Hash-based peer selection for load distribution
	// Different cache keys will rotate the peer list differently, spreading load
	hashValue := xxhash.Sum64String(cacheKey.String())
	selectedIndex := int(hashValue % uint64(len(egIDs)))
	orderedPeers := rotateSlice(egIDs, selectedIndex)

	for _, egID := range orderedPeers {
		if egID == m.egID {
			continue // Skip self
		}

		start := time.Now()
		req := &PullRequest{
			HostID:      cacheKey.HostID,
			DimensionID: cacheKey.DimensionID,
			URLHash:     cacheKey.URLHash,
		}

		resp, err := m.client.Pull(ctx, egID, req)
		duration := time.Since(start).Seconds()

		if err == nil {
			m.metrics.RecordPullRequest(egID, true, duration)
			m.metrics.RecordBytesTransferred("pull", "received", len(resp.Content))
			m.logger.Info("Successfully pulled cache from remote EG",
				zap.String("source_eg", egID),
				zap.String("cache_key", cacheKey.String()),
				zap.Int("content_size", len(resp.Content)))
			return resp.Content, nil
		}

		m.logger.Warn("Failed to pull cache from EG, trying next",
			zap.String("source_eg", egID),
			zap.String("cache_key", cacheKey.String()),
			zap.Error(err))
		m.metrics.RecordPullRequest(egID, false, duration)
		m.metrics.RecordError("pull_failed")
	}

	return nil, fmt.Errorf("failed to pull cache from all EGs")
}

// GetMetrics returns the metrics instance
func (m *Manager) GetMetrics() *Metrics {
	return m.metrics
}

// IsEnabled returns whether sharding is enabled
func (m *Manager) IsEnabled() bool {
	if m == nil {
		return false
	}
	return m.config != nil && m.config.Enabled != nil && *m.config.Enabled
}

// GetEgID returns the EG ID
func (m *Manager) GetEgID() string {
	if m == nil {
		return ""
	}
	return m.egID
}

// GetReplicationFactor returns the configured replication factor
func (m *Manager) GetReplicationFactor() int {
	if m == nil || m.config == nil || m.config.ReplicationFactor == nil {
		return 2 // Default replication factor
	}
	return *m.config.ReplicationFactor
}

// GetInterEgTimeout returns the inter-EG timeout constant
func (m *Manager) GetInterEgTimeout() time.Duration {
	return interEgTimeout
}

// GetHealthyEGs returns a list of all healthy/alive EG instances
func (m *Manager) GetHealthyEGs(ctx context.Context) ([]EGInfo, error) {
	if m == nil || m.registry == nil {
		return nil, fmt.Errorf("manager or registry not initialized")
	}
	return m.registry.GetHealthyEGs(ctx)
}

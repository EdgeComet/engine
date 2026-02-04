package sharding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	registryKeyPrefix = "registry:eg:"

	defaultRegistryTTL       = 3 * time.Second
	defaultHeartbeatInterval = 1 * time.Second
)

// EGInfo is an alias for the common EGInfo type
type EGInfo = types.EGInfo

// Registry manages EG registration and discovery
type Registry interface {
	Register(ctx context.Context, egID string, address string) error
	Deregister(ctx context.Context, egID string) error
	Heartbeat(ctx context.Context) error
	GetHealthyEGs(ctx context.Context) ([]EGInfo, error)
	GetEGAddress(ctx context.Context, egID string) (string, error)
	GetClusterMembers(ctx context.Context) ([]string, error)
}

// RedisRegistry implements Registry using Redis as the backend
type RedisRegistry struct {
	redis             *redis.Client
	logger            *zap.Logger
	registryTTL       time.Duration
	heartbeatInterval time.Duration

	// Store registered EG info for self-healing heartbeat
	// Set once during Register(), read-only during Heartbeat()
	egID    string
	address string
}

// NewRedisRegistry creates a new Redis-based registry
// If ttl or interval are zero, defaults are used (10s TTL, 2s heartbeat)
func NewRedisRegistry(redisClient *redis.Client, logger *zap.Logger) *RedisRegistry {
	return &RedisRegistry{
		redis:             redisClient,
		logger:            logger,
		registryTTL:       defaultRegistryTTL,
		heartbeatInterval: defaultHeartbeatInterval,
	}
}

// Register registers an EG instance in the registry
// Checks for duplicate eg_id, then delegates to Heartbeat() for actual registration
func (r *RedisRegistry) Register(ctx context.Context, egID string, address string) error {
	if egID == "" {
		return fmt.Errorf("eg_id cannot be empty")
	}
	if address == "" {
		return fmt.Errorf("address cannot be empty")
	}

	key := registryKeyPrefix + egID

	// Check if already registered by another instance
	exists, err := r.redis.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to check if eg_id exists: %w", err)
	}
	if exists {
		r.logger.Error("Looks like stale redis key", zap.String("eg", egID))
		return fmt.Errorf("eg_id '%s' is already registered by another instance", egID)
	}

	// Store EG info for heartbeat
	r.egID = egID
	r.address = address

	// Create initial registry entry via Heartbeat
	if err := r.Heartbeat(ctx); err != nil {
		// Clean up stored info on failure
		r.egID = ""
		r.address = ""
		return fmt.Errorf("failed to create initial registry entry: %w", err)
	}

	r.logger.Debug("EG registered in cluster",
		zap.String("eg_id", egID),
		zap.String("address", address),
		zap.Duration("ttl", r.registryTTL))

	return nil
}

// Deregister removes an EG instance from the registry
func (r *RedisRegistry) Deregister(ctx context.Context, egID string) error {
	if egID == "" {
		return fmt.Errorf("eg_id cannot be empty")
	}

	key := registryKeyPrefix + egID

	if err := r.redis.Del(ctx, key); err != nil {
		return fmt.Errorf("failed to deregister EG from Redis: %w", err)
	}

	// Clear stored info
	r.egID = ""
	r.address = ""

	r.logger.Info("EG deregistered from cluster")

	return nil
}

// Heartbeat updates the heartbeat timestamp for an EG instance
// Simply recreates the registry entry on every heartbeat (create-or-update pattern)
// This is simpler and more robust than GET-UPDATE-SET
func (r *RedisRegistry) Heartbeat(ctx context.Context) error {
	// Verify this registry was registered
	if r.egID == "" || r.address == "" {
		return fmt.Errorf("registry not initialized, must call Register() first")
	}

	address := r.address

	key := registryKeyPrefix + r.egID

	// Create fresh registry entry with current timestamp
	info := EGInfo{
		EgID:            r.egID,
		Address:         address,
		LastHeartbeat:   time.Now().UTC(),
		ShardingEnabled: true,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal registry data: %w", err)
	}

	if err := r.redis.Set(ctx, key, string(data), r.registryTTL); err != nil {
		return fmt.Errorf("failed to update heartbeat in Redis: %w", err)
	}

	return nil
}

// GetHealthyEGs returns a list of all healthy EG instances
// EGs are considered healthy if their registry key exists (TTL has not expired)
func (r *RedisRegistry) GetHealthyEGs(ctx context.Context) ([]EGInfo, error) {
	pattern := registryKeyPrefix + "*"
	keys, err := r.redis.Keys(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to query registry keys: %w", err)
	}

	var healthyEGs []EGInfo
	for _, key := range keys {
		data, err := r.redis.Get(ctx, key)
		if err != nil {
			r.logger.Warn("Failed to get registry data for key",
				zap.String("key", key),
				zap.Error(err))
			continue
		}

		var info EGInfo
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			r.logger.Warn("Failed to unmarshal registry data",
				zap.String("key", key),
				zap.Error(err))
			continue
		}

		// Only include EGs with sharding enabled
		if info.ShardingEnabled {
			healthyEGs = append(healthyEGs, info)
		}
	}

	// Sort EGs alphabetically by ID for deterministic ordering
	sort.Slice(healthyEGs, func(i, j int) bool {
		return healthyEGs[i].EgID < healthyEGs[j].EgID
	})

	return healthyEGs, nil
}

// GetEGAddress retrieves the address of a specific EG instance
func (r *RedisRegistry) GetEGAddress(ctx context.Context, egID string) (string, error) {
	if egID == "" {
		return "", fmt.Errorf("eg_id cannot be empty")
	}

	key := registryKeyPrefix + egID

	data, err := r.redis.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("EG not found in registry: %w", err)
	}

	var info EGInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return "", fmt.Errorf("failed to unmarshal registry data: %w", err)
	}

	return info.Address, nil
}

// GetClusterMembers returns a list of all EG IDs currently registered in the cluster
// This includes all EGs with active registry keys (regardless of health/sharding status)
// Used for detecting cluster presence when starting with sharding disabled
func (r *RedisRegistry) GetClusterMembers(ctx context.Context) ([]string, error) {
	pattern := registryKeyPrefix + "*"
	keys, err := r.redis.Keys(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to query registry keys: %w", err)
	}

	memberIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		// Extract eg_id from "registry:eg:eg1" -> "eg1"
		// Key format: "registry:eg:<eg_id>"
		if len(key) > len(registryKeyPrefix) {
			egID := key[len(registryKeyPrefix):]
			memberIDs = append(memberIDs, egID)
		}
	}

	// Sort alphabetically for deterministic ordering
	sort.Strings(memberIDs)

	return memberIDs, nil
}

// HeartbeatLoop runs a heartbeat loop that periodically updates the EG's heartbeat in the registry
// It should be run in a goroutine and will continue until the context is cancelled
func (r *RedisRegistry) HeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.Heartbeat(ctx); err != nil {
				r.logger.Warn("Heartbeat failed",
					zap.Error(err))
			}
		case <-ctx.Done():
			r.logger.Info("Heartbeat loop stopped")
			return
		}
	}
}

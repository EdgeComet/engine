package redis

import (
	"fmt"

	"github.com/edgecomet/engine/pkg/types"
)

const (
	lockKeyPrefix     = "lock:"
	metadataKeyPrefix = "meta:"
)

// Priority levels for recache queues
const (
	PriorityHigh        = "high"
	PriorityNormal      = "normal"
	PriorityAutorecache = "autorecache"
)

// KeyGenerator provides universal Redis key generation for cache operations
type KeyGenerator struct{}

// NewKeyGenerator creates a new KeyGenerator instance
func NewKeyGenerator() *KeyGenerator {
	return &KeyGenerator{}
}

// GenerateCacheKey creates a cache key from provided components
func (kg *KeyGenerator) GenerateCacheKey(hostID int, dimensionID int, urlHash string) *types.CacheKey {
	return &types.CacheKey{
		HostID:      hostID,
		DimensionID: dimensionID,
		URLHash:     urlHash,
	}
}

// ParseCacheKey extracts components from cache key string
func (kg *KeyGenerator) ParseCacheKey(cacheKeyStr string) (*types.CacheKey, error) {
	var hostID, dimensionID int
	var urlHash string

	n, err := fmt.Sscanf(cacheKeyStr, "cache:%d:%d:%s", &hostID, &dimensionID, &urlHash)
	if err != nil || n != 3 {
		return nil, fmt.Errorf("invalid cache key format: %s", cacheKeyStr)
	}

	return &types.CacheKey{
		HostID:      hostID,
		DimensionID: dimensionID,
		URLHash:     urlHash,
	}, nil
}

// GenerateLockKey generates the Redis lock key for a cache key
func (kg *KeyGenerator) GenerateLockKey(cacheKey *types.CacheKey) string {
	return lockKeyPrefix + cacheKey.String()
}

// GenerateMetadataKey generates the Redis metadata key for a cache key
func (kg *KeyGenerator) GenerateMetadataKey(cacheKey *types.CacheKey) string {
	return metadataKeyPrefix + cacheKey.String()
}

// RecacheQueueKey returns Redis key for recache queue (ZSET)
// Format: recache:{hostID}:{priority}
func (kg *KeyGenerator) RecacheQueueKey(hostID int, priority string) string {
	return fmt.Sprintf("recache:%d:%s", hostID, priority)
}

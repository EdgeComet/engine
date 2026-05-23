package types

import (
	"fmt"
	"time"
)

// CacheKey represents a unique cache identifier
type CacheKey struct {
	HostID      int    `json:"h"`
	DimensionID int    `json:"d"`
	URLHash     uint64 `json:"u"`
}

// String returns cache key in Redis format
func (ck CacheKey) String() string {
	return fmt.Sprintf("cache:%d:%d:%d", ck.HostID, ck.DimensionID, ck.URLHash)
}

// ParseCacheKey parses a cache key string in format "cache:host_id:dimension_id:url_hash"
func ParseCacheKey(s string) (*CacheKey, error) {
	var hostID, dimensionID int
	var urlHash uint64

	n, err := fmt.Sscanf(s, "cache:%d:%d:%d", &hostID, &dimensionID, &urlHash)
	if err != nil || n != 3 {
		return nil, fmt.Errorf("invalid cache key format: %s", s)
	}

	return &CacheKey{
		HostID:      hostID,
		DimensionID: dimensionID,
		URLHash:     urlHash,
	}, nil
}

// EGInfo represents information about an Edge Gateway instance in the registry
type EGInfo struct {
	EgID            string    `json:"eg_id"`
	Address         string    `json:"address"`
	LastHeartbeat   time.Time `json:"last_heartbeat"`
	ShardingEnabled bool      `json:"sharding_enabled"`
}

// CacheExpiredConfig defines behavior when cache entries expire
type CacheExpiredConfig struct {
	Strategy string    `yaml:"strategy" json:"strategy"`                       // "serve_stale" | "delete"
	StaleTTL *Duration `yaml:"stale_ttl,omitempty" json:"stale_ttl,omitempty"` // Time to live for stale cache after expiration
}

// Expiration strategy constants
const (
	ExpirationStrategyServeStale = "serve_stale" // Keep serving expired cache while recaching
	ExpirationStrategyDelete     = "delete"      // Delete expired cache and force fresh render
)

// Cache TTL constants
const (
	NoCacheTTL = 0 // Disables caching - content always fetched fresh
)

// Registry selection strategy constants
const (
	SelectionStrategyLeastLoaded   = "least_loaded"   // Select service with lowest load percentage
	SelectionStrategyMostAvailable = "most_available" // Select service with most available tabs
)

// Compression algorithm constants
const (
	CompressionNone   = "none"   // No compression
	CompressionSnappy = "snappy" // Snappy compression (default)
	CompressionLZ4    = "lz4"    // LZ4 compression
)

// Compression file extension constants
const (
	ExtSnappy = ".snappy"
	ExtLZ4    = ".lz4"
)

// CompressionMinSize is the minimum content size in bytes for compression to be applied.
// Files smaller than this are stored uncompressed.
const CompressionMinSize = 1024

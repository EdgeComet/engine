package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// Cache source constants
const (
	SourceRender = "render" // Cache entry from rendered content
	SourceBypass = "bypass" // Cache entry from bypass (direct fetch)
)

type CacheMetadata struct {
	Key         string              `json:"key"`
	URL         string              `json:"url"`
	FilePath    string              `json:"file_path"`
	HostID      int                 `json:"host_id"`
	Dimension   string              `json:"dimension"`
	RequestID   string              `json:"request_id"`
	CreatedAt   time.Time           `json:"created_at"`
	ExpiresAt   time.Time           `json:"expires_at"`
	Size        int64               `json:"size"`
	DiskSize    int64               `json:"disk_size"`
	LastAccess  time.Time           `json:"last_access"`
	Source      string              `json:"source"`                 // "render" or "bypass"
	StatusCode  int                 `json:"status_code"`            // HTTP status code (200, 404, etc.)
	Headers     map[string][]string `json:"headers,omitempty"`      // Important HTTP headers
	EgIDs       []string            `json:"eg_ids,omitempty"`       // List of EG IDs that store this cache (sharding)
	LastBotHit  *int64              `json:"last_bot_hit,omitempty"` // Unix timestamp, nil if not tracked
	IndexStatus int                 `json:"index_status,omitempty"` // Indexation status (1=indexable, 2=non200, 3=blocked, 4=noncanonical)
	Title       string              `json:"title,omitempty"`        // Page title extracted from HTML
}

func (cm *CacheMetadata) IsExpired() bool {
	return time.Now().UTC().After(cm.ExpiresAt)
}

func (cm *CacheMetadata) TTL() time.Duration {
	if cm.IsExpired() {
		return 0
	}
	return cm.ExpiresAt.Sub(time.Now().UTC())
}

// IsFresh returns true if cache has not expired
func (cm *CacheMetadata) IsFresh() bool {
	return !cm.IsExpired()
}

// IsStale returns true if cache is in stale period (expired but within stale TTL)
func (cm *CacheMetadata) IsStale(staleTTL time.Duration) bool {
	now := time.Now().UTC()
	staleExpiresAt := cm.ExpiresAt.Add(staleTTL)
	return now.After(cm.ExpiresAt) && now.Before(staleExpiresAt)
}

// StaleAge returns how long the cache has been stale (0 if not expired)
func (cm *CacheMetadata) StaleAge() time.Duration {
	if !cm.IsExpired() {
		return 0
	}
	return time.Now().UTC().Sub(cm.ExpiresAt)
}

// IsEmpty returns true if no EG IDs are stored
func (cm *CacheMetadata) IsEmpty() bool {
	return len(cm.EgIDs) == 0
}

// HasEgID checks if the given EG ID is in the list
func (cm *CacheMetadata) HasEgID(egID string) bool {
	for _, id := range cm.EgIDs {
		if id == egID {
			return true
		}
	}
	return false
}

// AddEgID adds an EG ID to the list if not already present (idempotent)
func (cm *CacheMetadata) AddEgID(egID string) {
	if !cm.HasEgID(egID) {
		cm.EgIDs = append(cm.EgIDs, egID)
	}
}

// SetEgIDs replaces the entire EG ID list
func (cm *CacheMetadata) SetEgIDs(egIDs []string) {
	cm.EgIDs = egIDs
}

// GetRemoteEgIDs returns all EG IDs except the given selfEgID
func (cm *CacheMetadata) GetRemoteEgIDs(selfEgID string) []string {
	remote := []string{}
	for _, egID := range cm.EgIDs {
		if egID != selfEgID {
			remote = append(remote, egID)
		}
	}
	return remote
}

// Count returns the number of EG IDs
func (cm *CacheMetadata) Count() int {
	return len(cm.EgIDs)
}

// ToHash converts CacheMetadata to Redis hash fields
func (cm *CacheMetadata) ToHash() map[string]interface{} {
	hash := map[string]interface{}{
		"key":         cm.Key,
		"url":         cm.URL,
		"file_path":   cm.FilePath,
		"host_id":     cm.HostID,
		"dimension":   cm.Dimension,
		"request_id":  cm.RequestID,
		"created_at":  cm.CreatedAt.Unix(),
		"expires_at":  cm.ExpiresAt.Unix(),
		"size":        cm.Size,
		"disk_size":   cm.DiskSize,
		"last_access": cm.LastAccess.Unix(),
		"source":      cm.Source,
		"status_code": cm.StatusCode,
	}

	// JSON-encode headers map if present
	if len(cm.Headers) > 0 {
		if headersJSON, err := json.Marshal(cm.Headers); err == nil {
			hash["headers"] = string(headersJSON)
		}
	}

	// Convert and add EG IDs if present (sharding support)
	// Inline conversion: sort for consistency, then join with commas
	if len(cm.EgIDs) > 0 {
		sorted := make([]string, len(cm.EgIDs))
		copy(sorted, cm.EgIDs)
		sort.Strings(sorted)
		hash["eg_ids"] = strings.Join(sorted, ",")
	}

	// Add last_bot_hit if present
	if cm.LastBotHit != nil {
		hash["last_bot_hit"] = *cm.LastBotHit
	}

	// Add index_status if non-zero
	if cm.IndexStatus != 0 {
		hash["index_status"] = cm.IndexStatus
	}

	// Add title if non-empty
	if cm.Title != "" {
		hash["title"] = cm.Title
	}

	return hash
}

// FromHash populates CacheMetadata from Redis hash fields
func (cm *CacheMetadata) FromHash(data map[string]string) error {
	cm.Key = data["key"]
	cm.URL = data["url"]
	cm.FilePath = data["file_path"]
	cm.Dimension = data["dimension"]
	cm.RequestID = data["request_id"]
	cm.Source = data["source"]

	// Parse integer fields
	if hostID, err := strconv.Atoi(data["host_id"]); err != nil {
		return fmt.Errorf("invalid host_id: %w", err)
	} else {
		cm.HostID = hostID
	}

	if statusCode, err := strconv.Atoi(data["status_code"]); err != nil {
		return fmt.Errorf("invalid status_code: %w", err)
	} else {
		cm.StatusCode = statusCode
	}

	if size, err := strconv.ParseInt(data["size"], 10, 64); err != nil {
		return fmt.Errorf("invalid size: %w", err)
	} else {
		cm.Size = size
	}

	// Parse disk_size if present (backward compat: defaults to 0 if missing)
	if diskSizeStr, ok := data["disk_size"]; ok && diskSizeStr != "" {
		diskSize, err := strconv.ParseInt(diskSizeStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid disk_size: %w", err)
		}
		cm.DiskSize = diskSize
	}

	// Parse timestamp fields
	if createdAt, err := strconv.ParseInt(data["created_at"], 10, 64); err != nil {
		return fmt.Errorf("invalid created_at: %w", err)
	} else {
		cm.CreatedAt = time.Unix(createdAt, 0).UTC()
	}

	if expiresAt, err := strconv.ParseInt(data["expires_at"], 10, 64); err != nil {
		return fmt.Errorf("invalid expires_at: %w", err)
	} else {
		cm.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	}

	if lastAccess, err := strconv.ParseInt(data["last_access"], 10, 64); err != nil {
		return fmt.Errorf("invalid last_access: %w", err)
	} else {
		cm.LastAccess = time.Unix(lastAccess, 0).UTC()
	}

	// Parse headers JSON if present
	if headersJSON, exists := data["headers"]; exists && headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &cm.Headers); err != nil {
			return fmt.Errorf("invalid headers JSON: %w", err)
		}
	}

	// Parse EG IDs if present (sharding support)
	// Inline conversion: split comma-separated string, trim whitespace, filter empties
	if egIDsStr, exists := data["eg_ids"]; exists && egIDsStr != "" {
		parts := strings.Split(egIDsStr, ",")
		cm.EgIDs = make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				cm.EgIDs = append(cm.EgIDs, trimmed)
			}
		}
	}

	// Parse last_bot_hit if present
	if lastBotHitStr, exists := data["last_bot_hit"]; exists && lastBotHitStr != "" {
		if lastBotHit, err := strconv.ParseInt(lastBotHitStr, 10, 64); err == nil {
			cm.LastBotHit = &lastBotHit
		}
	}

	// Parse index_status if present
	if indexStatusStr, exists := data["index_status"]; exists && indexStatusStr != "" {
		if indexStatus, err := strconv.Atoi(indexStatusStr); err == nil {
			cm.IndexStatus = indexStatus
		}
	}

	// Parse title if present
	cm.Title = data["title"]

	return nil
}

type MetadataStore struct {
	redis        *redis.Client
	keyGenerator *redis.KeyGenerator
	logger       *zap.Logger
	cacheDir     string
}

func NewMetadataStore(redisClient *redis.Client, keyGenerator *redis.KeyGenerator, cacheDir string, logger *zap.Logger) *MetadataStore {
	return &MetadataStore{
		redis:        redisClient,
		keyGenerator: keyGenerator,
		logger:       logger,
		cacheDir:     cacheDir,
	}
}

func (ms *MetadataStore) GetCacheEntry(ctx context.Context, cacheKey *types.CacheKey) (*CacheMetadata, error) {
	if cacheKey == nil {
		return nil, fmt.Errorf("cache key is required")
	}

	metaKey := ms.keyGenerator.GenerateMetadataKey(cacheKey)
	data, err := ms.redis.HGetAll(ctx, metaKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var metadata CacheMetadata
	if err := metadata.FromHash(data); err != nil {
		ms.logger.Error("Failed to parse cache metadata from hash",
			zap.String("key", cacheKey.String()),
			zap.Error(err))
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

func (ms *MetadataStore) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		// Config initialization ensures this is always set, so this indicates a bug
		return false, fmt.Errorf("invalid lock TTL: %s (must be positive)", ttl)
	}

	acquired, err := ms.redis.SetNX(ctx, key, "locked", ttl)
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		ms.logger.Debug("Lock acquired",
			zap.String("key", key),
			zap.Duration("ttl", ttl))
	}

	return acquired, nil
}

func (ms *MetadataStore) ReleaseLock(ctx context.Context, key string) error {
	if err := ms.redis.Del(ctx, key); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	ms.logger.Debug("Lock released", zap.String("key", key))
	return nil
}

// GenerateFilePath generates the filesystem path for a cache key without creating metadata
func (ms *MetadataStore) GenerateFilePath(cacheKey *types.CacheKey, timestamp time.Time) string {
	return ms.generateFilePath(cacheKey, timestamp)
}

// StoreMetadata stores pre-constructed metadata directly
func (ms *MetadataStore) StoreMetadata(ctx context.Context, metadata *CacheMetadata, cacheKey *types.CacheKey, staleTTL time.Duration) error {
	return ms.storeMetadata(ctx, metadata, cacheKey, staleTTL)
}

// DeleteMetadata deletes cache metadata from Redis
func (ms *MetadataStore) DeleteMetadata(ctx context.Context, cacheKey *types.CacheKey) error {
	metaKey := ms.keyGenerator.GenerateMetadataKey(cacheKey)
	return ms.redis.Del(ctx, metaKey)
}

func (ms *MetadataStore) storeMetadata(ctx context.Context, metadata *CacheMetadata, cacheKey *types.CacheKey, staleTTL time.Duration) error {
	metaKey := ms.keyGenerator.GenerateMetadataKey(cacheKey)

	// Calculate Redis TTL: base TTL + stale TTL (if enabled)
	redisTTL := metadata.TTL()
	if staleTTL > 0 {
		redisTTL = redisTTL + staleTTL
	}

	// CRITICAL: Refuse to store metadata that's already expired (TTL=0 means no expiration in Redis)
	if redisTTL <= 0 {
		ms.logger.Warn("Refusing to store expired cache metadata",
			zap.String("key", cacheKey.String()),
			zap.Duration("base_ttl", metadata.TTL()),
			zap.Duration("stale_ttl", staleTTL),
			zap.Duration("redis_ttl", redisTTL),
			zap.Time("expires_at", metadata.ExpiresAt))
		return fmt.Errorf("cache already expired (base_ttl=%s, stale_ttl=%s, redis_ttl=%s), refusing storage to prevent infinite Redis TTL",
			metadata.TTL(), staleTTL, redisTTL)
	}

	// Convert metadata to hash fields
	hashData := metadata.ToHash()

	// Flatten hash map for HSet variadic parameters
	var values []interface{}
	for k, v := range hashData {
		values = append(values, k, v)
	}

	if err := ms.redis.HSetWithExpire(ctx, metaKey, redisTTL, values...); err != nil {
		return fmt.Errorf("failed to store metadata in Redis: %w", err)
	}

	return nil
}

func (ms *MetadataStore) generateFilePath(cacheKey *types.CacheKey, timestamp time.Time) string {
	year := timestamp.Format("2006")
	month := timestamp.Format("01")
	day := timestamp.Format("02")
	hour := timestamp.Format("15")
	minute := timestamp.Format("04")

	filename := fmt.Sprintf("%s_%d.html", cacheKey.URLHash, cacheKey.DimensionID)

	// Return RELATIVE path (without base_path) so it can be used across different EGs
	return filepath.Join(
		fmt.Sprintf("%d", cacheKey.HostID),
		year, month, day, hour, minute,
		filename)
}

// GetAbsoluteFilePath converts a relative file path to an absolute path
// by prepending the base_path (cache directory) configured for this EG.
// Returns error if the resolved path escapes the cache directory (path traversal).
func (ms *MetadataStore) GetAbsoluteFilePath(relativePath string) (string, error) {
	absPath := filepath.Join(ms.cacheDir, relativePath)
	cleanPath := filepath.Clean(absPath)
	cleanBase := filepath.Clean(ms.cacheDir)
	if !strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) && cleanPath != cleanBase {
		return "", fmt.Errorf("path escapes cache directory: %s", relativePath)
	}
	return cleanPath, nil
}

// UpdateLastBotHit updates the last_bot_hit field in cache metadata
func (ms *MetadataStore) UpdateLastBotHit(ctx context.Context, cacheKey *types.CacheKey, timestamp time.Time) error {
	metaKey := ms.keyGenerator.GenerateMetadataKey(cacheKey)
	if err := ms.redis.HSet(ctx, metaKey, "last_bot_hit", timestamp.Unix()); err != nil {
		return fmt.Errorf("failed to update last_bot_hit: %w", err)
	}
	return nil
}

// ClearLastBotHit removes the last_bot_hit field from cache metadata
func (ms *MetadataStore) ClearLastBotHit(ctx context.Context, cacheKey *types.CacheKey) error {
	metaKey := ms.keyGenerator.GenerateMetadataKey(cacheKey)
	if err := ms.redis.HDel(ctx, metaKey, "last_bot_hit"); err != nil {
		return fmt.Errorf("failed to clear last_bot_hit: %w", err)
	}
	return nil
}

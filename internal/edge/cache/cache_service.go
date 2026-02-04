package cache

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

// CacheResponse holds cache information for serving
// Supports both file-based and memory-based serving modes
// IMPORTANT: Either FilePath OR Content should be set, never both
type CacheResponse struct {
	FilePath    string        // File-based serving: path to cache file on disk
	Content     []byte        // Memory-based serving: cache content in memory
	ContentSize int64         // Size of content (for both modes)
	CacheAge    time.Duration // Age of cache entry
}

// IsMemoryBased returns true if this response should be served from memory
func (cr *CacheResponse) IsMemoryBased() bool {
	return len(cr.Content) > 0
}

// IsFileBased returns true if this response should be served from file
func (cr *CacheResponse) IsFileBased() bool {
	return cr.FilePath != ""
}

// CacheService handles all cache-related operations
type CacheService struct {
	metadata   *MetadataStore
	filesystem *FilesystemCache
	logger     *zap.Logger
}

// NewCacheService creates a new CacheService instance
func NewCacheService(metadata *MetadataStore, filesystem *FilesystemCache, logger *zap.Logger) *CacheService {
	return &CacheService{
		metadata:   metadata,
		filesystem: filesystem,
		logger:     logger,
	}
}

// GetCacheEntry retrieves cache metadata if available
// Returns the cache metadata and a boolean indicating if it exists (true for both fresh and expired)
// Orchestrator will use metadata.IsFresh() to determine if cache is still valid
func (cs *CacheService) GetCacheEntry(renderCtx *edgectx.RenderContext) (*CacheMetadata, bool) {
	reqCtx, cancel := renderCtx.ContextWithTimeout(5 * time.Second) // Quick cache operation
	defer cancel()

	renderCtx.Logger.Debug("Checking cache for entry")

	metadata, err := cs.metadata.GetCacheEntry(reqCtx, renderCtx.CacheKey)
	if err != nil {
		renderCtx.Logger.Error("Failed to check cache metadata", zap.Error(err))
		return nil, false
	}

	// No cache found
	if metadata == nil {
		return nil, false
	}

	// Fresh cache - return with exists=true
	if !metadata.IsExpired() {
		renderCtx.Logger.Debug("Cache entry found and valid",
			zap.String("file_path", metadata.FilePath),
			zap.Duration("ttl", metadata.TTL()))
		return metadata, true
	}

	// Expired cache - return metadata with exists=true so orchestrator can check if it's stale
	renderCtx.Logger.Debug("Cache entry found but expired",
		zap.String("file_path", metadata.FilePath),
		zap.Time("expired_at", metadata.ExpiresAt))

	return metadata, true
}

// GetCacheFile prepares cache file information for serving
// For compressed files: reads and decompresses content, returns memory-based response
// For uncompressed files: returns file path for SendFile-based serving
func (cs *CacheService) GetCacheFile(cacheEntry *CacheMetadata, logger *zap.Logger) (*CacheResponse, error) {
	// Convert relative path (from Redis) to absolute path (for filesystem)
	absolutePath := cs.metadata.GetAbsoluteFilePath(cacheEntry.FilePath)

	logger.Debug("Preparing cache file for serving",
		zap.String("relative_path", cacheEntry.FilePath),
		zap.String("absolute_path", absolutePath))

	// Calculate cache age
	cacheAge := time.Now().UTC().Sub(cacheEntry.CreatedAt)

	// Check if file is compressed - need to decompress before serving
	if IsCompressed(absolutePath) {
		content, err := cs.filesystem.ReadCompressed(absolutePath)
		if err != nil {
			algorithm := DetectAlgorithmFromPath(absolutePath)
			logger.Warn("Cache decompression failed, treating as miss",
				zap.String("file_path", absolutePath),
				zap.String("algorithm", algorithm),
				zap.Error(err))

			// Self-healing: delete metadata to trigger re-render
			cacheKey, parseErr := types.ParseCacheKey(cacheEntry.Key)
			if parseErr == nil {
				deleteCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if deleteErr := cs.metadata.DeleteMetadata(deleteCtx, cacheKey); deleteErr != nil {
					logger.Warn("Failed to delete corrupted cache metadata",
						zap.String("cache_key", cacheEntry.Key),
						zap.Error(deleteErr))
				}
			}

			return nil, fmt.Errorf("%w: %w", ErrDecompression, err)
		}

		logger.Debug("Cache file decompressed for serving",
			zap.String("absolute_path", absolutePath),
			zap.Duration("cache_age", cacheAge),
			zap.Int64("original_size", cacheEntry.Size),
			zap.Int("decompressed_size", len(content)))

		return &CacheResponse{
			Content:     content,
			ContentSize: cacheEntry.Size, // Original uncompressed size
			CacheAge:    cacheAge,
		}, nil
	}

	// Uncompressed file - use file path for SendFile-based serving
	response := &CacheResponse{
		FilePath:    absolutePath, // Use absolute path for file operations
		ContentSize: cacheEntry.Size,
		CacheAge:    cacheAge,
	}

	logger.Debug("Cache file prepared",
		zap.String("absolute_path", absolutePath),
		zap.Duration("cache_age", cacheAge),
		zap.Int64("size", cacheEntry.Size))

	return response, nil
}

// ReadCacheFile reads cache content from the filesystem (for sharding pull operations)
// Returns the HTML content as a string
func (cs *CacheService) ReadCacheFile(cacheKey *types.CacheKey) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get metadata to find file path
	metadata, err := cs.metadata.GetCacheEntry(ctx, cacheKey)
	if err != nil {
		return "", fmt.Errorf("failed to get cache metadata: %w", err)
	}
	if metadata == nil {
		return "", fmt.Errorf("cache metadata not found for key: %s", cacheKey.String())
	}

	// Convert relative path to absolute
	absolutePath := cs.metadata.GetAbsoluteFilePath(metadata.FilePath)

	// Read file from filesystem
	content, err := cs.filesystem.ReadHTML(absolutePath)
	if err != nil {
		return "", fmt.Errorf("failed to read cache file: %w", err)
	}

	return string(content), nil
}

// WriteCacheFile writes cache content to the filesystem (for sharding push operations)
// Writes the HTML content to disk at the appropriate location
func (cs *CacheService) WriteCacheFile(cacheKey *types.CacheKey, content string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get existing metadata to determine file path
	metadata, err := cs.metadata.GetCacheEntry(ctx, cacheKey)
	if err != nil {
		return fmt.Errorf("failed to get cache metadata: %w", err)
	}
	if metadata == nil {
		return fmt.Errorf("cache metadata not found for key: %s", cacheKey.String())
	}

	// Write content to filesystem
	if err := cs.filesystem.WriteHTML(metadata.FilePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// WriteCacheFileWithTimestamp writes cache content using provided ExpiresAt timestamp
// Used by sharding push operations where metadata doesn't exist yet on receiving EG
func (cs *CacheService) WriteCacheFileWithTimestamp(cacheKey *types.CacheKey, content string, expiresAt time.Time) error {
	// Compute file path from ExpiresAt (same as rendering EG)
	relativePath := cs.metadata.GenerateFilePath(cacheKey, expiresAt)

	// Convert to absolute path (required for proper filesystem write)
	absolutePath := cs.metadata.GetAbsoluteFilePath(relativePath)

	// Write content to filesystem
	if err := cs.filesystem.WriteHTML(absolutePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// GetCacheMetadata retrieves cache metadata without validating expiration
// Used for sharding to check which EGs have the cache
func (cs *CacheService) GetCacheMetadata(ctx context.Context, cacheKey *types.CacheKey) (*CacheMetadata, error) {
	metadata, err := cs.metadata.GetCacheEntry(ctx, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache metadata: %w", err)
	}
	return metadata, nil
}

// StoreRemoteCacheLocally stores pulled cache content locally
// Used after successfully pulling from a remote EG
// NOTE: Content bytes are already compressed (if compression enabled on origin EG).
// The FilePath in metadata includes the compression extension (.snappy, .lz4).
// We write bytes as-is without recompression or decompression.
func (cs *CacheService) StoreRemoteCacheLocally(ctx context.Context, cacheKey *types.CacheKey, content []byte, metadata *CacheMetadata) error {
	// Convert relative path to absolute (includes compression extension from metadata)
	absolutePath := cs.metadata.GetAbsoluteFilePath(metadata.FilePath)

	// Write content directly - already compressed if applicable
	if err := cs.filesystem.WriteHTML(absolutePath, content); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	cs.logger.Debug("Stored remote cache locally",
		zap.String("cache_key", cacheKey.String()),
		zap.String("relative_path", metadata.FilePath),
		zap.String("absolute_path", absolutePath),
		zap.Int("content_size", len(content)))

	return nil
}

// GetAbsoluteFilePath converts relative path to absolute (delegates to MetadataStore)
func (cs *CacheService) GetAbsoluteFilePath(relativePath string) string {
	return cs.metadata.GetAbsoluteFilePath(relativePath)
}

// GenerateFilePath generates file path based on cache key and timestamp (delegates to MetadataStore)
func (cs *CacheService) GenerateFilePath(key *types.CacheKey, expiresAt time.Time) string {
	return cs.metadata.GenerateFilePath(key, expiresAt)
}

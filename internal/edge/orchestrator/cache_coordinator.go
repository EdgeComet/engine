package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/internal/edge/sharding"
	"github.com/edgecomet/engine/pkg/types"
)

// isStatusCodeCacheable checks if the status code is in the cacheable list
func isStatusCodeCacheable(statusCode int, cacheableStatusCodes []int) bool {
	for _, code := range cacheableStatusCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

// getStaleTTL extracts stale TTL from resolved config, returns 0 if not configured or strategy is delete
func getStaleTTL(expired types.CacheExpiredConfig) time.Duration {
	if expired.Strategy != types.ExpirationStrategyServeStale {
		return 0
	}
	if expired.StaleTTL == nil {
		return 0
	}
	return time.Duration(*expired.StaleTTL)
}

// ShardingManager interface for optional sharding operations
type ShardingManager interface {
	IsEnabled() bool
	ComputeTargets(ctx context.Context, cacheKey string) ([]string, error)
	IsTargetForCache(ctx context.Context, cacheKey string) (bool, error)
	PushToTargets(ctx context.Context, cacheKey *types.CacheKey, content []byte, metadata *cache.CacheMetadata, targetEgIDs []string, requestID string) ([]string, error)
	PullFromRemote(ctx context.Context, cacheKey *types.CacheKey, egIDs []string) ([]byte, error)
	GetEgID() string
	GetReplicationFactor() int
	GetInterEgTimeout() time.Duration
	GetHealthyEGs(ctx context.Context) ([]sharding.EGInfo, error)
}

// CacheCoordinator handles all cache-related operations
// Coordinates between MetadataStore, FilesystemCache, and CacheService
type CacheCoordinator struct {
	metadata        *cache.MetadataStore
	fsCache         *cache.FilesystemCache
	cacheService    *cache.CacheService
	shardingManager ShardingManager
	metrics         *metrics.MetricsCollector
	logger          *zap.Logger
}

// NewCacheCoordinator creates a new CacheCoordinator instance
func NewCacheCoordinator(
	metadata *cache.MetadataStore,
	fsCache *cache.FilesystemCache,
	cacheService *cache.CacheService,
	shardingManager ShardingManager,
	metricsCollector *metrics.MetricsCollector,
	logger *zap.Logger,
) *CacheCoordinator {
	return &CacheCoordinator{
		metadata:        metadata,
		fsCache:         fsCache,
		cacheService:    cacheService,
		shardingManager: shardingManager,
		metrics:         metricsCollector,
		logger:          logger,
	}
}

// LookupCache retrieves cache metadata if available and valid
// Returns the cache metadata and a boolean indicating if it exists
func (cc *CacheCoordinator) LookupCache(renderCtx *edgectx.RenderContext) (*cache.CacheMetadata, bool) {
	return cc.cacheService.GetCacheEntry(renderCtx)
}

// IsFileLocal checks if the cache file is stored on the current EG
// Returns true if current EG owns the file according to metadata.EgIDs, false otherwise
func (cc *CacheCoordinator) IsFileLocal(metadata *cache.CacheMetadata) bool {
	return metadata.HasEgID(cc.shardingManager.GetEgID())
}

// SaveCache saves content to cache with source-specific optimizations
// Redirects (3xx): metadata-only cache (no disk write, no cluster push)
// Content (2xx, 4xx, 5xx): full cache (disk write + cluster push)
// Unified method for both render and bypass caching
func (cc *CacheCoordinator) SaveCache(
	renderCtx *edgectx.RenderContext,
	content []byte,
	statusCode int,
	headers map[string][]string,
	source string,
	ttl time.Duration,
	pushOnRender bool,
	indexStatus types.IndexStatus,
	title string,
) error {
	// STEP 1: Generate file path
	// IMPORTANT: Must use UTC for timezone consistency with cleanup worker
	// File path timestamp represents cache expiration time (now + TTL)
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	relativeFilePath := cc.metadata.GenerateFilePath(renderCtx.CacheKey, expiresAt)
	absoluteFilePath := cc.metadata.GetAbsoluteFilePath(relativeFilePath)

	isRedirect := isRedirectStatusCode(statusCode)

	// Track content to write and disk size (may differ from original due to compression)
	contentToWrite := content
	diskSize := int64(len(content))

	// STEP 2: Write content to filesystem ONLY for non-redirects
	if !isRedirect {
		// Compress content based on config
		compression := renderCtx.ResolvedConfig.Compression
		compressedContent, ext, err := cache.Compress(content, compression)
		if err != nil {
			renderCtx.Logger.Error("Compression failed",
				zap.String("algorithm", compression),
				zap.Error(err))
			return fmt.Errorf("compression failed: %w", err)
		}

		// Update file paths with compression extension
		if ext != "" {
			relativeFilePath = relativeFilePath + ext
			absoluteFilePath = absoluteFilePath + ext
		}

		contentToWrite = compressedContent
		diskSize = int64(len(compressedContent))

		// Record compression metrics only when compression was applied
		if ext != "" && cc.metrics != nil {
			cc.metrics.RecordCompression(compression, len(content), len(compressedContent))
		}

		if err := cc.fsCache.WriteHTML(absoluteFilePath, contentToWrite); err != nil {
			renderCtx.Logger.Error("Failed to write content to filesystem",
				zap.String("relative_path", relativeFilePath),
				zap.String("absolute_path", absoluteFilePath),
				zap.String("source", source),
				zap.Error(err))
			return fmt.Errorf("filesystem write failed: %w", err)
		}

		renderCtx.Logger.Info("Content written to disk successfully",
			zap.String("relative_path", relativeFilePath),
			zap.Int("original_size", len(content)),
			zap.Int64("disk_size", diskSize),
			zap.Int("status_code", statusCode),
			zap.String("source", source))
	} else {
		location := ""
		if locations, ok := getHeaderCaseInsensitive(headers, "Location"); ok && len(locations) > 0 {
			location = locations[0]
		}
		renderCtx.Logger.Debug("Skipping disk write for redirect (metadata-only cache)",
			zap.Int("status_code", statusCode),
			zap.String("location", location),
			zap.String("source", source))
	}

	// STEP 3: Create metadata
	metadata := &cache.CacheMetadata{
		Key:         renderCtx.CacheKey.String(),
		URL:         renderCtx.TargetURL,
		FilePath:    relativeFilePath,
		HostID:      renderCtx.Host.ID,
		Dimension:   renderCtx.Dimension,
		RequestID:   renderCtx.RequestID,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Size:        int64(len(content)), // Original uncompressed size (for Content-Length)
		DiskSize:    diskSize,            // Actual size on disk (may be compressed)
		LastAccess:  now,
		Source:      source,
		StatusCode:  statusCode,
		Headers:     headers,
		IndexStatus: int(indexStatus),
		Title:       title,
	}

	// Initialize eg_ids with current EG (only for non-redirects that have files on disk)
	// Redirects are metadata-only and have no file to share with other EGs
	if !isRedirect {
		metadata.SetEgIDs([]string{cc.shardingManager.GetEgID()})
	} else {
		metadata.SetEgIDs([]string{}) // Empty eg_ids for redirects
	}

	// Use independent timeout to prevent race condition from request cancellation
	metaCtx, metaCancel := context.WithTimeout(context.Background(), redisCacheOperationTimeout)
	defer metaCancel()

	// Calculate stale TTL for Redis expiration extension
	staleTTL := getStaleTTL(renderCtx.ResolvedConfig.Cache.Expired)

	if err := cc.metadata.StoreMetadata(metaCtx, metadata, renderCtx.CacheKey, staleTTL); err != nil {
		renderCtx.Logger.Error("Failed to create cache metadata",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.String("source", source),
			zap.Error(err))
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	renderCtx.Logger.Debug("Cache entry created successfully",
		zap.String("relative_path", relativeFilePath),
		zap.Int("content_size", len(content)),
		zap.Int("status_code", statusCode),
		zap.Bool("is_redirect", isRedirect),
		zap.String("source", source))

	// STEP 4: Push to cluster based on source and content
	shouldPushToCluster := cc.shardingManager != nil &&
		cc.shardingManager.IsEnabled() &&
		pushOnRender &&
		!isRedirect

	// Additional threshold for bypass cache: only push if content size is substantial
	if source == cache.SourceBypass && len(content) <= minBypassBodySizeForReplication {
		shouldPushToCluster = false
		renderCtx.Logger.Debug("Skipping cluster push for small bypass response",
			zap.Int("body_size", len(content)))
	}

	if shouldPushToCluster {
		if err := cc.pushCacheToCluster(renderCtx, contentToWrite, metadata); err != nil {
			renderCtx.Logger.Warn("Failed to push cache to cluster (cached locally)",
				zap.String("cache_key", renderCtx.CacheKey.String()),
				zap.String("source", source),
				zap.Error(err))
		}
	} else if isRedirect {
		renderCtx.Logger.Debug("Skipping cluster push for redirect (metadata-only cache)",
			zap.Int("status_code", statusCode),
			zap.String("source", source))
	}

	return nil
}

// SaveRenderCache saves rendered content to cache using unified SaveCache method
func (cc *CacheCoordinator) SaveRenderCache(
	renderCtx *edgectx.RenderContext,
	renderResult *RenderServiceResult,
) error {
	// Ensure Location is in headers for redirects (render service provides it separately)
	headersWithLocation := renderResult.Headers
	if renderResult.RedirectLocation != "" {
		if headersWithLocation == nil {
			headersWithLocation = make(map[string][]string)
		} else {
			// Copy to avoid modifying original
			headersWithLocation = make(map[string][]string, len(renderResult.Headers)+1)
			for k, v := range renderResult.Headers {
				headersWithLocation[k] = v
			}
		}
		headersWithLocation["Location"] = []string{renderResult.RedirectLocation}
	}

	// Filter headers using safe_response_headers configuration (forCache=true: block Set-Cookie)
	headers := FilterHeaders(headersWithLocation, renderCtx.ResolvedConfig.SafeResponseHeaders, renderResult.StatusCode, true)

	// Extract Title and IndexStatus from PageSEO for cache metadata
	var title string
	var indexStatus types.IndexStatus
	if renderResult.PageSEO != nil {
		title = renderResult.PageSEO.Title
		indexStatus = renderResult.PageSEO.IndexStatus
	}

	return cc.SaveCache(
		renderCtx,
		renderResult.HTML,
		renderResult.StatusCode,
		headers,
		cache.SourceRender,
		renderCtx.ResolvedConfig.Cache.TTL,
		renderCtx.ResolvedConfig.Sharding.PushOnRender,
		indexStatus,
		title,
	)
}

// pushCacheToCluster pushes cache to other EGs in the cluster
func (cc *CacheCoordinator) pushCacheToCluster(renderCtx *edgectx.RenderContext, html []byte, metadata *cache.CacheMetadata) error {
	// Use configured inter-EG timeout from sharding manager (or default if manager is nil)
	timeout := defaultInterEgTimeout
	if cc.shardingManager != nil {
		timeout = cc.shardingManager.GetInterEgTimeout()
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Compute target EGs for this cache
	cacheKey := renderCtx.CacheKey.String()
	targetEgIDs, err := cc.shardingManager.ComputeTargets(ctx, cacheKey)
	if err != nil {
		return fmt.Errorf("failed to compute targets: %w", err)
	}

	start := time.Now()
	// Push to target EGs and get successful EG IDs
	successfulEgIDs, pushErr := cc.shardingManager.PushToTargets(ctx, renderCtx.CacheKey, html, metadata, targetEgIDs, renderCtx.RequestID)

	// Update metadata with ONLY successful EG IDs (even if pushErr != nil, we have local copy)
	metadata.SetEgIDs(successfulEgIDs)

	// Update metadata in Redis with successful EG IDs
	metaCtx, metaCancel := context.WithTimeout(context.Background(), redisCacheOperationTimeout)
	defer metaCancel()

	staleTTL := getStaleTTL(renderCtx.ResolvedConfig.Cache.Expired)

	if err := cc.metadata.StoreMetadata(metaCtx, metadata, renderCtx.CacheKey, staleTTL); err != nil {
		renderCtx.Logger.Warn("Failed to update metadata with EG IDs",
			zap.String("cache_key", cacheKey),
			zap.Error(err))
		// Don't fail the request - cache is already stored locally
	}

	// Log appropriate message based on push results
	elapsed := time.Since(start).Seconds()
	if pushErr != nil {
		// All remote pushes failed (replication under-satisfied)
		renderCtx.Logger.Warn("Cache stored locally only - all remote pushes failed",
			zap.String("cache_key", cacheKey),
			zap.Float64("elapsed", elapsed),
			zap.Strings("computed_targets", targetEgIDs),
			zap.Strings("successful_egs", successfulEgIDs),
			zap.Error(pushErr))
		return pushErr
	} else if len(successfulEgIDs) < len(targetEgIDs) {
		// Partial success - some remotes failed
		renderCtx.Logger.Info("Cache pushed to cluster with partial success",
			zap.String("cache_key", cacheKey),
			zap.Float64("elapsed", elapsed),
			zap.Strings("computed_targets", targetEgIDs),
			zap.Strings("successful_egs", successfulEgIDs),
			zap.Int("success_count", len(successfulEgIDs)),
			zap.Int("target_count", len(targetEgIDs)))
	} else {
		// Full success - all targets succeeded
		renderCtx.Logger.Info("Cache pushed to cluster successfully",
			zap.String("cache_key", cacheKey),
			zap.Float64("elapsed", elapsed),
			zap.Strings("computed_targets", targetEgIDs),
			zap.Strings("successful_egs", successfulEgIDs))
	}

	return nil
}

// SaveBypassCache saves bypass response to cache using unified SaveCache method
func (cc *CacheCoordinator) SaveBypassCache(renderCtx *edgectx.RenderContext, bypassResp *bypass.BypassResponse, pageSEO *types.PageSEO) error {
	// Filter headers using safe_response_headers configuration (forCache=true: block Set-Cookie)
	headers := FilterHeaders(bypassResp.Headers, renderCtx.ResolvedConfig.SafeResponseHeaders, bypassResp.StatusCode, true)

	var title string
	var indexStatus types.IndexStatus
	if pageSEO != nil {
		title = pageSEO.Title
		indexStatus = pageSEO.IndexStatus
	}

	return cc.SaveCache(
		renderCtx,
		bypassResp.Body,
		bypassResp.StatusCode,
		headers,
		cache.SourceBypass,
		renderCtx.ResolvedConfig.Bypass.Cache.TTL,
		true, // bypass cache always attempts push (size threshold handled in SaveCache)
		indexStatus,
		title,
	)
}

// GetCacheFileForServing prepares cache file information for serving
func (cc *CacheCoordinator) GetCacheFileForServing(cacheEntry *cache.CacheMetadata, logger *zap.Logger) (*cache.CacheResponse, error) {
	resp, err := cc.cacheService.GetCacheFile(cacheEntry, logger)
	if err != nil {
		// Record decompression error metric if applicable
		if errors.Is(err, cache.ErrDecompression) {
			algorithm := cache.DetectAlgorithmFromPath(cacheEntry.FilePath)
			if algorithm == "" {
				algorithm = "unknown"
			}
			cc.metrics.RecordDecompressionError(algorithm)
		}
		return nil, err
	}
	return resp, nil
}

// pullCacheContent is a private helper that pulls cache content from remote EGs
// Common logic used by both TryPullFromRemote and PullFromRemoteToMemory
// Returns (content, remoteEgIDs, nil) on success, (nil, nil, error) on failure
func (cc *CacheCoordinator) pullCacheContent(
	ctx context.Context,
	renderCtx *edgectx.RenderContext,
	metadata *cache.CacheMetadata,
) ([]byte, []string, error) {
	// 1. Check if sharding is enabled
	if cc.shardingManager == nil || !cc.shardingManager.IsEnabled() {
		return nil, nil, fmt.Errorf("sharding not enabled")
	}

	// 2. Check if metadata has eg_ids (indicates cache exists on other EGs)
	if metadata.IsEmpty() {
		return nil, nil, fmt.Errorf("no eg_ids in metadata")
	}

	// 3. Get remote EG IDs (filter out self)
	selfEgID := cc.shardingManager.GetEgID()
	remoteEgIDs := metadata.GetRemoteEgIDs(selfEgID)

	if len(remoteEgIDs) == 0 {
		return nil, nil, fmt.Errorf("no remote EGs available")
	}

	// 4. Filter remoteEgIDs to only include healthy/alive EGs (optimization)
	// This prevents wasted timeout attempts on offline EGs
	healthyEGs, err := cc.shardingManager.GetHealthyEGs(ctx)
	if err != nil {
		// Log warning but continue with original list (fallback behavior)
		renderCtx.Logger.Warn("Failed to get healthy EGs, will try all EGs in metadata",
			zap.Error(err))
	} else {
		// Build map of healthy EG IDs for fast lookup
		healthyEGMap := make(map[string]bool)
		for _, eg := range healthyEGs {
			healthyEGMap[eg.EgID] = true
		}

		// Filter remoteEgIDs to only healthy ones
		filteredRemoteEgIDs := []string{}
		skippedOfflineEGs := []string{}
		for _, egID := range remoteEgIDs {
			if healthyEGMap[egID] {
				filteredRemoteEgIDs = append(filteredRemoteEgIDs, egID)
			} else {
				skippedOfflineEGs = append(skippedOfflineEGs, egID)
			}
		}

		// Use filtered list only if we have at least one healthy EG
		if len(filteredRemoteEgIDs) > 0 {
			if len(skippedOfflineEGs) > 0 {
				renderCtx.Logger.Info("Filtered out offline EGs before pull",
					zap.Strings("offline_egs", skippedOfflineEGs),
					zap.Strings("healthy_egs", filteredRemoteEgIDs))
			}
			remoteEgIDs = filteredRemoteEgIDs
		} else {
			// All EGs appear offline - fall back to trying original list
			renderCtx.Logger.Warn("All remote EGs appear offline", zap.Strings("remote_egs", remoteEgIDs))

			return nil, nil, fmt.Errorf("no remote EGs online available")
		}
	}

	// 5. Pull content from remote (using filtered list of healthy EGs)
	cacheKey := renderCtx.CacheKey.String()
	content, err := cc.shardingManager.PullFromRemote(ctx, renderCtx.CacheKey, remoteEgIDs)
	if err != nil {
		renderCtx.Logger.Warn("Failed to pull cache from remote EGs",
			zap.String("cache_key", cacheKey),
			zap.Strings("remote_egs", remoteEgIDs),
			zap.Error(err))
		return nil, nil, fmt.Errorf("pull from remote failed: %w", err)
	}

	return content, remoteEgIDs, nil
}

// TryPullFromRemote attempts to pull cache from remote EGs and stores locally
// Accepts metadata as parameter (caller should already have it from LookupCache)
// Returns (metadata, true) if successful, (nil, false) otherwise
func (cc *CacheCoordinator) TryPullFromRemote(renderCtx *edgectx.RenderContext, metadata *cache.CacheMetadata) (*cache.CacheMetadata, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Pull content from remote using helper
	content, remoteEgIDs, err := cc.pullCacheContent(ctx, renderCtx, metadata)
	if err != nil {
		return nil, false
	}

	// Store pulled cache locally
	cacheKey := renderCtx.CacheKey.String()
	err = cc.cacheService.StoreRemoteCacheLocally(ctx, renderCtx.CacheKey, content, metadata)
	if err != nil {
		renderCtx.Logger.Error("Failed to store remote cache locally",
			zap.String("cache_key", cacheKey),
			zap.Error(err))
		return nil, false
	}

	// Update metadata to add self to eg_ids
	selfEgID := cc.shardingManager.GetEgID()

	// Add self if not already present (idempotent)
	if !metadata.HasEgID(selfEgID) {
		previousCount := metadata.Count()

		// Check if replication factor would be exceeded
		replicationFactor := renderCtx.ResolvedConfig.Sharding.ReplicationFactor
		if previousCount >= replicationFactor {
			renderCtx.Logger.Info("Skipping metadata update - replication factor already satisfied",
				zap.String("cache_key", cacheKey),
				zap.String("self_eg_id", selfEgID),
				zap.Int("current_replicas", previousCount),
				zap.Int("target_replicas", replicationFactor),
				zap.Strings("existing_eg_ids", metadata.EgIDs))
			// Cache is stored locally for serving, but not added to cluster eg_ids
			return metadata, true
		}

		metadata.AddEgID(selfEgID)

		// Update metadata in Redis
		metaCtx, metaCancel := context.WithTimeout(context.Background(), redisCacheOperationTimeout)
		defer metaCancel()

		staleTTL := getStaleTTL(renderCtx.ResolvedConfig.Cache.Expired)

		if err := cc.metadata.StoreMetadata(metaCtx, metadata, renderCtx.CacheKey, staleTTL); err != nil {
			renderCtx.Logger.Warn("Failed to update metadata with self after pull",
				zap.String("cache_key", cacheKey),
				zap.String("eg_id", selfEgID),
				zap.Error(err))
			// Don't fail - cache is stored locally, just not visible yet
		} else {
			renderCtx.Logger.Info("Updated metadata to include self after pull",
				zap.String("cache_key", cacheKey),
				zap.Int("previous_count", previousCount),
				zap.Int("new_count", metadata.Count()))
		}
	}

	renderCtx.Logger.Info("Successfully pulled and stored cache from remote EG",
		zap.String("cache_key", cacheKey),
		zap.Strings("source_egs", remoteEgIDs),
		zap.Int("content_size", len(content)))

	return metadata, true
}

// PullFromRemoteToMemory pulls cache from remote EGs without storing locally
// Accepts metadata as parameter (caller should already have it from LookupCache)
// Returns (content, true) if successful, (nil, false) otherwise
// Use this when you want to serve pulled content without disk I/O (proxy-only mode)
func (cc *CacheCoordinator) PullFromRemoteToMemory(renderCtx *edgectx.RenderContext, metadata *cache.CacheMetadata) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Pull content from remote using helper
	content, remoteEgIDs, err := cc.pullCacheContent(ctx, renderCtx, metadata)
	if err != nil {
		return nil, false
	}

	cacheKey := renderCtx.CacheKey.String()
	renderCtx.Logger.Info("Successfully pulled cache to memory (not stored locally)",
		zap.String("cache_key", cacheKey),
		zap.Strings("remote_egs", remoteEgIDs),
		zap.Int("content_size", len(content)))

	return content, true
}

// GetCacheResponseFromMemory prepares cache response for serving from memory
// Decompresses content if needed and creates a memory-based CacheResponse
func (cc *CacheCoordinator) GetCacheResponseFromMemory(metadata *cache.CacheMetadata, content []byte) *cache.CacheResponse {
	cacheAge := time.Since(metadata.CreatedAt)

	// Decompress if file path indicates compression
	decompressed, err := cache.Decompress(content, metadata.FilePath)
	if err != nil {
		// Log error but return compressed content as fallback
		// This shouldn't happen in practice
		cc.logger.Warn("Failed to decompress pulled content",
			zap.String("file_path", metadata.FilePath),
			zap.Error(err))
		decompressed = content
	}

	return &cache.CacheResponse{
		FilePath:    "",            // Empty for memory-based serving
		Content:     decompressed,  // Decompressed content for serving
		ContentSize: metadata.Size, // Original uncompressed size from metadata
		CacheAge:    cacheAge,      // Age since creation
	}
}

// IsStatusCodeCacheable checks if the given status code is in the cacheable list
func (cc *CacheCoordinator) IsStatusCodeCacheable(statusCode int, cacheableCodes []int) bool {
	for _, code := range cacheableCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

// DeleteStaleMetadata deletes Redis metadata for stale cache
// File cleanup happens later via cache daemon
func (cc *CacheCoordinator) DeleteStaleMetadata(renderCtx *edgectx.RenderContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := cc.metadata.DeleteMetadata(ctx, renderCtx.CacheKey); err != nil {
		renderCtx.Logger.Error("Failed to delete stale metadata",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Error(err))
		return err
	}

	renderCtx.Logger.Info("Deleted stale metadata after non-cacheable render",
		zap.String("cache_key", renderCtx.CacheKey.String()))

	return nil
}

package sharding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/edge/internal_server"
	"github.com/edgecomet/engine/pkg/types"
)

// RegisterEndpoints registers all sharding-related handlers with the internal server
func (m *Manager) RegisterEndpoints(server *internal_server.InternalServer) {
	server.RegisterHandler("GET", internal_server.PathCachePull, m.handlePull)
	server.RegisterHandler("POST", internal_server.PathCachePush, m.handlePush)
	server.RegisterHandler("GET", internal_server.PathCacheStatus, m.handleStatus)
}

// handlePull handles cache pull requests from other EGs using streaming
func (m *Manager) handlePull(ctx *fasthttp.RequestCtx) {
	cacheKeyStr := string(ctx.QueryArgs().Peek("cache_key"))
	if cacheKeyStr == "" {
		m.logger.Warn("Missing cache_key query parameter")
		httputil.JSONError(ctx, "missing cache_key parameter", fasthttp.StatusBadRequest)
		return
	}

	cacheKey, err := types.ParseCacheKey(cacheKeyStr)
	if err != nil {
		m.logger.Warn("Invalid cache key",
			zap.String("cache_key", cacheKeyStr),
			zap.Error(err))
		httputil.JSONError(ctx, "invalid cache_key", fasthttp.StatusBadRequest)
		return
	}

	metadata, err := m.cacheService.GetCacheMetadata(ctx, cacheKey)
	if err != nil {
		m.logger.Error("Failed to get cache metadata",
			zap.String("cache_key", cacheKey.String()),
			zap.Error(err))
		httputil.JSONError(ctx, "internal server error", fasthttp.StatusInternalServerError)
		return
	}

	if metadata == nil || metadata.FilePath == "" {
		m.logger.Debug("Cache metadata not found",
			zap.String("cache_key", cacheKey.String()))
		httputil.JSONError(ctx, "cache not found", fasthttp.StatusNotFound)
		return
	}

	filePath, err := m.cacheService.GetAbsoluteFilePath(metadata.FilePath)
	if err != nil {
		m.logger.Warn("Invalid cache file path",
			zap.String("cache_key", cacheKey.String()),
			zap.Error(err))
		httputil.JSONError(ctx, "invalid file path", fasthttp.StatusBadRequest)
		return
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Debug("Cache file not found at path",
				zap.String("cache_key", cacheKey.String()),
				zap.String("path", filePath))
			httputil.JSONError(ctx, "cache not found", fasthttp.StatusNotFound)
		} else {
			m.logger.Error("Failed to stat cache file",
				zap.String("cache_key", cacheKey.String()),
				zap.String("path", filePath),
				zap.Error(err))
			httputil.JSONError(ctx, "internal server error", fasthttp.StatusInternalServerError)
		}
		return
	}

	shardMetadata := ShardMetadata{
		CacheKey:  *cacheKey,
		CreatedAt: metadata.CreatedAt,
		ExpiresAt: metadata.ExpiresAt,
		RequestID: metadata.RequestID,
		EgID:      m.egID,
		FilePath:  metadata.FilePath, // Includes compression extension
	}

	metadataJSON, err := json.Marshal(shardMetadata)
	if err != nil {
		m.logger.Error("Failed to marshal metadata",
			zap.String("cache_key", cacheKey.String()),
			zap.Error(err))
		httputil.JSONError(ctx, "internal server error", fasthttp.StatusInternalServerError)
		return
	}

	ctx.Response.Header.Set("X-Shard-Metadata", string(metadataJSON))
	ctx.Response.Header.SetContentType("text/html; charset=utf-8")
	ctx.SendFile(filePath)

	m.logger.Info("PULL served",
		zap.String("cache_key", cacheKey.String()),
		zap.String("to", ctx.RemoteIP().String()),
		zap.Int64("bytes", fileInfo.Size()))
}

// handlePush handles cache push requests from other EGs using streaming
func (m *Manager) handlePush(ctx *fasthttp.RequestCtx) {
	metadataJSON := ctx.Request.Header.Peek("X-Shard-Metadata")
	if len(metadataJSON) == 0 {
		m.logger.Warn("Missing X-Shard-Metadata header")
		httputil.JSONError(ctx, "missing metadata header", fasthttp.StatusBadRequest)
		return
	}

	var metadata ShardMetadata
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		m.logger.Warn("Failed to parse shard metadata",
			zap.Error(err))
		httputil.JSONError(ctx, "invalid metadata header", fasthttp.StatusBadRequest)
		return
	}

	cacheKey := &metadata.CacheKey

	// Use FilePath from metadata if provided (includes compression extension)
	// Fall back to generating path for backward compatibility with older EGs
	filePath := metadata.FilePath
	if filePath == "" {
		filePath = m.cacheService.GenerateFilePath(cacheKey, metadata.ExpiresAt)
	}
	absolutePath, err := m.cacheService.GetAbsoluteFilePath(filePath)
	if err != nil {
		m.logger.Warn("Invalid cache file path in push metadata",
			zap.String("file_path", filePath),
			zap.Error(err))
		httputil.JSONError(ctx, "invalid file path", fasthttp.StatusBadRequest)
		return
	}

	parentDir := filepath.Dir(absolutePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		m.logger.Error("Failed to create cache directory",
			zap.String("dir", parentDir),
			zap.Error(err))
		httputil.JSONError(ctx, "failed to create directory", fasthttp.StatusInternalServerError)
		return
	}

	tempPath := absolutePath + ".tmp"
	htmlContent := ctx.Request.Body()

	if err := os.WriteFile(tempPath, htmlContent, 0644); err != nil {
		m.logger.Error("Failed to write temp file",
			zap.String("path", tempPath),
			zap.Error(err))
		httputil.JSONError(ctx, "failed to write temp file", fasthttp.StatusInternalServerError)
		return
	}

	bytesWritten := int64(len(htmlContent))

	if err := os.Rename(tempPath, absolutePath); err != nil {
		os.Remove(tempPath)
		m.logger.Error("Failed to rename temp file",
			zap.String("temp", tempPath),
			zap.String("final", absolutePath),
			zap.Error(err))
		httputil.JSONError(ctx, "failed to finalize file", fasthttp.StatusInternalServerError)
		return
	}

	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.SetContentLength(0)

	m.logger.Info("PUSH received",
		zap.String("cache_key", cacheKey.String()),
		zap.String("request_id", metadata.RequestID),
		zap.String("from", ctx.RemoteIP().String()),
		zap.Int64("bytes", bytesWritten))
}

// handleStatus handles status information requests
func (m *Manager) handleStatus(ctx *fasthttp.RequestCtx) {
	healthyEGs, err := m.registry.GetHealthyEGs(ctx)
	if err != nil {
		m.logger.Warn("Failed to get healthy EGs for status",
			zap.Error(err))
		healthyEGs = []types.EGInfo{}
	}

	availableEGs := make([]string, len(healthyEGs))
	for i, eg := range healthyEGs {
		availableEGs[i] = eg.EgID
	}

	replicationFactor := 2
	if m.config != nil && m.config.ReplicationFactor != nil {
		replicationFactor = *m.config.ReplicationFactor
	}

	strategy := "hash_modulo"
	if m.config != nil && m.config.DistributionStrategy != "" {
		strategy = m.config.DistributionStrategy
	}

	resp := StatusResponse{
		EgID:                 m.egID,
		ShardingEnabled:      m.IsEnabled(),
		ReplicationFactor:    replicationFactor,
		DistributionStrategy: strategy,
		LocalCacheCount:      0,
		LocalCacheSizeBytes:  0,
		AvailableEGs:         availableEGs,
		ClusterSize:          len(healthyEGs),
		UptimeSeconds:        int64(time.Since(m.startTime).Seconds()),
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		m.logger.Error("Failed to marshal status response",
			zap.Error(err))
		httputil.JSONError(ctx, "internal server error", fasthttp.StatusInternalServerError)
		return
	}

	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.SetContentType("application/json")
	ctx.Response.SetBody(respBody)
}

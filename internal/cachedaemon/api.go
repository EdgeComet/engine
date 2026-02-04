package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// ServeHTTP is the main HTTP request handler for the cache daemon API
func (d *CacheDaemon) ServeHTTP(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	method := string(ctx.Method())

	// Auth middleware - validate X-Internal-Auth header for all endpoints
	authKey := string(ctx.Request.Header.Peek("X-Internal-Auth"))
	if authKey != d.internalAuthKey {
		d.logger.Warn("Unauthorized API request",
			zap.String("path", path),
			zap.String("remote_addr", ctx.RemoteAddr().String()))
		httputil.JSONError(ctx, "unauthorized", fasthttp.StatusUnauthorized)
		return
	}

	// Route handling
	switch {
	case method == "POST" && path == "/internal/cache/recache":
		d.handleRecacheAPI(ctx)
	case method == "POST" && path == "/internal/cache/invalidate":
		d.handleInvalidateAPI(ctx)
	case method == "GET" && path == "/status":
		d.handleStatusAPI(ctx)
	case method == "POST" && path == "/internal/scheduler/pause":
		d.handleSchedulerPauseAPI(ctx)
	case method == "POST" && path == "/internal/scheduler/resume":
		d.handleSchedulerResumeAPI(ctx)
	default:
		httputil.JSONError(ctx, "not found", fasthttp.StatusNotFound)
	}
}

// handleRecacheAPI handles POST /internal/cache/recache
func (d *CacheDaemon) handleRecacheAPI(ctx *fasthttp.RequestCtx) {
	var req types.RecacheAPIRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		httputil.JSONError(ctx, fmt.Sprintf("invalid json: %s", err.Error()), fasthttp.StatusBadRequest)
		return
	}

	// Validate request
	if req.HostID == 0 {
		httputil.JSONError(ctx, "host_id is required", fasthttp.StatusBadRequest)
		return
	}

	if len(req.URLs) == 0 {
		httputil.JSONError(ctx, "urls array cannot be empty", fasthttp.StatusBadRequest)
		return
	}

	if len(req.URLs) > 10000 {
		httputil.JSONError(ctx, "urls array cannot exceed 10000 entries", fasthttp.StatusBadRequest)
		return
	}

	if req.Priority != "high" && req.Priority != "normal" {
		httputil.JSONError(ctx, "priority must be 'high' or 'normal'", fasthttp.StatusBadRequest)
		return
	}

	// Get host config
	host := d.GetHost(req.HostID)
	if host == nil {
		httputil.JSONError(ctx, fmt.Sprintf("host_id %d not found", req.HostID), fasthttp.StatusBadRequest)
		return
	}

	// Get all dimension IDs from host config
	allDimensionIDs := make([]int, 0, len(host.Render.Dimensions))
	for _, dim := range host.Render.Dimensions {
		allDimensionIDs = append(allDimensionIDs, dim.ID)
	}

	// Resolve dimension IDs (use all if empty)
	dimensionIDs := req.DimensionIDs
	if len(dimensionIDs) == 0 {
		dimensionIDs = allDimensionIDs
	} else {
		// Validate dimension IDs
		for _, dimID := range dimensionIDs {
			found := false
			for _, validDimID := range allDimensionIDs {
				if dimID == validDimID {
					found = true
					break
				}
			}
			if !found {
				httputil.JSONError(ctx, fmt.Sprintf("dimension_id %d not configured for host '%s'", dimID, host.Domain), fasthttp.StatusBadRequest)
				return
			}
		}
	}

	// Enqueue entries to ZSET
	queueKey := d.keyGenerator.RecacheQueueKey(req.HostID, req.Priority)
	score := float64(time.Now().UTC().Unix())
	entriesEnqueued := 0
	reqCtx := context.Background()

	for _, url := range req.URLs {
		// Normalize URL before ZADD
		normalizedResult, err := d.normalizer.Normalize(url, nil)
		if err != nil {
			d.logger.Error("Invalid URL, skipping",
				zap.String("url", url),
				zap.Error(err))
			continue
		}

		for _, dimensionID := range dimensionIDs {
			member := types.RecacheMember{
				URL:         normalizedResult.NormalizedURL,
				DimensionID: dimensionID,
			}
			memberJSON, _ := json.Marshal(member)

			if err := d.redis.ZAdd(reqCtx, queueKey, score, string(memberJSON)); err != nil {
				d.logger.Error("Failed to add entry to ZSET",
					zap.String("queue", queueKey),
					zap.String("url", normalizedResult.NormalizedURL),
					zap.Int("dimension_id", dimensionID),
					zap.Error(err))
				continue
			}
			entriesEnqueued++
		}
	}

	// Return response
	data := types.RecacheAPIData{
		HostID:            req.HostID,
		URLsCount:         len(req.URLs),
		DimensionIDsCount: len(dimensionIDs),
		EntriesEnqueued:   entriesEnqueued,
		Priority:          req.Priority,
	}
	httputil.JSONData(ctx, data, fasthttp.StatusOK)

	d.logger.Info("Recache request processed",
		zap.Int("host_id", req.HostID),
		zap.Int("urls_count", len(req.URLs)),
		zap.Int("dimensions_count", len(dimensionIDs)),
		zap.Int("entries_enqueued", entriesEnqueued),
		zap.String("priority", req.Priority))
}

// handleInvalidateAPI handles POST /internal/cache/invalidate
func (d *CacheDaemon) handleInvalidateAPI(ctx *fasthttp.RequestCtx) {
	var req types.InvalidateAPIRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		httputil.JSONError(ctx, fmt.Sprintf("invalid json: %s", err.Error()), fasthttp.StatusBadRequest)
		return
	}

	// Validate request
	if req.HostID == 0 {
		httputil.JSONError(ctx, "host_id is required", fasthttp.StatusBadRequest)
		return
	}

	if len(req.URLs) == 0 {
		httputil.JSONError(ctx, "urls array cannot be empty", fasthttp.StatusBadRequest)
		return
	}

	// Get host config
	host := d.GetHost(req.HostID)
	if host == nil {
		httputil.JSONError(ctx, fmt.Sprintf("host_id %d not found", req.HostID), fasthttp.StatusBadRequest)
		return
	}

	// Get all dimension IDs from host config
	allDimensionIDs := make([]int, 0, len(host.Render.Dimensions))
	for _, dim := range host.Render.Dimensions {
		allDimensionIDs = append(allDimensionIDs, dim.ID)
	}

	// Resolve dimension IDs (use all if empty)
	dimensionIDs := req.DimensionIDs
	if len(dimensionIDs) == 0 {
		dimensionIDs = allDimensionIDs
	} else {
		// Validate dimension IDs
		for _, dimID := range dimensionIDs {
			found := false
			for _, validDimID := range allDimensionIDs {
				if dimID == validDimID {
					found = true
					break
				}
			}
			if !found {
				httputil.JSONError(ctx, fmt.Sprintf("dimension_id %d not configured for host '%s'", dimID, host.Domain), fasthttp.StatusBadRequest)
				return
			}
		}
	}

	// Invalidate cache entries
	entriesInvalidated := 0
	reqCtx := context.Background()

	for _, url := range req.URLs {
		// Normalize URL
		normalizedResult, err := d.normalizer.Normalize(url, nil)
		if err != nil {
			d.logger.Error("Invalid URL, skipping",
				zap.String("url", url),
				zap.Error(err))
			continue
		}

		for _, dimensionID := range dimensionIDs {
			// Generate cache metadata key
			urlHash := d.normalizer.Hash(normalizedResult.NormalizedURL)
			cacheKey := d.keyGenerator.GenerateCacheKey(req.HostID, dimensionID, urlHash)
			metadataKey := d.keyGenerator.GenerateMetadataKey(cacheKey)

			// Delete metadata from Redis
			if err := d.redis.Del(reqCtx, metadataKey); err != nil {
				d.logger.Error("Failed to delete cache metadata",
					zap.String("metadata_key", metadataKey),
					zap.Error(err))
				continue
			}
			entriesInvalidated++
		}
	}

	// Return response
	data := types.InvalidateAPIData{
		HostID:             req.HostID,
		URLsCount:          len(req.URLs),
		DimensionIDsCount:  len(dimensionIDs),
		EntriesInvalidated: entriesInvalidated,
	}
	httputil.JSONData(ctx, data, fasthttp.StatusOK)

	d.logger.Info("Invalidate request processed",
		zap.Int("host_id", req.HostID),
		zap.Int("urls_count", len(req.URLs)),
		zap.Int("dimensions_count", len(dimensionIDs)),
		zap.Int("entries_invalidated", entriesInvalidated))
}

// handleStatusAPI handles GET /status
func (d *CacheDaemon) handleStatusAPI(ctx *fasthttp.RequestCtx) {
	d.lastTickMu.RLock()
	lastTick := d.lastTickTime
	d.lastTickMu.RUnlock()

	status := StatusResponse{
		Daemon: DaemonStatus{
			DaemonID:      d.daemonConfig.DaemonID,
			UptimeSeconds: int(time.Since(d.startTime).Seconds()),
			LastTick:      lastTick.UTC().Format(time.RFC3339),
		},
		InternalQueue: InternalQueueStatus{
			Size:                d.internalQueue.Size(),
			MaxSize:             d.daemonConfig.InternalQueue.MaxSize,
			CapacityUsedPercent: float64(d.internalQueue.Size()) / float64(d.daemonConfig.InternalQueue.MaxSize) * 100,
		},
		RSCapacity: d.GetRSCapacityStatus(),
		Queues:     d.GetQueuesStatus(),
	}

	respJSON, _ := json.Marshal(status)

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType("application/json")
	ctx.SetBody(respJSON)
}

// GetQueuesStatus returns the status of all recache queues for all configured hosts
func (d *CacheDaemon) GetQueuesStatus() map[int]HostQueuesStatus {
	hosts := d.GetConfiguredHosts()
	queuesStatus := make(map[int]HostQueuesStatus)
	now := time.Now().UTC().Unix()
	reqCtx := context.Background()

	for _, hostID := range hosts {
		highKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityHigh)
		normalKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityNormal)
		autoKey := d.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)

		// Use ZCARD for total count, ZCOUNT for due entries
		highTotal, _ := d.redis.ZCard(reqCtx, highKey)
		normalTotal, _ := d.redis.ZCard(reqCtx, normalKey)
		autoTotal, _ := d.redis.ZCard(reqCtx, autoKey)

		highDue, _ := d.redis.ZCount(reqCtx, highKey, "-inf", fmt.Sprintf("%d", now))
		normalDue, _ := d.redis.ZCount(reqCtx, normalKey, "-inf", fmt.Sprintf("%d", now))
		autoDue, _ := d.redis.ZCount(reqCtx, autoKey, "-inf", fmt.Sprintf("%d", now))

		queuesStatus[hostID] = HostQueuesStatus{
			High:        QueueStatus{Total: int(highTotal), DueNow: int(highDue)},
			Normal:      QueueStatus{Total: int(normalTotal), DueNow: int(normalDue)},
			Autorecache: QueueStatus{Total: int(autoTotal), DueNow: int(autoDue)},
		}
	}

	return queuesStatus
}

// handleSchedulerPauseAPI handles POST /internal/scheduler/pause (when scheduler_control_api enabled)
func (d *CacheDaemon) handleSchedulerPauseAPI(ctx *fasthttp.RequestCtx) {
	if !d.daemonConfig.HTTPApi.SchedulerControlAPI {
		httputil.JSONError(ctx, "Scheduler control API not enabled", fasthttp.StatusForbidden)
		return
	}

	d.PauseScheduler()

	httputil.JSONSuccess(ctx, "Scheduler paused", fasthttp.StatusOK)
}

// handleSchedulerResumeAPI handles POST /internal/scheduler/resume (when scheduler_control_api enabled)
func (d *CacheDaemon) handleSchedulerResumeAPI(ctx *fasthttp.RequestCtx) {
	if !d.daemonConfig.HTTPApi.SchedulerControlAPI {
		httputil.JSONError(ctx, "Scheduler control API not enabled", fasthttp.StatusForbidden)
		return
	}

	d.ResumeScheduler()

	httputil.JSONSuccess(ctx, "Scheduler resumed", fasthttp.StatusOK)
}

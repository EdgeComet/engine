package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// bypassDimensionID is used for unmatched User-Agent bypass cache entries (dimension IDs 1+ are render dimensions)
const bypassDimensionID = 0

// ServeHTTP is the main HTTP request handler for the cache daemon API
func (d *CacheDaemon) ServeHTTP(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	method := string(ctx.Method())

	// Auth middleware - validate X-Internal-Auth header for all endpoints
	authKey := string(ctx.Request.Header.Peek("X-Internal-Auth"))
	if authKey == "" {
		d.logger.Warn("Missing X-Internal-Auth header",
			zap.String("path", path),
			zap.String("remote_addr", ctx.RemoteAddr().String()))
		httputil.JSONError(ctx, "unauthorized", fasthttp.StatusUnauthorized)
		return
	}
	if authKey != d.internalAuthKey {
		d.logger.Warn("Invalid X-Internal-Auth header",
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
	case method == "POST" && path == "/internal/cache/invalidate-all":
		d.handleInvalidateAllAPI(ctx)
	case method == "GET" && path == "/status":
		d.handleStatusAPI(ctx)
	case method == "POST" && path == "/internal/scheduler/pause":
		d.handleSchedulerPauseAPI(ctx)
	case method == "POST" && path == "/internal/scheduler/resume":
		d.handleSchedulerResumeAPI(ctx)
	case method == "GET" && path == "/internal/cache/urls":
		d.handleCacheURLsAPI(ctx)
	case method == "GET" && path == "/internal/cache/summary":
		d.handleCacheSummaryAPI(ctx)
	case method == "GET" && path == "/internal/cache/queue":
		d.handleCacheQueueAPI(ctx)
	case method == "GET" && path == "/internal/cache/queue/summary":
		d.handleCacheQueueSummaryAPI(ctx)
	default:
		httputil.JSONError(ctx, "not found", fasthttp.StatusNotFound)
	}
}

// resolveDimensionIDs builds the full list of dimension IDs for a host and validates any explicitly
// requested IDs against it. When includeBypass is true, bypass dimension 0 is included (for invalidation).
// When false, only render dimensions are included (for recache, since bypass entries can't be re-rendered).
// Returns all dimensions if requestedIDs is empty.
func resolveDimensionIDs(host *types.Host, requestedIDs []int, includeBypass bool) ([]int, error) {
	capacity := len(host.Render.Dimensions)
	if includeBypass {
		capacity++
	}
	allDimensionIDs := make([]int, 0, capacity)
	if includeBypass {
		allDimensionIDs = append(allDimensionIDs, bypassDimensionID)
	}
	for _, dim := range host.Render.Dimensions {
		allDimensionIDs = append(allDimensionIDs, dim.ID)
	}

	if len(requestedIDs) == 0 {
		return allDimensionIDs, nil
	}

	for _, dimID := range requestedIDs {
		found := false
		for _, validDimID := range allDimensionIDs {
			if dimID == validDimID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("dimension_id %d not configured for host '%s'", dimID, host.Domain)
		}
	}
	return requestedIDs, nil
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

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs, false)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
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

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs, true)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
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

		urlHash := d.normalizer.Hash(normalizedResult.NormalizedURL)
		urlDeleted := 0

		for _, dimensionID := range dimensionIDs {
			cacheKey := d.keyGenerator.GenerateCacheKey(req.HostID, dimensionID, urlHash)
			metadataKey := d.keyGenerator.GenerateMetadataKey(cacheKey)

			deleted, err := d.redis.DelCount(reqCtx, metadataKey)
			if err != nil {
				d.logger.Error("Failed to delete cache metadata",
					zap.String("metadata_key", metadataKey),
					zap.Error(err))
				continue
			}
			urlDeleted += int(deleted)
		}

		if urlDeleted == 0 {
			d.logger.Warn("No cache metadata found for URL during invalidation",
				zap.String("url", normalizedResult.NormalizedURL),
				zap.String("url_hash", urlHash),
				zap.Int("host_id", req.HostID))
		}
		entriesInvalidated += urlDeleted
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

// luaInvalidateAllBatch scans and deletes cache metadata keys in a single batch.
// Returns {nextCursor, deletedCount}. Caller loops until nextCursor is "0".
// ARGV: [1] hostID, [2] cursor, [3...] dimension IDs to filter (empty = all)
const luaInvalidateAllBatch = `
local prefix = "meta:cache:" .. ARGV[1] .. ":"
local cursor = ARGV[2]
local max_iterations = 200
local del_chunk_size = 1000
local deleted = 0

local dim_filter = {}
local has_filter = false
for i = 3, #ARGV do
    dim_filter[ARGV[i]] = true
    has_filter = true
end

local iterations = 0
local to_delete = {}

repeat
    local res = redis.call("SCAN", cursor, "MATCH", prefix .. "*", "COUNT", 500)
    cursor = res[1]
    iterations = iterations + 1

    for _, key in ipairs(res[2]) do
        if has_filter then
            local parts = {}
            for part in string.gmatch(key, "[^:]+") do
                parts[#parts + 1] = part
            end
            -- key format: meta:cache:{hostID}:{dimID}:{hash}
            local dim_id = parts[4]
            if dim_filter[dim_id] then
                to_delete[#to_delete + 1] = key
            end
        else
            to_delete[#to_delete + 1] = key
        end
    end

    -- Flush in chunks to avoid Lua unpack stack overflow
    while #to_delete >= del_chunk_size do
        local chunk = {}
        for i = 1, del_chunk_size do
            chunk[i] = to_delete[i]
        end
        deleted = deleted + redis.call("DEL", unpack(chunk))
        local remaining = {}
        for i = del_chunk_size + 1, #to_delete do
            remaining[#remaining + 1] = to_delete[i]
        end
        to_delete = remaining
    end
until cursor == "0" or iterations >= max_iterations

if #to_delete > 0 then
    deleted = deleted + redis.call("DEL", unpack(to_delete))
end

return {cursor, deleted}
`

// handleInvalidateAllAPI handles POST /internal/cache/invalidate-all
func (d *CacheDaemon) handleInvalidateAllAPI(ctx *fasthttp.RequestCtx) {
	var req types.InvalidateAllAPIRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		httputil.JSONError(ctx, fmt.Sprintf("invalid json: %s", err.Error()), fasthttp.StatusBadRequest)
		return
	}

	if req.HostID == 0 {
		httputil.JSONError(ctx, "host_id is required", fasthttp.StatusBadRequest)
		return
	}

	host := d.GetHost(req.HostID)
	if host == nil {
		httputil.JSONError(ctx, fmt.Sprintf("host_id %d not found", req.HostID), fasthttp.StatusBadRequest)
		return
	}

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs, true)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}

	// Build Lua script args: hostID, cursor, dimension IDs...
	hasFilter := len(req.DimensionIDs) > 0
	args := make([]interface{}, 0, len(dimensionIDs)+2)
	args = append(args, strconv.Itoa(req.HostID))
	args = append(args, "0") // initial cursor

	if hasFilter {
		for _, dimID := range dimensionIDs {
			args = append(args, strconv.Itoa(dimID))
		}
	}

	reqCtx := context.Background()
	totalDeleted := 0

	for {
		result, err := d.redis.Eval(reqCtx, luaInvalidateAllBatch, nil, args...)
		if err != nil {
			d.logger.Error("Failed to execute invalidate-all batch",
				zap.Int("host_id", req.HostID),
				zap.Int("entries_invalidated_before_error", totalDeleted),
				zap.Error(err))
			httputil.JSONError(ctx, "internal error during invalidation", fasthttp.StatusInternalServerError)
			return
		}

		batch, ok := result.([]interface{})
		if !ok || len(batch) != 2 {
			d.logger.Error("Unexpected Lua script result",
				zap.Int("host_id", req.HostID),
				zap.Int("entries_invalidated_before_error", totalDeleted))
			httputil.JSONError(ctx, "internal error during invalidation", fasthttp.StatusInternalServerError)
			return
		}

		nextCursor, cursorOk := batch[0].(string)
		if !cursorOk {
			d.logger.Error("Unexpected cursor type in Lua script result",
				zap.Int("host_id", req.HostID),
				zap.Int("entries_invalidated_before_error", totalDeleted))
			httputil.JSONError(ctx, "internal error during invalidation", fasthttp.StatusInternalServerError)
			return
		}

		batchDeleted, _ := batch[1].(int64)
		totalDeleted += int(batchDeleted)

		if nextCursor == "0" {
			break
		}

		// Update cursor for next iteration
		args[1] = nextCursor
	}

	data := types.InvalidateAllAPIData{
		HostID:             req.HostID,
		DimensionIDsCount:  len(dimensionIDs),
		EntriesInvalidated: totalDeleted,
	}
	httputil.JSONData(ctx, data, fasthttp.StatusOK)

	d.logger.Info("Invalidate-all request processed",
		zap.Int("host_id", req.HostID),
		zap.Int("dimensions_count", len(dimensionIDs)),
		zap.Int("entries_invalidated", totalDeleted))
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

func (d *CacheDaemon) handleCacheURLsAPI(ctx *fasthttp.RequestCtx) {
	host, hostID, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	cursor := queryParamString(ctx, "cursor")
	if cursor == "" {
		cursor = "0"
	}

	limit, err := queryParamInt(ctx, "limit", defaultLimit)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if limit < 1 || limit > maxLimit {
		httputil.JSONError(ctx, fmt.Sprintf("limit must be between 1 and %d", maxLimit), fasthttp.StatusBadRequest)
		return
	}

	statusFilter := queryParamString(ctx, "status")
	if statusFilter != "" {
		allowed := map[string]bool{"active": true, "stale": true, "expired": true}
		parsed, err := parseCSVFilter(statusFilter, allowed, "status")
		if err != nil {
			httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
			return
		}
		statusFilter = strings.Join(parsed, ",")
	}

	dimensionFilter := queryParamString(ctx, "dimension")
	if dimensionFilter != "" {
		dims := strings.Split(dimensionFilter, ",")
		trimmedDims := make([]string, 0, len(dims))
		for _, dimName := range dims {
			dimName = strings.TrimSpace(dimName)
			if _, exists := host.Render.Dimensions[dimName]; !exists {
				httputil.JSONError(ctx, fmt.Sprintf("dimension '%s' not configured for host", dimName), fasthttp.StatusBadRequest)
				return
			}
			trimmedDims = append(trimmedDims, dimName)
		}
		dimensionFilter = strings.Join(trimmedDims, ",")
	}

	urlContains := queryParamString(ctx, "url_contains")
	if len(urlContains) > 200 {
		httputil.JSONError(ctx, "url_contains must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	sizeMin, err := queryParamInt(ctx, "size_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if sizeMin < 0 {
		httputil.JSONError(ctx, "size_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}

	sizeMax, err := queryParamInt(ctx, "size_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if sizeMax < 0 {
		httputil.JSONError(ctx, "size_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if sizeMax > 0 && sizeMin > 0 && sizeMax < sizeMin {
		httputil.JSONError(ctx, "size_max must be >= size_min", fasthttp.StatusBadRequest)
		return
	}

	cacheAgeMin, err := queryParamInt(ctx, "cache_age_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if cacheAgeMin < 0 {
		httputil.JSONError(ctx, "cache_age_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}

	cacheAgeMax, err := queryParamInt(ctx, "cache_age_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if cacheAgeMax < 0 {
		httputil.JSONError(ctx, "cache_age_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if cacheAgeMax > 0 && cacheAgeMin > 0 && cacheAgeMax < cacheAgeMin {
		httputil.JSONError(ctx, "cache_age_max must be >= cache_age_min", fasthttp.StatusBadRequest)
		return
	}

	statusCodeFilter := queryParamString(ctx, "status_code")
	if statusCodeFilter != "" {
		codes := strings.Split(statusCodeFilter, ",")
		trimmedCodes := make([]string, 0, len(codes))
		for _, sc := range codes {
			sc = strings.TrimSpace(sc)
			if _, err := strconv.Atoi(sc); err != nil {
				httputil.JSONError(ctx, fmt.Sprintf("invalid status_code filter: %s (must be numeric)", sc), fasthttp.StatusBadRequest)
				return
			}
			trimmedCodes = append(trimmedCodes, sc)
		}
		statusCodeFilter = strings.Join(trimmedCodes, ",")
	}

	sourceFilter := queryParamString(ctx, "source")
	if sourceFilter != "" && sourceFilter != "render" && sourceFilter != "bypass" {
		httputil.JSONError(ctx, "source must be 'render' or 'bypass'", fasthttp.StatusBadRequest)
		return
	}

	indexStatusFilter := queryParamString(ctx, "index_status")
	if indexStatusFilter != "" {
		allowed := map[string]bool{"1": true, "2": true, "3": true, "4": true}
		parsed, err := parseCSVFilter(indexStatusFilter, allowed, "index_status")
		if err != nil {
			httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
			return
		}
		indexStatusFilter = strings.Join(parsed, ",")
	}

	staleTTL := d.getStaleTTL(host)

	params := CacheListParams{
		HostID:            hostID,
		Cursor:            cursor,
		Limit:             limit,
		StatusFilter:      statusFilter,
		DimensionFilter:   dimensionFilter,
		URLContains:       urlContains,
		SizeMin:           int64(sizeMin),
		SizeMax:           int64(sizeMax),
		CacheAgeMin:       int64(cacheAgeMin),
		CacheAgeMax:       int64(cacheAgeMax),
		StatusCodeFilter:  statusCodeFilter,
		SourceFilter:      sourceFilter,
		IndexStatusFilter: indexStatusFilter,
		StaleTTL:          staleTTL,
	}

	result, err := d.cacheReader.ListURLs(params)
	if handleRedisError(ctx, err, d.logger) {
		return
	}

	httputil.JSONData(ctx, result, fasthttp.StatusOK)

	d.logger.Debug("Cache URLs request served",
		zap.Int("host_id", hostID),
		zap.String("cursor", cursor),
		zap.Int("limit", limit),
		zap.Int("items_returned", len(result.Items)),
		zap.Bool("has_more", result.HasMore))
}

func (d *CacheDaemon) handleCacheSummaryAPI(ctx *fasthttp.RequestCtx) {
	host, hostID, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	staleTTL := d.getStaleTTL(host)

	result, err := d.cacheReader.GetSummary(hostID, staleTTL)
	if handleRedisError(ctx, err, d.logger) {
		return
	}

	httputil.JSONData(ctx, result, fasthttp.StatusOK)

	d.logger.Debug("Cache summary request served",
		zap.Int("host_id", hostID),
		zap.Int("total_urls", result.TotalUrls))
}

func (d *CacheDaemon) handleCacheQueueAPI(ctx *fasthttp.RequestCtx) {
	host, _, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	cursor := queryParamString(ctx, "cursor")
	if cursor == "" {
		cursor = "0"
	}

	limit, err := queryParamInt(ctx, "limit", defaultLimit)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if limit < 1 || limit > maxLimit {
		httputil.JSONError(ctx, fmt.Sprintf("limit must be between 1 and %d", maxLimit), fasthttp.StatusBadRequest)
		return
	}

	var priorityFilter []string
	priorityRaw := queryParamString(ctx, "priority")
	if priorityRaw != "" {
		allowed := map[string]bool{"high": true, "normal": true, "autorecache": true}
		priorityFilter, err = parseCSVFilter(priorityRaw, allowed, "priority")
		if err != nil {
			httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
			return
		}
	}

	params := QueueListParams{
		HostID:         host.ID,
		Cursor:         cursor,
		Limit:          limit,
		PriorityFilter: priorityFilter,
	}

	result, err := d.queueReader.ListQueueItems(params, host.Render.Dimensions)
	if handleRedisError(ctx, err, d.logger) {
		return
	}

	httputil.JSONData(ctx, result, fasthttp.StatusOK)

	d.logger.Debug("Cache queue request served",
		zap.Int("host_id", host.ID),
		zap.Int("items_returned", len(result.Items)),
		zap.Bool("has_more", result.HasMore))
}

func (d *CacheDaemon) handleCacheQueueSummaryAPI(ctx *fasthttp.RequestCtx) {
	host, _, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	result, err := d.queueReader.GetQueueSummary(host.ID)
	if handleRedisError(ctx, err, d.logger) {
		return
	}

	httputil.JSONData(ctx, result, fasthttp.StatusOK)

	d.logger.Debug("Cache queue summary request served",
		zap.Int("host_id", host.ID),
		zap.Int("pending", result.Pending),
		zap.Int("processing", result.Processing))
}

func queryParamInt(ctx *fasthttp.RequestCtx, name string, defaultValue int) (int, error) {
	raw := string(ctx.QueryArgs().Peek(name))
	if raw == "" {
		return defaultValue, nil
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer", name)
	}
	return val, nil
}

func queryParamString(ctx *fasthttp.RequestCtx, name string) string {
	return string(ctx.QueryArgs().Peek(name))
}

func (d *CacheDaemon) resolveHost(ctx *fasthttp.RequestCtx) (*types.Host, int, bool) {
	hostID, err := queryParamInt(ctx, "host_id", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return nil, 0, false
	}
	if hostID <= 0 {
		httputil.JSONError(ctx, "host_id is required", fasthttp.StatusBadRequest)
		return nil, 0, false
	}
	host := d.GetHost(hostID)
	if host == nil {
		httputil.JSONError(ctx, fmt.Sprintf("host_id %d not found", hostID), fasthttp.StatusNotFound)
		return nil, 0, false
	}
	return host, hostID, true
}

func handleRedisError(ctx *fasthttp.RequestCtx, err error, logger *zap.Logger) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "BUSY") {
		httputil.JSONError(ctx, "redis busy, try again later", fasthttp.StatusServiceUnavailable)
	} else {
		logger.Error("Redis error in cache reader", zap.Error(err))
		httputil.JSONError(ctx, "internal error", fasthttp.StatusInternalServerError)
	}
	return true
}

func parseCSVFilter(value string, allowed map[string]bool, fieldName string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
		if !allowed[parts[i]] {
			return nil, fmt.Errorf("invalid %s filter: %s", fieldName, parts[i])
		}
	}
	return parts, nil
}

package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

const reloadTimeout = 10 * time.Second

// URLStatusResponse is the response for the url-status endpoint
type URLStatusResponse struct {
	URL   string         `json:"url"`
	Cache URLCacheStatus `json:"cache"`
	Queue URLQueueStatus `json:"queue"`
}

// URLCacheStatus contains cache information for a single URL
type URLCacheStatus struct {
	Exists     bool    `json:"exists"`
	Status     *string `json:"status"`
	CreatedAt  *int64  `json:"created_at"`
	StatusCode *int    `json:"status_code"`
}

// URLQueueStatus contains queue information for a single URL
type URLQueueStatus struct {
	Pending     bool    `json:"pending"`
	Priority    *string `json:"priority"`
	ScheduledAt *int64  `json:"scheduled_at"`
}

// URLEntriesResponse is the response for the url-entries endpoint: one cache entry
// per dimension that has cache, resolved by direct key lookup (no listing index).
type URLEntriesResponse struct {
	URL     string         `json:"url"`
	Entries []URLEntryItem `json:"entries"`
}

// URLEntryItem is a single dimension's cache entry for one URL.
type URLEntryItem struct {
	Dimension   string `json:"dimension"`
	DimensionID int    `json:"dimension_id"`
	CacheKey    string `json:"cache_key"`
	Status      string `json:"status"`
	StatusCode  int    `json:"status_code"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
	CacheAge    int64  `json:"cache_age"`
	Size        int64  `json:"size"`
}

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
	case method == "POST" && path == "/internal/reload":
		d.handleReloadAPI(ctx)
	case method == "GET" && path == "/internal/cache/urls":
		d.handleCacheURLsAPI(ctx)
	case method == "GET" && path == "/internal/cache/summary":
		d.handleCacheSummaryAPI(ctx)
	case method == "GET" && path == "/internal/cache/queue":
		d.handleCacheQueueAPI(ctx)
	case method == "GET" && path == "/internal/cache/queue/summary":
		d.handleCacheQueueSummaryAPI(ctx)
	case method == "GET" && path == "/internal/cache/url-status":
		d.handleURLStatusAPI(ctx)
	case method == "GET" && path == "/internal/cache/url-entries":
		d.handleURLEntriesAPI(ctx)
	default:
		httputil.JSONError(ctx, "not found", fasthttp.StatusNotFound)
	}
}

// resolveDimensionIDs builds the list of non-block dimension IDs for a host and validates any
// explicitly requested IDs against it. Block dimensions never produce cache entries.
// Returns all non-block dimensions if requestedIDs is empty.
func resolveDimensionIDs(host *types.Host, requestedIDs []int) ([]int, error) {
	allDimensionIDs := make([]int, 0, len(host.Dimensions))
	for _, dim := range host.Dimensions {
		if dim.EffectiveAction() == types.ActionBlock {
			continue
		}
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

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}

	// Enqueue entries to ZSET
	queueKey := d.keyGenerator.RecacheQueueKey(req.HostID, req.Priority)
	score := float64(time.Now().UTC().Unix())
	entriesEnqueued := 0
	reqCtx := context.Background()
	normalize := d.hostURLNormalizer(host)

	for _, url := range req.URLs {
		// Canonicalize the URL (strip tracking params) before ZADD so the queue is keyed
		// the same way the cache is written and read.
		normalizedURL, _, err := normalize(url)
		if err != nil {
			d.logger.Error("Invalid URL, skipping",
				zap.String("url", url),
				zap.Error(err))
			continue
		}

		for _, dimensionID := range dimensionIDs {
			member := types.RecacheMember{
				URL:         normalizedURL,
				DimensionID: dimensionID,
			}
			memberJSON, _ := json.Marshal(member)

			if err := d.redis.ZAdd(reqCtx, queueKey, score, string(memberJSON)); err != nil {
				d.logger.Error("Failed to add entry to ZSET",
					zap.String("queue", queueKey),
					zap.String("url", normalizedURL),
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

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}

	// Invalidate cache entries
	entriesInvalidated := 0
	reqCtx := context.Background()
	normalize := d.hostURLNormalizer(host)

	for _, url := range req.URLs {
		// Canonicalize the URL (strip tracking params) so invalidation targets the same
		// key the cache was written under.
		normalizedURL, urlHash, err := normalize(url)
		if err != nil {
			d.logger.Error("Invalid URL, skipping",
				zap.String("url", url),
				zap.Error(err))
			continue
		}

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
				zap.String("url", normalizedURL),
				zap.Uint64("url_hash", urlHash),
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

	dimensionIDs, err := resolveDimensionIDs(host, req.DimensionIDs)
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
		RSCapacity:  d.GetRSCapacityStatus(),
		Queues:      d.GetQueuesStatus(),
		Concurrency: d.concurrencyLimiter.AllStats(),
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

// handleReloadAPI handles POST /internal/reload
func (d *CacheDaemon) handleReloadAPI(ctx *fasthttp.RequestCtx) {
	if d.reloadFunc == nil {
		httputil.JSONSuccess(ctx, "reload not configured", fasthttp.StatusOK)
		return
	}

	reloadCtx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
	defer cancel()

	if err := d.reloadFunc(reloadCtx); err != nil {
		d.logger.Error("reload failed", zap.Error(err))
		httputil.JSONError(ctx, fmt.Sprintf("reload failed: %s", err.Error()), fasthttp.StatusInternalServerError)
		return
	}

	d.logger.Info("Configuration reloaded via API")
	httputil.JSONSuccess(ctx, "reload completed", fasthttp.StatusOK)
}

// handleCacheURLsAPI handles GET /internal/cache/urls
// Query params:
//
//	host_id            - required, host identifier
//	cursor             - pagination cursor (default "0")
//	limit              - items per page (1-100, default 25)
//	status             - CSV: active,stale,expired
//	dimension          - CSV: dimension names
//	url_contains       - case-insensitive URL substring
//	size_min           - minimum size (bytes)
//	size_max           - maximum size (bytes)
//	cache_age_min      - minimum cache age (seconds)
//	cache_age_max      - maximum cache age (seconds)
//	status_code        - CSV: HTTP status codes
//	source             - render or bypass
//	index_status       - CSV: 1,2,3,4
//	title              - case-insensitive title substring
//	created_at_min     - minimum created_at timestamp (unix seconds)
//	created_at_max     - maximum created_at timestamp (unix seconds)
//	expires_at_min     - minimum expires_at timestamp
//	expires_at_max     - maximum expires_at timestamp
//	last_access_min    - minimum last_access timestamp
//	last_access_max    - maximum last_access timestamp
//	last_bot_hit_min   - minimum last_bot_hit timestamp (excludes null)
//	last_bot_hit_max   - maximum last_bot_hit timestamp (excludes null)
//	url_starts_with    - case-insensitive URL prefix
//	url_ends_with      - case-insensitive URL suffix
//	url_neq            - case-insensitive URL not-equal
//	url_not_contains   - case-insensitive URL not-contains
//	title_starts_with  - case-insensitive title prefix
//	title_ends_with    - case-insensitive title suffix
//	title_neq          - case-insensitive title not-equal
//	title_not_contains - case-insensitive title not-contains
//	last_bot_hit_exists - "true" or "false"
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
		parsed, err := httputil.ParseCSVFilter(statusFilter, allowed, "status")
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
			if _, exists := host.Dimensions[dimName]; !exists {
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
		parsed, err := httputil.ParseCSVFilter(indexStatusFilter, allowed, "index_status")
		if err != nil {
			httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
			return
		}
		indexStatusFilter = strings.Join(parsed, ",")
	}

	titleFilter := queryParamString(ctx, "title")
	if len(titleFilter) > 200 {
		httputil.JSONError(ctx, "title must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	createdAtMin, err := queryParamInt64(ctx, "created_at_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if createdAtMin < 0 {
		httputil.JSONError(ctx, "created_at_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	createdAtMax, err := queryParamInt64(ctx, "created_at_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if createdAtMax < 0 {
		httputil.JSONError(ctx, "created_at_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if createdAtMax > 0 && createdAtMin > 0 && createdAtMax < createdAtMin {
		httputil.JSONError(ctx, "created_at_max must be >= created_at_min", fasthttp.StatusBadRequest)
		return
	}

	expiresAtMin, err := queryParamInt64(ctx, "expires_at_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if expiresAtMin < 0 {
		httputil.JSONError(ctx, "expires_at_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	expiresAtMax, err := queryParamInt64(ctx, "expires_at_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if expiresAtMax < 0 {
		httputil.JSONError(ctx, "expires_at_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if expiresAtMax > 0 && expiresAtMin > 0 && expiresAtMax < expiresAtMin {
		httputil.JSONError(ctx, "expires_at_max must be >= expires_at_min", fasthttp.StatusBadRequest)
		return
	}

	lastAccessMin, err := queryParamInt64(ctx, "last_access_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if lastAccessMin < 0 {
		httputil.JSONError(ctx, "last_access_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	lastAccessMax, err := queryParamInt64(ctx, "last_access_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if lastAccessMax < 0 {
		httputil.JSONError(ctx, "last_access_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if lastAccessMax > 0 && lastAccessMin > 0 && lastAccessMax < lastAccessMin {
		httputil.JSONError(ctx, "last_access_max must be >= last_access_min", fasthttp.StatusBadRequest)
		return
	}

	lastBotHitMin, err := queryParamInt64(ctx, "last_bot_hit_min", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if lastBotHitMin < 0 {
		httputil.JSONError(ctx, "last_bot_hit_min must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	lastBotHitMax, err := queryParamInt64(ctx, "last_bot_hit_max", 0)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}
	if lastBotHitMax < 0 {
		httputil.JSONError(ctx, "last_bot_hit_max must be >= 0", fasthttp.StatusBadRequest)
		return
	}
	if lastBotHitMax > 0 && lastBotHitMin > 0 && lastBotHitMax < lastBotHitMin {
		httputil.JSONError(ctx, "last_bot_hit_max must be >= last_bot_hit_min", fasthttp.StatusBadRequest)
		return
	}

	urlStartsWith := queryParamString(ctx, "url_starts_with")
	if len(urlStartsWith) > 200 {
		httputil.JSONError(ctx, "url_starts_with must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	urlEndsWith := queryParamString(ctx, "url_ends_with")
	if len(urlEndsWith) > 200 {
		httputil.JSONError(ctx, "url_ends_with must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	urlNeq := queryParamString(ctx, "url_neq")
	if len(urlNeq) > 200 {
		httputil.JSONError(ctx, "url_neq must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	urlNotContains := queryParamString(ctx, "url_not_contains")
	if len(urlNotContains) > 200 {
		httputil.JSONError(ctx, "url_not_contains must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	titleStartsWith := queryParamString(ctx, "title_starts_with")
	if len(titleStartsWith) > 200 {
		httputil.JSONError(ctx, "title_starts_with must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	titleEndsWith := queryParamString(ctx, "title_ends_with")
	if len(titleEndsWith) > 200 {
		httputil.JSONError(ctx, "title_ends_with must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	titleNeq := queryParamString(ctx, "title_neq")
	if len(titleNeq) > 200 {
		httputil.JSONError(ctx, "title_neq must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	titleNotContains := queryParamString(ctx, "title_not_contains")
	if len(titleNotContains) > 200 {
		httputil.JSONError(ctx, "title_not_contains must be at most 200 characters", fasthttp.StatusBadRequest)
		return
	}

	lastBotHitExists := queryParamString(ctx, "last_bot_hit_exists")
	if lastBotHitExists != "" && lastBotHitExists != "true" && lastBotHitExists != "false" {
		httputil.JSONError(ctx, "last_bot_hit_exists must be 'true' or 'false'", fasthttp.StatusBadRequest)
		return
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
		Title:             titleFilter,
		CreatedAtMin:      createdAtMin,
		CreatedAtMax:      createdAtMax,
		ExpiresAtMin:      expiresAtMin,
		ExpiresAtMax:      expiresAtMax,
		LastAccessMin:     lastAccessMin,
		LastAccessMax:     lastAccessMax,
		LastBotHitMin:     lastBotHitMin,
		LastBotHitMax:     lastBotHitMax,
		URLStartsWith:     urlStartsWith,
		URLEndsWith:       urlEndsWith,
		URLNeq:            urlNeq,
		URLNotContains:    urlNotContains,
		TitleStartsWith:   titleStartsWith,
		TitleEndsWith:     titleEndsWith,
		TitleNeq:          titleNeq,
		TitleNotContains:  titleNotContains,
		LastBotHitExists:  lastBotHitExists,
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
		priorityFilter, err = httputil.ParseCSVFilter(priorityRaw, allowed, "priority")
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

	result, err := d.queueReader.ListQueueItems(params, host.Dimensions)
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

func (d *CacheDaemon) handleURLStatusAPI(ctx *fasthttp.RequestCtx) {
	host, hostID, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	rawURL := queryParamString(ctx, "url")
	if rawURL == "" {
		httputil.JSONError(ctx, "url is required", fasthttp.StatusBadRequest)
		return
	}

	normalizedURL, urlHash, err := d.normalizeURLForHost(host, rawURL)
	if err != nil {
		httputil.JSONError(ctx, "invalid url", fasthttp.StatusBadRequest)
		return
	}

	dimensionIDs, err := resolveDimensionIDs(host, nil)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}

	staleTTL := d.getStaleTTL(host)
	now := time.Now().UTC().Unix()
	reqCtx := context.Background()

	// Cache lookup: keep the most recently created entry across all dimensions.
	var best *dimCacheMeta
	for _, dimID := range dimensionIDs {
		meta, ok := d.readDimCacheMeta(reqCtx, hostID, dimID, urlHash, staleTTL, now)
		if !ok {
			continue
		}
		if best == nil || meta.CreatedAt > best.CreatedAt {
			best = meta
		}
	}
	cacheFound := best != nil

	// Queue lookup: find highest priority match
	priorities := []string{redis.PriorityHigh, redis.PriorityNormal, redis.PriorityAutorecache}
	var queuePriority string
	var queueScheduledAt int64
	queueFound := false

	for _, priority := range priorities {
		if queueFound {
			break
		}
		queueKey := d.keyGenerator.RecacheQueueKey(hostID, priority)
		for _, dimID := range dimensionIDs {
			member := types.RecacheMember{
				URL:         normalizedURL,
				DimensionID: dimID,
			}
			memberJSON, _ := json.Marshal(member)

			score, err := d.redis.ZScore(reqCtx, queueKey, string(memberJSON))
			if err != nil {
				continue
			}
			queuePriority = priority
			queueScheduledAt = int64(score)
			queueFound = true
			break
		}
	}

	// Build response
	response := URLStatusResponse{
		URL: normalizedURL,
		Cache: URLCacheStatus{
			Exists: cacheFound,
		},
		Queue: URLQueueStatus{
			Pending: queueFound,
		},
	}

	if cacheFound {
		response.Cache.Status = &best.Status
		response.Cache.CreatedAt = &best.CreatedAt
		response.Cache.StatusCode = &best.StatusCode
	}

	if queueFound {
		response.Queue.Priority = &queuePriority
		response.Queue.ScheduledAt = &queueScheduledAt
	}

	httputil.JSONData(ctx, response, fasthttp.StatusOK)
}

// handleURLEntriesAPI handles GET /internal/cache/url-entries. It resolves one cache
// entry per host dimension (or a single dimension when dimension_id is given) by direct
// key lookup, without consulting the listing index. A URL with no cached entries returns
// 200 with an empty entries array.
func (d *CacheDaemon) handleURLEntriesAPI(ctx *fasthttp.RequestCtx) {
	host, hostID, ok := d.resolveHost(ctx)
	if !ok {
		return
	}

	rawURL := queryParamString(ctx, "url")
	if rawURL == "" {
		httputil.JSONError(ctx, "url is required", fasthttp.StatusBadRequest)
		return
	}

	normalizedURL, urlHash, err := d.normalizeURLForHost(host, rawURL)
	if err != nil {
		httputil.JSONError(ctx, "invalid url", fasthttp.StatusBadRequest)
		return
	}

	var requestedDims []int
	if ctx.QueryArgs().Has("dimension_id") {
		dimID, err := queryParamInt(ctx, "dimension_id", 0)
		if err != nil {
			httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
			return
		}
		requestedDims = []int{dimID}
	}

	dimensionIDs, err := resolveDimensionIDs(host, requestedDims)
	if err != nil {
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusBadRequest)
		return
	}

	dimNames := dimensionIDToName(host)
	staleTTL := d.getStaleTTL(host)
	now := time.Now().UTC().Unix()
	reqCtx := context.Background()

	entries := make([]URLEntryItem, 0, len(dimensionIDs))
	for _, dimID := range dimensionIDs {
		meta, ok := d.readDimCacheMeta(reqCtx, hostID, dimID, urlHash, staleTTL, now)
		if !ok {
			continue
		}
		cacheKey := d.keyGenerator.GenerateCacheKey(hostID, dimID, urlHash)
		entries = append(entries, URLEntryItem{
			Dimension:   dimNames[dimID],
			DimensionID: dimID,
			CacheKey:    cacheKey.String(),
			Status:      meta.Status,
			StatusCode:  meta.StatusCode,
			CreatedAt:   meta.CreatedAt,
			ExpiresAt:   meta.ExpiresAt,
			CacheAge:    now - meta.CreatedAt,
			Size:        meta.Size,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].DimensionID < entries[j].DimensionID
	})

	httputil.JSONData(ctx, URLEntriesResponse{URL: normalizedURL, Entries: entries}, fasthttp.StatusOK)
}

// dimCacheMeta is one dimension's cache metadata resolved from Redis.
type dimCacheMeta struct {
	CreatedAt  int64
	ExpiresAt  int64
	Size       int64
	StatusCode int
	Status     string // active | stale | expired
}

// readDimCacheMeta loads cache metadata for a single dimension. Returns false when no
// entry exists for that dimension. Status (active/stale/expired) is computed from the
// expiry and the host's stale TTL.
func (d *CacheDaemon) readDimCacheMeta(ctx context.Context, hostID, dimID int, urlHash uint64, staleTTL, now int64) (*dimCacheMeta, bool) {
	cacheKey := d.keyGenerator.GenerateCacheKey(hostID, dimID, urlHash)
	metadataKey := d.keyGenerator.GenerateMetadataKey(cacheKey)

	data, err := d.redis.HGetAll(ctx, metadataKey)
	if err != nil {
		d.logger.Error("Failed to get cache metadata",
			zap.Int("host_id", hostID),
			zap.Int("dimension_id", dimID),
			zap.Error(err))
		return nil, false
	}
	if len(data) == 0 {
		return nil, false
	}

	createdAt, _ := strconv.ParseInt(data["created_at"], 10, 64)
	expiresAt, _ := strconv.ParseInt(data["expires_at"], 10, 64)
	size, _ := strconv.ParseInt(data["size"], 10, 64)
	statusCode, _ := strconv.Atoi(data["status_code"])

	var status string
	switch {
	case now < expiresAt:
		status = "active"
	case staleTTL > 0 && now < expiresAt+staleTTL:
		status = "stale"
	default:
		status = "expired"
	}

	return &dimCacheMeta{
		CreatedAt:  createdAt,
		ExpiresAt:  expiresAt,
		Size:       size,
		StatusCode: statusCode,
		Status:     status,
	}, true
}

// hostURLNormalizer returns a per-host normalizer that strips tracking parameters exactly as
// the edge does when writing cache keys (resolved global -> host -> URL-rule). The config
// resolver is per-host, so callers that normalize many URLs (recache, invalidate) build it once
// and reuse the returned closure across the batch.
func (d *CacheDaemon) hostURLNormalizer(host *types.Host) func(rawURL string) (string, uint64, error) {
	egConfig := d.configManager.GetConfig()
	resolver := config.NewConfigResolver(
		&egConfig.Render,
		&egConfig.Bypass,
		egConfig.TrackingParams,
		egConfig.CacheSharding,
		egConfig.BothitRecache,
		egConfig.Headers,
		egConfig.Storage.Compression,
		host,
	)

	return func(rawURL string) (string, uint64, error) {
		resolved := resolver.ResolveForURL(rawURL)

		var stripPatterns []config.CompiledStripPattern
		if resolved.TrackingParams != nil && resolved.TrackingParams.Enabled {
			stripPatterns = resolved.TrackingParams.CompiledPatterns
		}

		result, err := d.normalizer.Normalize(rawURL, stripPatterns)
		if err != nil {
			return "", 0, err
		}
		return result.NormalizedURL, d.normalizer.Hash(result.NormalizedURL), nil
	}
}

// normalizeURLForHost normalizes a single rawURL for the host (see hostURLNormalizer).
func (d *CacheDaemon) normalizeURLForHost(host *types.Host, rawURL string) (string, uint64, error) {
	return d.hostURLNormalizer(host)(rawURL)
}

// dimensionIDToName maps a host's dimension IDs to their configured names.
func dimensionIDToName(host *types.Host) map[int]string {
	names := make(map[int]string, len(host.Dimensions))
	for name, dim := range host.Dimensions {
		names[dim.ID] = name
	}
	return names
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

func queryParamInt64(ctx *fasthttp.RequestCtx, name string, defaultValue int64) (int64, error) {
	raw := string(ctx.QueryArgs().Peek(name))
	if raw == "" {
		return defaultValue, nil
	}
	val, err := strconv.ParseInt(raw, 10, 64)
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

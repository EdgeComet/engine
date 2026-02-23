package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/htmlprocessor"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	// Lock TTL calculation
	lockTTLBuffer = 3 * time.Second
	minLockTTL    = 30 * time.Second

	// Concurrent render wait timeout calculation
	concurrentRenderWaitPercent = 0.8 // 80% of host render timeout
	minConcurrentWait           = 5 * time.Second
	maxConcurrentWait           = 60 * time.Second

	// Poll interval for checking cache during concurrent wait
	concurrentRenderPollInterval = 200 * time.Millisecond

	// Redis operation timeouts (independent of request context to prevent race conditions)
	redisTabOperationTimeout   = 2 * time.Second // Tab reservation/release
	redisLockOperationTimeout  = 3 * time.Second // Lock acquisition
	redisCacheOperationTimeout = 5 * time.Second // Cache metadata storage

	// Sharding operation timeouts
	defaultInterEgTimeout = 3 * time.Second // Default timeout for inter-EG operations (push/pull)

	// Bypass cache sharding threshold
	// Responses smaller than this are not replicated (e.g., 301/302 redirects with empty bodies)
	// Metadata (status code, headers) is already in Redis and shared across all EGs
	minBypassBodySizeForReplication = 100 // bytes

	contentTypeHTML = "text/html"
)

// selectAndReserveScript atomically selects a healthy render service and reserves an available tab
const selectAndReserveScript = `
-- Atomically selects a healthy render service and reserves an available tab
-- ARGV[1] = request_id
-- ARGV[2] = strategy ("least_loaded", "most_available", or "round_robin")
-- ARGV[3] = reservation TTL (seconds, typically 2)

local request_id = ARGV[1]
local strategy = ARGV[2]
local reservation_ttl = tonumber(ARGV[3])

-- 1. Find all render services
local service_keys = redis.call('KEYS', 'service:render:*')
if #service_keys == 0 then
    return {false, 'no_services'}
end

-- 2. Filter healthy services and collect tab availability info
local candidates = {}
for _, service_key in ipairs(service_keys) do
    local service_data = redis.call('GET', service_key)
    if service_data then
        local service = cjson.decode(service_data)

        -- Only consider services with available capacity (registry already handles staleness via TTL)
        -- Check: capacity exists, is positive, and has available slots (load < capacity)
        if service.capacity and service.capacity > 0 and (service.load or 0) < service.capacity then
            local service_id = service.id
            local tabs_key = 'tabs:' .. service_id

            if redis.call('EXISTS', tabs_key) == 1 then
                local tabs = redis.call('HGETALL', tabs_key)
                local available_count = 0
                local first_available = nil

                for i = 1, #tabs, 2 do
                    local tab_id = tonumber(tabs[i])
                    local tab_value = tabs[i + 1]

                    if tab_value == '' then
                        available_count = available_count + 1
                        if first_available == nil then
                            first_available = tab_id
                        end
                    end
                end

                if available_count > 0 then
                    -- Calculate load percentage with nil check
                    local load = service.load or 0
                    local load_pct = load / service.capacity

                    table.insert(candidates, {
                        service_id = service_id,
                        service = service,
                        tabs_key = tabs_key,
                        available_count = available_count,
                        first_available = first_available,
                        load_pct = load_pct
                    })
                end
            end
        end
    end
end

if #candidates == 0 then
    return {false, 'no_capacity'}
end

-- 3. Select best service based on strategy
local selected = candidates[1]

if strategy == 'least_loaded' then
    for _, candidate in ipairs(candidates) do
        if candidate.load_pct < selected.load_pct then
            selected = candidate
        end
    end
elseif strategy == 'most_available' then
    for _, candidate in ipairs(candidates) do
        if candidate.available_count > selected.available_count then
            selected = candidate
        end
    end
end

-- 4. Reserve the first available tab
local tab_id = selected.first_available
redis.call('HSET', selected.tabs_key, tostring(tab_id), request_id)
redis.call('EXPIRE', selected.tabs_key, reservation_ttl)

-- 5. Return result: {service_id, tab_id, address, port}
return {
    selected.service_id,
    tostring(tab_id),
    selected.service.address,
    tostring(selected.service.port)
}
`

// WaitResult represents the outcome of waiting for a concurrent render
type WaitResult int

const (
	WaitCacheAvailable WaitResult = iota // Cache became available, request served
	WaitRequestTimeout                   // Request timeout during wait
	WaitTimeout                          // Wait timeout exceeded
)

// ResponseSource indicates where the response content came from
type ResponseSource int

const (
	ServedFromCache       ResponseSource = iota // Content served from cache
	ServedFromRender                            // Content freshly rendered
	ServedFromBypass                            // Content proxied from origin
	ServedFromBypassCache                       // Content served from bypass cache
)

// RenderResult represents the outcome of processing a render request
type RenderResult struct {
	Source      ResponseSource // Where the content came from
	ServiceID   string         // Render service ID (if rendered)
	Duration    time.Duration  // Processing duration
	BytesServed int64          // Response size in bytes

	// Extended fields for event logging
	StatusCode   int                // HTTP status code
	PageSEO      *types.PageSEO     // Full SEO metadata (nil for cache hits; populated for renders and bypass HTML)
	Metrics      *types.PageMetrics // Page metrics (nil for cache hits)
	CacheAge     time.Duration      // Cache age (for cache hits)
	ChromeID     string             // Chrome instance ID (for renders)
	RenderTime   time.Duration      // Render duration (for renders)
	ErrorType    string             // Structured error category (e.g., "soft_timeout", "origin_4xx")
	ErrorMessage string             // Detailed error description
}

// RenderOrchestrator coordinates rendering requests, service selection, and fallback handling
type RenderOrchestrator struct {
	// Specialized coordinators
	cacheCoord     *CacheCoordinator
	lockCoord      *LockCoordinator
	responseWriter *ResponseWriter

	// Existing dependencies
	bypassSvc        *bypass.BypassService
	metricsCollector *metrics.MetricsCollector
	serviceRegistry  *registry.ServiceRegistry
	rsClient         *rsclient.RSClient
	redis            *redis.Client
	logger           *zap.Logger
	configManager    configtypes.EGConfigManager
}

// TabReservation contains service and tab info from Lua script
type TabReservation struct {
	ServiceID string
	TabID     int
	Address   string
	Port      int
}

// RenderServiceResult encapsulates the complete result from a render service call
// Contains HTML content and all metrics captured during rendering
type RenderServiceResult struct {
	HTML             []byte              // Rendered HTML content
	StatusCode       int                 // HTTP status code captured by renderer
	RedirectLocation string              // Target URL for 3xx redirects (from FinalURL)
	RenderTime       time.Duration       // Time taken to render
	ChromeID         string              // ID of Chrome instance that performed render
	Metrics          types.PageMetrics   // Complete page metrics (lifecycle, errors, etc.)
	Headers          map[string][]string // HTTP response headers from rendered page
	HAR              []byte              // HAR data for debugging (JSON bytes)
	PageSEO          *types.PageSEO      // Comprehensive SEO metadata extracted from HTML
	ErrorType        string              // Structured error category from render service
	ErrorMessage     string              // Detailed error description from render service
}

// NewRenderOrchestrator creates a new RenderOrchestrator instance
func NewRenderOrchestrator(
	metadata *cache.MetadataStore,
	bypassSvc *bypass.BypassService,
	cacheService *cache.CacheService,
	metricsCollector *metrics.MetricsCollector,
	serviceRegistry *registry.ServiceRegistry,
	fsCache *cache.FilesystemCache,
	rsClient *rsclient.RSClient,
	redisClient *redis.Client,
	configManager configtypes.EGConfigManager,
	shardingManager ShardingManager,
	logger *zap.Logger,
) *RenderOrchestrator {
	// Create specialized coordinators
	cacheCoord := NewCacheCoordinator(metadata, fsCache, cacheService, shardingManager, metricsCollector, logger)
	lockCoord := NewLockCoordinator(metadata, logger)
	responseWriter := NewResponseWriter()

	return &RenderOrchestrator{
		cacheCoord:       cacheCoord,
		lockCoord:        lockCoord,
		responseWriter:   responseWriter,
		bypassSvc:        bypassSvc,
		metricsCollector: metricsCollector,
		serviceRegistry:  serviceRegistry,
		rsClient:         rsClient,
		redis:            redisClient,
		configManager:    configManager,
		logger:           logger,
	}
}

// ProcessRenderRequest handles the complete render workflow with caching and fallback
func (ro *RenderOrchestrator) ProcessRenderRequest(renderCtx *edgectx.RenderContext) (*RenderResult, error) {
	// Use pre-resolved config from renderCtx (resolved in server.go)
	// Config resolution happens ONCE per request before calling orchestrator
	resolved := renderCtx.ResolvedConfig
	if resolved == nil {
		// Should never happen - defensive check
		return nil, fmt.Errorf("resolved config not found in render context")
	}

	renderCtx.Logger.Debug("Using pre-resolved configuration",
		zap.String("url", renderCtx.TargetURL),
		zap.String("action", string(resolved.Action)),
		zap.Duration("cache_ttl", resolved.Cache.TTL),
		zap.Duration("render_timeout", resolved.Render.Timeout))

	// Handle status actions immediately (no rendering, no caching, no bypass)
	if resolved.Action.IsStatusAction() {
		ro.metricsCollector.RecordBypass(renderCtx.Host.Domain, fmt.Sprintf("status_%d", resolved.Status.Code))
		ro.responseWriter.WriteStatusResponse(renderCtx, resolved.Status)
		return &RenderResult{
			Source:      ServedFromBypass, // Treated similar to bypass
			Duration:    time.Millisecond, // Minimal duration
			BytesServed: int64(renderCtx.HTTPCtx.Response.Header.ContentLength()),
			StatusCode:  resolved.Status.Code,
		}, nil
	}

	// Handle bypass action immediately (skip rendering and caching)
	if resolved.Action == types.ActionBypass {
		renderCtx.Logger.Info("URL matched bypass rule, fetching from origin directly")
		return ro.serveBypass(renderCtx, "url_rule")
	}

	// Continue with normal render workflow for action="render"

	// 1. CHECK CACHE FIRST (early optimization - avoids locking for cache hits)
	// Detect both fresh and stale cache, but only serve fresh immediately
	var staleCache *cache.CacheMetadata
	if cached, exists := ro.cacheCoord.LookupCache(renderCtx); exists {
		// Check if cache is fresh or stale
		if cached.IsFresh() {
			// Redirects are metadata-only and accessible via Redis on all EGs
			// Regular content requires file ownership check
			isRedirect := isRedirectStatusCode(cached.StatusCode)

			if isRedirect || ro.cacheCoord.IsFileLocal(cached) {
				result, err := ro.serveFromCache(renderCtx, cached)
				if err == nil {
					renderCtx.Logger.Info("Early cache hit, served without locking")
					return result, nil
				}
				// File not accessible locally - will try to pull from remote in next step
				renderCtx.Logger.Warn("Cache file not accessible, will attempt pull or render",
					zap.String("relative_file_path", cached.FilePath),
					zap.Error(err))
			}
			// If not local, try pulling from remote EG immediately
			if result, pulled := ro.tryPullFromRemoteSmartly(renderCtx, cached, false); pulled {
				return result, nil
			}
		} else if ro.isStaleServable(renderCtx, cached) {
			// Cache is stale but servable - store for later use if render fails
			staleCache = cached
			renderCtx.Logger.Debug("Stale cache detected, will use as fallback if render fails",
				zap.Duration("stale_age", cached.StaleAge()))
			// Don't serve stale now - attempt fresh render first
		}
	}

	// 2. TRY TO ACQUIRE LOCK FOR RENDERING
	acquired, err := ro.lockCoord.AcquireLock(renderCtx)
	if err != nil {
		return ro.serveBypass(renderCtx, "lock_error")
	}

	if !acquired {
		// 3. WAIT FOR CONCURRENT RENDER
		waitResult, err := ro.lockCoord.WaitForConcurrentRender(renderCtx, ro.cacheCoord, ro.metricsCollector)
		if err != nil {
			// Try to serve stale cache if available
			if staleCache != nil {
				return ro.serveStaleCache(renderCtx, staleCache, "wait_error")
			}
			return ro.serveBypass(renderCtx, "wait_error")
		}

		switch waitResult {
		case WaitCacheAvailable:
			// Cache metadata appeared during wait - delegate to specialized handler
			cached, exists := ro.cacheCoord.LookupCache(renderCtx)
			if !exists {
				// Try to serve stale cache if available
				if staleCache != nil {
					return ro.serveStaleCache(renderCtx, staleCache, "cache_disappeared")
				}
				return ro.serveBypass(renderCtx, "cache_disappeared")
			}
			return ro.handleCacheAvailableAfterWait(renderCtx, cached, staleCache)
		case WaitRequestTimeout:
			// Try to serve stale cache if available
			if staleCache != nil {
				return ro.serveStaleCache(renderCtx, staleCache, "request_timeout")
			}
			return ro.serveBypass(renderCtx, "request_timeout")
		case WaitTimeout:
			// Try to serve stale cache if available
			if staleCache != nil {
				return ro.serveStaleCache(renderCtx, staleCache, "concurrent_render_timeout")
			}
			return ro.serveBypass(renderCtx, "concurrent_render_timeout")
		default:
			// Try to serve stale cache if available
			if staleCache != nil {
				return ro.serveStaleCache(renderCtx, staleCache, "unexpected_wait_result")
			}
			return ro.serveBypass(renderCtx, "unexpected_wait_result")
		}
	}

	// 4. WE HAVE THE LOCK - will release after cache write completes
	// NOTE: Lock must be held until cache metadata is fully committed to Redis
	// to prevent race conditions where subsequent requests miss the cache entry

	// 5. DOUBLE-CHECK CACHE (another request might have rendered while we waited for lock)
	if cached, exists := ro.cacheCoord.LookupCache(renderCtx); exists && cached.IsFresh() {
		// Only attempt to serve locally if current EG owns the file
		if ro.cacheCoord.IsFileLocal(cached) {
			result, err := ro.serveFromCache(renderCtx, cached)
			if err == nil {
				renderCtx.Logger.Info("Cache appeared while waiting for lock, served without rendering")
				ro.lockCoord.ReleaseLock(renderCtx)
				return result, nil
			}
			renderCtx.Logger.Warn("Cache metadata exists but file not accessible, proceeding to render",
				zap.String("file_path", cached.FilePath),
				zap.Error(err))
		}
		// If not local, proceed to render (lock will be released after render completes)
	}

	// 6. EXECUTE RENDER WORKFLOW (lock will be released inside this function)
	return ro.executeRenderWithExplicitServing(renderCtx, staleCache)
}

// selectServiceAndReserveTab atomically selects service and reserves tab using Lua script
func (ro *RenderOrchestrator) selectServiceAndReserveTab(ctx context.Context, requestID string, logger *zap.Logger) (*TabReservation, error) {
	// Use independent timeout to prevent race condition from request cancellation
	// This ensures tab reservation always completes or fails atomically
	redisCtx, cancel := context.WithTimeout(context.Background(), redisTabOperationTimeout)
	defer cancel()

	// Get selection strategy from config (default applied in config.applyDefaults())
	strategy := ro.configManager.GetConfig().Registry.SelectionStrategy

	// Execute Lua script to atomically select service and reserve tab
	result, err := ro.redis.Eval(
		redisCtx, // ✅ Independent context prevents orphaned reservations
		selectAndReserveScript,
		[]string{}, // No KEYS needed
		requestID,
		strategy, // selection strategy from config
		2,        // reservation TTL (seconds)
	)
	if err != nil {
		logger.Error("Lua script execution failed",
			zap.String("request_id", requestID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to execute service selection script: %w", err)
	}

	// Parse result
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) < 2 {
		logger.Error("Invalid script result format",
			zap.String("request_id", requestID))
		return nil, fmt.Errorf("invalid script result")
	}

	// Check for error codes
	if resultSlice[0] == nil || resultSlice[0] == false {
		reason := "unknown"
		if len(resultSlice) > 1 {
			if r, ok := resultSlice[1].(string); ok {
				reason = r
			}
		}

		logger.Debug("Service selection failed",
			zap.String("request_id", requestID),
			zap.String("reason", reason))

		if reason == "no_services" {
			return nil, fmt.Errorf("no healthy services available")
		}
		if reason == "no_capacity" {
			return nil, fmt.Errorf("all services at capacity")
		}
		return nil, fmt.Errorf("service selection failed: %s", reason)
	}

	// Parse successful result: {service_id, tab_id, address, port}
	if len(resultSlice) < 4 {
		logger.Error("Incomplete result from script",
			zap.String("request_id", requestID),
			zap.Int("length", len(resultSlice)))
		return nil, fmt.Errorf("incomplete script result")
	}

	serviceID, _ := resultSlice[0].(string)
	tabIDStr, _ := resultSlice[1].(string)
	address, _ := resultSlice[2].(string)
	portStr, _ := resultSlice[3].(string)

	tabID, err := strconv.Atoi(tabIDStr)
	if err != nil {
		logger.Error("Invalid tab_id",
			zap.String("request_id", requestID),
			zap.String("tab_id_str", tabIDStr),
			zap.Error(err))
		return nil, fmt.Errorf("invalid tab_id: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		logger.Error("Invalid port",
			zap.String("request_id", requestID),
			zap.String("port_str", portStr),
			zap.Error(err))
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	reservation := &TabReservation{
		ServiceID: serviceID,
		TabID:     tabID,
		Address:   address,
		Port:      port,
	}

	logger.Debug("Selected service and reserved tab",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("address", reservation.Address),
		zap.Int("port", reservation.Port))

	return reservation, nil
}

// releaseTabReservation clears the reserved tab in Redis (EG side cleanup)
func (ro *RenderOrchestrator) releaseTabReservation(ctx context.Context, reservation *TabReservation, requestID string, logger *zap.Logger) {
	if reservation == nil {
		return
	}

	tabsKey := fmt.Sprintf("tabs:%s", reservation.ServiceID)
	err := ro.redis.HSet(ctx, tabsKey, fmt.Sprintf("%d", reservation.TabID), "")
	if err != nil {
		logger.Error("Failed to release tab reservation",
			zap.String("request_id", requestID),
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.Error(err))
	} else {
		logger.Debug("Released tab reservation from EG",
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID))
	}
}

// executeRenderWithExplicitServing handles the actual rendering workflow with explicit serving
// staleCache parameter contains stale cache metadata if available, or nil if not
func (ro *RenderOrchestrator) executeRenderWithExplicitServing(renderCtx *edgectx.RenderContext, staleCache *cache.CacheMetadata) (*RenderResult, error) {
	reqCtx, cancel := renderCtx.GetContext()
	defer cancel()

	// Check timeout before selecting render service
	if renderCtx.IsTimedOut() {
		renderCtx.Logger.Warn("Request timeout before service selection",
			zap.Duration("time_remaining", renderCtx.TimeRemaining()))
		ro.lockCoord.ReleaseLock(renderCtx)

		// Try to serve stale cache if available
		if staleCache != nil {
			return ro.serveStaleCache(renderCtx, staleCache, "request_timeout")
		}
		return ro.serveBypass(renderCtx, "request_timeout")
	}

	renderCtx.Logger.Debug("Selecting render service and reserving tab",
		zap.Duration("time_remaining", renderCtx.TimeRemaining()))
	reservation, err := ro.selectServiceAndReserveTab(reqCtx, renderCtx.RequestID, renderCtx.Logger)
	if err != nil || reservation == nil {
		ro.lockCoord.ReleaseLock(renderCtx)

		// Try to serve stale cache if available
		if staleCache != nil {
			renderCtx.Logger.Info("No render services available, serving stale cache")
			return ro.serveStaleCache(renderCtx, staleCache, "no_services")
		}

		renderCtx.Logger.Info("No render services available, serving via bypass")
		return ro.serveBypass(renderCtx, "no_services")
	}

	// Defer cleanup: EG owns tab lifecycle (allocates and deallocates)
	// Lock and tab released on ALL exit paths (success, failure, timeout, bypass)
	// Separate defers for independent panic protection (LIFO: lock released last, tab released first)
	defer ro.lockCoord.ReleaseLock(renderCtx)
	defer ro.releaseTabReservation(context.Background(), reservation, renderCtx.RequestID, renderCtx.Logger)

	// Check timeout before forwarding to render service
	if renderCtx.IsTimedOut() {
		renderCtx.Logger.Warn("Request timeout before render service call",
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.Duration("time_remaining", renderCtx.TimeRemaining()))
		// Lock and tab will be released by defer

		// Try to serve stale cache if available
		if staleCache != nil {
			return ro.serveStaleCache(renderCtx, staleCache, "request_timeout")
		}
		return ro.serveBypass(renderCtx, "request_timeout")
	}

	renderCtx.Logger.Info("Forwarding to render service",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.Duration("time_remaining", renderCtx.TimeRemaining()))
	renderStart := time.Now().UTC()

	// Perform actual render with tab reservation
	renderResult, renderErr := ro.performActualRenderWithTab(renderCtx, reservation)
	if renderErr != nil {
		renderCtx.Logger.Warn("Render service failed",
			zap.String("rs", reservation.ServiceID),
			zap.Error(renderErr))
		// Lock and tab will be released by defer

		// Try to serve stale cache if available
		if staleCache != nil {
			return ro.serveStaleCache(renderCtx, staleCache, "service_failed")
		}
		return ro.serveBypass(renderCtx, "service_failed")
	}

	// Extract values for clarity
	html := renderResult.HTML
	statusCode := renderResult.StatusCode
	redirectLocation := renderResult.RedirectLocation

	// Record successful render duration
	renderDuration := time.Since(renderStart)
	ro.metricsCollector.RecordRenderDuration(renderCtx.Host.Domain, renderCtx.Dimension, reservation.ServiceID, renderDuration)

	// Record status code metrics
	ro.metricsCollector.RecordStatusCodeResponse(renderCtx.Host.Domain, renderCtx.Dimension, statusCode)

	// Check for 5xx responses - serve stale cache instead of caching errors
	if statusCode >= 500 && statusCode < 600 {
		renderCtx.Logger.Warn("Render returned 5xx status code",
			zap.Int("status_code", statusCode))
		// Lock and tab will be released by defer

		// Try to serve stale cache if available
		if staleCache != nil {
			return ro.serveStaleCache(renderCtx, staleCache, "render_5xx_error")
		}
		// No stale cache available - serve the 5xx response to client
		renderCtx.Logger.Info("No stale cache available, serving 5xx response")
	}

	// Check if status code is cacheable (configurable)
	cacheableStatusCodes := renderCtx.ResolvedConfig.Cache.StatusCodes
	shouldCache := renderCtx.ResolvedConfig.Cache.TTL > 0 &&
		isStatusCodeCacheable(statusCode, cacheableStatusCodes)

	if shouldCache {
		if err := ro.cacheCoord.SaveRenderCache(renderCtx, renderResult); err != nil {
			renderCtx.Logger.Error("Failed to save render to cache", zap.Error(err))
			// Continue - we can still serve the response to client
		}
	} else {
		renderCtx.Logger.Info("Skipping cache for status code",
			zap.Int("status_code", statusCode),
			zap.Ints("cacheable_codes", cacheableStatusCodes),
			zap.Duration("cache_ttl", renderCtx.ResolvedConfig.Cache.TTL),
			zap.String("url", renderCtx.TargetURL))

		// Delete stale cache - non-cacheable status indicates state change
		if staleCache != nil {
			ctx, cancel := context.WithTimeout(context.Background(), redisCacheOperationTimeout)
			defer cancel()

			if err := ro.cacheCoord.metadata.DeleteMetadata(ctx, renderCtx.CacheKey); err != nil {
				renderCtx.Logger.Warn("Failed to delete stale metadata after non-cacheable render",
					zap.String("cache_key", renderCtx.CacheKey.String()),
					zap.Error(err))
			} else {
				renderCtx.Logger.Info("Deleted stale cache after non-cacheable render",
					zap.String("cache_key", renderCtx.CacheKey.String()))
			}
		}
	}

	// Serve the rendered content with actual status code
	startTime := time.Now().UTC()
	if err := ro.responseWriter.WriteRenderedResponse(renderCtx, html, statusCode, redirectLocation, reservation.ServiceID, renderResult.Headers); err != nil {
		return nil, err
	}

	// Determine error type based on render result and origin status code
	errorType := renderResult.ErrorType
	errorMessage := renderResult.ErrorMessage
	if errorType == "" {
		// Check for origin errors (4xx/5xx)
		if statusCode >= 400 && statusCode < 500 {
			errorType = types.ErrorTypeOrigin4xx
			errorMessage = fmt.Sprintf("Origin returned %d", statusCode)
		} else if statusCode >= 500 && statusCode < 600 {
			errorType = types.ErrorTypeOrigin5xx
			errorMessage = fmt.Sprintf("Origin returned %d", statusCode)
		}
	}

	duration := time.Since(startTime)
	result := &RenderResult{
		Source:       ServedFromRender,
		ServiceID:    reservation.ServiceID,
		Duration:     duration,
		BytesServed:  int64(len(html)),
		StatusCode:   renderResult.StatusCode,
		PageSEO:      renderResult.PageSEO,
		Metrics:      &renderResult.Metrics,
		ChromeID:     renderResult.ChromeID,
		RenderTime:   renderResult.RenderTime,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
	}

	// Lock and tab will be released by defer AFTER cache write and serving complete
	// This ensures subsequent requests will find the cache entry in Redis
	return result, nil
}

// performActualRenderWithTab communicates with the render service using tab reservation and returns render result with all metrics
func (ro *RenderOrchestrator) performActualRenderWithTab(renderCtx *edgectx.RenderContext, reservation *TabReservation) (*RenderServiceResult, error) {
	serviceURL := fmt.Sprintf("http://%s:%d", reservation.Address, reservation.Port)

	renderCtx.Logger.Debug("Forwarding request to render service with tab reservation",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("request_id", renderCtx.RequestID),
		zap.String("service_url", serviceURL))

	// Get dimension config for viewport
	dimension, exists := renderCtx.Host.Render.Dimensions[renderCtx.Dimension]
	if !exists {
		return nil, fmt.Errorf("dimension '%s' not found in host config", renderCtx.Dimension)
	}

	// Build render request with TabID (use resolved config to respect URL pattern overrides)
	var extraWait time.Duration
	if renderCtx.ResolvedConfig.Render.Events.AdditionalWait != nil {
		extraWait = time.Duration(*renderCtx.ResolvedConfig.Render.Events.AdditionalWait)
	}

	req := &types.RenderRequest{
		RequestID:            renderCtx.RequestID,
		URL:                  renderCtx.TargetURL,
		TabID:                reservation.TabID, // Include reserved tab ID
		ViewportWidth:        dimension.Width,
		ViewportHeight:       dimension.Height,
		UserAgent:            dimension.RenderUA,
		Timeout:              renderCtx.ResolvedConfig.Render.Timeout,              // Use resolved timeout
		WaitFor:              renderCtx.ResolvedConfig.Render.Events.WaitFor,       // Use resolved WaitFor
		ExtraWait:            extraWait,                                            // Use resolved AdditionalWait
		BlockedPatterns:      renderCtx.ResolvedConfig.Render.BlockedPatterns,      // Use resolved patterns (Global → Host → Pattern)
		BlockedResourceTypes: renderCtx.ResolvedConfig.Render.BlockedResourceTypes, // Use resolved resource types (Global → Host → Pattern)
		Headers:              renderCtx.ClientHeaders,                              // Forwarded client request headers
		StripScripts:         renderCtx.ResolvedConfig.Render.StripScripts,         // Strip executable scripts from rendered HTML
	}

	// Call render service with context
	ctx, cancel := renderCtx.GetContext()
	defer cancel()

	resp, err := ro.rsClient.CallRenderService(ctx, serviceURL, req)
	if err != nil {
		renderCtx.Logger.Error("Render service call failed",
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.String("service_url", serviceURL),
			zap.Error(err))
		return nil, fmt.Errorf("render service call failed: %w", err)
	}

	// Check response success
	if !resp.Success {
		renderCtx.Logger.Warn("Render service returned failure",
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.String("error", resp.Error))
		return nil, fmt.Errorf("render failed: %s", resp.Error)
	}

	// Check if status code was captured (0 means failed to capture)
	statusCode := resp.Metrics.StatusCode
	if statusCode == 0 {
		renderCtx.Logger.Warn("Status code not captured by render service, falling back to bypass",
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.String("url", renderCtx.TargetURL))
		return nil, fmt.Errorf("status code not captured")
	}

	// Validate HTML content (allow empty for redirects)
	if len(resp.HTML) == 0 && (statusCode < 300 || statusCode >= 400) {
		return nil, fmt.Errorf("render service returned empty HTML")
	}

	renderCtx.Logger.Info("Render service returned HTML successfully",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.Int("status_code", statusCode),
		zap.Int("html_size", resp.HTMLSize),
		zap.Duration("render_time", resp.RenderTime),
		zap.String("chrome_id", resp.ChromeID))

	return &RenderServiceResult{
		HTML:             []byte(resp.HTML),
		StatusCode:       resp.Metrics.StatusCode,
		RedirectLocation: resp.Metrics.FinalURL,
		RenderTime:       resp.RenderTime,
		ChromeID:         resp.ChromeID,
		Metrics:          resp.Metrics,
		Headers:          resp.Headers,
		HAR:              resp.HAR,
		PageSEO:          resp.PageSEO,
		ErrorType:        resp.ErrorType,
		ErrorMessage:     resp.Error,
	}, nil
}

// serveFromCache serves content from cache (render or bypass) and returns render result
// Unified method that handles both render cache and bypass cache
func (ro *RenderOrchestrator) serveFromCache(renderCtx *edgectx.RenderContext, cacheEntry *cache.CacheMetadata) (*RenderResult, error) {
	startTime := time.Now().UTC()

	// Determine response source based on cache type
	source := ServedFromCache
	if cacheEntry.Source == cache.SourceBypass {
		source = ServedFromBypassCache
	}

	isRedirect := isRedirectStatusCode(cacheEntry.StatusCode)

	if isRedirect {
		// Serve redirect from metadata only (no file read)
		location := ""
		if locations, ok := getHeaderCaseInsensitive(cacheEntry.Headers, "Location"); ok && len(locations) > 0 {
			location = locations[0]
		}
		renderCtx.Logger.Debug("Serving redirect from cache metadata",
			zap.Int("status_code", cacheEntry.StatusCode),
			zap.String("location", location),
			zap.String("source", cacheEntry.Source))

		if err := ro.responseWriter.WriteCachedRedirectResponse(renderCtx, cacheEntry); err != nil {
			return nil, err
		}

		return &RenderResult{
			Source:      source,
			Duration:    time.Since(startTime),
			BytesServed: 0, // No body content
			StatusCode:  cacheEntry.StatusCode,
			CacheAge:    time.Since(cacheEntry.CreatedAt),
		}, nil
	}

	// File-based serving for non-redirects (200, 404, etc.)
	renderCtx.Logger.Debug("Serving from cache file",
		zap.String("file_path", cacheEntry.FilePath),
		zap.Duration("cache_age", time.Since(cacheEntry.CreatedAt)),
		zap.String("source", cacheEntry.Source))

	cacheResp, err := ro.cacheCoord.GetCacheFileForServing(cacheEntry, renderCtx.Logger)
	if err != nil {
		renderCtx.Logger.Error("Failed to prepare cache file",
			zap.String("source", cacheEntry.Source),
			zap.Error(err))
		return nil, fmt.Errorf("failed to prepare cache file: %w", err)
	}

	if err := ro.responseWriter.WriteCacheResponse(renderCtx, cacheEntry, cacheResp); err != nil {
		renderCtx.Logger.Error("Failed to serve cache file to client",
			zap.String("source", cacheEntry.Source),
			zap.Error(err))
		return nil, fmt.Errorf("failed to serve cache file: %w", err)
	}

	// TODO: Cache hit metrics will be handled by ClickHouse integration

	duration := time.Since(startTime)
	return &RenderResult{
		Source:      source,
		Duration:    duration,
		BytesServed: cacheResp.ContentSize,
		StatusCode:  cacheEntry.StatusCode,
		CacheAge:    time.Since(cacheEntry.CreatedAt),
	}, nil
}

// tryPullFromRemoteSmartly attempts to pull cache from remote with smart storage decision
// Returns (RenderResult, true) if successful, (nil, false) if pull failed/not needed
// Uses replicate_on_pull to decide storage: true = store locally, false = memory-only (proxy mode)
// Parameters:
//   - renderCtx: request context
//   - metadata: pre-fetched cache metadata (if nil, will fetch internally)
//   - allowStale: whether to pull stale/expired cache (true for serveStaleCache)
func (ro *RenderOrchestrator) tryPullFromRemoteSmartly(
	renderCtx *edgectx.RenderContext,
	metadata *cache.CacheMetadata,
	allowStale bool,
) (*RenderResult, bool) {
	// Skip pulling if cache is stale unless explicitly allowed
	if metadata.IsExpired() && !allowStale {
		renderCtx.Logger.Debug("Skipping remote pull of stale cache, will render fresh",
			zap.Duration("stale_age", metadata.StaleAge()))
		return nil, false
	}

	// Get eg_ids to analyze replication status
	egIDs := metadata.EgIDs

	startTime := time.Now().UTC()

	// Detect cache source for appropriate response writer
	isBypassCache := metadata.Source == "bypass"

	// Decision logic based on replicate_on_pull setting
	if !renderCtx.ResolvedConfig.Sharding.ReplicateOnPull {
		// Proxy mode: pull to memory only, never store locally
		renderCtx.Logger.Info("Proxy mode enabled, pulling to memory without storing",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Int("remote_replicas", len(egIDs)),
			zap.Bool("is_bypass_cache", isBypassCache))

		content, pulled := ro.cacheCoord.PullFromRemoteToMemory(renderCtx, metadata)
		if !pulled {
			return nil, false
		}

		cacheResp := ro.cacheCoord.GetCacheResponseFromMemory(metadata, content)

		// Use appropriate response writer based on cache source
		var err error
		if isBypassCache {
			err = ro.responseWriter.WriteBypassCacheResponse(renderCtx, metadata, cacheResp)
		} else {
			err = ro.responseWriter.WriteCacheResponse(renderCtx, metadata, cacheResp)
		}

		if err != nil {
			renderCtx.Logger.Error("Failed to serve pulled cache from memory", zap.Error(err))
			return nil, false
		}

		renderCtx.Logger.Info("Successfully pulled to memory and served (proxy mode)",
			zap.Int("content_size", len(content)),
			zap.Duration("duration", time.Since(startTime)))

		source := ServedFromCache
		if isBypassCache {
			source = ServedFromBypassCache
		}

		return &RenderResult{
			Source:      source,
			Duration:    time.Since(startTime),
			BytesServed: int64(len(content)),
			StatusCode:  metadata.StatusCode,
			CacheAge:    time.Since(metadata.CreatedAt),
		}, true
	}

	// replicate_on_pull: true - decide based on hash distribution
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	isTarget, err := ro.cacheCoord.shardingManager.IsTargetForCache(ctx, renderCtx.CacheKey.String())
	if err != nil {
		renderCtx.Logger.Warn("Failed to compute distribution targets, falling back to memory-only pull",
			zap.Error(err))
		isTarget = false
	}

	if isTarget {
		// Current EG is a distribution target - pull AND store (healing OR normal ownership)
		replicationFactor := renderCtx.ResolvedConfig.Sharding.ReplicationFactor
		renderCtx.Logger.Info("Current EG is distribution target for this cache, pulling and storing locally",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Int("current_replicas", len(egIDs)),
			zap.Int("target_replicas", replicationFactor))

		pulledCache, pulled := ro.cacheCoord.TryPullFromRemote(renderCtx, metadata)
		if !pulled {
			return nil, false
		}

		result, err := ro.serveFromCache(renderCtx, pulledCache)
		if err != nil {
			renderCtx.Logger.Warn("Pulled and stored but failed to serve", zap.Error(err))
			return nil, false
		}

		renderCtx.Logger.Info("Successfully pulled, stored, and served (distribution target)",
			zap.Duration("duration", time.Since(startTime)))
		return result, true
	} else {
		// Current EG is NOT a target - pull to memory only (proxy mode, don't store)
		renderCtx.Logger.Info("Current EG is NOT distribution target, pulling to memory only",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Int("replicas", len(egIDs)),
			zap.Bool("is_bypass_cache", isBypassCache))

		content, pulled := ro.cacheCoord.PullFromRemoteToMemory(renderCtx, metadata)
		if !pulled {
			return nil, false
		}

		cacheResp := ro.cacheCoord.GetCacheResponseFromMemory(metadata, content)

		// Use appropriate response writer based on cache source
		var err error
		if isBypassCache {
			err = ro.responseWriter.WriteBypassCacheResponse(renderCtx, metadata, cacheResp)
		} else {
			err = ro.responseWriter.WriteCacheResponse(renderCtx, metadata, cacheResp)
		}

		if err != nil {
			renderCtx.Logger.Error("Failed to serve pulled cache from memory", zap.Error(err))
			return nil, false
		}

		renderCtx.Logger.Info("Successfully pulled to memory and served (non-target EG, no storage)",
			zap.Int("content_size", len(content)),
			zap.Duration("duration", time.Since(startTime)))

		source := ServedFromCache
		if isBypassCache {
			source = ServedFromBypassCache
		}

		return &RenderResult{
			Source:      source,
			Duration:    time.Since(startTime),
			BytesServed: int64(len(content)),
			StatusCode:  metadata.StatusCode,
			CacheAge:    time.Since(metadata.CreatedAt),
		}, true
	}
}

// handleCacheAvailableAfterWait handles cache serving after lock wait completes
// Decides between local serve, pull-and-store, or pull-to-memory based on:
// - ReplicateOnPull configuration
// - Current replication factor vs target
// Returns RenderResult or error (including bypass fallback)
func (ro *RenderOrchestrator) handleCacheAvailableAfterWait(
	renderCtx *edgectx.RenderContext,
	cached *cache.CacheMetadata,
	staleCache *cache.CacheMetadata,
) (*RenderResult, error) {
	// Only serve fresh cache - if stale, use fallback logic
	if !cached.IsFresh() {
		renderCtx.Logger.Debug("Cache appeared during wait but is stale, will not serve it")
		// Try to serve stale cache if available
		if staleCache != nil {
			return ro.serveStaleCache(renderCtx, staleCache, "cache_stale_after_wait")
		}
		return ro.serveBypass(renderCtx, "cache_stale_after_wait")
	}

	// Try local cache first (fast path) - only if current EG owns the file
	if ro.cacheCoord.IsFileLocal(cached) {
		result, err := ro.serveFromCache(renderCtx, cached)
		if err == nil {
			renderCtx.Logger.Info("Served from local cache after lock wait")
			return result, nil
		}
		// File not accessible locally - fall through to pull from remote
	}

	// Try smart pull from remote (use cached metadata from parameter)
	if result, pulled := ro.tryPullFromRemoteSmartly(renderCtx, cached, false); pulled {
		return result, nil
	}

	// Pull failed - try to serve stale cache if available
	if staleCache != nil {
		renderCtx.Logger.Info("Failed to pull cache from remote after wait, trying stale cache")
		return ro.serveStaleCache(renderCtx, staleCache, "remote_pull_failed_after_wait")
	}

	// No stale cache available - fall back to bypass
	renderCtx.Logger.Warn("Failed to pull cache from remote after wait, using bypass")
	return ro.serveBypass(renderCtx, "remote_pull_failed_after_wait")
}

// extractBypassSEO parses HTML bypass response and extracts SEO metadata.
// Returns nil for non-HTML content types or on parse failure.
func extractBypassSEO(body []byte, contentType string, statusCode int, targetURL string, logger *zap.Logger) *types.PageSEO {
	if !strings.Contains(contentType, contentTypeHTML) {
		return nil
	}

	doc, err := htmlprocessor.ParseWithDOM(body)
	if err != nil {
		logger.Warn("Failed to parse bypass HTML for SEO extraction",
			zap.String("url", targetURL),
			zap.Error(err))
		return nil
	}

	return doc.ExtractPageSEO(statusCode, targetURL)
}

// serveBypass proxies the request to origin and returns render result
// Supports bypass caching if enabled in configuration
func (ro *RenderOrchestrator) serveBypass(renderCtx *edgectx.RenderContext, reason string) (*RenderResult, error) {
	startTime := time.Now().UTC()

	renderCtx.Logger.Info("Serving via bypass",
		zap.String("reason", reason),
		zap.String("target_url", renderCtx.TargetURL),
		zap.Bool("bypass_cache_enabled", renderCtx.ResolvedConfig.Bypass.Cache.Enabled))

	// Record bypass metrics
	ro.metricsCollector.RecordBypass(renderCtx.Host.Domain, reason)

	// Check if bypass caching is enabled
	if renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		// 1. CHECK LOCAL CACHE FIRST for bypass entries
		if cached, exists := ro.cacheCoord.LookupCache(renderCtx); exists {
			// Verify it's a bypass cache entry (not render)
			if cached.Source == cache.SourceBypass {
				// Check if cache is fresh
				if cached.IsFresh() {
					// Redirects are metadata-only and accessible via Redis on all EGs
					// Regular content requires file ownership check
					isRedirect := isRedirectStatusCode(cached.StatusCode)

					if isRedirect || ro.cacheCoord.IsFileLocal(cached) {
						result, err := ro.serveFromCache(renderCtx, cached)
						if err == nil {
							renderCtx.Logger.Info("Bypass cache hit (local), served from cache")
							return result, nil
						}
						renderCtx.Logger.Warn("Bypass cache file not accessible locally, will try remote",
							zap.String("file_path", cached.FilePath),
							zap.Error(err))
					}

					// 1.5. TRY PULL FROM REMOTE EGs for bypass cache (if not local and sharding enabled)
					if !cached.IsEmpty() {
						// Use shared smart pull logic (handles bypass cache via metadata.Source)
						if result, pulled := ro.tryPullFromRemoteSmartly(renderCtx, cached, false); pulled {
							return result, nil
						}
					}
				}
				// If expired, fall through to fetch from origin
			}
		}
	}

	// 2. FETCH FROM ORIGIN (cache miss or caching disabled)
	bypassResp, err := ro.bypassSvc.FetchContent(renderCtx.TargetURL, renderCtx.ClientHeaders, renderCtx.Logger)
	if err != nil {
		renderCtx.Logger.Error("Bypass request failed",
			zap.String("target_url", renderCtx.TargetURL),
			zap.Error(err))
		return nil, fmt.Errorf("bypass request failed: %w", err)
	}

	// 2.5. EXTRACT SEO METADATA from HTML responses
	pageSEO := extractBypassSEO(bypassResp.Body, bypassResp.ContentType, bypassResp.StatusCode, renderCtx.TargetURL, renderCtx.Logger)

	// 3. SAVE TO CACHE if enabled, TTL > 0, and status code is cacheable
	if renderCtx.ResolvedConfig.Bypass.Cache.Enabled && renderCtx.ResolvedConfig.Bypass.Cache.TTL > 0 &&
		ro.cacheCoord.IsStatusCodeCacheable(bypassResp.StatusCode, renderCtx.ResolvedConfig.Bypass.Cache.StatusCodes) {
		// CRITICAL: Never overwrite render cache with bypass cache (render cache is higher quality)
		if existing, exists := ro.cacheCoord.LookupCache(renderCtx); exists && existing.Source == cache.SourceRender {
			renderCtx.Logger.Info("Skipping bypass cache save - render cache already exists (higher priority)",
				zap.String("existing_source", existing.Source),
				zap.Int("existing_status", existing.StatusCode),
				zap.Duration("existing_age", time.Since(existing.CreatedAt)))
		} else {
			if err := ro.cacheCoord.SaveBypassCache(renderCtx, bypassResp, pageSEO); err != nil {
				renderCtx.Logger.Error("Failed to save bypass response to cache", zap.Error(err))
				// Continue - we can still serve the response
			}
		}
	} else if renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		if renderCtx.ResolvedConfig.Bypass.Cache.TTL == 0 {
			renderCtx.Logger.Info("Skipping bypass cache (TTL=0 - no caching)")
		} else {
			renderCtx.Logger.Info("Skipping bypass cache for non-cacheable status code",
				zap.Int("status_code", bypassResp.StatusCode),
				zap.Ints("cacheable_codes", renderCtx.ResolvedConfig.Bypass.Cache.StatusCodes))
		}
	}

	// 4. SERVE RESPONSE to client
	if err := ro.responseWriter.WriteBypassResponse(renderCtx, bypassResp); err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	return &RenderResult{
		Source:      ServedFromBypass,
		Duration:    duration,
		BytesServed: int64(len(bypassResp.Body)),
		StatusCode:  bypassResp.StatusCode,
		PageSEO:     pageSEO,
	}, nil
}

// ServeUnmatchedBypass handles bypass for requests with unmatched User-Agent dimension
func (ro *RenderOrchestrator) ServeUnmatchedBypass(renderCtx *edgectx.RenderContext) (*RenderResult, error) {
	renderCtx.Logger.Info("Bypassing render for unmatched User-Agent",
		zap.String("user_agent", string(renderCtx.HTTPCtx.UserAgent())))

	result, err := ro.serveBypass(renderCtx, "unmatched_user_agent")
	if err != nil {
		ro.metricsCollector.RecordError("unmatched_bypass_failed", renderCtx.Host.Domain)
		return nil, err
	}

	// Set unmatched dimension header
	renderCtx.HTTPCtx.Response.Header.Set("X-Unmatched-Dimension", "true")

	return result, nil
}

// isStaleServable checks if stale cache can be served
func (ro *RenderOrchestrator) isStaleServable(
	renderCtx *edgectx.RenderContext,
	cached *cache.CacheMetadata,
) bool {
	// Must have stale strategy configured
	if renderCtx.ResolvedConfig.Cache.Expired.Strategy != types.ExpirationStrategyServeStale {
		return false
	}

	// Must have stale TTL configured
	if renderCtx.ResolvedConfig.Cache.Expired.StaleTTL == nil {
		return false
	}

	// Must be in stale period (not fully expired)
	staleTTL := time.Duration(*renderCtx.ResolvedConfig.Cache.Expired.StaleTTL)
	if !cached.IsStale(staleTTL) {
		return false
	}

	// Status code must be in current cacheable list
	if !isStatusCodeCacheable(cached.StatusCode, renderCtx.ResolvedConfig.Cache.StatusCodes) {
		return false
	}

	return true
}

// serveStaleCache attempts to serve stale cache with fallback to bypass
func (ro *RenderOrchestrator) serveStaleCache(
	renderCtx *edgectx.RenderContext,
	staleCache *cache.CacheMetadata,
	reason string,
) (*RenderResult, error) {
	renderCtx.Logger.Info("Attempting to serve stale cache",
		zap.String("reason", reason),
		zap.Duration("cache_age", time.Since(staleCache.CreatedAt)),
		zap.Duration("stale_age", staleCache.StaleAge()))

	// For redirects, serve directly from metadata (no file on disk)
	if isRedirectStatusCode(staleCache.StatusCode) {
		result, err := ro.serveFromCache(renderCtx, staleCache)
		if err == nil {
			ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension)
			return result, nil
		}
		renderCtx.Logger.Warn("Stale redirect metadata unavailable, falling back to bypass",
			zap.Error(err))
		return ro.serveBypass(renderCtx, "stale_redirect_unavailable")
	}

	// Check if file is local
	if ro.cacheCoord.IsFileLocal(staleCache) {
		result, err := ro.serveFromCache(renderCtx, staleCache)
		if err == nil {
			ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension)
			return result, nil
		}
		renderCtx.Logger.Warn("Stale cache file not accessible locally, trying remote",
			zap.Error(err))
	}

	// Try to pull stale cache from remote EG with smart storage decision
	if result, pulled := ro.tryPullFromRemoteSmartly(renderCtx, staleCache, true); pulled {
		ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension)
		return result, nil
	}

	// Final fallback: bypass
	renderCtx.Logger.Warn("Failed to serve stale cache, falling back to bypass")
	return ro.serveBypass(renderCtx, "stale_unavailable")
}

// RenderWithHAR performs a render request with HAR capture enabled
// This is used by the debug HAR render endpoint
func (ro *RenderOrchestrator) RenderWithHAR(ctx context.Context, req *types.RenderRequest, host *types.Host, dimensionConfig *types.Dimension) (*types.RenderResponse, error) {
	logger := ro.logger.With(
		zap.String("request_id", req.RequestID),
		zap.String("url", req.URL),
		zap.String("host", host.Domain))

	// Reserve tab atomically
	reservation, err := ro.selectServiceAndReserveTab(ctx, req.RequestID, logger)
	if err != nil {
		logger.Error("Failed to reserve render tab", zap.Error(err))
		return nil, fmt.Errorf("no available render capacity: %w", err)
	}

	// Always release tab when done
	defer ro.releaseTabReservation(ctx, reservation, req.RequestID, logger)

	// Build complete render request
	req.TabID = reservation.TabID
	req.IncludeHAR = true
	req.WaitFor = host.Render.Events.WaitFor
	if host.Render.Events.AdditionalWait != nil {
		req.ExtraWait = time.Duration(*host.Render.Events.AdditionalWait)
	}
	req.BlockedPatterns = host.Render.BlockedPatterns
	req.BlockedResourceTypes = host.Render.BlockedResourceTypes

	// Build service URL
	serviceURL := fmt.Sprintf("http://%s:%d", reservation.Address, reservation.Port)

	logger.Debug("Calling render service for HAR",
		zap.String("service_id", reservation.ServiceID),
		zap.String("service_url", serviceURL),
		zap.Int("tab_id", reservation.TabID))

	// Call render service with timeout context
	resp, err := ro.rsClient.CallRenderService(ctx, serviceURL, req)
	if err != nil {
		logger.Error("Render service call failed", zap.Error(err))
		return nil, fmt.Errorf("render failed: %w", err)
	}

	if !resp.Success {
		logger.Error("Render returned failure",
			zap.String("error", resp.Error))
		return nil, fmt.Errorf("render failed: %s", resp.Error)
	}

	logger.Info("HAR render completed",
		zap.Duration("render_time", resp.RenderTime),
		zap.Int("har_size", len(resp.HAR)))

	return resp, nil
}

// HasAvailableCapacity checks if any render service has available capacity
func (ro *RenderOrchestrator) HasAvailableCapacity(ctx context.Context) bool {
	services, err := ro.serviceRegistry.ListHealthyServices(ctx)
	if err != nil || len(services) == 0 {
		return false
	}
	for _, svc := range services {
		if svc.Capacity > svc.Load {
			return true
		}
	}
	return false
}

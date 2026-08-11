package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
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

	// Content processing timeout for bypass path
	contentProcessingTimeout = 5 * time.Second

	// Sharding operation timeouts
	defaultInterEgTimeout = 3 * time.Second // Default timeout for inter-EG operations (push/pull)

	// Bypass cache sharding threshold
	// Responses smaller than this are not replicated (e.g., 301/302 redirects with empty bodies)
	// Metadata (status code, headers) is already in Redis and shared across all EGs
	minBypassBodySizeForReplication = 100 // bytes

	contentTypeHTML = "text/html"
)

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
	StatusCode      int                // HTTP status code
	PageSEO         *types.PageSEO     // Full SEO metadata (nil for cache hits; populated for renders and bypass HTML)
	Metrics         *types.PageMetrics // Page metrics (nil for cache hits)
	CacheAge        time.Duration      // Cache age (for cache hits)
	ChromeID        string             // Chrome instance ID (for renders)
	RenderTime      time.Duration      // Render duration (for renders)
	ErrorType       string             // Structured error category (e.g., "soft_timeout", "origin_4xx")
	ErrorMessage    string             // Detailed error description
	RedirectTo      string             // Redirect target URL (Location header value for 3xx)
	RuleIDs         []uint32           // Content processor rule IDs
	OriginalPageSEO *types.PageSEO     // PageSEO before content processing (nil when unmodified)
	Extraction      json.RawMessage    // Custom extraction output (opaque JSON; populated by EE content processor)
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
	tabSelector      *registry.TabSelector
	rsClient         *rsclient.RSClient
	logger           *zap.Logger
	configManager    configtypes.EGConfigManager

	contentProcessor ContentProcessor // optional, nil = no-op
}

// SetContentProcessor sets an optional content processor for post-render transformations.
func (ro *RenderOrchestrator) SetContentProcessor(cp ContentProcessor) {
	ro.contentProcessor = cp
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
		tabSelector:      registry.NewTabSelector(redisClient, logger),
		rsClient:         rsClient,
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

	// 1. CHECK CACHE FIRST, before branching on action (early optimization - avoids
	// locking for cache hits). A fresh cached record is served regardless of action:
	// for "render" this is the normal early cache hit; for "bypass" it serves a precached
	// render record (mode:render) sitting in the same (host,dimension,url) slot, applying
	// the render-wins precedence to the bypass read path. Stale capture stays render-only;
	// the bypass body keeps its own stale/origin logic for bypass records.
	var staleCache *cache.CacheMetadata
	if cached, exists := ro.cacheCoord.LookupCache(renderCtx); exists {
		if cached.IsFresh() {
			// Bypass action serves only a fresh render-sourced record here; a fresh bypass
			// record falls through to serveBypass so its metrics/stale logic are unchanged.
			if resolved.Action == types.ActionRender || cached.Source == cache.SourceRender {
				// Metadata-only entries (redirects, status overrides) are accessible via Redis on
				// all EGs; regular content requires file ownership check.
				isMetadataOnly := cached.DiskSize == 0

				if isMetadataOnly || ro.cacheCoord.IsFileLocal(cached) {
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
			}
		} else if resolved.Action == types.ActionRender && ro.isStaleServable(renderCtx, cached) {
			// Cache is stale but servable - store for later use if render fails
			staleCache = cached
			renderCtx.Logger.Debug("Stale cache detected, will use as fallback if render fails",
				zap.Duration("stale_age", cached.StaleAge()))
			// Don't serve stale now - attempt fresh render first
		}
	}

	// Handle bypass action (skip rendering): origin fetch + bypass caching. A fresh render
	// record for this URL was already served above, so this is the genuine not-precached path.
	if resolved.Action == types.ActionBypass {
		if renderCtx.DimensionUnmatched {
			return ro.ServeUnmatchedBypass(renderCtx)
		}
		renderCtx.Logger.Info("URL matched bypass rule, fetching from origin directly")
		return ro.serveBypass(renderCtx, "url_rule")
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

// selectServiceAndReserveTab atomically selects service and reserves tab
func (ro *RenderOrchestrator) selectServiceAndReserveTab(ctx context.Context, requestID string, logger *zap.Logger) (*registry.TabReservation, error) {
	// Use independent timeout to prevent race condition from request cancellation
	// This ensures tab reservation always completes or fails atomically
	redisCtx, cancel := context.WithTimeout(context.Background(), redisTabOperationTimeout)
	defer cancel()

	// Get selection strategy from config (default applied in config.applyDefaults())
	strategy := ro.configManager.GetConfig().Registry.SelectionStrategy

	reservation, err := ro.tabSelector.SelectAndReserve(redisCtx, requestID, strategy)
	if err != nil {
		// Saturation and an empty registry are expected states, not faults
		if errors.Is(err, registry.ErrNoServices) || errors.Is(err, registry.ErrNoCapacity) {
			logger.Debug("Service selection failed",
				zap.String("request_id", requestID),
				zap.Error(err))
		} else {
			logger.Error("Service selection failed",
				zap.String("request_id", requestID),
				zap.Error(err))
		}
		return nil, err
	}

	logger.Debug("Selected service and reserved tab",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("address", reservation.Address),
		zap.Int("port", reservation.Port))

	return reservation, nil
}

// releaseTabReservation clears the reserved tab in Redis (EG side cleanup)
func (ro *RenderOrchestrator) releaseTabReservation(ctx context.Context, reservation *registry.TabReservation, requestID string, logger *zap.Logger) {
	if reservation == nil {
		return
	}

	if err := ro.tabSelector.Release(ctx, reservation); err != nil {
		logger.Error("Failed to release tab reservation",
			zap.String("request_id", requestID),
			zap.String("rs", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.Error(err))
		return
	}

	logger.Debug("Released tab reservation from EG",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID))
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
		// No stale cache available - serve the 5xx response directly without content processing
		// Skip ProcessContent to avoid running content processor on error page HTML
		renderCtx.Logger.Info("No stale cache available, serving 5xx response directly")
		startTime5xx := time.Now().UTC()
		if err := ro.responseWriter.WriteRenderedResponse(renderCtx, renderResult.HTML, statusCode, redirectLocation, reservation.ServiceID, renderResult.Headers); err != nil {
			return nil, err
		}
		return &RenderResult{
			Source:       ServedFromRender,
			ServiceID:    reservation.ServiceID,
			Duration:     time.Since(startTime5xx),
			BytesServed:  int64(len(renderResult.HTML)),
			StatusCode:   statusCode,
			ErrorType:    types.ErrorTypeOrigin5xx,
			ErrorMessage: fmt.Sprintf("Origin returned %d", statusCode),
		}, nil
	}

	// Content processing: script cleaning, SEO extraction, and optional content processor.
	// Only HTML responses are processed (mirrors the bypass path content-type guard).
	html := renderResult.HTML
	var processed *ProcessedContent
	var pageSEO *types.PageSEO

	if isHTMLContentType(renderResult.Headers) {
		procCtx, procCancel := context.WithTimeout(context.Background(), contentProcessingTimeout)
		defer procCancel()

		stripScripts := renderCtx.ResolvedConfig.Render.StripScripts
		processed = ProcessContent(
			procCtx,
			renderResult.HTML,
			statusCode,
			renderResult.Headers,
			renderCtx.TargetURL,
			stripScripts,
			renderCtx.Host.ID,
			ro.contentProcessor,
			renderCtx.Logger,
		)
		html = processed.HTML
		pageSEO = processed.PageSEO

		// Handle content processor override (e.g., redirect or status change)
		if processed.Override != nil {
			return ro.serveOverride(renderCtx, processed, overrideParams{
				source:       ServedFromRender,
				cacheSource:  cache.SourceRender,
				cacheTTL:     renderCtx.ResolvedConfig.Cache.TTL,
				staleTTL:     getStaleTTL(renderCtx.ResolvedConfig.Cache.Expired),
				cacheEnabled: true, // render cache has no separate Enabled flag
				startTime:    renderStart,
				serviceID:    reservation.ServiceID,
			})
		}
	}

	// Check if status code is cacheable (configurable)
	cacheableStatusCodes := renderCtx.ResolvedConfig.Cache.StatusCodes
	shouldCache := renderCtx.ResolvedConfig.Cache.TTL > 0 &&
		isStatusCodeCacheable(statusCode, cacheableStatusCodes)

	if shouldCache {
		// Use processed HTML (scripts stripped) for cache, not raw RS HTML
		renderResult.HTML = html
		if err := ro.cacheCoord.SaveRenderCache(renderCtx, renderResult, pageSEO); err != nil {
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

	redirectTo := ""
	if isRedirectStatusCode(renderResult.StatusCode) {
		redirectTo = renderResult.RedirectLocation
	}

	var ruleIDs []uint32
	var originalPageSEO *types.PageSEO
	var extraction json.RawMessage
	if processed != nil {
		ruleIDs = processed.RuleIDs
		originalPageSEO = processed.OriginalPageSEO
		extraction = processed.Extraction
	}

	result := &RenderResult{
		Source:          ServedFromRender,
		ServiceID:       reservation.ServiceID,
		Duration:        duration,
		BytesServed:     int64(len(html)),
		StatusCode:      renderResult.StatusCode,
		PageSEO:         pageSEO,
		Metrics:         &renderResult.Metrics,
		ChromeID:        renderResult.ChromeID,
		RenderTime:      renderResult.RenderTime,
		ErrorType:       errorType,
		ErrorMessage:    errorMessage,
		RedirectTo:      redirectTo,
		RuleIDs:         ruleIDs,
		OriginalPageSEO: originalPageSEO,
		Extraction:      extraction,
	}

	// Lock and tab will be released by defer AFTER cache write and serving complete
	// This ensures subsequent requests will find the cache entry in Redis
	return result, nil
}

// RenderResponseFailureReason identifies which response-validity rule a render service
// response violated. The RS-reported ErrorType is not a usable discriminator (it is empty
// on the status-not-captured and empty-HTML cases), so callers switch on this instead.
type RenderResponseFailureReason int

const (
	RenderFailureNotSuccessful RenderResponseFailureReason = iota
	RenderFailureStatusNotCaptured
	RenderFailureEmptyHTML
)

// RenderResponseFailure describes an unusable render service response.
type RenderResponseFailure struct {
	Reason     RenderResponseFailureReason
	ErrorType  string // RS ErrorType verbatim for RenderFailureNotSuccessful, a types.ErrorType* constant otherwise
	Message    string
	StatusCode int // as reported by the render service; 0 when not captured
}

// ValidateRenderResponse returns the first response-validity violation, or nil when the
// response is usable. Shared by the live render path and recache so the two cannot drift.
// Check order is load-bearing and the helper deliberately does not log: the live path logs
// a different message per reason, and the empty-HTML case logs nothing at all.
func ValidateRenderResponse(resp *types.RenderResponse) *RenderResponseFailure {
	statusCode := resp.Metrics.StatusCode

	if !resp.Success {
		return &RenderResponseFailure{
			Reason:     RenderFailureNotSuccessful,
			ErrorType:  resp.ErrorType,
			Message:    fmt.Sprintf("render failed: %s", resp.Error),
			StatusCode: statusCode,
		}
	}

	if statusCode == 0 {
		return &RenderResponseFailure{
			Reason:    RenderFailureStatusNotCaptured,
			ErrorType: types.ErrorTypeStatusCaptureFailed,
			Message:   "status code not captured",
		}
	}

	// Empty HTML is legitimate for redirects only
	if len(resp.HTML) == 0 && (statusCode < 300 || statusCode >= 400) {
		return &RenderResponseFailure{
			Reason:     RenderFailureEmptyHTML,
			ErrorType:  types.ErrorTypeEmptyResponse,
			Message:    "render service returned empty HTML",
			StatusCode: statusCode,
		}
	}

	return nil
}

// performActualRenderWithTab communicates with the render service using tab reservation and returns render result with all metrics
func (ro *RenderOrchestrator) performActualRenderWithTab(renderCtx *edgectx.RenderContext, reservation *registry.TabReservation) (*RenderServiceResult, error) {
	serviceURL := fmt.Sprintf("http://%s:%d", reservation.Address, reservation.Port)

	renderCtx.Logger.Debug("Forwarding request to render service with tab reservation",
		zap.String("rs", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("request_id", renderCtx.RequestID),
		zap.String("service_url", serviceURL))

	// Get dimension config for viewport
	dimension, exists := renderCtx.Host.Dimensions[renderCtx.Dimension]
	if !exists {
		return nil, fmt.Errorf("dimension '%s' not found in host config", renderCtx.Dimension)
	}

	// Build render request with TabID (use resolved config to respect URL pattern overrides)
	req := BuildRenderRequest(renderCtx.TargetURL, renderCtx.RequestID, renderCtx.Host.RenderKey, reservation.TabID, &renderCtx.ResolvedConfig.Render, &dimension)
	req.Headers = renderCtx.ClientHeaders

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

	if failure := ValidateRenderResponse(resp); failure != nil {
		switch failure.Reason {
		case RenderFailureNotSuccessful:
			renderCtx.Logger.Warn("Render service returned failure",
				zap.String("rs", reservation.ServiceID),
				zap.Int("tab_id", reservation.TabID),
				zap.String("error", resp.Error))
		case RenderFailureStatusNotCaptured:
			renderCtx.Logger.Warn("Status code not captured by render service, falling back to bypass",
				zap.String("rs", reservation.ServiceID),
				zap.Int("tab_id", reservation.TabID),
				zap.String("url", renderCtx.TargetURL))
		}
		return nil, errors.New(failure.Message)
	}

	statusCode := resp.Metrics.StatusCode

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

	// Metadata-only entries: redirects (3xx) and status overrides (404/410)
	if cacheEntry.DiskSize == 0 {
		if err := ro.responseWriter.WriteCachedMetadataResponse(renderCtx, cacheEntry); err != nil {
			return nil, err
		}
		return &RenderResult{
			Source:      source,
			Duration:    time.Since(startTime),
			BytesServed: 0,
			StatusCode:  cacheEntry.StatusCode,
			CacheAge:    time.Since(cacheEntry.CreatedAt),
			PageSEO:     pageSEOFromCacheMetadata(cacheEntry),
			RedirectTo:  redirectLocationFromMetadata(cacheEntry),
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
		PageSEO:     pageSEOFromCacheMetadata(cacheEntry),
	}, nil
}

// pageSEOFromCacheMetadata creates a minimal PageSEO from cache metadata.
// Only Title and IndexStatus are available in Redis cache metadata.
func pageSEOFromCacheMetadata(meta *cache.CacheMetadata) *types.PageSEO {
	if meta.Title == "" && meta.IndexStatus == 0 {
		return nil
	}
	return &types.PageSEO{
		Title:       meta.Title,
		IndexStatus: types.IndexStatus(meta.IndexStatus),
	}
}

// redirectLocationFromMetadata extracts the Location header from cache metadata for redirect responses
func redirectLocationFromMetadata(meta *cache.CacheMetadata) string {
	if !isRedirectStatusCode(meta.StatusCode) {
		return ""
	}
	return LocationHeaderValue(meta.Headers)
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

		err := ro.responseWriter.WriteCacheResponse(renderCtx, metadata, cacheResp)

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
			PageSEO:     pageSEOFromCacheMetadata(metadata),
			RedirectTo:  redirectLocationFromMetadata(metadata),
		}, true
	}

	// replicate_on_pull: true - decide based on hash distribution
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	isTarget, err := ro.cacheCoord.shardingManager.IsTargetForCache(ctx, renderCtx.CacheKey)
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

		err := ro.responseWriter.WriteCacheResponse(renderCtx, metadata, cacheResp)

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
			PageSEO:     pageSEOFromCacheMetadata(metadata),
			RedirectTo:  redirectLocationFromMetadata(metadata),
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

	// Metadata-only entries (redirects, status overrides) don't need file check
	// Regular content requires file ownership check
	if cached.DiskSize == 0 || ro.cacheCoord.IsFileLocal(cached) {
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

func processedRuleIDs(p *ProcessedContent) []uint32 {
	if p == nil {
		return nil
	}
	return p.RuleIDs
}

func processedOriginalPageSEO(p *ProcessedContent) *types.PageSEO {
	if p == nil {
		return nil
	}
	return p.OriginalPageSEO
}

func processedExtraction(p *ProcessedContent) json.RawMessage {
	if p == nil {
		return nil
	}
	return p.Extraction
}

// overrideParams captures the differences between render and bypass override handling.
type overrideParams struct {
	source       ResponseSource
	cacheSource  string
	cacheTTL     time.Duration
	staleTTL     time.Duration
	cacheEnabled bool
	startTime    time.Time
	serviceID    string
}

// serveOverride handles a content processor response override (redirect or status code).
// Caches the override as a metadata-only entry and serves the status response.
func (ro *RenderOrchestrator) serveOverride(
	renderCtx *edgectx.RenderContext,
	processed *ProcessedContent,
	params overrideParams,
) (*RenderResult, error) {
	override := processed.Override

	var overrideHeaders map[string][]string
	if override.Location != "" {
		overrideHeaders = map[string][]string{
			"Location": {override.Location},
		}
	}

	indexStatus := types.IndexStatusIndexable
	if override.StatusCode != 200 {
		indexStatus = types.IndexStatusNon200
	}

	if params.cacheEnabled && params.cacheTTL > 0 {
		if err := ro.cacheCoord.SaveCache(
			renderCtx, nil, override.StatusCode, overrideHeaders,
			params.cacheSource, params.cacheTTL, params.staleTTL,
			false, indexStatus, "",
		); err != nil {
			renderCtx.Logger.Error("Failed to cache override", zap.Error(err))
		}
	}

	ro.responseWriter.WriteStatusResponse(renderCtx, config.ResolvedStatusConfig{
		Code:    override.StatusCode,
		Headers: singleValueHeaders(overrideHeaders),
	})

	return &RenderResult{
		Source:          params.source,
		ServiceID:       params.serviceID,
		Duration:        time.Since(params.startTime),
		BytesServed:     0,
		StatusCode:      override.StatusCode,
		RedirectTo:      override.Location,
		PageSEO:         processed.PageSEO,
		RuleIDs:         processed.RuleIDs,
		OriginalPageSEO: processed.OriginalPageSEO,
		Extraction:      processed.Extraction,
	}, nil
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

	var staleBypassCache *cache.CacheMetadata

	// Check if bypass caching is enabled
	if renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		// 1. CHECK LOCAL CACHE FIRST for bypass entries
		if cached, exists := ro.cacheCoord.LookupCache(renderCtx); exists {
			// Verify it's a bypass cache entry (not render)
			if cached.Source == cache.SourceBypass {
				// Check if cache is fresh
				if cached.IsFresh() {
					// Metadata-only entries (redirects, status overrides) are accessible via Redis on all EGs
					// Regular content requires file ownership check
					isMetadataOnly := cached.DiskSize == 0

					if isMetadataOnly || ro.cacheCoord.IsFileLocal(cached) {
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
				} else if ro.isBypassStaleServable(renderCtx, cached) {
					staleBypassCache = cached
					renderCtx.Logger.Debug("Stale bypass cache detected, will use as fallback if origin fails",
						zap.Duration("stale_age", cached.StaleAge()))
				}
				// If expired, fall through to fetch from origin
			}
		}
	}

	// 2. FETCH FROM ORIGIN (cache miss or caching disabled)
	bypassResp, err := ro.bypassSvc.FetchContent(renderCtx.TargetURL, renderCtx.ClientHeaders, renderCtx.Host.RenderKey, renderCtx.Logger)
	if err != nil {
		if staleBypassCache != nil {
			if result, staleErr := ro.serveStaleBypassCache(renderCtx, staleBypassCache, "origin_error"); staleErr == nil {
				return result, nil
			}
		}
		renderCtx.Logger.Error("Bypass request failed",
			zap.String("target_url", renderCtx.TargetURL),
			zap.Error(err))
		return nil, fmt.Errorf("bypass request failed: %w", err)
	}

	// 2.5. CHECK FOR 5xx - serve stale bypass if available (before content processing)
	if bypassResp.StatusCode >= 500 && staleBypassCache != nil {
		if result, staleErr := ro.serveStaleBypassCache(renderCtx, staleBypassCache, "origin_5xx"); staleErr == nil {
			return result, nil
		}
	}

	// 2.7. EXTRACT SEO METADATA and run content processing on HTML responses
	var pageSEO *types.PageSEO
	var processed *ProcessedContent

	if IsHTMLContentTypeValue(bypassResp.ContentType) {
		procCtx, procCancel := context.WithTimeout(context.Background(), contentProcessingTimeout)
		defer procCancel()

		processed = ProcessContent(
			procCtx,
			bypassResp.Body,
			bypassResp.StatusCode,
			bypassResp.Headers,
			renderCtx.TargetURL,
			false,
			renderCtx.Host.ID,
			ro.contentProcessor,
			renderCtx.Logger,
		)
		pageSEO = processed.PageSEO

		if processed.OriginalPageSEO != nil {
			bypassResp.Body = processed.HTML
		}
	}

	// Handle content processor override
	if processed != nil && processed.Override != nil {
		return ro.serveOverride(renderCtx, processed, overrideParams{
			source:       ServedFromBypass,
			cacheSource:  cache.SourceBypass,
			cacheTTL:     renderCtx.ResolvedConfig.Bypass.Cache.TTL,
			staleTTL:     getStaleTTL(renderCtx.ResolvedConfig.Bypass.Cache.Expired),
			cacheEnabled: renderCtx.ResolvedConfig.Bypass.Cache.Enabled,
			startTime:    startTime,
		})
	}

	// 3. SAVE TO CACHE if all preconditions are met
	if canSave, reason := ro.cacheCoord.CanSaveBypassCache(renderCtx, bypassResp.StatusCode); canSave {
		if err := ro.cacheCoord.SaveBypassCache(renderCtx, bypassResp, pageSEO); err != nil {
			renderCtx.Logger.Error("Failed to save bypass response to cache", zap.Error(err))
		}
	} else if renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		renderCtx.Logger.Info("Skipping bypass cache save",
			zap.String("reason", reason),
			zap.Int("status_code", bypassResp.StatusCode))
	}

	// 4. SERVE RESPONSE to client
	if err := ro.responseWriter.WriteBypassResponse(renderCtx, bypassResp); err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	redirectTo := ""
	if isRedirectStatusCode(bypassResp.StatusCode) {
		redirectTo = LocationHeaderValue(bypassResp.Headers)
	}

	return &RenderResult{
		Source:          ServedFromBypass,
		Duration:        duration,
		BytesServed:     int64(len(bypassResp.Body)),
		StatusCode:      bypassResp.StatusCode,
		PageSEO:         pageSEO,
		RedirectTo:      redirectTo,
		RuleIDs:         processedRuleIDs(processed),
		OriginalPageSEO: processedOriginalPageSEO(processed),
		Extraction:      processedExtraction(processed),
	}, nil
}

// ServeStatusAction handles status actions (redirects, blocks, custom status codes)
func (ro *RenderOrchestrator) ServeStatusAction(renderCtx *edgectx.RenderContext) (*RenderResult, error) {
	resolved := renderCtx.ResolvedConfig

	ro.metricsCollector.RecordBypass(renderCtx.Host.Domain, fmt.Sprintf("status_%d", resolved.Status.Code))
	ro.responseWriter.WriteStatusResponse(renderCtx, resolved.Status)

	redirectTo := ""
	if isRedirectStatusCode(resolved.Status.Code) {
		for k, v := range resolved.Status.Headers {
			if strings.EqualFold(k, "location") {
				redirectTo = v
				break
			}
		}
	}

	return &RenderResult{
		Source:      ServedFromBypass,
		Duration:    time.Millisecond,
		BytesServed: int64(renderCtx.HTTPCtx.Response.Header.ContentLength()),
		StatusCode:  resolved.Status.Code,
		RedirectTo:  redirectTo,
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

	return result, nil
}

func isCacheStaleServable(cached *cache.CacheMetadata, expired types.CacheExpiredConfig, statusCodes []int) bool {
	if expired.Strategy != types.ExpirationStrategyServeStale {
		return false
	}

	if expired.StaleTTL == nil {
		return false
	}

	staleTTL := time.Duration(*expired.StaleTTL)
	if !cached.IsStale(staleTTL) {
		return false
	}

	if !isStatusCodeCacheable(cached.StatusCode, statusCodes) {
		return false
	}

	return true
}

func (ro *RenderOrchestrator) isStaleServable(renderCtx *edgectx.RenderContext, cached *cache.CacheMetadata) bool {
	return isCacheStaleServable(cached, renderCtx.ResolvedConfig.Cache.Expired, renderCtx.ResolvedConfig.Cache.StatusCodes)
}

func (ro *RenderOrchestrator) isBypassStaleServable(renderCtx *edgectx.RenderContext, cached *cache.CacheMetadata) bool {
	return isCacheStaleServable(cached, renderCtx.ResolvedConfig.Bypass.Cache.Expired, renderCtx.ResolvedConfig.Bypass.Cache.StatusCodes)
}

func (ro *RenderOrchestrator) tryServeStaleFromCache(
	renderCtx *edgectx.RenderContext,
	staleCache *cache.CacheMetadata,
	source string,
	reason string,
) (*RenderResult, error) {
	renderCtx.Logger.Info("Attempting to serve stale cache",
		zap.String("reason", reason),
		zap.String("source", source),
		zap.Duration("cache_age", time.Since(staleCache.CreatedAt)),
		zap.Duration("stale_age", staleCache.StaleAge()))

	// Metadata-only entries (redirects, status overrides) don't need file check
	if staleCache.DiskSize == 0 {
		result, err := ro.serveFromCache(renderCtx, staleCache)
		if err == nil {
			ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension, source)
			return result, nil
		}
		return nil, fmt.Errorf("stale metadata-only entry unavailable: %w", err)
	}

	if ro.cacheCoord.IsFileLocal(staleCache) {
		result, err := ro.serveFromCache(renderCtx, staleCache)
		if err == nil {
			ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension, source)
			return result, nil
		}
		renderCtx.Logger.Warn("Stale cache file not accessible locally, trying remote",
			zap.Error(err))
	}

	if result, pulled := ro.tryPullFromRemoteSmartly(renderCtx, staleCache, true); pulled {
		ro.metricsCollector.RecordStaleServed(renderCtx.Host.Domain, renderCtx.Dimension, source)
		return result, nil
	}

	return nil, fmt.Errorf("stale cache unavailable from all sources")
}

func (ro *RenderOrchestrator) serveStaleCache(
	renderCtx *edgectx.RenderContext,
	staleCache *cache.CacheMetadata,
	reason string,
) (*RenderResult, error) {
	result, err := ro.tryServeStaleFromCache(renderCtx, staleCache, "render", reason)
	if err == nil {
		return result, nil
	}

	renderCtx.Logger.Warn("Failed to serve stale cache, falling back to bypass",
		zap.Error(err))
	return ro.serveBypass(renderCtx, "stale_unavailable")
}

func (ro *RenderOrchestrator) serveStaleBypassCache(
	renderCtx *edgectx.RenderContext,
	staleCache *cache.CacheMetadata,
	reason string,
) (*RenderResult, error) {
	result, err := ro.tryServeStaleFromCache(renderCtx, staleCache, "bypass", reason)
	if err == nil {
		return result, nil
	}

	renderCtx.Logger.Warn("Failed to serve stale bypass cache",
		zap.Error(err))
	return nil, fmt.Errorf("stale bypass cache unavailable: %w", err)
}

// resolveRenderConfig resolves render configuration for a URL the same way the live render path
// does, so HAR debug and preview honour URL-rule-level overrides instead of host defaults only.
func (ro *RenderOrchestrator) resolveRenderConfig(targetURL string, host *types.Host) *config.ResolvedRenderConfig {
	globalConfig := ro.configManager.GetConfig()
	resolver := config.NewConfigResolver(&globalConfig.Render, &globalConfig.Bypass, globalConfig.TrackingParams,
		globalConfig.CacheSharding, globalConfig.BothitRecache, globalConfig.Headers, globalConfig.Storage.Compression, host)

	return resolver.ResolveRenderForURL(targetURL)
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

	// The caller owns the timeout: both callers size their own context around the value they
	// passed, so a resolved timeout that is larger would simply be cut off by that deadline.
	callerTimeout := req.Timeout
	applyRenderConfig(req, ro.resolveRenderConfig(req.URL, host))
	if callerTimeout > 0 {
		req.Timeout = callerTimeout
	}

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

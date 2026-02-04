package server

import (
	"context"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/cachedaemon"
	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/common/requestid"
	"github.com/edgecomet/engine/internal/edge/auth"
	"github.com/edgecomet/engine/internal/edge/bot"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/clientip"
	"github.com/edgecomet/engine/internal/edge/device"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/sharding"
	"github.com/edgecomet/engine/pkg/types"
)

type Server struct {
	configManager configtypes.EGConfigManager
	redis         *redis.Client
	keyGenerator  *redis.KeyGenerator
	logger        *zap.Logger

	// Service components
	authService        *auth.AuthenticationService
	deviceDetector     *device.DeviceDetector
	renderOrchestrator *orchestrator.RenderOrchestrator
	metricsCollector   *metrics.MetricsCollector
	shardingManager    *sharding.Manager
	metadataStore      *cache.MetadataStore
	autorecacheClient  *cachedaemon.AutorecacheClient

	// Event logging (nil if disabled)
	eventEmitter events.EventEmitter
	instanceID   string
}

func NewServer(
	configManager configtypes.EGConfigManager,
	redisClient *redis.Client,
	keyGenerator *redis.KeyGenerator,
	logger *zap.Logger,
	authService *auth.AuthenticationService,
	deviceDetector *device.DeviceDetector,
	renderOrchestrator *orchestrator.RenderOrchestrator,
	metricsCollector *metrics.MetricsCollector,
	shardingManager *sharding.Manager,
	metadataStore *cache.MetadataStore,
	autorecacheClient *cachedaemon.AutorecacheClient,
	eventEmitter events.EventEmitter,
	instanceID string,
) *Server {
	return &Server{
		configManager:      configManager,
		redis:              redisClient,
		keyGenerator:       keyGenerator,
		logger:             logger,
		authService:        authService,
		deviceDetector:     deviceDetector,
		renderOrchestrator: renderOrchestrator,
		metricsCollector:   metricsCollector,
		shardingManager:    shardingManager,
		metadataStore:      metadataStore,
		autorecacheClient:  autorecacheClient,
		eventEmitter:       eventEmitter,
		instanceID:         instanceID,
	}
}

func (s *Server) HandleRequest(ctx *fasthttp.RequestCtx) {
	// Extract custom request ID from header (if provided)
	customRequestID := string(ctx.Request.Header.Peek("X-Request-ID"))

	// Generate request ID (with custom ID if provided, otherwise UUID)
	requestID := requestid.GenerateRequestID(customRequestID)

	// Add request ID to response headers for tracing
	ctx.Response.Header.Set("X-Request-ID", requestID)

	logger := s.logger.With(zap.String("request_id", requestID))

	// Handle system endpoints
	switch string(ctx.Path()) {
	case "/health":
		s.handleHealth(ctx)
		return
	case "/ready":
		s.handleReady(ctx)
		return
	case "/render":
		// Validate HTTP method
		if !ctx.IsGet() && !ctx.IsHead() {
			logger.Warn("Method not allowed", zap.String("method", string(ctx.Method())))
			s.writeError(ctx, fasthttp.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Process render request
		if err := s.processRenderRequest(ctx, requestID, logger); err != nil {
			logger.Error("Request processing failed", zap.Error(err))
			// Error handling is done within processRenderRequest
		}
		return
	default:
		logger.Warn("Not found", zap.String("path", string(ctx.Path())))
		s.writeError(ctx, fasthttp.StatusNotFound, "Endpoint not found")
		return
	}
}

// processRenderRequest handles the main render request workflow
func (s *Server) processRenderRequest(ctx *fasthttp.RequestCtx, requestID string, logger *zap.Logger) error {
	start := time.Now()

	// Create render context with timeout from config
	cfg := s.configManager.GetConfig()
	renderCtx := edgectx.NewRenderContext(requestID, ctx, logger, time.Duration(cfg.Server.Timeout))

	// Track active requests
	s.metricsCollector.IncActiveRequests()
	defer s.metricsCollector.DecActiveRequests()

	// Extract and validate URL
	targetURL, err := orchestrator.ExtractURL(renderCtx)
	if err != nil {
		duration := time.Since(start)
		reqErr := &requestError{
			statusCode: fasthttp.StatusBadRequest,
			message:    fmt.Sprintf("Invalid URL: %v", err),
			category:   "invalid_url",
		}
		s.handleRequestError(ctx, renderCtx, err, reqErr, duration)
		return err
	}

	renderCtx.WithTargetURL(targetURL)
	renderCtx.Logger.Info("STARTING Processing render request")

	// Authenticate and get host configuration
	host, err := s.authService.ValidateRenderKey(renderCtx)
	if err != nil {
		duration := time.Since(start)
		reqErr := &requestError{
			statusCode: fasthttp.StatusUnauthorized,
			message:    fmt.Sprintf("Authentication failed: %v", err),
			category:   "auth_failed",
		}
		s.handleRequestError(ctx, renderCtx, err, reqErr, duration)
		return err
	}

	renderCtx.WithHost(host)

	// Extract client IP from configured headers or RemoteAddr
	clientIPHeaders := s.resolveClientIPHeaders(host)
	extractedIP := clientip.Extract(ctx, clientIPHeaders)
	renderCtx.WithClientIP(extractedIP)

	// Detect device dimension (needed for config resolution)
	dimension, dimensionMatched := s.deviceDetector.DetectDimension(renderCtx)
	renderCtx.WithDimension(dimension)

	// Resolve configuration ONCE for this URL (Global → Host → Pattern)
	// This provides the complete merged config for tracking params, cache, render, bypass, sharding, etc.
	globalConfig := s.configManager.GetConfig()
	resolver := config.NewConfigResolver(&globalConfig.Render, &globalConfig.Bypass, globalConfig.TrackingParams, globalConfig.CacheSharding, globalConfig.BothitRecache, globalConfig.Headers, globalConfig.Storage.Compression, host)
	resolved := resolver.ResolveForURL(targetURL)

	// Store resolved config in context for use by orchestrator and cache key generation
	renderCtx.ResolvedConfig = resolved

	// Extract safe client request headers for forwarding to origin
	clientHeaders := ExtractClientHeaders(ctx, resolved.SafeRequestHeaders)
	if clientHeaders != nil {
		renderCtx.WithClientHeaders(clientHeaders)
	}

	// Apply dimension override if specified in URL pattern
	if resolved.Render.Dimension != "" {
		renderCtx.Logger.Debug("Dimension overridden by URL pattern",
			zap.String("detected_dimension", dimension),
			zap.String("override_dimension", resolved.Render.Dimension))
		dimension = resolved.Render.Dimension
		dimensionMatched = true // Pattern override counts as matched
		renderCtx.WithDimension(dimension)
	}

	// Handle unmatched User-Agent according to resolved configuration
	if !dimensionMatched {
		unmatchedBehavior := resolved.Render.UnmatchedDimension

		renderCtx.Logger.Info("User-Agent did not match any dimension pattern",
			zap.String("user_agent", string(ctx.UserAgent())),
			zap.String("unmatched_behavior", unmatchedBehavior))

		switch unmatchedBehavior {
		case types.UnmatchedDimensionBlock:
			// Block the request - return 403 Forbidden
			return s.handleUnmatchedBlock(ctx, renderCtx, start)

		case types.UnmatchedDimensionBypass:
			// Skip rendering entirely - use bypass service
			return s.handleUnmatchedBypass(ctx, renderCtx, start)

		default:
			// Use specified dimension name as fallback
			dimension = s.selectFallbackDimension(renderCtx, unmatchedBehavior, dimension)
			renderCtx.DimensionUnmatched = true
			renderCtx.WithDimension(dimension)
		}
	}

	renderCtx.Logger.Debug("Configuration resolved for URL",
		zap.String("url", targetURL),
		zap.String("action", string(resolved.Action)),
		zap.String("matched_rule", resolved.MatchedRuleID),
		zap.Bool("tracking_params_enabled", resolved.TrackingParams != nil && resolved.TrackingParams.Enabled),
		zap.Bool("sharding_enabled", resolved.Sharding.Enabled),
		zap.Bool("push_on_render", resolved.Sharding.PushOnRender),
		zap.Bool("replicate_on_pull", resolved.Sharding.ReplicateOnPull))

	// Normalize URL (includes tracking param stripping if enabled)
	normalizer := hash.NewURLNormalizer()
	var stripPatterns []config.CompiledStripPattern
	if resolved.TrackingParams != nil && resolved.TrackingParams.Enabled {
		stripPatterns = resolved.TrackingParams.CompiledPatterns
	}

	normalizeResult, err := normalizer.Normalize(targetURL, stripPatterns)
	if err != nil {
		duration := time.Since(start)
		reqErr := &requestError{
			statusCode: fasthttp.StatusBadRequest,
			message:    fmt.Sprintf("Failed to normalize URL: %v", err),
			category:   "url_normalization_error",
		}
		s.handleRequestError(ctx, renderCtx, err, reqErr, duration)
		return err
	}

	// Set X-Processed-URL header
	renderCtx.HTTPCtx.Response.Header.Set("X-Processed-URL", normalizeResult.NormalizedURL)

	// Log if tracking parameters were stripped
	if normalizeResult.WasModified {
		renderCtx.Logger.Info("Tracking parameters stripped",
			zap.String("original_url", normalizeResult.OriginalURL),
			zap.String("normalized_url", normalizeResult.NormalizedURL),
			zap.Strings("stripped_params", normalizeResult.StrippedParams))
	}

	// Hash normalized URL
	urlHash := normalizer.Hash(normalizeResult.NormalizedURL)

	// Store in context
	renderCtx.WithProcessedURL(normalizeResult.NormalizedURL).WithURLHash(urlHash)

	// Generate cache key and lock key from context data
	// Safety check: dimension must exist in host configuration
	dimConfig, exists := host.Render.Dimensions[dimension]
	if !exists {
		duration := time.Since(start)
		renderCtx.Logger.Error("Dimension not found in host configuration",
			zap.String("dimension", dimension),
			zap.String("host", host.Domain))
		reqErr := &requestError{
			statusCode: fasthttp.StatusInternalServerError,
			message:    "Configuration error: dimension not found",
			category:   "invalid_dimension",
		}
		s.handleRequestError(ctx, renderCtx, fmt.Errorf("dimension '%s' not found in host configuration", dimension), reqErr, duration)
		return fmt.Errorf("dimension '%s' not found in host configuration", dimension)
	}

	dimensionID := dimConfig.ID
	cacheKey := s.keyGenerator.GenerateCacheKey(host.ID, dimensionID, renderCtx.URLHash)
	lockKey := s.keyGenerator.GenerateLockKey(cacheKey)

	renderCtx.WithCacheKey(cacheKey).WithLockKey(lockKey)

	// Process render request through orchestrator (handles cache, rendering, fallback)
	result, err := s.renderOrchestrator.ProcessRenderRequest(renderCtx)
	if err != nil {
		duration := time.Since(start)
		reqErr := &requestError{
			statusCode: fasthttp.StatusInternalServerError,
			message:    "Internal server error",
			category:   "render_error",
		}
		s.handleRequestError(ctx, renderCtx, err, reqErr, duration)
		return err
	}

	// Bot detection on cache hit (automatic recache scheduling)
	if result.Source == orchestrator.ServedFromCache && renderCtx.ResolvedConfig.BothitRecache.Enabled {
		userAgent := string(ctx.Request.Header.Peek("User-Agent"))
		if bot.IsBotRequest(userAgent, &renderCtx.ResolvedConfig.BothitRecache) {
			now := time.Now().UTC()

			// Update last_bot_hit in cache metadata
			if err := s.metadataStore.UpdateLastBotHit(ctx, renderCtx.CacheKey, now); err != nil {
				renderCtx.Logger.Error("Failed to update last_bot_hit",
					zap.String("cache_key", renderCtx.CacheKey.String()),
					zap.Error(err))
				// Non-fatal error, continue
			}

			// Schedule autorecache
			interval := renderCtx.ResolvedConfig.BothitRecache.Interval
			scheduledAt := now.Add(interval)
			if err := s.autorecacheClient.ScheduleAutorecache(ctx, renderCtx.Host.ID, renderCtx.TargetURL, renderCtx.CacheKey.DimensionID, scheduledAt); err != nil {
				renderCtx.Logger.Error("Failed to schedule autorecache",
					zap.Error(err))
				// Non-fatal error, continue serving the response
			} else {
				renderCtx.Logger.Debug("Bot detected on cache hit, autorecache scheduled",
					zap.String("user_agent", userAgent))
			}
		}
	}

	// Record metrics and get source string
	duration := time.Since(start)
	sourceStr := s.recordResultMetrics(renderCtx, result, duration)

	// Emit request event for access logging
	if s.eventEmitter != nil {
		event := events.BuildRequestEvent(renderCtx, result, duration, s.instanceID)
		s.eventEmitter.Emit(event)
	}

	renderCtx.Logger.Info("END Request completed successfully",
		zap.String("source", sourceStr),
		zap.String("service_id", result.ServiceID),
		zap.Duration("process_duration", result.Duration),
		zap.Int64("bytes_served", result.BytesServed))

	return nil
}

func (s *Server) handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Content-Type", "text/plain")
	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.SetBodyString("OK")
}

func (s *Server) handleReady(ctx *fasthttp.RequestCtx) {
	reqCtx := context.Background()

	if err := s.redis.HealthCheck(reqCtx); err != nil {
		s.writeError(ctx, fasthttp.StatusServiceUnavailable, "Redis not available")
		return
	}

	// TODO: Implement service registry health check
	// services, err := s.registry.ListHealthyServices(reqCtx)
	// if err != nil {
	//     s.writeError(ctx, fasthttp.StatusServiceUnavailable, "Service registry unavailable")
	//     return
	// }
	services := []interface{}{} // Placeholder

	ctx.Response.Header.Set("Content-Type", "text/plain")
	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.SetBodyString(fmt.Sprintf("OK - %d render services available", len(services)))
}

func (s *Server) writeError(ctx *fasthttp.RequestCtx, statusCode int, message string) {
	ctx.Response.Header.Set("Content-Type", "text/plain")
	ctx.Response.SetStatusCode(statusCode)
	ctx.Response.SetBodyString(message)
}

func (s *Server) resolveClientIPHeaders(host *types.Host) []string {
	if host != nil && host.ClientIP != nil {
		return host.ClientIP.Headers
	}
	cfg := s.configManager.GetConfig()
	if cfg.ClientIP != nil {
		return cfg.ClientIP.Headers
	}
	return nil
}

// Shutdown gracefully shuts down the server and closes resources
func (s *Server) Shutdown() error {
	if s.eventEmitter != nil {
		if err := s.eventEmitter.Close(); err != nil {
			s.logger.Warn("Failed to close event emitter", zap.Error(err))
			return err
		}
	}
	return nil
}

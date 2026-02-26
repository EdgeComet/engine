package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/clientip"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
)

// requestError represents an error with HTTP status code and metrics category
type requestError struct {
	statusCode int
	message    string
	category   string
}

// handleRequestError writes error response, logs, records metrics, and emits error event
func (s *Server) handleRequestError(ctx *fasthttp.RequestCtx, renderCtx *edgectx.RenderContext, err error, reqErr *requestError, duration time.Duration) {
	renderCtx.Logger.Warn("Request failed", zap.Error(err), zap.String("category", reqErr.category))
	s.writeError(ctx, reqErr.statusCode, reqErr.message)

	domain := ""
	hostID := 0
	dimension := ""
	if renderCtx.Host != nil {
		domain = renderCtx.Host.Domain
		hostID = renderCtx.Host.ID
	}
	if renderCtx.Dimension != "" {
		dimension = renderCtx.Dimension
	}

	s.metricsCollector.RecordRequest(domain, dimension, reqErr.category, duration)
	if reqErr.category != "invalid_url" {
		s.metricsCollector.RecordError(reqErr.category, domain)
	}

	// Emit error event for access logging
	if s.eventEmitter != nil {
		eventClientIP := renderCtx.ClientIP
		if eventClientIP == "" {
			cfg := s.configManager.GetConfig()
			var globalHeaders []string
			if cfg.ClientIP != nil {
				globalHeaders = cfg.ClientIP.Headers
			}
			eventClientIP = clientip.Extract(ctx, globalHeaders)
		}
		event := events.BuildErrorEvent(
			renderCtx.RequestID,
			domain,
			hostID,
			renderCtx.TargetURL,
			string(ctx.UserAgent()),
			eventClientIP,
			reqErr.category,
			err.Error(),
			reqErr.statusCode,
			s.instanceID,
		)
		s.eventEmitter.Emit(event)
	}
}

// handleUnmatchedBlock blocks request with 403 Forbidden
func (s *Server) handleUnmatchedBlock(ctx *fasthttp.RequestCtx, renderCtx *edgectx.RenderContext, start time.Time) error {
	duration := time.Since(start)

	renderCtx.Logger.Warn("Blocking request - User-Agent not supported",
		zap.String("user_agent", string(ctx.UserAgent())))

	ctx.Response.Header.Set("X-Unmatched-Dimension", "true")
	s.writeError(ctx, fasthttp.StatusForbidden, "User-Agent not supported for rendering")
	s.metricsCollector.RecordRequest(renderCtx.Host.Domain, "", "unmatched_blocked", duration)
	s.metricsCollector.RecordError("unmatched_user_agent", renderCtx.Host.Domain)

	// Emit error event for access logging
	if s.eventEmitter != nil {
		event := events.BuildErrorEvent(
			renderCtx.RequestID,
			renderCtx.Host.Domain,
			renderCtx.Host.ID,
			renderCtx.TargetURL,
			string(ctx.UserAgent()),
			renderCtx.ClientIP,
			"unmatched_blocked",
			"User-Agent not supported for rendering",
			fasthttp.StatusForbidden,
			s.instanceID,
		)
		s.eventEmitter.Emit(event)
	}

	return fmt.Errorf("user-agent not supported")
}

// handleUnmatchedBypass serves content via bypass service for unmatched User-Agent
func (s *Server) handleUnmatchedBypass(ctx *fasthttp.RequestCtx, renderCtx *edgectx.RenderContext, start time.Time) error {
	result, err := s.renderOrchestrator.ServeUnmatchedBypass(renderCtx)
	if err != nil {
		duration := time.Since(start)
		s.writeError(ctx, fasthttp.StatusBadGateway, "Failed to fetch content")
		s.metricsCollector.RecordRequest(renderCtx.Host.Domain, "", "unmatched_bypass_failed", duration)

		// Emit error event for access logging
		if s.eventEmitter != nil {
			event := events.BuildErrorEvent(
				renderCtx.RequestID,
				renderCtx.Host.Domain,
				renderCtx.Host.ID,
				renderCtx.TargetURL,
				string(ctx.UserAgent()),
				renderCtx.ClientIP,
				"unmatched_bypass_failed",
				err.Error(),
				fasthttp.StatusBadGateway,
				s.instanceID,
			)
			s.eventEmitter.Emit(event)
		}
		return err
	}

	s.metricsCollector.RecordRequest(renderCtx.Host.Domain, "", "unmatched_bypass", result.Duration)

	// Emit request event for successful unmatched bypass
	if s.eventEmitter != nil {
		duration := time.Since(start)
		event := events.BuildRequestEvent(renderCtx, result, duration, s.instanceID)
		s.eventEmitter.Emit(event)
	}

	renderCtx.Logger.Info("Successfully bypassed for unmatched User-Agent",
		zap.Duration("duration", result.Duration),
		zap.Int64("bytes_served", result.BytesServed))

	return nil
}

// selectFallbackDimension selects fallback dimension for unmatched User-Agent
func (s *Server) selectFallbackDimension(renderCtx *edgectx.RenderContext, unmatchedBehavior, detectedDimension string) string {
	// Check if configured fallback dimension exists
	if _, exists := renderCtx.Host.Render.Dimensions[unmatchedBehavior]; !exists {
		// Fallback dimension doesn't exist - configuration error
		renderCtx.Logger.Error("Configured unmatched_dimension not found in host dimensions",
			zap.String("configured", unmatchedBehavior),
			zap.String("host", renderCtx.Host.Domain),
			zap.String("detector_fallback", detectedDimension))
		// Return detector fallback (may be empty string)
		// Runtime safety check at dimension lookup will catch empty value
		return detectedDimension
	}

	renderCtx.Logger.Info("Using configured fallback dimension for unmatched User-Agent",
		zap.String("fallback_dimension", unmatchedBehavior))

	return unmatchedBehavior
}

// recordResultMetrics records metrics based on render result source
func (s *Server) recordResultMetrics(renderCtx *edgectx.RenderContext, result *orchestrator.RenderResult, duration time.Duration) string {
	host := renderCtx.Host
	dimension := renderCtx.Dimension

	var sourceStr string
	switch result.Source {
	case orchestrator.ServedFromCache:
		sourceStr = "cache"
		s.metricsCollector.RecordCacheHit(host.Domain, dimension)
		s.metricsCollector.RecordRequest(host.Domain, dimension, "cache_hit", duration)

	case orchestrator.ServedFromRender:
		sourceStr = "render"
		s.metricsCollector.RecordCacheMiss(host.Domain, dimension)
		s.metricsCollector.RecordRequest(host.Domain, dimension, "success", duration)

	case orchestrator.ServedFromBypass:
		sourceStr = "bypass"
		s.metricsCollector.RecordCacheMiss(host.Domain, dimension)
		s.metricsCollector.RecordRequest(host.Domain, dimension, "success", duration)

	default:
		sourceStr = "unknown"
		s.metricsCollector.RecordRequest(host.Domain, dimension, "success", duration)
	}

	return sourceStr
}

// ExtractClientHeaders extracts safe request headers from the client request.
// Uses case-insensitive matching per RFC 7230.
// Returns nil if no headers match or safeRequestHeaders is empty.
func ExtractClientHeaders(ctx *fasthttp.RequestCtx, safeRequestHeaders []string) map[string][]string {
	if len(safeRequestHeaders) == 0 {
		return nil
	}

	// Build lowercase lookup map for case-insensitive matching
	safeHeadersLower := make(map[string]bool, len(safeRequestHeaders))
	for _, header := range safeRequestHeaders {
		safeHeadersLower[strings.ToLower(header)] = true
	}

	headers := make(map[string][]string)

	// Iterate through all request headers
	for key, value := range ctx.Request.Header.All() {
		headerName := string(key)
		headerLower := strings.ToLower(headerName)

		if safeHeadersLower[headerLower] {
			// Preserve original header name case, collect all values
			headers[headerName] = append(headers[headerName], string(value))
		}
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}

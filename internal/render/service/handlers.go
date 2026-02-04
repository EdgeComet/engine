package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/render/chrome"
	"github.com/edgecomet/engine/internal/render/metrics"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status             string `json:"status"`
	PoolSize           int    `json:"pool_size"`
	AvailableInstances int    `json:"available_instances"`
	ActiveInstances    int    `json:"active_instances"`
}

// writeBinaryResponse writes metadata (JSON) + HTML (raw) in length-prefixed format
func writeBinaryResponse(ctx *fasthttp.RequestCtx, renderResp *types.RenderResponse, path string, metricsCollector *metrics.MetricsCollector, logger *zap.Logger) {
	// Extract metadata (without HTML)
	metadata := types.RenderResponseMetadata{
		RequestID:  renderResp.RequestID,
		Success:    renderResp.Success,
		Error:      renderResp.Error,
		ErrorType:  renderResp.ErrorType,
		RenderTime: renderResp.RenderTime,
		HTMLSize:   renderResp.HTMLSize,
		Timestamp:  renderResp.Timestamp,
		ChromeID:   renderResp.ChromeID,
		Metrics:    renderResp.Metrics,
		Headers:    renderResp.Headers,
		HAR:        renderResp.HAR,
		PageSEO:    renderResp.PageSEO,
	}

	// Marshal metadata to JSON
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		// Fallback to error response
		writeErrorResponse(ctx, fasthttp.StatusInternalServerError, "Failed to marshal metadata", renderResp.RequestID, path, metricsCollector, "internal", "", logger)
		logger.Error("Failed to marshal metadata",
			zap.String("request_id", renderResp.RequestID),
			zap.Error(err))
		return
	}

	// Calculate response size
	metadataLen := uint32(len(metadataJSON))
	totalSize := 4 + int(metadataLen) + len(renderResp.HTML)

	// Prepare response buffer
	responseBody := make([]byte, totalSize)

	// Write length prefix (big-endian uint32)
	binary.BigEndian.PutUint32(responseBody[0:4], metadataLen)

	// Write metadata JSON
	copy(responseBody[4:4+metadataLen], metadataJSON)

	// Write raw HTML
	copy(responseBody[4+metadataLen:], []byte(renderResp.HTML))

	// Set response
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(responseBody)
	ctx.SetContentType("application/octet-stream")
	metricsCollector.RecordHTTPRequest(path, "200")

	logger.Debug("Sent binary response",
		zap.String("request_id", renderResp.RequestID),
		zap.Int("metadata_bytes", len(metadataJSON)),
		zap.Int("html_bytes", len(renderResp.HTML)),
		zap.Int("total_bytes", totalSize))
}

// writeJSONResponse writes a JSON response with proper error handling
func writeJSONResponse(ctx *fasthttp.RequestCtx, statusCode int, response interface{}, path string, metricsCollector *metrics.MetricsCollector, logger *zap.Logger) {
	body, err := json.Marshal(response)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(`{"success":false,"error":"Failed to marshal response"}`)
		ctx.SetContentType("application/json")
		metricsCollector.RecordHTTPRequest(path, "500")
		logger.Error("Failed to marshal JSON response",
			zap.String("path", path),
			zap.Error(err))
		return
	}

	ctx.SetStatusCode(statusCode)
	ctx.SetBody(body)
	ctx.SetContentType("application/json")
	metricsCollector.RecordHTTPRequest(path, fmt.Sprintf("%d", statusCode))
}

// writeErrorResponse writes an error response with consistent formatting
// errorCategory is for metrics (validation, internal, render)
// structuredErrorType is the types.ErrorType* constant for the event system
func writeErrorResponse(ctx *fasthttp.RequestCtx, statusCode int, errorMsg string, requestID string, path string, metricsCollector *metrics.MetricsCollector, errorCategory string, structuredErrorType string, logger *zap.Logger) {
	resp := types.RenderResponse{
		RequestID: requestID,
		Success:   false,
		Error:     errorMsg,
		ErrorType: structuredErrorType,
		Timestamp: time.Now().UTC(),
	}

	writeJSONResponse(ctx, statusCode, resp, path, metricsCollector, logger)

	// Record specific error metrics
	switch errorCategory {
	case "validation":
		metricsCollector.RecordValidationError()
	case "internal":
		metricsCollector.RecordInternalError()
	case "render":
		metricsCollector.RecordRenderError()
		metricsCollector.RecordRenderErrorMetric()
	}
}

// HandleRender processes POST /render requests
func HandleRender(ctx *fasthttp.RequestCtx, pool *chrome.ChromePool, tabManager *registry.TabManager, metricsCollector *metrics.MetricsCollector, logger *zap.Logger, renderConfig *config.RSRenderConfig) {
	startTime := time.Now().UTC()

	// Parse request body
	var req types.RenderRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		writeErrorResponse(ctx, fasthttp.StatusBadRequest, "Invalid JSON body", "", "/render", metricsCollector, "validation", types.ErrorTypeInvalidURL, logger)
		logger.Warn("Invalid request body",
			zap.String("url", string(ctx.RequestURI())),
			zap.Error(err))
		return
	}

	// Validate required fields
	if req.URL == "" {
		writeErrorResponse(ctx, fasthttp.StatusBadRequest, "url field is required", req.RequestID, "/render", metricsCollector, "validation", types.ErrorTypeInvalidURL, logger)
		return
	}

	if req.RequestID == "" {
		writeErrorResponse(ctx, fasthttp.StatusBadRequest, "request_id field is required", "", "/render", metricsCollector, "validation", types.ErrorTypeInvalidURL, logger)
		logger.Warn("Missing request_id in render request")
		return
	}

	// Enforce maximum timeout (EG always provides timeout from host config)
	renderTimeout := req.Timeout
	if renderTimeout > time.Duration(renderConfig.MaxTimeout) {
		renderTimeout = time.Duration(renderConfig.MaxTimeout)
		req.Timeout = time.Duration(renderConfig.MaxTimeout)
	}

	// Validate tab reservation and extend TTL
	if req.TabID < 0 || req.TabID >= tabManager.GetPoolSize() {
		errorMsg := fmt.Sprintf("Invalid tab_id: %d (pool size: %d)", req.TabID, tabManager.GetPoolSize())
		writeErrorResponse(ctx, fasthttp.StatusBadRequest, errorMsg, req.RequestID, "/render", metricsCollector, "validation", types.ErrorTypeInvalidURL, logger)
		logger.Error("Invalid tab_id",
			zap.String("request_id", req.RequestID),
			zap.Int("tab_id", req.TabID))
		return
	}

	// Extend TTL for render duration + safety margin
	renderTTL := renderTimeout + 5*time.Second
	if err := tabManager.ExtendTTL(ctx, renderTTL); err != nil {
		logger.Warn("Failed to extend tabs TTL",
			zap.String("request_id", req.RequestID),
			zap.Duration("ttl", renderTTL),
			zap.Error(err))
		// Continue - not critical
	}

	logger.Info("Starting render request",
		zap.String("request_id", req.RequestID),
		zap.String("url", req.URL),
		zap.Int("tab_id", req.TabID),
		zap.Duration("timeout", renderTimeout))

	// Acquire Chrome instance
	instance, err := pool.AcquireChrome(req.RequestID)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to acquire instance: %v", err)
		// Categorize pool errors using sentinel errors
		poolErrorType := types.ErrorTypePoolUnavailable
		if errors.Is(err, chrome.ErrInstanceDead) {
			poolErrorType = types.ErrorTypeChromeCrash
		} else if errors.Is(err, chrome.ErrRestartFailed) {
			poolErrorType = types.ErrorTypeChromeRestartFailed
		} else if errors.Is(err, chrome.ErrPoolShutdown) {
			poolErrorType = types.ErrorTypePoolUnavailable
		}
		writeErrorResponse(ctx, fasthttp.StatusServiceUnavailable, errorMsg, req.RequestID, "/render", metricsCollector, "internal", poolErrorType, logger)
		logger.Error("Acquisition failed",
			zap.String("request_id", req.RequestID),
			zap.Int("tab_id", req.TabID),
			zap.Error(err))
		return
	}

	// Always release Chrome instance back to pool
	// Note: Tab cleanup handled by Edge Gateway (single ownership model)
	defer func() {
		pool.ReleaseChrome(instance)
	}()

	// Create hard timeout context (max_timeout enforces hard limit)
	hardTimeout := time.Duration(renderConfig.MaxTimeout)
	renderCtx, renderCancel := context.WithTimeout(context.Background(), hardTimeout)
	defer renderCancel()

	// Perform rendering with timeout
	renderResp, renderErr := instance.Render(renderCtx, &req)

	duration := time.Since(startTime).Seconds()
	metricsCollector.RecordRenderDuration(duration)

	// Handle hard timeout (check context first, as it's the source of timeout)
	if renderCtx.Err() == context.DeadlineExceeded {
		errorMsg := fmt.Sprintf("Hard timeout exceeded (%v)", hardTimeout)
		writeErrorResponse(ctx, fasthttp.StatusGatewayTimeout, errorMsg, req.RequestID, "/render", metricsCollector, "render", types.ErrorTypeHardTimeout, logger)
		metricsCollector.RecordRenderHardTimeout()
		logger.Error("Render hard timeout",
			zap.String("request_id", req.RequestID),
			zap.String("url", req.URL),
			zap.Int("instance_id", instance.ID),
			zap.Duration("hard_timeout", hardTimeout),
			zap.Float64("duration", duration))
		return
	}

	if renderErr != nil {
		// Rendering error (navigation failed, etc.) - use ErrorType from response if available
		errorMsg := fmt.Sprintf("Rendering failed: %v", renderErr)
		errorType := renderResp.ErrorType
		if errorType == "" {
			errorType = types.ErrorTypeNavigationFailed // Default for render errors
		}
		writeErrorResponse(ctx, fasthttp.StatusOK, errorMsg, req.RequestID, "/render", metricsCollector, "render", errorType, logger)
		logger.Error("Render failed",
			zap.String("request_id", req.RequestID),
			zap.String("url", req.URL),
			zap.Error(renderErr))
		return
	}

	// Success (may have timed out but still returned HTML)
	writeBinaryResponse(ctx, renderResp, "/render", metricsCollector, logger)

	// Track as soft timeout if navigation wait exceeded, otherwise success
	if renderResp.Metrics.TimedOut {
		metricsCollector.RecordRenderTimeout()
		logger.Warn("Render completed with soft timeout",
			zap.String("request_id", req.RequestID),
			zap.String("instance_id", renderResp.ChromeID),
			zap.String("url", req.URL),
			zap.Float64("duration", duration),
			zap.Float64("render_time", renderResp.RenderTime.Seconds()),
			zap.Int("html_bytes", renderResp.HTMLSize),
			zap.Int("status_code", renderResp.Metrics.StatusCode),
			zap.Int("lifecycle_events", len(renderResp.Metrics.LifecycleEvents)),
			zap.Bool("timed_out", renderResp.Metrics.TimedOut))
	} else {
		metricsCollector.RecordRenderSuccess()
		logger.Info("Render successful",
			zap.String("request_id", req.RequestID),
			zap.String("instance_id", renderResp.ChromeID),
			zap.String("url", req.URL),
			zap.Float64("duration", duration),
			zap.Float64("render_time", renderResp.RenderTime.Seconds()),
			zap.Int("html_bytes", renderResp.HTMLSize),
			zap.Int("status_code", renderResp.Metrics.StatusCode),
			zap.Int("lifecycle_events", len(renderResp.Metrics.LifecycleEvents)),
			zap.Bool("timed_out", renderResp.Metrics.TimedOut))
	}
}

// HandleHealth returns the current health status and pool statistics
func HandleHealth(ctx *fasthttp.RequestCtx, pool *chrome.ChromePool, metricsCollector *metrics.MetricsCollector, logger *zap.Logger) {
	stats := pool.GetStats()

	resp := HealthResponse{
		Status:             "ok",
		PoolSize:           stats.TotalInstances,
		AvailableInstances: stats.AvailableInstances,
		ActiveInstances:    stats.ActiveInstances,
	}

	writeJSONResponse(ctx, fasthttp.StatusOK, resp, "/health", metricsCollector, logger)
}

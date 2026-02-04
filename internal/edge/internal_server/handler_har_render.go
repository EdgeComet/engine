package internal_server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/requestid"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	tabPollingInterval = 500 * time.Millisecond
	tabMaxWaitTime     = 10 * time.Second
)

// HARRenderOrchestrator defines the interface for render orchestration
type HARRenderOrchestrator interface {
	HasAvailableCapacity(ctx context.Context) bool
	RenderWithHAR(ctx context.Context, req *types.RenderRequest, host *types.Host, dimensionConfig *types.Dimension) (*types.RenderResponse, error)
}

// HARRenderHandler handles HAR debug render requests
type HARRenderHandler struct {
	configManager configtypes.EGConfigManager
	orchestrator  HARRenderOrchestrator
	logger        *zap.Logger
}

// NewHARRenderHandler creates a new HAR render handler
func NewHARRenderHandler(configManager configtypes.EGConfigManager, orchestrator HARRenderOrchestrator, logger *zap.Logger) *HARRenderHandler {
	return &HARRenderHandler{
		configManager: configManager,
		orchestrator:  orchestrator,
		logger:        logger,
	}
}

// RegisterEndpoints registers the HAR render handler with the internal server
func (h *HARRenderHandler) RegisterEndpoints(server *InternalServer) {
	server.RegisterHandler("GET", PathDebugHARRender, h.handleHARRender)
}

// harRenderParams holds parsed request parameters
type harRenderParams struct {
	URL       *url.URL
	Dimension string
	Timeout   time.Duration // 0 means use host config default
}

// handleHARRender handles HAR debug render requests
// GET /debug/har/render?url={targetURL}&dimension={dimID}&timeout={duration}
func (h *HARRenderHandler) handleHARRender(ctx *fasthttp.RequestCtx) {
	params, err := h.parseAndValidateParams(ctx)
	if err != nil {
		return
	}

	h.logger.Debug("HAR render request validated",
		zap.String("url", params.URL.String()),
		zap.String("dimension", params.Dimension),
		zap.Duration("timeout", params.Timeout))

	// Find host by domain
	host, err := h.findHostByDomain(ctx, params.URL.Hostname())
	if err != nil {
		return
	}

	// Resolve dimension
	dimension, err := h.resolveDimension(ctx, host, params.Dimension)
	if err != nil {
		return
	}

	h.logger.Debug("Host and dimension resolved",
		zap.String("host_domain", host.Domain),
		zap.Int("host_id", host.ID),
		zap.String("dimension", dimension))

	// Check URL rules
	if err := h.checkURLRules(ctx, host, params.URL.String()); err != nil {
		return
	}

	// Wait for available render tab
	if err := h.waitForAvailableTab(ctx); err != nil {
		return
	}

	// Get dimension config
	dimConfig := host.Render.Dimensions[dimension]

	// Resolve timeout
	timeout := params.Timeout
	if timeout == 0 {
		timeout = time.Duration(host.Render.Timeout)
	}

	// Build render request with basic fields
	// Orchestrator will add TabID, WaitFor, BlockedPatterns, etc.
	requestID := requestid.GenerateRequestID("har-debug")
	req := &types.RenderRequest{
		RequestID:      requestID,
		URL:            params.URL.String(),
		ViewportWidth:  dimConfig.Width,
		ViewportHeight: dimConfig.Height,
		UserAgent:      dimConfig.RenderUA,
		Timeout:        timeout,
	}

	h.logger.Debug("Sending render request",
		zap.String("request_id", requestID),
		zap.String("url", req.URL),
		zap.Int("viewport_width", req.ViewportWidth),
		zap.Int("viewport_height", req.ViewportHeight),
		zap.Duration("timeout", req.Timeout))

	// Create timeout context for render operation
	renderCtx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()

	// Call render service through orchestrator
	resp, err := h.orchestrator.RenderWithHAR(renderCtx, req, host, &dimConfig)
	if err != nil {
		h.logger.Error("Render service call failed",
			zap.String("request_id", requestID),
			zap.Error(err))
		httputil.JSONError(ctx, "render_failed: "+err.Error(), fasthttp.StatusBadGateway)
		return
	}

	// Check if render was successful
	if !resp.Success {
		h.logger.Error("Render failed",
			zap.String("request_id", requestID),
			zap.String("error", resp.Error))
		httputil.JSONError(ctx, "render_failed: "+resp.Error, fasthttp.StatusBadGateway)
		return
	}

	h.logger.Info("HAR render completed",
		zap.String("request_id", requestID),
		zap.Duration("render_time", resp.RenderTime),
		zap.Int("har_size", len(resp.HAR)))

	// Return raw HAR JSON
	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.SetContentType("application/json")
	ctx.Response.SetBody(resp.HAR)
}

// waitForAvailableTab polls for available render capacity
func (h *HARRenderHandler) waitForAvailableTab(ctx *fasthttp.RequestCtx) error {
	deadline := time.Now().Add(tabMaxWaitTime)
	pollCount := 0

	for time.Now().Before(deadline) {
		pollCount++
		if h.orchestrator.HasAvailableCapacity(context.Background()) {
			h.logger.Debug("Render tab available",
				zap.Int("poll_count", pollCount))
			return nil
		}
		time.Sleep(tabPollingInterval)
	}

	h.logger.Warn("No render tabs available after timeout",
		zap.Duration("timeout", tabMaxWaitTime),
		zap.Int("poll_count", pollCount))

	httputil.JSONError(ctx, "no_tabs_available: No render tabs available after 10s timeout",
		fasthttp.StatusServiceUnavailable)
	return errValidation
}

// checkURLRules checks if the URL matches any non-render rules
func (h *HARRenderHandler) checkURLRules(ctx *fasthttp.RequestCtx, host *types.Host, targetURL string) error {
	if len(host.URLRules) == 0 {
		return nil
	}

	matcher := config.NewPatternMatcher(host.URLRules)
	matchedRule, _ := matcher.FindMatchingRule(targetURL)

	if matchedRule == nil {
		// No rule matched, default is render
		return nil
	}

	action := matchedRule.Action
	if action == "" || action == types.ActionRender {
		// Render action, proceed
		return nil
	}

	h.logger.Debug("URL matched non-render rule",
		zap.String("url", targetURL),
		zap.String("action", string(action)),
		zap.Any("pattern", matchedRule.Match))

	switch action {
	case types.ActionBlock, types.ActionStatus403:
		httputil.JSONError(ctx, "url_blocked: URL matched block rule", fasthttp.StatusForbidden)
		return errValidation

	case types.ActionStatus404:
		httputil.JSONError(ctx, "url_status: URL matched status_404 rule", fasthttp.StatusNotFound)
		return errValidation

	case types.ActionStatus410:
		httputil.JSONError(ctx, "url_status: URL matched status_410 rule", fasthttp.StatusGone)
		return errValidation

	case types.ActionStatus:
		statusCode := fasthttp.StatusInternalServerError
		if matchedRule.Status != nil && matchedRule.Status.Code != nil {
			statusCode = *matchedRule.Status.Code
		}
		msg := fmt.Sprintf("url_status: URL matched status rule (code %d)", statusCode)
		httputil.JSONError(ctx, msg, statusCode)
		return errValidation

	case types.ActionBypass:
		httputil.JSONError(ctx, "url_bypass: Bypass rules not supported in debug endpoint", fasthttp.StatusBadRequest)
		return errValidation

	default:
		// Unknown action, treat as error
		msg := fmt.Sprintf("url_unknown_action: Unknown action '%s'", action)
		httputil.JSONError(ctx, msg, fasthttp.StatusBadRequest)
		return errValidation
	}
}

// findHostByDomain finds a host configuration by domain name
// Uses O(1) domain lookup that matches any domain in host.Domains array
func (h *HARRenderHandler) findHostByDomain(ctx *fasthttp.RequestCtx, hostname string) (*types.Host, error) {
	normalizedHostname := strings.ToLower(hostname)

	host := h.configManager.GetHostByDomain(normalizedHostname)
	if host != nil && host.Enabled {
		return host, nil
	}

	// Build available hosts list only on error path
	hosts := h.configManager.GetHosts()
	var availableHosts []string
	for _, h := range hosts {
		if h.Enabled {
			availableHosts = append(availableHosts, h.Domain)
		}
	}
	sort.Strings(availableHosts)

	h.logger.Warn("Host not found",
		zap.String("hostname", normalizedHostname),
		zap.Strings("available_hosts", availableHosts))

	msg := fmt.Sprintf("host_not_found: No host configuration for domain '%s'. Available: %v",
		normalizedHostname, availableHosts)
	httputil.JSONError(ctx, msg, fasthttp.StatusNotFound)
	return nil, errValidation
}

// resolveDimension resolves and validates the dimension for a host
func (h *HARRenderHandler) resolveDimension(ctx *fasthttp.RequestCtx, host *types.Host, requestedDimension string) (string, error) {
	// Use requested dimension or fall back to default
	dimension := requestedDimension
	if dimension == "" {
		// Check if UnmatchedDimension is a valid dimension
		if _, exists := host.Render.Dimensions[host.Render.UnmatchedDimension]; exists {
			dimension = host.Render.UnmatchedDimension
		} else {
			// UnmatchedDimension is not a valid dimension (e.g., "block")
			// Return error requiring explicit dimension
			availableDims := make([]string, 0, len(host.Render.Dimensions))
			for name := range host.Render.Dimensions {
				availableDims = append(availableDims, name)
			}
			sort.Strings(availableDims)

			h.logger.Warn("Invalid default dimension configured",
				zap.String("host", host.Domain),
				zap.String("unmatched_dimension", host.Render.UnmatchedDimension),
				zap.Strings("available_dimensions", availableDims))

			msg := fmt.Sprintf("invalid_dimension: Default dimension '%s' not found for host '%s'. Available: %v",
				host.Render.UnmatchedDimension, host.Domain, availableDims)
			httputil.JSONError(ctx, msg, fasthttp.StatusBadRequest)
			return "", errValidation
		}
	}

	// Validate dimension exists
	if _, exists := host.Render.Dimensions[dimension]; !exists {
		availableDims := make([]string, 0, len(host.Render.Dimensions))
		for name := range host.Render.Dimensions {
			availableDims = append(availableDims, name)
		}
		sort.Strings(availableDims)

		h.logger.Warn("Invalid dimension",
			zap.String("dimension", dimension),
			zap.String("host", host.Domain),
			zap.Strings("available_dimensions", availableDims))

		msg := fmt.Sprintf("invalid_dimension: Dimension '%s' not found for host '%s'. Available: %v",
			dimension, host.Domain, availableDims)
		httputil.JSONError(ctx, msg, fasthttp.StatusBadRequest)
		return "", errValidation
	}

	return dimension, nil
}

// parseAndValidateParams parses and validates query parameters
func (h *HARRenderHandler) parseAndValidateParams(ctx *fasthttp.RequestCtx) (*harRenderParams, error) {
	args := ctx.QueryArgs()

	// Parse URL (required)
	urlStr := string(args.Peek("url"))
	if urlStr == "" {
		h.logger.Warn("Missing URL parameter")
		httputil.JSONError(ctx, "missing_url: URL parameter is required", fasthttp.StatusBadRequest)
		return nil, errValidation
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		h.logger.Warn("Invalid URL format",
			zap.String("url", urlStr),
			zap.Error(err))
		httputil.JSONError(ctx, "invalid_url: "+err.Error(), fasthttp.StatusBadRequest)
		return nil, errValidation
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		h.logger.Warn("Invalid URL: missing scheme or host",
			zap.String("url", urlStr))
		httputil.JSONError(ctx, "invalid_url: URL must have scheme and host", fasthttp.StatusBadRequest)
		return nil, errValidation
	}

	// Parse dimension (optional)
	dimension := string(args.Peek("dimension"))

	// Parse timeout (optional)
	var timeout time.Duration
	timeoutStr := string(args.Peek("timeout"))
	if timeoutStr != "" {
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			h.logger.Warn("Invalid timeout format",
				zap.String("timeout", timeoutStr),
				zap.Error(err))
			httputil.JSONError(ctx, "invalid_timeout: "+err.Error(), fasthttp.StatusBadRequest)
			return nil, errValidation
		}
		if timeout < 0 {
			h.logger.Warn("Negative timeout value",
				zap.String("timeout", timeoutStr))
			httputil.JSONError(ctx, "invalid_timeout: timeout must be positive", fasthttp.StatusBadRequest)
			return nil, errValidation
		}
	}

	return &harRenderParams{
		URL:       parsedURL,
		Dimension: dimension,
		Timeout:   timeout,
	}, nil
}

// errValidation is a sentinel error for validation failures
var errValidation = &validationError{}

type validationError struct{}

func (e *validationError) Error() string {
	return "validation error"
}

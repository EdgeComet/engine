package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

// ResponseWriter handles all HTTP response writing operations
// Pure HTTP writing with no business logic or metrics
type ResponseWriter struct {
	// No dependencies needed - pure HTTP writing
}

// NewResponseWriter creates a new ResponseWriter instance
func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{}
}

// WriteRenderedResponse writes freshly rendered content to HTTP response
func (rw *ResponseWriter) WriteRenderedResponse(renderCtx *edgectx.RenderContext, html []byte, statusCode int, redirectLocation string, serviceID string, headers map[string][]string) error {
	renderCtx.Logger.Info("Serving rendered content",
		zap.String("service_id", serviceID),
		zap.Int("status_code", statusCode),
		zap.Int("content_size", len(html)))

	// Set actual status code from rendered page
	renderCtx.HTTPCtx.Response.SetStatusCode(statusCode)

	// Set Location header for redirects (backwards compatibility)
	if statusCode >= 300 && statusCode < 400 && redirectLocation != "" {
		renderCtx.HTTPCtx.Response.Header.Set("Location", redirectLocation)
	}

	// Serve headers from rendered page (filter using safe_response_headers config)
	filteredHeaders := FilterHeaders(headers, renderCtx.ResolvedConfig.SafeResponseHeaders, statusCode, false)
	for name, values := range filteredHeaders {
		// Skip Location as it is handled explicitly above using redirectLocation
		if strings.EqualFold(name, "Location") {
			continue
		}
		for _, value := range values {
			renderCtx.HTTPCtx.Response.Header.Add(name, value)
		}
	}

	// Set response headers
	renderCtx.HTTPCtx.Response.Header.Set("Content-Type", "text/html; charset=utf-8")
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Source", "rendered")
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Service", serviceID)
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Cache", "new")
	renderCtx.HTTPCtx.Response.Header.SetContentLength(len(html))

	// Set X-Unmatched-Dimension header if fallback dimension was used
	if renderCtx.DimensionUnmatched {
		renderCtx.HTTPCtx.Response.Header.Set("X-Unmatched-Dimension", "true")
	}

	// Set matched rule header if available
	if renderCtx.ResolvedConfig != nil && renderCtx.ResolvedConfig.MatchedRuleID != "" {
		renderCtx.HTTPCtx.Response.Header.Set("X-Matched-Rule", renderCtx.ResolvedConfig.MatchedRuleID)
	}

	// Close connection to prevent client hang
	renderCtx.HTTPCtx.Response.SetConnectionClose()

	// Serve the content
	renderCtx.HTTPCtx.Response.SetBody(html)

	return nil
}

// WriteBypassResponse writes the bypass response to the HTTP context
func (rw *ResponseWriter) WriteBypassResponse(renderCtx *edgectx.RenderContext, bypassResp *bypass.BypassResponse) error {
	// Set status code
	renderCtx.HTTPCtx.Response.SetStatusCode(bypassResp.StatusCode)

	// Set content type
	renderCtx.HTTPCtx.Response.Header.Set("Content-Type", bypassResp.ContentType)

	// Set bypass-specific headers
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Source", "bypass")
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Cache", "bypass")

	// Set matched rule header if available
	if renderCtx.ResolvedConfig != nil && renderCtx.ResolvedConfig.MatchedRuleID != "" {
		renderCtx.HTTPCtx.Response.Header.Set("X-Matched-Rule", renderCtx.ResolvedConfig.MatchedRuleID)
	}

	// Always preserve Location for redirects (essential for proper redirect behavior)
	// Case-insensitive lookup per RFC 7230 (origin may send "location" lowercase)
	if bypassResp.StatusCode >= 300 && bypassResp.StatusCode < 400 {
		if locations, ok := getHeaderCaseInsensitive(bypassResp.Headers, "Location"); ok && len(locations) > 0 {
			renderCtx.HTTPCtx.Response.Header.Set("Location", locations[0])
		}
	}

	// Serve headers from bypass response (filter using safe_response_headers config)
	filteredHeaders := FilterHeaders(bypassResp.Headers, renderCtx.ResolvedConfig.SafeResponseHeaders, bypassResp.StatusCode, false)
	for name, values := range filteredHeaders {
		// Skip Location as it is handled explicitly above
		if strings.EqualFold(name, "Location") {
			continue
		}
		for _, value := range values {
			renderCtx.HTTPCtx.Response.Header.Add(name, value)
		}
	}

	// Set content length
	renderCtx.HTTPCtx.Response.Header.SetContentLength(len(bypassResp.Body))

	// Close connection to prevent client hang
	renderCtx.HTTPCtx.Response.SetConnectionClose()

	// Set body
	renderCtx.HTTPCtx.Response.SetBody(bypassResp.Body)

	return nil
}

// WriteCacheResponse writes cached content to the HTTP context
// Supports both render cache and bypass cache
// Supports both file-based (FilePath) and memory-based (Content) serving
func (rw *ResponseWriter) WriteCacheResponse(renderCtx *edgectx.RenderContext, cacheEntry *cache.CacheMetadata, cacheResp *cache.CacheResponse) error {
	// Set status code
	renderCtx.HTTPCtx.Response.SetStatusCode(cacheEntry.StatusCode)

	// Set content type based on cache source
	if cacheEntry.Source == "bypass" {
		// Bypass cache: use content type from cached headers or default
		contentType := "text/html; charset=utf-8"
		if ct, exists := cacheEntry.Headers["Content-Type"]; exists && len(ct) > 0 {
			contentType = ct[0]
		}
		renderCtx.HTTPCtx.Response.Header.Set("Content-Type", contentType)
	} else {
		// Render cache: always text/html
		renderCtx.HTTPCtx.Response.Header.Set("Content-Type", "text/html; charset=utf-8")
	}

	// Set cache headers (source, hit/stale, age, matched rule)
	rw.setCacheHeaders(renderCtx, cacheEntry, cacheResp.CacheAge)

	// Serve headers from cache (re-filter against current config for security)
	filteredHeaders := FilterHeaders(cacheEntry.Headers, renderCtx.ResolvedConfig.SafeResponseHeaders, cacheEntry.StatusCode, false)
	for name, values := range filteredHeaders {
		for _, value := range values {
			renderCtx.HTTPCtx.Response.Header.Add(name, value)
		}
	}

	// Close connection to prevent client hang
	renderCtx.HTTPCtx.Response.SetConnectionClose()

	// Serve based on response type
	if cacheResp.IsMemoryBased() {
		// Memory-based serving: content in memory
		renderCtx.HTTPCtx.Response.Header.SetContentLength(len(cacheResp.Content))
		renderCtx.HTTPCtx.Response.SetBody(cacheResp.Content)

		renderCtx.Logger.Debug("Served cache from memory",
			zap.String("source", cacheEntry.Source),
			zap.Int("content_size", len(cacheResp.Content)))

		return nil
	} else if cacheResp.IsFileBased() {
		// File-based serving: efficient file streaming
		err := renderCtx.HTTPCtx.Response.SendFile(cacheResp.FilePath)
		if err != nil {
			renderCtx.Logger.Error("Failed to serve cache file",
				zap.String("source", cacheEntry.Source),
				zap.String("file_path", cacheResp.FilePath),
				zap.Error(err))
			return fmt.Errorf("failed to send cache file: %w", err)
		}

		return nil
	} else {
		// Invalid state: neither file nor memory mode
		return fmt.Errorf("invalid cache response: neither FilePath nor Content is set")
	}
}

// WriteCachedMetadataResponse writes a metadata-only cached response (no file on disk).
// Handles both redirects (3xx with Location) and status overrides (404/410 from content processor).
// Sets proper cache headers: X-Render-Source, X-Render-Cache, X-Cache-Age.
func (rw *ResponseWriter) WriteCachedMetadataResponse(renderCtx *edgectx.RenderContext, cacheEntry *cache.CacheMetadata) error {
	renderCtx.Logger.Debug("Serving cached metadata-only response",
		zap.Int("status_code", cacheEntry.StatusCode),
		zap.String("source", cacheEntry.Source))

	// Set status code
	renderCtx.HTTPCtx.Response.SetStatusCode(cacheEntry.StatusCode)

	// Set Location header from metadata (for redirects)
	if locations, ok := getHeaderCaseInsensitive(cacheEntry.Headers, "Location"); ok && len(locations) > 0 {
		renderCtx.HTTPCtx.Response.Header.Set("Location", locations[0])
	}

	// Serve other headers from cache (re-filter against current config for security)
	filteredHeaders := FilterHeaders(cacheEntry.Headers, renderCtx.ResolvedConfig.SafeResponseHeaders, cacheEntry.StatusCode, false)
	for name, values := range filteredHeaders {
		if strings.EqualFold(name, "Location") {
			continue
		}
		for _, value := range values {
			renderCtx.HTTPCtx.Response.Header.Add(name, value)
		}
	}

	// Set cache headers (source, hit/stale, age, matched rule)
	cacheAge := renderCtx.HTTPCtx.Time().Sub(cacheEntry.CreatedAt)
	rw.setCacheHeaders(renderCtx, cacheEntry, cacheAge)

	// Body: empty for redirects, status text for 4xx/5xx
	if isRedirectStatusCode(cacheEntry.StatusCode) {
		renderCtx.HTTPCtx.Response.SetBodyString("")
	} else {
		renderCtx.HTTPCtx.Response.Header.Set("Content-Type", "text/plain; charset=utf-8")
		renderCtx.HTTPCtx.Response.SetBodyString(fasthttp.StatusMessage(cacheEntry.StatusCode))
	}

	renderCtx.HTTPCtx.Response.SetConnectionClose()

	return nil
}

// WriteStatusResponse writes a status action response (3xx, 4xx, 5xx)
func (rw *ResponseWriter) WriteStatusResponse(renderCtx *edgectx.RenderContext, statusConfig config.ResolvedStatusConfig) error {
	renderCtx.Logger.Info("URL matched status rule, returning status",
		zap.Int("status_code", statusConfig.Code),
		zap.String("reason", statusConfig.Reason))

	// Set status code
	renderCtx.HTTPCtx.Response.SetStatusCode(statusConfig.Code)

	// Set default headers
	renderCtx.HTTPCtx.Response.Header.Set("Content-Type", "text/plain; charset=utf-8")
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Action", "status")

	// Set matched rule header if available
	if renderCtx.ResolvedConfig != nil && renderCtx.ResolvedConfig.MatchedRuleID != "" {
		renderCtx.HTTPCtx.Response.Header.Set("X-Matched-Rule", renderCtx.ResolvedConfig.MatchedRuleID)
	}

	// Apply custom headers (can override defaults including Content-Type)
	for key, value := range statusConfig.Headers {
		renderCtx.HTTPCtx.Response.Header.Set(key, value)
	}

	// Generate response body based on status code class
	statusClass := statusConfig.Code / 100

	switch statusClass {
	case 3: // 3xx Redirects
		// For redirects, no body is needed (most clients ignore it)
		// Body is intentionally empty for redirect responses
		renderCtx.HTTPCtx.Response.SetBodyString("")

	case 4, 5: // 4xx and 5xx Errors
		// Get status text (e.g., "Not Found", "Forbidden", "Internal Server Error")
		statusText := fasthttp.StatusMessage(statusConfig.Code)

		var body string
		if statusConfig.Reason != "" {
			body = fmt.Sprintf("%s: %s", statusText, statusConfig.Reason)
		} else {
			body = statusText
		}

		renderCtx.HTTPCtx.Response.SetBodyString(body)

	default:
		// For other status codes (should not happen with validation), use status text
		renderCtx.HTTPCtx.Response.SetBodyString(fasthttp.StatusMessage(statusConfig.Code))
	}

	renderCtx.HTTPCtx.Response.SetConnectionClose()

	return nil
}

// setCacheHeaders sets common cache response headers: X-Render-Source, X-Render-Cache (hit/stale),
// X-Cache-Age, and X-Matched-Rule.
func (rw *ResponseWriter) setCacheHeaders(renderCtx *edgectx.RenderContext, cacheEntry *cache.CacheMetadata, cacheAge time.Duration) {
	if cacheEntry.Source == "bypass" {
		renderCtx.HTTPCtx.Response.Header.Set("X-Render-Source", "bypass_cache")
	} else {
		renderCtx.HTTPCtx.Response.Header.Set("X-Render-Source", "cache")
	}

	cacheStatus := "hit"
	if cacheEntry.IsExpired() {
		expiredConfig := getExpiredConfigForSource(renderCtx, cacheEntry.Source)
		if expiredConfig.StaleTTL != nil {
			staleTTL := time.Duration(*expiredConfig.StaleTTL)
			if cacheEntry.IsStale(staleTTL) {
				cacheStatus = "stale"
			}
		}
	}
	renderCtx.HTTPCtx.Response.Header.Set("X-Render-Cache", cacheStatus)
	renderCtx.HTTPCtx.Response.Header.Set("X-Cache-Age", fmt.Sprintf("%d", int(cacheAge.Seconds())))

	if renderCtx.ResolvedConfig != nil && renderCtx.ResolvedConfig.MatchedRuleID != "" {
		renderCtx.HTTPCtx.Response.Header.Set("X-Matched-Rule", renderCtx.ResolvedConfig.MatchedRuleID)
	}
}

func getExpiredConfigForSource(renderCtx *edgectx.RenderContext, source string) types.CacheExpiredConfig {
	if source == cache.SourceBypass {
		return renderCtx.ResolvedConfig.Bypass.Cache.Expired
	}
	return renderCtx.ResolvedConfig.Cache.Expired
}

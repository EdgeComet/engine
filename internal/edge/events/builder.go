package events

import (
	"time"

	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

// Event type constants
const (
	EventTypeCacheHit    = "cache_hit"
	EventTypeRender      = "render"
	EventTypeBypass      = "bypass"
	EventTypeBypassCache = "bypass_cache"
	EventTypePrecache    = "precache"
	EventTypeError       = "error"
)

// Source constants
const (
	SourceCache       = "cache"
	SourceRender      = "render"
	SourceBypass      = "bypass"
	SourceBypassCache = "bypass_cache"
)

// Error type constants are defined in pkg/types for shared use:
// types.ErrorTypeHardTimeout, types.ErrorTypeChromeCrash, etc.

// BuildRequestEvent creates a RequestEvent from request context and result
func BuildRequestEvent(
	renderCtx *edgectx.RenderContext,
	result *orchestrator.RenderResult,
	duration time.Duration,
	egInstanceID string,
) *RequestEvent {
	event := &RequestEvent{
		CreatedAt:    time.Now().UTC(),
		EGInstanceID: egInstanceID,
		ServeTime:    duration.Seconds(),
	}

	// Populate from RenderContext
	if renderCtx != nil {
		event.RequestID = renderCtx.RequestID
		event.URL = renderCtx.TargetURL
		event.URLHash = renderCtx.URLHash
		event.Dimension = renderCtx.Dimension
		if renderCtx.HTTPCtx != nil {
			event.UserAgent = string(renderCtx.HTTPCtx.UserAgent())
		}
		event.ClientIP = renderCtx.ClientIP

		if renderCtx.Host != nil {
			event.Host = renderCtx.Host.Domain
			event.HostID = renderCtx.Host.ID
		}

		if renderCtx.CacheKey != nil {
			event.CacheKey = renderCtx.CacheKey.String()
		}

		// Get matched rule from resolved config
		if renderCtx.ResolvedConfig != nil {
			event.MatchedRule = renderCtx.ResolvedConfig.MatchedPattern
		}
	}

	// Populate from RenderResult
	if result != nil {
		event.StatusCode = result.StatusCode
		event.PageSize = result.BytesServed
		event.RenderServiceID = result.ServiceID
		event.ChromeID = result.ChromeID
		event.RenderTime = result.RenderTime.Seconds()
		event.CacheAge = int(result.CacheAge.Seconds())
		event.ErrorType = result.ErrorType
		event.ErrorMessage = result.ErrorMessage

		// Map ResponseSource to EventType and Source
		event.EventType, event.Source = mapResponseSource(result.Source)

		// Convert PageMetrics if present
		if result.Metrics != nil {
			event.Metrics = convertPageMetrics(result.Metrics)
		}

		// Convert PageSEO if present (render events only)
		if result.PageSEO != nil {
			event.PageSEO = convertPageSEO(result.PageSEO)
		}
	}

	// Override EventType for precache requests
	if renderCtx != nil && renderCtx.IsPrecache {
		event.EventType = EventTypePrecache
	}

	return event
}

// BuildErrorEvent creates an error event for early failures (auth, validation, etc.)
func BuildErrorEvent(
	requestID string,
	host string,
	hostID int,
	url string,
	userAgent string,
	clientIP string,
	errorType string,
	errorMessage string,
	statusCode int,
	egInstanceID string,
) *RequestEvent {
	return &RequestEvent{
		RequestID:    requestID,
		Host:         host,
		HostID:       hostID,
		URL:          url,
		UserAgent:    userAgent,
		ClientIP:     clientIP,
		EventType:    EventTypeError,
		StatusCode:   statusCode,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		CreatedAt:    time.Now().UTC(),
		EGInstanceID: egInstanceID,
	}
}

// mapResponseSource converts orchestrator.ResponseSource to event type and source strings
func mapResponseSource(source orchestrator.ResponseSource) (eventType, sourceStr string) {
	switch source {
	case orchestrator.ServedFromCache:
		return EventTypeCacheHit, SourceCache
	case orchestrator.ServedFromRender:
		return EventTypeRender, SourceRender
	case orchestrator.ServedFromBypass:
		return EventTypeBypass, SourceBypass
	case orchestrator.ServedFromBypassCache:
		return EventTypeBypassCache, SourceBypassCache
	default:
		return EventTypeBypass, SourceBypass
	}
}

// convertPageMetrics converts types.PageMetrics to PageMetricsEvent
func convertPageMetrics(metrics *types.PageMetrics) *PageMetricsEvent {
	if metrics == nil {
		return nil
	}

	result := &PageMetricsEvent{
		FinalURL:           metrics.FinalURL,
		TotalRequests:      metrics.TotalRequests,
		TotalBytes:         metrics.TotalBytes,
		SameOriginRequests: metrics.SameOriginRequests,
		SameOriginBytes:    metrics.SameOriginBytes,
		ThirdPartyRequests: metrics.ThirdPartyRequests,
		ThirdPartyBytes:    metrics.ThirdPartyBytes,
		ThirdPartyDomains:  metrics.ThirdPartyDomains,
		BlockedCount:       metrics.BlockedCount,
		FailedCount:        metrics.FailedCount,
		TimedOut:           metrics.TimedOut,
		ConsoleMessages:    metrics.ConsoleMessages,
		ErrorCount:         countConsoleType(metrics.ConsoleMessages, types.ConsoleTypeError),
		WarningCount:       countConsoleType(metrics.ConsoleMessages, types.ConsoleTypeWarning),
		TimeToFirstRequest: metrics.TimeToFirstRequest,
		TimeToLastResponse: metrics.TimeToLastResponse,
		LifecycleEvents:    metrics.LifecycleEvents,
		StatusCounts:       metrics.StatusCounts,
		BytesByType:        metrics.BytesByType,
		RequestsByType:     metrics.RequestsByType,
	}

	if len(metrics.DomainStats) > 0 {
		result.DomainStats = make(map[string]*DomainStatsEvent, len(metrics.DomainStats))
		for domain, stats := range metrics.DomainStats {
			result.DomainStats[domain] = &DomainStatsEvent{
				Requests:   stats.Requests,
				Bytes:      stats.Bytes,
				Failed:     stats.Failed,
				Blocked:    stats.Blocked,
				AvgLatency: stats.AvgLatency,
			}
		}
	}

	return result
}

// countConsoleType counts console messages of a specific type
func countConsoleType(messages []types.ConsoleError, targetType string) int {
	count := 0
	for _, msg := range messages {
		if msg.Type == targetType {
			count++
		}
	}
	return count
}

// convertPageSEO converts types.PageSEO to PageSEOEvent
func convertPageSEO(seo *types.PageSEO) *PageSEOEvent {
	if seo == nil {
		return nil
	}

	event := &PageSEOEvent{
		Title:               seo.Title,
		IndexStatus:         int(seo.IndexStatus),
		MetaDescription:     seo.MetaDescription,
		CanonicalURL:        seo.CanonicalURL,
		MetaRobots:          seo.MetaRobots,
		H1s:                 seo.H1s,
		H2s:                 seo.H2s,
		H3s:                 seo.H3s,
		LinksTotal:          seo.LinksTotal,
		LinksInternal:       seo.LinksInternal,
		LinksExternal:       seo.LinksExternal,
		ExternalDomains:     seo.ExternalDomains,
		ImagesTotal:         seo.ImagesTotal,
		ImagesInternal:      seo.ImagesInternal,
		ImagesExternal:      seo.ImagesExternal,
		StructuredDataTypes: seo.StructuredDataTypes,
	}

	// Convert hreflang entries
	if len(seo.Hreflang) > 0 {
		event.Hreflang = make([]HreflangEntryEvent, len(seo.Hreflang))
		for i, h := range seo.Hreflang {
			event.Hreflang[i] = HreflangEntryEvent{
				Lang: h.Lang,
				URL:  h.URL,
			}
		}
	}

	return event
}

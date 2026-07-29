package events

import (
	"iter"
	"time"

	"github.com/valyala/fasthttp"

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
		event.OriginalURL = renderCtx.OriginalURL
		event.URLHash = renderCtx.URLHash
		event.Dimension = renderCtx.Dimension
		if renderCtx.HTTPCtx != nil {
			event.UserAgent = string(renderCtx.HTTPCtx.UserAgent())
			event.RequestHeaders = copyHeaders(renderCtx.HTTPCtx.Request.Header.All())
			event.ResponseHeaders = copyHeaders(renderCtx.HTTPCtx.Response.Header.All())
		}
		event.ClientIP = renderCtx.ClientIP
		event.DimensionAction = renderCtx.DimensionAction

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
		event.RedirectTo = result.RedirectTo

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

		if len(result.RuleIDs) > 0 {
			event.RuleIDs = result.RuleIDs
		}
		if result.OriginalPageSEO != nil {
			event.PageSEOOriginal = convertPageSEOWithoutLinks(result.OriginalPageSEO)
		}
		if len(result.Extraction) > 0 {
			event.Extraction = result.Extraction
		}
	}

	// Override EventType for precache requests
	if renderCtx != nil && renderCtx.IsPrecache {
		event.EventType = EventTypePrecache
	}

	return event
}

// BuildErrorEvent creates an error event for early failures (auth, validation, etc.).
// httpCtx may be nil; when set, request and response headers are captured from it.
func BuildErrorEvent(
	httpCtx *fasthttp.RequestCtx,
	requestID string,
	host string,
	hostID int,
	url string,
	originalURL string,
	userAgent string,
	clientIP string,
	errorType string,
	errorMessage string,
	statusCode int,
	egInstanceID string,
) *RequestEvent {
	event := &RequestEvent{
		RequestID:    requestID,
		Host:         host,
		HostID:       hostID,
		URL:          url,
		OriginalURL:  originalURL,
		UserAgent:    userAgent,
		ClientIP:     clientIP,
		EventType:    EventTypeError,
		StatusCode:   statusCode,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
		CreatedAt:    time.Now().UTC(),
		EGInstanceID: egInstanceID,
	}

	if httpCtx != nil {
		event.RequestHeaders = copyHeaders(httpCtx.Request.Header.All())
		event.ResponseHeaders = copyHeaders(httpCtx.Response.Header.All())
	}

	return event
}

// copyHeaders copies header key/value pairs out of a pooled fasthttp context.
// Returns nil when there are no headers.
func copyHeaders(headers iter.Seq2[[]byte, []byte]) map[string][]string {
	var result map[string][]string
	for key, value := range headers {
		if result == nil {
			result = make(map[string][]string)
		}
		name := string(key)
		result[name] = append(result[name], string(value))
	}
	return result
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

// convertPageSEO converts types.PageSEO to PageSEOEvent, including the captured
// outbound link graph.
func convertPageSEO(seo *types.PageSEO) *PageSEOEvent {
	event := convertPageSEOWithoutLinks(seo)
	if event == nil {
		return nil
	}

	if len(seo.PageLinks) > 0 {
		event.PageLinks = make([]PageLinkEvent, len(seo.PageLinks))
		for i, l := range seo.PageLinks {
			event.PageLinks[i] = PageLinkEvent{
				Target:     l.Target,
				Anchor:     l.Anchor,
				IsInternal: l.IsInternal,
				Nofollow:   l.Nofollow,
				Sponsored:  l.Sponsored,
				UGC:        l.UGC,
				IsImage:    l.IsImage,
				DomPath:    l.DomPath,
			}
		}
	}

	return event
}

// convertPageSEOWithoutLinks converts everything except the captured link graph. The
// original-SEO snapshot uses it: that snapshot exists for before/after SEO comparison
// and the link graph is read from the primary snapshot only, so carrying it twice
// would double the event for no consumer.
func convertPageSEOWithoutLinks(seo *types.PageSEO) *PageSEOEvent {
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
		ImagesWithAlt:       seo.ImagesWithAlt,
		ImagesWithoutAlt:    seo.ImagesWithoutAlt,
		WordCount:           seo.WordCount,
		PageMinHash:         seo.PageMinHash,
		HreflangSelf:        seo.HreflangSelf,
		StructuredDataTypes: seo.StructuredDataTypes,
		PageLinksTruncated:  seo.PageLinksTruncated,
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

	// Convert breadcrumbs (extracted today but previously dropped before the event)
	if len(seo.Breadcrumbs) > 0 {
		event.Breadcrumbs = make([]BreadcrumbEntryEvent, len(seo.Breadcrumbs))
		for i, b := range seo.Breadcrumbs {
			event.Breadcrumbs[i] = BreadcrumbEntryEvent{
				Name: b.Name,
				URL:  b.URL,
			}
		}
	}

	return event
}

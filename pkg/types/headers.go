package types

// HeaderRenderKey is the HTTP header that carries the per-host render key.
// Used inbound to authenticate client requests to the Edge Gateway, and
// outbound on origin fetches so the customer's origin can identify EdgeComet traffic.
const HeaderRenderKey = "X-Render-Key"

// HeaderEdgeRender marks requests originating from EdgeComet itself (render
// service Chrome fetches and edge gateway bypass fetches) so the customer's
// integration skips re-routing them back into EdgeComet and forwards to origin.
const HeaderEdgeRender = "X-Edge-Render"

// Informational response headers (client-facing). The EC- prefix replaces the
// deprecated X- convention (RFC 6648). These supersede the former X-Render-Source,
// X-Render-Cache, X-Render-Service, X-Cache-Age, X-Processed-URL, X-Matched-Rule,
// X-Unmatched-Dimension and X-Render-Action set. Internal/functional headers
// (HeaderRenderKey, HeaderEdgeRender, X-Internal-Auth) keep the X- prefix.
const (
	// HeaderRequestID carries the request trace ID. Read inbound (a fresh UUID is
	// generated if absent) and echoed outbound.
	HeaderRequestID = "EC-Request-ID"
	// HeaderCacheAge reports the age of cached content in whole seconds.
	HeaderCacheAge = "EC-Cache-Age"
	// HeaderSource reports how the Edge Gateway produced the response.
	HeaderSource = "EC-Source"
	// HeaderMatchedRule reports the ID of the matched URL rule. Emitted only when
	// the matched rule has an explicit ID (absent otherwise).
	HeaderMatchedRule = "EC-Matched-Rule"
)

// EC-Source values. Intentionally distinct from internal/edge/events Source*
// (the access-log/dashboard contract) and from cache.SourceBypass (cache
// provenance) -- do not unify these vocabularies.
const (
	SourceRender      = "render"       // Freshly rendered
	SourceBypass      = "bypass"       // Proxied from origin, not cached
	SourceRenderCache = "render_cache" // Fresh from render cache
	SourceBypassCache = "bypass_cache" // Fresh from bypass cache
	SourceRenderStale = "render_stale" // Stale render cache served within stale-TTL
	SourceBypassStale = "bypass_stale" // Stale bypass cache served within stale-TTL
	SourceStatus      = "status"       // Configured status action (3xx/4xx/5xx rule)
)

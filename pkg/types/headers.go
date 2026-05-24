package types

// HeaderRenderKey is the HTTP header that carries the per-host render key.
// Used inbound to authenticate client requests to the Edge Gateway, and
// outbound on origin fetches so the customer's origin can identify EdgeComet traffic.
const HeaderRenderKey = "X-Render-Key"

// HeaderEdgeRender marks requests originating from EdgeComet itself (render
// service Chrome fetches and edge gateway bypass fetches) so the customer's
// integration skips re-routing them back into EdgeComet and forwards to origin.
const HeaderEdgeRender = "X-Edge-Render"

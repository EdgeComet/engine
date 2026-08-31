package testutil

import (
	"net/http"

	"github.com/edgecomet/engine/pkg/types"
)

// Render key fixture, built on the shared echo page. Both origins echo back the X-Render-Key they
// received, so a spec can prove exactly which requests carried it: the main document and the
// same-host XHR must, the third-party subresource must not. The echoes report a sentinel rather
// than an empty string when the header is absent, so "no key" is distinguishable from "the fetch
// never ran and the element stayed empty".
const (
	RenderKeyPagePath = "/render-key-test/page"
	renderKeyEchoPath = "/render-key-test/echo"

	// RenderKeyAbsent is echoed when the request carried no X-Render-Key header.
	RenderKeyAbsent = "NO_RENDER_KEY"

	// Markers the spec asserts on. Each is followed by the echoed key value.
	RenderKeyDocumentMarker   = "DOCUMENT_KEY="
	RenderKeySameHostMarker   = "SAME_HOST_KEY="
	RenderKeyThirdPartyMarker = "THIRD_PARTY_KEY="

	// thirdPartyHost keeps the third-party origin on a different hostname as well as a
	// different port, so the same-host check fails on both counts.
	thirdPartyHost = "127.0.0.1"

	renderKeyPageTitle = "Render Key Test Page"
)

// renderKeyMarkers are the prefixes the render key spec asserts on.
var renderKeyMarkers = headerEchoMarkers{
	document:   RenderKeyDocumentMarker,
	sameHost:   RenderKeySameHostMarker,
	thirdParty: RenderKeyThirdPartyMarker,
}

// receivedRenderKey reports the X-Render-Key the request carried, or the absent sentinel.
func receivedRenderKey(r *http.Request) string {
	return receivedHeaderValue(r, types.HeaderRenderKey, RenderKeyAbsent)
}

// registerRenderKeyRoutes wires the fixture page and the same-host echo onto the main
// origin's mux. thirdPartyBaseURL is the origin the page's third-party fetch targets.
func registerRenderKeyRoutes(mux *http.ServeMux, thirdPartyBaseURL string) {
	mux.HandleFunc(RenderKeyPagePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		html := headerEchoPage(renderKeyPageTitle, renderKeyMarkers, receivedRenderKey(r),
			renderKeyEchoPath, thirdPartyBaseURL+renderKeyEchoPath)

		w.Write([]byte(html))
	})

	mux.HandleFunc(renderKeyEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeHeaderEcho(w, r, receivedRenderKey(r), false)
	})
}

// newThirdPartyMux builds the third-party origin. It serves only the echo endpoints and a
// health check; nothing here is reachable through the Edge Gateway.
func newThirdPartyMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","server":"third-party"}`))
	})

	mux.HandleFunc(renderKeyEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeHeaderEcho(w, r, receivedRenderKey(r), true)
	})

	registerSetHeadersThirdPartyRoutes(mux)

	return mux
}

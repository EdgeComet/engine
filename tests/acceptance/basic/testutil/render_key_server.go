package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/edgecomet/engine/pkg/types"
)

// Render key fixture. Both origins echo back the X-Render-Key they received, so a spec can
// prove exactly which requests carried it: the main document and the same-host XHR must,
// the third-party subresource must not. The echoes report a sentinel rather than an empty
// string when the header is absent, so "no key" is distinguishable from "the fetch never
// ran and the element stayed empty".
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

	// renderKeyEchoDelayMs holds both echo requests in flight while the browser waits for
	// them. A timer would let networkIdle fire before the values reach the DOM.
	renderKeyEchoDelayMs = 1200

	renderKeyDocumentElementID   = "document-key"
	renderKeySameHostElementID   = "same-host-key"
	renderKeyThirdPartyElementID = "third-party-key"
)

// renderKeyPageTemplate reports the document's own key server-side and fills the two
// subresource markers from the browser. Verbs in order: document element id, document key,
// same-host element id, third-party element id, same-host echo URL, third-party echo URL,
// echo delay, same-host element id, same-host marker, third-party element id,
// third-party marker.
const renderKeyPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Render Key Test Page</title>
	<meta name="description" content="Origin echoes the received X-Render-Key">
</head>
<body>
	<h1>Render Key Test Page</h1>
	<div id="%s">%s%s</div>
	<div id="%s"></div>
	<div id="%s"></div>
	<script>
	document.addEventListener('DOMContentLoaded', function () {
		var sameHost = '%s?delay=%d';
		var thirdParty = '%s?delay=%d';

		fetch(sameHost)
			.then(function (response) { return response.json(); })
			.then(function (payload) {
				document.getElementById('%s').textContent = '%s' + payload.render_key;
			});

		fetch(thirdParty)
			.then(function (response) { return response.json(); })
			.then(function (payload) {
				document.getElementById('%s').textContent = '%s' + payload.render_key;
			});
	});
	</script>
</body>
</html>`

// receivedRenderKey reports the X-Render-Key the request carried, or the absent sentinel.
func receivedRenderKey(r *http.Request) string {
	if key := r.Header.Get(types.HeaderRenderKey); key != "" {
		return key
	}
	return RenderKeyAbsent
}

// writeRenderKeyEcho answers with the received key as JSON. allowCrossOrigin is set on the
// third-party origin so the browser does not drop the response before the page reads it.
func writeRenderKeyEcho(w http.ResponseWriter, r *http.Request, allowCrossOrigin bool) {
	sleepRequestDelay(r, renderKeyEchoDelayMs)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if allowCrossOrigin {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]string{"render_key": receivedRenderKey(r)})
}

// registerRenderKeyRoutes wires the fixture page and the same-host echo onto the main
// origin's mux. thirdPartyBaseURL is the origin the page's third-party fetch targets.
func registerRenderKeyRoutes(mux *http.ServeMux, thirdPartyBaseURL string) {
	mux.HandleFunc(RenderKeyPagePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		html := fmt.Sprintf(renderKeyPageTemplate,
			renderKeyDocumentElementID, RenderKeyDocumentMarker, receivedRenderKey(r),
			renderKeySameHostElementID,
			renderKeyThirdPartyElementID,
			renderKeyEchoPath, renderKeyEchoDelayMs,
			thirdPartyBaseURL+renderKeyEchoPath, renderKeyEchoDelayMs,
			renderKeySameHostElementID, RenderKeySameHostMarker,
			renderKeyThirdPartyElementID, RenderKeyThirdPartyMarker)

		w.Write([]byte(html))
	})

	mux.HandleFunc(renderKeyEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeRenderKeyEcho(w, r, false)
	})
}

// newThirdPartyMux builds the third-party origin. It serves only the echo endpoint and a
// health check; nothing here is reachable through the Edge Gateway.
func newThirdPartyMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","server":"third-party"}`))
	})

	mux.HandleFunc(renderKeyEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeRenderKeyEcho(w, r, true)
	})

	return mux
}

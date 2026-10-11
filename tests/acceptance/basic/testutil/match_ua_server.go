package testutil

import (
	"fmt"
	"net/http"
)

// Every path under MatchUAPathPrefix answers 200 with the same page. The body carries the origin
// version, and MatchUARenderedMarker only when Chrome rendered it.
const (
	MatchUAPathPrefix = "/match-ua/"

	// MatchUAOriginVersionMarker is followed by the origin version the page was served with.
	MatchUAOriginVersionMarker  = "ORIGIN_VERSION="
	MatchUADefaultOriginVersion = "v1"

	// Written by the page script after its AJAX call returns, from two halves, so the raw origin
	// HTML never contains the joined marker.
	matchUARenderedMarkerHead = "MATCH_UA_"
	matchUARenderedMarkerTail = "RENDERED"
	MatchUARenderedMarker     = matchUARenderedMarkerHead + matchUARenderedMarkerTail

	matchUAAjaxDelayMs = 200
)

const matchUAPageTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>match_ua test page</title>
</head>
<body>
<h1>match_ua test page</h1>
<p id="origin-version">%s%s</p>
<div id="rendered"></div>
<script>
document.addEventListener('DOMContentLoaded', function () {
	fetch('/api/mock-data?delay=%d')
		.then(function (response) { return response.json(); })
		.then(function () {
			document.getElementById('rendered').textContent = '%s' + '%s';
		});
});
</script>
</body>
</html>`

// MatchUAPageHandler serves the match_ua page with a fixed origin version, for an origin a spec
// can take down.
func MatchUAPageHandler(version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMatchUAPage(w, version)
	})
}

// SetMatchUAOriginVersion changes the version the shared origin serves from now on.
func (ts *TestServer) SetMatchUAOriginVersion(version string) {
	ts.matchUAOriginVersion.Store(&version)
}

func (ts *TestServer) registerMatchUARoutes(mux *http.ServeMux) {
	mux.HandleFunc(MatchUAPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		version := MatchUADefaultOriginVersion
		if stored := ts.matchUAOriginVersion.Load(); stored != nil {
			version = *stored
		}
		writeMatchUAPage(w, version)
	})
}

func writeMatchUAPage(w http.ResponseWriter, version string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, matchUAPageTemplate, MatchUAOriginVersionMarker, version, matchUAAjaxDelayMs,
		matchUARenderedMarkerHead, matchUARenderedMarkerTail)
}

package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Shared echo-origin machinery. An origin that reports the request headers it received is the
// only way to observe what EdgeComet sent: the values never appear in a response to the bot. One
// page shape serves every such fixture - the document reports what its own request carried, a
// same-host fetch and a third-party fetch report what theirs carried - which is what separates a
// header that reached only the document from one that reached every same-origin subrequest.
const (
	// headerEchoDelayMs holds both echo requests in flight while the browser waits for them. A
	// timer would let networkIdle fire before the values reach the DOM.
	headerEchoDelayMs = 1200

	headerEchoDocumentElementID   = "echo-document"
	headerEchoSameHostElementID   = "echo-same-host"
	headerEchoThirdPartyElementID = "echo-third-party"

	// headerEchoValueSeparator joins the values of a repeated header, so a duplicate that should
	// have been collapsed is visible instead of hidden behind the first value.
	headerEchoValueSeparator = ","
)

// headerEchoMarkers are the prefixes a spec asserts on, one per request the page reports.
type headerEchoMarkers struct {
	document   string
	sameHost   string
	thirdParty string
}

// headerEchoPageTemplate is filled by headerEchoPage. Verbs in order: page title, heading,
// document element id, document marker, document echo, same-host element id, third-party element
// id, same-host echo URL, echo delay, third-party echo URL, echo delay, same-host element id,
// same-host marker, third-party element id, third-party marker.
const headerEchoPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>%s</title>
	<meta name="description" content="Origin echoes the request headers it received">
</head>
<body>
	<h1>%s</h1>
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
				document.getElementById('%s').textContent = '%s' + payload.echo;
			});

		fetch(thirdParty)
			.then(function (response) { return response.json(); })
			.then(function (payload) {
				document.getElementById('%s').textContent = '%s' + payload.echo;
			});
	});
	</script>
</body>
</html>`

// headerEchoPage renders one echo page. documentEcho is computed by the caller from the request
// that fetched the page, because only the caller knows which headers its fixture reports.
func headerEchoPage(title string, markers headerEchoMarkers, documentEcho, sameHostEchoURL, thirdPartyEchoURL string) string {
	return fmt.Sprintf(headerEchoPageTemplate,
		title, title,
		headerEchoDocumentElementID, markers.document, documentEcho,
		headerEchoSameHostElementID,
		headerEchoThirdPartyElementID,
		sameHostEchoURL, headerEchoDelayMs,
		thirdPartyEchoURL, headerEchoDelayMs,
		headerEchoSameHostElementID, markers.sameHost,
		headerEchoThirdPartyElementID, markers.thirdParty)
}

// receivedHeaderValue reports one header the request carried, or absent when it carried none.
func receivedHeaderValue(r *http.Request, name, absent string) string {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return absent
	}
	return strings.Join(values, headerEchoValueSeparator)
}

// writeHeaderEcho answers with what the request carried, as JSON. allowCrossOrigin is set on the
// third-party origin so the browser does not drop the response before the page reads it.
func writeHeaderEcho(w http.ResponseWriter, r *http.Request, echo string, allowCrossOrigin bool) {
	sleepRequestDelay(r, headerEchoDelayMs)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if allowCrossOrigin {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]string{"echo": echo})
}

// headerRecorder keeps the headers of each request the echo origin served, keyed by request URI.
// Two paths never return the origin's response to the spec that triggered them - precache is
// dispatched by the daemon and HAR debug answers with a HAR - so recording is the only way for a
// spec to see what those requests carried.
type headerRecorder struct {
	mu           sync.Mutex
	byRequestURI map[string]http.Header
}

func newHeaderRecorder() *headerRecorder {
	return &headerRecorder{byRequestURI: make(map[string]http.Header)}
}

func (hr *headerRecorder) record(r *http.Request) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.byRequestURI[r.URL.RequestURI()] = r.Header.Clone()
}

func (hr *headerRecorder) lookup(requestURI string) http.Header {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	return hr.byRequestURI[requestURI]
}

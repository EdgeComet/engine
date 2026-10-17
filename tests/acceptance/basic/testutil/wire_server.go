package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Wire spelling fixture. Every request under WirePathPrefix is recorded with its verbatim
// request-target before the mux runs: ServeMux redirects targets holding "..", "." or "//" and
// matches on the unescaped path, so neither could report what EdgeComet actually sent.
//
// Layout: /wire/{kind}/{token}/{mode}/{rest...}. kind selects the host URL rule, token keys the
// recording, mode selects the answer.
const (
	WirePathPrefix = "/wire/"

	WireKindBypass      = "bypass"
	WireKindRender      = "render"
	WireKindBypassStale = "bypass-stale"
	WireKindRenderStale = "render-stale"

	// WireModeOK answers 200 with a page that loads no subresources.
	WireModeOK = "ok"
	// WireModeCanon emulates an origin with one canonical spelling: the registered request-target
	// gets 200, every other spelling gets a 301 to it.
	WireModeCanon = "canon"
	// WireModeStatus answers with the status, Location and delay registered for the token.
	WireModeStatus = "status"
	// WireModeFetchSelf is WireModeCanon whose 200 page fetches the decoded spelling of its own
	// path, which the canon rule answers with a 301 back to the page.
	WireModeFetchSelf = "fetchself"

	wireTokenSegment = 3
	wireModeSegment  = 4
	wireMinSegments  = 5

	wirePageTitle     = "Wire fixture"
	wireFetchedMarker = "WIRE_FETCHED"
)

const wirePage = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>` + wirePageTitle + `</title>
</head>
<body>
<h1>` + wirePageTitle + `</h1>
<p>Status %d</p>
%s
</body>
</html>`

// wireFetchSelfScript keeps the network busy until the fetch settles, so the render cannot finish
// before the subresource redirect is seen.
const wireFetchSelfScript = `<div id="fetched"></div>
<script>
document.addEventListener('DOMContentLoaded', function () {
	fetch(%s)
		.then(function (response) { return response.text(); })
		.then(function () {
			document.getElementById('fetched').textContent = '` + wireFetchedMarker + `';
		});
});
</script>`

type wireResponse struct {
	status   int
	location string
	delay    time.Duration
}

type wireRecorder struct {
	mu        sync.Mutex
	requests  map[string][]string
	canonical map[string]string
	responses map[string]wireResponse
}

func newWireRecorder() *wireRecorder {
	return &wireRecorder{
		requests:  make(map[string][]string),
		canonical: make(map[string]string),
		responses: make(map[string]wireResponse),
	}
}

// wireSegments splits a request-target into its path segments. Only the fixed prefix segments are
// read, and those never carry escapes or a query.
func wireSegments(requestURI string) ([]string, bool) {
	if !strings.HasPrefix(requestURI, WirePathPrefix) {
		return nil, false
	}
	segments := strings.Split(requestURI, "/")
	if len(segments) < wireMinSegments {
		return nil, false
	}
	return segments, true
}

// wrapWire answers wire requests itself and hands everything else to next.
func (ts *TestServer) wrapWire(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments, ok := wireSegments(r.RequestURI)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		token := segments[wireTokenSegment]
		ts.wire.record(token, r.RequestURI)
		ts.wire.serve(w, r, token, segments[wireModeSegment])
	})
}

func (wr *wireRecorder) record(token, requestURI string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.requests[token] = append(wr.requests[token], requestURI)
}

func (wr *wireRecorder) serve(w http.ResponseWriter, r *http.Request, token, mode string) {
	wr.mu.Lock()
	canonical := wr.canonical[token]
	response, hasResponse := wr.responses[token]
	wr.mu.Unlock()

	switch mode {
	case WireModeCanon, WireModeFetchSelf:
		if r.RequestURI != canonical {
			// Absolute on the host the request named, as Shoper answers, so the Location maps to
			// the cache key of the request that received it.
			w.Header().Set("Location", "http://"+r.Host+canonical)
			writeWirePage(w, http.StatusMovedPermanently, "")
			return
		}
		script := ""
		if mode == WireModeFetchSelf {
			script = wireFetchSelf(canonical)
		}
		writeWirePage(w, http.StatusOK, script)
	case WireModeStatus:
		if !hasResponse {
			writeWirePage(w, http.StatusOK, "")
			return
		}
		time.Sleep(response.delay)
		if response.location != "" {
			w.Header().Set("Location", response.location)
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		writeWirePage(w, status, "")
	default:
		writeWirePage(w, http.StatusOK, "")
	}
}

func wireFetchSelf(canonical string) string {
	decoded, err := url.PathUnescape(canonical)
	if err != nil {
		decoded = canonical
	}
	target, _ := json.Marshal(decoded)
	return fmt.Sprintf(wireFetchSelfScript, target)
}

// writeWirePage sends redirects without a body: the gateway stores a bypass redirect body's length
// as disk_size without writing a file, and such an entry is never served from cache.
func writeWirePage(w http.ResponseWriter, status int, script string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if status >= http.StatusMultipleChoices && status < http.StatusBadRequest {
		return
	}
	fmt.Fprintf(w, wirePage, status, script)
}

// SetWireCanonical registers the one request-target the canon and fetchself modes answer with 200.
func (ts *TestServer) SetWireCanonical(token, requestTarget string) {
	ts.wire.mu.Lock()
	defer ts.wire.mu.Unlock()
	ts.wire.canonical[token] = requestTarget
}

// SetWireResponse sets the status and Location the status mode answers with. An empty location
// sends no Location header.
func (ts *TestServer) SetWireResponse(token string, status int, location string) {
	ts.wire.mu.Lock()
	defer ts.wire.mu.Unlock()
	response := ts.wire.responses[token]
	response.status = status
	response.location = location
	ts.wire.responses[token] = response
}

// SetWireDelay holds every status mode answer for the token by d.
func (ts *TestServer) SetWireDelay(token string, d time.Duration) {
	ts.wire.mu.Lock()
	defer ts.wire.mu.Unlock()
	response := ts.wire.responses[token]
	response.delay = d
	ts.wire.responses[token] = response
}

// WireRequests returns the request-targets the origin received for the token, in arrival order.
func (ts *TestServer) WireRequests(token string) []string {
	ts.wire.mu.Lock()
	defer ts.wire.mu.Unlock()
	return append([]string(nil), ts.wire.requests[token]...)
}

// WireCount returns how many requests the origin received for the token.
func (ts *TestServer) WireCount(token string) int {
	ts.wire.mu.Lock()
	defer ts.wire.mu.Unlock()
	return len(ts.wire.requests[token])
}

// WirePath builds the request-target of a wire fixture page. rest is appended verbatim.
func WirePath(kind, token, mode, rest string) string {
	return WirePathPrefix + kind + "/" + token + "/" + mode + "/" + rest
}

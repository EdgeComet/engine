package testutil

import (
	"net/http"
	"strings"
)

// Explicit request header fixture (request_headers_set). The origin reports the headers it
// received on the document, on a same-host XHR and on a third-party subresource, and records
// every document request so the paths that never hand their response back to the spec - precache
// and HAR debug - are observable too.
const (
	// SetHeadersPathPrefix serves every page of this fixture. The subpath selects which URL rule
	// the Edge Gateway matches, so one handler covers the render, bypass and override cases.
	SetHeadersPathPrefix = "/set-headers-test/"

	// SetHeadersOverridePath is matched by a URL rule that overrides one set header.
	SetHeadersOverridePath = SetHeadersPathPrefix + "override/page"
	// SetHeadersBypassPath is matched by a URL rule with action bypass.
	SetHeadersBypassPath = SetHeadersPathPrefix + "bypass/page"
	// SetHeadersBypassCachePath is matched by a bypass rule with caching enabled, which is what
	// the precache bypass path needs to have anything to write.
	SetHeadersBypassCachePath = SetHeadersPathPrefix + "bypass-cache/page"
	// SetHeadersRenderPath matches no rule, so it renders under the host's own configuration.
	SetHeadersRenderPath = SetHeadersPathPrefix + "page"

	setHeadersEchoPath = "/set-headers-echo"

	// HeaderEchoAbsent is reported for a header the request did not carry.
	HeaderEchoAbsent = "ABSENT"

	// Markers the specs assert on. Each is followed by the echoed header entries.
	SetHeadersDocumentMarker   = "DOCUMENT_HEADERS="
	SetHeadersSameHostMarker   = "SAME_HOST_HEADERS="
	SetHeadersThirdPartyMarker = "THIRD_PARTY_HEADERS="

	setHeadersPageTitle = "Request Headers Set Test Page"

	// Header names the fixture reports. The host sets the first two, global configuration sets
	// the third, and a URL rule overrides the first.
	SetHeaderAPIKey   = "X-Api-Key"
	SetHeaderTenantID = "X-Tenant-Id"
	SetHeaderGlobal   = "X-Global-Marker"

	// Values the configuration sets. The host and rule values are mirrored from the host fixture
	// so a spec asserts on the same literal the Edge Gateway sends; the global value is written
	// into the generated Edge Gateway config by the config builder.
	SetHeaderHostAPIKeyValue = "host-api-key"
	SetHeaderRuleAPIKeyValue = "rule-api-key"
	SetHeaderTenantValue     = "host-tenant"
	SetHeaderGlobalValue     = "global-value"
)

// setHeadersEchoed lists the reported headers in the order the fixture reports them.
var setHeadersEchoed = []string{SetHeaderAPIKey, SetHeaderTenantID, SetHeaderGlobal}

// HeaderEchoEntry formats the exact substring a spec asserts on for one received header. The
// brackets are what make "received once" assertable: a repeated header joins its values inside
// them, so an expectation naming a single value stops matching.
func HeaderEchoEntry(name, value string) string {
	return "[" + name + "=" + value + "]"
}

// setHeadersEcho formats every reported header of one request as bracketed entries.
func setHeadersEcho(r *http.Request) string {
	var echo strings.Builder
	for _, name := range setHeadersEchoed {
		echo.WriteString(HeaderEchoEntry(name, receivedHeaderValue(r, name, HeaderEchoAbsent)))
	}
	return echo.String()
}

// registerSetHeadersRoutes wires the fixture pages and the same-host echo onto the main origin's
// mux. thirdPartyBaseURL is the origin the page's third-party fetch targets.
func (ts *TestServer) registerSetHeadersRoutes(mux *http.ServeMux) {
	mux.HandleFunc(SetHeadersPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		ts.receivedHeaders.record(r)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		markers := headerEchoMarkers{
			document:   SetHeadersDocumentMarker,
			sameHost:   SetHeadersSameHostMarker,
			thirdParty: SetHeadersThirdPartyMarker,
		}
		html := headerEchoPage(setHeadersPageTitle, markers, setHeadersEcho(r),
			setHeadersEchoPath, ts.thirdPartyBaseURL+setHeadersEchoPath)

		w.Write([]byte(html))
	})

	mux.HandleFunc(setHeadersEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeHeaderEcho(w, r, setHeadersEcho(r), false)
	})
}

// registerSetHeadersThirdPartyRoutes wires the echo onto the third-party origin, which the page
// reaches cross-origin and which nothing may reach through the Edge Gateway.
func registerSetHeadersThirdPartyRoutes(mux *http.ServeMux) {
	mux.HandleFunc(setHeadersEchoPath, func(w http.ResponseWriter, r *http.Request) {
		writeHeaderEcho(w, r, setHeadersEcho(r), true)
	})
}

// ReceivedHeaders returns the headers the origin received on the last request to requestURI (path
// and query, as the origin saw it), or nil when nothing requested it.
func (ts *TestServer) ReceivedHeaders(requestURI string) http.Header {
	return ts.receivedHeaders.lookup(requestURI)
}

package orchestrator

import (
	"strings"
)

// securityHeadersDenyList contains headers that must never be cached or served for security reasons
// These headers are blocked regardless of safe_headers configuration
var securityHeadersDenyList = map[string]bool{
	// Standard HTTP authentication headers
	"authorization":       true,
	"www-authenticate":    true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	// Common custom authentication headers
	"x-auth-token":    true,
	"x-access-token":  true,
	"x-refresh-token": true,
	"x-api-key":       true,
	"x-csrf-token":    true,
	"x-xsrf-token":    true,
}

// cacheOnlyDenyList contains headers that must never be stored in cache
// but can be served to clients if explicitly configured in safe_response
const headerSetCookie = "set-cookie"

const headerContentType = "Content-Type"

const headerLocation = "Location"

// LocationHeaderValue returns the Location header value, matched case-insensitively per RFC 7230,
// or an empty string when the header is absent. Callers gate on the status code themselves: the
// recache path records the target of an uncacheable redirect, the serving path only 301/302/307/308.
func LocationHeaderValue(headers map[string][]string) string {
	if locations, ok := getHeaderCaseInsensitive(headers, headerLocation); ok && len(locations) > 0 {
		return locations[0]
	}
	return ""
}

// isRedirectStatusCode checks if the status code is a redirect (3xx)
func isRedirectStatusCode(statusCode int) bool {
	return statusCode == 301 || statusCode == 302 || statusCode == 307 || statusCode == 308
}

// firstHeaderValueSorted returns the first value of the header matching name
// case-insensitively, resolving ties on the lexicographically smallest matching name.
//
// getHeaderCaseInsensitive ranges over the map and stops at the first hit, so an origin
// that sends the same header under two spellings gets a different answer on every run.
// That is tolerable when the value only steers serving, but not when it is stored as
// evidence: repeated processing of one response must yield one answer.
func firstHeaderValueSorted(headers map[string][]string, name string) (string, bool) {
	var selected string
	found := false
	for headerName := range headers {
		if !strings.EqualFold(headerName, name) {
			continue
		}
		if !found || headerName < selected {
			selected = headerName
			found = true
		}
	}
	if !found || len(headers[selected]) == 0 {
		return "", false
	}
	return headers[selected][0], true
}

// getHeaderCaseInsensitive retrieves header value with case-insensitive key matching
// HTTP headers are case-insensitive per RFC 7230
func getHeaderCaseInsensitive(headers map[string][]string, name string) ([]string, bool) {
	nameLower := strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == nameLower {
			return v, true
		}
	}
	return nil, false
}

// IsHTMLContentTypeValue reports whether a Content-Type value indicates an HTML body.
// An empty value is treated as HTML, since both origins and the renderer default
// to text/html when no Content-Type is present. Exported so the recache service (separate
// package) can mirror the bypass serve path's content-type guard exactly.
func IsHTMLContentTypeValue(contentType string) bool {
	if contentType == "" {
		return true
	}
	return strings.Contains(contentType, contentTypeHTML)
}

// isHTMLContentType reports whether response headers indicate an HTML body.
func isHTMLContentType(headers map[string][]string) bool {
	ct, ok := getHeaderCaseInsensitive(headers, headerContentType)
	if !ok || len(ct) == 0 {
		return true
	}
	return IsHTMLContentTypeValue(ct[0])
}

// FilterHeaders filters headers to only include those in the safe headers list
// and NOT in the security deny list.
// When forCache=true, Set-Cookie is always blocked (cookies must not be cached).
// When forCache=false, Set-Cookie is allowed if explicitly in safeHeaders.
// Always includes Location header for redirect responses (3xx).
// Returns a new map with the filtered headers.
func FilterHeaders(headers map[string][]string, safeHeaders []string, statusCode int, forCache bool) map[string][]string {
	if len(headers) == 0 {
		return nil
	}

	var filtered map[string][]string

	// Filter by safe headers list if configured
	if len(safeHeaders) > 0 {
		// Build lowercase lookup map for case-insensitive matching
		safeHeadersLower := make(map[string]bool, len(safeHeaders))
		for _, header := range safeHeaders {
			safeHeadersLower[strings.ToLower(header)] = true
		}

		// Match case-insensitively, preserve original header case from response
		filtered = make(map[string][]string)
		for headerName, headerValues := range headers {
			headerLower := strings.ToLower(headerName)

			// 1. Skip security-sensitive headers (deny list takes precedence)
			if securityHeadersDenyList[headerLower] {
				continue
			}

			// 2. Block Set-Cookie when caching (cookies must never be cached)
			if headerLower == headerSetCookie && forCache {
				continue
			}

			// 3. Allow if in safe list
			if safeHeadersLower[headerLower] {
				filtered[headerName] = headerValues
			}
		}
	}

	// Always include Location header for redirects (case-insensitive per RFC 7230)
	if isRedirectStatusCode(statusCode) {
		if locations, ok := getHeaderCaseInsensitive(headers, "Location"); ok && len(locations) > 0 {
			if filtered == nil {
				filtered = make(map[string][]string)
			}
			filtered["Location"] = locations
		}
	}

	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func singleValueHeaders(headers map[string][]string) map[string]string {
	if headers == nil {
		return nil
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}

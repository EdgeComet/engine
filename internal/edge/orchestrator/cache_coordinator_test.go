package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestFilterSafeHeaders(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string][]string
		safeHeaders []string
		statusCode  int
		expected    map[string][]string
	}{
		{
			name: "case-insensitive matching - lowercase input, Title-Case config",
			headers: map[string][]string{
				"content-type":  {"text/html"},
				"cache-control": {"max-age=3600"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control"},
			statusCode:  200,
			expected: map[string][]string{
				"content-type":  {"text/html"},
				"cache-control": {"max-age=3600"},
			},
		},
		{
			name: "case-insensitive matching - Title-Case input, lowercase config",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
			},
			safeHeaders: []string{"content-type", "cache-control"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
			},
		},
		{
			name: "mixed case variations - multiple representations of same header",
			headers: map[string][]string{
				"CONTENT-TYPE": {"text/html"},
				"content-type": {"application/json"}, // Note: This will overwrite in real scenario
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected: map[string][]string{
				"CONTENT-TYPE": {"text/html"},
				"content-type": {"application/json"},
			},
		},
		{
			name: "empty safe_headers list returns nil",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
			safeHeaders: []string{},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "nil safe_headers returns nil",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
			safeHeaders: nil,
			statusCode:  200,
			expected:    nil,
		},
		{
			name:        "empty headers map returns nil",
			headers:     map[string][]string{},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name:        "nil headers map returns nil",
			headers:     nil,
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "headers not in safe list are filtered out",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"X-Custom":     {"value"},
				"Server":       {"nginx"},
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "preserves original header case from response",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"ETag":         {`"abc123"`},
			},
			safeHeaders: []string{"content-type", "etag"}, // lowercase config
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"}, // preserves Title-Case from response
				"ETag":         {`"abc123"`},  // preserves ETag case
			},
		},
		{
			name: "multiple headers match all returned",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
				"ETag":          {`"abc123"`},
				"Expires":       {"Wed, 21 Oct 2025 07:28:00 GMT"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control", "ETag", "Expires"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type":  {"text/html"},
				"Cache-Control": {"max-age=3600"},
				"ETag":          {`"abc123"`},
				"Expires":       {"Wed, 21 Oct 2025 07:28:00 GMT"},
			},
		},
		{
			name: "no matching headers returns nil",
			headers: map[string][]string{
				"X-Custom":       {"value"},
				"X-Another":      {"value2"},
				"X-Third-Header": {"value3"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control"},
			statusCode:  200,
			expected:    nil,
		},
		{
			name: "partial matches with some filtered",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"Server":       {"nginx"},
				"ETag":         {`"abc123"`},
				"X-Custom":     {"value"},
			},
			safeHeaders: []string{"Content-Type", "ETag"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
				"ETag":         {`"abc123"`},
			},
		},
		{
			name: "multi-value headers preserved",
			headers: map[string][]string{
				"X-Custom-Multi": {"value1", "value2"},
				"Vary":           {"Accept-Encoding", "Accept-Language"},
			},
			safeHeaders: []string{"X-Custom-Multi", "Vary"},
			statusCode:  200,
			expected: map[string][]string{
				"X-Custom-Multi": {"value1", "value2"},
				"Vary":           {"Accept-Encoding", "Accept-Language"},
			},
		},
		{
			name: "security headers blocked even if in safe list",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"Set-Cookie":   {"session=abc123"},
				"X-Auth-Token": {"secret-token"},
			},
			safeHeaders: []string{"Content-Type", "Set-Cookie", "X-Auth-Token"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "security headers case-insensitive blocking",
			headers: map[string][]string{
				"Content-Type":  {"text/html"},
				"SET-COOKIE":    {"session=abc123"},
				"Authorization": {"Bearer xyz"},
			},
			safeHeaders: []string{"Content-Type", "SET-COOKIE", "Authorization"},
			statusCode:  200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		{
			name: "all security headers in deny list are blocked",
			headers: map[string][]string{
				"Content-Type":        {"text/html"},
				"Set-Cookie":          {"session=abc"},
				"Authorization":       {"Bearer token"},
				"WWW-Authenticate":    {"Basic"},
				"Proxy-Authenticate":  {"Basic"},
				"Proxy-Authorization": {"Basic creds"},
				"X-Auth-Token":        {"token123"},
				"X-Access-Token":      {"access123"},
				"X-Refresh-Token":     {"refresh123"},
				"X-API-Key":           {"apikey123"},
				"X-CSRF-Token":        {"csrf123"},
				"X-XSRF-Token":        {"xsrf123"},
			},
			safeHeaders: []string{
				"Content-Type", "Set-Cookie", "Authorization", "WWW-Authenticate",
				"Proxy-Authenticate", "Proxy-Authorization", "X-Auth-Token",
				"X-Access-Token", "X-Refresh-Token", "X-API-Key", "X-CSRF-Token", "X-XSRF-Token",
			},
			statusCode: 200,
			expected: map[string][]string{
				"Content-Type": {"text/html"},
			},
		},
		// Redirect Location header tests
		{
			name: "lowercase location header with redirect status",
			headers: map[string][]string{
				"Content-Type": {"text/html"},
				"location":     {"https://example.com/new-path"}, // lowercase
			},
			safeHeaders: []string{"Content-Type"},
			statusCode:  301, // redirect status
			expected: map[string][]string{
				"Content-Type": {"text/html"},
				"Location":     {"https://example.com/new-path"}, // normalized to canonical "Location"
			},
		},
		{
			name: "redirect auto-includes Location even without safe_headers",
			headers: map[string][]string{
				"Location": {"https://example.com/redirect"},
				"Server":   {"nginx"},
			},
			safeHeaders: []string{}, // empty safe_headers
			statusCode:  302,
			expected: map[string][]string{
				"Location": {"https://example.com/redirect"},
			},
		},
		{
			name: "non-redirect status does not auto-include Location",
			headers: map[string][]string{
				"Location": {"https://example.com/not-redirect"},
			},
			safeHeaders: []string{},
			statusCode:  200, // not a redirect
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterHeaders(tt.headers, tt.safeHeaders, tt.statusCode, true)

			if tt.expected == nil {
				require.Nil(t, result, "Expected nil but got: %v", result)
			} else {
				require.NotNil(t, result, "Expected non-nil result")
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFilterHeaders_SetCookieServing(t *testing.T) {
	// Set-Cookie should pass through when forCache=false and explicitly in safe list
	headers := map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123; Path=/; HttpOnly"},
	}
	safeHeaders := []string{"Content-Type", "Set-Cookie"}

	// forCache=false: Set-Cookie allowed (for serving to client)
	result := FilterHeaders(headers, safeHeaders, 200, false)
	require.NotNil(t, result)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123; Path=/; HttpOnly"},
	}, result)

	// forCache=true: Set-Cookie blocked (for caching)
	resultCache := FilterHeaders(headers, safeHeaders, 200, true)
	require.NotNil(t, resultCache)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
	}, resultCache)
}

func TestFilterHeaders_SetCookieNotInSafeList(t *testing.T) {
	// Set-Cookie should NOT pass through if not in safe list, even with forCache=false
	headers := map[string][]string{
		"Content-Type": {"text/html"},
		"Set-Cookie":   {"session=abc123"},
	}
	safeHeaders := []string{"Content-Type"} // Set-Cookie not in list

	result := FilterHeaders(headers, safeHeaders, 200, false)
	require.NotNil(t, result)
	assert.Equal(t, map[string][]string{
		"Content-Type": {"text/html"},
	}, result)
}

func TestFilterSafeHeaders_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string][]string
		safeHeaders []string
		expected    map[string][]string
	}{
		{
			name: "typical CDN response headers",
			headers: map[string][]string{
				"Content-Type":  {"text/html; charset=utf-8"},
				"Cache-Control": {"public, max-age=3600"},
				"ETag":          {`"5f8-5e5c0b5f5a5a"`},
				"Vary":          {"Accept-Encoding"},
				"Server":        {"cloudflare"},
				"CF-RAY":        {"8a1b2c3d4e5f6-SJC"},
				"X-Powered-By":  {"Express"},
			},
			safeHeaders: []string{"Content-Type", "Cache-Control", "ETag", "Vary"},
			expected: map[string][]string{
				"Content-Type":  {"text/html; charset=utf-8"},
				"Cache-Control": {"public, max-age=3600"},
				"ETag":          {`"5f8-5e5c0b5f5a5a"`},
				"Vary":          {"Accept-Encoding"},
			},
		},
		{
			name: "redirect response with Location",
			headers: map[string][]string{
				"Location":      {"https://example.com/new-path"},
				"Cache-Control": {"no-cache"},
				"Server":        {"nginx"},
			},
			safeHeaders: []string{"Location", "Cache-Control"},
			expected: map[string][]string{
				"Location":      {"https://example.com/new-path"},
				"Cache-Control": {"no-cache"},
			},
		},
		{
			name: "API response with custom headers",
			headers: map[string][]string{
				"Content-Type":          {"application/json"},
				"X-RateLimit-Limit":     {"1000"},
				"X-RateLimit-Remaining": {"999"},
				"X-Request-ID":          {"abc-123-def"},
			},
			safeHeaders: []string{"Content-Type", "X-RateLimit-Limit", "X-RateLimit-Remaining"},
			expected: map[string][]string{
				"Content-Type":          {"application/json"},
				"X-RateLimit-Limit":     {"1000"},
				"X-RateLimit-Remaining": {"999"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterHeaders(tt.headers, tt.safeHeaders, 200, true)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_getStaleTTL(t *testing.T) {
	ptrDuration := func(d time.Duration) *types.Duration {
		td := types.Duration(d)
		return &td
	}

	tests := []struct {
		name     string
		expired  types.CacheExpiredConfig
		expected time.Duration
	}{
		{
			name: "serve_stale strategy with stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: ptrDuration(2 * time.Hour),
			},
			expected: 2 * time.Hour,
		},
		{
			name: "delete strategy with stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
				StaleTTL: ptrDuration(2 * time.Hour),
			},
			expected: 0,
		},
		{
			name: "serve_stale strategy without stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyServeStale,
				StaleTTL: nil,
			},
			expected: 0,
		},
		{
			name: "delete strategy without stale_ttl",
			expired: types.CacheExpiredConfig{
				Strategy: types.ExpirationStrategyDelete,
				StaleTTL: nil,
			},
			expected: 0,
		},
		{
			name: "empty strategy",
			expired: types.CacheExpiredConfig{
				Strategy: "",
				StaleTTL: ptrDuration(1 * time.Hour),
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStaleTTL(tt.expired)
			assert.Equal(t, tt.expected, result)
		})
	}
}

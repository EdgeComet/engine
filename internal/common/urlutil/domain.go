package urlutil

import (
	"net/url"
	"strings"
)

// ExtractHost extracts and lowercases the host from a URL string.
// Returns empty string if URL is invalid or has no host.
func ExtractHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

// ExtractHostname extracts the hostname from a host string, removing the port if present.
// Input is a host string (NOT a full URL), e.g., "example.com:8080" or "example.com".
// Handles IPv6 addresses correctly - does not strip the port portion of an IPv6 literal.
func ExtractHostname(host string) string {
	// Handle bracketed IPv6 addresses: [::1]:8080 or [::1]
	if strings.HasPrefix(host, "[") {
		if bracketIdx := strings.Index(host, "]"); bracketIdx != -1 {
			// Return everything up to and including the closing bracket
			return host[:bracketIdx+1]
		}
		return host
	}
	// For non-bracketed hosts, only strip port if there's exactly one colon
	// This handles: example.com:8080 -> example.com
	// But preserves bare IPv6: ::1 -> ::1
	if idx := strings.LastIndex(host, ":"); idx != -1 && strings.Count(host, ":") == 1 {
		return host[:idx]
	}
	return host
}

// IsSameOrigin returns true if hosts are the same domain or one is a subdomain of the other.
// Strips ports before comparison. Both hosts should already be lowercased.
func IsSameOrigin(baseHost, requestHost string) bool {
	if baseHost == "" || requestHost == "" {
		return false
	}

	// Strip ports for comparison
	base := ExtractHostname(baseHost)
	req := ExtractHostname(requestHost)

	if base == req {
		return true
	}
	// Check if requestHost is a subdomain of baseHost
	if strings.HasSuffix(req, "."+base) {
		return true
	}
	// Check if baseHost is a subdomain of requestHost
	if strings.HasSuffix(base, "."+req) {
		return true
	}
	return false
}

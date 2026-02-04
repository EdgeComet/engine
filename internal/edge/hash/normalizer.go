package hash

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/edgecomet/engine/internal/common/config"
)

type URLNormalizer struct {
	preserveFragment bool
	sortQuery        bool
}

// NormalizeResult contains the result of URL normalization
type NormalizeResult struct {
	OriginalURL    string   // URL as provided (before processing)
	NormalizedURL  string   // Final normalized URL (after stripping + normalization)
	StrippedParams []string // Tracking parameters that were removed
	WasModified    bool     // True if tracking params were stripped
}

func NewURLNormalizer() *URLNormalizer {
	return &URLNormalizer{
		preserveFragment: false, // Remove fragments by default
		sortQuery:        true,  // Sort query parameters
	}
}

// Normalize converts URL to canonical form, optionally stripping tracking parameters
func (n *URLNormalizer) Normalize(rawURL string, stripPatterns []config.CompiledStripPattern) (*NormalizeResult, error) {
	// Handle URLs without scheme by prepending https://
	if !strings.Contains(rawURL, "://") && !strings.HasPrefix(rawURL, "//") {
		rawURL = "https://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Validate host is not empty and looks like a valid domain
	if u.Host == "" {
		return nil, fmt.Errorf("invalid URL: missing host")
	}
	// Host should contain at least one dot (for domain.tld) OR be localhost
	// Use Hostname() to strip port for validation
	hostname := u.Hostname()
	if !strings.Contains(hostname, ".") && hostname != "localhost" {
		return nil, fmt.Errorf("invalid URL: invalid host '%s'", u.Host)
	}

	// Initialize result
	result := &NormalizeResult{
		OriginalURL:    rawURL,
		StrippedParams: []string{},
		WasModified:    false,
	}

	// Strip tracking parameters if patterns provided
	if len(stripPatterns) > 0 {
		query := u.Query()
		originalCount := len(query)

		// Iterate through parameters and check against patterns
		for paramName := range query {
			if shouldStripParam(paramName, stripPatterns) {
				query.Del(paramName)
				result.StrippedParams = append(result.StrippedParams, paramName)
			}
		}

		// Update result
		result.WasModified = len(query) < originalCount

		// Rebuild URL with remaining query parameters
		u.RawQuery = query.Encode()
	}

	// Normalize scheme
	u.Scheme = strings.ToLower(u.Scheme)

	// Normalize host
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimSuffix(u.Host, ".")

	// Remove default ports
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}

	// Normalize path
	if u.Path == "" {
		u.Path = "/"
	}
	u.Path = normalizePath(u.Path)

	// Normalize query
	if n.sortQuery {
		u.RawQuery = NormalizeQuery(u.RawQuery)
	}

	// Remove fragment unless preserved
	if !n.preserveFragment {
		u.Fragment = ""
	}

	result.NormalizedURL = u.String()

	return result, nil
}

// Hash generates XXHash64 of normalized URL
func (n *URLNormalizer) Hash(normalizedURL string) string {
	h := xxhash.Sum64String(normalizedURL)
	return fmt.Sprintf("%016x", h)
}

// shouldStripParam delegates to config package for pattern matching
func shouldStripParam(paramName string, patterns []config.CompiledStripPattern) bool {
	return config.ShouldStripParam(paramName, patterns)
}

func normalizePath(path string) string {
	// Remove duplicate slashes
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	// Resolve relative paths
	parts := strings.Split(path, "/")
	var resolved []string

	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) > 0 && resolved[len(resolved)-1] != ".." {
				resolved = resolved[:len(resolved)-1]
			}
		default:
			resolved = append(resolved, part)
		}
	}

	result := "/" + strings.Join(resolved, "/")
	if len(result) > 1 && strings.HasSuffix(path, "/") {
		result += "/"
	}

	return result
}

// NormalizeQuery sorts and normalizes URL query parameters for consistent ordering
// This ensures that URLs with the same query params in different order are treated identically
func NormalizeQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery // Return original if parsing fails
	}

	// Sort keys for consistent ordering
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Rebuild query string
	var parts []string
	for _, key := range keys {
		for _, value := range values[key] {
			if value == "" {
				parts = append(parts, url.QueryEscape(key))
			} else {
				parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
			}
		}
	}

	return strings.Join(parts, "&")
}

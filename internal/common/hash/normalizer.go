package hash

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

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
	// FetchURL is the URL to send to the origin. When FetchExact is true it carries the received
	// path spelling with NormalizedURL's scheme, host and query; otherwise it equals NormalizedURL.
	FetchURL string
	// FetchExact reports that FetchURL preserves the received spelling and must be sent with the
	// exact (non-normalizing) bypass client.
	FetchExact bool
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

	// RawPath holds the received spelling whenever it differs from Go's default encoding; when it
	// is empty the received spelling is EscapedPath.
	receivedPath := u.RawPath
	if receivedPath == "" {
		receivedPath = u.EscapedPath()
	}
	if receivedPath == "" {
		receivedPath = "/"
	}
	decodedPath := u.Path
	if decodedPath == "" {
		decodedPath = "/"
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

	// Store the path in its readable Unicode form: percent-encoding in the path is
	// decoded (e.g. /%D0%BD%D0%BE -> /но) so the persisted/hashed URL matches across
	// encoded and decoded inputs and is human-readable. Falls back to the escaped form
	// when the path is malformed or would decode to invalid UTF-8.
	result.NormalizedURL = decodeURLPathUnicode(u)

	result.FetchURL = result.NormalizedURL
	if isExactFetchPath(receivedPath, decodedPath) {
		result.FetchURL = buildURL(u, decodeUnreservedEscapes(receivedPath))
		result.FetchExact = true
	}

	return result, nil
}

// decodeURLPathUnicode builds the normalized URL with a readable Unicode path. We cannot
// use u.String here: it always percent-encodes non-ASCII path bytes (via EscapedPath)
// with no option to disable, which is exactly what we want to avoid. u.Path is already
// percent-decoded by url.Parse, so we emit it directly; the escaped path is used only
// when u.Path is not valid UTF-8 (e.g. %FF byte runs), so a path that cannot be
// represented in Unicode never produces a broken stored string. Fragments are already
// stripped before this point, so they need no handling here.
func decodeURLPathUnicode(u *url.URL) string {
	path := u.Path
	if !utf8.ValidString(path) {
		path = u.EscapedPath()
	}
	return buildURL(u, path)
}

// buildURL writes u with path in place of its own path. Fragments are already stripped.
func buildURL(u *url.URL, path string) string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	if u.User != nil {
		b.WriteString(u.User.String())
		b.WriteByte('@')
	}
	b.WriteString(u.Host)
	b.WriteString(path)
	if u.ForceQuery || u.RawQuery != "" {
		b.WriteByte('?')
		b.WriteString(u.RawQuery)
	}
	return b.String()
}

// isExactFetchPath reports whether the received path can be sent byte for byte: its decoded form
// is already normalized, it has no %2F, %2E, %5C or raw backslash, and every byte is unreserved,
// one of $&+,;=:@/ or part of a %XX escape. Other paths keep the legacy fetch, which resolves
// traversal payloads and keeps raw sub-delims, spaces and non-ASCII on today's wire.
func isExactFetchPath(receivedPath, decodedPath string) bool {
	if normalizePath(decodedPath) != decodedPath {
		return false
	}
	for i := 0; i < len(receivedPath); i++ {
		c := receivedPath[i]
		switch {
		case c == '%':
			if i+2 >= len(receivedPath) || !isHexDigit(receivedPath[i+1]) || !isHexDigit(receivedPath[i+2]) {
				return false
			}
			if isStructuralEscape(receivedPath[i+1], receivedPath[i+2]) {
				return false
			}
			i += 2
		case isUnreserved(c) || strings.IndexByte(exactPathDelimiters, c) >= 0:
		default:
			return false
		}
	}
	return true
}

const (
	// exactPathDelimiters are the reserved bytes both transports already send literal in a path.
	exactPathDelimiters = "$&+,;=:@/"
	hexLetterValue      = 10
)

// isStructuralEscape reports %2F, %2E and %5C in either hex case.
func isStructuralEscape(hi, lo byte) bool {
	switch unhex(hi)<<4 | unhex(lo) {
	case '/', '.', '\\':
		return true
	}
	return false
}

// decodeUnreservedEscapes decodes escapes of unreserved characters (%41 -> A) and keeps every
// other escape with its hex case. The normalizer decodes them for the cache key too, so the
// origin receives the same bytes as today.
func decodeUnreservedEscapes(path string) string {
	if strings.IndexByte(path, '%') < 0 {
		return path
	}
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '%' && i+2 < len(path) {
			if c := unhex(path[i+1])<<4 | unhex(path[i+2]); isUnreserved(c) {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + hexLetterValue
	case 'A' <= c && c <= 'F':
		return c - 'A' + hexLetterValue
	}
	return 0
}

// Hash generates XXHash64 of normalized URL
func (n *URLNormalizer) Hash(normalizedURL string) uint64 {
	return xxhash.Sum64String(normalizedURL)
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

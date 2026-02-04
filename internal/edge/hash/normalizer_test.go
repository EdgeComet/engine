package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/config"
)

func TestURLNormalization(t *testing.T) {
	normalizer := NewURLNormalizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic URL",
			input:    "https://example.com/path",
			expected: "https://example.com/path",
		},
		{
			name:     "uppercase scheme and host",
			input:    "HTTPS://EXAMPLE.COM/Path",
			expected: "https://example.com/Path",
		},
		{
			name:     "default port removal",
			input:    "https://example.com:443/path",
			expected: "https://example.com/path",
		},
		{
			name:     "query parameter sorting",
			input:    "https://example.com/path?c=3&a=1&b=2",
			expected: "https://example.com/path?a=1&b=2&c=3",
		},
		{
			name:     "duplicate slashes",
			input:    "https://example.com//path//to//resource",
			expected: "https://example.com/path/to/resource",
		},
		{
			name:     "relative path resolution",
			input:    "https://example.com/path/../other/./final",
			expected: "https://example.com/other/final",
		},
		{
			name:     "fragment removal",
			input:    "https://example.com/path#fragment",
			expected: "https://example.com/path",
		},
		{
			name:     "empty path normalization",
			input:    "https://example.com",
			expected: "https://example.com/",
		},
		{
			name:     "missing scheme defaults to https",
			input:    "example.com/path",
			expected: "https://example.com/path",
		},
		{
			name:     "http default port removal",
			input:    "http://example.com:80/path",
			expected: "http://example.com/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.Normalize(tt.input, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.NormalizedURL)
		})
	}
}

func TestHashConsistency(t *testing.T) {
	normalizer := NewURLNormalizer()

	url1 := "https://example.com/path?a=1&b=2"
	url2 := "https://example.com/path?b=2&a=1"

	hash1 := normalizer.Hash(url1)
	hash2 := normalizer.Hash(url2)

	// Different order should produce same hash after normalization
	result1, _ := normalizer.Normalize(url1, nil)
	result2, _ := normalizer.Normalize(url2, nil)

	normHash1 := normalizer.Hash(result1.NormalizedURL)
	normHash2 := normalizer.Hash(result2.NormalizedURL)

	assert.Equal(t, normHash1, normHash2, "Normalized URLs should have same hash")
	assert.Len(t, hash1, 16, "Hash should be 16 characters (64-bit hex)")
	assert.NotEqual(t, hash1, hash2, "Non-normalized URLs should have different hashes")
}

// TestHashConsistency_DifferentEncodings validates that URLs with different
// query parameter encodings produce identical normalized URLs and cache keys
func TestHashConsistency_DifferentEncodings(t *testing.T) {
	normalizer := NewURLNormalizer()

	tests := []struct {
		name string
		urls []string
		desc string
	}{
		{
			name: "space encoding variations",
			urls: []string{
				"https://example.com/page?q=hello%20world",
				"https://example.com/page?q=hello+world",
			},
			desc: "Both %20 and + represent spaces and should normalize identically",
		},
		{
			name: "special characters",
			urls: []string{
				"https://example.com/page?ref=https%3A%2F%2Fexample.com",
				"https://example.com/page?ref=https://example.com",
			},
			desc: "Encoded and unencoded special chars should normalize to same result",
		},
		{
			name: "mixed encoding with multiple params",
			urls: []string{
				"https://example.com/page?q=hello%20world&category=tech&sort=desc",
				"https://example.com/page?sort=desc&q=hello+world&category=tech",
			},
			desc: "Different param order and space encoding should produce same hash",
		},
		{
			name: "percent vs plus in complex query",
			urls: []string{
				"https://example.com/search?term=foo%20bar%20baz&filter=active",
				"https://example.com/search?filter=active&term=foo+bar+baz",
			},
			desc: "Spaces as %20 vs + should normalize identically regardless of order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var normalizedURLs []string
			var hashes []string

			for _, url := range tt.urls {
				result, err := normalizer.Normalize(url, nil)
				require.NoError(t, err, "URL: %s", url)
				hash := normalizer.Hash(result.NormalizedURL)

				normalizedURLs = append(normalizedURLs, result.NormalizedURL)
				hashes = append(hashes, hash)
			}

			// All normalized URLs should be identical
			for i := 1; i < len(normalizedURLs); i++ {
				assert.Equal(t, normalizedURLs[0], normalizedURLs[i],
					"%s: URLs should normalize to identical strings\nURL1: %s\nURL2: %s",
					tt.desc, tt.urls[0], tt.urls[i])
			}

			// All hashes should be identical
			for i := 1; i < len(hashes); i++ {
				assert.Equal(t, hashes[0], hashes[i],
					"%s: URLs should produce identical cache keys\nURL1: %s\nURL2: %s",
					tt.desc, tt.urls[0], tt.urls[i])
			}
		})
	}
}

func TestInvalidURL(t *testing.T) {
	normalizer := NewURLNormalizer()

	tests := []struct {
		name        string
		url         string
		expectError string
	}{
		{
			name:        "malformed URL with colon",
			url:         "://invalid",
			expectError: "invalid URL",
		},
		{
			name:        "single word without dot",
			url:         "not-a-valid-url",
			expectError: "invalid host 'not-a-valid-url'",
		},
		{
			name:        "missing scheme and no dot",
			url:         "invalidhost",
			expectError: "invalid host 'invalidhost'",
		},
		{
			name:        "https but no dot in host",
			url:         "https://singleword",
			expectError: "invalid host 'singleword'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizer.Normalize(tt.url, nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestValidHostnames(t *testing.T) {
	normalizer := NewURLNormalizer()

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "localhost",
			url:  "http://localhost/path",
		},
		{
			name: "localhost with port",
			url:  "http://localhost:9081/path",
		},
		{
			name: "localhost without scheme",
			url:  "localhost/path",
		},
		{
			name: "domain with subdomain",
			url:  "sub.example.com/path",
		},
		{
			name: "IP address",
			url:  "192.168.1.1/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizer.Normalize(tt.url, nil)
			assert.NoError(t, err, "URL should be valid: %s", tt.url)
		})
	}
}

func TestPathNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "/path/to/resource",
			expected: "/path/to/resource",
		},
		{
			name:     "duplicate slashes",
			input:    "//path///to//resource",
			expected: "/path/to/resource",
		},
		{
			name:     "dot segments",
			input:    "/path/./to/resource",
			expected: "/path/to/resource",
		},
		{
			name:     "dot-dot segments",
			input:    "/path/../to/resource",
			expected: "/to/resource",
		},
		{
			name:     "complex relative path",
			input:    "/path/sub/../other/./final/../end",
			expected: "/path/other/end",
		},
		{
			name:     "trailing slash preservation",
			input:    "/path/to/resource/",
			expected: "/path/to/resource/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQueryNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty query",
			input:    "",
			expected: "",
		},
		{
			name:     "single parameter",
			input:    "a=1",
			expected: "a=1",
		},
		{
			name:     "multiple parameters",
			input:    "c=3&a=1&b=2",
			expected: "a=1&b=2&c=3",
		},
		{
			name:     "parameter without value",
			input:    "c&a=1&b=2",
			expected: "a=1&b=2&c",
		},
		{
			name:     "url encoded parameters",
			input:    "space=hello%20world&special=%21%40%23",
			expected: "space=hello+world&special=%21%40%23",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeQuery(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeQuery_Exported tests the exported NormalizeQuery function directly
func TestNormalizeQuery_Exported(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic alphabetical sorting",
			input:    "z=1&a=2&m=3",
			expected: "a=2&m=3&z=1",
		},
		{
			name:     "reverse order",
			input:    "b=2&a=1",
			expected: "a=1&b=2",
		},
		{
			name:     "same order unchanged",
			input:    "a=1&b=2&c=3",
			expected: "a=1&b=2&c=3",
		},
		{
			name:     "multiple values same key",
			input:    "a=1&a=2&a=3",
			expected: "a=1&a=2&a=3",
		},
		{
			name:     "empty value",
			input:    "a=&b=2",
			expected: "a&b=2",
		},
		{
			name:     "special characters escaped",
			input:    "q=hello world&ref=https://example.com",
			expected: "q=hello+world&ref=https%3A%2F%2Fexample.com",
		},
		{
			name:     "query with multiple equals signs",
			input:    "invalid===query",
			expected: "invalid=%3D%3Dquery", // Parsed as key="invalid", value="==query"
		},
		{
			name:     "empty query",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeQuery(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeQuery_ConsistencyWithMatcher tests that NormalizeQuery produces
// consistent results for pattern matching in config matcher
func TestNormalizeQuery_ConsistencyWithMatcher(t *testing.T) {
	// This test ensures that the exported NormalizeQuery is used consistently
	// in both URL hashing and pattern matching contexts

	tests := []struct {
		name   string
		query1 string
		query2 string
		equal  bool
	}{
		{
			name:   "different order same params",
			query1: "a=1&b=2&c=3",
			query2: "c=3&a=1&b=2",
			equal:  true,
		},
		{
			name:   "different params",
			query1: "a=1&b=2",
			query2: "a=1&c=3",
			equal:  false,
		},
		{
			name:   "different values",
			query1: "a=1&b=2",
			query2: "a=2&b=1",
			equal:  false,
		},
		{
			name:   "extra param",
			query1: "a=1&b=2",
			query2: "a=1&b=2&c=3",
			equal:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			norm1 := NormalizeQuery(tt.query1)
			norm2 := NormalizeQuery(tt.query2)

			if tt.equal {
				assert.Equal(t, norm1, norm2, "Queries should normalize to the same value")
			} else {
				assert.NotEqual(t, norm1, norm2, "Queries should normalize to different values")
			}
		})
	}
}

func BenchmarkNormalization(b *testing.B) {
	normalizer := NewURLNormalizer()
	url := "https://example.com/path/to/resource?param1=value1&param2=value2&param3=value3"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalizer.Normalize(url, nil)
	}
}

func BenchmarkHashing(b *testing.B) {
	normalizer := NewURLNormalizer()
	url := "https://example.com/path/to/resource?param1=value1&param2=value2&param3=value3"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = normalizer.Hash(url)
	}
}

func BenchmarkNormalizeAndHash(b *testing.B) {
	normalizer := NewURLNormalizer()
	url := "https://example.com/path/to/resource?param1=value1&param2=value2&param3=value3"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ := normalizer.Normalize(url, nil)
		_ = normalizer.Hash(result.NormalizedURL)
	}
}

// TestStripTrackingParams_Basic tests basic parameter stripping
func TestStripTrackingParams_Basic(t *testing.T) {
	normalizer := NewURLNormalizer()
	// Compile patterns for utm_source only
	patterns, err := config.CompileStripPatterns([]string{"utm_source"})
	require.NoError(t, err)

	tests := []struct {
		name             string
		url              string
		expectedURL      string
		expectedStripped []string
		expectedModified bool
	}{
		{
			name:             "single param stripped",
			url:              "https://example.com/page?utm_source=google",
			expectedURL:      "https://example.com/page",
			expectedStripped: []string{"utm_source"},
			expectedModified: true,
		},
		{
			name:             "single param stripped with others preserved",
			url:              "https://example.com/page?utm_source=google&product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedStripped: []string{"utm_source"},
			expectedModified: true,
		},
		{
			name:             "no params to strip",
			url:              "https://example.com/page?product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedStripped: []string{},
			expectedModified: false,
		},
		{
			name:             "no query params",
			url:              "https://example.com/page",
			expectedURL:      "https://example.com/page",
			expectedStripped: []string{},
			expectedModified: false,
		},
		{
			name:             "trailing question mark removed",
			url:              "https://example.com/page?utm_source=google",
			expectedURL:      "https://example.com/page",
			expectedStripped: []string{"utm_source"},
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.Normalize(tt.url, patterns)
			require.NoError(t, err)

			assert.Equal(t, tt.url, result.OriginalURL)
			assert.Equal(t, tt.expectedURL, result.NormalizedURL)
			assert.ElementsMatch(t, tt.expectedStripped, result.StrippedParams)
			assert.Equal(t, tt.expectedModified, result.WasModified)
		})
	}
}

// TestStripTrackingParams_MultipleParams tests stripping multiple parameters
func TestStripTrackingParams_MultipleParams(t *testing.T) {
	normalizer := NewURLNormalizer()
	// Compile patterns for multiple UTM params
	patterns, err := config.CompileStripPatterns([]string{"utm_source", "utm_medium", "utm_campaign", "gclid", "fbclid"})
	require.NoError(t, err)

	tests := []struct {
		name             string
		url              string
		expectedURL      string
		expectedCount    int
		expectedModified bool
	}{
		{
			name:             "multiple params stripped",
			url:              "https://example.com/page?utm_source=google&utm_medium=cpc&utm_campaign=spring&product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedCount:    3,
			expectedModified: true,
		},
		{
			name:             "all params stripped",
			url:              "https://example.com/page?utm_source=google&gclid=abc123",
			expectedURL:      "https://example.com/page",
			expectedCount:    2,
			expectedModified: true,
		},
		{
			name:             "mixed tracking and non-tracking params",
			url:              "https://example.com/page?fbclid=xyz&product=123&utm_source=facebook&category=tech",
			expectedURL:      "https://example.com/page?category=tech&product=123",
			expectedCount:    2,
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.Normalize(tt.url, patterns)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedURL, result.NormalizedURL)
			assert.Len(t, result.StrippedParams, tt.expectedCount)
			assert.Equal(t, tt.expectedModified, result.WasModified)
		})
	}
}

// TestStripTrackingParams_WildcardPatterns tests wildcard pattern matching
func TestStripTrackingParams_WildcardPatterns(t *testing.T) {
	normalizer := NewURLNormalizer()
	// Use wildcard patterns
	patterns, err := config.CompileStripPatterns([]string{"utm_*", "ga_*"})
	require.NoError(t, err)

	tests := []struct {
		name             string
		url              string
		expectedURL      string
		expectedModified bool
	}{
		{
			name:             "utm_* matches all utm params",
			url:              "https://example.com/page?utm_source=x&utm_medium=y&utm_campaign=z&product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedModified: true,
		},
		{
			name:             "ga_* matches all ga params",
			url:              "https://example.com/page?ga_session=abc&ga_client=def&product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedModified: true,
		},
		{
			name:             "mixed utm and ga params",
			url:              "https://example.com/page?utm_source=x&ga_session=y&product=123",
			expectedURL:      "https://example.com/page?product=123",
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.Normalize(tt.url, patterns)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedURL, result.NormalizedURL)
			assert.Equal(t, tt.expectedModified, result.WasModified)
		})
	}
}

// TestStripTrackingParams_CaseInsensitive tests case-insensitive matching
func TestStripTrackingParams_CaseInsensitive(t *testing.T) {
	normalizer := NewURLNormalizer()
	// Compile patterns - exact and wildcard should be case-insensitive
	patterns, err := config.CompileStripPatterns([]string{"utm_source", "utm_*"})
	require.NoError(t, err)

	tests := []struct {
		name             string
		url              string
		expectedModified bool
		description      string
	}{
		{
			name:             "UTM_SOURCE uppercase",
			url:              "https://example.com/page?UTM_SOURCE=google&product=123",
			expectedModified: true,
			description:      "Uppercase UTM_SOURCE should be stripped",
		},
		{
			name:             "Utm_Source mixed case",
			url:              "https://example.com/page?Utm_Source=google&product=123",
			expectedModified: true,
			description:      "Mixed case Utm_Source should be stripped",
		},
		{
			name:             "UTM_CAMPAIGN matches utm_*",
			url:              "https://example.com/page?UTM_CAMPAIGN=spring&product=123",
			expectedModified: true,
			description:      "Uppercase UTM_CAMPAIGN should match utm_* pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.Normalize(tt.url, patterns)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedModified, result.WasModified, tt.description)
			if tt.expectedModified {
				assert.NotContains(t, result.NormalizedURL, "utm", "URL should not contain utm parameters")
				assert.NotContains(t, result.NormalizedURL, "UTM", "URL should not contain UTM parameters")
			}
		})
	}
}

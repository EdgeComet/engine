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
			var hashes []uint64

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

// TestPathUnicodeDecoding validates that percent-encoded UTF-8 in the path is stored
// in its readable Unicode form, that encoded and decoded inputs normalize identically
// (same string and hash), and that malformed or non-UTF-8 paths keep the escaped form.
func TestPathUnicodeDecoding(t *testing.T) {
	normalizer := NewURLNormalizer()

	t.Run("normalized form", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "cyrillic encoded decodes to unicode",
				input:    "https://example.com/%D0%BD%D0%BE%D0%B2%D0%B0%D1%8F",
				expected: "https://example.com/новая",
			},
			{
				name:     "cyrillic literal stays unicode",
				input:    "https://example.com/новая",
				expected: "https://example.com/новая",
			},
			{
				name:     "accented latin decodes",
				input:    "https://example.com/caf%C3%A9",
				expected: "https://example.com/café",
			},
			{
				name:     "invalid utf8 stays escaped",
				input:    "https://example.com/a%FFb",
				expected: "https://example.com/a%FFb",
			},
			{
				name:     "encoded query is not decoded",
				input:    "https://example.com/новая?q=%D0%BD",
				expected: "https://example.com/новая?q=%D0%BD",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := normalizer.Normalize(tt.input, nil)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result.NormalizedURL)
			})
		}
	})

	t.Run("encoded and decoded inputs share one cache key", func(t *testing.T) {
		encoded := "https://example.com/%D0%BD%D0%BE%D0%B2%D0%B0%D1%8F-%D1%81%D1%82%D1%80%D0%B0%D0%BD%D0%B8%D1%86%D0%B0"
		decoded := "https://example.com/новая-страница"

		re, err := normalizer.Normalize(encoded, nil)
		require.NoError(t, err)
		rd, err := normalizer.Normalize(decoded, nil)
		require.NoError(t, err)

		assert.Equal(t, rd.NormalizedURL, re.NormalizedURL)
		assert.Equal(t, normalizer.Hash(rd.NormalizedURL), normalizer.Hash(re.NormalizedURL))
	})
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

// TestNormalizeFrozen pins NormalizedURL and the hash, both of which key every cache entry in
// production. Expected values were generated once from the normalizer and must never be
// regenerated: a changed row orphans every cached entry of that URL shape.
func TestNormalizeFrozen(t *testing.T) {
	patterns, err := config.CompileStripPatterns([]string{"utm_*", "gclid"})
	require.NoError(t, err)
	normalizer := NewURLNormalizer()

	tests := []struct {
		input      string
		strip      bool
		normalized string
		hash       uint64
		wantErr    bool
	}{
		{input: "https://example.com/p%20q", strip: false, normalized: "https://example.com/p q", hash: 11128299243850293701},
		{input: "https://example.com/p%21q", strip: false, normalized: "https://example.com/p!q", hash: 1203222061682795741},
		{input: "https://example.com/p%22q", strip: false, normalized: "https://example.com/p\"q", hash: 16190004269989876625},
		{input: "https://example.com/p%23q", strip: false, normalized: "https://example.com/p#q", hash: 1856375285874812001},
		{input: "https://example.com/p%24q", strip: false, normalized: "https://example.com/p$q", hash: 300647914351296559},
		{input: "https://example.com/p%25q", strip: false, normalized: "https://example.com/p%q", hash: 15881077465922050902},
		{input: "https://example.com/p%26q", strip: false, normalized: "https://example.com/p&q", hash: 1934433113232973106},
		{input: "https://example.com/p%27q", strip: false, normalized: "https://example.com/p'q", hash: 4518779726088642196},
		{input: "https://example.com/p%28q", strip: false, normalized: "https://example.com/p(q", hash: 1348614337859463715},
		{input: "https://example.com/p%29q", strip: false, normalized: "https://example.com/p)q", hash: 14460012966406702863},
		{input: "https://example.com/p%2Aq", strip: false, normalized: "https://example.com/p*q", hash: 11779053136235720486},
		{input: "https://example.com/p%2aq", strip: false, normalized: "https://example.com/p*q", hash: 11779053136235720486},
		{input: "https://example.com/p%2Bq", strip: false, normalized: "https://example.com/p+q", hash: 4064929577917683086},
		{input: "https://example.com/p%2bq", strip: false, normalized: "https://example.com/p+q", hash: 4064929577917683086},
		{input: "https://example.com/p%2Cq", strip: false, normalized: "https://example.com/p,q", hash: 11796665091895408513},
		{input: "https://example.com/p%2cq", strip: false, normalized: "https://example.com/p,q", hash: 11796665091895408513},
		{input: "https://example.com/p%2Dq", strip: false, normalized: "https://example.com/p-q", hash: 15243885653940839712},
		{input: "https://example.com/p%2dq", strip: false, normalized: "https://example.com/p-q", hash: 15243885653940839712},
		{input: "https://example.com/p%2Eq", strip: false, normalized: "https://example.com/p.q", hash: 9387296249652656992},
		{input: "https://example.com/p%2eq", strip: false, normalized: "https://example.com/p.q", hash: 9387296249652656992},
		{input: "https://example.com/p%2Fq", strip: false, normalized: "https://example.com/p/q", hash: 13483539978463846453},
		{input: "https://example.com/p%2fq", strip: false, normalized: "https://example.com/p/q", hash: 13483539978463846453},
		{input: "https://example.com/p%30q", strip: false, normalized: "https://example.com/p0q", hash: 906878760870422454},
		{input: "https://example.com/p%31q", strip: false, normalized: "https://example.com/p1q", hash: 14385757742941422998},
		{input: "https://example.com/p%32q", strip: false, normalized: "https://example.com/p2q", hash: 14377928122176068192},
		{input: "https://example.com/p%33q", strip: false, normalized: "https://example.com/p3q", hash: 14969374889917124150},
		{input: "https://example.com/p%34q", strip: false, normalized: "https://example.com/p4q", hash: 9233850158733789352},
		{input: "https://example.com/p%35q", strip: false, normalized: "https://example.com/p5q", hash: 63190444013549125},
		{input: "https://example.com/p%36q", strip: false, normalized: "https://example.com/p6q", hash: 1076924614822035383},
		{input: "https://example.com/p%37q", strip: false, normalized: "https://example.com/p7q", hash: 4005413003874035598},
		{input: "https://example.com/p%38q", strip: false, normalized: "https://example.com/p8q", hash: 16161066509409329706},
		{input: "https://example.com/p%39q", strip: false, normalized: "https://example.com/p9q", hash: 13328247995925735249},
		{input: "https://example.com/p%3Aq", strip: false, normalized: "https://example.com/p:q", hash: 6225288725668282602},
		{input: "https://example.com/p%3aq", strip: false, normalized: "https://example.com/p:q", hash: 6225288725668282602},
		{input: "https://example.com/p%3Bq", strip: false, normalized: "https://example.com/p;q", hash: 2168697672650802583},
		{input: "https://example.com/p%3bq", strip: false, normalized: "https://example.com/p;q", hash: 2168697672650802583},
		{input: "https://example.com/p%3Cq", strip: false, normalized: "https://example.com/p<q", hash: 5934441433829591334},
		{input: "https://example.com/p%3cq", strip: false, normalized: "https://example.com/p<q", hash: 5934441433829591334},
		{input: "https://example.com/p%3Dq", strip: false, normalized: "https://example.com/p=q", hash: 3509365858636012264},
		{input: "https://example.com/p%3dq", strip: false, normalized: "https://example.com/p=q", hash: 3509365858636012264},
		{input: "https://example.com/p%3Eq", strip: false, normalized: "https://example.com/p>q", hash: 14134200154475769491},
		{input: "https://example.com/p%3eq", strip: false, normalized: "https://example.com/p>q", hash: 14134200154475769491},
		{input: "https://example.com/p%3Fq", strip: false, normalized: "https://example.com/p?q", hash: 8563293864957262708},
		{input: "https://example.com/p%3fq", strip: false, normalized: "https://example.com/p?q", hash: 8563293864957262708},
		{input: "https://example.com/p%40q", strip: false, normalized: "https://example.com/p@q", hash: 1585118419430820612},
		{input: "https://example.com/p%41q", strip: false, normalized: "https://example.com/pAq", hash: 2046413217411058182},
		{input: "https://example.com/p%42q", strip: false, normalized: "https://example.com/pBq", hash: 11401889718267649117},
		{input: "https://example.com/p%43q", strip: false, normalized: "https://example.com/pCq", hash: 14323770798271492184},
		{input: "https://example.com/p%44q", strip: false, normalized: "https://example.com/pDq", hash: 1268508508998795801},
		{input: "https://example.com/p%45q", strip: false, normalized: "https://example.com/pEq", hash: 11048640730670755424},
		{input: "https://example.com/p%46q", strip: false, normalized: "https://example.com/pFq", hash: 18089627863450506975},
		{input: "https://example.com/p%47q", strip: false, normalized: "https://example.com/pGq", hash: 12932149636410167340},
		{input: "https://example.com/p%48q", strip: false, normalized: "https://example.com/pHq", hash: 9918821124659781114},
		{input: "https://example.com/p%49q", strip: false, normalized: "https://example.com/pIq", hash: 12418550359393873114},
		{input: "https://example.com/p%4Aq", strip: false, normalized: "https://example.com/pJq", hash: 11006624210639276123},
		{input: "https://example.com/p%4aq", strip: false, normalized: "https://example.com/pJq", hash: 11006624210639276123},
		{input: "https://example.com/p%4Bq", strip: false, normalized: "https://example.com/pKq", hash: 11509009868134693730},
		{input: "https://example.com/p%4bq", strip: false, normalized: "https://example.com/pKq", hash: 11509009868134693730},
		{input: "https://example.com/p%4Cq", strip: false, normalized: "https://example.com/pLq", hash: 17296480970571048226},
		{input: "https://example.com/p%4cq", strip: false, normalized: "https://example.com/pLq", hash: 17296480970571048226},
		{input: "https://example.com/p%4Dq", strip: false, normalized: "https://example.com/pMq", hash: 9670887089126979377},
		{input: "https://example.com/p%4dq", strip: false, normalized: "https://example.com/pMq", hash: 9670887089126979377},
		{input: "https://example.com/p%4Eq", strip: false, normalized: "https://example.com/pNq", hash: 14698824953255375661},
		{input: "https://example.com/p%4eq", strip: false, normalized: "https://example.com/pNq", hash: 14698824953255375661},
		{input: "https://example.com/p%4Fq", strip: false, normalized: "https://example.com/pOq", hash: 17813495636866105251},
		{input: "https://example.com/p%4fq", strip: false, normalized: "https://example.com/pOq", hash: 17813495636866105251},
		{input: "https://example.com/p%50q", strip: false, normalized: "https://example.com/pPq", hash: 17368298373100861408},
		{input: "https://example.com/p%51q", strip: false, normalized: "https://example.com/pQq", hash: 7249396856254521548},
		{input: "https://example.com/p%52q", strip: false, normalized: "https://example.com/pRq", hash: 2450532659758592660},
		{input: "https://example.com/p%53q", strip: false, normalized: "https://example.com/pSq", hash: 15984609884027497830},
		{input: "https://example.com/p%54q", strip: false, normalized: "https://example.com/pTq", hash: 5947905199513958160},
		{input: "https://example.com/p%55q", strip: false, normalized: "https://example.com/pUq", hash: 14776683482266385052},
		{input: "https://example.com/p%56q", strip: false, normalized: "https://example.com/pVq", hash: 14487715342964440750},
		{input: "https://example.com/p%57q", strip: false, normalized: "https://example.com/pWq", hash: 1540924774322021791},
		{input: "https://example.com/p%58q", strip: false, normalized: "https://example.com/pXq", hash: 14124317921129041429},
		{input: "https://example.com/p%59q", strip: false, normalized: "https://example.com/pYq", hash: 12564514596276001602},
		{input: "https://example.com/p%5Aq", strip: false, normalized: "https://example.com/pZq", hash: 3280787563505798208},
		{input: "https://example.com/p%5aq", strip: false, normalized: "https://example.com/pZq", hash: 3280787563505798208},
		{input: "https://example.com/p%5Bq", strip: false, normalized: "https://example.com/p[q", hash: 9004118319533529690},
		{input: "https://example.com/p%5bq", strip: false, normalized: "https://example.com/p[q", hash: 9004118319533529690},
		{input: "https://example.com/p%5Cq", strip: false, normalized: "https://example.com/p\\q", hash: 5182387916630718816},
		{input: "https://example.com/p%5cq", strip: false, normalized: "https://example.com/p\\q", hash: 5182387916630718816},
		{input: "https://example.com/p%5Dq", strip: false, normalized: "https://example.com/p]q", hash: 16324985670366431135},
		{input: "https://example.com/p%5dq", strip: false, normalized: "https://example.com/p]q", hash: 16324985670366431135},
		{input: "https://example.com/p%5Eq", strip: false, normalized: "https://example.com/p^q", hash: 18318168665894059620},
		{input: "https://example.com/p%5eq", strip: false, normalized: "https://example.com/p^q", hash: 18318168665894059620},
		{input: "https://example.com/p%5Fq", strip: false, normalized: "https://example.com/p_q", hash: 7075546281040275656},
		{input: "https://example.com/p%5fq", strip: false, normalized: "https://example.com/p_q", hash: 7075546281040275656},
		{input: "https://example.com/p%60q", strip: false, normalized: "https://example.com/p`q", hash: 4148692337096643038},
		{input: "https://example.com/p%61q", strip: false, normalized: "https://example.com/paq", hash: 10514270490497148164},
		{input: "https://example.com/p%62q", strip: false, normalized: "https://example.com/pbq", hash: 16589728370800441959},
		{input: "https://example.com/p%63q", strip: false, normalized: "https://example.com/pcq", hash: 4935912923097312182},
		{input: "https://example.com/p%64q", strip: false, normalized: "https://example.com/pdq", hash: 14145791485570430564},
		{input: "https://example.com/p%65q", strip: false, normalized: "https://example.com/peq", hash: 4561952963904214136},
		{input: "https://example.com/p%66q", strip: false, normalized: "https://example.com/pfq", hash: 18366061726765622338},
		{input: "https://example.com/p%67q", strip: false, normalized: "https://example.com/pgq", hash: 17982723393895570840},
		{input: "https://example.com/p%68q", strip: false, normalized: "https://example.com/phq", hash: 1297907140229536433},
		{input: "https://example.com/p%69q", strip: false, normalized: "https://example.com/piq", hash: 9514936731248991878},
		{input: "https://example.com/p%6Aq", strip: false, normalized: "https://example.com/pjq", hash: 13107595489354415675},
		{input: "https://example.com/p%6aq", strip: false, normalized: "https://example.com/pjq", hash: 13107595489354415675},
		{input: "https://example.com/p%6Bq", strip: false, normalized: "https://example.com/pkq", hash: 14745436073969889298},
		{input: "https://example.com/p%6bq", strip: false, normalized: "https://example.com/pkq", hash: 14745436073969889298},
		{input: "https://example.com/p%6Cq", strip: false, normalized: "https://example.com/plq", hash: 1517410281553057666},
		{input: "https://example.com/p%6cq", strip: false, normalized: "https://example.com/plq", hash: 1517410281553057666},
		{input: "https://example.com/p%6Dq", strip: false, normalized: "https://example.com/pmq", hash: 428228192264488898},
		{input: "https://example.com/p%6dq", strip: false, normalized: "https://example.com/pmq", hash: 428228192264488898},
		{input: "https://example.com/p%6Eq", strip: false, normalized: "https://example.com/pnq", hash: 2491096400650280492},
		{input: "https://example.com/p%6eq", strip: false, normalized: "https://example.com/pnq", hash: 2491096400650280492},
		{input: "https://example.com/p%6Fq", strip: false, normalized: "https://example.com/poq", hash: 11168633909690485977},
		{input: "https://example.com/p%6fq", strip: false, normalized: "https://example.com/poq", hash: 11168633909690485977},
		{input: "https://example.com/p%70q", strip: false, normalized: "https://example.com/ppq", hash: 14530927651745909590},
		{input: "https://example.com/p%71q", strip: false, normalized: "https://example.com/pqq", hash: 17244671631085890068},
		{input: "https://example.com/p%72q", strip: false, normalized: "https://example.com/prq", hash: 9079632416790076469},
		{input: "https://example.com/p%73q", strip: false, normalized: "https://example.com/psq", hash: 12109792876345619872},
		{input: "https://example.com/p%74q", strip: false, normalized: "https://example.com/ptq", hash: 5870132174351748131},
		{input: "https://example.com/p%75q", strip: false, normalized: "https://example.com/puq", hash: 3566950628400607208},
		{input: "https://example.com/p%76q", strip: false, normalized: "https://example.com/pvq", hash: 1769650602666026462},
		{input: "https://example.com/p%77q", strip: false, normalized: "https://example.com/pwq", hash: 7136264647796809634},
		{input: "https://example.com/p%78q", strip: false, normalized: "https://example.com/pxq", hash: 13726874425679340844},
		{input: "https://example.com/p%79q", strip: false, normalized: "https://example.com/pyq", hash: 9498754161040785009},
		{input: "https://example.com/p%7Aq", strip: false, normalized: "https://example.com/pzq", hash: 3651019753450144889},
		{input: "https://example.com/p%7aq", strip: false, normalized: "https://example.com/pzq", hash: 3651019753450144889},
		{input: "https://example.com/p%7Bq", strip: false, normalized: "https://example.com/p{q", hash: 5277115381652064269},
		{input: "https://example.com/p%7bq", strip: false, normalized: "https://example.com/p{q", hash: 5277115381652064269},
		{input: "https://example.com/p%7Cq", strip: false, normalized: "https://example.com/p|q", hash: 14477845541764900033},
		{input: "https://example.com/p%7cq", strip: false, normalized: "https://example.com/p|q", hash: 14477845541764900033},
		{input: "https://example.com/p%7Dq", strip: false, normalized: "https://example.com/p}q", hash: 12752643830820526931},
		{input: "https://example.com/p%7dq", strip: false, normalized: "https://example.com/p}q", hash: 12752643830820526931},
		{input: "https://example.com/p%7Eq", strip: false, normalized: "https://example.com/p~q", hash: 1117337039526897191},
		{input: "https://example.com/p%7eq", strip: false, normalized: "https://example.com/p~q", hash: 1117337039526897191},
		{input: "https://example.com/caf%C3%A9", strip: false, normalized: "https://example.com/café", hash: 13859778274269477983},
		{input: "https://example.com/caf%c3%a9", strip: false, normalized: "https://example.com/café", hash: 13859778274269477983},
		{input: "https://example.com/café", strip: false, normalized: "https://example.com/café", hash: 13859778274269477983},
		{input: "https://example.com/%D0%BD%D0%BE", strip: false, normalized: "https://example.com/но", hash: 4777825998067685962},
		{input: "https://example.com/a%FFb", strip: false, normalized: "https://example.com/a%FFb", hash: 4193949163077948346},
		{input: "https://example.com/%ff", strip: false, normalized: "https://example.com/%ff", hash: 18380065188885885386},
		{input: "https://example.com/a//b", strip: false, normalized: "https://example.com/a/b", hash: 6933594335711966570},
		{input: "https://example.com/a/./b/../c", strip: false, normalized: "https://example.com/a/c", hash: 12473623051786149115},
		{input: "https://example.com/a/%2e%2e/b", strip: false, normalized: "https://example.com/b", hash: 2028557209270044821},
		{input: "https://example.com/a/%2E/b", strip: false, normalized: "https://example.com/a/b", hash: 6933594335711966570},
		{input: "https://example.com/api%2F.env", strip: false, normalized: "https://example.com/api/.env", hash: 1454636270053975716},
		{input: "https://example.com/a%2fb", strip: false, normalized: "https://example.com/a/b", hash: 6933594335711966570},
		{input: "https://example.com/a%3Fb", strip: false, normalized: "https://example.com/a?b", hash: 10779737019557205518},
		{input: "https://example.com/a%3Fb?page=2", strip: false, normalized: "https://example.com/a?b?page=2", hash: 4737014102051201524},
		{input: "https://example.com/a%23b", strip: false, normalized: "https://example.com/a#b", hash: 6047842865012955794},
		{input: "https://example.com/50%2541", strip: false, normalized: "https://example.com/50%41", hash: 5139369313150303531},
		{input: "https://example.com/50%25zz", strip: false, normalized: "https://example.com/50%zz", hash: 16267298345684934418},
		{input: "https://example.com/%252C", strip: false, normalized: "https://example.com/%2C", hash: 1474723081997833893},
		{input: "https://example.com/p%0A", strip: false, normalized: "https://example.com/p\n", hash: 1256996947955515603},
		{input: "https://example.com/p%0D", strip: false, normalized: "https://example.com/p\r", hash: 784602237453051543},
		{input: "https://example.com/p%09", strip: false, normalized: "https://example.com/p\t", hash: 17401100589639503130},
		{input: "https://example.com/p%00", strip: false, normalized: "https://example.com/p\x00", hash: 7692022936065092329},
		{input: "https://example.com/p?utm_source=x&a=1", strip: true, normalized: "https://example.com/p?a=1", hash: 17212736928487900873},
		{input: "https://example.com/p?gclid=1&utm_medium=y", strip: true, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "https://example.com/p?utm_source=x&a=1", strip: false, normalized: "https://example.com/p?a=1&utm_source=x", hash: 17583161400586318570},
		{input: "https://example.com/p?b=2&a=1", strip: false, normalized: "https://example.com/p?a=1&b=2", hash: 1026708374973747941},
		{input: "https://example.com/p?b=2&a=1,2&c=", strip: false, normalized: "https://example.com/p?a=1%2C2&b=2&c", hash: 7151258930350159567},
		{input: "https://example.com/p?a=2&a=1", strip: false, normalized: "https://example.com/p?a=2&a=1", hash: 1680990393179525992},
		{input: "https://example.com", strip: false, normalized: "https://example.com/", hash: 11821315579082154447},
		{input: "https://example.com?x=1", strip: false, normalized: "https://example.com/?x=1", hash: 17117722092441999065},
		{input: "https://example.com/?", strip: false, normalized: "https://example.com/?", hash: 9654869809318601790},
		{input: "https://example.com/p?", strip: false, normalized: "https://example.com/p?", hash: 10679154270464684996},
		{input: "https://example.com:443/p", strip: false, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "http://example.com:80/p", strip: false, normalized: "http://example.com/p", hash: 12701096838139358341},
		{input: "http://example.com:8080/p", strip: false, normalized: "http://example.com:8080/p", hash: 5421491262940249347},
		{input: "https://example.com:8443/p", strip: false, normalized: "https://example.com:8443/p", hash: 9916779480483371075},
		{input: "https://EXAMPLE.COM/Path", strip: false, normalized: "https://example.com/Path", hash: 12508133902139292356},
		{input: "HTTPS://Example.Com/p", strip: false, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "https://example.com./p", strip: false, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "https://user:pass@example.com/p", strip: false, normalized: "https://user:pass@example.com/p", hash: 17619311147042443764},
		{input: "https://us%40er@example.com/p", strip: false, normalized: "https://us%40er@example.com/p", hash: 1656063408266305124},
		{input: "http://[::1]:8080/p", strip: false, wantErr: true},
		{input: "http://[2001:db8::1]/p", strip: false, wantErr: true},
		{input: "http://[::ffff:192.0.2.1]/p", strip: false, normalized: "http://[::ffff:192.0.2.1]/p", hash: 473006210767449431},
		{input: "example.com/p", strip: false, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "https://example.com/p#frag", strip: false, normalized: "https://example.com/p", hash: 3139857920743470576},
		{input: "https://example.com/a%2Cb/53", strip: false, normalized: "https://example.com/a,b/53", hash: 6086767947613376438},
		{input: "https://example.com/a%2cb/53", strip: false, normalized: "https://example.com/a,b/53", hash: 6086767947613376438},
		{input: "https://example.com/a,b/53", strip: false, normalized: "https://example.com/a,b/53", hash: 6086767947613376438},
		{input: "https://example.com/a!b'c(d)e*f", strip: false, normalized: "https://example.com/a!b'c(d)e*f", hash: 6256482036344368219},
		{input: "https://example.com/a b", strip: false, normalized: "https://example.com/a b", hash: 10630285503743538962},
		{input: "https://example.com/a\\b", strip: false, normalized: "https://example.com/a\\b", hash: 14245093082557385806},
		{input: "https://example.com/a/", strip: false, normalized: "https://example.com/a/", hash: 14509002799532613585},
		{input: "https://localhost/p", strip: false, normalized: "https://localhost/p", hash: 13170707070216662324},
		{input: "https://localhost:9081/p", strip: false, normalized: "https://localhost:9081/p", hash: 5942143357622008740},
		{input: "http://127.0.0.1:9081/p", strip: false, normalized: "http://127.0.0.1:9081/p", hash: 4424400293002618748},
		{input: "https://meblini.pl/pl/blog/Rozowy-pokoj-dla-dziewczynki-jak-polaczyc-kolory%2C-meble-i-dodatki/53", strip: false, normalized: "https://meblini.pl/pl/blog/Rozowy-pokoj-dla-dziewczynki-jak-polaczyc-kolory,-meble-i-dodatki/53", hash: 540108179028897811},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var stripPatterns []config.CompiledStripPattern
			if tt.strip {
				stripPatterns = patterns
			}
			result, err := normalizer.Normalize(tt.input, stripPatterns)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.normalized, result.NormalizedURL)
			assert.Equal(t, tt.hash, normalizer.Hash(result.NormalizedURL))
		})
	}
}

func TestNormalizeFetchURL(t *testing.T) {
	patterns, err := config.CompileStripPatterns([]string{"utm_source"})
	require.NoError(t, err)
	normalizer := NewURLNormalizer()

	tests := []struct {
		name      string
		input     string
		strip     bool
		wantExact bool
		wantFetch string
	}{
		{name: "reported meblini URL", input: "https://meblini.pl/pl/blog/a%2Cb/53", wantExact: true, wantFetch: "https://meblini.pl/pl/blog/a%2Cb/53"},
		{name: "lowercase hex kept", input: "https://example.com/a%2cb", wantExact: true, wantFetch: "https://example.com/a%2cb"},
		{name: "literal comma", input: "https://example.com/a,b", wantExact: true, wantFetch: "https://example.com/a,b"},
		{name: "literal colon", input: "https://example.com/a:b", wantExact: true, wantFetch: "https://example.com/a:b"},
		{name: "literal plus", input: "https://example.com/a+b", wantExact: true, wantFetch: "https://example.com/a+b"},
		{name: "reserved delimiters", input: "https://example.com/a$b&c;d=e@f", wantExact: true, wantFetch: "https://example.com/a$b&c;d=e@f"},
		{name: "escaped UTF-8", input: "https://example.com/caf%C3%A9", wantExact: true, wantFetch: "https://example.com/caf%C3%A9"},
		{name: "escaped UTF-8 lowercase hex", input: "https://example.com/caf%c3%a9", wantExact: true, wantFetch: "https://example.com/caf%c3%a9"},
		{name: "escaped colon", input: "https://example.com/a%3Ab", wantExact: true, wantFetch: "https://example.com/a%3Ab"},
		{name: "escaped question mark", input: "https://example.com/a%3Fb", wantExact: true, wantFetch: "https://example.com/a%3Fb"},
		{name: "escaped hash", input: "https://example.com/a%23b", wantExact: true, wantFetch: "https://example.com/a%23b"},
		{name: "escaped percent", input: "https://example.com/50%2541", wantExact: true, wantFetch: "https://example.com/50%2541"},
		{name: "escaped newline", input: "https://example.com/p%0A", wantExact: true, wantFetch: "https://example.com/p%0A"},
		{name: "escaped tilde decoded", input: "https://example.com/a%7Eb", wantExact: true, wantFetch: "https://example.com/a~b"},
		{name: "escaped letter decoded", input: "https://example.com/a%41", wantExact: true, wantFetch: "https://example.com/aA"},
		{name: "unreserved escapes decoded in any hex case", input: "https://example.com/%2d%5f%7e%30%61", wantExact: true, wantFetch: "https://example.com/-_~0a"},
		{name: "escaped sub-delim kept", input: "https://example.com/a%21b", wantExact: true, wantFetch: "https://example.com/a%21b"},
		{name: "raw exclamation mark", input: "https://example.com/a!b"},
		{name: "raw apostrophe", input: "https://example.com/a'b"},
		{name: "raw parentheses", input: "https://example.com/a(b)"},
		{name: "raw brackets", input: "https://example.com/a[b]"},
		{name: "raw pipe", input: "https://example.com/a|b"},
		{name: "raw sub-delim with an escape", input: "https://example.com/x'y%2Cz"},
		{name: "double slash", input: "https://example.com/a//b"},
		{name: "dot dot segment", input: "https://example.com/a/../b"},
		{name: "dot segment", input: "https://example.com/a/./b"},
		{name: "escaped dot dot segment", input: "https://example.com/a/%2e%2e/b"},
		{name: "escaped dot inside a segment", input: "https://example.com/a%2Eb"},
		{name: "escaped slash", input: "https://example.com/api%2F.env"},
		{name: "escaped slash lowercase", input: "https://example.com/a%2fb"},
		{name: "escaped backslash", input: "https://example.com/a%5Cb"},
		{name: "raw backslash", input: "https://example.com/a\\b"},
		{name: "raw space", input: "https://example.com/a b"},
		{name: "raw UTF-8", input: "https://example.com/café"},
		{name: "query normalized and stripped", input: "https://example.com/p?b=2&a=1&utm_source=x", strip: true, wantExact: true, wantFetch: "https://example.com/p?a=1&b=2"},
		{name: "query normalized next to an exact path", input: "https://example.com/a%2Cb?b=2&a=1", wantExact: true, wantFetch: "https://example.com/a%2Cb?a=1&b=2"},
		{name: "force query kept", input: "https://example.com/a%2Cb?", wantExact: true, wantFetch: "https://example.com/a%2Cb?"},
		{name: "fragment dropped", input: "https://example.com/a%2Cb#frag", wantExact: true, wantFetch: "https://example.com/a%2Cb"},
		{name: "empty path", input: "https://example.com", wantExact: true, wantFetch: "https://example.com/"},
		{name: "empty path with query", input: "https://example.com?x=1", wantExact: true, wantFetch: "https://example.com/?x=1"},
		{name: "trailing slash", input: "https://example.com/a%2Cb/", wantExact: true, wantFetch: "https://example.com/a%2Cb/"},
		{name: "userinfo", input: "https://user:pass@example.com/a%2Cb", wantExact: true, wantFetch: "https://user:pass@example.com/a%2Cb"},
		{name: "IPv6 host", input: "http://[::ffff:192.0.2.1]:8080/a%2Cb", wantExact: true, wantFetch: "http://[::ffff:192.0.2.1]:8080/a%2Cb"},
		{name: "explicit port", input: "https://example.com:8443/a%2Cb", wantExact: true, wantFetch: "https://example.com:8443/a%2Cb"},
		{name: "default port removed", input: "https://example.com:443/a%2Cb", wantExact: true, wantFetch: "https://example.com/a%2Cb"},
		{name: "scheme and host lowercased", input: "HTTPS://EXAMPLE.COM./A%2Cb", wantExact: true, wantFetch: "https://example.com/A%2Cb"},
		{name: "no scheme", input: "example.com/a%2Cb", wantExact: true, wantFetch: "https://example.com/a%2Cb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stripPatterns []config.CompiledStripPattern
			if tt.strip {
				stripPatterns = patterns
			}
			result, err := normalizer.Normalize(tt.input, stripPatterns)
			require.NoError(t, err)

			assert.Equal(t, tt.wantExact, result.FetchExact)
			if tt.wantExact {
				assert.Equal(t, tt.wantFetch, result.FetchURL)
			} else {
				assert.Equal(t, result.NormalizedURL, result.FetchURL)
			}

			fetchResult, err := normalizer.Normalize(result.FetchURL, stripPatterns)
			require.NoError(t, err)
			assert.Equal(t, result.NormalizedURL, fetchResult.NormalizedURL, "the fetch URL must share the request's cache key")
		})
	}
}

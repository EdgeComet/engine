package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHTMLContentType(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string][]string
		expected bool
	}{
		{
			name:     "text/html",
			headers:  map[string][]string{"Content-Type": {"text/html"}},
			expected: true,
		},
		{
			name:     "text/html with charset",
			headers:  map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
			expected: true,
		},
		{
			name:     "lowercase header key",
			headers:  map[string][]string{"content-type": {"text/html; charset=utf-8"}},
			expected: true,
		},
		{
			name:     "json is not html",
			headers:  map[string][]string{"Content-Type": {"application/json"}},
			expected: false,
		},
		{
			name:     "xml is not html",
			headers:  map[string][]string{"Content-Type": {"application/xml"}},
			expected: false,
		},
		{
			name:     "missing content-type defaults to html",
			headers:  map[string][]string{"X-Other": {"value"}},
			expected: true,
		},
		{
			name:     "nil headers default to html",
			headers:  nil,
			expected: true,
		},
		{
			name:     "empty value defaults to html",
			headers:  map[string][]string{"Content-Type": {}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isHTMLContentType(tt.headers))
		})
	}
}

// IsHTMLContentTypeValue is exported for the recache service, which mirrors the bypass serve
// path's content-type guard before content processing / extraction. Lock its contract.
func TestIsHTMLContentTypeValue(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"text/html", "text/html", true},
		{"text/html with charset", "text/html; charset=utf-8", true},
		{"empty defaults to html", "", true},
		{"json is not html", "application/json", false},
		{"pdf is not html", "application/pdf", false},
		{"image is not html", "image/png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsHTMLContentTypeValue(tt.contentType))
		})
	}
}

func TestFirstHeaderValueSorted(t *testing.T) {
	tests := []struct {
		name      string
		headers   map[string][]string
		lookup    string
		expected  string
		expectHit bool
	}{
		{
			name:      "exact name",
			headers:   map[string][]string{"Last-Modified": {"Wed, 05 Mar 2024 08:00:00 GMT"}},
			lookup:    "Last-Modified",
			expected:  "Wed, 05 Mar 2024 08:00:00 GMT",
			expectHit: true,
		},
		{
			name:      "case-insensitive name",
			headers:   map[string][]string{"last-modified": {"Wed, 05 Mar 2024 08:00:00 GMT"}},
			lookup:    "Last-Modified",
			expected:  "Wed, 05 Mar 2024 08:00:00 GMT",
			expectHit: true,
		},
		{
			name:      "first of several values",
			headers:   map[string][]string{"Last-Modified": {"first", "second"}},
			lookup:    "Last-Modified",
			expected:  "first",
			expectHit: true,
		},
		{
			// Uppercase sorts before mixed case, which sorts before lowercase.
			name:      "smallest matching name wins",
			headers:   map[string][]string{"last-modified": {"lower"}, "LAST-MODIFIED": {"upper"}, "Last-Modified": {"mixed"}},
			lookup:    "Last-Modified",
			expected:  "upper",
			expectHit: true,
		},
		{
			name:      "absent header",
			headers:   map[string][]string{"Cache-Control": {"max-age=60"}},
			lookup:    "Last-Modified",
			expectHit: false,
		},
		{
			name:      "nil map",
			headers:   nil,
			lookup:    "Last-Modified",
			expectHit: false,
		},
		{
			name:      "matching name without values",
			headers:   map[string][]string{"Last-Modified": {}},
			lookup:    "Last-Modified",
			expectHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := firstHeaderValueSorted(tt.headers, tt.lookup)
			assert.Equal(t, tt.expectHit, ok)
			assert.Equal(t, tt.expected, value)
		})
	}
}

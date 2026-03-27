package ajax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAjaxRequest(t *testing.T) {
	tests := []struct {
		name           string
		acceptHeader   string
		xRequestedWith string
		expected       bool
	}{
		{
			name:         "application/json Accept",
			acceptHeader: "application/json",
			expected:     true,
		},
		{
			name:         "application/json with charset",
			acceptHeader: "application/json; charset=utf-8",
			expected:     true,
		},
		{
			name:         "application/xml Accept",
			acceptHeader: "application/xml",
			expected:     true,
		},
		{
			name:         "text/xml Accept",
			acceptHeader: "text/xml",
			expected:     true,
		},
		{
			name:         "application/rss+xml Accept",
			acceptHeader: "application/rss+xml",
			expected:     true,
		},
		{
			name:         "application/atom+xml Accept",
			acceptHeader: "application/atom+xml",
			expected:     true,
		},
		{
			name:         "JSON with wildcard fallback",
			acceptHeader: "application/json, text/plain, */*",
			expected:     true,
		},
		{
			name:         "browser page request with text/html",
			acceptHeader: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			expected:     false,
		},
		{
			name:         "text/html with application/json",
			acceptHeader: "text/html, application/json",
			expected:     false,
		},
		{
			name:         "plain text/html",
			acceptHeader: "text/html",
			expected:     false,
		},
		{
			name:         "wildcard Accept",
			acceptHeader: "*/*",
			expected:     false,
		},
		{
			name:         "empty Accept",
			acceptHeader: "",
			expected:     false,
		},
		{
			name:         "text/plain only",
			acceptHeader: "text/plain",
			expected:     false,
		},
		{
			name:         "image Accept",
			acceptHeader: "image/webp,image/png,image/*",
			expected:     false,
		},
		{
			name:           "X-Requested-With XMLHttpRequest",
			acceptHeader:   "",
			xRequestedWith: "XMLHttpRequest",
			expected:       true,
		},
		{
			name:           "X-Requested-With case insensitive",
			acceptHeader:   "",
			xRequestedWith: "xmlhttprequest",
			expected:       true,
		},
		{
			name:           "X-Requested-With overrides text/html Accept",
			acceptHeader:   "text/html",
			xRequestedWith: "XMLHttpRequest",
			expected:       true,
		},
		{
			name:           "X-Requested-With with JSON Accept",
			acceptHeader:   "application/json",
			xRequestedWith: "XMLHttpRequest",
			expected:       true,
		},
		{
			name:           "unrelated X-Requested-With value",
			acceptHeader:   "",
			xRequestedWith: "SomeOtherValue",
			expected:       false,
		},
		{
			name:           "both empty",
			acceptHeader:   "",
			xRequestedWith: "",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAjaxRequest(tt.acceptHeader, tt.xRequestedWith)
			assert.Equal(t, tt.expected, result)
		})
	}
}

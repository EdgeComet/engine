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

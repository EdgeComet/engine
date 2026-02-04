package orchestrator

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
)

func TestExtractURL(t *testing.T) {
	tests := []struct {
		name          string
		urlParam      string
		expectedURL   string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid HTTPS URL",
			urlParam:    "https://example.com/page",
			expectedURL: "https://example.com/page",
			expectError: false,
		},
		{
			name:        "valid HTTP URL",
			urlParam:    "http://example.com/page",
			expectedURL: "http://example.com/page",
			expectError: false,
		},
		{
			name:        "URL with query parameters",
			urlParam:    "https://example.com/page?foo=bar&baz=qux",
			expectedURL: "https://example.com/page?foo=bar&baz=qux",
			expectError: false,
		},
		{
			name:        "URL with fragment",
			urlParam:    "https://example.com/page#section",
			expectedURL: "https://example.com/page#section",
			expectError: false,
		},
		{
			name:        "URL with path and query",
			urlParam:    "https://example.com/path/to/page?param=value",
			expectedURL: "https://example.com/path/to/page?param=value",
			expectError: false,
		},
		{
			name:          "missing url parameter",
			urlParam:      "",
			expectError:   true,
			errorContains: "missing required 'url' query parameter",
		},
		{
			name:          "invalid scheme - FTP",
			urlParam:      "ftp://example.com/file",
			expectError:   true,
			errorContains: "only HTTP and HTTPS schemes are supported",
		},
		{
			name:          "no scheme",
			urlParam:      "example.com/page",
			expectError:   true,
			errorContains: "only HTTP and HTTPS schemes are supported",
		},
		{
			name:          "invalid URL format - no host",
			urlParam:      "https://",
			expectError:   true,
			errorContains: "URL must have a valid host",
		},
		{
			name:          "URL too long",
			urlParam:      "https://example.com/" + string(make([]byte, 2100)),
			expectError:   true,
			errorContains: "URL exceeds maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetMethod("GET")

			if tt.urlParam != "" {
				ctx.Request.SetRequestURI("/render?url=" + url.QueryEscape(tt.urlParam))
			} else {
				ctx.Request.SetRequestURI("/render")
			}

			renderCtx := edgectx.NewRenderContext("test-request", ctx, zap.NewNop(), 30*time.Second)
			extractedURL, err := ExtractURL(renderCtx)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Empty(t, extractedURL)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedURL, extractedURL)
			}
		})
	}
}

package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

func TestExtractBypassSEO(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name        string
		body        []byte
		contentType string
		statusCode  int
		targetURL   string
		wantNil     bool
		checkSEO    func(t *testing.T, seo *types.PageSEO)
	}{
		{
			name:        "HTML with title and 200 status",
			body:        []byte(`<html><head><title>Test Page</title></head><body><h1>Hello</h1></body></html>`),
			contentType: "text/html",
			statusCode:  200,
			targetURL:   "https://example.com/page",
			wantNil:     false,
			checkSEO: func(t *testing.T, seo *types.PageSEO) {
				assert.Equal(t, "Test Page", seo.Title)
				assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
			},
		},
		{
			name:        "application/json content type",
			body:        []byte(`{"key": "value"}`),
			contentType: "application/json",
			statusCode:  200,
			targetURL:   "https://example.com/api",
			wantNil:     true,
		},
		{
			name:        "empty content type",
			body:        []byte(`<html><head><title>Test</title></head></html>`),
			contentType: "",
			statusCode:  200,
			targetURL:   "https://example.com/page",
			wantNil:     true,
		},
		{
			name: "HTML with noindex meta",
			body: []byte(`<html><head><title>Blocked</title>` +
				`<meta name="robots" content="noindex"></head><body></body></html>`),
			contentType: "text/html",
			statusCode:  200,
			targetURL:   "https://example.com/blocked",
			wantNil:     false,
			checkSEO: func(t *testing.T, seo *types.PageSEO) {
				assert.Equal(t, "Blocked", seo.Title)
				assert.Equal(t, types.IndexStatusBlockedByMeta, seo.IndexStatus)
			},
		},
		{
			name:        "HTML with 404 status code",
			body:        []byte(`<html><head><title>Not Found</title></head><body></body></html>`),
			contentType: "text/html",
			statusCode:  404,
			targetURL:   "https://example.com/missing",
			wantNil:     false,
			checkSEO: func(t *testing.T, seo *types.PageSEO) {
				assert.Equal(t, "Not Found", seo.Title)
				assert.Equal(t, types.IndexStatusNon200, seo.IndexStatus)
			},
		},
		{
			name:        "empty body with text/html",
			body:        []byte{},
			contentType: "text/html",
			statusCode:  200,
			targetURL:   "https://example.com/empty",
			wantNil:     false,
			checkSEO: func(t *testing.T, seo *types.PageSEO) {
				assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
			},
		},
		{
			name:        "text/html with charset suffix",
			body:        []byte(`<html><head><title>Charset Page</title></head><body></body></html>`),
			contentType: "text/html; charset=utf-8",
			statusCode:  200,
			targetURL:   "https://example.com/charset",
			wantNil:     false,
			checkSEO: func(t *testing.T, seo *types.PageSEO) {
				assert.Equal(t, "Charset Page", seo.Title)
				assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seo := extractBypassSEO(tt.body, tt.contentType, tt.statusCode, tt.targetURL, logger)

			if tt.wantNil {
				assert.Nil(t, seo)
				return
			}

			require.NotNil(t, seo)
			if tt.checkSEO != nil {
				tt.checkSEO(t, seo)
			}
		})
	}
}

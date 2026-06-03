package bypass

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

// TestFetchContentSetsLoopPreventionHeaders verifies that bypass fetches carry
// X-Edge-Render (so the integration routes them straight to origin instead of
// looping back into the Edge Gateway) and X-Render-Key, and that X-Edge-Render
// cannot be overridden by forwarded client headers.
func TestFetchContentSetsLoopPreventionHeaders(t *testing.T) {
	var got http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer origin.Close()

	ssrfOff := false
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	clientHeaders := map[string][]string{
		types.HeaderEdgeRender: {"spoofed"}, // must be overridden by engine value
	}

	resp, err := svc.FetchContent(origin.URL, clientHeaders, "render-key-123", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, edgeRenderSource, got.Get(types.HeaderEdgeRender),
		"bypass fetch must set X-Edge-Render to the engine value, not the forwarded one")
	assert.Equal(t, "render-key-123", got.Get(types.HeaderRenderKey))
}

// TestFetchContentLargeResponseHeaders verifies that an origin returning 200 OK
// with a response header block larger than fasthttp's 4 KB default read buffer is
// read successfully instead of failing with a 502. Regression for origins that
// emit large CSP/NEL/Report-To headers (e.g. Cloudflare-fronted sites).
func TestFetchContentLargeResponseHeaders(t *testing.T) {
	// CSP value well past the 4 KB default read buffer. Leading-space repeat avoids
	// a trailing space, which HTTP servers trim from header values.
	largeCSP := "default-src 'self';" + strings.Repeat(" https://cdn.example.com", 400)
	require.Greater(t, len(largeCSP), 4096, "test header must exceed the 4 KB default buffer")

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", largeCSP)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rendered body"))
	}))
	defer origin.Close()

	ssrfOff := false
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	resp, err := svc.FetchContent(origin.URL, nil, "", zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "rendered body", string(resp.Body))
	assert.Equal(t, largeCSP, resp.Headers["Content-Security-Policy"][0])
}

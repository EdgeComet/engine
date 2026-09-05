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
	assert.Empty(t, resp.TransportError, "a real origin response must not be marked as a transport failure")
}

// An unreachable origin is answered with a synthetic 502 so bots still get a response. The
// marker is what lets a caller tell that apart from an origin that genuinely said 502; the
// served status, body and content type must not change.
func TestFetchContentMarksTransportFailure(t *testing.T) {
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent: "EdgeCometTest/1.0",
	}, zap.NewNop())

	// Loopback is rejected by the SSRF-safe dialer, which surfaces as a transport failure
	// without depending on network reachability.
	resp, err := svc.FetchContent("http://127.0.0.1:1/page", nil, "", zap.NewNop())

	require.NoError(t, err, "FetchContent reports transport failures through the response, not an error")
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "Bad Gateway: Origin unreachable", string(resp.Body))
	assert.Equal(t, "text/plain; charset=utf-8", resp.ContentType)
	assert.NotEmpty(t, resp.TransportError)
}

// SentHeaders is the evidence stored on the event as sent to the origin, so it must show the engine-managed
// headers and the configured User-Agent rather than whatever the bot sent.
func TestFetchContentCapturesSentHeaders(t *testing.T) {
	var got http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	ssrfOff := false
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	clientHeaders := map[string][]string{
		"User-Agent":    {"Mozilla/5.0 (compatible; Googlebot/2.1)"},
		"Authorization": {"Bearer token"},
	}

	resp, err := svc.FetchContent(origin.URL, clientHeaders, "render-key-123", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, []string{"render-key-123"}, resp.SentHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{edgeRenderSource}, resp.SentHeaders[types.HeaderEdgeRender])
	assert.Equal(t, []string{"EdgeCometTest/1.0"}, resp.SentHeaders["User-Agent"],
		"the configured User-Agent is sent, not the client's")
	assert.Equal(t, []string{"Bearer token"}, resp.SentHeaders["Authorization"])
	assert.Equal(t, "EdgeCometTest/1.0", got.Get("User-Agent"))
}

// The capture happens before the dial, so the row for an origin that was never reached still
// says what the EG had prepared.
func TestFetchContentTransportFailureCapturesSentHeaders(t *testing.T) {
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent: "EdgeCometTest/1.0",
	}, zap.NewNop())

	// Loopback is rejected by the SSRF-safe dialer, which surfaces as a transport failure
	// without depending on network reachability.
	resp, err := svc.FetchContent("http://127.0.0.1:1/page", nil, "render-key-123", zap.NewNop())

	require.NoError(t, err)
	require.NotEmpty(t, resp.TransportError)
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Empty(t, resp.Headers, "no response was received")
	assert.Equal(t, []string{"render-key-123"}, resp.SentHeaders[types.HeaderRenderKey])
	assert.Equal(t, []string{edgeRenderSource}, resp.SentHeaders[types.HeaderEdgeRender])
	assert.Equal(t, []string{"EdgeCometTest/1.0"}, resp.SentHeaders["User-Agent"])
}

// Host is written by fasthttp from the URI during Do, after the capture, so it is on the wire
// but not in SentHeaders. Pins that difference: the field documents it, and a reader answering
// "what host did we ask for" must use the event URL.
func TestFetchContentSentHeadersOmitHostWrittenAtSendTime(t *testing.T) {
	var gotHost string
	var got http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	ssrfOff := false
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	resp, err := svc.FetchContent(origin.URL, nil, "", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NotEmpty(t, gotHost, "the listener must have received a Host")
	assert.Equal(t, strings.TrimPrefix(origin.URL, "http://"), gotHost)

	for name := range resp.SentHeaders {
		assert.False(t, strings.EqualFold(name, "Host"),
			"Host is filled from the URI at send time, after the capture")
	}

	// Every other header the listener saw is in the capture, in some spelling.
	for name := range got {
		if strings.EqualFold(name, "Host") {
			continue
		}
		found := false
		for captured := range resp.SentHeaders {
			if strings.EqualFold(captured, name) {
				found = true
			}
		}
		assert.True(t, found, "header %q reached the wire but was not captured", name)
	}
}

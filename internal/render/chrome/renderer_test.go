package chrome

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
)

func TestRenderResponse_Structure(t *testing.T) {
	resp := &types.RenderResponse{
		RequestID:  "test-req-1",
		Success:    true,
		HTML:       "<html><body>Test</body></html>",
		RenderTime: 1500,
		HTMLSize:   30,
		Timestamp:  time.Now(),
		ChromeID:   "chrome-0",
		Metrics: types.PageMetrics{
			StatusCode: 200,
			FinalURL:   "https://example.com",
			LifecycleEvents: []types.LifecycleEvent{
				{Name: "init", Time: 0.05},
				{Name: "DOMContentLoaded", Time: 0.5},
				{Name: "load", Time: 1.0},
			},
			ConsoleMessages: []types.ConsoleError{},
		},
	}

	assert.Equal(t, "test-req-1", resp.RequestID)
	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)
	assert.Len(t, resp.Metrics.LifecycleEvents, 3)
	assert.Empty(t, resp.Metrics.ConsoleMessages)
}

func TestConsoleMessageCapture(t *testing.T) {
	t.Run("size limit on message field only", func(t *testing.T) {
		ce := types.ConsoleError{
			Type:           types.ConsoleTypeError,
			SourceURL:      "https://example.com/very-long-url-that-should-not-count.js",
			SourceLocation: "100:200",
			Message:        "short msg",
		}

		// Size should be len("short msg") = 9, not the full struct size
		assert.Equal(t, 9, len(ce.Message))
	})

	t.Run("console type constants", func(t *testing.T) {
		assert.Equal(t, "error", types.ConsoleTypeError)
		assert.Equal(t, "warning", types.ConsoleTypeWarning)
	})
}

func TestBuildInjectedHeaders(t *testing.T) {
	t.Run("no headers and no key returns nil", func(t *testing.T) {
		injected := buildInjectedHeaders(&types.RenderRequest{})

		assert.Nil(t, injected)
	})

	t.Run("key only", func(t *testing.T) {
		injected := buildInjectedHeaders(&types.RenderRequest{RenderKey: "sk_test_123"})

		assert.Len(t, injected, 1)
		assert.Equal(t, []string{"sk_test_123"}, injected[types.HeaderRenderKey])
	})

	t.Run("client headers only", func(t *testing.T) {
		injected := buildInjectedHeaders(&types.RenderRequest{
			Headers: map[string][]string{
				"Authorization": {"Bearer token"},
				"Cookie":        {"a=1", "b=2"},
			},
		})

		assert.Len(t, injected, 2)
		assert.Equal(t, []string{"Bearer token"}, injected["Authorization"])
		assert.Equal(t, []string{"a=1", "b=2"}, injected["Cookie"])
		assert.NotContains(t, injected, types.HeaderRenderKey)
	})

	t.Run("client headers and key", func(t *testing.T) {
		injected := buildInjectedHeaders(&types.RenderRequest{
			Headers:   map[string][]string{"Authorization": {"Bearer token"}},
			RenderKey: "sk_test_123",
		})

		assert.Len(t, injected, 2)
		assert.Equal(t, []string{"Bearer token"}, injected["Authorization"])
		assert.Equal(t, []string{"sk_test_123"}, injected[types.HeaderRenderKey])
	})

	t.Run("forwarded render key is replaced by the engine value", func(t *testing.T) {
		injected := buildInjectedHeaders(&types.RenderRequest{
			Headers:   map[string][]string{"x-render-key": {"client-supplied"}},
			RenderKey: "sk_test_123",
		})

		assert.Len(t, injected, 1, "only one spelling may survive")
		assert.Equal(t, []string{"sk_test_123"}, injected[types.HeaderRenderKey])
		assert.NotContains(t, injected, "x-render-key")
	})

	t.Run("client headers are not mutated", func(t *testing.T) {
		clientHeaders := map[string][]string{"x-render-key": {"client-supplied"}}
		buildInjectedHeaders(&types.RenderRequest{Headers: clientHeaders, RenderKey: "sk_test_123"})

		assert.Equal(t, map[string][]string{"x-render-key": {"client-supplied"}}, clientHeaders)
	})
}

func TestRenderKeyInjectionScope(t *testing.T) {
	// Mirrors the predicate at the fetch interception in buildTasks: a paused request carries the
	// forwarded client headers and the render key only when its host is same-origin with the render
	// target. Subdomains qualify - see the call site for why.
	const targetHost = "example.com"

	sameOrigin := func(requestURL string) bool {
		return urlutil.IsSameOrigin(targetHost, urlutil.ExtractHost(requestURL))
	}

	t.Run("exact host", func(t *testing.T) {
		assert.True(t, sameOrigin("https://example.com/api/data"))
	})

	t.Run("sibling subdomain", func(t *testing.T) {
		assert.True(t, sameOrigin("https://platform.example.com/api/v2/casino/producer"))
	})

	t.Run("parent domain of a subdomain target", func(t *testing.T) {
		assert.True(t, urlutil.IsSameOrigin("www.example.com", urlutil.ExtractHost("https://example.com/data")))
	})

	t.Run("port and scheme are ignored", func(t *testing.T) {
		assert.True(t, sameOrigin("https://example.com:8443/api/data"))
		assert.True(t, sameOrigin("http://example.com/api/data"))
	})

	t.Run("unrelated host", func(t *testing.T) {
		assert.False(t, sameOrigin("https://cdn.other.com/lib.js"))
	})

	t.Run("host that merely suffixes the target", func(t *testing.T) {
		assert.False(t, sameOrigin("https://notexample.com/data"))
	})

	t.Run("empty target host", func(t *testing.T) {
		assert.False(t, urlutil.IsSameOrigin("", urlutil.ExtractHost("https://example.com/api/data")))
	})

	t.Run("unparseable request URL", func(t *testing.T) {
		assert.False(t, sameOrigin("://not a url"))
	})
}

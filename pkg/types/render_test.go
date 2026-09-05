package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderRequest_InjectedHeaders(t *testing.T) {
	t.Run("no headers and no key returns nil", func(t *testing.T) {
		injected := (&RenderRequest{}).InjectedHeaders()

		assert.Nil(t, injected)
	})

	t.Run("key only", func(t *testing.T) {
		injected := (&RenderRequest{RenderKey: "sk_test_123"}).InjectedHeaders()

		assert.Len(t, injected, 1)
		assert.Equal(t, []string{"sk_test_123"}, injected[HeaderRenderKey])
	})

	t.Run("client headers only", func(t *testing.T) {
		injected := (&RenderRequest{
			Headers: map[string][]string{
				"Authorization": {"Bearer token"},
				"Cookie":        {"a=1", "b=2"},
			},
		}).InjectedHeaders()

		assert.Len(t, injected, 2)
		assert.Equal(t, []string{"Bearer token"}, injected["Authorization"])
		assert.Equal(t, []string{"a=1", "b=2"}, injected["Cookie"])
		assert.NotContains(t, injected, HeaderRenderKey)
	})

	t.Run("client headers and key", func(t *testing.T) {
		injected := (&RenderRequest{
			Headers:   map[string][]string{"Authorization": {"Bearer token"}},
			RenderKey: "sk_test_123",
		}).InjectedHeaders()

		assert.Len(t, injected, 2)
		assert.Equal(t, []string{"Bearer token"}, injected["Authorization"])
		assert.Equal(t, []string{"sk_test_123"}, injected[HeaderRenderKey])
	})

	t.Run("forwarded render key is replaced by the engine value", func(t *testing.T) {
		injected := (&RenderRequest{
			Headers:   map[string][]string{"x-render-key": {"client-supplied"}},
			RenderKey: "sk_test_123",
		}).InjectedHeaders()

		assert.Len(t, injected, 1, "only one spelling may survive")
		assert.Equal(t, []string{"sk_test_123"}, injected[HeaderRenderKey])
		assert.NotContains(t, injected, "x-render-key")
	})

	t.Run("a configured render key cannot reach the wire", func(t *testing.T) {
		// Configuration refuses X-Render-Key in request_headers_set, so this shape should never
		// be built. The render-side guard is the backstop for that rejection and must stay
		// proven: whatever spelling arrives in the forwarded map, the engine's own key wins.
		injected := (&RenderRequest{
			Headers: map[string][]string{
				"X-Render-Key": {"configured-key"},
				"x-render-KEY": {"another-configured-key"},
			},
			RenderKey: "sk_test_123",
		}).InjectedHeaders()

		assert.Len(t, injected, 1, "no configured spelling may survive alongside the engine key")
		assert.Equal(t, []string{"sk_test_123"}, injected[HeaderRenderKey])
	})

	t.Run("client headers are not mutated", func(t *testing.T) {
		clientHeaders := map[string][]string{"x-render-key": {"client-supplied"}}
		(&RenderRequest{Headers: clientHeaders, RenderKey: "sk_test_123"}).InjectedHeaders()

		assert.Equal(t, map[string][]string{"x-render-key": {"client-supplied"}}, clientHeaders)
	})
}

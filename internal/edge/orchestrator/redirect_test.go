package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/internal/edge/cache"
)

func TestRedirectLocationFromMetadata(t *testing.T) {
	t.Run("redirect with Location header", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 301,
			Headers: map[string][]string{
				"Location": {"https://example.com/new"},
			},
		}
		assert.Equal(t, "https://example.com/new", redirectLocationFromMetadata(meta))
	})

	t.Run("redirect with nil headers", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 302,
			Headers:    nil,
		}
		assert.Empty(t, redirectLocationFromMetadata(meta))
	})

	t.Run("redirect with no Location key", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 301,
			Headers: map[string][]string{
				"Content-Type": {"text/html"},
			},
		}
		assert.Empty(t, redirectLocationFromMetadata(meta))
	})

	t.Run("non-redirect status ignores Location header", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 200,
			Headers: map[string][]string{
				"Location": {"https://example.com/should-be-ignored"},
			},
		}
		assert.Empty(t, redirectLocationFromMetadata(meta))
	})

	t.Run("case-insensitive Location lookup", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 307,
			Headers: map[string][]string{
				"location": {"https://example.com/lowercase"},
			},
		}
		assert.Equal(t, "https://example.com/lowercase", redirectLocationFromMetadata(meta))
	})

	t.Run("308 permanent redirect", func(t *testing.T) {
		meta := &cache.CacheMetadata{
			StatusCode: 308,
			Headers: map[string][]string{
				"Location": {"https://example.com/permanent"},
			},
		}
		assert.Equal(t, "https://example.com/permanent", redirectLocationFromMetadata(meta))
	})
}

package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/pkg/types"
)

// TestPageSEOFromCacheMetadata_LeavesDatesNil guards the producer side of the
// inspected/uninspected distinction. Cache metadata holds no date evidence, so the
// rebuilt struct must keep a nil slice: an initialized empty one would travel to the
// event as the claim that the engine inspected the page and found no date signal.
func TestPageSEOFromCacheMetadata_LeavesDatesNil(t *testing.T) {
	t.Run("metadata with title", func(t *testing.T) {
		seo := pageSEOFromCacheMetadata(&cache.CacheMetadata{
			Title:       "Cached title",
			IndexStatus: int(types.IndexStatusIndexable),
		})
		require.NotNil(t, seo)
		assert.Nil(t, seo.Dates)
	})

	t.Run("empty metadata", func(t *testing.T) {
		assert.Nil(t, pageSEOFromCacheMetadata(&cache.CacheMetadata{}))
	})
}

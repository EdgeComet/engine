package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/pkg/types"
)

// TestPageSEOFromCacheMetadata_LeavesInspectionEvidenceNil guards the producer side of
// the inspected/uninspected distinction. A cache hit serves bytes nothing re-read, so
// every signal that claims the engine looked at the page must stay absent: an
// initialized empty date slice would travel to the event as "inspected, no date signal",
// and a JSON-LD envelope of {"blocks":0} as "inspected, carries no structured data".
// Both are findings a report acts on, and neither was established here.
func TestPageSEOFromCacheMetadata_LeavesInspectionEvidenceNil(t *testing.T) {
	t.Run("metadata with title", func(t *testing.T) {
		seo := pageSEOFromCacheMetadata(&cache.CacheMetadata{
			Title:       "Cached title",
			IndexStatus: int(types.IndexStatusIndexable),
		})
		require.NotNil(t, seo)
		assert.Nil(t, seo.Dates)
		assert.Nil(t, seo.SchemaOrg)
	})

	t.Run("empty metadata", func(t *testing.T) {
		assert.Nil(t, pageSEOFromCacheMetadata(&cache.CacheMetadata{}))
	})
}

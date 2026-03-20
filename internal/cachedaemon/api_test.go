package cachedaemon

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/pkg/types"
)

func makePostRequest(daemon *CacheDaemon, path string, body []byte) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.SetRequestURI(path)
	ctx.Request.Header.Set("X-Internal-Auth", "test-auth-key")
	ctx.Request.Header.SetContentType("application/json")
	ctx.Request.SetBody(body)
	daemon.ServeHTTP(ctx)
	return ctx
}

func TestHandleInvalidateAPI_BypassDimension(t *testing.T) {
	t.Run("invalidate without dimension_ids includes bypass dimension 0", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		// Create bypass cache entry (dimension 0)
		populateMetadataHash(mr, 1, 0, "abc123", map[string]string{
			"url": "https://example.com/page", "dimension": "",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "bypass",
		})

		// Normalize the URL the same way the handler does
		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/page", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		// Re-populate with the correct hash
		populateMetadataHash(mr, 1, 0, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "bypass",
		})

		metadataKey := daemon.keyGenerator.GenerateMetadataKey(
			daemon.keyGenerator.GenerateCacheKey(1, 0, urlHash),
		)
		require.True(t, mr.Exists(metadataKey))

		body, _ := json.Marshal(types.InvalidateAPIRequest{
			HostID: 1,
			URLs:   []string{"https://example.com/page"},
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.False(t, mr.Exists(metadataKey))
	})

	t.Run("invalidate with explicit dimension_ids [0] is accepted", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/page2", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		populateMetadataHash(mr, 1, 0, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "bypass",
		})

		metadataKey := daemon.keyGenerator.GenerateMetadataKey(
			daemon.keyGenerator.GenerateCacheKey(1, 0, urlHash),
		)
		require.True(t, mr.Exists(metadataKey))

		body, _ := json.Marshal(types.InvalidateAPIRequest{
			HostID:       1,
			URLs:         []string{"https://example.com/page2"},
			DimensionIDs: []int{0},
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.False(t, mr.Exists(metadataKey))
	})
}

func TestHandleRecacheAPI_BypassDimension(t *testing.T) {
	t.Run("recache without dimension_ids includes bypass dimension 0", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:   1,
			URLs:     []string{"https://example.com/page"},
			Priority: "normal",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		// Verify bypass dimension 0 was enqueued
		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
		members, err := mr.ZMembers(queueKey)
		require.NoError(t, err)

		foundBypass := false
		for _, m := range members {
			var member types.RecacheMember
			require.NoError(t, json.Unmarshal([]byte(m), &member))
			if member.DimensionID == 0 {
				foundBypass = true
				break
			}
		}
		assert.True(t, foundBypass, "bypass dimension 0 should be in recache queue")
	})

	t.Run("recache with explicit dimension_ids [0] is accepted", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:       1,
			URLs:         []string{"https://example.com/page"},
			DimensionIDs: []int{0},
			Priority:     "high",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
		members, err := mr.ZMembers(queueKey)
		require.NoError(t, err)
		require.Len(t, members, 1)

		var member types.RecacheMember
		require.NoError(t, json.Unmarshal([]byte(members[0]), &member))
		assert.Equal(t, 0, member.DimensionID)
	})
}

func TestHandleInvalidateAllAPI(t *testing.T) {
	t.Run("deletes all metadata for host", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		// Create entries across dimensions 0, 1, 2
		populateMetadataHash(mr, 1, 0, "hash1", map[string]string{
			"url": "https://example.com/a", "dimension": "",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "bypass",
		})
		populateMetadataHash(mr, 1, 1, "hash2", map[string]string{
			"url": "https://example.com/b", "dimension": "mobile",
			"size": "200", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
		populateMetadataHash(mr, 1, 2, "hash3", map[string]string{
			"url": "https://example.com/c", "dimension": "desktop",
			"size": "300", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})

		key0 := "meta:cache:1:0:hash1"
		key1 := "meta:cache:1:1:hash2"
		key2 := "meta:cache:1:2:hash3"
		require.True(t, mr.Exists(key0))
		require.True(t, mr.Exists(key1))
		require.True(t, mr.Exists(key2))

		body, _ := json.Marshal(types.InvalidateAllAPIRequest{
			HostID: 1,
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.False(t, mr.Exists(key0))
		assert.False(t, mr.Exists(key1))
		assert.False(t, mr.Exists(key2))

		var resp struct {
			Data types.InvalidateAllAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		assert.Equal(t, 3, resp.Data.EntriesInvalidated)
	})

	t.Run("filters by dimension_ids", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		populateMetadataHash(mr, 1, 0, "hash1", map[string]string{
			"url": "https://example.com/a", "dimension": "",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "bypass",
		})
		populateMetadataHash(mr, 1, 1, "hash2", map[string]string{
			"url": "https://example.com/b", "dimension": "mobile",
			"size": "200", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
		populateMetadataHash(mr, 1, 2, "hash3", map[string]string{
			"url": "https://example.com/c", "dimension": "desktop",
			"size": "300", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})

		body, _ := json.Marshal(types.InvalidateAllAPIRequest{
			HostID:       1,
			DimensionIDs: []int{1},
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.True(t, mr.Exists("meta:cache:1:0:hash1"), "dimension 0 should survive")
		assert.False(t, mr.Exists("meta:cache:1:1:hash2"), "dimension 1 should be deleted")
		assert.True(t, mr.Exists("meta:cache:1:2:hash3"), "dimension 2 should survive")

		var resp struct {
			Data types.InvalidateAllAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		assert.Equal(t, 1, resp.Data.EntriesInvalidated)
	})

	t.Run("empty host returns 0 invalidated", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		body, _ := json.Marshal(types.InvalidateAllAPIRequest{
			HostID: 1,
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		var resp struct {
			Data types.InvalidateAllAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		assert.Equal(t, 0, resp.Data.EntriesInvalidated)
	})

	t.Run("invalid host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		body, _ := json.Marshal(types.InvalidateAllAPIRequest{
			HostID: 999,
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("does not delete metadata for other hosts", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		// Host 1 entry
		populateMetadataHash(mr, 1, 1, "hash1", map[string]string{
			"url": "https://example.com/a", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
		// Host 2 entry
		populateMetadataHash(mr, 2, 1, "hash2", map[string]string{
			"url": "https://nocache.com/b", "dimension": "mobile",
			"size": "200", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})

		body, _ := json.Marshal(types.InvalidateAllAPIRequest{
			HostID: 1,
		})
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.False(t, mr.Exists("meta:cache:1:1:hash1"), "host 1 entry should be deleted")
		assert.True(t, mr.Exists("meta:cache:2:1:hash2"), "host 2 entry should survive")
	})
}

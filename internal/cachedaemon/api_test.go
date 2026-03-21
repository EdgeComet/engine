package cachedaemon

import (
	"encoding/json"
	"sort"
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

func TestResolveDimensionIDs(t *testing.T) {
	host := &types.Host{
		Domain: "example.com",
		Dimensions: map[string]types.Dimension{
			"bypass":  {ID: 0, Action: types.ActionBypass},
			"mobile":  {ID: 1},
			"desktop": {ID: 2},
			"blocked": {ID: 3, Action: types.ActionBlock},
		},
	}

	t.Run("empty request returns all non-block dimension IDs", func(t *testing.T) {
		ids, err := resolveDimensionIDs(host, nil)
		require.NoError(t, err)
		sort.Ints(ids)
		assert.Equal(t, []int{0, 1, 2}, ids)
	})

	t.Run("requesting bypass dimension 0 is accepted", func(t *testing.T) {
		ids, err := resolveDimensionIDs(host, []int{0})
		require.NoError(t, err)
		assert.Equal(t, []int{0}, ids)
	})

	t.Run("requesting block dimension 3 is rejected", func(t *testing.T) {
		_, err := resolveDimensionIDs(host, []int{3})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dimension_id 3 not configured")
	})

	t.Run("requesting unconfigured dimension is rejected", func(t *testing.T) {
		_, err := resolveDimensionIDs(host, []int{99})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dimension_id 99 not configured")
	})

	t.Run("requesting specific valid IDs returns them", func(t *testing.T) {
		ids, err := resolveDimensionIDs(host, []int{1, 2})
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, ids)
	})
}

func TestHandleInvalidateAPI_BypassDimension(t *testing.T) {
	t.Run("invalidate without dimension_ids includes bypass dimension 0", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/page", nil)
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
	t.Run("recache without dimension_ids includes bypass and excludes block", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:   1,
			URLs:     []string{"https://example.com/page"},
			Priority: "normal",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
		members, err := mr.ZMembers(queueKey)
		require.NoError(t, err)

		var dimensionIDs []int
		for _, m := range members {
			var member types.RecacheMember
			require.NoError(t, json.Unmarshal([]byte(m), &member))
			dimensionIDs = append(dimensionIDs, member.DimensionID)
		}
		sort.Ints(dimensionIDs)
		assert.Equal(t, []int{0, 1, 2}, dimensionIDs, "should include bypass (0) and render dims, but not block (3)")
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

	t.Run("recache with explicit block dimension_ids [3] is rejected", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:       1,
			URLs:         []string{"https://example.com/page"},
			DimensionIDs: []int{3},
			Priority:     "high",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})
}

func TestHandleInvalidateAllAPI(t *testing.T) {
	t.Run("deletes all metadata for host", func(t *testing.T) {
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

		populateMetadataHash(mr, 1, 1, "hash1", map[string]string{
			"url": "https://example.com/a", "dimension": "mobile",
			"size": "100", "created_at": "1000000", "expires_at": "9999999999", "source": "render",
		})
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

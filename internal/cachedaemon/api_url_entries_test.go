package cachedaemon

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/common/httputil"
)

func parseURLEntriesResponse(t *testing.T, ctx *fasthttp.RequestCtx) URLEntriesResponse {
	t.Helper()
	var apiResp httputil.APIResponse
	err := json.Unmarshal(ctx.Response.Body(), &apiResp)
	require.NoError(t, err)
	require.True(t, apiResp.Success)

	dataBytes, err := json.Marshal(apiResp.Data)
	require.NoError(t, err)

	var result URLEntriesResponse
	err = json.Unmarshal(dataBytes, &result)
	require.NoError(t, err)
	return result
}

func TestHandleURLEntriesAPI(t *testing.T) {
	now := time.Now().UTC().Unix()

	t.Run("missing url param returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("missing host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?url=https://example.com/page")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("unknown host_id returns 404", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=999&url=https://example.com/page")
		assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	})

	t.Run("no cached entries returns 200 with empty array", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/notcached")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLEntriesResponse(t, ctx)
		assert.Equal(t, "https://example.com/notcached", result.URL)
		assert.NotNil(t, result.Entries)
		assert.Len(t, result.Entries, 0)
	})

	t.Run("returns one entry per cached dimension sorted by dimension_id", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/multi", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		// Seed bypass (dim 0) and mobile (dim 1); leave desktop (dim 2) uncached.
		populateMetadataHash(mr, 1, 0, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "bypass",
			"size": "512", "created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600), "status_code": "200", "source": "bypass",
		})
		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "mobile",
			"size": "2048", "created_at": fmt.Sprintf("%d", now-50),
			"expires_at": fmt.Sprintf("%d", now+7200), "status_code": "200", "source": "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/multi")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLEntriesResponse(t, ctx)
		require.Len(t, result.Entries, 2)

		// Sorted by dimension_id: bypass (0) first, mobile (1) second.
		bypass := result.Entries[0]
		assert.Equal(t, 0, bypass.DimensionID)
		assert.Equal(t, "bypass", bypass.Dimension)
		assert.Equal(t, daemon.keyGenerator.GenerateCacheKey(1, 0, urlHash).String(), bypass.CacheKey)
		assert.Equal(t, "active", bypass.Status)
		assert.Equal(t, 200, bypass.StatusCode)
		assert.Equal(t, int64(512), bypass.Size)
		assert.Equal(t, now-100, bypass.CreatedAt)
		assert.Equal(t, now+3600, bypass.ExpiresAt)
		assert.GreaterOrEqual(t, bypass.CacheAge, int64(100))

		mobile := result.Entries[1]
		assert.Equal(t, 1, mobile.DimensionID)
		assert.Equal(t, "mobile", mobile.Dimension)
		assert.Equal(t, int64(2048), mobile.Size)
	})

	t.Run("dimension_id filter restricts to one dimension", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/filter", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		populateMetadataHash(mr, 1, 0, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "bypass",
			"size": "100", "created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600), "status_code": "200", "source": "bypass",
		})
		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "mobile",
			"size": "200", "created_at": fmt.Sprintf("%d", now-50),
			"expires_at": fmt.Sprintf("%d", now+3600), "status_code": "200", "source": "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/filter&dimension_id=0")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLEntriesResponse(t, ctx)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, 0, result.Entries[0].DimensionID)
		assert.Equal(t, "bypass", result.Entries[0].Dimension)
	})

	t.Run("unknown dimension_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/page&dimension_id=99")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("non-numeric dimension_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/page&dimension_id=abc")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("expired entry reports expired status", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/old", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url": normalizedResult.NormalizedURL, "dimension": "mobile",
			"size": "10", "created_at": fmt.Sprintf("%d", now-7200),
			"expires_at": fmt.Sprintf("%d", now-3600), "status_code": "200", "source": "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/old")
		result := parseURLEntriesResponse(t, ctx)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "expired", result.Entries[0].Status)
	})

	t.Run("tracking params stripped before lookup", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		// Cache key is written from the stripped URL (no tracking params present).
		clean, err := daemon.normalizer.Normalize("https://example.com/strip", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(clean.NormalizedURL)

		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url": clean.NormalizedURL, "dimension": "mobile",
			"size": "300", "created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600), "status_code": "200", "source": "render",
		})

		// Request the same URL carrying a default tracking param; it must resolve to the
		// stripped key and find the entry.
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-entries?host_id=1&url=https://example.com/strip%3Futm_source%3Dnewsletter")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLEntriesResponse(t, ctx)
		assert.Equal(t, clean.NormalizedURL, result.URL)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "mobile", result.Entries[0].Dimension)
		assert.Equal(t, int64(300), result.Entries[0].Size)
	})
}

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
	"github.com/edgecomet/engine/pkg/types"
)

func parseURLStatusResponse(t *testing.T, ctx *fasthttp.RequestCtx) URLStatusResponse {
	t.Helper()
	var apiResp httputil.APIResponse
	err := json.Unmarshal(ctx.Response.Body(), &apiResp)
	require.NoError(t, err)
	require.True(t, apiResp.Success)

	dataBytes, err := json.Marshal(apiResp.Data)
	require.NoError(t, err)

	var result URLStatusResponse
	err = json.Unmarshal(dataBytes, &result)
	require.NoError(t, err)
	return result
}

func TestHandleURLStatusAPI(t *testing.T) {
	now := time.Now().UTC().Unix()

	t.Run("missing url param returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("missing host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?url=https://example.com/page")
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("unknown host_id returns 404", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=999&url=https://example.com/page")
		assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	})

	t.Run("url not in cache and not in queue", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/notfound")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.False(t, result.Cache.Exists)
		assert.Nil(t, result.Cache.Status)
		assert.Nil(t, result.Cache.CreatedAt)
		assert.Nil(t, result.Cache.StatusCode)
		assert.False(t, result.Queue.Pending)
		assert.Nil(t, result.Queue.Priority)
		assert.Nil(t, result.Queue.ScheduledAt)
	})

	t.Run("url in cache active", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/cached", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		createdAt := now - 100
		expiresAt := now + 3600

		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", createdAt),
			"expires_at":  fmt.Sprintf("%d", expiresAt),
			"status_code": "200",
			"source":      "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/cached")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Cache.Exists)
		require.NotNil(t, result.Cache.Status)
		assert.Equal(t, "active", *result.Cache.Status)
		require.NotNil(t, result.Cache.CreatedAt)
		assert.Equal(t, createdAt, *result.Cache.CreatedAt)
		require.NotNil(t, result.Cache.StatusCode)
		assert.Equal(t, 200, *result.Cache.StatusCode)
		assert.False(t, result.Queue.Pending)
	})

	t.Run("url in cache expired", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/expired", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		createdAt := now - 7200
		expiresAt := now - 3600

		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", createdAt),
			"expires_at":  fmt.Sprintf("%d", expiresAt),
			"status_code": "200",
			"source":      "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/expired")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Cache.Exists)
		require.NotNil(t, result.Cache.Status)
		assert.Equal(t, "expired", *result.Cache.Status)
	})

	t.Run("url in queue", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/queued", nil)
		require.NoError(t, err)

		scheduledAt := float64(now - 10)
		member := types.RecacheMember{
			URL:         normalizedResult.NormalizedURL,
			DimensionID: 1,
		}
		memberJSON, _ := json.Marshal(member)

		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
		mr.ZAdd(queueKey, scheduledAt, string(memberJSON))

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/queued")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.False(t, result.Cache.Exists)
		assert.True(t, result.Queue.Pending)
		require.NotNil(t, result.Queue.Priority)
		assert.Equal(t, "normal", *result.Queue.Priority)
		require.NotNil(t, result.Queue.ScheduledAt)
		assert.Equal(t, int64(scheduledAt), *result.Queue.ScheduledAt)
	})

	t.Run("url in both cache and queue", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/both", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		createdAt := now - 100
		expiresAt := now + 3600

		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", createdAt),
			"expires_at":  fmt.Sprintf("%d", expiresAt),
			"status_code": "200",
			"source":      "render",
		})

		scheduledAt := float64(now - 5)
		member := types.RecacheMember{
			URL:         normalizedResult.NormalizedURL,
			DimensionID: 1,
		}
		memberJSON, _ := json.Marshal(member)

		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
		mr.ZAdd(queueKey, scheduledAt, string(memberJSON))

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/both")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Cache.Exists)
		assert.Equal(t, "active", *result.Cache.Status)
		assert.True(t, result.Queue.Pending)
		assert.Equal(t, "high", *result.Queue.Priority)
	})

	t.Run("multi-dimension returns most recent cache entry", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/multidim", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		// Older entry in dimension 1
		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", now-200),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"status_code": "200",
			"source":      "render",
		})

		// Newer entry in dimension 2
		newerCreatedAt := now - 50
		populateMetadataHash(mr, 1, 2, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", newerCreatedAt),
			"expires_at":  fmt.Sprintf("%d", now+7200),
			"status_code": "301",
			"source":      "render",
		})

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/multidim")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Cache.Exists)
		require.NotNil(t, result.Cache.CreatedAt)
		assert.Equal(t, newerCreatedAt, *result.Cache.CreatedAt)
		require.NotNil(t, result.Cache.StatusCode)
		assert.Equal(t, 301, *result.Cache.StatusCode)
	})

	t.Run("url normalization matches cache", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		// Normalize with uppercase host - normalizer lowercases it
		normalizedResult, err := daemon.normalizer.Normalize("https://EXAMPLE.COM/normalized", nil)
		require.NoError(t, err)
		urlHash := daemon.normalizer.Hash(normalizedResult.NormalizedURL)

		createdAt := now - 100
		populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
			"url":         normalizedResult.NormalizedURL,
			"created_at":  fmt.Sprintf("%d", createdAt),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"status_code": "200",
			"source":      "render",
		})

		// Request with uppercase host - normalization should lowercase it and match
		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://EXAMPLE.COM/normalized")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Cache.Exists)
		require.NotNil(t, result.Cache.CreatedAt)
		assert.Equal(t, createdAt, *result.Cache.CreatedAt)
	})

	t.Run("queue priority ordering returns highest", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)

		normalizedResult, err := daemon.normalizer.Normalize("https://example.com/multiprio", nil)
		require.NoError(t, err)

		member := types.RecacheMember{
			URL:         normalizedResult.NormalizedURL,
			DimensionID: 1,
		}
		memberJSON, _ := json.Marshal(member)

		// Add to both normal and high queues
		normalKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
		highKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
		mr.ZAdd(normalKey, float64(now-20), string(memberJSON))
		mr.ZAdd(highKey, float64(now-10), string(memberJSON))

		ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/multiprio")
		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		result := parseURLStatusResponse(t, ctx)
		assert.True(t, result.Queue.Pending)
		require.NotNil(t, result.Queue.Priority)
		assert.Equal(t, "high", *result.Queue.Priority, "should return highest priority")
	})
}

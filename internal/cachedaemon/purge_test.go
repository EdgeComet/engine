package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// seedPurgeQueue fills recache:{hostID}:{priority} with count distinct members.
func seedPurgeQueue(t *testing.T, daemon *CacheDaemon, hostID int, priority string, count int) {
	t.Helper()
	ctx := context.Background()
	key := daemon.keyGenerator.RecacheQueueKey(hostID, priority)
	for i := 0; i < count; i++ {
		member := types.RecacheMember{
			URL:         fmt.Sprintf("https://example.com/%s/%d", priority, i),
			DimensionID: 1,
		}
		raw, err := json.Marshal(member)
		require.NoError(t, err)
		require.NoError(t, daemon.redis.ZAdd(ctx, key, float64(i), string(raw)))
	}
}

func purgeQueueDepth(t *testing.T, daemon *CacheDaemon, hostID int, priority string) int64 {
	t.Helper()
	n, err := daemon.redis.ZCard(context.Background(), daemon.keyGenerator.RecacheQueueKey(hostID, priority))
	require.NoError(t, err)
	return n
}

func TestResolvePurgePriorities(t *testing.T) {
	t.Run("omitted priorities fall back to high and normal", func(t *testing.T) {
		resolved, err := resolvePurgePriorities(nil)
		require.NoError(t, err)
		assert.Equal(t, []string{redis.PriorityHigh, redis.PriorityNormal}, resolved)
	})

	t.Run("empty priorities fall back to high and normal", func(t *testing.T) {
		resolved, err := resolvePurgePriorities([]string{})
		require.NoError(t, err)
		assert.Equal(t, []string{redis.PriorityHigh, redis.PriorityNormal}, resolved,
			"an explicit empty array means the default set, not purge nothing")
	})

	t.Run("autorecache is accepted when named", func(t *testing.T) {
		resolved, err := resolvePurgePriorities([]string{redis.PriorityAutorecache})
		require.NoError(t, err)
		assert.Equal(t, []string{redis.PriorityAutorecache}, resolved)
	})

	t.Run("unknown priority is rejected", func(t *testing.T) {
		_, err := resolvePurgePriorities([]string{"urgent"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid priority")
	})

	t.Run("uppercase priority is rejected", func(t *testing.T) {
		_, err := resolvePurgePriorities([]string{"HIGH"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid priority")
	})

	t.Run("duplicates pass through unchanged", func(t *testing.T) {
		resolved, err := resolvePurgePriorities([]string{redis.PriorityHigh, redis.PriorityHigh})
		require.NoError(t, err)
		assert.Equal(t, []string{redis.PriorityHigh, redis.PriorityHigh}, resolved)
	})
}

func TestPurgeRecacheQueues(t *testing.T) {
	const hostID = 1

	t.Run("removes the named priorities and counts them", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 7)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityNormal, 4)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityAutorecache, 3)

		purged, err := daemon.PurgeRecacheQueues(context.Background(), hostID,
			[]string{redis.PriorityHigh, redis.PriorityNormal})
		require.NoError(t, err)
		assert.Equal(t, 11, purged)

		assert.Equal(t, int64(0), purgeQueueDepth(t, daemon, hostID, redis.PriorityHigh))
		assert.Equal(t, int64(0), purgeQueueDepth(t, daemon, hostID, redis.PriorityNormal))
		assert.Equal(t, int64(3), purgeQueueDepth(t, daemon, hostID, redis.PriorityAutorecache))
	})

	t.Run("leaves other hosts untouched", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 5)
		seedPurgeQueue(t, daemon, 2, redis.PriorityHigh, 5)

		purged, err := daemon.PurgeRecacheQueues(context.Background(), hostID, []string{redis.PriorityHigh})
		require.NoError(t, err)
		assert.Equal(t, 5, purged)
		assert.Equal(t, int64(5), purgeQueueDepth(t, daemon, 2, redis.PriorityHigh))
	})

	t.Run("an already empty queue counts zero", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		purged, err := daemon.PurgeRecacheQueues(context.Background(), hostID,
			[]string{redis.PriorityHigh, redis.PriorityNormal})
		require.NoError(t, err)
		assert.Equal(t, 0, purged)
	})

	t.Run("a repeated priority is only counted once", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 6)

		purged, err := daemon.PurgeRecacheQueues(context.Background(), hostID,
			[]string{redis.PriorityHigh, redis.PriorityHigh})
		require.NoError(t, err)
		assert.Equal(t, 6, purged, "the second pass finds an empty key")
	})
}

func TestHandleQueuePurgeAPI(t *testing.T) {
	const hostID = 1

	purgedFromResponse := func(t *testing.T, ctx *fasthttp.RequestCtx) int {
		t.Helper()
		var resp struct {
			Data types.QueuePurgeAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		return resp.Data.EntriesPurged
	}

	t.Run("omitted priorities purge high and normal but keep autorecache", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 3)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityNormal, 2)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityAutorecache, 4)

		body, _ := json.Marshal(types.QueuePurgeAPIRequest{HostID: hostID})
		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Equal(t, 5, purgedFromResponse(t, ctx))
		assert.Equal(t, int64(0), purgeQueueDepth(t, daemon, hostID, redis.PriorityHigh))
		assert.Equal(t, int64(0), purgeQueueDepth(t, daemon, hostID, redis.PriorityNormal))
		assert.Equal(t, int64(4), purgeQueueDepth(t, daemon, hostID, redis.PriorityAutorecache))
	})

	t.Run("an explicit empty array purges the default priorities", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 3)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityAutorecache, 4)

		ctx := makePostRequest(daemon, "/internal/cache/queue/purge",
			[]byte(fmt.Sprintf(`{"host_id":%d,"priorities":[]}`, hostID)))

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Equal(t, 3, purgedFromResponse(t, ctx))
		assert.Equal(t, int64(4), purgeQueueDepth(t, daemon, hostID, redis.PriorityAutorecache))
	})

	t.Run("autorecache is purged only when named", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 3)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityAutorecache, 4)

		body, _ := json.Marshal(types.QueuePurgeAPIRequest{
			HostID:     hostID,
			Priorities: []string{redis.PriorityAutorecache},
		})
		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Equal(t, 4, purgedFromResponse(t, ctx))
		assert.Equal(t, int64(3), purgeQueueDepth(t, daemon, hostID, redis.PriorityHigh))
		assert.Equal(t, int64(0), purgeQueueDepth(t, daemon, hostID, redis.PriorityAutorecache))
	})

	t.Run("unknown priority returns 400 and purges nothing", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		seedPurgeQueue(t, daemon, hostID, redis.PriorityHigh, 3)

		body, _ := json.Marshal(types.QueuePurgeAPIRequest{
			HostID:     hostID,
			Priorities: []string{redis.PriorityHigh, "urgent"},
		})
		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", body)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
		assert.Equal(t, int64(3), purgeQueueDepth(t, daemon, hostID, redis.PriorityHigh),
			"validation must run before any key is touched")
	})

	t.Run("uppercase priority returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		body, _ := json.Marshal(types.QueuePurgeAPIRequest{
			HostID:     hostID,
			Priorities: []string{"HIGH"},
		})
		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", body)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("unknown host returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		body, _ := json.Marshal(types.QueuePurgeAPIRequest{HostID: 999})
		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", body)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("missing host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", []byte(`{}`))

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makePostRequest(daemon, "/internal/cache/queue/purge", []byte(`{"host_id":`))

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})
}

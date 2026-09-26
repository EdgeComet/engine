package cachedaemon

// Real-Redis integration tests for the chunked cache listing. miniredis
// paginates SCAN over the sorted, MATCH-filtered key list and cannot reproduce
// real Redis hash-bucket scatter, where a small host's keys sit behind
// thousands of a large neighbor's keys and a single bounded Eval returns an
// empty page. Gated on TEST_REDIS_ADDR (CI provides a Redis service
// container). Uses a dedicated DB and flushes it around each test.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	integrationRedisDB      = 15
	integrationPipelineSize = 10000
)

func setupIntegrationCacheReader(t *testing.T) *CacheReader {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set, skipping real-Redis integration test")
	}

	logger := zap.NewNop()
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{
		Addr: addr,
		DB:   integrationRedisDB,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { redisClient.Close() })

	rdb := redisClient.GetClient()
	require.NoError(t, rdb.FlushDB(context.Background()).Err())
	t.Cleanup(func() { rdb.FlushDB(context.Background()) })

	return NewCacheReader(redisClient, redis.NewKeyGenerator(), logger)
}

func populateIntegrationHost(t *testing.T, cr *CacheReader, hostID, count int, now int64) {
	populateIntegrationDimension(t, cr, hostID, 1, count, now)
}

func populateIntegrationDimension(t *testing.T, cr *CacheReader, hostID, dimID, count int, now int64) {
	ctx := context.Background()
	pipe := cr.redis.GetClient().Pipeline()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("meta:cache:%d:%d:%d", hostID, dimID, i)
		pipe.HSet(ctx, key,
			"url", fmt.Sprintf("https://host%d.example.com/page%d", hostID, i),
			"dimension", "mobile",
			"size", "500",
			"created_at", fmt.Sprintf("%d", now-100),
			"expires_at", fmt.Sprintf("%d", now+3600),
			"source", "render",
		)
		if pipe.Len() >= integrationPipelineSize {
			_, err := pipe.Exec(ctx)
			require.NoError(t, err)
		}
	}
	if pipe.Len() > 0 {
		_, err := pipe.Exec(ctx)
		require.NoError(t, err)
	}
}

func TestCacheReaderIntegration_ScanScatter(t *testing.T) {
	cr := setupIntegrationCacheReader(t)
	now := time.Now().Unix()

	// The incident keyspace shape: cluster 2 held 754k keys, ~93% of them one
	// host's, and host 110's 2 precached URLs never surfaced on page 1.
	populateIntegrationHost(t, cr, 107, 100000, now)
	populateIntegrationHost(t, cr, 110, 2, now)
	populateIntegrationHost(t, cr, 111, 60, now)

	t.Run("two keys among 100k plus neighbors returned on page 1", func(t *testing.T) {
		result, err := cr.ListURLs(context.Background(), CacheListParams{
			HostID:   110,
			Cursor:   "0",
			Limit:    25,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		require.Len(t, result.Items, 2)
		assert.Equal(t, "0", result.Cursor)
		assert.False(t, result.HasMore)
	})

	t.Run("scan cursor resumes across requests without loss or duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := "0"
		for pages := 0; ; pages++ {
			require.Less(t, pages, 100, "pagination did not terminate")

			result, err := cr.ListURLs(context.Background(), CacheListParams{
				HostID:   111,
				Cursor:   cursor,
				Limit:    25,
				StaleTTL: 600,
			})
			require.NoError(t, err)
			for _, item := range result.Items {
				seen[item.URL]++
			}
			if !result.HasMore {
				break
			}
			cursor = result.Cursor
		}

		assert.Len(t, seen, 60, "every cached URL must be reachable across paginated listing")
		for url, count := range seen {
			assert.Equal(t, 1, count, "URL %s returned more than once", url)
		}
	})
}

func TestCacheReaderIntegration_SummaryScanScatter(t *testing.T) {
	cr := setupIntegrationCacheReader(t)
	now := time.Now()

	// The 2026-09-26 incident shape: host 1's 835 keys on a shard dominated by
	// other hosts made one unbounded summary Eval walk the whole keyspace.
	const neighborKeys = 100000
	const smallHostKeys = 835
	populateIntegrationHost(t, cr, 107, neighborKeys, now.Unix())
	populateIntegrationHost(t, cr, 1, smallHostKeys, now.Unix())

	cases := []struct {
		name   string
		hostID int
		keys   int
	}{
		{"small host among large neighbor", 1, smallHostKeys},
		{"large host", 107, neighborKeys},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evals := 0
			cr.nowFunc = func() time.Time {
				evals++
				return now
			}

			result, err := cr.GetSummary(context.Background(), tc.hostID, 600)
			require.NoError(t, err)
			require.Greater(t, evals, 1, "walk must span more than one bounded Eval")

			assert.Equal(t, tc.keys, result.TotalUrls)
			assert.Equal(t, tc.keys, result.ActiveCount)
			assert.Zero(t, result.StaleCount)
			assert.Zero(t, result.ExpiredCount)
			assert.Equal(t, int64(tc.keys*500), result.TotalSize)
			assert.Equal(t, map[string]int{"mobile": tc.keys}, result.ByDimension)
			assert.Equal(t, map[string]int{"render": tc.keys}, result.BySource)
		})
	}
}

// Multi-chunk invalidation must run on real Redis: miniredis's SCAN cursor is
// an offset into its sorted key list, so deleting during a walk skips keys.
func TestInvalidateAllIntegration_Chunked(t *testing.T) {
	const neighborKeys = 100000
	const hostKeysPerDim = 2 * scanChunkMaxMatchedKeys

	setup := func(t *testing.T) (*CacheDaemon, *CacheReader) {
		cr := setupIntegrationCacheReader(t)
		now := time.Now().Unix()
		populateIntegrationHost(t, cr, 107, neighborKeys, now)
		populateIntegrationDimension(t, cr, 1, 1, hostKeysPerDim, now)
		populateIntegrationDimension(t, cr, 1, 2, hostKeysPerDim, now)
		return newTestDaemon(cr.redis), cr
	}
	countKeys := func(t *testing.T, cr *CacheReader, pattern string) int {
		keys, err := cr.redis.GetClient().Keys(context.Background(), pattern).Result()
		require.NoError(t, err)
		return len(keys)
	}
	// invalidate returns the entries deleted and the number of Evals the walk took.
	invalidate := func(t *testing.T, daemon *CacheDaemon, cr *CacheReader, req types.InvalidateAllAPIRequest) (int, int) {
		evalsBefore := evalCalls(t, cr)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		ctx := makePostRequest(daemon, "/internal/cache/invalidate-all", body)
		require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		var resp struct {
			Data types.InvalidateAllAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		return resp.Data.EntriesInvalidated, evalCalls(t, cr) - evalsBefore
	}

	t.Run("deletes every entry of the host and none of its neighbor", func(t *testing.T) {
		daemon, cr := setup(t)

		deleted, evals := invalidate(t, daemon, cr, types.InvalidateAllAPIRequest{HostID: 1})

		assert.Greater(t, evals, 1, "walk must span more than one bounded Eval")
		assert.Equal(t, 2*hostKeysPerDim, deleted)
		assert.Zero(t, countKeys(t, cr, "meta:cache:1:*"))
		assert.Equal(t, neighborKeys, countKeys(t, cr, "meta:cache:107:*"))
	})

	t.Run("dimension filter deletes only that dimension", func(t *testing.T) {
		daemon, cr := setup(t)

		deleted, evals := invalidate(t, daemon, cr, types.InvalidateAllAPIRequest{HostID: 1, DimensionIDs: []int{1}})

		assert.Greater(t, evals, 1, "walk must span more than one bounded Eval")
		assert.Equal(t, hostKeysPerDim, deleted)
		assert.Zero(t, countKeys(t, cr, "meta:cache:1:1:*"))
		assert.Equal(t, hostKeysPerDim, countKeys(t, cr, "meta:cache:1:2:*"))
		assert.Equal(t, neighborKeys, countKeys(t, cr, "meta:cache:107:*"))
	})
	t.Run("filtered walk over a host that dominates the shard stays chunked", func(t *testing.T) {
		// No neighbors and a filter that keeps few keys: the whole shard fits in
		// one iteration-capped Eval, so only counting every host key toward the
		// matched cap splits this walk.
		const rejectedKeys = 6 * scanChunkMaxMatchedKeys
		const keptKeys = 100
		require.Less(t, rejectedKeys+keptKeys, scanChunkCount*scanChunkMaxIterations)

		cr := setupIntegrationCacheReader(t)
		now := time.Now().Unix()
		populateIntegrationDimension(t, cr, 1, 2, rejectedKeys, now)
		populateIntegrationDimension(t, cr, 1, 1, keptKeys, now)
		daemon := newTestDaemon(cr.redis)

		deleted, evals := invalidate(t, daemon, cr, types.InvalidateAllAPIRequest{HostID: 1, DimensionIDs: []int{1}})

		assert.Greater(t, evals, 1, "rejected host keys must count toward the per-Eval cap")
		assert.Equal(t, keptKeys, deleted)
		assert.Equal(t, rejectedKeys, countKeys(t, cr, "meta:cache:1:2:*"))
	})
}

// evalCalls reads the server-wide EVAL counter, so a test can prove a walk was
// split into several bounded Evals.
func evalCalls(t *testing.T, cr *CacheReader) int {
	info, err := cr.redis.GetClient().Info(context.Background(), "commandstats").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\r\n") {
		if rest, ok := strings.CutPrefix(line, "cmdstat_eval:calls="); ok {
			calls, _, _ := strings.Cut(rest, ",")
			n, err := strconv.Atoi(calls)
			require.NoError(t, err)
			return n
		}
	}
	return 0
}

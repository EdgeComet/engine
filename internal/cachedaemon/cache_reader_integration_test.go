package cachedaemon

// Real-Redis integration tests for the chunked cache listing. miniredis
// paginates SCAN over the sorted, MATCH-filtered key list and cannot reproduce
// real Redis hash-bucket scatter, where a small host's keys sit behind
// thousands of a large neighbor's keys and a single bounded Eval returns an
// empty page. Gated on TEST_REDIS_ADDR (CI provides a Redis service
// container). Uses a dedicated DB and flushes it around each test.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
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
	ctx := context.Background()
	pipe := cr.redis.GetClient().Pipeline()
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("meta:cache:%d:1:%d", hostID, i)
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

package cachedaemon

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
)

func setupTestCacheReader(t *testing.T) (*CacheReader, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{
		Addr: mr.Addr(),
	}, logger)
	require.NoError(t, err)

	keyGen := redis.NewKeyGenerator()
	cr := NewCacheReader(redisClient, keyGen, logger)
	return cr, mr
}

func populateMetadataHash(mr *miniredis.Miniredis, hostID, dimID int, urlHash string, fields map[string]string) {
	key := fmt.Sprintf("meta:cache:%d:%d:%s", hostID, dimID, urlHash)
	for k, v := range fields {
		mr.HSet(key, k, v)
	}
}

func TestCacheReader_ListURLs(t *testing.T) {
	now := time.Now().Unix()

	t.Run("no filters returns items up to limit", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		for i := 0; i < 5; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("hash%d", i), map[string]string{
				"url":         fmt.Sprintf("https://example.com/page%d", i),
				"dimension":   "mobile",
				"size":        "1000",
				"created_at":  fmt.Sprintf("%d", now-100),
				"expires_at":  fmt.Sprintf("%d", now+3600),
				"status_code": "200",
				"source":      "render",
			})
		}

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    3,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 3)
	})

	t.Run("status filter active only", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// Active entries (expires in future)
		for i := 0; i < 3; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("active%d", i), map[string]string{
				"url":        fmt.Sprintf("https://example.com/active%d", i),
				"dimension":  "mobile",
				"size":       "500",
				"created_at": fmt.Sprintf("%d", now-100),
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     "render",
			})
		}

		// Expired entries (expires in past, beyond stale window)
		for i := 0; i < 2; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("expired%d", i), map[string]string{
				"url":        fmt.Sprintf("https://example.com/expired%d", i),
				"dimension":  "mobile",
				"size":       "500",
				"created_at": fmt.Sprintf("%d", now-7200),
				"expires_at": fmt.Sprintf("%d", now-3600),
				"source":     "render",
			})
		}

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			StatusFilter: "active",
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 3)
		for _, item := range result.Items {
			assert.Equal(t, "active", item.Status)
		}
	})

	t.Run("dimension filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "mobile1", map[string]string{
			"url":        "https://example.com/m1",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 2, "desktop1", map[string]string{
			"url":        "https://example.com/d1",
			"dimension":  "desktop",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:          1,
			Cursor:          "0",
			Limit:           100,
			DimensionFilter: "mobile",
			StaleTTL:        600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "mobile", result.Items[0].Dimension)
	})

	t.Run("url_contains filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "prod1", map[string]string{
			"url":        "https://example.com/products/shoes",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "about1", map[string]string{
			"url":        "https://example.com/about",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:      1,
			Cursor:      "0",
			Limit:       100,
			URLContains: "products",
			StaleTTL:    600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "products")
	})

	t.Run("size range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "small1", map[string]string{
			"url":        "https://example.com/small",
			"dimension":  "mobile",
			"size":       "50",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "med1", map[string]string{
			"url":        "https://example.com/medium",
			"dimension":  "mobile",
			"size":       "300",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "large1", map[string]string{
			"url":        "https://example.com/large",
			"dimension":  "mobile",
			"size":       "1000",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			SizeMin:  100,
			SizeMax:  500,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, int64(300), result.Items[0].Size)
	})

	t.Run("cache age range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// Recent (age ~100s)
		populateMetadataHash(mr, 1, 1, "recent1", map[string]string{
			"url":        "https://example.com/recent",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		// Old (age ~7200s)
		populateMetadataHash(mr, 1, 1, "old1", map[string]string{
			"url":        "https://example.com/old",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-7200),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:      1,
			Cursor:      "0",
			Limit:       100,
			CacheAgeMin: 3600,
			CacheAgeMax: 10000,
			StaleTTL:    600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "old")
	})

	t.Run("cache_age between filter excludes both too young and too old", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// Very recent (age ~60s) - should be excluded by min
		populateMetadataHash(mr, 1, 1, "recent60", map[string]string{
			"url":        "https://example.com/very-recent",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-60),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		// Medium age (~3600s) - should be included
		populateMetadataHash(mr, 1, 1, "medium3600", map[string]string{
			"url":        "https://example.com/medium-age",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		// Very old (age ~86400s) - should be excluded by max
		populateMetadataHash(mr, 1, 1, "old86400", map[string]string{
			"url":        "https://example.com/very-old",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-86400),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:      1,
			Cursor:      "0",
			Limit:       100,
			CacheAgeMin: 1800,
			CacheAgeMax: 7200,
			StaleTTL:    600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "medium-age")
	})

	t.Run("status_code filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "ok1", map[string]string{
			"url":         "https://example.com/ok",
			"dimension":   "mobile",
			"size":        "500",
			"created_at":  fmt.Sprintf("%d", now-100),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"status_code": "200",
			"source":      "render",
		})
		populateMetadataHash(mr, 1, 1, "notfound1", map[string]string{
			"url":         "https://example.com/notfound",
			"dimension":   "mobile",
			"size":        "500",
			"created_at":  fmt.Sprintf("%d", now-100),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"status_code": "404",
			"source":      "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:           1,
			Cursor:           "0",
			Limit:            100,
			StatusCodeFilter: "200",
			StaleTTL:         600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, 200, result.Items[0].StatusCode)
	})

	t.Run("source filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "render1", map[string]string{
			"url":        "https://example.com/rendered",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "bypass1", map[string]string{
			"url":        "https://example.com/bypassed",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "bypass",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			SourceFilter: "render",
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "render", result.Items[0].Source)
	})

	t.Run("index_status filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "idx1", map[string]string{
			"url":          "https://example.com/indexable",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"source":       "render",
			"index_status": "1",
		})
		populateMetadataHash(mr, 1, 1, "idx2", map[string]string{
			"url":          "https://example.com/noindex",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"source":       "render",
			"index_status": "2",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:            1,
			Cursor:            "0",
			Limit:             100,
			IndexStatusFilter: "1",
			StaleTTL:          600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, 1, result.Items[0].IndexStatus)
	})

	t.Run("combined filters", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// This entry matches all filters
		populateMetadataHash(mr, 1, 1, "match1", map[string]string{
			"url":        "https://example.com/products/1",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		// Active but desktop
		populateMetadataHash(mr, 1, 2, "nomatch1", map[string]string{
			"url":        "https://example.com/products/2",
			"dimension":  "desktop",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		// Mobile but bypass source
		populateMetadataHash(mr, 1, 1, "nomatch2", map[string]string{
			"url":        "https://example.com/products/3",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "bypass",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:          1,
			Cursor:          "0",
			Limit:           100,
			StatusFilter:    "active",
			DimensionFilter: "mobile",
			SourceFilter:    "render",
			StaleTTL:        600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "mobile", result.Items[0].Dimension)
		assert.Equal(t, "render", result.Items[0].Source)
	})

	t.Run("limit caps returned items", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		for i := 0; i < 10; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("page%d", i), map[string]string{
				"url":        fmt.Sprintf("https://example.com/page%d", i),
				"dimension":  "mobile",
				"size":       "500",
				"created_at": fmt.Sprintf("%d", now-100),
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     "render",
			})
		}

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    5,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 5)

		// Request all items with large limit
		allResult, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, allResult.Items, 10)
		assert.Equal(t, "0", allResult.Cursor)
		assert.False(t, allResult.HasMore)
	})

	t.Run("empty result", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "entry1", map[string]string{
			"url":        "https://example.com/page",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			SourceFilter: "bypass",
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Items)
		assert.Equal(t, "0", result.Cursor)
		assert.False(t, result.HasMore)
	})

	t.Run("missing optional fields", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "minimal1", map[string]string{
			"url":        "https://example.com/minimal",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			StaleTTL: 600,
		})
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		assert.Equal(t, "", result.Items[0].Title)
		assert.Nil(t, result.Items[0].LastBotHit)
		assert.Equal(t, 0, result.Items[0].IndexStatus)
	})

	t.Run("title contains filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tprod1", map[string]string{
			"url":        "https://example.com/page1",
			"title":      "Product Guide",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tman1", map[string]string{
			"url":        "https://example.com/page2",
			"title":      "User Manual",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tfaq1", map[string]string{
			"url":        "https://example.com/page3",
			"title":      "Product FAQ",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			Title:    "product",
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.Contains(t, strings.ToLower(item.Title), "product")
		}
	})

	t.Run("title contains is case insensitive", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tupper1", map[string]string{
			"url":        "https://example.com/upper",
			"title":      "UPPERCASE TITLE",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			Title:    "uppercase",
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "UPPERCASE TITLE", result.Items[0].Title)
	})

	t.Run("created_at range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "c1hour", map[string]string{
			"url":        "https://example.com/one-hour",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "c2hour", map[string]string{
			"url":        "https://example.com/two-hours",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-7200),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "crecent", map[string]string{
			"url":        "https://example.com/recent",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			CreatedAtMin: now - 8000,
			CreatedAtMax: now - 1000,
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.GreaterOrEqual(t, item.CreatedAt, now-8000)
			assert.LessOrEqual(t, item.CreatedAt, now-1000)
		}
	})

	t.Run("expires_at range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "esoon", map[string]string{
			"url":        "https://example.com/exp-soon",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+1800),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "elater", map[string]string{
			"url":        "https://example.com/exp-later",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+7200),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "efar", map[string]string{
			"url":        "https://example.com/exp-far",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+86400),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			ExpiresAtMin: now + 3600,
			ExpiresAtMax: now + 50000,
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "exp-later")
	})

	t.Run("last_access range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "larecent", map[string]string{
			"url":         "https://example.com/la-recent",
			"dimension":   "mobile",
			"size":        "500",
			"created_at":  fmt.Sprintf("%d", now-3600),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"last_access": fmt.Sprintf("%d", now-60),
			"source":      "render",
		})
		populateMetadataHash(mr, 1, 1, "laold", map[string]string{
			"url":         "https://example.com/la-old",
			"dimension":   "mobile",
			"size":        "500",
			"created_at":  fmt.Sprintf("%d", now-3600),
			"expires_at":  fmt.Sprintf("%d", now+3600),
			"last_access": fmt.Sprintf("%d", now-7200),
			"source":      "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			LastAccessMin: now - 600,
			LastAccessMax: now,
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "la-recent")
	})

	t.Run("last_bot_hit range filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "lbhrecent", map[string]string{
			"url":          "https://example.com/lbh-recent",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-3600),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-100),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhold", map[string]string{
			"url":          "https://example.com/lbh-old",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-3600),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-7200),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhnone", map[string]string{
			"url":        "https://example.com/lbh-none",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			LastBotHitMin: now - 3600,
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "lbh-recent")
	})

	t.Run("last_bot_hit range excludes entries without last_bot_hit", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "lbhwith", map[string]string{
			"url":          "https://example.com/with-lbh",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-3600),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-100),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhwithout", map[string]string{
			"url":        "https://example.com/without-lbh",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			LastBotHitMax: now + 9999,
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "with-lbh")
	})

	t.Run("title and timestamp combined", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "combo1", map[string]string{
			"url":        "https://example.com/combo1",
			"title":      "Product Page",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "combo2", map[string]string{
			"url":        "https://example.com/combo2",
			"title":      "Product Page",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "combo3", map[string]string{
			"url":        "https://example.com/combo3",
			"title":      "About Us",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3600),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:       1,
			Cursor:       "0",
			Limit:        100,
			Title:        "product",
			CreatedAtMin: now - 5000,
			CreatedAtMax: now - 1000,
			StaleTTL:     600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "combo1")
	})

	t.Run("stale_ttl = 0 means no stale entries", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// Entry just expired (1 second ago)
		populateMetadataHash(mr, 1, 1, "justexpired1", map[string]string{
			"url":        "https://example.com/just-expired",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-3700),
			"expires_at": fmt.Sprintf("%d", now-1),
			"source":     "render",
		})
		// Active entry
		populateMetadataHash(mr, 1, 1, "active1", map[string]string{
			"url":        "https://example.com/active",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			StaleTTL: 0,
		})
		require.NoError(t, err)

		staleCount := 0
		for _, item := range result.Items {
			if item.Status == "stale" {
				staleCount++
			}
		}
		assert.Equal(t, 0, staleCount, "with stale_ttl=0 there should be no stale entries")

		// The just-expired entry should be "expired" not "stale"
		for _, item := range result.Items {
			if item.URL == "https://example.com/just-expired" {
				assert.Equal(t, "expired", item.Status)
			}
		}
	})

	t.Run("url starts_with filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "swprod1", map[string]string{
			"url":        "https://example.com/products/shoes",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "swabout1", map[string]string{
			"url":        "https://example.com/about",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			URLStartsWith: "https://example.com/prod",
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "products/shoes")
	})

	t.Run("url ends_with filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "ewpdf1", map[string]string{
			"url":        "https://example.com/page.pdf",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "ewhtml1", map[string]string{
			"url":        "https://example.com/page.html",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:      1,
			Cursor:      "0",
			Limit:       100,
			URLEndsWith: ".pdf",
			StaleTTL:    600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "page.pdf")
	})

	t.Run("url neq filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "neq1", map[string]string{
			"url":        "https://example.com/page1",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "neq2", map[string]string{
			"url":        "https://example.com/page2",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "neq3", map[string]string{
			"url":        "https://example.com/page3",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			URLNeq:   "https://example.com/page2",
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.NotEqual(t, "https://example.com/page2", item.URL)
		}
	})

	t.Run("url not_contains filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "ncadmin1", map[string]string{
			"url":        "https://example.com/admin/settings",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "ncprod1", map[string]string{
			"url":        "https://example.com/products/shoes",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "ncadmin2", map[string]string{
			"url":        "https://example.com/admin/users",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:         1,
			Cursor:         "0",
			Limit:          100,
			URLNotContains: "admin",
			StaleTTL:       600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "products")
	})

	t.Run("title starts_with filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tsw1", map[string]string{
			"url":        "https://example.com/p1",
			"title":      "Getting Started Guide",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tsw2", map[string]string{
			"url":        "https://example.com/p2",
			"title":      "API Reference",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tsw3", map[string]string{
			"url":        "https://example.com/p3",
			"title":      "Getting Help",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:          1,
			Cursor:          "0",
			Limit:           100,
			TitleStartsWith: "getting",
			StaleTTL:        600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.True(t, strings.HasPrefix(strings.ToLower(item.Title), "getting"))
		}
	})

	t.Run("title ends_with filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tew1", map[string]string{
			"url":        "https://example.com/p1",
			"title":      "User Guide",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tew2", map[string]string{
			"url":        "https://example.com/p2",
			"title":      "Admin Guide",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tew3", map[string]string{
			"url":        "https://example.com/p3",
			"title":      "Quick Start",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			TitleEndsWith: "guide",
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.True(t, strings.HasSuffix(strings.ToLower(item.Title), "guide"))
		}
	})

	t.Run("title neq filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tneq1", map[string]string{
			"url":        "https://example.com/p1",
			"title":      "Home",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tneq2", map[string]string{
			"url":        "https://example.com/p2",
			"title":      "About",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tneq3", map[string]string{
			"url":        "https://example.com/p3",
			"title":      "Contact",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:   1,
			Cursor:   "0",
			Limit:    100,
			TitleNeq: "about",
			StaleTTL: 600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.NotEqual(t, "About", item.Title)
		}
	})

	t.Run("title not_contains filter", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "tnc1", map[string]string{
			"url":        "https://example.com/p1",
			"title":      "Product A Review",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tnc2", map[string]string{
			"url":        "https://example.com/p2",
			"title":      "Product B Info",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "tnc3", map[string]string{
			"url":        "https://example.com/p3",
			"title":      "Company News",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:           1,
			Cursor:           "0",
			Limit:            100,
			TitleNotContains: "product",
			StaleTTL:         600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "Company News", result.Items[0].Title)
	})

	t.Run("last_bot_hit_exists true", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "lbhe1", map[string]string{
			"url":          "https://example.com/hit1",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-500),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhe2", map[string]string{
			"url":          "https://example.com/hit2",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-200),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhe3", map[string]string{
			"url":        "https://example.com/nohit",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:           1,
			Cursor:           "0",
			Limit:            100,
			LastBotHitExists: "true",
			StaleTTL:         600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		for _, item := range result.Items {
			assert.NotNil(t, item.LastBotHit)
		}
	})

	t.Run("last_bot_hit_exists false", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "lbhef1", map[string]string{
			"url":          "https://example.com/hit1",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-500),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhef2", map[string]string{
			"url":          "https://example.com/hit2",
			"dimension":    "mobile",
			"size":         "500",
			"created_at":   fmt.Sprintf("%d", now-100),
			"expires_at":   fmt.Sprintf("%d", now+3600),
			"last_bot_hit": fmt.Sprintf("%d", now-200),
			"source":       "render",
		})
		populateMetadataHash(mr, 1, 1, "lbhef3", map[string]string{
			"url":        "https://example.com/nohit",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:           1,
			Cursor:           "0",
			Limit:            100,
			LastBotHitExists: "false",
			StaleTTL:         600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Nil(t, result.Items[0].LastBotHit)
	})

	t.Run("string ops are case insensitive", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "cicase1", map[string]string{
			"url":        "https://Example.COM/Products/Shoes",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:        1,
			Cursor:        "0",
			Limit:         100,
			URLStartsWith: "https://example.com/products",
			StaleTTL:      600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
	})

	t.Run("combined string ops", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		populateMetadataHash(mr, 1, 1, "cso1", map[string]string{
			"url":        "https://example.com/products/shoes",
			"title":      "Running Shoes",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "cso2", map[string]string{
			"url":        "https://example.com/products/hats",
			"title":      "Summer Hats",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})
		populateMetadataHash(mr, 1, 1, "cso3", map[string]string{
			"url":        "https://example.com/about",
			"title":      "About Running",
			"dimension":  "mobile",
			"size":       "500",
			"created_at": fmt.Sprintf("%d", now-100),
			"expires_at": fmt.Sprintf("%d", now+3600),
			"source":     "render",
		})

		result, err := cr.ListURLs(CacheListParams{
			HostID:           1,
			Cursor:           "0",
			Limit:            100,
			URLStartsWith:    "https://example.com/products",
			TitleNotContains: "running",
			StaleTTL:         600,
		})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Contains(t, result.Items[0].URL, "hats")
	})
}

func TestCacheReader_GetSummary(t *testing.T) {
	now := time.Now().Unix()

	t.Run("counts active stale expired correctly", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		staleTTL := int64(600)

		// 60 active entries
		for i := 0; i < 60; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("active%d", i), map[string]string{
				"dimension":  "mobile",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     "render",
			})
		}

		// 25 stale entries (expired but within stale_ttl window)
		for i := 0; i < 25; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("stale%d", i), map[string]string{
				"dimension":  "desktop",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now-300),
				"source":     "render",
			})
		}

		// 15 expired entries (past stale_ttl window)
		for i := 0; i < 15; i++ {
			populateMetadataHash(mr, 1, 2, fmt.Sprintf("expired%d", i), map[string]string{
				"dimension":  "mobile",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now-3600),
				"source":     "bypass",
			})
		}

		result, err := cr.GetSummary(1, staleTTL)
		require.NoError(t, err)
		assert.Equal(t, 100, result.TotalUrls)
		assert.Equal(t, 60, result.ActiveCount)
		assert.Equal(t, 25, result.StaleCount)
		assert.Equal(t, 15, result.ExpiredCount)
	})

	t.Run("totalSize is sum of all size fields", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		for i := 0; i < 10; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("sized%d", i), map[string]string{
				"dimension":  "mobile",
				"size":       "1000",
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     "render",
			})
		}

		result, err := cr.GetSummary(1, 600)
		require.NoError(t, err)
		assert.Equal(t, int64(10000), result.TotalSize)
	})

	t.Run("byDimension and bySource breakdowns", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		for i := 0; i < 40; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("mob%d", i), map[string]string{
				"dimension":  "mobile",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     "render",
			})
		}
		for i := 0; i < 60; i++ {
			src := "render"
			if i >= 30 {
				src = "bypass"
			}
			populateMetadataHash(mr, 1, 2, fmt.Sprintf("desk%d", i), map[string]string{
				"dimension":  "desktop",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now+3600),
				"source":     src,
			})
		}

		result, err := cr.GetSummary(1, 600)
		require.NoError(t, err)
		assert.Equal(t, 40, result.ByDimension["mobile"])
		assert.Equal(t, 60, result.ByDimension["desktop"])
		assert.Equal(t, 70, result.BySource["render"])
		assert.Equal(t, 30, result.BySource["bypass"])
	})

	t.Run("stale_ttl = 0 means no stale entries", func(t *testing.T) {
		cr, mr := setupTestCacheReader(t)

		// Entry just expired
		for i := 0; i < 5; i++ {
			populateMetadataHash(mr, 1, 1, fmt.Sprintf("justexp%d", i), map[string]string{
				"dimension":  "mobile",
				"size":       "100",
				"expires_at": fmt.Sprintf("%d", now-1),
				"source":     "render",
			})
		}

		result, err := cr.GetSummary(1, 0)
		require.NoError(t, err)
		assert.Equal(t, 0, result.StaleCount)
		assert.Equal(t, 5, result.ExpiredCount)
	})

	t.Run("empty host returns all zeros", func(t *testing.T) {
		cr, _ := setupTestCacheReader(t)

		result, err := cr.GetSummary(999, 600)
		require.NoError(t, err)
		assert.Equal(t, 0, result.TotalUrls)
		assert.Equal(t, 0, result.ActiveCount)
		assert.Equal(t, 0, result.StaleCount)
		assert.Equal(t, 0, result.ExpiredCount)
		assert.Equal(t, int64(0), result.TotalSize)
		assert.Empty(t, result.ByDimension)
		assert.Empty(t, result.BySource)
	})
}

package cachedaemon

import (
	"fmt"
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

	t.Run("urlContains filter", func(t *testing.T) {
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

	t.Run("statusCode filter", func(t *testing.T) {
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

	t.Run("indexStatus filter", func(t *testing.T) {
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

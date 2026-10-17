package recache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/hash"
	customredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	fetchQueueURL    = "https://example.com/a,b/53"
	fetchEncodedURL  = "https://example.com/a%2Cb/53"
	fetchRecacheID   = "recache-1-0-fetch"
	fetchWriterID    = "live-writer"
	fetchStoredTTL   = time.Hour
	fetchDimensionID = 0
)

func fetchURLService(t *testing.T) (*RecacheService, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := customredis.NewClient(&config.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)

	metadataStore := cache.NewMetadataStore(redisClient, customredis.NewKeyGenerator(), t.TempDir(), logger)
	fsCache := cache.NewFilesystemCache(logger)
	cacheService := cache.NewCacheService(metadataStore, fsCache, logger)

	return &RecacheService{
		cacheCoord:    orchestrator.NewCacheCoordinator(metadataStore, fsCache, cacheService, localShardingManager{}, nil, logger),
		metadataStore: metadataStore,
		logger:        logger,
	}, mr
}

// fetchURLContext is the context buildRecacheContext produces for queueURL.
func fetchURLContext(t *testing.T, queueURL string) *edgectx.RenderContext {
	t.Helper()
	normalizer := hash.NewURLNormalizer()
	result, err := normalizer.Normalize(queueURL, nil)
	require.NoError(t, err)
	urlHash := normalizer.Hash(result.NormalizedURL)

	return &edgectx.RenderContext{
		TargetURL: queueURL,
		FetchURL:  queueURL,
		URLHash:   urlHash,
		Host:      &types.Host{ID: 1, Domain: "example.com"},
		Dimension: "desktop",
		CacheKey:  &types.CacheKey{HostID: 1, DimensionID: fetchDimensionID, URLHash: urlHash},
		RequestID: fetchRecacheID,
		Logger:    zap.NewNop(),
	}
}

func seedFetchSpelling(t *testing.T, rs *RecacheService, renderCtx *edgectx.RenderContext, fetchURL, fetchURLRequestID string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, rs.metadataStore.StoreMetadata(context.Background(), &cache.CacheMetadata{
		Key:               renderCtx.CacheKey.String(),
		URL:               renderCtx.TargetURL,
		HostID:            renderCtx.Host.ID,
		Dimension:         renderCtx.Dimension,
		RequestID:         fetchWriterID,
		CreatedAt:         now,
		ExpiresAt:         now.Add(fetchStoredTTL),
		LastAccess:        now,
		Source:            cache.SourceBypass,
		StatusCode:        200,
		FetchURL:          fetchURL,
		FetchURLRequestID: fetchURLRequestID,
	}, renderCtx.CacheKey, 0))
}

func TestResolveFetchURL(t *testing.T) {
	tests := []struct {
		name       string
		queueURL   string
		seed       bool
		spelling   string
		writerID   string
		wantFetch  string
		wantExact  bool
		wantReason string
		wantStored string
		closeRedis bool
	}{
		{
			name: "stored spelling of the same key is used", queueURL: fetchQueueURL,
			seed: true, spelling: fetchEncodedURL, writerID: fetchWriterID,
			wantFetch: fetchEncodedURL, wantExact: true,
		},
		{
			name: "lowercase hex is kept", queueURL: fetchQueueURL,
			seed: true, spelling: "https://example.com/a%2cb/53", writerID: fetchWriterID,
			wantFetch: "https://example.com/a%2cb/53", wantExact: true,
		},
		{
			name: "exact save equal to the entry URL fetches that URL exactly", queueURL: fetchQueueURL,
			seed: true, spelling: "", writerID: fetchWriterID,
			wantFetch: fetchQueueURL, wantExact: true,
		},
		{
			name: "spelling of another key is ignored", queueURL: fetchQueueURL,
			seed: true, spelling: "https://example.com/other%2Cb/53", writerID: fetchWriterID,
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonMismatch, wantStored: "https://example.com/other%2Cb/53",
		},
		{
			name: "non-exact spelling is ignored", queueURL: "https://example.com/a!b",
			seed: true, spelling: "https://example.com/a!b", writerID: fetchWriterID,
			wantFetch: "https://example.com/a!b", wantReason: fetchURLReasonMismatch, wantStored: "https://example.com/a!b",
		},
		{
			name: "unparsable spelling is ignored", queueURL: fetchQueueURL,
			seed: true, spelling: "https://example.com/a%zz", writerID: fetchWriterID,
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonMismatch, wantStored: "https://example.com/a%zz",
		},
		{
			name: "spelling written by another request is ignored", queueURL: fetchQueueURL,
			seed: true, spelling: fetchEncodedURL, writerID: "older-writer",
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonNoSpelling,
		},
		{
			name: "legacy save keeps the queue URL", queueURL: fetchQueueURL,
			seed: true, spelling: "", writerID: "",
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonNoSpelling,
		},
		{
			name: "missing entry keeps the queue URL", queueURL: fetchQueueURL,
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonNoEntry,
		},
		{
			name: "redis error keeps the queue URL", queueURL: fetchQueueURL,
			seed: true, spelling: fetchEncodedURL, writerID: fetchWriterID, closeRedis: true,
			wantFetch: fetchQueueURL, wantReason: fetchURLReasonNoEntry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, mr := fetchURLService(t)
			renderCtx := fetchURLContext(t, tt.queueURL)
			core, logs := observer.New(zapcore.DebugLevel)
			renderCtx.Logger = zap.New(core)
			if tt.seed {
				seedFetchSpelling(t, rs, renderCtx, tt.spelling, tt.writerID)
			}
			if tt.closeRedis {
				mr.Close()
			}

			rs.resolveFetchURL(renderCtx)

			assert.Equal(t, tt.wantFetch, renderCtx.FetchURL)
			assert.Equal(t, tt.wantExact, renderCtx.FetchExact)
			assert.Equal(t, tt.queueURL, renderCtx.TargetURL, "the queue URL stays the target")

			message := "Recache fetches the stored spelling"
			if tt.wantReason != "" {
				message = "Recache fetches the queue URL"
			}
			entries := logs.FilterMessage(message).All()
			require.Len(t, entries, 1)
			fields := entries[0].ContextMap()
			assert.Equal(t, tt.wantFetch, fields["fetch_url"], "fetch_url is the URL the origin receives")
			if tt.wantReason != "" {
				assert.Equal(t, tt.wantReason, fields["reason"])
				assert.Equal(t, tt.wantStored, fields["stored_fetch_url"])
			}
		})
	}
}

func TestProcessRecacheRenderFetchesStoredSpelling(t *testing.T) {
	rs, _, sentRequest := stampRenderRecacheService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><head><title>Page</title></head><body>rendered</body></html>",
		Metrics: types.PageMetrics{StatusCode: 200},
	})
	renderCtx := fetchURLContext(t, fetchQueueURL)
	seedFetchSpelling(t, rs, renderCtx, fetchEncodedURL, fetchWriterID)

	require.NoError(t, rs.ProcessRecache(context.Background(), fetchQueueURL, 1, fetchDimensionID, types.RecacheModeRender))

	assert.Equal(t, fetchEncodedURL, sentRequest().URL)

	entry, err := rs.metadataStore.GetCacheEntry(context.Background(), renderCtx.CacheKey)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, fetchQueueURL, entry.URL)
	assert.Equal(t, fetchEncodedURL, entry.EffectiveFetchURL(), "the recache save keeps the spelling for the next pass")
}

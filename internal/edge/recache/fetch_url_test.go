package recache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestResolveFetchURL_QuerySpellings saves each live request the way the cache coordinator does and
// recaches the entry's queue URL, which carries the normalized query.
func TestResolveFetchURL_QuerySpellings(t *testing.T) {
	patterns, err := config.CompileStripPatterns([]string{"utm_*"})
	require.NoError(t, err)

	tests := []struct {
		name      string
		liveURL   string
		wantFetch string
	}{
		{name: "stripped name absent from the stored spelling", liveURL: "https://example.com/q?utm_source=x&brand=a,b", wantFetch: "https://example.com/q?brand=a,b"},
		{name: "duplicate values of one name", liveURL: "https://example.com/q?b=1&a=2,1&%61=1", wantFetch: "https://example.com/q?b=1&a=2,1&%61=1"},
		{name: "empty value", liveURL: "https://example.com/q?key=", wantFetch: "https://example.com/q?key="},
		{name: "escaped space", liveURL: "https://example.com/q?q=a%20b", wantFetch: "https://example.com/q?q=a%20b"},
		{name: "query that fell back", liveURL: "https://example.com/q?id=5;x=1&a=1", wantFetch: "https://example.com/q?a=1"},
		{name: "query that fell back next to a respelled path", liveURL: "https://example.com/a%2Cb/53?id=5;x=1&a=1", wantFetch: "https://example.com/a%2Cb/53?a=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			live, err := hash.NewURLNormalizer().Normalize(tt.liveURL, patterns)
			require.NoError(t, err)
			require.True(t, live.FetchExact)
			require.Equal(t, tt.wantFetch, live.FetchURL)

			rs, _ := fetchURLService(t)
			renderCtx := fetchURLContext(t, live.NormalizedURL)
			spelling := ""
			if live.FetchURL != live.NormalizedURL {
				spelling = live.FetchURL
			}
			seedFetchSpelling(t, rs, renderCtx, spelling, fetchWriterID)

			rs.resolveFetchURL(renderCtx)

			assert.Equal(t, tt.wantFetch, renderCtx.FetchURL)
			assert.True(t, renderCtx.FetchExact)
		})
	}
}

func TestProcessRecacheKeepsStoredQuerySpelling(t *testing.T) {
	const (
		queueURL = "https://example.com/q?brand=a%2Cb"
		spelling = "https://example.com/q?brand=a,b"
	)
	rs, _, sentRequest := stampRenderRecacheService(t, types.RenderResponse{
		Success: true,
		HTML:    "<html><head><title>Page</title></head><body>rendered</body></html>",
		Metrics: types.PageMetrics{StatusCode: 200},
	})
	renderCtx := fetchURLContext(t, queueURL)
	seedFetchSpelling(t, rs, renderCtx, spelling, fetchWriterID)

	for pass := range 2 {
		require.NoError(t, rs.ProcessRecache(context.Background(), queueURL, 1, fetchDimensionID, types.RecacheModeRender))
		assert.Equal(t, spelling, sentRequest().URL, "pass %d", pass)

		entry, err := rs.metadataStore.GetCacheEntry(context.Background(), renderCtx.CacheKey)
		require.NoError(t, err)
		require.NotNil(t, entry)
		assert.Equal(t, queueURL, entry.URL)
		assert.Equal(t, spelling, entry.EffectiveFetchURL(), "pass %d", pass)
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

// TestProcessBypassRecacheNeverServedQuery recaches a URL no live request has saved, against an origin
// that redirects the encoded comma to the raw one.
func TestProcessBypassRecacheNeverServedQuery(t *testing.T) {
	const (
		queuePath     = "/shop/?brand=a%2Cb"
		commaPath     = "/shop/?brand=a,b"
		commaLocation = "?brand=a,b"
		encodedComma  = "%2C"
		passesBefore  = 2
	)

	var requestURI string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI = r.RequestURI
		if strings.Contains(r.URL.RawQuery, encodedComma) {
			w.Header().Set("Location", commaLocation)
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>Shop</title></head><body>shop</body></html>"))
	}))
	t.Cleanup(origin.Close)
	queueURL := origin.URL + queuePath

	tests := []struct {
		name          string
		cacheable     []int
		wantErrorType string
	}{
		{name: "cacheable redirect is cached", cacheable: []int{http.StatusOK, http.StatusMovedPermanently}},
		{name: "uncacheable redirect is a permanent failure", cacheable: []int{http.StatusOK}, wantErrorType: types.ErrorTypeOriginRedirect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, _ := fetchURLService(t)
			rs.bypassSvc = stampBypassService()

			newContext := func() *edgectx.RenderContext {
				renderCtx := fetchURLContext(t, queueURL)
				renderCtx.ResolvedConfig = &config.ResolvedConfig{
					Action:      types.ActionBypass,
					Compression: types.CompressionNone,
					Bypass: config.ResolvedBypassConfig{Cache: config.ResolvedBypassCacheConfig{
						Enabled: true, TTL: fetchStoredTTL, StatusCodes: tt.cacheable,
					}},
				}
				return renderCtx
			}
			recache := func() (*cache.CacheMetadata, error) {
				renderCtx := newContext()
				rs.resolveFetchURL(renderCtx)
				err := rs.processBypassRecache(context.Background(), queueURL, renderCtx, time.Now())
				entry, lookupErr := rs.metadataStore.GetCacheEntry(context.Background(), renderCtx.CacheKey)
				require.NoError(t, lookupErr)
				return entry, err
			}

			for pass := range passesBefore {
				entry, err := recache()
				assert.Equal(t, queuePath, requestURI, "pass %d", pass)

				if tt.wantErrorType == "" {
					require.NoError(t, err, "pass %d", pass)
					require.NotNil(t, entry)
					assert.Equal(t, http.StatusMovedPermanently, entry.StatusCode)
					assert.Equal(t, []string{commaLocation}, entry.Headers["Location"])
					assert.Empty(t, entry.EffectiveFetchURL(), "a recache save records no spelling, so the next pass fetches the queue URL again")
					continue
				}
				var failure *recacheError
				require.ErrorAs(t, err, &failure, "pass %d", pass)
				assert.Equal(t, tt.wantErrorType, failure.errorType)
				assert.True(t, failure.permanent)
				assert.Equal(t, commaLocation, failure.redirectTo)
				assert.Nil(t, entry)
			}

			seedFetchSpelling(t, rs, newContext(), origin.URL+commaPath, fetchWriterID)

			entry, err := recache()
			require.NoError(t, err)
			assert.Equal(t, commaPath, requestURI, "the live save's spelling reaches the origin")
			require.NotNil(t, entry)
			assert.Equal(t, http.StatusOK, entry.StatusCode)
			assert.Equal(t, origin.URL+commaPath, entry.EffectiveFetchURL())
		})
	}
}

package cachedaemon

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	skipHostID = 1

	// Host 1 of newTestDaemon: dimension 0 is bypass, 1 and 2 render by default.
	// setupSkipTestDaemon adds skipDimStatus.
	skipDimBypass  = 0
	skipDimRender  = 1
	skipDimRender2 = 2
	skipDimStatus  = 4

	skipStatusDimName = "gone"

	skipRecachePath = "/internal/cache/recache"
	skipURL         = "https://example.com/skip"
	skipURLA        = "https://example.com/skip-a"
	skipURLB        = "https://example.com/skip-b"
	skipURLC        = "https://example.com/skip-c"
	skipInvalidURL  = "/no-host"

	skipFreshIn    = time.Hour
	skipStaleSince = -time.Minute
	skipCopyTTL    = 24 * time.Hour
	skipStaleTTL   = time.Hour
	skipWindow2h   = int64(2 * time.Hour / time.Second)

	skipMsgReadFailed     = "Failed to get cache metadata"
	skipMsgRedisReadFail  = "Redis HGETALL failed"
	skipMsgRecacheSummary = "Recache request processed"
	skipFieldSkipped      = "entries_skipped"
	skipFieldApplied      = "skip_applied"
	skipWrongTypeValue    = "x"
)

// setupSkipTestDaemon reuses newTestDaemon with an observed logger on both the daemon and
// its Redis client, and adds a status_404 dimension to host 1.
func setupSkipTestDaemon(t *testing.T) (*CacheDaemon, *miniredis.Miniredis, *observer.ObservedLogs) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{Addr: mr.Addr()}, logger)
	require.NoError(t, err)

	daemon := newTestDaemon(redisClient)
	daemon.logger = logger
	daemon.GetHost(skipHostID).Dimensions[skipStatusDimName] = types.Dimension{ID: skipDimStatus, Action: types.ActionStatus404}
	return daemon, mr, logs
}

func skipWindow(v int64) *int64 { return &v }

func skipNormalize(t *testing.T, daemon *CacheDaemon, rawURL string) (string, uint64) {
	t.Helper()
	normalizedURL, urlHash, err := daemon.normalizeURLForHost(daemon.GetHost(skipHostID), rawURL)
	require.NoError(t, err)
	return normalizedURL, urlHash
}

func skipMetaKey(t *testing.T, daemon *CacheDaemon, rawURL string, dimID int) string {
	t.Helper()
	_, urlHash := skipNormalize(t, daemon, rawURL)
	return daemon.keyGenerator.GenerateMetadataKey(daemon.keyGenerator.GenerateCacheKey(skipHostID, dimID, urlHash))
}

// seedSkipEntry writes the metadata hash through the edge's MetadataStore, so field names,
// value encoding, key shape and the TTL+stale Redis expiry match a real cache write.
func seedSkipEntry(t *testing.T, daemon *CacheDaemon, rawURL string, dimID int, source string, expiresAt time.Time, statusCode int) {
	t.Helper()
	host := daemon.GetHost(skipHostID)
	normalizedURL, urlHash := skipNormalize(t, daemon, rawURL)
	cacheKey := daemon.keyGenerator.GenerateCacheKey(skipHostID, dimID, urlHash)
	createdAt := expiresAt.Add(-skipCopyTTL)

	store := cache.NewMetadataStore(daemon.redis, daemon.keyGenerator, "", zap.NewNop())
	require.NoError(t, store.StoreMetadata(context.Background(), &cache.CacheMetadata{
		Key:        cacheKey.String(),
		URL:        normalizedURL,
		HostID:     skipHostID,
		Dimension:  dimensionIDToName(host)[dimID],
		CreatedAt:  createdAt,
		ExpiresAt:  expiresAt,
		LastAccess: createdAt,
		Source:     source,
		StatusCode: statusCode,
	}, cacheKey, skipStaleTTL))
}

func postSkipRecache(t *testing.T, daemon *CacheDaemon, req types.RecacheAPIRequest) *fasthttp.RequestCtx {
	t.Helper()
	req.HostID = skipHostID
	req.Priority = redis.PriorityNormal
	body, err := json.Marshal(req)
	require.NoError(t, err)
	return makePostRequest(daemon, skipRecachePath, body)
}

func decodeSkipRecache(t *testing.T, ctx *fasthttp.RequestCtx) types.RecacheAPIData {
	t.Helper()
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode(), string(ctx.Response.Body()))
	var resp struct {
		Data types.RecacheAPIData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
	return resp.Data
}

func queuedSkipMembers(t *testing.T, daemon *CacheDaemon, mr *miniredis.Miniredis) []types.RecacheMember {
	t.Helper()
	queueKey := daemon.keyGenerator.RecacheQueueKey(skipHostID, redis.PriorityNormal)
	if !mr.Exists(queueKey) {
		return nil
	}
	raw, err := mr.ZMembers(queueKey)
	require.NoError(t, err)
	members := make([]types.RecacheMember, 0, len(raw))
	for _, m := range raw {
		var member types.RecacheMember
		require.NoError(t, json.Unmarshal([]byte(m), &member))
		members = append(members, member)
	}
	return members
}

func skipMember(t *testing.T, daemon *CacheDaemon, rawURL string, dimID int, mode string) types.RecacheMember {
	t.Helper()
	normalizedURL, _ := skipNormalize(t, daemon, rawURL)
	return types.RecacheMember{URL: normalizedURL, DimensionID: dimID, Mode: mode}
}

type skipSeed struct {
	source     string
	expiresIn  time.Duration
	statusCode int
}

func freshSeed(source string) *skipSeed {
	return &skipSeed{source: source, expiresIn: skipFreshIn, statusCode: fasthttp.StatusOK}
}

func TestRecacheSkip_FreshnessRules(t *testing.T) {
	cases := []struct {
		name    string
		dimID   int
		mode    string
		seed    *skipSeed
		window  int64
		skipped bool
	}{
		{name: "no entry", dimID: skipDimRender, seed: nil, skipped: false},
		{name: "fresh render entry on render dimension", dimID: skipDimRender, seed: freshSeed(cache.SourceRender), skipped: true},
		{name: "fresh bypass entry on render dimension", dimID: skipDimRender, seed: freshSeed(cache.SourceBypass), skipped: false},
		{name: "fresh bypass entry on bypass dimension", dimID: skipDimBypass, seed: freshSeed(cache.SourceBypass), skipped: true},
		{name: "fresh render entry on bypass dimension", dimID: skipDimBypass, seed: freshSeed(cache.SourceRender), skipped: true},
		{name: "mode render over fresh bypass entry on bypass dimension", dimID: skipDimBypass, mode: types.RecacheModeRender, seed: freshSeed(cache.SourceBypass), skipped: false},
		{name: "mode render over fresh render entry on bypass dimension", dimID: skipDimBypass, mode: types.RecacheModeRender, seed: freshSeed(cache.SourceRender), skipped: true},
		{name: "mode bypass over fresh render entry on render dimension", dimID: skipDimRender, mode: types.RecacheModeBypass, seed: freshSeed(cache.SourceRender), skipped: true},
		{name: "mode bypass over fresh bypass entry on render dimension", dimID: skipDimRender, mode: types.RecacheModeBypass, seed: freshSeed(cache.SourceBypass), skipped: true},
		{name: "render entry expiring in 1h, window 2h", dimID: skipDimRender, seed: &skipSeed{cache.SourceRender, time.Hour, fasthttp.StatusOK}, window: skipWindow2h, skipped: false},
		{name: "render entry expiring in 3h, window 2h", dimID: skipDimRender, seed: &skipSeed{cache.SourceRender, 3 * time.Hour, fasthttp.StatusOK}, window: skipWindow2h, skipped: true},
		// The handler's now is never earlier than the seed's, so the copy cannot outlive the window.
		{name: "render entry expiring exactly at the window", dimID: skipDimRender, seed: &skipSeed{cache.SourceRender, 2 * time.Hour, fasthttp.StatusOK}, window: skipWindow2h, skipped: false},
		{name: "stale render entry on render dimension", dimID: skipDimRender, seed: &skipSeed{cache.SourceRender, skipStaleSince, fasthttp.StatusOK}, skipped: false},
		{name: "stale bypass entry on bypass dimension", dimID: skipDimBypass, seed: &skipSeed{cache.SourceBypass, skipStaleSince, fasthttp.StatusOK}, skipped: false},
		{name: "fresh render 404 on render dimension", dimID: skipDimRender, seed: &skipSeed{cache.SourceRender, skipFreshIn, fasthttp.StatusNotFound}, skipped: true},
		{name: "fresh entry without source on render dimension", dimID: skipDimRender, seed: freshSeed(""), skipped: false},
		{name: "fresh entry without source on bypass dimension", dimID: skipDimBypass, seed: freshSeed(""), skipped: true},
		{name: "fresh render entry, max int64 window", dimID: skipDimRender, seed: freshSeed(cache.SourceRender), window: math.MaxInt64, skipped: false},
		{name: "fresh render entry on status dimension", dimID: skipDimStatus, seed: freshSeed(cache.SourceRender), skipped: true},
		{name: "fresh bypass entry on status dimension", dimID: skipDimStatus, seed: freshSeed(cache.SourceBypass), skipped: true},
		{name: "stale render entry on status dimension", dimID: skipDimStatus, seed: &skipSeed{cache.SourceRender, skipStaleSince, fasthttp.StatusOK}, skipped: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			daemon, mr, _ := setupSkipTestDaemon(t)
			if tc.seed != nil {
				expiresAt := time.Now().UTC().Add(tc.seed.expiresIn)
				seedSkipEntry(t, daemon, skipURL, tc.dimID, tc.seed.source, expiresAt, tc.seed.statusCode)
			}

			data := decodeSkipRecache(t, postSkipRecache(t, daemon, types.RecacheAPIRequest{
				URLs:           []string{skipURL},
				DimensionIDs:   []int{tc.dimID},
				Mode:           tc.mode,
				SkipIfFreshFor: skipWindow(tc.window),
			}))

			assert.True(t, data.SkipApplied)
			assert.Equal(t, 1, data.URLsCount)
			assert.Equal(t, 1, data.DimensionIDsCount)
			if tc.skipped {
				assert.Equal(t, 1, data.EntriesSkipped)
				assert.Equal(t, 0, data.EntriesEnqueued)
				assert.Empty(t, queuedSkipMembers(t, daemon, mr))
				return
			}
			assert.Equal(t, 0, data.EntriesSkipped)
			assert.Equal(t, 1, data.EntriesEnqueued)
			assert.Equal(t, []types.RecacheMember{skipMember(t, daemon, skipURL, tc.dimID, tc.mode)}, queuedSkipMembers(t, daemon, mr))
		})
	}
}

// A WRONGTYPE value at the metadata key makes any HGETALL fail and log, while ZADD on the
// queue key still works; the absence of the log proves no metadata read happened.
func TestRecacheSkip_FieldAbsentReadsNoMetadata(t *testing.T) {
	daemon, mr, logs := setupSkipTestDaemon(t)
	require.NoError(t, mr.Set(skipMetaKey(t, daemon, skipURL, skipDimRender), skipWrongTypeValue))

	ctx := postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:         []string{skipURL},
		DimensionIDs: []int{skipDimRender},
	})
	data := decodeSkipRecache(t, ctx)

	assert.Equal(t, 1, data.EntriesEnqueued)
	assert.Equal(t, 0, data.EntriesSkipped)
	assert.False(t, data.SkipApplied)
	assert.Equal(t, []types.RecacheMember{skipMember(t, daemon, skipURL, skipDimRender, "")}, queuedSkipMembers(t, daemon, mr))
	assert.Zero(t, logs.FilterMessage(skipMsgReadFailed).Len())
	assert.Zero(t, logs.FilterMessage(skipMsgRedisReadFail).Len())

	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ctx.Response.Body(), &raw))
	assert.JSONEq(t, "0", string(raw.Data[skipFieldSkipped]))
	assert.JSONEq(t, "false", string(raw.Data[skipFieldApplied]))
}

func TestRecacheSkip_ReadErrorFailsOpen(t *testing.T) {
	daemon, mr, logs := setupSkipTestDaemon(t)
	require.NoError(t, mr.Set(skipMetaKey(t, daemon, skipURL, skipDimRender), skipWrongTypeValue))

	data := decodeSkipRecache(t, postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:           []string{skipURL},
		DimensionIDs:   []int{skipDimRender},
		SkipIfFreshFor: skipWindow(0),
	}))

	assert.Equal(t, 1, data.EntriesEnqueued)
	assert.Equal(t, 0, data.EntriesSkipped)
	assert.True(t, data.SkipApplied)
	assert.Equal(t, []types.RecacheMember{skipMember(t, daemon, skipURL, skipDimRender, "")}, queuedSkipMembers(t, daemon, mr))
	assert.Equal(t, 1, logs.FilterMessage(skipMsgReadFailed).Len())
}

func TestRecacheSkip_NegativeWindowRejected(t *testing.T) {
	daemon, mr, _ := setupSkipTestDaemon(t)
	seedSkipEntry(t, daemon, skipURL, skipDimRender, cache.SourceRender, time.Now().UTC().Add(skipFreshIn), fasthttp.StatusOK)

	ctx := postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:           []string{skipURL},
		DimensionIDs:   []int{skipDimRender},
		SkipIfFreshFor: skipWindow(-1),
	})

	require.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	var resp httputil.APIResponse
	require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
	assert.False(t, resp.Success)
	assert.Equal(t, skipIfFreshForNegativeMessage, resp.Message)
	assert.Empty(t, queuedSkipMembers(t, daemon, mr))
}

func TestRecacheSkip_CountsEntriesAcrossURLsAndDimensions(t *testing.T) {
	daemon, mr, logs := setupSkipTestDaemon(t)
	fresh := time.Now().UTC().Add(skipFreshIn)
	seedSkipEntry(t, daemon, skipURLA, skipDimRender, cache.SourceRender, fresh, fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLA, skipDimRender2, cache.SourceRender, fresh, fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLB, skipDimRender, cache.SourceRender, fresh, fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLC, skipDimRender, cache.SourceBypass, fresh, fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLC, skipDimRender2, cache.SourceRender, fresh, fasthttp.StatusOK)

	data := decodeSkipRecache(t, postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:           []string{skipURLA, skipURLB, skipURLC},
		DimensionIDs:   []int{skipDimRender, skipDimRender2},
		SkipIfFreshFor: skipWindow(0),
	}))

	assert.Equal(t, 4, data.EntriesSkipped)
	assert.Equal(t, 2, data.EntriesEnqueued)
	assert.True(t, data.SkipApplied)
	assert.Equal(t, 3, data.URLsCount)
	assert.Equal(t, 2, data.DimensionIDsCount)
	assert.ElementsMatch(t, []types.RecacheMember{
		skipMember(t, daemon, skipURLB, skipDimRender2, ""),
		skipMember(t, daemon, skipURLC, skipDimRender, ""),
	}, queuedSkipMembers(t, daemon, mr))

	summary := logs.FilterMessage(skipMsgRecacheSummary).All()
	require.Len(t, summary, 1)
	assert.Equal(t, int64(4), summary[0].ContextMap()[skipFieldSkipped])
}

func TestRecacheSkip_DuplicateAndInvalidURLs(t *testing.T) {
	daemon, mr, _ := setupSkipTestDaemon(t)
	seedSkipEntry(t, daemon, skipURLA, skipDimRender, cache.SourceRender, time.Now().UTC().Add(skipFreshIn), fasthttp.StatusOK)

	data := decodeSkipRecache(t, postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:           []string{skipURLA, skipURLA, skipInvalidURL, skipURLB, skipURLB},
		DimensionIDs:   []int{skipDimRender},
		SkipIfFreshFor: skipWindow(0),
	}))

	assert.Equal(t, 2, data.EntriesSkipped, "a cached URL sent twice is counted twice")
	assert.Equal(t, 2, data.EntriesEnqueued, "an uncached URL sent twice is counted twice")
	assert.Equal(t, 5, data.URLsCount)
	assert.Equal(t, []types.RecacheMember{skipMember(t, daemon, skipURLB, skipDimRender, "")}, queuedSkipMembers(t, daemon, mr),
		"ZADD dedups the repeated member")
}

func TestRecacheSkip_PausedHost(t *testing.T) {
	daemon, mr, _ := setupSkipTestDaemon(t)
	_, err := daemon.PauseHost(context.Background(), skipHostID)
	require.NoError(t, err)
	seedSkipEntry(t, daemon, skipURL, skipDimRender, cache.SourceRender, time.Now().UTC().Add(skipFreshIn), fasthttp.StatusOK)

	data := decodeSkipRecache(t, postSkipRecache(t, daemon, types.RecacheAPIRequest{
		URLs:           []string{skipURL},
		DimensionIDs:   []int{skipDimRender, skipDimRender2},
		SkipIfFreshFor: skipWindow(0),
	}))

	assert.True(t, data.Paused)
	assert.True(t, data.SkipApplied)
	assert.Equal(t, 1, data.EntriesSkipped)
	assert.Equal(t, 1, data.EntriesEnqueued)
	assert.Equal(t, []types.RecacheMember{skipMember(t, daemon, skipURL, skipDimRender2, "")}, queuedSkipMembers(t, daemon, mr))
}

// isFreshEnough takes now as a parameter, so the one-second boundary is exact here.
func TestRecacheSkip_IsFreshEnoughBoundary(t *testing.T) {
	daemon, _, _ := setupSkipTestDaemon(t)
	ctx := context.Background()
	renderDim := dimensionsByID(daemon.GetHost(skipHostID), []int{skipDimRender})[0]
	require.NotNil(t, renderDim)

	now := time.Now().UTC().Truncate(time.Second)
	window := time.Duration(skipWindow2h) * time.Second
	seedSkipEntry(t, daemon, skipURLA, skipDimRender, cache.SourceRender, now.Add(window), fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLB, skipDimRender, cache.SourceRender, now.Add(window+time.Second), fasthttp.StatusOK)
	seedSkipEntry(t, daemon, skipURLC, skipDimRender, cache.SourceRender, now, fasthttp.StatusOK)
	_, hashA := skipNormalize(t, daemon, skipURLA)
	_, hashB := skipNormalize(t, daemon, skipURLB)
	_, hashC := skipNormalize(t, daemon, skipURLC)

	assert.False(t, daemon.isFreshEnough(ctx, skipHostID, renderDim, hashA, "", skipWindow2h, now.Unix()), "expires exactly at the window")
	assert.True(t, daemon.isFreshEnough(ctx, skipHostID, renderDim, hashB, "", skipWindow2h, now.Unix()), "outlives the window by one second")
	assert.False(t, daemon.isFreshEnough(ctx, skipHostID, renderDim, hashB, "", math.MaxInt64, now.Unix()), "max window must not overflow into a skip")
	assert.False(t, daemon.isFreshEnough(ctx, skipHostID, renderDim, hashC, "", 0, now.Unix()), "expires at now is not fresh")
	assert.False(t, daemon.isFreshEnough(ctx, skipHostID, nil, hashB, "", 0, now.Unix()), "unknown dimension is not cached")
}

func TestRecacheSkip_DimensionsByID(t *testing.T) {
	daemon, _, _ := setupSkipTestDaemon(t)
	const unknownDimID = 99

	dims := dimensionsByID(daemon.GetHost(skipHostID), []int{skipDimRender2, skipDimBypass, unknownDimID, skipDimStatus})

	require.Len(t, dims, 4)
	require.NotNil(t, dims[0])
	require.NotNil(t, dims[1])
	require.NotNil(t, dims[3])
	assert.Equal(t, skipDimRender2, dims[0].ID)
	assert.Equal(t, skipDimBypass, dims[1].ID)
	assert.Equal(t, types.ActionBypass, dims[1].Action)
	assert.Nil(t, dims[2])
	assert.Equal(t, types.ActionStatus404, dims[3].Action)
}

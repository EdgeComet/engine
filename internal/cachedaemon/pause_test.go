package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/internal/cachedaemon/metrics"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	pauseExpiredMessage = "Recache pause expired, host resumes draining"
	unusablePauseField  = "Removing unusable recache pause field"
)

// seedPauseField writes a pause value straight into the hash, bypassing PauseHost so a
// test can install an expiry that has already passed. miniredis FastForward cannot do
// this: it moves miniredis's own key-TTL clock, while the expiry lives in the stored
// value and is compared against Go's time.Now().
func seedPauseField(t *testing.T, daemon *CacheDaemon, hostID int, expiresAt int64) {
	t.Helper()
	require.NoError(t, daemon.redis.HSet(context.Background(), daemon.keyGenerator.RecachePausedKey(),
		strconv.Itoa(hostID), strconv.FormatInt(expiresAt, 10)))
}

func pauseFields(t *testing.T, daemon *CacheDaemon) map[string]string {
	t.Helper()
	fields, err := daemon.redis.HGetAll(context.Background(), daemon.keyGenerator.RecachePausedKey())
	require.NoError(t, err)
	return fields
}

func TestPauseHostRoundTrip(t *testing.T) {
	const hostID = 1
	ctx := context.Background()

	t.Run("pause is visible to the scheduler and lifts on resume", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		expiresAt, err := daemon.PauseHost(ctx, hostID)
		require.NoError(t, err)
		assert.InDelta(t, time.Now().UTC().Add(pauseTTL).Unix(), expiresAt, 5,
			"the stored expiry is now plus the fixed pause window")

		paused, err := daemon.PausedHosts(ctx, time.Now().UTC().Unix())
		require.NoError(t, err)
		assert.Equal(t, map[int]int64{hostID: expiresAt}, paused)

		require.NoError(t, daemon.ResumeHost(ctx, hostID))

		paused, err = daemon.PausedHosts(ctx, time.Now().UTC().Unix())
		require.NoError(t, err)
		assert.Empty(t, paused)
	})

	t.Run("resuming a host that is not paused changes nothing", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		require.NoError(t, daemon.ResumeHost(ctx, hostID))
		assert.Empty(t, pauseFields(t, daemon))
	})

	t.Run("a repeat pause extends the window", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		shortExpiry := time.Now().UTC().Add(time.Minute).Unix()
		seedPauseField(t, daemon, hostID, shortExpiry)

		expiresAt, err := daemon.PauseHost(ctx, hostID)
		require.NoError(t, err)
		assert.Greater(t, expiresAt, shortExpiry, "pausing again must push the expiry out, not no-op")
		assert.Equal(t, strconv.FormatInt(expiresAt, 10), pauseFields(t, daemon)[strconv.Itoa(hostID)])
	})

	t.Run("PauseExpiry reports zero for an expired field", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		nowUnix := time.Now().UTC().Unix()
		seedPauseField(t, daemon, hostID, nowUnix-1)

		expiresAt, err := daemon.PauseExpiry(ctx, hostID, nowUnix)
		require.NoError(t, err)
		assert.Zero(t, expiresAt)
	})
}

func TestPausedHostsSweep(t *testing.T) {
	ctx := context.Background()

	t.Run("expired fields are removed and live ones survive", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		nowUnix := time.Now().UTC().Unix()
		liveExpiry := nowUnix + 600
		seedPauseField(t, daemon, 1, liveExpiry)
		seedPauseField(t, daemon, 2, nowUnix-1)

		paused, err := daemon.PausedHosts(ctx, nowUnix)
		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: liveExpiry}, paused)
		assert.Equal(t, map[string]string{"1": strconv.FormatInt(liveExpiry, 10)}, pauseFields(t, daemon))
	})

	t.Run("expired field for a host that is no longer configured is removed", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		nowUnix := time.Now().UTC().Unix()
		const movedHostID = 777
		require.Nil(t, daemon.GetHost(movedHostID), "the fixture must not configure this host")
		seedPauseField(t, daemon, movedHostID, nowUnix-1)

		paused, err := daemon.PausedHosts(ctx, nowUnix)
		require.NoError(t, err)
		assert.Empty(t, paused)
		assert.Empty(t, pauseFields(t, daemon), "a host that moved clusters must not leak a field here")
	})

	t.Run("fields that cannot be parsed are removed and logged", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		core, logs := observer.New(zap.WarnLevel)
		daemon.logger = zap.New(core)

		key := daemon.keyGenerator.RecachePausedKey()
		require.NoError(t, daemon.redis.HSet(ctx, key, "not-a-host", "12345"))
		require.NoError(t, daemon.redis.HSet(ctx, key, "3", "not-a-timestamp"))

		paused, err := daemon.PausedHosts(ctx, time.Now().UTC().Unix())
		require.NoError(t, err)
		assert.Empty(t, paused)
		assert.Empty(t, pauseFields(t, daemon), "an uninterpretable field can neither gate nor expire")
		assert.Equal(t, 2, logs.FilterMessage(unusablePauseField).Len())
	})
}

// TestSweepPauseFieldValueGuard covers the race the compare-and-delete exists for: a
// daemon reads the hash, sees an expired field, and an operator re-pauses the host
// before the daemon gets to remove it. A bare HDEL would cancel the fresh pause.
func TestSweepPauseFieldValueGuard(t *testing.T) {
	const hostID = 1
	ctx := context.Background()

	t.Run("a field rewritten after the read survives the sweep", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		nowUnix := time.Now().UTC().Unix()
		observedValue := strconv.FormatInt(nowUnix-1, 10)
		seedPauseField(t, daemon, hostID, nowUnix-1)

		freshExpiry, err := daemon.PauseHost(ctx, hostID)
		require.NoError(t, err)

		removed := daemon.sweepPauseField(ctx, daemon.keyGenerator.RecachePausedKey(),
			strconv.Itoa(hostID), observedValue)
		assert.False(t, removed)

		paused, err := daemon.PausedHosts(ctx, nowUnix)
		require.NoError(t, err)
		assert.Equal(t, map[int]int64{hostID: freshExpiry}, paused, "the new pause must still gate the host")
	})

	t.Run("a field still holding the observed value is removed", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		nowUnix := time.Now().UTC().Unix()
		seedPauseField(t, daemon, hostID, nowUnix-1)

		removed := daemon.sweepPauseField(ctx, daemon.keyGenerator.RecachePausedKey(),
			strconv.Itoa(hostID), strconv.FormatInt(nowUnix-1, 10))
		assert.True(t, removed)
		assert.Empty(t, pauseFields(t, daemon))
	})
}

// TestSchedulerPauseGate drives a full tick: a paused host must not move anything out of
// its durable queue, while every other host in the same tick keeps draining.
func TestSchedulerPauseGate(t *testing.T) {
	const pausedHost, runningHost = 1, 2
	const dimID = 1
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: pausedHost, domain: "paused.test", maxConcurrent: 5, dimensionID: dimID},
		{id: runningHost, domain: "running.test", maxConcurrent: 5, dimensionID: dimID},
	})

	pausedURLs := make([]string, 20)
	runningURLs := make([]string, 20)
	for i := range pausedURLs {
		pausedURLs[i] = fmt.Sprintf("https://paused.test/p%d", i)
		runningURLs[i] = fmt.Sprintf("https://running.test/p%d", i)
	}
	env.enqueueZSet(t, pausedHost, redis.PriorityHigh, dimID, pausedURLs, 0)
	env.enqueueZSet(t, runningHost, redis.PriorityHigh, dimID, runningURLs, 0)

	ctx := context.Background()
	_, err := env.daemon.PauseHost(ctx, pausedHost)
	require.NoError(t, err)

	env.daemon.runOneTick(ctx, 1)

	assert.Equal(t, 0, env.dispatchedFor(pausedHost))
	assert.Equal(t, int64(20), env.zcard(t, pausedHost, redis.PriorityHigh),
		"a paused host's work stays durable in Redis")
	assert.Equal(t, 20, env.dispatchedFor(runningHost))
	assert.Equal(t, int64(0), env.zcard(t, runningHost, redis.PriorityHigh))

	require.NoError(t, env.daemon.ResumeHost(ctx, pausedHost))
	env.daemon.runOneTick(ctx, 2)

	assert.Equal(t, 20, env.dispatchedFor(pausedHost), "resume restores draining on the next tick")
	assert.Equal(t, int64(0), env.zcard(t, pausedHost, redis.PriorityHigh))
}

// TestPauseLeavesInternalQueueDraining pins the deliberate asymmetry of the single pull
// gate: entries already past it finish. If a dispatch gate were ever added that merely
// re-enqueued them, they would squat the internal queue -- which is shared by every host
// -- for the whole pause.
func TestPauseLeavesInternalQueueDraining(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, []string{"Q0", "Q1", "Q2"}, 0)
	for i := 0; i < 3; i++ {
		require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
			HostID:      hostID,
			URL:         fmt.Sprintf("https://h.test/already-pulled-%d", i),
			DimensionID: dimID,
			Priority:    redis.PriorityHigh,
			QueuedAt:    time.Now().UTC(),
		}))
	}

	ctx := context.Background()
	_, err := env.daemon.PauseHost(ctx, hostID)
	require.NoError(t, err)

	env.daemon.runOneTick(ctx, 1)

	assert.Equal(t, 3, env.totalDispatched(), "entries already in the internal queue still dispatch")
	assert.Equal(t, 0, env.daemon.internalQueue.Size(), "the internal queue drains to zero, it does not squat")
	assert.Equal(t, int64(3), env.zcard(t, hostID, redis.PriorityHigh), "nothing new was pulled")
}

// TestExpiredPauseResumesOnNextTick seeds an expiry that has already passed, which is the
// only way to exercise the in-Go comparison: the value carries the deadline, so advancing
// miniredis's clock would not reach it.
func TestExpiredPauseResumesOnNextTick(t *testing.T) {
	const hostID = 1
	const dimID = 1
	env := newSchedulerEnv(t, 200, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	core, logs := observer.New(zap.InfoLevel)
	env.daemon.logger = zap.New(core)

	env.enqueueZSet(t, hostID, redis.PriorityHigh, dimID, []string{"E0", "E1", "E2"}, 0)

	nowUnix := time.Now().UTC().Unix()
	key := env.daemon.keyGenerator.RecachePausedKey()
	require.NoError(t, env.daemon.redis.HSet(context.Background(), key,
		strconv.Itoa(hostID), strconv.FormatInt(nowUnix-1, 10)))

	env.daemon.runOneTick(context.Background(), 1)

	assert.Equal(t, 3, env.dispatchedFor(hostID), "an expired pause must not gate the pull")
	assert.Equal(t, int64(0), env.zcard(t, hostID, redis.PriorityHigh))

	fields, err := env.daemon.redis.HGetAll(context.Background(), key)
	require.NoError(t, err)
	assert.Empty(t, fields, "the expired field is swept by the tick that observed it")

	expiredLogs := logs.FilterMessage(pauseExpiredMessage)
	require.Equal(t, 1, expiredLogs.Len())
	assert.Equal(t, int64(hostID), expiredLogs.All()[0].ContextMap()["host_id"])
}

func TestHandleRecachePauseAPI(t *testing.T) {
	const hostID = 1

	pauseDataFromResponse := func(t *testing.T, ctx *fasthttp.RequestCtx) types.RecachePauseAPIData {
		t.Helper()
		var resp struct {
			Data types.RecachePauseAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		return resp.Data
	}

	pauseBody := func(hostID int) []byte {
		body, _ := json.Marshal(types.RecachePauseAPIRequest{HostID: hostID})
		return body
	}

	t.Run("pause reports the window it opened", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makePostRequest(daemon, "/internal/cache/recache/pause", pauseBody(hostID))

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		data := pauseDataFromResponse(t, ctx)
		assert.True(t, data.Paused)
		assert.InDelta(t, time.Now().UTC().Add(pauseTTL).Unix(), data.ExpiresAt, 5)
	})

	t.Run("a repeat pause returns a later expiry", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		shortExpiry := time.Now().UTC().Add(time.Minute).Unix()
		seedPauseField(t, daemon, hostID, shortExpiry)

		ctx := makePostRequest(daemon, "/internal/cache/recache/pause", pauseBody(hostID))

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Greater(t, pauseDataFromResponse(t, ctx).ExpiresAt, shortExpiry)
	})

	t.Run("resume clears the pause and omits an expiry", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		makePostRequest(daemon, "/internal/cache/recache/pause", pauseBody(hostID))

		ctx := makePostRequest(daemon, "/internal/cache/recache/resume", pauseBody(hostID))

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
		assert.Equal(t, types.RecachePauseAPIData{Paused: false}, pauseDataFromResponse(t, ctx))
		assert.NotContains(t, string(ctx.Response.Body()), "expires_at")
		assert.Empty(t, pauseFields(t, daemon))
	})

	t.Run("resuming twice succeeds", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		first := makePostRequest(daemon, "/internal/cache/recache/resume", pauseBody(hostID))
		second := makePostRequest(daemon, "/internal/cache/recache/resume", pauseBody(hostID))

		assert.Equal(t, fasthttp.StatusOK, first.Response.StatusCode())
		assert.Equal(t, fasthttp.StatusOK, second.Response.StatusCode())
	})

	t.Run("unknown host returns 400 on both endpoints", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		pauseCtx := makePostRequest(daemon, "/internal/cache/recache/pause", pauseBody(999))
		resumeCtx := makePostRequest(daemon, "/internal/cache/recache/resume", pauseBody(999))

		assert.Equal(t, fasthttp.StatusBadRequest, pauseCtx.Response.StatusCode())
		assert.Equal(t, fasthttp.StatusBadRequest, resumeCtx.Response.StatusCode())
	})

	t.Run("missing host_id returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makePostRequest(daemon, "/internal/cache/recache/pause", []byte(`{}`))

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)

		ctx := makePostRequest(daemon, "/internal/cache/recache/pause", []byte(`{"host_id":`))

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})
}

func TestPauseStateOnReadSurfaces(t *testing.T) {
	const hostID = 1

	t.Run("enqueue succeeds while paused and says so", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		_, err := daemon.PauseHost(context.Background(), hostID)
		require.NoError(t, err)

		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:       hostID,
			URLs:         []string{"https://example.com/page"},
			DimensionIDs: []int{1},
			Priority:     redis.PriorityHigh,
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		var resp struct {
			Data types.RecacheAPIData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		assert.Equal(t, 1, resp.Data.EntriesEnqueued, "a pause must not cost the operator the work list")
		assert.True(t, resp.Data.Paused)
		assert.Equal(t, int64(1), purgeQueueDepth(t, daemon, hostID, redis.PriorityHigh))
	})

	t.Run("queue summary carries the pause and its deadline", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		expiresAt, err := daemon.PauseHost(context.Background(), hostID)
		require.NoError(t, err)

		ctx := makeTestRequest(daemon, "GET", fmt.Sprintf("/internal/cache/queue/summary?host_id=%d", hostID))

		assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		var resp struct {
			Data QueueSummaryResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(ctx.Response.Body(), &resp))
		assert.True(t, resp.Data.Paused)
		assert.Equal(t, expiresAt, resp.Data.PausedUntil)
	})

	t.Run("status reports paused per host", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		_, err := daemon.PauseHost(context.Background(), hostID)
		require.NoError(t, err)

		queues := daemon.GetQueuesStatus()

		assert.True(t, queues[hostID].Paused)
		assert.False(t, queues[2].Paused, "the pause is scoped to one host")
	})
}

// TestPublishPauseMetricsFollowsHostsOutOfConfig covers the gauge for a host that is
// cluster-moved away while paused. Publishing only for the configured hosts would leave
// its series stuck at 1 until the process restarts, which reads as a pause nobody can
// find or clear.
func TestPublishPauseMetricsFollowsHostsOutOfConfig(t *testing.T) {
	const movedHostID = 777
	configuredHosts := []int{1, 2}

	scrape := func(t *testing.T, daemon *CacheDaemon) string {
		t.Helper()
		ctx := &fasthttp.RequestCtx{}
		daemon.metricsCollector.ServeHTTP(ctx)
		return string(ctx.Response.Body())
	}

	daemon, _ := setupTestDaemon(t)
	daemon.metricsCollector = metrics.NewMetricsCollector("edgecomet", zap.NewNop())

	daemon.publishPauseMetrics(configuredHosts, map[int]int64{movedHostID: time.Now().UTC().Unix() + 600})
	assert.Contains(t, scrape(t, daemon), `recache_paused{host_id="777"} 1`)

	// The host has since moved clusters and its pause field has been swept, so it is in
	// neither the configured set nor the paused set.
	daemon.publishPauseMetrics(configuredHosts, nil)
	assert.Contains(t, scrape(t, daemon), `recache_paused{host_id="777"} 0`)

	daemon.publishPauseMetrics(configuredHosts, nil)
	assert.NotContains(t, daemon.pausedMetricHosts, movedHostID,
		"a host reported as resumed does not need reporting again")
}

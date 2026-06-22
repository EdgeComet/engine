package cachedaemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/pkg/types"
)

func TestHandleRecacheAPI_Mode(t *testing.T) {
	t.Run("invalid mode returns 400", func(t *testing.T) {
		daemon, _ := setupTestDaemon(t)
		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:   1,
			URLs:     []string{"https://example.com/page"},
			Priority: "normal",
			Mode:     "warp",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)
		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	for _, mode := range []string{types.RecacheModeRender, types.RecacheModeBypass} {
		mode := mode
		t.Run("mode "+mode+" stored on queue member", func(t *testing.T) {
			daemon, mr := setupTestDaemon(t)
			body, _ := json.Marshal(types.RecacheAPIRequest{
				HostID:       1,
				URLs:         []string{"https://example.com/page"},
				DimensionIDs: []int{0},
				Priority:     "high",
				Mode:         mode,
			})
			ctx := makePostRequest(daemon, "/internal/cache/recache", body)
			require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

			queueKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
			members, err := mr.ZMembers(queueKey)
			require.NoError(t, err)
			require.Len(t, members, 1)

			var member types.RecacheMember
			require.NoError(t, json.Unmarshal([]byte(members[0]), &member))
			assert.Equal(t, mode, member.Mode)
		})
	}

	t.Run("empty mode omitted from member json", func(t *testing.T) {
		daemon, mr := setupTestDaemon(t)
		body, _ := json.Marshal(types.RecacheAPIRequest{
			HostID:       1,
			URLs:         []string{"https://example.com/page"},
			DimensionIDs: []int{0},
			Priority:     "high",
		})
		ctx := makePostRequest(daemon, "/internal/cache/recache", body)
		require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

		queueKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
		members, err := mr.ZMembers(queueKey)
		require.NoError(t, err)
		require.Len(t, members, 1)
		// omitempty keeps no-mode members byte-identical to autorecache / pre-feature
		// members, so dedup and the url-status exact-match lookup still work.
		assert.NotContains(t, members[0], "mode")
	})
}

func TestActionForEntry_ModeOverride(t *testing.T) {
	daemon, _ := setupTestDaemon(t)
	daemon.reloadMu.RLock()
	defer daemon.reloadMu.RUnlock()

	// Host 1 dims: bypass(0, bypass action), mobile(1, render default), desktop(2, render).
	cases := []struct {
		name string
		dim  int
		mode string
		want types.URLRuleAction
	}{
		{"bypass dim, no override -> bypass", 0, "", types.ActionBypass},
		{"bypass dim, render override -> render", 0, types.RecacheModeRender, types.ActionRender},
		{"render dim, no override -> render", 1, "", types.ActionRender},
		{"render dim, bypass override -> bypass", 1, types.RecacheModeBypass, types.ActionBypass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := daemon.actionForEntry(InternalQueueEntry{HostID: 1, DimensionID: tc.dim, Mode: tc.mode})
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("missing host still discarded despite override", func(t *testing.T) {
		got := daemon.actionForEntry(InternalQueueEntry{HostID: 999, DimensionID: 0, Mode: types.RecacheModeRender})
		assert.Equal(t, types.URLRuleAction(""), got)
	})
}

func TestHandleURLStatusAPI_ModeOverrideMember(t *testing.T) {
	daemon, mr := setupTestDaemon(t)
	now := time.Now().UTC().Unix()

	normalizedResult, err := daemon.normalizer.Normalize("https://example.com/queued-render", nil)
	require.NoError(t, err)

	member := types.RecacheMember{
		URL:         normalizedResult.NormalizedURL,
		DimensionID: 0,
		Mode:        types.RecacheModeRender,
	}
	memberJSON, _ := json.Marshal(member)
	queueKey := daemon.keyGenerator.RecacheQueueKey(1, "high")
	mr.ZAdd(queueKey, float64(now-5), string(memberJSON))

	ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/queued-render")
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

	result := parseURLStatusResponse(t, ctx)
	assert.True(t, result.Queue.Pending, "url-status must find a queue member tagged with a mode override")
	require.NotNil(t, result.Queue.Priority)
	assert.Equal(t, "high", *result.Queue.Priority)
}

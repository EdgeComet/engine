package cachedaemon

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/pkg/types"
)

// These tests cover the canonical-URL consistency fix: recache enqueue and invalidate must
// strip tracking params (host-resolved) so the queue and invalidation key off the same
// canonical URL the cache is written/read under.

func TestRecacheCanonicalizesQueueMember(t *testing.T) {
	daemon, mr := setupTestDaemon(t)

	clean, err := daemon.normalizer.Normalize("https://example.com/page", nil)
	require.NoError(t, err)
	canonical := clean.NormalizedURL

	body, _ := json.Marshal(types.RecacheAPIRequest{
		HostID:       1,
		URLs:         []string{"https://example.com/page?utm_source=newsletter"},
		DimensionIDs: []int{1},
		Priority:     "normal",
	})
	ctx := makePostRequest(daemon, "/internal/cache/recache", body)
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

	queueKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
	members, err := mr.ZMembers(queueKey)
	require.NoError(t, err)
	require.Len(t, members, 1)

	var member types.RecacheMember
	require.NoError(t, json.Unmarshal([]byte(members[0]), &member))
	assert.Equal(t, canonical, member.URL, "queue member must be the canonical (stripped) URL")
}

func TestRecacheDedupsParamVariants(t *testing.T) {
	daemon, mr := setupTestDaemon(t)

	body, _ := json.Marshal(types.RecacheAPIRequest{
		HostID: 1,
		URLs: []string{
			"https://example.com/page?utm_source=a",
			"https://example.com/page?utm_source=b",
		},
		DimensionIDs: []int{1},
		Priority:     "normal",
	})
	ctx := makePostRequest(daemon, "/internal/cache/recache", body)
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

	queueKey := daemon.keyGenerator.RecacheQueueKey(1, "normal")
	members, err := mr.ZMembers(queueKey)
	require.NoError(t, err)
	assert.Len(t, members, 1, "param variants of the same page collapse to one queue member")
}

func TestRecacheThenURLStatusMatches(t *testing.T) {
	daemon, _ := setupTestDaemon(t)

	body, _ := json.Marshal(types.RecacheAPIRequest{
		HostID:       1,
		URLs:         []string{"https://example.com/page?utm_source=x"},
		DimensionIDs: []int{1},
		Priority:     "normal",
	})
	require.Equal(t, fasthttp.StatusOK, makePostRequest(daemon, "/internal/cache/recache", body).Response.StatusCode())

	// Poll url-status with the same tracking-laden URL; it must resolve to the queued member.
	ctx := makeTestRequest(daemon, "GET", "/internal/cache/url-status?host_id=1&url=https://example.com/page%3Futm_source%3Dx")
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

	result := parseURLStatusResponse(t, ctx)
	assert.True(t, result.Queue.Pending, "queue lookup must match the canonicalized enqueued URL")
	require.NotNil(t, result.Queue.Priority)
	assert.Equal(t, "normal", *result.Queue.Priority)
}

func TestInvalidateCanonicalizesLookup(t *testing.T) {
	daemon, mr := setupTestDaemon(t)

	// Cache entry is stored under the canonical (stripped) key.
	clean, err := daemon.normalizer.Normalize("https://example.com/page", nil)
	require.NoError(t, err)
	urlHash := daemon.normalizer.Hash(clean.NormalizedURL)
	populateMetadataHash(mr, 1, 1, urlHash, map[string]string{
		"url": clean.NormalizedURL, "dimension": "mobile",
		"size": "100", "created_at": "1000000", "expires_at": "9999999999", "status_code": "200",
	})

	// Invalidate using a tracking-laden URL; it must delete the canonical entry.
	body, _ := json.Marshal(types.InvalidateAPIRequest{
		HostID:       1,
		URLs:         []string{"https://example.com/page?utm_source=x"},
		DimensionIDs: []int{1},
	})
	ctx := makePostRequest(daemon, "/internal/cache/invalidate", body)
	require.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())

	var apiResp httputil.APIResponse
	require.NoError(t, json.Unmarshal(ctx.Response.Body(), &apiResp))
	dataBytes, _ := json.Marshal(apiResp.Data)
	var data types.InvalidateAPIData
	require.NoError(t, json.Unmarshal(dataBytes, &data))
	assert.Equal(t, 1, data.EntriesInvalidated, "invalidate must hit the canonical key")

	metadataKey := fmt.Sprintf("meta:cache:1:1:%d", urlHash)
	assert.False(t, mr.Exists(metadataKey), "canonical metadata entry should be deleted")
}

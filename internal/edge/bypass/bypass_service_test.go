package bypass

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

// TestFetchContentSetsLoopPreventionHeaders verifies that bypass fetches carry
// X-Edge-Render (so the integration routes them straight to origin instead of
// looping back into the Edge Gateway) and X-Render-Key, and that X-Edge-Render
// cannot be overridden by forwarded client headers.
func TestFetchContentSetsLoopPreventionHeaders(t *testing.T) {
	var got http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer origin.Close()

	ssrfOff := false
	svc := NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	clientHeaders := map[string][]string{
		types.HeaderEdgeRender: {"spoofed"}, // must be overridden by engine value
	}

	resp, err := svc.FetchContent(origin.URL, clientHeaders, "render-key-123", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, edgeRenderSource, got.Get(types.HeaderEdgeRender),
		"bypass fetch must set X-Edge-Render to the engine value, not the forwarded one")
	assert.Equal(t, "render-key-123", got.Get(types.HeaderRenderKey))
}

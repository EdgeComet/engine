package recache

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

// TestHandleRecache_SurvivesServerShutdown covers the edge gateway draining its internal server
// while a recache is in flight. fasthttp closes the Done channel of every in-flight RequestCtx as
// soon as Server.Shutdown starts, so a recache that used the request as its context lost its
// render and its cache write at the start of the drain instead of finishing within it. The test
// runs the real handler in a real server, holds the recache in the pre-render hook (the first step
// that receives the handler's context), starts the shutdown, then lets the recache finish.
func TestHandleRecache_SurvivesServerShutdown(t *testing.T) {
	rs, _ := successPathService(t)
	ttl := types.Duration(testCacheableTTL)
	rs.configManager = &mockEGConfigManager{hosts: []types.Host{{
		ID:      1,
		Domain:  "example.com",
		Domains: []string{"example.com"},
		Render: types.RenderConfig{
			Timeout: types.Duration(30 * time.Second),
			Cache:   &types.RenderCacheConfig{TTL: &ttl, StatusCodes: []int{http.StatusOK, http.StatusGone}},
		},
		Dimensions: map[string]types.Dimension{"desktop": {ID: 0}},
	}}}

	hookEntered := make(chan struct{})
	release := make(chan struct{})
	hookCtxErr := make(chan error, 1)
	rs.SetPreRenderHook(func(ctx context.Context, _ *edgectx.RenderContext) (*orchestrator.PreRenderDecision, error) {
		close(hookEntered)
		<-release
		hookCtxErr <- ctx.Err()
		return &orchestrator.PreRenderDecision{Handled: true, StatusCode: http.StatusGone}, nil
	})

	srv := &fasthttp.Server{Handler: rs.handleRecache}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	addr := ln.Addr().String()

	body, err := json.Marshal(RecacheRequest{URL: "https://example.com/page", HostID: 1, DimensionID: 0})
	require.NoError(t, err)
	status := make(chan int, 1)
	go func() {
		req, resp := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		defer fasthttp.ReleaseResponse(resp)
		req.SetRequestURI("http://" + addr + "/recache")
		req.Header.SetMethod(fasthttp.MethodPost)
		req.SetBody(body)
		if err := (&fasthttp.HostClient{Addr: addr}).Do(req, resp); err != nil {
			status <- 0
			return
		}
		status <- resp.StatusCode()
	}()
	<-hookEntered

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.ShutdownWithContext(shutdownCtx) }()

	// fasthttp closes the request contexts before it closes the listener, so a refused dial means
	// the cancellation this test guards against has already happened
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, 2*time.Second, 10*time.Millisecond, "server shutdown never started")
	close(release)

	assert.NoError(t, <-hookCtxErr, "the server shutdown must not cancel an in-flight recache")
	assert.Equal(t, fasthttp.StatusOK, <-status, "the recache must complete during the drain")
	require.NoError(t, <-shutdownDone)
}

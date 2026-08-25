package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/edgectx"
)

func hookContext() *edgectx.RenderContext {
	return &edgectx.RenderContext{
		Logger:    zap.NewNop(),
		TargetURL: "https://example.com/page",
	}
}

// The hook is an optimisation, so every way it can fail has to leave the render running. A
// regression here does not surface as an error: it silently serves a status nobody asked for.
func TestRunPreRenderHook_DeclinesRenderNormally(t *testing.T) {
	cases := []struct {
		name     string
		hook     PreRenderHook
		expected string
	}{
		{
			name: "nil hook",
			hook: nil,
		},
		{
			name: "nil decision",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return nil, nil
			},
		},
		{
			name: "decision not handled",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{StatusCode: 404}, nil
			},
		},
		{
			name: "hook error",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return nil, errors.New("origin unreachable")
			},
		},
		{
			name: "error alongside a handled decision",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: 404}, errors.New("partial failure")
			},
		},
		{
			name: "zero status code",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true}, nil
			},
		},
		{
			// The dangerous one: mapping an origin's "this URL is fine" to Handled instead of
			// declining would serve and cache an empty body for every good page on the host.
			name: "200, which would publish an empty page",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: 200}, nil
			},
		},
		{
			// Same accident as 200 to an indexing crawler: the whole 2xx class reads as success.
			name: "2xx other than 204",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: 201}, nil
			},
		},
		{
			// 1xx is never a final response; serving and caching one is protocol nonsense.
			name: "1xx informational",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: 100}, nil
			},
		},
		{
			name: "status code below the HTTP range",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: 0}, nil
			},
		},
		{
			// A panic must be as harmless as an error. Unrecovered it would unwind past the
			// caller's explicit lock release and strand the URL's render lock for its whole TTL.
			name: "panicking hook",
			hook: func(_ context.Context, rc *edgectx.RenderContext) (*PreRenderDecision, error) {
				// The exact production shape: a hook reading HTTPCtx, which the live path sets
				// and the recache path leaves nil.
				_ = rc.HTTPCtx.UserAgent()
				return nil, nil
			},
		},
		{
			name: "status code above the HTTP range",
			hook: func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
				return &PreRenderDecision{Handled: true, StatusCode: maxHTTPStatusCode + 1}, nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Nil(t, RunPreRenderHook(context.Background(), tc.hook, hookContext()))
		})
	}
}

func TestRunPreRenderHook_AppliesHandledDecision(t *testing.T) {
	hook := func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
		return &PreRenderDecision{Handled: true, StatusCode: 301, Location: "/casino"}, nil
	}

	decision := RunPreRenderHook(context.Background(), hook, hookContext())

	require.NotNil(t, decision)
	assert.Equal(t, 301, decision.StatusCode)
	assert.Equal(t, "/casino", decision.Location)
}

func TestRunPreRenderHook_AcceptsRangeBoundaries(t *testing.T) {
	// Only statuses where an empty body is meaningful: 204, and 300 upward.
	for _, code := range []int{statusCodeNoContent, minNonSuccessStatus, 301, 404, maxHTTPStatusCode} {
		hook := func(context.Context, *edgectx.RenderContext) (*PreRenderDecision, error) {
			return &PreRenderDecision{Handled: true, StatusCode: code}, nil
		}

		decision := RunPreRenderHook(context.Background(), hook, hookContext())

		require.NotNil(t, decision, "status %d should be accepted", code)
		assert.Equal(t, code, decision.StatusCode)
	}
}

// The hook holds the render lock while it runs, so it must be handed a bounded context rather
// than the caller's.
func TestRunPreRenderHook_BoundsTheHookContext(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool

	hook := func(ctx context.Context, _ *edgectx.RenderContext) (*PreRenderDecision, error) {
		deadline, hasDeadline = ctx.Deadline()
		return nil, nil
	}

	RunPreRenderHook(context.Background(), hook, hookContext())

	require.True(t, hasDeadline, "hook context must carry a deadline")
	assert.LessOrEqual(t, time.Until(deadline), preRenderHookTimeout)
}

// A caller that is already cancelled must not have its deadline extended by the hook's own bound.
func TestRunPreRenderHook_KeepsEarlierCallerDeadline(t *testing.T) {
	callerTimeout := preRenderHookTimeout / 2
	ctx, cancel := context.WithTimeout(context.Background(), callerTimeout)
	defer cancel()

	var deadline time.Time

	hook := func(hookCtx context.Context, _ *edgectx.RenderContext) (*PreRenderDecision, error) {
		deadline, _ = hookCtx.Deadline()
		return nil, nil
	}

	RunPreRenderHook(ctx, hook, hookContext())

	assert.LessOrEqual(t, time.Until(deadline), callerTimeout)
}

func TestRunPreRenderHook_ReceivesRenderContext(t *testing.T) {
	renderCtx := hookContext()
	var seen *edgectx.RenderContext

	hook := func(_ context.Context, rc *edgectx.RenderContext) (*PreRenderDecision, error) {
		seen = rc
		return nil, nil
	}

	RunPreRenderHook(context.Background(), hook, renderCtx)

	assert.Same(t, renderCtx, seen)
}

// The decision is served and cached through the existing override path, so it has to arrive in
// that shape intact - a dropped Location turns a redirect into a bare status.
func TestPreRenderDecision_AsProcessedContent(t *testing.T) {
	decision := &PreRenderDecision{Handled: true, StatusCode: 404, Location: "/en/404"}

	processed := decision.AsProcessedContent()

	require.NotNil(t, processed)
	require.NotNil(t, processed.Override)
	assert.Equal(t, 404, processed.Override.StatusCode)
	assert.Equal(t, "/en/404", processed.Override.Location)
	assert.Nil(t, processed.HTML)
	assert.Nil(t, processed.PageSEO)
}

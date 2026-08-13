package chrome

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

// fakeClock advances only when the loop pauses, so duration bounds are testable without waiting.
type fakeClock struct {
	now     time.Time
	perStep time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) sleep(context.Context, time.Duration) error {
	c.now = c.now.Add(c.perStep)
	return nil
}

// scrollCall records what the loop asked the page to do on one step.
type scrollCall struct {
	Mode   string
	StepPx int
}

// stepRecorder captures the mode and step size of every call, which is how the phase machine and
// the adaptive step size are asserted without a browser.
type stepRecorder struct {
	calls []scrollCall
	fn    func(call int, mode string) (scrollState, error)
}

func (r *stepRecorder) step(_ context.Context, mode string, stepPx int) (scrollState, error) {
	r.calls = append(r.calls, scrollCall{Mode: mode, StepPx: stepPx})
	return r.fn(len(r.calls), mode)
}

func (r *stepRecorder) modes() []string {
	modes := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		modes = append(modes, c.Mode)
	}
	return modes
}

func newTestLoop(step func(ctx context.Context, mode string, stepPx int) (scrollState, error), perStep time.Duration) (scrollLoop, *fakeClock) {
	clock := &fakeClock{now: time.Unix(0, 0), perStep: perStep}
	loop := scrollLoop{
		step:    step,
		pause:   clock.sleep,
		restore: func(context.Context) error { return nil },
		now:     clock.Now,
	}
	return loop, clock
}

func newRecordedLoop(fn func(call int, mode string) (scrollState, error), perStep time.Duration) (scrollLoop, *stepRecorder, *fakeClock) {
	recorder := &stepRecorder{fn: fn}
	loop, clock := newTestLoop(recorder.step, perStep)
	return loop, recorder, clock
}

func newTestInstance() *ChromeInstance {
	return &ChromeInstance{ID: 7, logger: zap.NewNop()}
}

func newScrollRequest(scroll bool) *types.RenderRequest {
	return &types.RenderRequest{RequestID: "scroll-test", URL: "https://example.com/page", Scroll: scroll}
}

// settledPage is a page that scrolls, sits at its bottom and has nothing more to give.
func settledPage() scrollState {
	return scrollState{
		AnyTarget:    true,
		PageFound:    true,
		PageTarget:   "body",
		PageTop:      1920,
		PageClient:   1080,
		PageHeight:   3000,
		PageAtBottom: true,
		Links:        42,
	}
}

func TestScrollLoopSettlesOnStablePage(t *testing.T) {
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return settledPage(), nil
	}, scrollStepPause)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.True(t, outcome.Performed)
	assert.True(t, outcome.ReachedBottom)
	assert.Equal(t, "body", outcome.PageTarget)
	assert.Equal(t, 3000, outcome.FinalHeight)
	assert.Equal(t, scrollStopSettled, outcome.StopReason)
	// One step establishes height and link count, then the settle rounds confirm them.
	assert.Equal(t, 1+scrollSettleRounds, outcome.Steps)
}

func TestScrollLoopKeepsGoingWhileLinksArrive(t *testing.T) {
	links := 0
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		links++
		state.Links = links
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	// At the bottom with a stable height on every step, but content is still arriving. Settling on
	// height alone would cut this page short.
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
	assert.Equal(t, scrollStopMaxSteps, outcome.StopReason)
}

func TestScrollLoopKeepsGoingWhileHeightGrows(t *testing.T) {
	height := 3000
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		height += 100
		state.PageHeight = height
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
}

func TestScrollLoopKeepsGoingWhileNotAtBottom(t *testing.T) {
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		state.PageAtBottom = false
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	// A stable height alone is not settled: the page still has viewport left to travel.
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
	assert.False(t, outcome.ReachedBottom)
}

func TestScrollLoopMovesToInnerContainerOnceThePageSettles(t *testing.T) {
	loop, recorder, _ := newRecordedLoop(func(_ int, mode string) (scrollState, error) {
		state := settledPage()
		state.InnerLeft = true
		if mode == scrollModeInner {
			state.InnerTarget = "DIV.panel"
		}
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	// The page is walked first, then the container gets the remainder of the budget.
	assert.Equal(t, []string{
		scrollModePage, scrollModePage, scrollModePage, scrollModePage,
		scrollModeInner, scrollModeInner, scrollModeInner, scrollModeInner,
	}, recorder.modes()[:8])
	assert.Equal(t, "DIV.panel", outcome.InnerTarget)
}

func TestScrollLoopBoundsTheInnerPhase(t *testing.T) {
	loop, _, _ := newRecordedLoop(func(_ int, mode string) (scrollState, error) {
		state := settledPage()
		// A virtualized list never reaches its bottom, so it must not hold the pass open.
		state.InnerLeft = true
		if mode == scrollModeInner {
			state.InnerTarget = "DIV.feed"
		}
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, scrollInnerMaxSteps, outcome.InnerSteps)
}

func TestScrollLoopReturnsToThePageWhenItGrows(t *testing.T) {
	height := 3000
	loop, recorder, _ := newRecordedLoop(func(_ int, mode string) (scrollState, error) {
		state := settledPage()
		state.InnerLeft = true
		state.PageHeight = height
		if mode == scrollModeInner {
			// The container revealed content and the page got taller with it.
			state.InnerTarget = "DIV.panel"
			height += 200
			state.PageHeight = height
		}
		return state, nil
	}, 0)

	_, err := loop.run(context.Background())

	require.NoError(t, err)
	// Page, then the container once settled, then back to the page because it grew.
	assert.Equal(t, scrollModeInner, recorder.calls[4].Mode)
	assert.Equal(t, scrollModePage, recorder.calls[5].Mode)
}

func TestScrollLoopGoesStraightToTheContainerWhenTheDocumentDoesNotScroll(t *testing.T) {
	loop, recorder, _ := newRecordedLoop(func(_ int, mode string) (scrollState, error) {
		// An app shell: the document itself does not scroll at all.
		state := scrollState{AnyTarget: true, InnerLeft: true}
		if mode == scrollModeInner {
			state.InnerTarget = "DIV.shell"
		}
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, scrollModeInner, recorder.calls[1].Mode)
	assert.Equal(t, "DIV.shell", outcome.InnerTarget)
	assert.Empty(t, outcome.PageTarget)
}

func TestScrollLoopEnlargesTheStepWhenTheDocumentOutgrowsTheBudget(t *testing.T) {
	loop, recorder, _ := newRecordedLoop(func(int, string) (scrollState, error) {
		state := settledPage()
		// Far more page left than one viewport per remaining step can cover.
		state.PageHeight = 500000
		state.PageAtBottom = false
		return state, nil
	}, 0)

	_, err := loop.run(context.Background())

	require.NoError(t, err)
	// The first step has no measurement to work from and travels one viewport.
	assert.Zero(t, recorder.calls[0].StepPx)
	assert.Greater(t, recorder.calls[1].StepPx, settledPage().PageClient)
}

func TestScrollLoopKeepsOneViewportStepsWhenThePageFits(t *testing.T) {
	loop, recorder, _ := newRecordedLoop(func(int, string) (scrollState, error) {
		state := settledPage()
		state.PageAtBottom = false
		return state, nil
	}, 0)

	_, err := loop.run(context.Background())

	require.NoError(t, err)
	for i, call := range recorder.calls {
		assert.Zerof(t, call.StepPx, "step %d should travel one viewport", i+1)
	}
}

func TestScrollLoopRetriesBeforeReportingNoTarget(t *testing.T) {
	loop, recorder, _ := newRecordedLoop(func(int, string) (scrollState, error) {
		return scrollState{AnyTarget: false}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.False(t, outcome.Performed)
	assert.True(t, outcome.NoTarget)
	assert.Equal(t, scrollStopNoTarget, outcome.StopReason)
	// An early start means the page has not laid out yet, so one empty answer is not a verdict.
	assert.Len(t, recorder.calls, scrollMaxEmptySteps)
}

func TestScrollLoopRecoversWhenThePageLaysOutLate(t *testing.T) {
	loop, _, _ := newRecordedLoop(func(call int, _ string) (scrollState, error) {
		if call <= 2 {
			return scrollState{AnyTarget: false}, nil
		}
		return settledPage(), nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.True(t, outcome.Performed)
	assert.False(t, outcome.NoTarget)
	assert.Equal(t, scrollStopSettled, outcome.StopReason)
}

func TestScrollLoopDoesNotReportNoTargetAfterScrolling(t *testing.T) {
	loop, _, _ := newRecordedLoop(func(call int, _ string) (scrollState, error) {
		if call > 1 {
			// The SPA replaced the container and there is nothing left to drive.
			return scrollState{AnyTarget: false}, nil
		}
		return settledPage(), nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.True(t, outcome.Performed)
	// The page did scroll, so this says nothing about the page having no scroller.
	assert.False(t, outcome.NoTarget)
}

func TestScrollLoopStopsOnMaxSteps(t *testing.T) {
	height := 1000
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		height += 100
		state.PageHeight = height
		return state, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
	assert.Equal(t, scrollStopMaxSteps, outcome.StopReason)
}

func TestScrollLoopStopsOnMaxDuration(t *testing.T) {
	height := 1000
	perStep := types.ScrollMaxDuration / 3
	loop, clock := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		height += 100
		state.PageHeight = height
		return state, nil
	}, perStep)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Less(t, outcome.Steps, scrollMaxSteps)
	assert.Equal(t, scrollStopDuration, outcome.StopReason)
	assert.False(t, clock.Now().Before(time.Unix(0, 0).Add(types.ScrollMaxDuration)))
}

func TestScrollLoopStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	loop, _, _ := newRecordedLoop(func(call int, _ string) (scrollState, error) {
		cancel()
		state := settledPage()
		state.PageHeight = 1000 + call
		return state, nil
	}, 0)

	outcome, err := loop.run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, outcome.Steps)
	assert.Equal(t, scrollStopCancelled, outcome.StopReason)
}

func TestScrollLoopReportsNothingOnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return settledPage(), nil
	}, 0)

	outcome, err := loop.run(ctx)

	require.NoError(t, err)
	assert.False(t, outcome.Performed)
	// The loop never asked the page anything, so it cannot claim the page has no scroller.
	assert.False(t, outcome.NoTarget)
}

func TestScrollLoopReturnsPartialOutcomeOnStepError(t *testing.T) {
	stepErr := errors.New("evaluate failed")
	loop, _, _ := newRecordedLoop(func(call int, _ string) (scrollState, error) {
		if call > 1 {
			return scrollState{}, stepErr
		}
		return settledPage(), nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.ErrorIs(t, err, stepErr)
	assert.True(t, outcome.Performed)
	assert.Equal(t, "body", outcome.PageTarget)
	assert.Equal(t, 1, outcome.Steps)
	assert.Equal(t, scrollStopError, outcome.StopReason)
}

func TestScrollIsSkippedWhenNotRequested(t *testing.T) {
	instance := newTestInstance()
	var metrics types.PageMetrics

	require.NoError(t, instance.scrollIfRequested(newScrollRequest(false), &metrics).Do(context.Background()))

	assert.False(t, metrics.ScrollPerformed)
	assert.Zero(t, metrics.ScrollSteps)
	assert.Zero(t, metrics.ScrollDuration)
	assert.Empty(t, metrics.ScrollTarget)
	assert.Empty(t, metrics.ScrollStopReason)
}

func TestScrollRecordsOutcomeOnMetrics(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		state.PageHeight = 4200
		return state, nil
	}, scrollStepPause)

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.True(t, metrics.ScrollPerformed)
	assert.True(t, metrics.ScrollReachedBottom)
	assert.Equal(t, "body", metrics.ScrollTarget)
	assert.Equal(t, 1+scrollSettleRounds, metrics.ScrollSteps)
	assert.Equal(t, 4200, metrics.ScrollFinalHeight)
	assert.Equal(t, scrollStopSettled, metrics.ScrollStopReason)
	assert.Positive(t, metrics.ScrollDuration)
}

func TestScrollRecordsBudgetExhaustionOnMetrics(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		state := settledPage()
		state.PageHeight = 500000
		state.PageAtBottom = false
		return state, nil
	}, 0)

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	// The page was scrolled, but anything anchored to its bottom is missing from the capture.
	assert.True(t, metrics.ScrollPerformed)
	assert.False(t, metrics.ScrollReachedBottom)
	assert.Equal(t, scrollStopMaxSteps, metrics.ScrollStopReason)
}

func TestScrollStepErrorDoesNotFailTheRender(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return scrollState{}, errors.New("evaluate failed")
	}, 0)

	var metrics types.PageMetrics
	// A nil error is what lets buildTasks continue on to extractHTML.
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.False(t, metrics.ScrollPerformed)
	assert.Zero(t, metrics.ScrollSteps)
	// A failed step says nothing about whether the page has a scroller.
	assert.False(t, metrics.ScrollNoTarget)
}

func TestScrollRestoresAfterStepError(t *testing.T) {
	instance := newTestInstance()
	restored := false
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		// The evaluation moved the page before the Go side failed to read its result back.
		return scrollState{}, errors.New("unmarshal failed")
	}, 0)
	loop.restore = func(context.Context) error {
		restored = true
		return nil
	}

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.True(t, restored, "A step that failed may still have scrolled, so the page must be put back")
}

func TestScrollReportsNoTargetOnMetrics(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return scrollState{AnyTarget: false}, nil
	}, 0)

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.False(t, metrics.ScrollPerformed)
	assert.True(t, metrics.ScrollNoTarget)
}

func TestScrollRestoreFailureDoesNotFailTheRender(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return settledPage(), nil
	}, 0)
	loop.restore = func(context.Context) error { return errors.New("restore failed") }

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.True(t, metrics.ScrollPerformed)
}

func TestScrollSkipsRestoreWhenNothingScrolled(t *testing.T) {
	instance := newTestInstance()
	restored := false
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return scrollState{AnyTarget: false}, nil
	}, 0)
	loop.restore = func(context.Context) error {
		restored = true
		return nil
	}

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.False(t, metrics.ScrollPerformed)
	assert.False(t, restored, "Nothing was scrolled, so there is no position to restore")
}

func TestScrollStepPauseIsShortWhileTravelling(t *testing.T) {
	// Travelling steps only need the page to keep moving; the full pause is what gives lazy
	// sections time to mount, and it is held for every step that could reveal something.
	assert.Equal(t, scrollTravelPause, stepPause(scrollModePage, 0))
	assert.Equal(t, scrollStepPause, stepPause(scrollModePage, 1), "settle rounds must not be rushed")
	assert.Equal(t, scrollStepPause, stepPause(scrollModeInner, 0))
}

func TestScrollSettleWindowUsesTheFullPause(t *testing.T) {
	var pauses []time.Duration
	loop, _ := newTestLoop(func(context.Context, string, int) (scrollState, error) {
		return settledPage(), nil
	}, 0)
	loop.pause = func(_ context.Context, d time.Duration) error {
		pauses = append(pauses, d)
		return nil
	}

	_, err := loop.run(context.Background())

	require.NoError(t, err)
	// The page is at its bottom from the first step, so only that step counts as travel.
	require.Len(t, pauses, scrollSettleRounds)
	assert.Equal(t, scrollTravelPause, pauses[0])
	for _, d := range pauses[1:] {
		assert.Equal(t, scrollStepPause, d)
	}
}

func TestScrollStepJSSelectsMode(t *testing.T) {
	assert.Contains(t, scrollStepJS(scrollModePage, 0), "const PAGE_MODE = true;")
	assert.Contains(t, scrollStepJS(scrollModeInner, 0), "const PAGE_MODE = false;")
	assert.Contains(t, scrollStepJS(scrollModePage, 2500), "STEP_PX = 2500")
}

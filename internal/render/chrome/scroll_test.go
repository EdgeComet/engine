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

func newTestLoop(step func(ctx context.Context) (scrollState, error), perStep time.Duration) (scrollLoop, *fakeClock) {
	clock := &fakeClock{now: time.Unix(0, 0), perStep: perStep}
	loop := scrollLoop{
		step:    step,
		pause:   clock.sleep,
		restore: func(context.Context) error { return nil },
		now:     clock.Now,
	}
	return loop, clock
}

func newTestInstance() *ChromeInstance {
	return &ChromeInstance{ID: 7, logger: zap.NewNop()}
}

func newScrollRequest(scroll bool) *types.RenderRequest {
	return &types.RenderRequest{RequestID: "scroll-test", URL: "https://example.com/page", Scroll: scroll}
}

func TestScrollLoopSettlesOnStableHeight(t *testing.T) {
	settled := scrollState{Found: true, Target: "body", ScrollHeight: 3000, AtBottom: true}
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) { return settled, nil }, scrollStepPause)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.True(t, outcome.Performed)
	assert.Equal(t, "body", outcome.Target)
	assert.Equal(t, 3000, outcome.FinalHeight)
	// One step establishes the height, then the settle rounds confirm it.
	assert.Equal(t, 1+scrollSettleRounds, outcome.Steps)
}

func TestScrollLoopReportsNoScroller(t *testing.T) {
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) { return scrollState{Found: false}, nil }, scrollStepPause)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.False(t, outcome.Performed)
	assert.True(t, outcome.NoTarget)
	assert.Zero(t, outcome.Steps)
}

func TestScrollLoopDoesNotReportNoTargetAfterScrolling(t *testing.T) {
	steps := 0
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		steps++
		if steps > 1 {
			// The SPA replaced the container and detection has nothing left to pick.
			return scrollState{Found: false}, nil
		}
		return scrollState{Found: true, Target: "DIV.feed", ScrollHeight: 2000}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.True(t, outcome.Performed)
	// The page did scroll, so this is not the detection heuristic failing on the page.
	assert.False(t, outcome.NoTarget)
}

func TestScrollLoopStopsOnMaxSteps(t *testing.T) {
	height := 1000
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		height += 100
		return scrollState{Found: true, Target: "body", ScrollHeight: height, AtBottom: true}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
}

func TestScrollLoopStopsOnMaxDuration(t *testing.T) {
	height := 1000
	perStep := types.ScrollMaxDuration / 3
	loop, clock := newTestLoop(func(context.Context) (scrollState, error) {
		height += 100
		return scrollState{Found: true, Target: "body", ScrollHeight: height, AtBottom: true}, nil
	}, perStep)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	assert.Less(t, outcome.Steps, scrollMaxSteps)
	assert.True(t, clock.Now().After(time.Unix(0, 0).Add(types.ScrollMaxDuration)))
}

func TestScrollLoopStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	steps := 0
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		steps++
		cancel()
		return scrollState{Found: true, Target: "body", ScrollHeight: 1000 + steps}, nil
	}, 0)

	outcome, err := loop.run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, outcome.Steps)
}

func TestScrollLoopReportsNothingOnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: true, Target: "body", ScrollHeight: 3000}, nil
	}, 0)

	outcome, err := loop.run(ctx)

	require.NoError(t, err)
	assert.False(t, outcome.Performed)
	// The loop never asked the page anything, so it cannot claim the page has no scroller.
	assert.False(t, outcome.NoTarget)
}

func TestScrollLoopReturnsPartialOutcomeOnStepError(t *testing.T) {
	stepErr := errors.New("evaluate failed")
	steps := 0
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		steps++
		if steps > 1 {
			return scrollState{}, stepErr
		}
		return scrollState{Found: true, Target: "DIV.feed", ScrollHeight: 2000}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.ErrorIs(t, err, stepErr)
	assert.True(t, outcome.Performed)
	assert.Equal(t, "DIV.feed", outcome.Target)
	assert.Equal(t, 1, outcome.Steps)
}

func TestScrollLoopKeepsGoingWhileHeightGrows(t *testing.T) {
	height := 1000
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		height += 10
		return scrollState{Found: true, Target: "body", ScrollHeight: height, AtBottom: true}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	// At the bottom on every step, but the page keeps producing content, so it never settles.
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
}

func TestScrollLoopKeepsGoingWhileNotAtBottom(t *testing.T) {
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: true, Target: "body", ScrollHeight: 9000, AtBottom: false}, nil
	}, 0)

	outcome, err := loop.run(context.Background())

	require.NoError(t, err)
	// A stable height alone is not settled: the target still has viewport left to travel.
	assert.Equal(t, scrollMaxSteps, outcome.Steps)
}

func TestScrollIsSkippedWhenNotRequested(t *testing.T) {
	instance := newTestInstance()
	var metrics types.PageMetrics

	require.NoError(t, instance.scrollIfRequested(newScrollRequest(false), &metrics).Do(context.Background()))

	assert.False(t, metrics.ScrollPerformed)
	assert.Zero(t, metrics.ScrollSteps)
	assert.Zero(t, metrics.ScrollDuration)
	assert.Empty(t, metrics.ScrollTarget)
}

func TestScrollRecordsOutcomeOnMetrics(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: true, Target: "DIV.feed", ScrollHeight: 4200, AtBottom: true}, nil
	}, scrollStepPause)

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.True(t, metrics.ScrollPerformed)
	assert.Equal(t, "DIV.feed", metrics.ScrollTarget)
	assert.Equal(t, 1+scrollSettleRounds, metrics.ScrollSteps)
	assert.Equal(t, 4200, metrics.ScrollFinalHeight)
	assert.Positive(t, metrics.ScrollDuration)
}

func TestScrollStepErrorDoesNotFailTheRender(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
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
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
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
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: false}, nil
	}, 0)

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.False(t, metrics.ScrollPerformed)
	assert.True(t, metrics.ScrollNoTarget)
}

func TestScrollRestoreFailureDoesNotFailTheRender(t *testing.T) {
	instance := newTestInstance()
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: true, Target: "body", ScrollHeight: 3000, AtBottom: true}, nil
	}, 0)
	loop.restore = func(context.Context) error { return errors.New("restore failed") }

	var metrics types.PageMetrics
	require.NoError(t, instance.runScroll(context.Background(), loop, newScrollRequest(true), &metrics))

	assert.True(t, metrics.ScrollPerformed)
}

func TestScrollSkipsRestoreWhenNothingScrolled(t *testing.T) {
	instance := newTestInstance()
	restored := false
	loop, _ := newTestLoop(func(context.Context) (scrollState, error) {
		return scrollState{Found: false}, nil
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

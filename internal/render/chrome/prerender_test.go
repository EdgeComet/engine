package chrome

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/pkg/types"
)

// probeSequence replays one scripted answer per tick, and repeats the last one once the script
// runs out, which is how "never ready" is expressed.
type probeSequence struct {
	answers []probeAnswer
	calls   atomic.Int64
}

type probeAnswer struct {
	state prerenderState
	err   error
}

func (p *probeSequence) probe(context.Context) (prerenderState, error) {
	index := int(p.calls.Add(1)) - 1
	if index >= len(p.answers) {
		index = len(p.answers) - 1
	}
	answer := p.answers[index]

	return answer.state, answer.err
}

func notReady() probeAnswer { return probeAnswer{} }
func ready() probeAnswer    { return probeAnswer{state: prerenderState{Ready: true}} }
func probeFails() probeAnswer {
	return probeAnswer{err: errors.New("Cannot find context with specified id")}
}

func parked(url string) probeAnswer {
	return probeAnswer{state: prerenderState{RedirectURL: url}}
}

// pollTestTimeout has to outlast several ticks so a test that expects an exit is not racing the
// budget, while keeping the timeout cases quick.
const pollTestTimeout = 2 * time.Second

func TestPollPrerenderReady(t *testing.T) {
	t.Run("exits when the page reports itself ready", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{notReady(), notReady(), ready()}}

		redirectURL, err := pollPrerenderReady(context.Background(), seq.probe, pollTestTimeout)

		require.NoError(t, err)
		assert.Empty(t, redirectURL)
		assert.Equal(t, int64(3), seq.calls.Load())
	})

	t.Run("exits on a parked redirect", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{notReady(), parked("/en/404")}}

		redirectURL, err := pollPrerenderReady(context.Background(), seq.probe, pollTestTimeout)

		require.NoError(t, err)
		assert.Equal(t, "/en/404", redirectURL)
	})

	t.Run("a redirect wins over readiness in the same tick", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{
			{state: prerenderState{Ready: true, RedirectURL: "/en/404"}},
		}}

		redirectURL, err := pollPrerenderReady(context.Background(), seq.probe, pollTestTimeout)

		require.NoError(t, err)
		assert.Equal(t, "/en/404", redirectURL, "a soft 404 sets both, and the redirect is the truth")
	})

	t.Run("a probe error keeps polling", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{probeFails(), probeFails(), ready()}}

		redirectURL, err := pollPrerenderReady(context.Background(), seq.probe, pollTestTimeout)

		require.NoError(t, err, "an evaluation failure must never fail the render")
		assert.Empty(t, redirectURL)
		assert.Equal(t, int64(3), seq.calls.Load())
	})

	t.Run("expiry returns the wait timeout", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{notReady()}}

		redirectURL, err := pollPrerenderReady(context.Background(), seq.probe, 4*prerenderPollInterval)

		assert.ErrorIs(t, err, ErrWaitTimeout, "the caller's soft-timeout path keys on this error")
		assert.Empty(t, redirectURL)
		assert.Greater(t, seq.calls.Load(), int64(1), "the page is sampled repeatedly, not once")
	})

	t.Run("a page that only ever fails to answer times out", func(t *testing.T) {
		seq := &probeSequence{answers: []probeAnswer{probeFails()}}

		_, err := pollPrerenderReady(context.Background(), seq.probe, 4*prerenderPollInterval)

		assert.ErrorIs(t, err, ErrWaitTimeout)
	})

	// A page running a long synchronous task leaves an evaluation unanswered for as long as the
	// task runs. Without a bound on each sample the loop sits inside the probe, misses its own
	// budget, and on a page that stays busy past the render's hard deadline reports that
	// cancellation instead of a soft timeout - losing the partial capture the wait exists to keep.
	t.Run("a page that never answers still expires on its own budget", func(t *testing.T) {
		hardDeadline := 8 * prerenderProbeTimeout
		softTimeout := 2 * prerenderProbeTimeout

		ctx, cancel := context.WithTimeout(context.Background(), hardDeadline)
		defer cancel()

		var calls atomic.Int64
		unanswered := func(ctx context.Context) (prerenderState, error) {
			calls.Add(1)
			<-ctx.Done()
			return prerenderState{}, ctx.Err()
		}

		start := time.Now()
		_, err := pollPrerenderReady(ctx, unanswered, softTimeout)

		assert.ErrorIs(t, err, ErrWaitTimeout)
		assert.NotErrorIs(t, err, context.DeadlineExceeded, "the soft budget must expire first")
		assert.Less(t, time.Since(start), hardDeadline)
		assert.Greater(t, calls.Load(), int64(1), "an unanswered sample is abandoned, not waited on")
	})

	t.Run("context cancellation returns the context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		seq := &probeSequence{answers: []probeAnswer{notReady()}}

		go func() {
			time.Sleep(2 * prerenderPollInterval)
			cancel()
		}()

		start := time.Now()
		_, err := pollPrerenderReady(ctx, seq.probe, time.Minute)
		cancel()

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, ErrWaitTimeout, "a cancelled render is not a soft timeout")
		assert.Less(t, time.Since(start), 5*time.Second, "cancellation must return promptly")
	})
}

func TestPrerenderPollScript(t *testing.T) {
	script := fmt.Sprintf(prerenderPollTemplate, types.WaitForPrerenderContentReady)

	assert.Contains(t, script, `window["prerenderContentReady"]`, "the configured property is read")
	assert.Contains(t, script, "window."+prerenderRedirectProperty,
		"both properties are read in one evaluation")
	// One catch per property read plus the outer one: a failure anywhere reads as not ready with
	// no redirect instead of surfacing as an evaluation error.
	assert.Equal(t, 3, strings.Count(script, "catch (e)"))
}

func TestSeedPrerenderFlagScript(t *testing.T) {
	assert.Contains(t, prerenderSeedScript, "window.isPrerender = true")
	assert.Contains(t, prerenderSeedScript, "catch (e)", "a page that forbids the write must not break the render")
}

func TestSeedPrerenderFlagIsGatedOnTheWait(t *testing.T) {
	// Running the action without a browser is what makes the gate visible: the seeded branch
	// reaches for the target and reports the failure, the other branch never looks.
	runSeed := func(waitFor string) []observer.LoggedEntry {
		core, logs := observer.New(zap.WarnLevel)
		instance := &ChromeInstance{ID: 1, logger: zap.New(core)}

		action := instance.seedPrerenderFlag(&types.RenderRequest{RequestID: "test", WaitFor: waitFor})
		assert.NoError(t, action(context.Background()), "a failed install must never fail the render")

		return logs.All()
	}

	for _, waitFor := range []string{types.WaitForPrerenderReady, types.WaitForPrerenderContentReady} {
		assert.Len(t, runSeed(waitFor), 1, "%s installs the flag", waitFor)
	}

	for _, waitFor := range []string{types.LifecycleEventNetworkIdle, types.LifecycleEventLoad, ""} {
		assert.Empty(t, runSeed(waitFor), "%q must not touch the page", waitFor)
	}
}

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/pkg/types"
)

func prerenderBudgetRequest(waitFor string, timeout time.Duration) *types.RenderRequest {
	return &types.RenderRequest{
		RequestID: "budget-test",
		URL:       "https://example.com/page",
		Timeout:   timeout,
		WaitFor:   waitFor,
	}
}

func TestWarnOnPrerenderBudgetShortfall(t *testing.T) {
	tests := []struct {
		name       string
		waitFor    string
		timeout    time.Duration
		wantWarned bool
	}{
		{
			name:       "lifecycle wait never warns",
			waitFor:    types.LifecycleEventNetworkIdle,
			timeout:    testMaxTimeout,
			wantWarned: false,
		},
		{
			name:       "readiness wait with headroom",
			waitFor:    types.WaitForPrerenderContentReady,
			timeout:    testMaxTimeout - time.Second,
			wantWarned: false,
		},
		{
			name:       "readiness wait can use the whole budget",
			waitFor:    types.WaitForPrerenderContentReady,
			timeout:    testMaxTimeout,
			wantWarned: true,
		},
		{
			name:       "a timeout clamped up to the hard limit warns too",
			waitFor:    types.WaitForPrerenderReady,
			timeout:    testMaxTimeout + time.Minute,
			wantWarned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			prerenderBudgetGate = &logGate{interval: budgetWarnInterval}

			warnOnPrerenderBudgetShortfall(prerenderBudgetRequest(tt.waitFor, tt.timeout), testMaxTimeout, zap.New(core))

			if tt.wantWarned {
				assert.Equal(t, 1, logs.Len())
				return
			}
			assert.Zero(t, logs.Len())
		})
	}
}

func TestWarnOnPrerenderBudgetShortfallIsThrottled(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	prerenderBudgetGate = &logGate{interval: budgetWarnInterval}
	logger := zap.New(core)

	req := prerenderBudgetRequest(types.WaitForPrerenderContentReady, testMaxTimeout)
	for range 5 {
		warnOnPrerenderBudgetShortfall(req, testMaxTimeout, logger)
	}

	assert.Equal(t, 1, logs.Len(), "A misconfigured host must not flood the log")
}

func TestBudgetWarningsAreGatedSeparately(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	scrollBudgetGate = &logGate{interval: budgetWarnInterval}
	prerenderBudgetGate = &logGate{interval: budgetWarnInterval}
	logger := zap.New(core)

	req := prerenderBudgetRequest(types.WaitForPrerenderContentReady, testMaxTimeout)
	req.Scroll = true

	warnOnScrollBudgetShortfall(req, testMaxTimeout, logger)
	warnOnPrerenderBudgetShortfall(req, testMaxTimeout, logger)

	assert.Equal(t, 2, logs.Len(), "one warning must not swallow the other")
}

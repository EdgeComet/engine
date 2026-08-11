package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/edgecomet/engine/pkg/types"
)

const testMaxTimeout = 50 * time.Second

func scrollBudgetRequest(scroll bool, timeout time.Duration) *types.RenderRequest {
	return &types.RenderRequest{
		RequestID: "budget-test",
		URL:       "https://example.com/page",
		Timeout:   timeout,
		Scroll:    scroll,
	}
}

func TestWarnOnScrollBudgetShortfall(t *testing.T) {
	tests := []struct {
		name       string
		scroll     bool
		timeout    time.Duration
		wantWarned bool
	}{
		{
			name:       "scroll disabled never warns",
			scroll:     false,
			timeout:    testMaxTimeout,
			wantWarned: false,
		},
		{
			name:       "budget fits",
			scroll:     true,
			timeout:    testMaxTimeout - types.ScrollMaxDuration,
			wantWarned: false,
		},
		{
			name:       "budget overruns the hard timeout",
			scroll:     true,
			timeout:    testMaxTimeout - types.ScrollMaxDuration + time.Second,
			wantWarned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			scrollBudgetGate = &logGate{interval: scrollBudgetWarnInterval}

			warnOnScrollBudgetShortfall(scrollBudgetRequest(tt.scroll, tt.timeout), testMaxTimeout, zap.New(core))

			if tt.wantWarned {
				assert.Equal(t, 1, logs.Len())
				return
			}
			assert.Zero(t, logs.Len())
		})
	}
}

func TestWarnOnScrollBudgetShortfallIsThrottled(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	scrollBudgetGate = &logGate{interval: scrollBudgetWarnInterval}
	logger := zap.New(core)

	req := scrollBudgetRequest(true, testMaxTimeout)
	for range 5 {
		warnOnScrollBudgetShortfall(req, testMaxTimeout, logger)
	}

	assert.Equal(t, 1, logs.Len(), "A misconfigured host must not flood the log")
}

func TestLogGateReopensAfterInterval(t *testing.T) {
	gate := &logGate{interval: time.Minute}
	start := time.Now()

	assert.True(t, gate.allow(start))
	assert.False(t, gate.allow(start.Add(59*time.Second)))
	assert.True(t, gate.allow(start.Add(time.Minute)))
}

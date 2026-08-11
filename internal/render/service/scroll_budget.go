package service

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

// scrollBudgetWarnInterval throttles the misconfiguration warning below. The condition is a
// property of a host's configuration, not of the individual request, so one line per interval
// carries the whole signal while a busy host cannot flood the log with it.
const scrollBudgetWarnInterval = 5 * time.Minute

// logGate admits one event per interval.
type logGate struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func (g *logGate) allow(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.last.IsZero() && now.Sub(g.last) < g.interval {
		return false
	}
	g.last = now

	return true
}

var scrollBudgetGate = &logGate{interval: scrollBudgetWarnInterval}

// warnOnScrollBudgetShortfall reports a request whose scroll pass cannot fit inside the hard
// render timeout. Neither process holds both values at startup - max_timeout is render-service
// configuration and the host render timeout arrives per request - so this is the only place the
// two meet. It is a signal, not a rejection: the render proceeds and the hard timeout cuts it
// short if the page really does use the whole budget.
func warnOnScrollBudgetShortfall(req *types.RenderRequest, maxTimeout time.Duration, logger *zap.Logger) {
	if !req.Scroll || req.Timeout+types.ScrollMaxDuration <= maxTimeout {
		return
	}
	if !scrollBudgetGate.allow(time.Now()) {
		return
	}

	logger.Warn("Scroll budget does not fit inside the hard render timeout, renders may be cut short",
		zap.String("request_id", req.RequestID),
		zap.String("url", req.URL),
		zap.Duration("render_timeout", req.Timeout),
		zap.Duration("scroll_max_duration", types.ScrollMaxDuration),
		zap.Duration("max_timeout", maxTimeout))
}

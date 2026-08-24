package service

import (
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

var prerenderBudgetGate = &logGate{interval: budgetWarnInterval}

// warnOnPrerenderBudgetShortfall reports a host whose render timeout leaves no room inside the
// hard timeout for anything after the wait. A readiness wait is far more likely to run its full
// length than a lifecycle wait: a page that stops setting the readiness property never signals at
// all, where the lifecycle events fire on almost every page.
//
// Like the scroll warning, this is the only place a render-service limit and a per-request host
// timeout meet, and it is a signal rather than a rejection: the render proceeds.
func warnOnPrerenderBudgetShortfall(req *types.RenderRequest, maxTimeout time.Duration, logger *zap.Logger) {
	if !types.IsPrerenderWait(req.WaitFor) || req.Timeout < maxTimeout {
		return
	}
	if !prerenderBudgetGate.allow(time.Now()) {
		return
	}

	logger.Warn("Readiness wait can consume the whole hard timeout, leaving no budget for HTML extraction: a page that never signals ends the render as a hard timeout with no HTML",
		zap.String("request_id", req.RequestID),
		zap.String("url", req.URL),
		zap.String("wait_for", req.WaitFor),
		zap.Duration("render_timeout", req.Timeout),
		zap.Duration("max_timeout", maxTimeout))
}

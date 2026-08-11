package orchestrator

import (
	"time"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

// BuildRenderRequest creates a RenderRequest from resolved config and dimension.
// Caller-specific fields (Headers, IncludeHAR) must be set separately.
func BuildRenderRequest(url, requestID, renderKey string, tabID int, resolvedRender *config.ResolvedRenderConfig, dimension *types.Dimension) *types.RenderRequest {
	req := &types.RenderRequest{
		RequestID:      requestID,
		URL:            url,
		TabID:          tabID,
		ViewportWidth:  dimension.Width,
		ViewportHeight: dimension.Height,
		UserAgent:      dimension.RenderUA,
		RenderKey:      renderKey,
	}

	applyRenderConfig(req, resolvedRender)

	return req
}

// applyRenderConfig copies resolved render configuration onto a render request. Every path that
// builds a render request routes through it, so a new render field reaches all of them at once
// instead of being filled by hand on the paths someone remembers.
func applyRenderConfig(req *types.RenderRequest, resolvedRender *config.ResolvedRenderConfig) {
	var extraWait time.Duration
	if resolvedRender.Events.AdditionalWait != nil {
		extraWait = time.Duration(*resolvedRender.Events.AdditionalWait)
	}

	req.Timeout = resolvedRender.Timeout
	req.WaitFor = resolvedRender.Events.WaitFor
	req.ExtraWait = extraWait
	req.BlockedPatterns = resolvedRender.BlockedPatterns
	req.BlockedResourceTypes = resolvedRender.BlockedResourceTypes
	req.Scroll = resolvedRender.Scroll
}

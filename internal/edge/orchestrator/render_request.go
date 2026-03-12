package orchestrator

import (
	"time"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

// BuildRenderRequest creates a RenderRequest from resolved config and dimension.
// Caller-specific fields (Headers, IncludeHAR) must be set separately.
func BuildRenderRequest(url, requestID string, tabID int, resolvedRender *config.ResolvedRenderConfig, dimension *types.Dimension) *types.RenderRequest {
	var extraWait time.Duration
	if resolvedRender.Events.AdditionalWait != nil {
		extraWait = time.Duration(*resolvedRender.Events.AdditionalWait)
	}

	return &types.RenderRequest{
		RequestID:            requestID,
		URL:                  url,
		TabID:                tabID,
		ViewportWidth:        dimension.Width,
		ViewportHeight:       dimension.Height,
		UserAgent:            dimension.RenderUA,
		Timeout:              resolvedRender.Timeout,
		WaitFor:              resolvedRender.Events.WaitFor,
		ExtraWait:            extraWait,
		BlockedPatterns:      resolvedRender.BlockedPatterns,
		BlockedResourceTypes: resolvedRender.BlockedResourceTypes,
		StripScripts:         resolvedRender.StripScripts,
	}
}

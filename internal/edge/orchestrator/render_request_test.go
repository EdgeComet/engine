package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

func TestBuildRenderRequest(t *testing.T) {
	additionalWait := types.Duration(500 * time.Millisecond)

	resolved := &config.ResolvedRenderConfig{
		Timeout: 30 * time.Second,
		Events: types.RenderEvents{
			WaitFor:        "networkIdle",
			AdditionalWait: &additionalWait,
		},
		BlockedPatterns:      []string{"*.analytics.com"},
		BlockedResourceTypes: []string{"Image", "Font"},
		StripScripts:         true,
	}

	dimension := &types.Dimension{
		Width:    1920,
		Height:   1080,
		RenderUA: "TestBot/1.0",
	}

	req := BuildRenderRequest("https://example.com/page", "req-123", 5, resolved, dimension)

	assert.Equal(t, "https://example.com/page", req.URL)
	assert.Equal(t, "req-123", req.RequestID)
	assert.Equal(t, 5, req.TabID)
	assert.Equal(t, 1920, req.ViewportWidth)
	assert.Equal(t, 1080, req.ViewportHeight)
	assert.Equal(t, "TestBot/1.0", req.UserAgent)
	assert.Equal(t, 30*time.Second, req.Timeout)
	assert.Equal(t, "networkIdle", req.WaitFor)
	assert.Equal(t, 500*time.Millisecond, req.ExtraWait)
	assert.Equal(t, []string{"*.analytics.com"}, req.BlockedPatterns)
	assert.Equal(t, []string{"Image", "Font"}, req.BlockedResourceTypes)
	assert.True(t, req.StripScripts)
	assert.False(t, req.IncludeHAR)
	assert.Nil(t, req.Headers)
}

func TestBuildRenderRequest_NilAdditionalWait(t *testing.T) {
	resolved := &config.ResolvedRenderConfig{
		Timeout: 15 * time.Second,
		Events: types.RenderEvents{
			WaitFor: "load",
		},
		StripScripts: false,
	}

	dimension := &types.Dimension{
		Width:    375,
		Height:   812,
		RenderUA: "MobileBot/1.0",
	}

	req := BuildRenderRequest("https://example.com/m", "req-456", 2, resolved, dimension)

	assert.Equal(t, time.Duration(0), req.ExtraWait)
	assert.Equal(t, "load", req.WaitFor)
	assert.Equal(t, 15*time.Second, req.Timeout)
	assert.False(t, req.StripScripts)
	assert.Equal(t, 375, req.ViewportWidth)
	assert.Equal(t, 812, req.ViewportHeight)
	assert.Equal(t, "MobileBot/1.0", req.UserAgent)
}

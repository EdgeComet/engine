package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

func testLogger() *zap.Logger {
	return zap.NewNop()
}

func TestInjectBypassDimension_NoDimensions(t *testing.T) {
	host := &types.Host{}

	err := PrepareHost(host, nil, "test", testLogger())
	require.NoError(t, err)

	dim, exists := host.Dimensions[types.BypassDimensionName]
	require.True(t, exists, "bypass dimension should be injected")
	assert.Equal(t, types.BypassDimensionID, dim.ID)
	assert.Equal(t, types.ActionBypass, dim.Action)
	assert.Equal(t, defaultBypassWidth, dim.Width)
	assert.Equal(t, defaultBypassHeight, dim.Height)
	assert.Equal(t, defaultBypassRenderUA, dim.RenderUA)
	assert.Nil(t, dim.MatchUA)
}

func TestInjectBypassDimension_EmptyDimensionsMap(t *testing.T) {
	host := &types.Host{
		Dimensions: map[string]types.Dimension{},
	}

	err := PrepareHost(host, nil, "test", testLogger())
	require.NoError(t, err)

	dim, exists := host.Dimensions[types.BypassDimensionName]
	require.True(t, exists, "bypass dimension should be injected into empty map")
	assert.Equal(t, types.BypassDimensionID, dim.ID)
	assert.Equal(t, types.ActionBypass, dim.Action)
	assert.Equal(t, defaultBypassWidth, dim.Width)
	assert.Equal(t, defaultBypassHeight, dim.Height)
}

func TestInjectBypassDimension_UserDeclaredWithMatchUA(t *testing.T) {
	host := &types.Host{
		Dimensions: map[string]types.Dimension{
			types.BypassDimensionName: {
				MatchUA: []string{"*Googlebot*", "*Bingbot*"},
			},
		},
	}

	err := PrepareHost(host, nil, "test", testLogger())
	require.NoError(t, err)

	dim := host.Dimensions[types.BypassDimensionName]
	assert.Equal(t, types.BypassDimensionID, dim.ID)
	assert.Equal(t, types.ActionBypass, dim.Action)
	assert.Equal(t, defaultBypassWidth, dim.Width)
	assert.Equal(t, defaultBypassHeight, dim.Height)
	assert.Equal(t, defaultBypassRenderUA, dim.RenderUA)
	assert.Equal(t, []string{"*Googlebot*", "*Bingbot*"}, dim.MatchUA)
}

func TestInjectBypassDimension_UserDeclaredCustomWidthHeight(t *testing.T) {
	host := &types.Host{
		Dimensions: map[string]types.Dimension{
			types.BypassDimensionName: {
				Width:  1024,
				Height: 768,
			},
		},
	}

	err := PrepareHost(host, nil, "test", testLogger())
	require.NoError(t, err)

	dim := host.Dimensions[types.BypassDimensionName]
	assert.Equal(t, 1024, dim.Width)
	assert.Equal(t, 768, dim.Height)
	assert.Equal(t, types.ActionBypass, dim.Action)
	assert.Equal(t, defaultBypassRenderUA, dim.RenderUA)
}

func TestInjectBypassDimension_InheritedGlobalDimensions(t *testing.T) {
	globalConfig := &configtypes.EgConfig{
		Dimensions: map[string]types.Dimension{
			"desktop": {
				ID:    1,
				Width: 1920,
			},
		},
	}
	host := &types.Host{}

	err := PrepareHost(host, globalConfig, "test", testLogger())
	require.NoError(t, err)

	_, hasDesktop := host.Dimensions["desktop"]
	assert.True(t, hasDesktop, "desktop dimension should be inherited")

	dim, hasBypass := host.Dimensions[types.BypassDimensionName]
	assert.True(t, hasBypass, "bypass dimension should be injected after inheritance")
	assert.Equal(t, types.BypassDimensionID, dim.ID)
	assert.Equal(t, types.ActionBypass, dim.Action)
}

func TestInjectBypassDimension_MatchUAPatternsCompiled(t *testing.T) {
	host := &types.Host{
		Dimensions: map[string]types.Dimension{
			types.BypassDimensionName: {
				MatchUA: []string{"*Googlebot*"},
			},
		},
	}

	err := PrepareHost(host, nil, "test", testLogger())
	require.NoError(t, err)

	dim := host.Dimensions[types.BypassDimensionName]
	require.Len(t, dim.CompiledPatterns, 1, "MatchUA patterns should be compiled")
	assert.True(t, dim.CompiledPatterns[0].Match("Mozilla/5.0 Googlebot/2.1"))
	assert.False(t, dim.CompiledPatterns[0].Match("Mozilla/5.0 Bingbot/2.0"))
}

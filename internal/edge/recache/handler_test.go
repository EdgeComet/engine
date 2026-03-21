package recache

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

type mockEGConfigManager struct {
	hosts []types.Host
}

func (m *mockEGConfigManager) GetConfig() *configtypes.EgConfig {
	return &configtypes.EgConfig{}
}

func (m *mockEGConfigManager) GetHosts() []types.Host {
	return m.hosts
}

func (m *mockEGConfigManager) GetHostByDomain(domain string) *types.Host {
	return nil
}

func TestHandleRecache_Validation(t *testing.T) {
	rs := &RecacheService{
		logger: zap.NewNop(),
		configManager: &mockEGConfigManager{
			hosts: []types.Host{
				{
					ID:     1,
					Domain: "example.com",
					Dimensions: map[string]types.Dimension{
						"bypass": {ID: 0, Action: types.ActionBypass},
						"mobile": {ID: 1},
					},
				},
			},
		},
	}

	t.Run("dimension_id 0 is accepted past validation", func(t *testing.T) {
		req := RecacheRequest{
			URL:         "https://example.com/page",
			HostID:      1,
			DimensionID: 0,
		}
		body, _ := json.Marshal(req)

		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetBody(body)

		rs.handleRecache(ctx)

		// Validation passes (not 400); fails later in ProcessRecache due to missing dependencies
		assert.NotEqual(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("missing url is rejected", func(t *testing.T) {
		req := RecacheRequest{
			URL:         "",
			HostID:      1,
			DimensionID: 1,
		}
		body, _ := json.Marshal(req)

		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetBody(body)

		rs.handleRecache(ctx)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})

	t.Run("missing host_id is rejected", func(t *testing.T) {
		req := RecacheRequest{
			URL:         "https://example.com/page",
			HostID:      0,
			DimensionID: 1,
		}
		body, _ := json.Marshal(req)

		ctx := &fasthttp.RequestCtx{}
		ctx.Request.SetBody(body)

		rs.handleRecache(ctx)

		assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	})
}

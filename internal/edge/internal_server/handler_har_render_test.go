package internal_server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

// mockConfigManager implements configtypes.EGConfigManager for testing
type mockConfigManager struct {
	hosts []types.Host
}

func (m *mockConfigManager) GetConfig() *configtypes.EgConfig {
	return nil
}

func (m *mockConfigManager) GetHosts() []types.Host {
	return m.hosts
}

func (m *mockConfigManager) GetHostByDomain(domain string) *types.Host {
	normalizedDomain := strings.ToLower(domain)
	for i := range m.hosts {
		// Check all domains in the Domains array
		for _, d := range m.hosts[i].Domains {
			if strings.ToLower(d) == normalizedDomain {
				return &m.hosts[i]
			}
		}
		// Fallback: check Domain field for backward compat with tests
		if strings.ToLower(m.hosts[i].Domain) == normalizedDomain {
			return &m.hosts[i]
		}
	}
	return nil
}

// mockOrchestrator implements HARRenderOrchestrator for testing
type mockOrchestrator struct {
	available     bool
	callCount     int
	availableAt   int // Returns true after this many calls
	renderErr     error
	renderSuccess bool
	renderHAR     []byte
}

func (m *mockOrchestrator) HasAvailableCapacity(ctx context.Context) bool {
	m.callCount++
	if m.availableAt > 0 && m.callCount >= m.availableAt {
		return true
	}
	return m.available
}

func (m *mockOrchestrator) RenderWithHAR(ctx context.Context, req *types.RenderRequest, host *types.Host, dimensionConfig *types.Dimension) (*types.RenderResponse, error) {
	if m.renderErr != nil {
		return nil, m.renderErr
	}
	har := m.renderHAR
	if har == nil {
		har = []byte(`{"log":{"version":"1.2"}}`)
	}
	return &types.RenderResponse{
		RequestID: req.RequestID,
		Success:   m.renderSuccess,
		HAR:       har,
		Error:     "",
	}, nil
}

// intPtr is a helper to create pointer to int for tests
func intPtr(i int) *int {
	return &i
}

// createTestConfigManager creates a config manager with test hosts
func createTestConfigManager() *mockConfigManager {
	return &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
						"mobile":  {ID: 2, Width: 375, Height: 667},
					},
				},
			},
			{
				ID:      2,
				Domain:  "other.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
			},
			{
				ID:      3,
				Domain:  "disabled.com",
				Enabled: false,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
			},
		},
	}
}

func TestHandleHARRender_MissingURL(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "missing_url")
}

func TestHandleHARRender_InvalidURLFormat(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=:invalid")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_url")
}

func TestHandleHARRender_URLMissingScheme(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_url")
	assert.Contains(t, string(ctx.Response.Body()), "scheme and host")
}

func TestHandleHARRender_InvalidTimeout(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com&timeout=invalid")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_timeout")
}

func TestHandleHARRender_ValidParameters(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page&dimension=desktop&timeout=30s")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should return 501 Not Implemented (Phase 2 complete)
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_ValidURLMinimal(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should pass validation with default dimension
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_ValidURLWithQueryParams(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/search%3Fq%3Dtest")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_URLOnlyScheme(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_url")
	assert.Contains(t, string(ctx.Response.Body()), "scheme and host")
}

func TestHandleHARRender_NegativeTimeout(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com&timeout=-5s")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_timeout")
	assert.Contains(t, string(ctx.Response.Body()), "positive")
}

func TestHandleHARRender_LargeTimeout(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com&timeout=999h")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Large timeouts are accepted (no upper limit in spec)
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

// Phase 2 tests: Host and Dimension Resolution

func TestHandleHARRender_HostNotFound(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://unknown.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "host_not_found")
	assert.Contains(t, string(ctx.Response.Body()), "unknown.com")
	// Should list available hosts
	assert.Contains(t, string(ctx.Response.Body()), "example.com")
	assert.Contains(t, string(ctx.Response.Body()), "other.com")
}

func TestHandleHARRender_DisabledHostNotMatched(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://disabled.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "host_not_found")
	// Available list should only contain enabled hosts
	body := string(ctx.Response.Body())
	assert.Contains(t, body, "example.com")
	assert.Contains(t, body, "other.com")
	// The Available list is "[example.com other.com]" - disabled.com is not there
}

func TestHandleHARRender_HostFoundCaseInsensitive(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://EXAMPLE.COM/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should find host with case-insensitive matching
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_URLWithPort(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com:8080/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should find host by stripping port from hostname
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_MixedCaseDomainInConfig(t *testing.T) {
	// Config with mixed-case domain
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "MixedCase.COM",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://mixedcase.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should find host despite case mismatch
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_InvalidDimension(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com&dimension=tablet")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_dimension")
	assert.Contains(t, string(ctx.Response.Body()), "tablet")
	// Should list available dimensions
	assert.Contains(t, string(ctx.Response.Body()), "desktop")
	assert.Contains(t, string(ctx.Response.Body()), "mobile")
}

func TestHandleHARRender_ValidExplicitDimension(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com&dimension=mobile")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_DefaultDimensionUsed(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	// No dimension parameter - should use default "desktop"
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_InvalidDefaultDimension(t *testing.T) {
	// Config with invalid default dimension
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "baddefault.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "nonexistent", // Invalid default
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://baddefault.com")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "invalid_dimension")
	assert.Contains(t, string(ctx.Response.Body()), "nonexistent")
}

func TestHARRenderHandler_RegisterEndpoints(t *testing.T) {
	handler := NewHARRenderHandler(createTestConfigManager(), &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())
	server := NewInternalServer("test-key", zap.NewNop())

	handler.RegisterEndpoints(server)

	assert.NotNil(t, server.routes["GET"][PathDebugHARRender])
}

// Phase 3 tests: URL Pattern Rule Checking

func TestHandleHARRender_URLMatchesRenderRule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/render/*", Action: types.ActionRender},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/render/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should proceed to next phase (501 Not Implemented)
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

func TestHandleHARRender_URLMatchesBlockRule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/admin/*", Action: types.ActionBlock},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/admin/secret")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusForbidden, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "url_blocked")
}

func TestHandleHARRender_URLMatchesBypassRule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/static/*", Action: types.ActionBypass},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/static/image.png")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadRequest, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "url_bypass")
	assert.Contains(t, string(ctx.Response.Body()), "not supported")
}

func TestHandleHARRender_URLMatchesStatus404Rule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/deleted/*", Action: types.ActionStatus404},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/deleted/old-page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusNotFound, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "url_status")
	assert.Contains(t, string(ctx.Response.Body()), "status_404")
}

func TestHandleHARRender_URLMatchesStatus410Rule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/gone/*", Action: types.ActionStatus410},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/gone/permanently")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusGone, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "url_status")
	assert.Contains(t, string(ctx.Response.Body()), "status_410")
}

func TestHandleHARRender_URLMatchesCustomStatusRule(t *testing.T) {
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: []types.URLRule{
					{Match: "/redirect/*", Action: types.ActionStatus, Status: &types.StatusRuleConfig{Code: intPtr(301)}},
				},
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/redirect/old")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, 301, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "url_status")
	assert.Contains(t, string(ctx.Response.Body()), "301")
}

func TestHandleHARRender_NoURLRules(t *testing.T) {
	// Host with no URL rules - should proceed
	cm := &mockConfigManager{
		hosts: []types.Host{
			{
				ID:      1,
				Domain:  "example.com",
				Enabled: true,
				Render: types.RenderConfig{
					UnmatchedDimension: "desktop",
					Dimensions: map[string]types.Dimension{
						"desktop": {ID: 1, Width: 1920, Height: 1080},
					},
				},
				URLRules: nil, // No rules
			},
		},
	}
	handler := NewHARRenderHandler(cm, &mockOrchestrator{available: true, renderSuccess: true}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/any/path")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should proceed to next phase
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
}

// Phase 4 tests: Tab Availability Polling

func TestHandleHARRender_TabAvailableImmediately(t *testing.T) {
	checker := &mockOrchestrator{available: true, renderSuccess: true}
	handler := NewHARRenderHandler(createTestConfigManager(), checker, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should proceed to next phase
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, 1, checker.callCount)
}

func TestHandleHARRender_TabBecomesAvailable(t *testing.T) {
	// Returns true on 3rd call
	checker := &mockOrchestrator{available: false, availableAt: 3, renderSuccess: true}
	handler := NewHARRenderHandler(createTestConfigManager(), checker, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	// Should proceed after polling
	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, 3, checker.callCount)
}

// Phase 5 tests: Render Service Integration

func TestHandleHARRender_ReturnsHARJSON(t *testing.T) {
	expectedHAR := []byte(`{"log":{"version":"1.2","creator":{"name":"test"}}}`)
	checker := &mockOrchestrator{available: true, renderSuccess: true, renderHAR: expectedHAR}
	handler := NewHARRenderHandler(createTestConfigManager(), checker, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusOK, ctx.Response.StatusCode())
	assert.Equal(t, "application/json", string(ctx.Response.Header.ContentType()))
	assert.Equal(t, expectedHAR, ctx.Response.Body())
}

func TestHandleHARRender_RenderServiceError(t *testing.T) {
	checker := &mockOrchestrator{available: true, renderErr: fmt.Errorf("connection refused")}
	handler := NewHARRenderHandler(createTestConfigManager(), checker, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadGateway, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "render_failed")
	assert.Contains(t, string(ctx.Response.Body()), "connection refused")
}

func TestHandleHARRender_RenderFails(t *testing.T) {
	checker := &mockOrchestrator{available: true, renderSuccess: false}
	handler := NewHARRenderHandler(createTestConfigManager(), checker, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/debug/har/render?url=https://example.com/page")
	ctx.Request.Header.SetMethod("GET")

	handler.handleHARRender(ctx)

	assert.Equal(t, fasthttp.StatusBadGateway, ctx.Response.StatusCode())
	assert.Contains(t, string(ctx.Response.Body()), "render_failed")
}

package auth

import (
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/pkg/types"
)

// AuthenticationService handles render key validation and host authorization
type AuthenticationService struct {
	configManager configtypes.EGConfigManager
	logger        *zap.Logger
}

// NewAuthenticationService creates a new AuthenticationService instance
func NewAuthenticationService(configManager configtypes.EGConfigManager, logger *zap.Logger) *AuthenticationService {
	return &AuthenticationService{
		configManager: configManager,
		logger:        logger,
	}
}

// ValidateRenderKey validates the X-Render-Key header and returns the authorized host
func (as *AuthenticationService) ValidateRenderKey(renderCtx *edgectx.RenderContext) (*types.Host, error) {
	renderKey := string(renderCtx.HTTPCtx.Request.Header.Peek("X-Render-Key"))
	if renderKey == "" {
		return nil, fmt.Errorf("X-Render-Key header is required")
	}

	parsedURL, err := url.Parse(renderCtx.TargetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target URL: %w", err)
	}

	// Normalize hostname to lowercase for case-insensitive comparison (RFC 1123)
	hostname := parsedURL.Hostname()
	normalizedHostname := strings.ToLower(hostname)

	// Use O(1) domain lookup - matches any domain in host.Domains array
	host := as.configManager.GetHostByDomain(normalizedHostname)
	if host != nil && host.RenderKey == renderKey && host.Enabled {
		renderCtx.Logger.Debug("Authentication successful",
			zap.String("domain", host.Domain),
			zap.Int("host_id", host.ID))
		return host, nil
	}

	renderCtx.Logger.Warn("Authentication failed",
		zap.String("domain", hostname),
		zap.String("render_key_prefix", renderKey[:min(8, len(renderKey))]))

	return nil, fmt.Errorf("invalid render key or domain mismatch")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

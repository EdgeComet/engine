package orchestrator

import (
	"fmt"
	"net/url"

	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/internal/edge/edgectx"
)

// ExtractURL extracts and validates URL from the query parameter
func ExtractURL(renderCtx *edgectx.RenderContext) (string, error) {
	urlParam := renderCtx.HTTPCtx.QueryArgs().Peek("url")
	if len(urlParam) == 0 {
		return "", fmt.Errorf("missing required 'url' query parameter")
	}

	targetURL := string(urlParam)

	if len(targetURL) > 2048 {
		return "", fmt.Errorf("URL exceeds maximum length of 2048 characters")
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("only HTTP and HTTPS schemes are supported")
	}

	if parsedURL.Host == "" {
		return "", fmt.Errorf("URL must have a valid host")
	}

	if err := urlutil.ValidateHostNotPrivateIP(parsedURL.Hostname()); err != nil {
		return "", fmt.Errorf("SSRF protection: %w", err)
	}

	return targetURL, nil
}

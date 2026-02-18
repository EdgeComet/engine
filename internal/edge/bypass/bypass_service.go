package bypass

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/urlutil"
)

// BypassResponse holds the fetched content from bypass request
type BypassResponse struct {
	StatusCode  int
	Body        []byte
	ContentType string
	Headers     map[string][]string
}

// BypassService handles direct HTTP proxying when render services are unavailable
type BypassService struct {
	config *config.GlobalBypassConfig
	client *fasthttp.Client
	logger *zap.Logger
}

// NewBypassService creates a new BypassService instance
func NewBypassService(cfg *config.GlobalBypassConfig, logger *zap.Logger) *BypassService {
	// Get timeout value (used for both read and write operations)
	var timeout time.Duration
	if cfg.Timeout != nil {
		timeout = time.Duration(*cfg.Timeout)
	}

	client := &fasthttp.Client{
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	// Enable SSRF protection by default (blocks DNS rebinding to private IPs)
	if cfg.SSRFProtection == nil || *cfg.SSRFProtection {
		client.Dial = ssrfSafeDial
	}

	return &BypassService{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// FetchContent fetches content directly from the target URL without rendering.
// clientHeaders contains safe request headers to forward to the origin.
func (bs *BypassService) FetchContent(targetURL string, clientHeaders map[string][]string, logger *zap.Logger) (*BypassResponse, error) {
	logger.Info("Using bypass mode", zap.String("url", targetURL))

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(targetURL)
	req.Header.SetMethod("GET")
	req.Header.Set("User-Agent", bs.config.UserAgent)

	// Add client request headers (skip User-Agent - always use config value)
	for name, values := range clientHeaders {
		if strings.EqualFold(name, "user-agent") {
			continue
		}
		for i, value := range values {
			if i == 0 {
				req.Header.Set(name, value)
			} else {
				req.Header.Add(name, value)
			}
		}
	}

	if err := bs.client.Do(req, resp); err != nil {
		// Check if error is timeout-related or connection failure
		// All timeout/connection errors should return 502 Bad Gateway
		// This includes read timeouts, write timeouts, dial timeouts, and connection failures
		logger.Warn("Bypass request failed, returning 502 Bad Gateway",
			zap.String("url", targetURL),
			zap.Error(err))

		return &BypassResponse{
			StatusCode:  502,
			Body:        []byte("Bad Gateway: Origin unreachable"),
			ContentType: "text/plain; charset=utf-8",
			Headers:     make(map[string][]string),
		}, nil
	}

	// Extract headers using All iterator to capture all values for multi-value headers
	headers := make(map[string][]string)
	for key, value := range resp.Header.All() {
		k := string(key)
		headers[k] = append(headers[k], string(value))
	}

	// Determine content type
	contentType := string(resp.Header.ContentType())
	if contentType == "" {
		contentType = "text/html; charset=utf-8" // Default content type
	}

	response := &BypassResponse{
		StatusCode:  resp.StatusCode(),
		Body:        append([]byte(nil), resp.Body()...), // Copy the body
		ContentType: contentType,
		Headers:     headers,
	}

	logger.Info("Bypass request completed successfully",
		zap.String("url", targetURL),
		zap.Int("status_code", resp.StatusCode()),
		zap.Int("response_size", len(response.Body)))

	return response, nil
}

// ssrfSafeDial resolves the hostname, validates all IPs are public, then connects.
// Prevents DNS rebinding attacks where an attacker's domain resolves to a private IP.
func ssrfSafeDial(addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for %q", host)
	}

	for _, ip := range ips {
		if err := urlutil.ValidateResolvedIP(ip); err != nil {
			return nil, fmt.Errorf("SSRF protection for %q: %w", host, err)
		}
	}

	// All IPs validated as public; connect to the first one
	return fasthttp.DialTimeout(net.JoinHostPort(ips[0].String(), port), 10*time.Second)
}

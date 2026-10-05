package bypass

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
)

// edgeRenderSource identifies edge-gateway bypass fetches in the X-Edge-Render
// header (render service uses its serviceID; the EG bypass path has no per-tab id).
const edgeRenderSource = "edge-gateway"

// bypassReadBufferSize sizes the fasthttp response read buffer. Origins can emit
// large response headers (big CSP/NEL/Report-To blocks); fasthttp's 4 KB default
// fails to read them and surfaces as a 502. 32 KB covers real-world header bloat.
const bypassReadBufferSize = 32 * 1024

// BypassResponse holds the fetched content from bypass request
type BypassResponse struct {
	StatusCode  int
	Body        []byte
	ContentType string
	Headers     map[string][]string
	// SentHeaders is what the EG set on the outgoing origin request, read back after the header
	// block was built and before the request was sent. Host is absent: fasthttp fills it from
	// the URI while sending, after this capture.
	SentHeaders map[string][]string
	// TransportError is set only when the origin was never reached and StatusCode/Body are the
	// synthetic 502 below. Consumers that must not confuse "origin unreachable" with "origin
	// said 502" (recache classification) check it; the serving path ignores it and keeps
	// returning the synthetic 502 to bots.
	TransportError string
}

// BypassService handles direct HTTP proxying when render services are unavailable
type BypassService struct {
	client *fasthttp.Client
	logger *zap.Logger
}

// NewBypassService creates a new BypassService instance. Only process-wide settings come from the
// global config; the timeout and User-Agent are per request (see FetchContent), so the client
// carries no ReadTimeout/WriteTimeout: fasthttp applies the earlier of a client timeout and the
// request deadline, which would cap a host-level timeout at the global one.
func NewBypassService(cfg *config.GlobalBypassConfig, logger *zap.Logger) *BypassService {
	client := &fasthttp.Client{
		ReadBufferSize: bypassReadBufferSize,
	}

	// Enable SSRF protection by default (blocks DNS rebinding to private IPs)
	if cfg.SSRFProtection == nil || *cfg.SSRFProtection {
		client.Dial = ssrfSafeDial
	}

	return &BypassService{
		client: client,
		logger: logger,
	}
}

// FetchContent fetches content directly from the target URL without rendering.
// bypassCfg is the request's resolved bypass configuration (global -> host -> URL rule); its
// UserAgent is sent to the origin and its Timeout is one deadline for writing the request and
// reading the response. Connecting is bounded by the dialer, not by Timeout: fasthttp hands the
// request deadline only to its built-in dialer, never to a custom Dial such as ssrfSafeDial.
// clientHeaders contains safe request headers to forward to the origin.
// renderKey, when non-empty, is sent as X-Render-Key so the origin can verify the request originated from EdgeComet.
func (bs *BypassService) FetchContent(targetURL string, bypassCfg config.ResolvedBypassConfig, clientHeaders map[string][]string, renderKey string, logger *zap.Logger) (*BypassResponse, error) {
	logger.Info("Using bypass mode", zap.String("url", targetURL))

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(targetURL)
	req.Header.SetMethod("GET")
	req.Header.Set("User-Agent", bypassCfg.UserAgent)
	req.SetTimeout(bypassCfg.Timeout)

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

	// Set engine-managed headers after clientHeaders so they cannot be overridden by forwarded headers.
	// X-Edge-Render marks this fetch as EdgeComet-originated so the integration routes it straight to
	// origin instead of looping it back into the Edge Gateway (which would serve the stale bypass cache).
	req.Header.Set(types.HeaderEdgeRender, edgeRenderSource)
	if renderKey != "" {
		req.Header.Set(types.HeaderRenderKey, renderKey)
	}

	// Read back before Do: the request goes back to the fasthttp pool, and the synthetic 502
	// below must carry what was prepared.
	sentHeaders := httputil.CopyHeaders(req.Header.All())

	if err := bs.client.Do(req, resp); err != nil {
		// Check if error is timeout-related or connection failure
		// All timeout/connection errors should return 502 Bad Gateway
		// This includes read timeouts, write timeouts, dial timeouts, and connection failures
		logger.Warn("Bypass request failed, returning 502 Bad Gateway",
			zap.String("url", targetURL),
			zap.Error(err))

		return &BypassResponse{
			StatusCode:     502,
			Body:           []byte("Bad Gateway: Origin unreachable"),
			ContentType:    "text/plain; charset=utf-8",
			Headers:        make(map[string][]string),
			SentHeaders:    sentHeaders,
			TransportError: err.Error(),
		}, nil
	}

	headers := httputil.CopyHeaders(resp.Header.All())

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
		SentHeaders: sentHeaders,
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

	// Prefer IPv4 address since fasthttp.DialTimeout only supports IPv4
	connectIP := ips[0]
	for _, ip := range ips {
		if ip.To4() != nil {
			connectIP = ip
			break
		}
	}

	return fasthttp.DialTimeout(net.JoinHostPort(connectIP.String(), port), 10*time.Second)
}

package edgectx

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/pkg/types"
)

// RenderContext encapsulates all the request state and dependencies
// needed throughout the render pipeline.
// The timeout fields (startTime, timeout) are immutable after creation,
// making TimeRemaining() safe to call from multiple goroutines.
type RenderContext struct {
	// Request metadata
	RequestID string
	Logger    *zap.Logger

	// HTTP context
	HTTPCtx *fasthttp.RequestCtx

	// Timeout management (immutable after creation)
	startTime time.Time
	timeout   time.Duration

	// Request data
	TargetURL          string
	URLHash            string
	Host               *types.Host
	Dimension          string
	DimensionUnmatched bool // True if User-Agent did not match any dimension and fallback was used
	CacheKey           *types.CacheKey
	LockKey            string
	ResolvedConfig     *config.ResolvedConfig // Resolved configuration for this URL
	ClientHeaders      map[string][]string    // Extracted safe request headers from client
	ClientIP           string

	// Event logging flags
	IsPrecache bool // True if this is a precache/recache request (set by recache handler)
}

// NewRenderContext creates a new render context with the provided request ID, HTTP context, and timeout
func NewRenderContext(requestID string, httpCtx *fasthttp.RequestCtx, baseLogger *zap.Logger, timeout time.Duration) *RenderContext {
	logger := baseLogger.With(zap.Duration("timeout", timeout))

	return &RenderContext{
		RequestID: requestID,
		Logger:    logger,
		HTTPCtx:   httpCtx,
		startTime: time.Now().UTC(),
		timeout:   timeout,
	}
}

// WithTargetURL enriches the context with target URL information (logs as origin_url)
func (rc *RenderContext) WithTargetURL(targetURL string) *RenderContext {
	rc.TargetURL = targetURL
	rc.Logger = rc.Logger.With(zap.String("origin_url", targetURL))
	return rc
}

// WithProcessedURL enriches the context with processed URL information after parameter stripping
func (rc *RenderContext) WithProcessedURL(processedURL string) *RenderContext {
	rc.TargetURL = processedURL
	rc.Logger = rc.Logger.With(zap.String("processed_url", processedURL))
	return rc
}

// WithURLHash enriches the context with URL hash information
func (rc *RenderContext) WithURLHash(urlHash string) *RenderContext {
	rc.URLHash = urlHash
	rc.Logger = rc.Logger.With(zap.String("url_hash", urlHash))
	return rc
}

// WithHost enriches the context with host information
func (rc *RenderContext) WithHost(host *types.Host) *RenderContext {
	rc.Host = host
	rc.Logger = rc.Logger.With(zap.String("host", host.Domain))
	return rc
}

// WithDimension enriches the context with dimension information
func (rc *RenderContext) WithDimension(dimension string) *RenderContext {
	rc.Dimension = dimension
	rc.Logger = rc.Logger.With(zap.String("dimension", dimension))
	return rc
}

// WithCacheKey enriches the context with cache key information
func (rc *RenderContext) WithCacheKey(cacheKey *types.CacheKey) *RenderContext {
	rc.CacheKey = cacheKey
	rc.Logger = rc.Logger.With(zap.String("cache_key", cacheKey.String()))
	return rc
}

// WithLockKey enriches the context with lock key information
func (rc *RenderContext) WithLockKey(lockKey string) *RenderContext {
	rc.LockKey = lockKey
	rc.Logger = rc.Logger.With(zap.String("lock_key", lockKey))
	return rc
}

// WithClientHeaders sets the extracted client request headers.
func (rc *RenderContext) WithClientHeaders(headers map[string][]string) *RenderContext {
	rc.ClientHeaders = headers
	return rc
}

// WithClientIP sets the extracted client IP address.
func (rc *RenderContext) WithClientIP(ip string) *RenderContext {
	rc.ClientIP = ip
	rc.Logger = rc.Logger.With(zap.String("client_ip", ip))
	return rc
}

// TimeRemaining returns how much time is left in the timeout budget.
// This method is safe to call from multiple goroutines since it only
// reads immutable fields.
func (rc *RenderContext) TimeRemaining() time.Duration {
	elapsed := time.Now().UTC().Sub(rc.startTime)
	remaining := rc.timeout - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsTimedOut returns true if the request has exceeded its timeout budget
func (rc *RenderContext) IsTimedOut() bool {
	return rc.TimeRemaining() == 0
}

// GetContext creates a context with the remaining timeout budget
func (rc *RenderContext) GetContext() (context.Context, context.CancelFunc) {
	remaining := rc.TimeRemaining()
	if remaining <= 0 {
		// Already timed out, return cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	return context.WithTimeout(context.Background(), remaining)
}

// ContextWithTimeout creates a context with a specific timeout, capped by the remaining budget
func (rc *RenderContext) ContextWithTimeout(operationTimeout time.Duration) (context.Context, context.CancelFunc) {
	remaining := rc.TimeRemaining()
	if remaining <= 0 {
		// Already timed out, return cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}

	// Use the smaller of the operation timeout or remaining budget
	timeout := operationTimeout
	if remaining < timeout {
		timeout = remaining
	}

	return context.WithTimeout(context.Background(), timeout)
}

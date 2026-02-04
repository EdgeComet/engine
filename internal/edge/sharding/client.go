package sharding

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

// PullRequest represents a request to pull cache from another EG
type PullRequest struct {
	HostID      int    `json:"host_id"`
	DimensionID int    `json:"dimension_id"`
	URLHash     string `json:"url_hash"`
}

// PullResponse represents the response from a pull request
type PullResponse struct {
	Content   []byte    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// PushRequest represents a request to push cache to another EG
type PushRequest struct {
	HostID      int       `json:"host_id"`
	DimensionID int       `json:"dimension_id"`
	URLHash     string    `json:"url_hash"`
	Content     []byte    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	RequestID   string    `json:"request_id"` // Origin request ID for tracing
	FilePath    string    `json:"file_path"`  // Relative file path (includes compression extension)
}

// PushResponse represents the response from a push request
type PushResponse struct {
	Success bool `json:"success"`
}

// StatusResponse represents the response from a status request
type StatusResponse struct {
	EgID                 string   `json:"eg_id"`
	ShardingEnabled      bool     `json:"sharding_enabled"`
	ReplicationFactor    int      `json:"replication_factor"`
	DistributionStrategy string   `json:"distribution_strategy"`
	LocalCacheCount      int      `json:"local_cache_count"`
	LocalCacheSizeBytes  int64    `json:"local_cache_size_bytes"`
	AvailableEGs         []string `json:"available_egs"`
	ClusterSize          int      `json:"cluster_size"`
	UptimeSeconds        int64    `json:"uptime_seconds"`
}

// ShardMetadata represents metadata transferred in X-Shard-Metadata header
type ShardMetadata struct {
	CacheKey  types.CacheKey `json:"k"`  // k: cache key (host_id, dimension_id, url_hash)
	CreatedAt time.Time      `json:"c"`  // c: created timestamp
	ExpiresAt time.Time      `json:"e"`  // e: expires timestamp
	RequestID string         `json:"r"`  // r: request ID for tracing
	EgID      string         `json:"eg"` // eg: edge gateway ID
	FilePath  string         `json:"fp"` // fp: relative file path (includes compression extension)
}

// Client handles inter-EG HTTP communication
type Client interface {
	Pull(ctx context.Context, targetEgID string, req *PullRequest) (*PullResponse, error)
	Push(ctx context.Context, targetEgID string, req *PushRequest) error
	Status(ctx context.Context, targetEgID string) (*StatusResponse, error)
}

// FastHTTPClient implements Client using FastHTTP
type FastHTTPClient struct {
	registry   Registry
	authKey    string
	protocol   string
	timeout    time.Duration
	httpClient *fasthttp.Client
	logger     *zap.Logger
}

// NewFastHTTPClient creates a new FastHTTP-based inter-EG client
func NewFastHTTPClient(registry Registry, authKey string, protocol string, timeout time.Duration, logger *zap.Logger) *FastHTTPClient {
	return &FastHTTPClient{
		registry: registry,
		authKey:  authKey,
		protocol: protocol,
		timeout:  timeout,
		httpClient: &fasthttp.Client{
			ReadTimeout:  timeout,
			WriteTimeout: timeout,

			MaxIdleConnDuration: 500 * time.Millisecond,
		},
		logger: logger,
	}
}

// Pull retrieves cache content from another EG using streaming
func (c *FastHTTPClient) Pull(ctx context.Context, targetEgID string, req *PullRequest) (*PullResponse, error) {
	// Get target EG address from registry
	address, err := c.registry.GetEGAddress(ctx, targetEgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get EG address: %w", err)
	}

	// Build cache key
	cacheKey := fmt.Sprintf("cache:%d:%d:%s", req.HostID, req.DimensionID, req.URLHash)

	// Build URL with query parameter
	url := fmt.Sprintf("%s://%s/internal/cache/pull?cache_key=%s", c.protocol, address, cacheKey)

	// Create FastHTTP request
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("GET")
	httpReq.Header.Set("X-Internal-Auth", c.authKey)

	// Create FastHTTP response
	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	// Execute request with timeout
	if err := c.httpClient.DoTimeout(httpReq, httpResp, c.timeout); err != nil {
		c.logger.Warn("PULL failed",
			zap.String("peer", url),
			zap.String("cache_key", cacheKey),
			zap.Error(err))
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check status code
	statusCode := httpResp.StatusCode()
	if statusCode != fasthttp.StatusOK {
		c.logger.Warn("PULL returned non-200",
			zap.String("peer", url),
			zap.String("cache_key", cacheKey),
			zap.Int("status", statusCode))
		return nil, fmt.Errorf("pull request failed with status %d: %s", statusCode, httpResp.Body())
	}

	// Parse metadata from response header
	metadataJSON := httpResp.Header.Peek("X-Shard-Metadata")
	var metadata ShardMetadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			c.logger.Warn("Failed to parse shard metadata from response",
				zap.Error(err))
			// Continue with default metadata
		}
	}

	// Read HTML content from body (zero-copy)
	htmlContent := httpResp.Body()

	c.logger.Info("PULL completed",
		zap.String("peer", url),
		zap.String("cache_key", cacheKey),
		zap.Int("bytes", len(htmlContent)))

	pullResp := &PullResponse{
		Content:   htmlContent,
		CreatedAt: metadata.CreatedAt,
	}

	return pullResp, nil
}

// Push sends cache content to another EG using streaming
func (c *FastHTTPClient) Push(ctx context.Context, targetEgID string, req *PushRequest) error {
	// Get target EG address from registry
	address, err := c.registry.GetEGAddress(ctx, targetEgID)
	if err != nil {
		return fmt.Errorf("failed to get EG address: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s://%s/internal/cache/push", c.protocol, address)

	// Create metadata for header with CacheKey struct (JSON serialized)
	metadata := ShardMetadata{
		CacheKey: types.CacheKey{
			HostID:      req.HostID,
			DimensionID: req.DimensionID,
			URLHash:     req.URLHash,
		},
		CreatedAt: req.CreatedAt,
		ExpiresAt: req.ExpiresAt,
		RequestID: req.RequestID,
		EgID:      "",
		FilePath:  req.FilePath, // Includes compression extension
	}

	// Marshal metadata to JSON for header
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Create FastHTTP request
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.Set("X-Internal-Auth", c.authKey)
	httpReq.Header.SetContentType("application/octet-stream")
	httpReq.Header.Set("X-Shard-Metadata", string(metadataJSON))

	// Set body directly (zero-copy)
	httpReq.SetBody(req.Content)

	// Create FastHTTP response
	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	// Execute request with timeout
	if err := c.httpClient.DoTimeout(httpReq, httpResp, c.timeout); err != nil {
		c.logger.Error("PUSH failed",
			zap.String("peer", url),
			zap.String("cache_key", metadata.CacheKey.String()),
			zap.Error(err))
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check status code
	statusCode := httpResp.StatusCode()
	if statusCode != fasthttp.StatusOK {
		c.logger.Error("PUSH failed",
			zap.String("peer", url),
			zap.String("cache_key", metadata.CacheKey.String()),
			zap.Int("status", statusCode))
		return fmt.Errorf("push request failed with status %d: %s", statusCode, httpResp.Body())
	}

	c.logger.Info("PUSH completed",
		zap.String("peer", url),
		zap.String("cache_key", metadata.CacheKey.String()),
		zap.Int("bytes", len(req.Content)),
		zap.String("request_id", req.RequestID))

	return nil
}

// Status retrieves status information from another EG
func (c *FastHTTPClient) Status(ctx context.Context, targetEgID string) (*StatusResponse, error) {
	// Get target EG address from registry
	address, err := c.registry.GetEGAddress(ctx, targetEgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get EG address: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s://%s/internal/cache/status", c.protocol, address)

	// Create FastHTTP request
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("GET")
	httpReq.Header.Set("X-Internal-Auth", c.authKey)

	// Create FastHTTP response
	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	// Execute request with timeout
	if err := c.httpClient.DoTimeout(httpReq, httpResp, c.timeout); err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check status code
	statusCode := httpResp.StatusCode()
	if statusCode != fasthttp.StatusOK {
		return nil, fmt.Errorf("status request failed with status %d: %s", statusCode, httpResp.Body())
	}

	// Parse response
	var statusResp StatusResponse
	if err := json.Unmarshal(httpResp.Body(), &statusResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status response: %w", err)
	}

	return &statusResp, nil
}

// PushParallel pushes cache to multiple EGs in parallel
func (c *FastHTTPClient) PushParallel(ctx context.Context, targetEgIDs []string, req *PushRequest) map[string]error {
	results := make(map[string]error)
	resultsCh := make(chan struct {
		egID string
		err  error
	}, len(targetEgIDs))

	// Launch parallel push operations
	for _, egID := range targetEgIDs {
		go func(targetEgID string) {
			err := c.Push(ctx, targetEgID, req)
			resultsCh <- struct {
				egID string
				err  error
			}{targetEgID, err}
		}(egID)
	}

	// Collect results
	for i := 0; i < len(targetEgIDs); i++ {
		result := <-resultsCh
		results[result.egID] = result.err
	}

	return results
}

// rotateSlice rotates a slice by n positions (for hash-based peer selection)
func rotateSlice(slice []string, n int) []string {
	if len(slice) == 0 {
		return slice
	}
	n = n % len(slice)
	return append(slice[n:], slice[:n]...)
}

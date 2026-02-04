package rsclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

// RSClient wraps HTTP client for communicating with render services
type RSClient struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewRSClient creates a new render service HTTP client
func NewRSClient(logger *zap.Logger) *RSClient {
	return &RSClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Default timeout, overridden by context
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger,
	}
}

// CallRenderService sends a render request to the render service and returns the response
func (rc *RSClient) CallRenderService(
	ctx context.Context,
	serviceURL string,
	request *types.RenderRequest,
) (*types.RenderResponse, error) {
	if serviceURL == "" {
		return nil, fmt.Errorf("service URL is empty")
	}
	if request == nil {
		return nil, fmt.Errorf("render request is nil")
	}

	// Build render endpoint URL
	renderURL := serviceURL + "/render"

	// Marshal request to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		rc.logger.Error("Failed to marshal render request",
			zap.String("service_url", serviceURL),
			zap.Error(err))
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request with context
	httpReq, err := http.NewRequestWithContext(ctx, "POST", renderURL, bytes.NewReader(requestBody))
	if err != nil {
		rc.logger.Error("Failed to create HTTP request",
			zap.String("render_url", renderURL),
			zap.Error(err))
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/octet-stream, application/json")
	httpReq.Header.Set("X-Request-ID", request.RequestID)

	rc.logger.Debug("Sending render request to service",
		zap.String("service_url", serviceURL),
		zap.String("request_id", request.RequestID),
		zap.String("url", request.URL))

	// Execute HTTP request
	startTime := time.Now().UTC()
	httpResp, err := rc.httpClient.Do(httpReq)
	if err != nil {
		duration := time.Since(startTime)
		rc.logger.Error("HTTP request to render service failed",
			zap.String("service_url", serviceURL),
			zap.String("request_id", request.RequestID),
			zap.Duration("duration", duration),
			zap.Error(err))
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	duration := time.Since(startTime)

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		rc.logger.Error("Failed to read response body",
			zap.String("service_url", serviceURL),
			zap.String("request_id", request.RequestID),
			zap.Int("status_code", httpResp.StatusCode),
			zap.Error(err))
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status code
	if httpResp.StatusCode != http.StatusOK {
		rc.logger.Warn("Render service returned non-200 status",
			zap.String("service_url", serviceURL),
			zap.String("request_id", request.RequestID),
			zap.Int("status_code", httpResp.StatusCode),
			zap.String("response", string(respBody)))
		return nil, fmt.Errorf("render service returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Check content type to determine response format
	contentType := httpResp.Header.Get("Content-Type")

	var response types.RenderResponse

	if contentType == "application/octet-stream" {
		// Binary format: [4 bytes length][JSON metadata][raw HTML]

		// Read length prefix
		if len(respBody) < 4 {
			return nil, fmt.Errorf("response too short for binary format: %d bytes", len(respBody))
		}

		metadataLen := binary.BigEndian.Uint32(respBody[0:4])

		// Validate metadata length
		if 4+int(metadataLen) > len(respBody) {
			return nil, fmt.Errorf("invalid metadata length: %d (total body: %d)", metadataLen, len(respBody))
		}

		// Parse metadata JSON
		metadataJSON := respBody[4 : 4+metadataLen]
		var metadata types.RenderResponseMetadata
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			rc.logger.Error("Failed to unmarshal render metadata",
				zap.String("service_url", serviceURL),
				zap.String("request_id", request.RequestID),
				zap.Error(err))
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		// Extract raw HTML
		htmlBytes := respBody[4+metadataLen:]

		// Reconstruct full response
		response = types.RenderResponse{
			RequestID:  metadata.RequestID,
			Success:    metadata.Success,
			HTML:       string(htmlBytes),
			Error:      metadata.Error,
			RenderTime: metadata.RenderTime,
			HTMLSize:   metadata.HTMLSize,
			Timestamp:  metadata.Timestamp,
			ChromeID:   metadata.ChromeID,
			Metrics:    metadata.Metrics,
			Headers:    metadata.Headers,
			HAR:        metadata.HAR,
			PageSEO:    metadata.PageSEO,
		}

		rc.logger.Debug("Parsed binary response",
			zap.String("request_id", request.RequestID),
			zap.Int("metadata_bytes", int(metadataLen)),
			zap.Int("html_bytes", len(htmlBytes)))

	} else {
		// Legacy JSON format (for errors or old RS versions)
		if err := json.Unmarshal(respBody, &response); err != nil {
			rc.logger.Error("Failed to unmarshal render response",
				zap.String("service_url", serviceURL),
				zap.String("request_id", request.RequestID),
				zap.String("response_preview", string(respBody[:min(200, len(respBody))])),
				zap.Error(err))
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	rc.logger.Debug("Render request completed",
		zap.String("service_url", serviceURL),
		zap.String("request_id", request.RequestID),
		zap.Bool("success", response.Success),
		zap.Int("html_size", response.HTMLSize),
		zap.Duration("duration", duration),
		zap.Duration("render_time", response.RenderTime))

	return &response, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

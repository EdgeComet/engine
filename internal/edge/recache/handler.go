package recache

import (
	"encoding/json"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/edge/internal_server"
)

// RecacheRequest represents a request to recache a URL
type RecacheRequest struct {
	URL         string `json:"url"`
	HostID      int    `json:"host_id"`
	DimensionID int    `json:"dimension_id"`
}

// RecacheResponse represents the response from a recache request
type RecacheResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// RegisterEndpoints registers the recache handler with the internal server
func (rs *RecacheService) RegisterEndpoints(server *internal_server.InternalServer) {
	server.RegisterHandler("POST", internal_server.PathCacheRecache, rs.handleRecache)
}

// handleRecache processes recache requests from the cache daemon
func (rs *RecacheService) handleRecache(ctx *fasthttp.RequestCtx) {
	var req RecacheRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		rs.logger.Warn("Invalid request body", zap.Error(err))
		httputil.JSONError(ctx, "invalid request body", fasthttp.StatusBadRequest)
		return
	}

	if req.URL == "" || req.HostID == 0 || req.DimensionID == 0 {
		rs.logger.Warn("Missing required fields",
			zap.String("url", req.URL),
			zap.Int("host_id", req.HostID),
			zap.Int("dimension_id", req.DimensionID))
		httputil.JSONError(ctx, "missing required fields", fasthttp.StatusBadRequest)
		return
	}

	rs.logger.Info("Processing recache request",
		zap.String("url", req.URL),
		zap.Int("host_id", req.HostID),
		zap.Int("dimension_id", req.DimensionID))

	if err := rs.ProcessRecache(ctx, req.URL, req.HostID, req.DimensionID); err != nil {
		rs.logger.Error("Recache request failed", zap.Error(err))
		httputil.JSONError(ctx, err.Error(), fasthttp.StatusInternalServerError)
		return
	}

	rs.logger.Info("Recache request completed successfully",
		zap.String("url", req.URL),
		zap.Int("host_id", req.HostID),
		zap.Int("dimension_id", req.DimensionID))

	httputil.JSONSuccess(ctx, "", fasthttp.StatusOK)
}

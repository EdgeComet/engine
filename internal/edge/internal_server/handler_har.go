// TODO: Move this handler to a dedicated debug service package when implementing debug service refactoring

package internal_server

import (
	"context"
	"strings"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
)

// HARStore defines the interface for HAR data operations
type HARStore interface {
	GetHAR(ctx context.Context, hostID, requestID string) ([]byte, error)
}

// HARHandler handles HAR retrieval requests
type HARHandler struct {
	harStore HARStore
	logger   *zap.Logger
}

// NewHARHandler creates a new HAR handler
func NewHARHandler(harStore HARStore, logger *zap.Logger) *HARHandler {
	return &HARHandler{
		harStore: harStore,
		logger:   logger,
	}
}

// RegisterEndpoints registers the HAR handler with the internal server
func (h *HARHandler) RegisterEndpoints(server *InternalServer) {
	server.RegisterHandler("GET", PathDebugHAR, h.handleHAR)
}

const (
	harPathPrefix = "/debug/har/"
)

// handleHAR handles HAR retrieval requests
// GET /debug/har/{hostID}/{requestID}
func (h *HARHandler) handleHAR(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	pathSuffix := strings.TrimPrefix(path, harPathPrefix)
	parts := strings.Split(pathSuffix, "/")

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		h.logger.Warn("Invalid HAR path",
			zap.String("path", path))
		httputil.JSONError(ctx, "invalid path, expected /debug/har/{hostID}/{requestID}", fasthttp.StatusBadRequest)
		return
	}

	hostID := parts[0]
	requestID := parts[1]

	h.logger.Debug("Processing HAR retrieval request",
		zap.String("host_id", hostID),
		zap.String("request_id", requestID))

	harData, err := h.harStore.GetHAR(ctx, hostID, requestID)
	if err != nil {
		h.logger.Error("Failed to retrieve HAR",
			zap.String("host_id", hostID),
			zap.String("request_id", requestID),
			zap.Error(err))
		httputil.JSONError(ctx, "internal server error", fasthttp.StatusInternalServerError)
		return
	}

	if harData == nil {
		h.logger.Debug("HAR not found",
			zap.String("host_id", hostID),
			zap.String("request_id", requestID))
		httputil.JSONError(ctx, "HAR not found", fasthttp.StatusNotFound)
		return
	}

	ctx.Response.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.SetContentType("application/json")
	ctx.Response.Header.Set("Content-Encoding", "gzip")
	ctx.Response.SetBody(harData)

	h.logger.Debug("HAR retrieval successful",
		zap.String("host_id", hostID),
		zap.String("request_id", requestID),
		zap.Int("size", len(harData)))
}

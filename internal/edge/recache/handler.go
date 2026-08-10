package recache

import (
	"encoding/json"
	"errors"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/edge/internal_server"
	"github.com/edgecomet/engine/pkg/types"
)

// RecacheRequest represents a request to recache a URL
type RecacheRequest struct {
	URL         string `json:"url"`
	HostID      int    `json:"host_id"`
	DimensionID int    `json:"dimension_id"`
	Mode        string `json:"mode,omitempty"` // Optional action override: render | bypass (empty = respect config)
}

// RegisterEndpoints registers the recache handler with the internal server
func (rs *RecacheService) RegisterEndpoints(server *internal_server.InternalServer) {
	server.RegisterHandler("POST", internal_server.PathCacheRecache, rs.handleRecache)
}

// handleRecache processes recache requests from the cache daemon.
// The response is the daemon's retry instruction: 200 for a terminal-ok outcome (cached, or
// declined by configuration), 422 for a failure no retry can resolve, 500 for one worth another
// attempt. data.outcome names the outcome so the daemon never has to parse an error string.
func (rs *RecacheService) handleRecache(ctx *fasthttp.RequestCtx) {
	var req RecacheRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
		rs.logger.Warn("Invalid request body", zap.Error(err))
		httputil.JSONError(ctx, "invalid request body", fasthttp.StatusBadRequest)
		return
	}

	if req.URL == "" || req.HostID == 0 {
		rs.logger.Warn("Missing required fields",
			zap.String("url", req.URL),
			zap.Int("host_id", req.HostID),
			zap.Int("dimension_id", req.DimensionID))
		httputil.JSONError(ctx, "missing required fields", fasthttp.StatusBadRequest)
		return
	}

	// Logged before dispatch so a request rejected before its render context exists (unknown
	// host, unknown dimension, SSRF, domain mismatch) still leaves a record of the attempt.
	rs.logger.Info("Processing recache request",
		zap.String("url", req.URL),
		zap.Int("host_id", req.HostID),
		zap.Int("dimension_id", req.DimensionID))

	if err := rs.ProcessRecache(ctx, req.URL, req.HostID, req.DimensionID, req.Mode); err != nil {
		if errors.Is(err, ErrRecacheSkipped) {
			rs.logger.Info("Recache skipped by configuration",
				zap.String("url", req.URL),
				zap.Int("host_id", req.HostID),
				zap.Int("dimension_id", req.DimensionID),
				zap.Error(err))
			httputil.JSONData(ctx, types.RecacheOutcomeData{
				Outcome: types.RecacheOutcomeSkipped,
				Reason:  err.Error(),
			}, fasthttp.StatusOK)
			return
		}

		rs.respondRecacheFailure(ctx, req, err)
		return
	}

	rs.logger.Info("Recache request completed successfully",
		zap.String("url", req.URL),
		zap.Int("host_id", req.HostID),
		zap.Int("dimension_id", req.DimensionID))

	httputil.JSONData(ctx, types.RecacheOutcomeData{Outcome: types.RecacheOutcomeCached}, fasthttp.StatusOK)
}

// respondRecacheFailure answers a failed recache with its retry instruction and logs it at the
// level its class earns: origin and capacity failures burst and are not ours to fix, so only
// edge-gateway-side faults reach error tracking.
func (rs *RecacheService) respondRecacheFailure(ctx *fasthttp.RequestCtx, req RecacheRequest, err error) {
	// Non-nil and not a configuration decline - the caller already checked - so a classification
	// always exists here.
	failure := classifiedFailure(err)

	fields := []zap.Field{
		zap.String("url", req.URL),
		zap.Int("host_id", req.HostID),
		zap.Int("dimension_id", req.DimensionID),
		zap.String("error_type", failure.errorType),
		zap.Int("status_code", failure.statusCode),
		zap.Bool("permanent", failure.permanent),
		zap.Error(err),
	}
	if failure.logAtError() {
		rs.logger.Error("Recache request failed", fields...)
	} else {
		rs.logger.Warn("Recache request failed", fields...)
	}

	statusCode := fasthttp.StatusInternalServerError
	if failure.permanent {
		statusCode = fasthttp.StatusUnprocessableEntity
	}

	// JSONData is not usable here: it hardcodes success:true, which would contradict the
	// failed outcome it carries.
	httputil.JSONResponse(ctx, false, failure.Error(), types.RecacheOutcomeData{
		Outcome:   types.RecacheOutcomeFailed,
		ErrorType: failure.errorType,
		Permanent: failure.permanent,
	}, statusCode)
}

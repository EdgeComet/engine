package service

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/render/chrome"
	"github.com/edgecomet/engine/internal/render/metrics"
	"github.com/edgecomet/engine/internal/render/registry"
)

// CreateHTTPHandler creates the main HTTP request handler with routing
func CreateHTTPHandler(pool *chrome.ChromePool, tabManager *registry.TabManager, metricsCollector *metrics.MetricsCollector, renderConfig *config.RSRenderConfig, logger *zap.Logger) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())
		method := string(ctx.Method())

		switch {
		case method == "POST" && path == "/render":
			HandleRender(ctx, pool, tabManager, metricsCollector, logger, renderConfig)
		case method == "GET" && path == "/health":
			HandleHealth(ctx, pool, metricsCollector, logger)
		default:
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			ctx.SetBodyString("Not Found")
			metricsCollector.RecordHTTPRequest(path, "404")
		}
	}
}

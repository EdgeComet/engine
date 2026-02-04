package metricsserver

import (
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// MetricsHandler interface for metrics collectors
type MetricsHandler interface {
	ServeHTTP(ctx *fasthttp.RequestCtx)
}

// StartMetricsServer creates and starts a separate metrics HTTP server.
// Returns nil if metrics are disabled.
// Returns *fasthttp.Server if metrics server was created and started.
// Metrics always run on a separate port (validated at config load time).
func StartMetricsServer(
	enabled bool,
	metricsListen string,
	metricsPath string,
	metricsHandler MetricsHandler,
	logger *zap.Logger,
) (*fasthttp.Server, error) {
	if !enabled {
		logger.Info("Metrics collection disabled")
		return nil, nil
	}

	logger.Debug("Starting metrics server",
		zap.String("listen", metricsListen),
		zap.String("path", metricsPath))

	handler := createMetricsHandler(metricsPath, metricsHandler, logger)

	metricsServer := &fasthttp.Server{
		Handler:            handler,
		Name:               "EdgeComet-Metrics",
		ReadTimeout:        10 * time.Second,
		WriteTimeout:       10 * time.Second,
		MaxRequestBodySize: 1 * 1024,
		DisableKeepalive:   false,
		TCPKeepalive:       true,
		TCPKeepalivePeriod: 30 * time.Second,
		MaxConnsPerIP:      100,
		MaxRequestsPerConn: 1000,
		Concurrency:        100,
	}

	go func() {
		logger.Info("Metrics server listening",
			zap.String("listen", metricsListen),
			zap.String("path", metricsPath))

		if err := metricsServer.ListenAndServe(metricsListen); err != nil {
			logger.Error("Metrics server stopped",
				zap.String("listen", metricsListen),
				zap.Error(err))
		}
	}()

	time.Sleep(100 * time.Millisecond)

	return metricsServer, nil
}

// createMetricsHandler creates a FastHTTP request handler for the metrics server
func createMetricsHandler(
	metricsPath string,
	metricsCollector MetricsHandler,
	logger *zap.Logger,
) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		if string(ctx.Path()) == metricsPath {
			metricsCollector.ServeHTTP(ctx)
			return
		}

		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not Found")
	}
}

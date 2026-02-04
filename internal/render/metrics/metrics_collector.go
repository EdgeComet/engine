package metrics

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// MetricsCollector centralizes all metrics recording for Render Service
type MetricsCollector struct {
	prometheus *PrometheusMetrics
	logger     *zap.Logger
}

// NewMetricsCollector creates a new MetricsCollector instance
func NewMetricsCollector(namespace string, logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		prometheus: NewPrometheusMetrics(namespace, logger),
		logger:     logger,
	}
}

// UpdateChromePoolSize updates the Chrome pool size metric
func (mc *MetricsCollector) UpdateChromePoolSize(size int) {
	mc.prometheus.UpdateChromePoolSize(float64(size))
}

// UpdateChromeAvailable updates the available Chrome instances metric
func (mc *MetricsCollector) UpdateChromeAvailable(available int) {
	mc.prometheus.UpdateChromeAvailable(float64(available))
}

// RecordRenderSuccess records a successful render
func (mc *MetricsCollector) RecordRenderSuccess() {
	mc.prometheus.RecordRender("success")
}

// RecordRenderError records a render error
func (mc *MetricsCollector) RecordRenderError() {
	mc.prometheus.RecordRender("error")
}

// RecordRenderTimeout records a render soft timeout (navigation wait exceeded, but HTML returned)
func (mc *MetricsCollector) RecordRenderTimeout() {
	mc.prometheus.RecordRender("timeout")
}

// RecordRenderHardTimeout records a render hard timeout (cancelled before completion)
func (mc *MetricsCollector) RecordRenderHardTimeout() {
	mc.prometheus.RecordRender("hard_timeout")
}

// RecordRenderQueueFull records a queue full rejection
func (mc *MetricsCollector) RecordRenderQueueFull() {
	mc.prometheus.RecordRender("queue_full")
}

// RecordRenderDuration records render duration in seconds
func (mc *MetricsCollector) RecordRenderDuration(seconds float64) {
	mc.prometheus.RecordRenderDuration(seconds)
}

// UpdateQueueDepth updates the current queue depth
func (mc *MetricsCollector) UpdateQueueDepth(depth int) {
	mc.prometheus.UpdateQueueDepth(float64(depth))
}

// RecordQueueRejection records a queue rejection
func (mc *MetricsCollector) RecordQueueRejection() {
	mc.prometheus.RecordQueueRejection()
	mc.logger.Debug("Recorded queue rejection")
}

// RecordHTTPRequest records an HTTP request
func (mc *MetricsCollector) RecordHTTPRequest(endpoint, status string) {
	mc.prometheus.RecordHTTPRequest(endpoint, status)
}

// RecordValidationError records a validation error
func (mc *MetricsCollector) RecordValidationError() {
	mc.prometheus.RecordError("validation")
}

// RecordRenderErrorMetric records a render error metric
func (mc *MetricsCollector) RecordRenderErrorMetric() {
	mc.prometheus.RecordError("render")
}

// RecordTimeoutError records a timeout error
func (mc *MetricsCollector) RecordTimeoutError() {
	mc.prometheus.RecordError("timeout")
}

// RecordInternalError records an internal error
func (mc *MetricsCollector) RecordInternalError() {
	mc.prometheus.RecordError("internal")
}

// ServeHTTP serves Prometheus metrics via HTTP
func (mc *MetricsCollector) ServeHTTP(ctx *fasthttp.RequestCtx) {
	mc.prometheus.ServeHTTP(ctx)
}

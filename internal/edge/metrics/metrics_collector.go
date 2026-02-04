package metrics

import (
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// MetricsCollector centralizes all metrics recording with proper labeling
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

// RecordRequest records a request with timing
func (mc *MetricsCollector) RecordRequest(host, dimension, status string, duration time.Duration) {
	mc.prometheus.RecordRequest(host, dimension, status, duration)

	mc.logger.Debug("Recorded request metric",
		zap.String("host", host),
		zap.String("dimension", dimension),
		zap.String("status", status),
		zap.Duration("duration", duration))
}

// RecordCacheHit records a successful cache hit
func (mc *MetricsCollector) RecordCacheHit(host, dimension string) {
	mc.prometheus.RecordCacheHit(host, dimension)

	mc.logger.Debug("Recorded cache hit metric",
		zap.String("host", host),
		zap.String("dimension", dimension))
}

// RecordCacheMiss records a cache miss
func (mc *MetricsCollector) RecordCacheMiss(host, dimension string) {
	mc.prometheus.RecordCacheMiss(host, dimension)

	mc.logger.Debug("Recorded cache miss metric",
		zap.String("host", host),
		zap.String("dimension", dimension))
}

// RecordRenderDuration records how long a render service call took
func (mc *MetricsCollector) RecordRenderDuration(host, dimension, serviceID string, duration time.Duration) {
	mc.prometheus.RecordRenderDuration(host, dimension, serviceID, duration)

	mc.logger.Debug("Recorded render duration metric",
		zap.String("host", host),
		zap.String("dimension", dimension),
		zap.String("service_id", serviceID),
		zap.Duration("duration", duration))
}

// RecordStatusCodeResponse records a response by status code
func (mc *MetricsCollector) RecordStatusCodeResponse(host, dimension string, statusCode int) {
	mc.prometheus.RecordStatusCodeResponse(host, dimension, statusCode)

	mc.logger.Debug("Recorded status code response metric",
		zap.String("host", host),
		zap.String("dimension", dimension),
		zap.Int("status_code", statusCode))
}

// RecordBypass records when bypass mode is used
func (mc *MetricsCollector) RecordBypass(host, reason string) {
	mc.prometheus.RecordBypass(host, reason)

	mc.logger.Debug("Recorded bypass metric",
		zap.String("host", host),
		zap.String("reason", reason))
}

// RecordError records an error by type
func (mc *MetricsCollector) RecordError(errorType, host string) {
	mc.prometheus.RecordError(errorType, host)

	mc.logger.Debug("Recorded error metric",
		zap.String("error_type", errorType),
		zap.String("host", host))
}

// IncActiveRequests increments active request counter
func (mc *MetricsCollector) IncActiveRequests() {
	mc.prometheus.IncActiveRequests()
}

// DecActiveRequests decrements active request counter
func (mc *MetricsCollector) DecActiveRequests() {
	mc.prometheus.DecActiveRequests()
}

// RecordWaitSuccess records a successful wait for concurrent render
func (mc *MetricsCollector) RecordWaitSuccess(host, dimension string, duration time.Duration) {
	mc.prometheus.RecordWaitSuccess(host, dimension, duration)

	mc.logger.Debug("Recorded wait success metric",
		zap.String("host", host),
		zap.String("dimension", dimension),
		zap.Duration("duration", duration))
}

// RecordWaitTimeout records a timeout while waiting for concurrent render
func (mc *MetricsCollector) RecordWaitTimeout(host, dimension string, duration time.Duration) {
	mc.prometheus.RecordWaitTimeout(host, dimension, duration)

	mc.logger.Debug("Recorded wait timeout metric",
		zap.String("host", host),
		zap.String("dimension", dimension),
		zap.Duration("duration", duration))
}

// ServeHTTP serves Prometheus metrics via HTTP
func (mc *MetricsCollector) ServeHTTP(ctx *fasthttp.RequestCtx) {
	mc.prometheus.ServeHTTP(ctx)
}

// RecordStaleServed records that a stale cache was served
func (mc *MetricsCollector) RecordStaleServed(host, dimension string) {
	mc.prometheus.RecordStaleServed(host, dimension)

	mc.logger.Debug("Recorded stale cache served metric",
		zap.String("host", host),
		zap.String("dimension", dimension))
}

// RecordCompression records compression metrics (ratio and bytes saved)
func (mc *MetricsCollector) RecordCompression(algorithm string, originalSize, compressedSize int) {
	if originalSize <= 0 {
		return
	}

	ratio := float64(compressedSize) / float64(originalSize)
	mc.prometheus.RecordCompressionRatio(algorithm, ratio)

	bytesSaved := int64(originalSize - compressedSize)
	mc.prometheus.RecordBytesSaved(algorithm, bytesSaved)

	mc.logger.Debug("Recorded compression metric",
		zap.String("algorithm", algorithm),
		zap.Int("original_size", originalSize),
		zap.Int("compressed_size", compressedSize),
		zap.Float64("ratio", ratio),
		zap.Int64("bytes_saved", bytesSaved))
}

// RecordDecompressionError records a decompression failure
func (mc *MetricsCollector) RecordDecompressionError(algorithm string) {
	mc.prometheus.RecordDecompressionError(algorithm)

	mc.logger.Debug("Recorded decompression error metric",
		zap.String("algorithm", algorithm))
}

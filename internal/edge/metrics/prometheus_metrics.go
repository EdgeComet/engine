package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"go.uber.org/zap"
)

// PrometheusMetrics provides high-performance metrics collection using Prometheus
type PrometheusMetrics struct {
	// Request metrics
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec

	// Cache metrics
	cacheHitsTotal        *prometheus.CounterVec
	cacheMissesTotal      *prometheus.CounterVec
	cacheHitRatio         *prometheus.GaugeVec
	staleCacheServedTotal *prometheus.CounterVec

	// Service metrics
	renderDuration       *prometheus.HistogramVec
	renderStatusCodeResp *prometheus.CounterVec
	bypassTotal          *prometheus.CounterVec
	activeRequests       prometheus.Gauge

	// Wait metrics (for concurrent render coordination)
	waitTotal    *prometheus.CounterVec
	waitDuration *prometheus.HistogramVec
	waitTimeouts *prometheus.CounterVec

	// System metrics
	cacheSize prometheus.Gauge
	errorRate *prometheus.CounterVec

	// Compression metrics
	cacheCompressionRatio        *prometheus.HistogramVec
	cacheBytesSavedTotal         *prometheus.CounterVec
	cacheDecompressionErrorTotal *prometheus.CounterVec

	logger      *zap.Logger
	httpHandler func(*fasthttp.RequestCtx)
}

// NewPrometheusMetrics creates a new Prometheus-based metrics collector
func NewPrometheusMetrics(namespace string, logger *zap.Logger) *PrometheusMetrics {
	return NewPrometheusMetricsWithRegistry(namespace, prometheus.DefaultRegisterer, logger)
}

// NewPrometheusMetricsWithRegistry creates a new Prometheus-based metrics collector with custom registry
func NewPrometheusMetricsWithRegistry(namespace string, registerer prometheus.Registerer, logger *zap.Logger) *PrometheusMetrics {
	pm := &PrometheusMetrics{
		logger: logger,
	}

	// Request metrics
	pm.requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "requests_total",
			Help:      "Total number of render requests processed",
		},
		[]string{"host", "dimension", "status"},
	)

	pm.requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "request_duration_seconds",
			Help:      "Time taken to process render requests",
			Buckets:   prometheus.DefBuckets, // Standard buckets: 0.005s to 10s
		},
		[]string{"host", "dimension", "status"},
	)

	// Cache metrics
	pm.cacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
		[]string{"host", "dimension"},
	)

	pm.cacheMissesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
		[]string{"host", "dimension"},
	)

	pm.cacheHitRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "cache_hit_ratio",
			Help:      "Cache hit ratio (0-1) for each host/dimension",
		},
		[]string{"host", "dimension"},
	)

	pm.staleCacheServedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "stale_cache_served_total",
			Help:      "Total number of stale cache entries served",
		},
		[]string{"host", "dimension"},
	)

	// Service metrics
	pm.renderDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "render_service_duration_seconds",
			Help:      "Time taken by render service to process requests",
			Buckets:   []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0}, // Render-specific buckets
		},
		[]string{"host", "dimension", "service_id"},
	)

	pm.renderStatusCodeResp = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "status_code_responses_total",
			Help:      "Total number of rendered responses by status code range",
		},
		[]string{"host", "dimension", "status_range"}, // status_range: 2xx, 3xx, 4xx, 5xx
	)

	pm.bypassTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "bypass_total",
			Help:      "Total number of requests that used bypass mode",
		},
		[]string{"host", "reason"},
	)

	pm.activeRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "active_requests",
			Help:      "Number of currently active render requests",
		},
	)

	// Wait metrics (for concurrent render coordination)
	pm.waitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "wait_total",
			Help:      "Total number of requests that waited for concurrent renders",
		},
		[]string{"host", "dimension", "outcome"}, // outcome: success, timeout
	)

	pm.waitDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "wait_duration_seconds",
			Help:      "Time spent waiting for concurrent renders to complete",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0}, // Wait-specific buckets
		},
		[]string{"host", "dimension", "outcome"},
	)

	pm.waitTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "wait_timeouts_total",
			Help:      "Total number of wait timeouts while waiting for concurrent renders",
		},
		[]string{"host", "dimension"},
	)

	// System metrics
	pm.cacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "cache_size_bytes",
			Help:      "Total size of cached content in bytes",
		},
	)

	pm.errorRate = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "errors_total",
			Help:      "Total number of errors by type",
		},
		[]string{"error_type", "host"},
	)

	// Compression metrics
	pm.cacheCompressionRatio = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "compression_ratio",
			Help:      "Compression ratio (compressed_size / original_size)",
			Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		},
		[]string{"algorithm"},
	)

	pm.cacheBytesSavedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "bytes_saved_total",
			Help:      "Total bytes saved by compression",
		},
		[]string{"algorithm"},
	)

	pm.cacheDecompressionErrorTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "decompression_errors_total",
			Help:      "Total decompression failures (triggers re-render)",
		},
		[]string{"algorithm"},
	)

	// Register all metrics
	registerer.MustRegister(
		pm.requestsTotal,
		pm.requestDuration,
		pm.cacheHitsTotal,
		pm.cacheMissesTotal,
		pm.cacheHitRatio,
		pm.staleCacheServedTotal,
		pm.renderDuration,
		pm.renderStatusCodeResp,
		pm.bypassTotal,
		pm.activeRequests,
		pm.waitTotal,
		pm.waitDuration,
		pm.waitTimeouts,
		pm.cacheSize,
		pm.errorRate,
		pm.cacheCompressionRatio,
		pm.cacheBytesSavedTotal,
		pm.cacheDecompressionErrorTotal,
	)

	// Create HTTP handler - registerer implements Gatherer interface
	gatherer, ok := registerer.(prometheus.Gatherer)
	if !ok {
		// Fallback to default gatherer if registerer doesn't implement Gatherer
		gatherer = prometheus.DefaultGatherer
	}
	pm.httpHandler = fasthttpadaptor.NewFastHTTPHandler(promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))

	logger.Debug("Prometheus metrics initialized")
	return pm
}

// RecordRequest records a request with timing
func (pm *PrometheusMetrics) RecordRequest(host, dimension, status string, duration time.Duration) {
	pm.requestsTotal.WithLabelValues(host, dimension, status).Inc()
	pm.requestDuration.WithLabelValues(host, dimension, status).Observe(duration.Seconds())
}

// RecordCacheHit records a cache hit and updates hit ratio
func (pm *PrometheusMetrics) RecordCacheHit(host, dimension string) {
	pm.cacheHitsTotal.WithLabelValues(host, dimension).Inc()
	pm.updateCacheHitRatio(host, dimension)
}

// RecordCacheMiss records a cache miss and updates hit ratio
func (pm *PrometheusMetrics) RecordCacheMiss(host, dimension string) {
	pm.cacheMissesTotal.WithLabelValues(host, dimension).Inc()
	pm.updateCacheHitRatio(host, dimension)
}

// RecordStaleServed records that a stale cache entry was served
func (pm *PrometheusMetrics) RecordStaleServed(host, dimension string) {
	pm.staleCacheServedTotal.WithLabelValues(host, dimension).Inc()
}

// RecordRenderDuration records time taken by render service
func (pm *PrometheusMetrics) RecordRenderDuration(host, dimension, serviceID string, duration time.Duration) {
	pm.renderDuration.WithLabelValues(host, dimension, serviceID).Observe(duration.Seconds())
}

// RecordStatusCodeResponse records a response by status code range
func (pm *PrometheusMetrics) RecordStatusCodeResponse(host, dimension string, statusCode int) {
	statusRange := getStatusCodeRange(statusCode)
	pm.renderStatusCodeResp.WithLabelValues(host, dimension, statusRange).Inc()
}

// getStatusCodeRange converts a status code to a range label (2xx, 3xx, 4xx, 5xx)
func getStatusCodeRange(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500 && statusCode < 600:
		return "5xx"
	default:
		return "unknown"
	}
}

// RecordBypass records when bypass mode is used
func (pm *PrometheusMetrics) RecordBypass(host, reason string) {
	pm.bypassTotal.WithLabelValues(host, reason).Inc()
}

// RecordError records an error by type
func (pm *PrometheusMetrics) RecordError(errorType, host string) {
	pm.errorRate.WithLabelValues(errorType, host).Inc()
}

// IncActiveRequests increments active request counter
func (pm *PrometheusMetrics) IncActiveRequests() {
	pm.activeRequests.Inc()
}

// DecActiveRequests decrements active request counter
func (pm *PrometheusMetrics) DecActiveRequests() {
	pm.activeRequests.Dec()
}

// UpdateCacheSize updates the total cache size metric
func (pm *PrometheusMetrics) UpdateCacheSize(sizeBytes float64) {
	pm.cacheSize.Set(sizeBytes)
}

// RecordWaitSuccess records a successful wait for concurrent render
func (pm *PrometheusMetrics) RecordWaitSuccess(host, dimension string, duration time.Duration) {
	pm.waitTotal.WithLabelValues(host, dimension, "success").Inc()
	pm.waitDuration.WithLabelValues(host, dimension, "success").Observe(duration.Seconds())
}

// RecordWaitTimeout records a timeout while waiting for concurrent render
func (pm *PrometheusMetrics) RecordWaitTimeout(host, dimension string, duration time.Duration) {
	pm.waitTotal.WithLabelValues(host, dimension, "timeout").Inc()
	pm.waitDuration.WithLabelValues(host, dimension, "timeout").Observe(duration.Seconds())
	pm.waitTimeouts.WithLabelValues(host, dimension).Inc()
}

// ServeHTTP serves Prometheus metrics via HTTP
func (pm *PrometheusMetrics) ServeHTTP(ctx *fasthttp.RequestCtx) {
	pm.httpHandler(ctx)
}

// updateCacheHitRatio calculates and updates cache hit ratio
func (pm *PrometheusMetrics) updateCacheHitRatio(host, dimension string) {
	// Get current values
	hits := pm.getCounterValue(pm.cacheHitsTotal.WithLabelValues(host, dimension))
	misses := pm.getCounterValue(pm.cacheMissesTotal.WithLabelValues(host, dimension))

	total := hits + misses
	if total > 0 {
		ratio := hits / total
		pm.cacheHitRatio.WithLabelValues(host, dimension).Set(ratio)
	}
}

// getCounterValue extracts current value from a counter (helper function)
func (pm *PrometheusMetrics) getCounterValue(counter prometheus.Counter) float64 {
	// Use a metric DTO to read the current value
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		pm.logger.Warn("Failed to read counter value", zap.Error(err))
		return 0
	}
	return metric.GetCounter().GetValue()
}

// RecordCompressionRatio records the compression ratio for a cached file
func (pm *PrometheusMetrics) RecordCompressionRatio(algorithm string, ratio float64) {
	pm.cacheCompressionRatio.WithLabelValues(algorithm).Observe(ratio)
}

// RecordBytesSaved records bytes saved by compression
func (pm *PrometheusMetrics) RecordBytesSaved(algorithm string, bytesSaved int64) {
	if bytesSaved > 0 {
		pm.cacheBytesSavedTotal.WithLabelValues(algorithm).Add(float64(bytesSaved))
	}
}

// RecordDecompressionError records a decompression failure
func (pm *PrometheusMetrics) RecordDecompressionError(algorithm string) {
	pm.cacheDecompressionErrorTotal.WithLabelValues(algorithm).Inc()
}

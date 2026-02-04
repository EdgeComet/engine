package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"go.uber.org/zap"
)

// PrometheusMetrics provides high-performance metrics collection for Render Service
type PrometheusMetrics struct {
	// Chrome pool metrics
	chromePoolSize  prometheus.Gauge
	chromeAvailable prometheus.Gauge

	// Render metrics
	rendersTotal   *prometheus.CounterVec
	renderDuration prometheus.Histogram

	// Queue metrics
	queueDepth      prometheus.Gauge
	queueRejections prometheus.Counter

	// HTTP metrics
	httpRequests *prometheus.CounterVec

	// Error metrics
	errorsTotal *prometheus.CounterVec

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

	// Chrome pool metrics
	pm.chromePoolSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "chrome_pool_size",
		Help:      "Total number of Chrome instances in the pool",
	})

	pm.chromeAvailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "chrome_available",
		Help:      "Number of available Chrome instances",
	})

	// Render metrics
	pm.rendersTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "renders_total",
		Help:      "Total number of render requests",
	}, []string{"status"}) // status: success, error, timeout, queue_full

	pm.renderDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "render_duration_seconds",
		Help:      "Time spent rendering pages",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
	})

	// Queue metrics
	pm.queueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "queue_depth",
		Help:      "Current number of requests waiting in queue",
	})

	pm.queueRejections = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "queue_rejections_total",
		Help:      "Total number of requests rejected due to full queue",
	})

	// HTTP metrics
	pm.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests by endpoint and status",
	}, []string{"endpoint", "status"})

	// Error metrics
	pm.errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "errors_total",
		Help:      "Total errors by type",
	}, []string{"type"}) // type: validation, render, timeout, internal

	// Register all metrics
	registerer.MustRegister(
		pm.chromePoolSize,
		pm.chromeAvailable,
		pm.rendersTotal,
		pm.renderDuration,
		pm.queueDepth,
		pm.queueRejections,
		pm.httpRequests,
		pm.errorsTotal,
	)

	// Create HTTP handler
	gatherer, ok := registerer.(prometheus.Gatherer)
	if !ok {
		gatherer = prometheus.DefaultGatherer
	}
	pm.httpHandler = fasthttpadaptor.NewFastHTTPHandler(promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))

	logger.Info("Render Service Prometheus metrics initialized")
	return pm
}

// UpdateChromePoolSize updates the Chrome pool size metric
func (pm *PrometheusMetrics) UpdateChromePoolSize(size float64) {
	pm.chromePoolSize.Set(size)
}

// UpdateChromeAvailable updates the available Chrome instances metric
func (pm *PrometheusMetrics) UpdateChromeAvailable(available float64) {
	pm.chromeAvailable.Set(available)
}

// RecordRender records a render request outcome
func (pm *PrometheusMetrics) RecordRender(status string) {
	pm.rendersTotal.WithLabelValues(status).Inc()
}

// RecordRenderDuration records render duration
func (pm *PrometheusMetrics) RecordRenderDuration(seconds float64) {
	pm.renderDuration.Observe(seconds)
}

// UpdateQueueDepth updates the current queue depth
func (pm *PrometheusMetrics) UpdateQueueDepth(depth float64) {
	pm.queueDepth.Set(depth)
}

// RecordQueueRejection records a queue rejection
func (pm *PrometheusMetrics) RecordQueueRejection() {
	pm.queueRejections.Inc()
}

// RecordHTTPRequest records an HTTP request
func (pm *PrometheusMetrics) RecordHTTPRequest(endpoint, status string) {
	pm.httpRequests.WithLabelValues(endpoint, status).Inc()
}

// RecordError records an error by type
func (pm *PrometheusMetrics) RecordError(errorType string) {
	pm.errorsTotal.WithLabelValues(errorType).Inc()
}

// ServeHTTP serves Prometheus metrics via HTTP
func (pm *PrometheusMetrics) ServeHTTP(ctx *fasthttp.RequestCtx) {
	pm.httpHandler(ctx)
}

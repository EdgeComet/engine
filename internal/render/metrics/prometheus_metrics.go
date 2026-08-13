package metrics

import (
	"strconv"

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

	// Scroll metrics
	scrollDuration   prometheus.Histogram
	scrollNoScroller prometheus.Counter
	scrollOutcomes   *prometheus.CounterVec

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

	// Scroll metrics
	pm.scrollDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "scroll_duration_seconds",
		Help:      "Time spent scrolling pages before HTML capture",
		Buckets:   prometheus.ExponentialBuckets(0.25, 2, 7), // 0.25s to 16s
	})

	pm.scrollNoScroller = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "scroll_no_scroller_total",
		Help:      "Total renders where scroll was requested but no scrollable element was found",
	})

	pm.scrollOutcomes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "rs",
		Name:      "scroll_outcomes_total",
		Help:      "Total scroll passes by how they ended and whether the page bottom was reached",
	}, []string{"stop_reason", "reached_bottom"}) // stop_reason: settled, duration, max_steps, no_target, cancelled, error

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
		pm.scrollDuration,
		pm.scrollNoScroller,
		pm.scrollOutcomes,
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

// RecordScrollDuration records the duration of a scroll pass
func (pm *PrometheusMetrics) RecordScrollDuration(seconds float64) {
	pm.scrollDuration.Observe(seconds)
}

// RecordScrollOutcome records how a scroll pass ended and whether it reached the page bottom
func (pm *PrometheusMetrics) RecordScrollOutcome(stopReason string, reachedBottom bool) {
	pm.scrollOutcomes.WithLabelValues(stopReason, strconv.FormatBool(reachedBottom)).Inc()
}

// RecordScrollNoScroller records a render where no scrollable element was found
func (pm *PrometheusMetrics) RecordScrollNoScroller() {
	pm.scrollNoScroller.Inc()
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

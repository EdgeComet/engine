package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"go.uber.org/zap"
)

type PrometheusMetrics struct {
	httpHandler func(*fasthttp.RequestCtx)
	logger      *zap.Logger

	recacheRequestsTotal *prometheus.CounterVec
	queueDepth           *prometheus.GaugeVec
	recacheDuration      prometheus.Histogram
	redisOperationsTotal *prometheus.CounterVec
	egRequestsTotal      *prometheus.CounterVec
}

func NewPrometheusMetrics(namespace string, logger *zap.Logger) *PrometheusMetrics {
	if namespace == "" {
		namespace = "edgecomet"
	}

	pm := &PrometheusMetrics{
		logger: logger,
	}

	pm.recacheRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_requests_total",
			Help:      "Total number of recache requests",
		},
		[]string{"status", "queue_type"},
	)

	pm.queueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "queue_depth",
			Help:      "Current depth of recache queues",
		},
		[]string{"queue_type"},
	)

	pm.recacheDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_duration_seconds",
			Help:      "Duration of recache operations in seconds",
			Buckets:   prometheus.DefBuckets,
		},
	)

	pm.redisOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "redis_operations_total",
			Help:      "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	pm.egRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "eg_requests_total",
			Help:      "Total number of requests to Edge Gateway",
		},
		[]string{"eg_id", "status"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm.recacheRequestsTotal)
	registry.MustRegister(pm.queueDepth)
	registry.MustRegister(pm.recacheDuration)
	registry.MustRegister(pm.redisOperationsTotal)
	registry.MustRegister(pm.egRequestsTotal)

	gatherer := prometheus.Gatherer(registry)
	handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})

	pm.httpHandler = fasthttpadaptor.NewFastHTTPHandler(handler)

	logger.Info("Prometheus metrics initialized for Cache Daemon",
		zap.String("namespace", namespace))

	return pm
}

func (pm *PrometheusMetrics) RecordRecacheRequest(status, queueType string) {
	pm.recacheRequestsTotal.WithLabelValues(status, queueType).Inc()
}

func (pm *PrometheusMetrics) SetQueueDepth(queueType string, depth int) {
	pm.queueDepth.WithLabelValues(queueType).Set(float64(depth))
}

func (pm *PrometheusMetrics) RecordRecacheDuration(duration float64) {
	pm.recacheDuration.Observe(duration)
}

func (pm *PrometheusMetrics) RecordRedisOperation(operation, status string) {
	pm.redisOperationsTotal.WithLabelValues(operation, status).Inc()
}

func (pm *PrometheusMetrics) RecordEGRequest(egID, status string) {
	pm.egRequestsTotal.WithLabelValues(egID, status).Inc()
}

func (pm *PrometheusMetrics) ServeHTTP(ctx *fasthttp.RequestCtx) {
	pm.httpHandler(ctx)
}

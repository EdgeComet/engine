package metrics

import (
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"go.uber.org/zap"
)

// hostConcurrencyTotals holds the previously published acquired/denied totals
// for a host. SetHostConcurrency adds only the delta to the Prometheus counter
// so externally-tracked monotonic values are exposed safely.
type hostConcurrencyTotals struct {
	acquired uint64
	denied   uint64
}

type PrometheusMetrics struct {
	httpHandler func(*fasthttp.RequestCtx)
	logger      *zap.Logger

	recacheRequestsTotal *prometheus.CounterVec
	queueDepth           *prometheus.GaugeVec
	recacheDuration      prometheus.Histogram
	redisOperationsTotal *prometheus.CounterVec
	egRequestsTotal      *prometheus.CounterVec

	// Per-host concurrency limiter metrics
	recacheInflight      *prometheus.GaugeVec
	recacheMaxConcurrent *prometheus.GaugeVec
	recacheAcquiredTotal *prometheus.CounterVec
	recacheDeniedTotal   *prometheus.CounterVec

	// Tracks last published totals per host so SetHostConcurrency can publish
	// the delta only — never deletes the series, never produces false counter
	// resets observable to scrapers.
	concurrencyMu     sync.Mutex
	concurrencyTotals map[int]hostConcurrencyTotals
}

func NewPrometheusMetrics(namespace string, logger *zap.Logger) *PrometheusMetrics {
	if namespace == "" {
		namespace = "edgecomet"
	}

	pm := &PrometheusMetrics{
		logger:            logger,
		concurrencyTotals: make(map[int]hostConcurrencyTotals),
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

	// Labels: host (primary domain string, matches EG-side metrics) + host_id
	// (numeric ID for disambiguation when a host has multiple domains).
	pm.recacheInflight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_inflight",
			Help:      "Current in-flight recache requests per host",
		},
		[]string{"host", "host_id"},
	)

	pm.recacheMaxConcurrent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_max_concurrent",
			Help:      "Configured max concurrent recache requests per host",
		},
		[]string{"host", "host_id"},
	)

	pm.recacheAcquiredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_acquired_total",
			Help:      "Total recache concurrency slot acquisitions per host",
		},
		[]string{"host", "host_id"},
	)

	pm.recacheDeniedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_denied_total",
			Help:      "Total recache concurrency slot denials per host (limit reached)",
		},
		[]string{"host", "host_id"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(pm.recacheRequestsTotal)
	registry.MustRegister(pm.queueDepth)
	registry.MustRegister(pm.recacheDuration)
	registry.MustRegister(pm.redisOperationsTotal)
	registry.MustRegister(pm.egRequestsTotal)
	registry.MustRegister(pm.recacheInflight)
	registry.MustRegister(pm.recacheMaxConcurrent)
	registry.MustRegister(pm.recacheAcquiredTotal)
	registry.MustRegister(pm.recacheDeniedTotal)

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

// SetHostConcurrency exposes the current concurrency state for a single host.
// Call from the daemon's status sampler whenever it gathers a snapshot.
//
// Counters (acquired/denied) are externally tracked monotonic values inside
// HostConcurrencyLimiter. We publish them via Add(delta) on the Prometheus
// counter so a scrape sees a smooth monotonic series. Earlier versions used
// Delete + Add which produced fake counter resets when a scrape interleaved
// the Delete and Add calls.
func (pm *PrometheusMetrics) SetHostConcurrency(hostID int, host string, inFlight, maxConcurrent int64, acquiredTotal, deniedTotal uint64) {
	hostIDLabel := strconv.Itoa(hostID)
	pm.recacheInflight.WithLabelValues(host, hostIDLabel).Set(float64(inFlight))
	pm.recacheMaxConcurrent.WithLabelValues(host, hostIDLabel).Set(float64(maxConcurrent))

	pm.concurrencyMu.Lock()
	prev := pm.concurrencyTotals[hostID]
	pm.concurrencyTotals[hostID] = hostConcurrencyTotals{acquired: acquiredTotal, denied: deniedTotal}
	pm.concurrencyMu.Unlock()

	if acquiredTotal > prev.acquired {
		pm.recacheAcquiredTotal.WithLabelValues(host, hostIDLabel).Add(float64(acquiredTotal - prev.acquired))
	}
	if deniedTotal > prev.denied {
		pm.recacheDeniedTotal.WithLabelValues(host, hostIDLabel).Add(float64(deniedTotal - prev.denied))
	}
}

func (pm *PrometheusMetrics) ServeHTTP(ctx *fasthttp.RequestCtx) {
	pm.httpHandler(ctx)
}

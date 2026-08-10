package metrics

import (
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/valyala/fasthttp"
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
	gatherer prometheus.Gatherer
	logger   *zap.Logger

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

	// Per-priority pull counter: count of entries popped from a Redis ZSET
	// into the internal queue. Labelled by priority and host_id so operators
	// can verify the unified drain is actually pulling normal/autorecache on
	// every tick, not just on the old 60s cadence.
	recachePulledTotal *prometheus.CounterVec

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
		[]string{"status", "queue_type", "host_id"},
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

	pm.recachePulledTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cd",
			Name:      "recache_pulled_total",
			Help:      "Total entries pulled from Redis recache queues into the internal queue per priority and host",
		},
		[]string{"priority", "host_id"},
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
	registry.MustRegister(pm.recachePulledTotal)

	pm.gatherer = registry

	logger.Info("Prometheus metrics initialized for Cache Daemon",
		zap.String("namespace", namespace))

	return pm
}

func (pm *PrometheusMetrics) RecordRecacheRequest(status, queueType string, hostID int) {
	pm.recacheRequestsTotal.WithLabelValues(status, queueType, strconv.Itoa(hostID)).Inc()
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

// RecordRecachePulled increments the per-priority pull counter by n.
func (pm *PrometheusMetrics) RecordRecachePulled(priority string, hostID int, n int) {
	if n <= 0 {
		return
	}
	pm.recachePulledTotal.WithLabelValues(priority, strconv.Itoa(hostID)).Add(float64(n))
}

// ServeHTTP gathers the registered metrics and writes the Prometheus text
// exposition directly to ctx. Encoding inline (instead of bridging through
// fasthttpadaptor.NewFastHTTPHandler) keeps the scrape fully synchronous: the
// adaptor spawned a goroutine and did a non-blocking modeDone send on an
// unbuffered channel, so a scraper preempted before its receive would block
// forever -- the source of the cache-daemon metrics-scrape hang under load.
func (pm *PrometheusMetrics) ServeHTTP(ctx *fasthttp.RequestCtx) {
	mfs, err := pm.gatherer.Gather()
	if err != nil {
		// Parity with promhttp.ContinueOnError: log and still emit whatever
		// families were gathered.
		pm.logger.Error("Failed to gather Prometheus metrics", zap.Error(err))
	}

	format := expfmt.NewFormat(expfmt.TypeTextPlain)
	ctx.SetContentType(string(format))

	enc := expfmt.NewEncoder(ctx.Response.BodyWriter(), format)
	for _, mf := range mfs {
		if encErr := enc.Encode(mf); encErr != nil {
			pm.logger.Error("Failed to encode Prometheus metric family", zap.Error(encErr))
			return
		}
	}
}

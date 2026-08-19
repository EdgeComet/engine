package metrics

import (
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

type MetricsCollector struct {
	prometheus *PrometheusMetrics
	logger     *zap.Logger
}

func NewMetricsCollector(namespace string, logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		prometheus: NewPrometheusMetrics(namespace, logger),
		logger:     logger,
	}
}

func (mc *MetricsCollector) RecordRecacheRequest(status, queueType string, hostID int) {
	mc.prometheus.RecordRecacheRequest(status, queueType, hostID)

	mc.logger.Debug("Recorded recache request metric",
		zap.String("status", status),
		zap.String("queue_type", queueType),
		zap.Int("host_id", hostID))
}

func (mc *MetricsCollector) SetQueueDepth(queueType string, depth int) {
	mc.prometheus.SetQueueDepth(queueType, depth)

	mc.logger.Debug("Set queue depth metric",
		zap.String("queue_type", queueType),
		zap.Int("depth", depth))
}

func (mc *MetricsCollector) RecordRecacheDuration(duration time.Duration) {
	mc.prometheus.RecordRecacheDuration(duration.Seconds())

	mc.logger.Debug("Recorded recache duration metric",
		zap.Duration("duration", duration))
}

func (mc *MetricsCollector) RecordRedisOperation(operation, status string) {
	mc.prometheus.RecordRedisOperation(operation, status)

	mc.logger.Debug("Recorded Redis operation metric",
		zap.String("operation", operation),
		zap.String("status", status))
}

func (mc *MetricsCollector) RecordEGRequest(egID, status string) {
	mc.prometheus.RecordEGRequest(egID, status)

	mc.logger.Debug("Recorded EG request metric",
		zap.String("eg_id", egID),
		zap.String("status", status))
}

// SetHostConcurrency publishes a snapshot of a host's per-origin concurrency state.
// `host` is the primary domain (matches the EG-side `host` label vocabulary);
// `hostID` is the numeric ID used for cross-service disambiguation.
func (mc *MetricsCollector) SetHostConcurrency(hostID int, host string, inFlight, maxConcurrent int64, acquiredTotal, deniedTotal uint64) {
	mc.prometheus.SetHostConcurrency(hostID, host, inFlight, maxConcurrent, acquiredTotal, deniedTotal)
}

// RecordRecachePulled exposes per-priority pull counts to Prometheus.
// Called by the scheduler each time a batch of entries is pulled from a
// Redis ZSET into the internal queue.
func (mc *MetricsCollector) RecordRecachePulled(priority string, hostID int, n int) {
	mc.prometheus.RecordRecachePulled(priority, hostID, n)
}

// SetRecachePaused exposes the per-host recache pause state to Prometheus. Published on
// each scheduler tick for every configured host plus any host the gauge last reported as
// paused, so neither a resumed host nor one moved to another cluster holds its last 1.
// While the whole scheduler is paused no tick runs and the gauge holds its last values.
func (mc *MetricsCollector) SetRecachePaused(hostID int, paused bool) {
	mc.prometheus.SetRecachePaused(hostID, paused)
}

func (mc *MetricsCollector) ServeHTTP(ctx *fasthttp.RequestCtx) {
	mc.prometheus.ServeHTTP(ctx)
}

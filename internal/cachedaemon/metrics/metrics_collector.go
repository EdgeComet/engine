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

func (mc *MetricsCollector) RecordRecacheRequest(status, queueType string) {
	mc.prometheus.RecordRecacheRequest(status, queueType)

	mc.logger.Debug("Recorded recache request metric",
		zap.String("status", status),
		zap.String("queue_type", queueType))
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

func (mc *MetricsCollector) ServeHTTP(ctx *fasthttp.RequestCtx) {
	mc.prometheus.ServeHTTP(ctx)
}

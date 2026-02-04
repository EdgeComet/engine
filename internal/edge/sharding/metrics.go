package sharding

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Prometheus metrics for sharding operations
type Metrics struct {
	RequestsTotal         *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	BytesTransferredTotal *prometheus.CounterVec
	ClusterSize           prometheus.Gauge
	UnderReplicatedTotal  *prometheus.CounterVec
	ErrorsTotal           *prometheus.CounterVec
	PushFailuresTotal     *prometheus.CounterVec
	LocalCacheEntries     prometheus.Gauge
}

// NewMetrics creates and registers Prometheus metrics for sharding
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "requests_total",
				Help:      "Total number of inter-EG requests",
			},
			[]string{"operation", "status", "target_eg_id"},
		),

		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "request_duration_seconds",
				Help:      "Duration of inter-EG requests in seconds",
				Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
			},
			[]string{"operation"},
		),

		BytesTransferredTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "bytes_transferred_total",
				Help:      "Total bytes transferred in inter-EG communication",
			},
			[]string{"operation", "direction"},
		),

		ClusterSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "cluster_size",
				Help:      "Number of healthy EGs in cluster (with sharding enabled)",
			},
		),

		UnderReplicatedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "under_replicated_total",
				Help:      "Number of cache entries created with fewer replicas than target",
			},
			[]string{"host_id"},
		),

		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "errors_total",
				Help:      "Inter-EG communication errors",
			},
			[]string{"error_type"},
		),

		PushFailuresTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "push_failures_total",
				Help:      "Failed push operations per target EG",
			},
			[]string{"target_eg_id"},
		),

		LocalCacheEntries: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "eg_sharding",
				Name:      "local_cache_entries",
				Help:      "Number of cache entries stored locally on this EG",
			},
		),
	}
}

// RecordPullRequest records metrics for a pull operation
func (m *Metrics) RecordPullRequest(targetEgID string, success bool, duration float64) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.RequestsTotal.WithLabelValues("pull", status, targetEgID).Inc()
	m.RequestDuration.WithLabelValues("pull").Observe(duration)
}

// RecordPushRequest records metrics for a push operation
func (m *Metrics) RecordPushRequest(targetEgID string, success bool, duration float64) {
	status := "success"
	if !success {
		status = "failure"
		m.PushFailuresTotal.WithLabelValues(targetEgID).Inc()
	}
	m.RequestsTotal.WithLabelValues("push", status, targetEgID).Inc()
	m.RequestDuration.WithLabelValues("push").Observe(duration)
}

// RecordBytesTransferred records bytes sent or received
func (m *Metrics) RecordBytesTransferred(operation string, direction string, bytes int) {
	m.BytesTransferredTotal.WithLabelValues(operation, direction).Add(float64(bytes))
}

// RecordError records an error by type
func (m *Metrics) RecordError(errorType string) {
	m.ErrorsTotal.WithLabelValues(errorType).Inc()
}

// RecordUnderReplication records under-replicated cache creation
func (m *Metrics) RecordUnderReplication(hostID string) {
	m.UnderReplicatedTotal.WithLabelValues(hostID).Inc()
}

// UpdateClusterSize updates the cluster size gauge
func (m *Metrics) UpdateClusterSize(size int) {
	m.ClusterSize.Set(float64(size))
}

// UpdateLocalCacheEntries updates the local cache entries gauge
func (m *Metrics) UpdateLocalCacheEntries(count int) {
	m.LocalCacheEntries.Set(float64(count))
}

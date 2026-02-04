package cleanup

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type CleanupMetrics struct {
	runsTotal          *prometheus.CounterVec
	directoriesDeleted *prometheus.CounterVec
	duration           *prometheus.HistogramVec
	errorsTotal        *prometheus.CounterVec
	logger             *zap.Logger
}

func NewCleanupMetrics(namespace string, logger *zap.Logger) *CleanupMetrics {
	return NewCleanupMetricsWithRegistry(namespace, prometheus.DefaultRegisterer, logger)
}

func NewCleanupMetricsWithRegistry(namespace string, registerer prometheus.Registerer, logger *zap.Logger) *CleanupMetrics {
	cm := &CleanupMetrics{
		logger: logger,
	}

	cm.runsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "filesystem_cleanup_runs_total",
			Help:      "Total cleanup runs per host",
		},
		[]string{"host_id", "status"},
	)

	cm.directoriesDeleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "filesystem_cleanup_directories_deleted_total",
			Help:      "Total directories deleted",
		},
		[]string{"host_id"},
	)

	cm.duration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "filesystem_cleanup_duration_seconds",
			Help:      "Duration of cleanup operations",
			Buckets:   []float64{0.1, 0.5, 1, 5, 10, 30, 60},
		},
		[]string{"host_id"},
	)

	cm.errorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "eg",
			Name:      "filesystem_cleanup_errors_total",
			Help:      "Cleanup errors by type",
		},
		[]string{"host_id", "error_type"},
	)

	registerer.MustRegister(
		cm.runsTotal,
		cm.directoriesDeleted,
		cm.duration,
		cm.errorsTotal,
	)

	return cm
}

func (cm *CleanupMetrics) RecordRun(hostID string, status string) {
	cm.runsTotal.WithLabelValues(hostID, status).Inc()
}

func (cm *CleanupMetrics) RecordDirectoriesDeleted(hostID string, count int) {
	cm.directoriesDeleted.WithLabelValues(hostID).Add(float64(count))
}

func (cm *CleanupMetrics) RecordDuration(hostID string, seconds float64) {
	cm.duration.WithLabelValues(hostID).Observe(seconds)
}

func (cm *CleanupMetrics) RecordError(hostID string, errorType string) {
	cm.errorsTotal.WithLabelValues(hostID, errorType).Inc()
}

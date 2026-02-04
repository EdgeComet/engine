package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

type FilesystemCleanupWorker struct {
	config        *configtypes.CleanupConfig
	basePath      string
	configManager configtypes.EGConfigManager
	logger        *zap.Logger
	metrics       *CleanupMetrics
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func NewFilesystemCleanupWorker(
	config *configtypes.CleanupConfig,
	basePath string,
	configManager configtypes.EGConfigManager,
	logger *zap.Logger,
	metrics *CleanupMetrics,
) *FilesystemCleanupWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &FilesystemCleanupWorker{
		config:        config,
		basePath:      basePath,
		configManager: configManager,
		logger:        logger,
		metrics:       metrics,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (w *FilesystemCleanupWorker) Start() {
	if !w.config.Enabled {
		w.logger.Info("Filesystem cleanup worker disabled")
		return
	}

	interval := time.Duration(w.config.Interval)
	w.logger.Info("Filesystem cleanup worker starting",
		zap.Duration("interval", interval),
		zap.Duration("safety_margin", time.Duration(w.config.SafetyMargin)))

	ticker := time.NewTicker(interval)
	w.wg.Add(1)

	go func() {
		defer w.wg.Done()
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.runCleanup()
			case <-w.ctx.Done():
				w.logger.Info("Filesystem cleanup worker shutting down")
				return
			}
		}
	}()
}

func (w *FilesystemCleanupWorker) Shutdown() {
	w.logger.Info("Stopping filesystem cleanup worker")
	w.cancel()
	w.wg.Wait()
	w.logger.Info("Filesystem cleanup worker stopped")
}

func (w *FilesystemCleanupWorker) runCleanup() {
	startTime := time.Now().UTC()
	w.logger.Info("Filesystem cleanup started")

	hosts := w.configManager.GetHosts()
	totalDeleted := 0

	for _, host := range hosts {
		deleted, err := w.cleanupHost(&host)
		if err != nil {
			w.logger.Error("Cleanup failed for host",
				zap.Int("host_id", host.ID),
				zap.String("domain", host.Domain),
				zap.Error(err))
			continue
		}

		if deleted > 0 {
			totalDeleted += deleted
			w.logger.Info("Cleanup completed for host",
				zap.Int("host_id", host.ID),
				zap.String("domain", host.Domain),
				zap.Int("directories_deleted", deleted))
		}
	}

	duration := time.Since(startTime)
	w.logger.Info("Filesystem cleanup finished",
		zap.Int("total_directories_deleted", totalDeleted),
		zap.Duration("duration", duration))
}

// getMaxRetentionTime returns maximum retention time across global, host and all patterns
// IMPORTANT: File path timestamp = cache expiration time in UTC (set by cache_coordinator.go)
// Retention = stale_ttl (for serve_stale) OR 0 (for delete) + safety_margin
// For delete strategy: no additional retention (path timestamp already includes cache_ttl)
// For serve_stale strategy: stale_ttl (additional time to serve stale after expiration)
func (w *FilesystemCleanupWorker) getMaxRetentionTime(host *types.Host) (time.Duration, string) {
	var maxRetention time.Duration
	source := "none"

	// Global-level cache config (baseline)
	globalConfig := w.configManager.GetConfig()
	if globalConfig.Render.Cache.Expired != nil {
		switch globalConfig.Render.Cache.Expired.Strategy {
		case types.ExpirationStrategyServeStale:
			// serve_stale: stale_ttl alone (additional time after expiration)
			if globalConfig.Render.Cache.Expired.StaleTTL != nil {
				retention := time.Duration(*globalConfig.Render.Cache.Expired.StaleTTL)
				if retention > maxRetention {
					maxRetention = retention
					source = "global(serve_stale)"
				}
			}
		case types.ExpirationStrategyDelete:
			// delete: no additional retention (path timestamp = expiration time)
			source = "global(delete)"
		}
	}

	// Host-level cache config (replaces global if expired section exists)
	if host.Render.Cache != nil && host.Render.Cache.Expired != nil {
		switch host.Render.Cache.Expired.Strategy {
		case types.ExpirationStrategyServeStale:
			// serve_stale: stale_ttl alone (additional time after expiration)
			if host.Render.Cache.Expired.StaleTTL != nil {
				retention := time.Duration(*host.Render.Cache.Expired.StaleTTL)
				// Host config replaces global (not max)
				maxRetention = retention
				source = "host(serve_stale)"
			}
		case types.ExpirationStrategyDelete:
			// delete: no additional retention (path timestamp = expiration time)
			// Only safety_margin will be added later
			maxRetention = 0
			source = "host(delete)"
		}
		// If host has cache but no expired section, keep global (inheritance)
	}

	// URL pattern overrides (only for render action)
	if host.URLRules != nil {
		for i, rule := range host.URLRules {
			// Only consider patterns with render action (bypass/status have no render cache)
			if rule.Action != types.ActionRender {
				continue
			}

			if rule.Render != nil && rule.Render.Cache != nil && rule.Render.Cache.Expired != nil {
				expired := rule.Render.Cache.Expired

				if expired.Strategy == types.ExpirationStrategyServeStale && expired.StaleTTL != nil {
					// serve_stale: stale_ttl alone (additional time after expiration)
					retention := time.Duration(*expired.StaleTTL)
					if retention > maxRetention {
						maxRetention = retention
						source = fmt.Sprintf("pattern[%d](serve_stale)", i)
					}
				} else if expired.Strategy == types.ExpirationStrategyDelete {
					// delete: no additional retention (path timestamp = expiration time)
					source = fmt.Sprintf("pattern[%d](delete)", i)
				}
			}
		}
	}

	return maxRetention, source
}

func (w *FilesystemCleanupWorker) cleanupHost(host *types.Host) (int, error) {
	hostID := fmt.Sprintf("%d", host.ID)
	startTime := time.Now().UTC()

	maxRetention, source := w.getMaxRetentionTime(host)
	if source == "none" {
		w.logger.Warn("Host has no cache config, skipping cleanup",
			zap.Int("host_id", host.ID),
			zap.String("domain", host.Domain))
		w.metrics.RecordError(hostID, "missing_cache_config")
		return 0, nil
	}

	safetyMargin := time.Duration(w.config.SafetyMargin)
	retention := maxRetention + safetyMargin
	threshold := time.Now().UTC().Add(-retention)

	hostDir := filepath.Join(w.basePath, fmt.Sprintf("%d", host.ID))

	if _, err := os.Stat(hostDir); os.IsNotExist(err) {
		w.metrics.RecordRun(hostID, "success")
		return 0, nil
	}

	w.logger.Info("Starting cleanup for host, will delete directories older than threshold",
		zap.Int("host_id", host.ID),
		zap.String("domain", host.Domain),
		zap.Duration("max_retention", maxRetention),
		zap.String("retention_source", source),
		zap.Duration("retention", retention),
		zap.Time("threshold", threshold))

	deleted, err := w.deleteOldDirectories(hostDir, threshold)

	duration := time.Since(startTime)
	w.metrics.RecordDuration(hostID, duration.Seconds())

	if err != nil {
		w.metrics.RecordRun(hostID, "failure")
		w.metrics.RecordError(hostID, "cleanup_error")
		return deleted, err
	}

	w.metrics.RecordRun(hostID, "success")
	if deleted > 0 {
		w.metrics.RecordDirectoriesDeleted(hostID, deleted)
	}

	return deleted, nil
}

func (w *FilesystemCleanupWorker) deleteOldDirectories(hostDir string, threshold time.Time) (int, error) {
	deleted := 0

	err := filepath.Walk(hostDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			w.logger.Warn("Error accessing path during cleanup",
				zap.String("path", path),
				zap.Error(err))
			return nil
		}

		if !info.IsDir() {
			return nil
		}

		if path == hostDir {
			return nil
		}

		dirTime, err := w.parseDirectoryTimestamp(path, hostDir)
		if err != nil {
			return nil
		}

		if dirTime.Before(threshold) {
			if err := os.RemoveAll(path); err != nil {
				w.logger.Warn("Failed to delete directory",
					zap.String("path", path),
					zap.Time("dir_time", dirTime),
					zap.Error(err))
				return nil
			}

			deleted++

			relPath, _ := filepath.Rel(w.basePath, path)
			age := threshold.Sub(dirTime)
			w.logger.Info("Deleted cache directory",
				zap.String("path", relPath),
				zap.Duration("age", age))

			w.removeEmptyParentDirectories(path)

			return filepath.SkipDir
		}

		return nil
	})

	return deleted, err
}

func (w *FilesystemCleanupWorker) removeEmptyParentDirectories(deletedPath string) {
	currentPath := filepath.Dir(deletedPath)

	for currentPath != w.basePath && currentPath != "." && currentPath != "/" {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			w.logger.Warn("Failed to read directory for empty check",
				zap.String("path", currentPath),
				zap.Error(err))
			break
		}

		if len(entries) > 0 {
			break
		}

		if err := os.Remove(currentPath); err != nil {
			w.logger.Warn("Failed to remove empty parent directory",
				zap.String("path", currentPath),
				zap.Error(err))
			break
		}

		relPath, _ := filepath.Rel(w.basePath, currentPath)
		w.logger.Info("Removed empty parent directory",
			zap.String("path", relPath))

		currentPath = filepath.Dir(currentPath)
	}
}

func (w *FilesystemCleanupWorker) parseDirectoryTimestamp(path string, hostDir string) (time.Time, error) {
	relPath, err := filepath.Rel(hostDir, path)
	if err != nil {
		return time.Time{}, err
	}

	parts := strings.Split(filepath.Clean(relPath), string(filepath.Separator))

	if len(parts) < 5 {
		return time.Time{}, fmt.Errorf("path too short: %s", relPath)
	}

	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid year: %s", parts[0])
	}

	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month: %s", parts[1])
	}

	day, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day: %s", parts[2])
	}

	hour, err := strconv.Atoi(parts[3])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour: %s", parts[3])
	}

	minute, err := strconv.Atoi(parts[4])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid minute: %s", parts[4])
	}

	dirTime := time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)

	if year < 2020 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("invalid timestamp values: %04d-%02d-%02d %02d:%02d", year, month, day, hour, minute)
	}

	return dirTime, nil
}

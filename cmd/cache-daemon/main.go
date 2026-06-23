package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/cachedaemon"
	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/internal/common/redis"
)

const (
	// daemonShutdownTimeout bounds the daemon drain (in-flight dispatch + iq
	// flush). It must exceed a single recache.timeout_per_url (60s deployed)
	// plus dispatch grace and the HTTP-shutdown allowance so the drain can
	// actually complete before SIGKILL. The deploy role / systemd unit must set
	// TimeoutStopSec >= 90s so the orchestrator does not kill mid-drain.
	daemonShutdownTimeout = 75 * time.Second

	// httpListenerShutdownTimeout bounds stopping the HTTP API listener early
	// (before the daemon drain) so no new recache requests arrive during
	// shutdown.
	httpListenerShutdownTimeout = 5 * time.Second
)

func main() {
	// Parse command-line flags
	configPath := flag.String("c", "configs/example/cache-daemon.yaml", "path to cache-daemon configuration file")
	flag.Parse()

	// Create initial logger for startup
	initialLogger, err := logger.NewDefaultLogger()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	initialLogger.Info("Starting Cache Daemon",
		zap.String("config_path", *configPath))

	// Load cache-daemon configuration
	daemonConfig, err := config.LoadCacheDaemonConfig(*configPath, initialLogger.Logger)
	if err != nil {
		initialLogger.Fatal("Failed to load cache-daemon config", zap.Error(err))
	}

	// Reconfigure logger based on daemon config settings (uses INFO level during startup if configured level is higher).
	// Must happen before constructing any long-lived component, otherwise that
	// component captures the default console-only debug logger and its later
	// output bypasses the configured file sink.
	dynamicLogger, err := logger.NewLoggerWithStartupOverride(daemonConfig.Logging)
	if err != nil {
		initialLogger.Fatal("Failed to create configured logger", zap.Error(err))
	}
	defer dynamicLogger.Sync()

	// Add Daemon ID to all logs
	zapLogger := dynamicLogger.With(zap.String("daemon_id", daemonConfig.DaemonID))

	// Resolve EG config path (relative paths are relative to daemon config directory)
	egConfigPath := daemonConfig.EgConfig
	if !filepath.IsAbs(egConfigPath) {
		daemonDir := filepath.Dir(*configPath)
		egConfigPath = filepath.Join(daemonDir, egConfigPath)
	}

	zapLogger.Info("Loading EG config for hosts",
		zap.String("eg_config_path", egConfigPath))

	// Load EG configuration (for hosts only)
	configManager, err := config.NewEGConfigManager(egConfigPath, zapLogger)
	if err != nil {
		fmt.Println(egConfigPath)
		zapLogger.Fatal("!!!!Failed to load EG config", zap.Error(err))
	}

	// Initialize Redis client from daemon config
	redisClient, err := redis.NewClient(&daemonConfig.Redis, zapLogger)
	if err != nil {
		zapLogger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	// Create cache daemon instance
	daemon, err := cachedaemon.NewCacheDaemon(daemonConfig, configManager, redisClient, zapLogger)
	if err != nil {
		zapLogger.Fatal("Failed to create cache daemon", zap.Error(err))
	}

	// Start daemon components (scheduler, etc.)
	ctx := context.Background()
	if err := daemon.Start(ctx); err != nil {
		zapLogger.Fatal("Failed to start daemon components", zap.Error(err))
	}

	// Setup HTTP server
	if daemonConfig.HTTPApi.Enabled {
		httpServer := &fasthttp.Server{
			Handler:                      httputil.RecoverHandler(daemon.ServeHTTP, zapLogger),
			Name:                         "CacheDaemon/1.0",
			ReadTimeout:                  time.Duration(daemonConfig.HTTPApi.RequestTimeout),
			WriteTimeout:                 time.Duration(daemonConfig.HTTPApi.RequestTimeout),
			IdleTimeout:                  60 * time.Second,
			DisablePreParseMultipartForm: true,
			NoDefaultServerHeader:        true,
			NoDefaultDate:                true,
		}

		listenAddr := daemonConfig.HTTPApi.Listen

		go func() {
			zapLogger.Info("HTTP API server starting", zap.String("addr", listenAddr))
			if err := httpServer.ListenAndServe(listenAddr); err != nil {
				zapLogger.Error("HTTP server error", zap.Error(err))
			}
		}()

		zapLogger.Info("Cache daemon started",
			zap.String("api_addr", listenAddr))

		// Switch to configured log level after startup is complete
		dynamicLogger.SwitchToConfiguredLevel()

		// Wait for shutdown signal
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		dynamicLogger.EnsureInfoLevelForShutdown()
		zapLogger.Info("Shutting down Cache Daemon...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), daemonShutdownTimeout)
		defer cancel()

		// Stop accepting new recache requests first so the daemon drains a
		// fixed in-flight set rather than a moving target. Bounded separately
		// from the daemon drain budget below.
		listenerCtx, listenerCancel := context.WithTimeout(context.Background(), httpListenerShutdownTimeout)
		if err := httpServer.ShutdownWithContext(listenerCtx); err != nil {
			zapLogger.Error("Failed to shutdown HTTP server gracefully", zap.Error(err))
		}
		listenerCancel()

		// Then drain the daemon (joins the scheduler, drains in-flight
		// dispatches within shutdownCtx, flushes the internal queue to Redis).
		if err := daemon.Shutdown(shutdownCtx); err != nil {
			zapLogger.Error("Failed to shutdown daemon components gracefully", zap.Error(err))
		}

		zapLogger.Info("Cache daemon stopped")
	} else {
		zapLogger.Warn("HTTP API is disabled in configuration")
		zapLogger.Info("Cache daemon started (HTTP API disabled)")

		// Switch to configured log level after startup is complete
		dynamicLogger.SwitchToConfiguredLevel()

		// Wait for shutdown signal
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		dynamicLogger.EnsureInfoLevelForShutdown()
		zapLogger.Info("Shutting down Cache Daemon...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), daemonShutdownTimeout)
		defer cancel()
		if err := daemon.Shutdown(shutdownCtx); err != nil {
			zapLogger.Error("Failed to shutdown daemon components gracefully", zap.Error(err))
		}
		zapLogger.Info("Cache daemon stopped")
	}
}

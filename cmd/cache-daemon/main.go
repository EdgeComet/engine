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
	"github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/internal/common/redis"
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

	// Resolve EG config path (relative paths are relative to daemon config directory)
	egConfigPath := daemonConfig.EgConfig
	if !filepath.IsAbs(egConfigPath) {
		daemonDir := filepath.Dir(*configPath)
		egConfigPath = filepath.Join(daemonDir, egConfigPath)
	}

	initialLogger.Info("Loading EG config for hosts",
		zap.String("eg_config_path", egConfigPath))

	// Load EG configuration (for hosts only)
	configManager, err := config.NewEGConfigManager(egConfigPath, initialLogger.Logger)
	if err != nil {
		fmt.Println(egConfigPath)
		initialLogger.Fatal("!!!!Failed to load EG config", zap.Error(err))
	}

	// Reconfigure logger based on daemon config settings (uses INFO level during startup if configured level is higher)
	dynamicLogger, err := logger.NewLoggerWithStartupOverride(daemonConfig.Logging)
	if err != nil {
		initialLogger.Fatal("Failed to create configured logger", zap.Error(err))
	}
	defer dynamicLogger.Sync()

	// Add Daemon ID to all logs
	zapLogger := dynamicLogger.With(zap.String("daemon_id", daemonConfig.DaemonID))

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
			Handler:                      daemon.ServeHTTP,
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
			zap.String("daemon_id", daemonConfig.DaemonID),
			zap.String("api_addr", listenAddr))

		// Switch to configured log level after startup is complete
		dynamicLogger.SwitchToConfiguredLevel()

		// Wait for shutdown signal
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		dynamicLogger.EnsureInfoLevelForShutdown()
		zapLogger.Info("Shutting down Cache Daemon...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown daemon components first
		if err := daemon.Shutdown(); err != nil {
			zapLogger.Error("Failed to shutdown daemon components gracefully", zap.Error(err))
		}

		// Then shutdown HTTP server
		if err := httpServer.ShutdownWithContext(shutdownCtx); err != nil {
			zapLogger.Error("Failed to shutdown HTTP server gracefully", zap.Error(err))
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
		if err := daemon.Shutdown(); err != nil {
			zapLogger.Error("Failed to shutdown daemon components gracefully", zap.Error(err))
		}
		zapLogger.Info("Cache daemon stopped")
	}
}

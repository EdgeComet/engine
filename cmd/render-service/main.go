package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	logutil "github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/internal/common/metricsserver"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/render/chrome"
	"github.com/edgecomet/engine/internal/render/metrics"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/internal/render/service"
)

func main() {
	// Parse command line flags
	configPath := flag.String("c", "configs/render-service.yaml",
		"Path to RS configuration file")
	flag.Parse()

	// Initialize logger (will be reconfigured from config)
	initialLogger, err := logutil.NewDefaultLogger()
	if err != nil {
		panic(err)
	}

	// Load configuration
	initialLogger.Info("Loading configuration", zap.String("path", *configPath))

	absPath, err := config.GetConfigPath(*configPath)
	if err != nil {
		initialLogger.Fatal("Invalid config path", zap.Error(err))
	}

	configMgr, err := config.NewRSConfigManager(absPath, initialLogger.Logger)
	if err != nil {
		initialLogger.Fatal("Failed to load configuration", zap.Error(err))
	}

	cfg := configMgr.GetConfig()

	// Reconfigure logger based on config settings (uses INFO level during startup if configured level is higher)
	dynamicLogger, err := logutil.NewLoggerWithStartupOverride(cfg.Log)
	if err != nil {
		initialLogger.Fatal("Failed to create configured logger", zap.Error(err))
	}

	logger := dynamicLogger.Logger

	logger.Info("Render Service starting",
		zap.String("rs", cfg.Server.ID),
		zap.String("listen", cfg.Server.Listen),
		zap.String("chrome_pool_size", cfg.Chrome.PoolSize))

	redisClient, err := redis.NewClient(&cfg.Redis, logger)
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	// Create Chrome configuration from YAML config
	chromeConfig := chrome.NewConfigFromYAML(
		cfg.Chrome.PoolSize,
		cfg.Chrome.Warmup.URL,
		time.Duration(cfg.Chrome.Warmup.Timeout),
		cfg.Chrome.Restart.AfterCount,
		time.Duration(cfg.Chrome.Restart.AfterTime),
		30*time.Second, // ShutdownTimeout - default 30s graceful shutdown
	)

	// Validate Chrome config
	if err := chromeConfig.Validate(); err != nil {
		logger.Fatal("Invalid Chrome configuration", zap.Error(err))
	}

	// Initialize metrics collector (before pool creation)
	metricsCollector := metrics.NewMetricsCollector(cfg.Metrics.Namespace, logger)

	// Start separate metrics server if needed
	metricsServer, err := metricsserver.StartMetricsServer(
		cfg.Metrics.Enabled,
		cfg.Metrics.Listen,
		cfg.Metrics.Path,
		metricsCollector,
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to start metrics server", zap.Error(err))
	}

	// Initialize RS registry (before pool creation)
	rsRegistry := registry.NewServiceRegistry(redisClient, logger)

	// Get hostname for metadata (before pool creation)
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = cfg.Server.ID
	}

	// Extract address and port from listen address
	listenHost, listenPort, err := configtypes.ParseListenAddress(cfg.Server.Listen)
	if err != nil {
		logger.Fatal("Failed to parse server.listen", zap.Error(err))
	}
	// Use 0.0.0.0 as default if no host specified
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}

	// Create service info (before pool creation)
	poolSize := chromeConfig.CalculatePoolSize()
	serviceInfo := &registry.ServiceInfo{
		ID:       cfg.Server.ID,
		Address:  listenHost,
		Port:     listenPort,
		Capacity: poolSize,
		Load:     0,
		Version:  "1.0.0",
	}
	serviceInfo.SetMetadata(poolSize, poolSize, hostname)

	// Create TabManager (but don't register tabs yet - wait for server to be ready)
	logger.Info("Initializing TabManager")
	tabManager := registry.NewTabManager(redisClient, cfg.Server.ID, poolSize, logger)

	// Initialize Chrome pool (but don't start heartbeat yet - wait for server to be ready)
	logger.Info("Initializing Chrome pool")
	pool, err := chrome.NewChromePool(chromeConfig, rsRegistry, serviceInfo, metricsCollector, tabManager, hostname, logger)
	if err != nil {
		logger.Fatal("Failed to create Chrome pool", zap.Error(err))
	}

	logger.Info("Chrome pool initialized",
		zap.Int("pool_size", poolSize))

	// Create HTTP handler with TabManager and RenderConfig
	httpHandler := service.CreateHTTPHandler(pool, tabManager, metricsCollector, &cfg.Chrome.Render, logger)

	// Calculate server timeout from render max_timeout + safety margin
	serverTimeout := cfg.Chrome.Render.CalculateServerTimeout()

	// Configure FastHTTP server
	server := &fasthttp.Server{
		Handler:      httpHandler,
		ReadTimeout:  serverTimeout,
		WriteTimeout: serverTimeout,
		IdleTimeout:  serverTimeout,
		Name:         "RenderService/" + cfg.Server.ID,
	}

	// Start server in background goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("Starting HTTP server",
			zap.String("listen", cfg.Server.Listen))
		if err := server.ListenAndServe(cfg.Server.Listen); err != nil {
			serverErrCh <- err
		}
	}()

	// Wait briefly for HTTP server to start listening
	logger.Info("Waiting for HTTP server to be ready")
	time.Sleep(100 * time.Millisecond)

	// Check if server failed to start
	select {
	case err := <-serverErrCh:
		logger.Fatal("HTTP server failed to start", zap.Error(err))
	default:
		// Server started successfully
	}

	// NOW register tabs in Redis (server is ready to accept requests)
	logger.Info("Registering tabs in Redis")
	if err := tabManager.RegisterTabs(context.Background()); err != nil {
		logger.Fatal("Failed to register tabs in Redis", zap.Error(err))
	}

	// Start periodic heartbeat to advertise availability (1 second interval)
	logger.Info("Starting service heartbeat")
	pool.StartPeriodicHeartbeat(1 * time.Second)

	logger.Info("Render Service fully ready and registered",
		zap.String("rs", cfg.Server.ID),
		zap.String("listen", cfg.Server.Listen),
		zap.Int("chrome_instances", poolSize))

	// Switch to configured log level after startup is complete
	dynamicLogger.SwitchToConfiguredLevel()

	// Wait for shutdown signal or server error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	case err := <-serverErrCh:
		logger.Error("Server error", zap.Error(err))
	}

	dynamicLogger.EnsureInfoLevelForShutdown()
	logger.Info("Shutting down gracefully...")

	// Stop heartbeat goroutine FIRST to prevent tabs hash recreation
	pool.StopHeartbeat()

	// Delete tabs hash from Redis (safe now, heartbeat stopped)
	deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := tabManager.DeleteTabs(deleteCtx); err != nil {
		logger.Error("Failed to delete tabs hash", zap.Error(err))
	}
	deleteCancel()

	// Deregister from Redis to prevent new traffic routing
	unregisterCtx, unregisterCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer unregisterCancel()
	if err := rsRegistry.UnregisterService(unregisterCtx, cfg.Server.ID); err != nil {
		logger.Error("Failed to deregister service", zap.Error(err))
	} else {
		logger.Info("Successfully deregistered from Redis")
	}

	// Wait briefly for Edge Gateway service cache to refresh
	logger.Info("Waiting for service cache propagation")
	time.Sleep(3 * time.Second)

	// Shutdown separate metrics server if exists
	if metricsServer != nil {
		metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := metricsServer.ShutdownWithContext(metricsShutdownCtx); err != nil {
			logger.Error("Metrics server shutdown error", zap.Error(err))
		} else {
			logger.Info("Metrics server shutdown complete")
		}
		metricsShutdownCancel()
	}

	// Graceful HTTP server shutdown - complete in-flight requests
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.ShutdownWithContext(shutdownCtx); err != nil {
		logger.Error("Server shutdown error", zap.Error(err))
	}

	// Shutdown Chrome pool
	if err := pool.Shutdown(); err != nil {
		logger.Error("Chrome pool shutdown error", zap.Error(err))
	}

	logger.Info("Render Service stopped")
}

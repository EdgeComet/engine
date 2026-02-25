package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/cachedaemon"
	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/internal/common/metricsserver"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/auth"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/cleanup"
	"github.com/edgecomet/engine/internal/edge/configtest"
	"github.com/edgecomet/engine/internal/edge/device"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/internal_server"
	"github.com/edgecomet/engine/internal/edge/metrics"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/recache"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/internal/edge/server"
	"github.com/edgecomet/engine/internal/edge/sharding"
	edgetls "github.com/edgecomet/engine/internal/edge/tls"
	"github.com/edgecomet/engine/internal/edge/validate"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("c", "configs/edge-gateway.yaml", "path to configuration file")
	testMode := flag.Bool("t", false, "test configuration and exit")
	flag.Parse()

	// If test mode, run validation
	if *testMode {
		var testURL string
		if flag.NArg() > 0 {
			testURL = flag.Arg(0)
		}
		exitCode := runConfigTest(*configPath, testURL)
		os.Exit(exitCode)
	}

	// Create initial logger for startup
	initialLogger, err := logger.NewDefaultLogger()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	initialLogger.Info("Starting Edge Gateway", zap.String("config_path", *configPath))

	configManager, err := config.NewEGConfigManager(*configPath, initialLogger.Logger)
	if err != nil {
		initialLogger.Fatal("Failed to create config manager", zap.Error(err))
	}

	cfg := configManager.GetConfig()

	// Reconfigure logger based on config settings
	dynamicLogger, err := logger.NewLoggerWithStartupOverride(cfg.Log)
	if err != nil {
		initialLogger.Fatal("Failed to create configured logger", zap.Error(err))
	}
	defer dynamicLogger.Sync()

	// Add Edge Gateway ID to all logs
	egLogger := dynamicLogger.With(zap.String("eg", cfg.EgID))

	// Create Redis client
	redisClient, err := redis.NewClient(&cfg.Redis, egLogger)
	if err != nil {
		egLogger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	// Initialize core services
	serviceRegistry := registry.NewServiceRegistry(redisClient, egLogger)
	keyGenerator := redis.NewKeyGenerator()
	metadataStore := cache.NewMetadataStore(redisClient, keyGenerator, cfg.Storage.BasePath, egLogger)
	fsCache := cache.NewFilesystemCache(egLogger)
	cacheService := cache.NewCacheService(metadataStore, fsCache, egLogger)

	// Initialize edge services
	authService := auth.NewAuthenticationService(configManager, egLogger)
	deviceDetector := device.NewDeviceDetector()
	bypassService := bypass.NewBypassService(&cfg.Bypass, egLogger)
	metricsCollector := metrics.NewMetricsCollector(cfg.Metrics.Namespace, egLogger)
	rsClient := rsclient.NewRSClient(egLogger)

	// Start metrics server if enabled
	metricsServer, err := metricsserver.StartMetricsServer(
		cfg.Metrics.Enabled,
		cfg.Metrics.Listen,
		cfg.Metrics.Path,
		metricsCollector,
		egLogger,
	)
	if err != nil {
		egLogger.Fatal("Failed to start metrics server", zap.Error(err))
	}

	// Initialize EG ID
	egID := cfg.EgID
	if egID == "" {
		egID = "default"
	}

	// Create sharding manager
	var shardingManager *sharding.Manager
	if cfg.CacheSharding != nil && cfg.CacheSharding.Enabled != nil && *cfg.CacheSharding.Enabled {
		egLogger.Info("Initializing sharding manager (enabled)",
			zap.String("eg_id", egID),
			zap.String("internal_listen", cfg.Internal.Listen))

		shardingManager, err = sharding.NewManager(
			cfg.CacheSharding,
			egID,
			cfg.Internal.AuthKey,
			redisClient,
			cacheService,
			cfg.Metrics.Namespace,
			egLogger,
		)
	} else {
		egLogger.Info("Initializing sharding manager (disabled)", zap.String("eg_id", egID))

		shardingManager, err = sharding.NewManager(
			&types.CacheShardingConfig{Enabled: ptrBool(false)},
			egID,
			"",
			redisClient,
			cacheService,
			cfg.Metrics.Namespace,
			egLogger,
		)
	}
	if err != nil {
		egLogger.Fatal("Failed to create sharding manager", zap.Error(err))
	}

	// Create render orchestrator
	renderOrchestrator := orchestrator.NewRenderOrchestrator(
		metadataStore,
		bypassService,
		cacheService,
		metricsCollector,
		serviceRegistry,
		fsCache,
		rsClient,
		redisClient,
		configManager,
		shardingManager,
		egLogger,
	)

	// Initialize autorecache client
	autorecacheClient := cachedaemon.NewAutorecacheClient(redisClient, egLogger)

	// Initialize event emitter
	var eventEmitter events.EventEmitter
	if cfg.EventLogging != nil && cfg.EventLogging.File.Enabled {
		fileEmitter, err := events.NewFileEmitter(cfg.EventLogging.File, egLogger)
		if err != nil {
			egLogger.Fatal("failed to create file emitter", zap.Error(err))
		}
		eventEmitter = events.NewMultiEmitter([]events.EventEmitter{fileEmitter}, egLogger)
		egLogger.Info("Event logging initialized",
			zap.String("path", cfg.EventLogging.File.Path))
	}

	// Initialize recache service
	cacheCoord := orchestrator.NewCacheCoordinator(metadataStore, fsCache, cacheService, shardingManager, metricsCollector, egLogger)
	recacheService := recache.NewRecacheService(configManager, cacheCoord, redisClient, rsClient, metadataStore, eventEmitter, cfg.EgID, egLogger)

	// Create internal server and register endpoints
	internalSrv := internal_server.NewInternalServer(cfg.Internal.AuthKey, egLogger)

	// Register sharding endpoints
	if shardingManager != nil && shardingManager.IsEnabled() {
		shardingManager.RegisterEndpoints(internalSrv)
	}

	// Register recache endpoint
	recacheService.RegisterEndpoints(internalSrv)

	// Register HAR render debug endpoint
	harRenderHandler := internal_server.NewHARRenderHandler(configManager, renderOrchestrator, egLogger)
	harRenderHandler.RegisterEndpoints(internalSrv)

	egLogger.Info("Internal server initialized with endpoints registered")

	// Initialize cleanup worker
	var cleanupWorker *cleanup.FilesystemCleanupWorker
	if cfg.Storage.Cleanup != nil {
		cleanupMetrics := cleanup.NewCleanupMetrics(cfg.Metrics.Namespace, egLogger)
		cleanupWorker = cleanup.NewFilesystemCleanupWorker(
			cfg.Storage.Cleanup,
			cfg.Storage.BasePath,
			configManager,
			egLogger,
			cleanupMetrics,
		)
	}

	// Create public server with pre-built services
	srv := server.NewServer(
		configManager,
		redisClient,
		keyGenerator,
		egLogger,
		authService,
		deviceDetector,
		renderOrchestrator,
		metricsCollector,
		shardingManager,
		metadataStore,
		autorecacheClient,
		eventEmitter,
		cfg.EgID,
	)

	// Start internal server (before cluster registration)
	ctx := context.Background()
	go func() {
		if err := internalSrv.Start(cfg.Internal.Listen); err != nil {
			egLogger.Error("Internal server failed", zap.Error(err))
		}
	}()
	egLogger.Info("Internal server started", zap.String("address", cfg.Internal.Listen))

	// Register with cluster
	if shardingManager != nil && shardingManager.IsEnabled() {
		if err := shardingManager.Start(ctx, cfg.Internal.Listen); err != nil {
			egLogger.Fatal("Failed to start sharding manager", zap.Error(err))
		}
		egLogger.Info("Sharding manager started successfully")
	}

	// Start cleanup worker
	if cleanupWorker != nil {
		cleanupWorker.Start()
		egLogger.Info("Filesystem cleanup worker started successfully")
	}

	// Create TLS listener before starting public servers to fail fast
	var tlsListener net.Listener
	if cfg.Server.TLS.Enabled {
		configDir := filepath.Dir(*configPath)
		certPath := cfg.Server.TLS.CertFile
		keyPath := cfg.Server.TLS.KeyFile
		if !filepath.IsAbs(certPath) {
			certPath = filepath.Join(configDir, certPath)
		}
		if !filepath.IsAbs(keyPath) {
			keyPath = filepath.Join(configDir, keyPath)
		}

		var err error
		tlsListener, err = edgetls.CreateTLSListener(cfg.Server.TLS.Listen, certPath, keyPath)
		if err != nil {
			egLogger.Fatal("Failed to create TLS listener", zap.Error(err))
		}
	}

	// Channel for server startup errors
	serverErrors := make(chan error, 2)

	// Create and start HTTP server
	httpLifecycle := &serverLifecycle{
		server:  newFastHTTPServer(srv.HandleRequest, time.Duration(cfg.Server.Timeout)),
		name:    "HTTP",
		address: cfg.Server.Listen,
		logger:  egLogger,
	}
	httpLifecycle.StartWithErrorChan(serverErrors)

	// Create and start HTTPS server if TLS is enabled
	var httpsLifecycle *serverLifecycle
	if cfg.Server.TLS.Enabled {
		httpsLifecycle = &serverLifecycle{
			server:   newFastHTTPServer(srv.HandleRequest, time.Duration(cfg.Server.Timeout)),
			listener: tlsListener,
			name:     "HTTPS",
			address:  cfg.Server.TLS.Listen,
			logger:   egLogger,
		}
		httpsLifecycle.StartWithErrorChan(serverErrors)
	}

	// Wait briefly for servers to start and check for immediate failures
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-serverErrors:
		egLogger.Fatal("Server failed to start", zap.Error(err))
	default:
		// Servers started successfully
	}

	if cfg.Server.TLS.Enabled {
		egLogger.Info("Edge Gateway started",
			zap.String("http_addr", cfg.Server.Listen),
			zap.String("https_addr", cfg.Server.TLS.Listen))
	} else {
		egLogger.Info("Edge Gateway started", zap.String("http_addr", cfg.Server.Listen))
	}

	// Switch to configured log level after startup is complete
	dynamicLogger.SwitchToConfiguredLevel()

	// Wait for shutdown signal or server error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		dynamicLogger.EnsureInfoLevelForShutdown()
		egLogger.Info("Shutting down Edge Gateway...")
	case err := <-serverErrors:
		dynamicLogger.EnsureInfoLevelForShutdown()
		egLogger.Error("Server startup failed, initiating shutdown", zap.Error(err))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown cleanup worker
	if cleanupWorker != nil {
		egLogger.Info("Shutting down filesystem cleanup worker")
		cleanupWorker.Shutdown()
	}

	// Shutdown metrics server
	if metricsServer != nil {
		egLogger.Info("Shutting down metrics server")
		if err := metricsServer.ShutdownWithContext(shutdownCtx); err != nil {
			egLogger.Error("Metrics server shutdown error", zap.Error(err))
		}
	}

	// Shutdown sharding manager (deregister from cluster)
	if shardingManager != nil && shardingManager.IsEnabled() {
		if err := shardingManager.Shutdown(shutdownCtx); err != nil {
			egLogger.Error("Failed to shutdown sharding manager gracefully", zap.Error(err))
		}
		egLogger.Info("Sharding manager shutdown complete")
	}

	// Shutdown internal server
	if internalSrv != nil {
		if err := internalSrv.Shutdown(shutdownCtx); err != nil {
			egLogger.Error("Failed to shutdown internal server gracefully", zap.Error(err))
		}
		egLogger.Info("Internal server shutdown complete")
	}

	// Shutdown public servers in parallel
	var wg sync.WaitGroup
	wg.Add(1)
	if httpsLifecycle != nil {
		wg.Add(1)
	}
	go func() {
		defer wg.Done()
		httpLifecycle.Shutdown(shutdownCtx)
	}()
	if httpsLifecycle != nil {
		go func() {
			defer wg.Done()
			httpsLifecycle.Shutdown(shutdownCtx)
		}()
	}
	wg.Wait()
	egLogger.Info("Public servers shutdown complete")

	// Shutdown event emitter
	if eventEmitter != nil {
		if err := eventEmitter.Close(); err != nil {
			egLogger.Error("Failed to close event emitter", zap.Error(err))
		}
		egLogger.Info("Event emitter shutdown complete")
	}

	egLogger.Info("Edge Gateway stopped")
}

func ptrBool(b bool) *bool {
	return &b
}

const serverName = "EdgeGateway/1.0"

func newFastHTTPServer(handler fasthttp.RequestHandler, timeout time.Duration) *fasthttp.Server {
	return &fasthttp.Server{
		Handler:                      handler,
		Name:                         serverName,
		ReadTimeout:                  timeout,
		WriteTimeout:                 timeout,
		IdleTimeout:                  timeout,
		DisablePreParseMultipartForm: true,
		NoDefaultServerHeader:        true,
		NoDefaultDate:                true,
	}
}

type serverLifecycle struct {
	server   *fasthttp.Server
	listener net.Listener // nil for HTTP (uses ListenAndServe), set for HTTPS
	name     string
	address  string
	logger   *zap.Logger
}

func (s *serverLifecycle) Start() {
	s.StartWithErrorChan(nil)
}

func (s *serverLifecycle) StartWithErrorChan(errChan chan<- error) {
	go func() {
		var err error
		if s.listener != nil {
			err = s.server.Serve(s.listener)
		} else {
			err = s.server.ListenAndServe(s.address)
		}
		if err != nil {
			s.logger.Error("Server error", zap.String("name", s.name), zap.Error(err))
			if errChan != nil {
				errChan <- fmt.Errorf("%s server failed: %w", s.name, err)
			}
		}
	}()
	s.logger.Info("Server started", zap.String("name", s.name), zap.String("address", s.address))
}

func (s *serverLifecycle) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server", zap.String("name", s.name))
	err := s.server.ShutdownWithContext(ctx)
	if err != nil {
		s.logger.Error("Server shutdown error", zap.String("name", s.name), zap.Error(err))
	}
	return err
}

// runConfigTest runs configuration validation and optional URL testing
func runConfigTest(configPath string, testURL string) int {
	result, err := validate.ValidateConfiguration(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Validation error: %v\n", err)
		return 1
	}

	if !result.Valid {
		fmt.Println("Configuration validation FAILED:")
		for _, e := range result.Errors {
			if e.Line > 0 {
				fmt.Printf("- %s line %d: %s\n", e.File, e.Line, e.Message)
			} else {
				fmt.Printf("- %s: %s\n", e.File, e.Message)
			}
		}
		return 1
	}

	fmt.Printf("configuration file %s syntax is ok\n", result.ConfigPath)

	if len(result.Warnings) > 0 {
		fmt.Println()
		fmt.Printf("Configuration warnings (%d):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			if w.Line > 0 {
				fmt.Printf("- %s line %d: %s\n", w.File, w.Line, w.Message)
			} else {
				fmt.Printf("- %s: %s\n", w.File, w.Message)
			}
		}
		fmt.Println()
	}

	fmt.Println("configuration test is successful")

	if testURL != "" {
		urlResult, err := configtest.TestURL(testURL, result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nURL test error: %v\n", err)
			return 1
		}
		configtest.PrintURLTestResult(urlResult)
	}

	return 0
}

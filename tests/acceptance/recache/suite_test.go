package recache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/edgecomet/engine/internal/cachedaemon"
	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/logger"
	"github.com/edgecomet/engine/pkg/types"
)

var (
	testEnv *RecacheTestEnvironment
)

// RecacheRequestReceived tracks recache requests received by mock EG
type RecacheRequestReceived struct {
	HostID      int
	URL         string
	DimensionID int
}

func TestRecacheAcceptance(t *testing.T) {
	RegisterFailHandler(Fail)

	// Configure Ginkgo to run specs sequentially
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.ParallelTotal = 1
	suiteConfig.Timeout = 30 * time.Minute
	reporterConfig.Succinct = true

	RunSpecs(t, "Recache Acceptance Test Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	By("Initializing recache test environment")
	var err error
	testEnv, err = NewRecacheTestEnvironment()
	Expect(err).ToNot(HaveOccurred())

	By("Starting test services (MinRedis, Mock EG, Cache Daemon)")
	Eventually(func() error {
		return testEnv.Start()
	}, 30*time.Second, 1*time.Second).Should(Succeed())

	By("Verifying services are healthy")
	Eventually(func() bool {
		return testEnv.CheckHealth()
	}, 15*time.Second, 500*time.Millisecond).Should(BeTrue())
})

var _ = AfterSuite(func() {
	By("Stopping test services")
	if testEnv != nil {
		testEnv.Stop()
	}
})

var _ = BeforeEach(func() {
	By("Clearing Redis before test")
	if testEnv != nil && testEnv.RedisClient != nil {
		testEnv.ClearRedis()
	}
})

// RecacheTestEnvironment manages the test environment for recache tests
type RecacheTestEnvironment struct {
	MiniRedis   *miniredis.Miniredis
	RedisClient *redis.Client

	// Daemon components
	DaemonCmd     *exec.Cmd // Daemon process handle
	ConfigManager *config.EGConfigManager
	Logger        *zap.Logger

	// Config paths
	TempConfigDir    string
	DaemonConfigPath string

	// Test configuration
	TestHostID      int
	DaemonPort      int
	InternalAuthKey string

	// Mock EG server
	MockEGServer     *fasthttp.Server
	MockEGPort       int
	MockEGResponses  chan bool // true = success, false = error
	MockEGReceivedCh chan RecacheRequestReceived
}

// NewRecacheTestEnvironment creates a new test environment
func NewRecacheTestEnvironment() (*RecacheTestEnvironment, error) {
	// Create logger based on DEBUG environment variable
	// Usage:
	//   TEST_MODE=local ginkgo run recache/           # Clean output, no logs
	//   DEBUG=1 TEST_MODE=local ginkgo run recache/   # Verbose output with DEBUG logs
	var zapLogger *zap.Logger
	if os.Getenv("DEBUG") != "" {
		dynamicLogger, err := logger.NewDefaultLogger()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		zapLogger = dynamicLogger.Logger
	} else {
		// Use nop logger when DEBUG is not set (suppresses all output)
		zapLogger = zap.NewNop()
	}

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Find another port for mock EG
	listener2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to find available port for mock EG: %w", err)
	}
	mockEGPort := listener2.Addr().(*net.TCPAddr).Port
	listener2.Close()

	return &RecacheTestEnvironment{
		TestHostID:       1,
		DaemonPort:       port,
		MockEGPort:       mockEGPort,
		InternalAuthKey:  "test-internal-auth-key",
		Logger:           zapLogger,
		MockEGResponses:  make(chan bool, 100),
		MockEGReceivedCh: make(chan RecacheRequestReceived, 500),
	}, nil
}

// writeDaemonConfig writes daemon configuration to YAML file
func writeDaemonConfig(path string, config *configtypes.CacheDaemonConfig) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// waitForDaemonReady polls daemon HTTP endpoint until ready
func waitForDaemonReady(port int, timeout time.Duration, internalAuthKey string) error {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/status", port)

	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("X-Internal-Auth", internalAuthKey)

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for daemon on port %d", port)
}

// Start initializes and starts all test services
func (env *RecacheTestEnvironment) Start() error {
	// Start MinRedis
	mr, err := miniredis.Run()
	if err != nil {
		return fmt.Errorf("failed to start miniredis: %w", err)
	}
	env.MiniRedis = mr

	// Initialize Redis client
	env.RedisClient = redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
		DB:   0,
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := env.RedisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to miniredis: %w", err)
	}

	// Config manager not needed for subprocess daemon
	// Daemon will load its own config from files
	env.ConfigManager = &config.EGConfigManager{}

	// Configure logging based on DEBUG environment variable
	// Always use stdout for test mode (enables scheduler control API)
	logLevel := "info"
	if os.Getenv("DEBUG") != "" {
		logLevel = "debug"
	}

	// Create temp config directory
	tempConfigDir, err := os.MkdirTemp("", "cache-daemon-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	env.TempConfigDir = tempConfigDir

	// Create hosts.yaml file
	projectRoot := filepath.Join("..", "..", "..")
	hostsConfigPath := filepath.Join(tempConfigDir, "hosts.yaml")

	hostsYAML := fmt.Sprintf(`hosts:
  - id: %d
    domain: "test.example.com"
    render_key: "test-render-key"
    enabled: true
    render:
      timeout: 30s

      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "desktop"
        mobile:
          id: 2
          width: 375
          height: 667
          render_ua: "mobile"
      events:
        wait_for: "networkIdle"
`, env.TestHostID)

	if err := os.WriteFile(hostsConfigPath, []byte(hostsYAML), 0644); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to write hosts config: %w", err)
	}

	// Create EG config YAML for tests
	egConfigPath := filepath.Join(tempConfigDir, "edge-gateway.yaml")

	egConfigYAML := fmt.Sprintf(`eg_id: "test-eg"
internal:
  listen: "localhost:9999"
  auth_key: "%s"

server:
  listen: ":9998"
  timeout: 120s

redis:
  addr: "%s"
  password: ""
  db: 0

storage:
  base_path: "/tmp/test-cache"

render:
  cache:
    ttl: 1h

bypass:
  timeout: 20s
  user_agent: "test"

registry:
  selection_strategy: "least_loaded"

log:
  level: "info"
  console:
    enabled: true
    format: "json"
  file:
    enabled: false

cache_sharding:
  enabled: true

hosts:
  include: "%s"
`, env.InternalAuthKey, mr.Addr(), hostsConfigPath)

	if err := os.WriteFile(egConfigPath, []byte(egConfigYAML), 0644); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to write EG config: %w", err)
	}

	// Create daemon config
	daemonConfig := &configtypes.CacheDaemonConfig{
		EgConfig: egConfigPath,
		DaemonID: "test-daemon",
		Redis: configtypes.RedisConfig{
			Addr:     mr.Addr(),
			Password: "",
			DB:       0,
		},
		Scheduler: configtypes.CacheDaemonScheduler{
			TickInterval:        types.Duration(100 * time.Millisecond),
			NormalCheckInterval: types.Duration(6 * time.Second), // 60 ticks * 100ms = 6s
		},
		InternalQueue: configtypes.CacheDaemonInternalQueue{
			MaxSize:        1000,
			MaxRetries:     3,
			RetryBaseDelay: types.Duration(100 * time.Millisecond), // Fast retries for testing
		},
		Recache: configtypes.CacheDaemonRecache{
			RSCapacityReserved: 0.30,
			TimeoutPerURL:      types.Duration(60 * time.Second),
		},
		HTTPApi: configtypes.CacheDaemonHTTPApi{
			Enabled:             true,
			Listen:              fmt.Sprintf(":%d", env.DaemonPort),
			RequestTimeout:      types.Duration(30 * time.Second),
			SchedulerControlAPI: true, // Enable scheduler control for tests
		},
		Logging: configtypes.CacheDaemonLogging{
			Level: logLevel,
			Console: configtypes.ConsoleLogConfig{
				Enabled: true,
				Format:  "json",
			},
			File: configtypes.FileLogConfig{
				Enabled: false,
			},
		},
	}

	// Write daemon config to file
	daemonConfigPath := filepath.Join(tempConfigDir, "daemon_config.yaml")
	if err := writeDaemonConfig(daemonConfigPath, daemonConfig); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to write daemon config: %w", err)
	}
	env.DaemonConfigPath = daemonConfigPath

	// Start daemon as subprocess
	daemonPath := filepath.Join(projectRoot, "cmd", "cache-daemon")

	daemonCmd := exec.Command("go", "run", ".", "-c", daemonConfigPath)
	daemonCmd.Dir = daemonPath
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create process group for clean kill
	}

	// Capture output based on DEBUG env
	if os.Getenv("DEBUG") != "" {
		daemonCmd.Stdout = os.Stdout
		daemonCmd.Stderr = os.Stderr
	} else {
		daemonCmd.Stdout = io.Discard
		daemonCmd.Stderr = io.Discard
	}

	if err := daemonCmd.Start(); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to start daemon process: %w", err)
	}
	env.DaemonCmd = daemonCmd

	// Wait for daemon to be ready
	if err := waitForDaemonReady(env.DaemonPort, 10*time.Second, env.InternalAuthKey); err != nil {
		// Kill daemon if it failed to become ready
		if pgid, err := syscall.Getpgid(daemonCmd.Process.Pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			daemonCmd.Process.Kill()
		}
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("daemon not ready: %w", err)
	}

	// Start mock EG server
	if err := env.StartMockEG(); err != nil {
		return fmt.Errorf("failed to start mock EG: %w", err)
	}

	return nil
}

// StartMockEG starts the mock Edge Gateway server
func (env *RecacheTestEnvironment) StartMockEG() error {
	// Mock EG handler
	handler := func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())

		if path == "/internal/cache/recache" {
			// Try to parse as batch API request first (URLs array format)
			var batchReq types.RecacheAPIRequest
			batchErr := json.Unmarshal(ctx.Request.Body(), &batchReq)

			// Track requests based on format
			entriesCount := 0

			if batchErr == nil && len(batchReq.URLs) > 0 {
				// Batch API format
				for _, url := range batchReq.URLs {
					for _, dimID := range batchReq.DimensionIDs {
						select {
						case env.MockEGReceivedCh <- RecacheRequestReceived{
							HostID:      batchReq.HostID,
							URL:         url,
							DimensionID: dimID,
						}:
						default:
							// Channel full, skip
						}
						entriesCount++
					}
				}
			} else {
				// Try internal single-URL format (daemon distribution)
				var internalReq struct {
					URL         string `json:"url"`
					HostID      int    `json:"host_id"`
					DimensionID int    `json:"dimension_id"`
				}
				if err := json.Unmarshal(ctx.Request.Body(), &internalReq); err != nil {
					ctx.SetStatusCode(fasthttp.StatusBadRequest)
					ctx.SetBodyString("Invalid request format")
					return
				}

				select {
				case env.MockEGReceivedCh <- RecacheRequestReceived{
					HostID:      internalReq.HostID,
					URL:         internalReq.URL,
					DimensionID: internalReq.DimensionID,
				}:
				default:
					// Channel full, skip
				}
				entriesCount = 1
			}

			// Check if we should succeed or fail
			success := true
			select {
			case success = <-env.MockEGResponses:
			default:
				// Default to success if no response configured
			}

			if success {
				data := types.RecacheAPIData{
					EntriesEnqueued: entriesCount,
				}
				apiResp := httputil.APIResponse{
					Success: true,
					Data:    data,
				}
				respBody, _ := json.Marshal(apiResp)
				ctx.SetStatusCode(fasthttp.StatusOK)
				ctx.SetContentType("application/json")
				ctx.SetBody(respBody)
			} else {
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBodyString("Internal server error")
			}
		} else {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
		}
	}

	env.MockEGServer = &fasthttp.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Start server in goroutine
	go func() {
		listenAddr := fmt.Sprintf("127.0.0.1:%d", env.MockEGPort)
		if err := env.MockEGServer.ListenAndServe(listenAddr); err != nil {
			env.Logger.Error("Mock EG server error", zap.Error(err))
		}
	}()

	// Wait for server to start by attempting a simple check
	// Poll until server is accepting connections
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", env.MockEGPort), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Register mock EG in registry
	// Note: daemon prepends "http://" so store address without protocol
	return env.AddMockEGToRegistry(fmt.Sprintf("127.0.0.1:%d", env.MockEGPort))
}

// RestartDaemon kills and restarts the daemon process
func (env *RecacheTestEnvironment) RestartDaemon() error {
	// Stop existing daemon process
	if env.DaemonCmd != nil && env.DaemonCmd.Process != nil {
		// Try graceful shutdown first (SIGTERM)
		pgid, err := syscall.Getpgid(env.DaemonCmd.Process.Pid)
		if err == nil {
			// Kill entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			// Fallback to killing just the parent
			env.DaemonCmd.Process.Signal(os.Interrupt)
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		go func() {
			done <- env.DaemonCmd.Wait()
		}()

		select {
		case <-done:
			// Graceful shutdown succeeded
		case <-time.After(2 * time.Second):
			// Force kill if timeout
			if pgid, err := syscall.Getpgid(env.DaemonCmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				env.DaemonCmd.Process.Kill()
			}
			// Wait a bit for force kill to complete
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Start new daemon process
	projectRoot := filepath.Join("..", "..", "..")
	daemonPath := filepath.Join(projectRoot, "cmd", "cache-daemon")

	daemonCmd := exec.Command("go", "run", ".", "-c", env.DaemonConfigPath)
	daemonCmd.Dir = daemonPath
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Capture output based on DEBUG env
	if os.Getenv("DEBUG") != "" {
		daemonCmd.Stdout = os.Stdout
		daemonCmd.Stderr = os.Stderr
	} else {
		daemonCmd.Stdout = io.Discard
		daemonCmd.Stderr = io.Discard
	}

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("failed to restart daemon process: %w", err)
	}
	env.DaemonCmd = daemonCmd

	// Wait for daemon to be ready
	if err := waitForDaemonReady(env.DaemonPort, 10*time.Second, env.InternalAuthKey); err != nil {
		// Kill daemon if it failed to become ready
		if pgid, err := syscall.Getpgid(daemonCmd.Process.Pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			daemonCmd.Process.Kill()
		}
		return fmt.Errorf("daemon not ready after restart: %w", err)
	}

	return nil
}

// Stop shuts down all test services
func (env *RecacheTestEnvironment) Stop() error {
	// Stop daemon process
	if env.DaemonCmd != nil && env.DaemonCmd.Process != nil {
		// Try graceful shutdown
		pgid, err := syscall.Getpgid(env.DaemonCmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			env.DaemonCmd.Process.Signal(os.Interrupt)
		}

		// Wait for graceful shutdown
		done := make(chan error, 1)
		go func() {
			done <- env.DaemonCmd.Wait()
		}()

		select {
		case <-done:
			// Graceful shutdown
		case <-time.After(3 * time.Second):
			// Force kill
			if pgid, err := syscall.Getpgid(env.DaemonCmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				env.DaemonCmd.Process.Kill()
			}
		}
	}

	// Shutdown mock EG server
	if env.MockEGServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := env.MockEGServer.ShutdownWithContext(shutdownCtx); err != nil {
			env.Logger.Error("Failed to shutdown mock EG server", zap.Error(err))
		}
	}

	// Close Redis client
	if env.RedisClient != nil {
		env.RedisClient.Close()
	}

	// Stop MinRedis last
	if env.MiniRedis != nil {
		env.MiniRedis.Close()
	}

	// Sync logger
	if env.Logger != nil {
		env.Logger.Sync()
	}

	return nil
}

// GetRSCapacityStatus calls daemon's /status endpoint and returns RS capacity info
func (env *RecacheTestEnvironment) GetRSCapacityStatus() cachedaemon.RSCapacityStatus {
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/status", env.DaemonPort)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Internal-Auth", env.InternalAuthKey)

	resp, err := client.Do(req)
	if err != nil {
		env.Logger.Error("Failed to get daemon status", zap.Error(err))
		return cachedaemon.RSCapacityStatus{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		env.Logger.Error("Daemon status returned non-200", zap.Int("status_code", resp.StatusCode))
		return cachedaemon.RSCapacityStatus{}
	}

	var statusResp cachedaemon.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		env.Logger.Error("Failed to decode status response", zap.Error(err))
		return cachedaemon.RSCapacityStatus{}
	}

	return statusResp.RSCapacity
}

// CheckHealth verifies all services are running
func (env *RecacheTestEnvironment) CheckHealth() bool {
	if env.RedisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := env.RedisClient.Ping(ctx).Result()
	return err == nil
}

// ClearRedis clears all keys from Redis
func (env *RecacheTestEnvironment) ClearRedis() error {
	if env.MiniRedis != nil {
		// Fast clear for miniredis
		env.MiniRedis.FlushAll()
		return nil
	}
	return nil
}

// Helper methods for test assertions

// GetZSETSize returns the number of entries in a ZSET
func (env *RecacheTestEnvironment) GetZSETSize(key string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return env.RedisClient.ZCard(ctx, key).Result()
}

// GetZSETMembers returns all members from a ZSET
func (env *RecacheTestEnvironment) GetZSETMembers(key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return env.RedisClient.ZRange(ctx, key, 0, -1).Result()
}

// GetZSETMembersWithScores returns all members with scores from a ZSET
func (env *RecacheTestEnvironment) GetZSETMembersWithScores(key string) ([]redis.Z, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return env.RedisClient.ZRangeWithScores(ctx, key, 0, -1).Result()
}

// GetZSETScore gets the score of a specific member
func (env *RecacheTestEnvironment) GetZSETScore(key, member string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return env.RedisClient.ZScore(ctx, key, member).Result()
}

// ZSETExists checks if a ZSET exists and has entries
func (env *RecacheTestEnvironment) ZSETExists(key string) bool {
	size, err := env.GetZSETSize(key)
	return err == nil && size > 0
}

// GetCacheMetadata retrieves cache metadata from Redis
func (env *RecacheTestEnvironment) GetCacheMetadata(cacheKey string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	metaKey := "meta:" + cacheKey
	return env.RedisClient.HGetAll(ctx, metaKey).Result()
}

// GetLastBotHit retrieves last_bot_hit field from cache metadata
func (env *RecacheTestEnvironment) GetLastBotHit(cacheKey string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	metaKey := "meta:" + cacheKey
	result, err := env.RedisClient.HGet(ctx, metaKey, "last_bot_hit").Result()
	if err != nil {
		return 0, err
	}

	var lastBotHit int64
	_, err = fmt.Sscanf(result, "%d", &lastBotHit)
	return lastBotHit, err
}

// CacheMetadataExists checks if cache metadata exists
func (env *RecacheTestEnvironment) CacheMetadataExists(cacheKey string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	metaKey := "meta:" + cacheKey
	exists, err := env.RedisClient.Exists(ctx, metaKey).Result()
	return err == nil && exists > 0
}

// WaitForCondition polls a condition until it's true or timeout
func (env *RecacheTestEnvironment) WaitForCondition(check func() bool, timeout time.Duration, pollInterval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return false
}

// FastForwardTime advances miniredis time (for TTL/ZSET score testing)
func (env *RecacheTestEnvironment) FastForwardTime(duration time.Duration) {
	if env.MiniRedis != nil {
		env.MiniRedis.FastForward(duration)
	}
}

// SendRecacheRequest sends HTTP POST request to daemon /internal/cache/recache endpoint
func (env *RecacheTestEnvironment) SendRecacheRequest(req types.RecacheAPIRequest) (*types.RecacheAPIData, int, error) {
	return env.SendRecacheRequestWithAuth(req, env.InternalAuthKey)
}

// SendRecacheRequestWithAuth sends HTTP POST request with custom auth header
func (env *RecacheTestEnvironment) SendRecacheRequestWithAuth(req types.RecacheAPIRequest, authKey string) (*types.RecacheAPIData, int, error) {
	// Marshal request to JSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare HTTP request
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/cache/recache", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.SetContentType("application/json")
	httpReq.Header.Set("X-Internal-Auth", authKey)
	httpReq.SetBody(reqBody)

	// Send request
	client := &fasthttp.Client{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}

	statusCode := httpResp.StatusCode()
	respBody := httpResp.Body()

	// Parse unified API response
	var apiResp httputil.APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, statusCode, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	// Extract data from unified response (may be nil for errors)
	var data types.RecacheAPIData
	if apiResp.Data != nil {
		dataBytes, _ := json.Marshal(apiResp.Data)
		json.Unmarshal(dataBytes, &data)
	}

	// Return data even if success=false (tests check statusCode)
	return &data, statusCode, nil
}

// SendRawRecacheRequest sends raw JSON body (for testing invalid JSON)
func (env *RecacheTestEnvironment) SendRawRecacheRequest(rawBody []byte, authKey string) ([]byte, int, error) {
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/cache/recache", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.SetContentType("application/json")
	httpReq.Header.Set("X-Internal-Auth", authKey)
	httpReq.SetBody(rawBody)

	client := &fasthttp.Client{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}

	statusCode := httpResp.StatusCode()
	respBody := make([]byte, len(httpResp.Body()))
	copy(respBody, httpResp.Body())

	return respBody, statusCode, nil
}

// SendInvalidateRequest sends HTTP POST request to daemon /internal/cache/invalidate endpoint
func (env *RecacheTestEnvironment) SendInvalidateRequest(req types.InvalidateAPIRequest) (*types.InvalidateAPIData, int, error) {
	return env.SendInvalidateRequestWithAuth(req, env.InternalAuthKey)
}

// SendInvalidateRequestWithAuth sends HTTP POST request with custom auth header
func (env *RecacheTestEnvironment) SendInvalidateRequestWithAuth(req types.InvalidateAPIRequest, authKey string) (*types.InvalidateAPIData, int, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/cache/invalidate", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.SetContentType("application/json")
	httpReq.Header.Set("X-Internal-Auth", authKey)
	httpReq.SetBody(reqBody)

	client := &fasthttp.Client{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}

	statusCode := httpResp.StatusCode()
	respBody := httpResp.Body()

	// Parse unified API response
	var apiResp httputil.APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, statusCode, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	// Extract data from unified response (may be nil for errors)
	var data types.InvalidateAPIData
	if apiResp.Data != nil {
		dataBytes, _ := json.Marshal(apiResp.Data)
		json.Unmarshal(dataBytes, &data)
	}

	// Return data even if success=false (tests check statusCode)
	return &data, statusCode, nil
}

// SendRawInvalidateRequest sends raw JSON body (for testing invalid JSON)
func (env *RecacheTestEnvironment) SendRawInvalidateRequest(rawBody []byte, authKey string) ([]byte, int, error) {
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/cache/invalidate", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.SetContentType("application/json")
	httpReq.Header.Set("X-Internal-Auth", authKey)
	httpReq.SetBody(rawBody)

	client := &fasthttp.Client{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}

	statusCode := httpResp.StatusCode()
	respBody := make([]byte, len(httpResp.Body()))
	copy(respBody, httpResp.Body())

	return respBody, statusCode, nil
}

// SendStatusRequest sends HTTP GET request to daemon /status endpoint
func (env *RecacheTestEnvironment) SendStatusRequest() ([]byte, int, error) {
	return env.SendStatusRequestWithAuth(env.InternalAuthKey)
}

// SendStatusRequestWithAuth sends HTTP GET request with custom auth header
func (env *RecacheTestEnvironment) SendStatusRequestWithAuth(authKey string) ([]byte, int, error) {
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/status", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("GET")
	httpReq.Header.Set("X-Internal-Auth", authKey)

	client := &fasthttp.Client{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}

	statusCode := httpResp.StatusCode()
	respBody := make([]byte, len(httpResp.Body()))
	copy(respBody, httpResp.Body())

	return respBody, statusCode, nil
}

// AddMockRSToRegistry adds a mock render service to the Redis registry
func (env *RecacheTestEnvironment) AddMockRSToRegistry(serviceID string, capacity, load int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Registry key format: service:render:{service_id}
	serviceKey := fmt.Sprintf("service:render:%s", serviceID)
	serviceListKey := "services:render:list"

	// Create ServiceInfo structure
	serviceInfo := map[string]interface{}{
		"id":        serviceID,
		"address":   "127.0.0.1",
		"port":      8080,
		"capacity":  capacity,
		"load":      load,
		"status":    "healthy",
		"last_seen": time.Now().Format(time.RFC3339Nano),
	}

	serviceJSON, err := json.Marshal(serviceInfo)
	if err != nil {
		return err
	}

	// Set service info and add to service list
	pipe := env.RedisClient.Pipeline()
	pipe.Set(ctx, serviceKey, string(serviceJSON), 60*time.Second)
	pipe.SAdd(ctx, serviceListKey, serviceID)
	_, err = pipe.Exec(ctx)

	return err
}

// AddMockEGToRegistry adds a mock Edge Gateway to the Redis registry
func (env *RecacheTestEnvironment) AddMockEGToRegistry(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// EG registry key format: registry:eg:{eg_id}
	egID := "test-eg-1"
	registryKey := fmt.Sprintf("registry:eg:%s", egID)

	// Create EGInfo structure
	egInfo := map[string]interface{}{
		"eg_id":            egID,
		"address":          address,
		"last_heartbeat":   time.Now().Format(time.RFC3339Nano),
		"sharding_enabled": true,
	}

	egJSON, err := json.Marshal(egInfo)
	if err != nil {
		return err
	}

	// Set EG info with TTL
	err = env.RedisClient.Set(ctx, registryKey, string(egJSON), 60*time.Second).Err()
	return err
}

// GetInternalQueueSize returns the current size of the daemon's internal queue
func (env *RecacheTestEnvironment) GetInternalQueueSize() int {
	// Access internal queue size through status endpoint
	statusResp, statusCode, err := env.SendStatusRequest()
	if err != nil || statusCode != 200 {
		return 0
	}

	var status map[string]interface{}
	if err := json.Unmarshal(statusResp, &status); err != nil {
		return 0
	}

	if internalQueue, ok := status["internal_queue"].(map[string]interface{}); ok {
		if size, ok := internalQueue["size"].(float64); ok {
			return int(size)
		}
	}

	return 0
}

// SetMockEGResponse configures the next response from mock EG
// success=true means 200 OK, success=false means 500 error
func (env *RecacheTestEnvironment) SetMockEGResponse(success bool) {
	env.MockEGResponses <- success
}

// SetMockEGResponses configures multiple responses from mock EG
func (env *RecacheTestEnvironment) SetMockEGResponses(responses []bool) {
	for _, success := range responses {
		env.MockEGResponses <- success
	}
}

// DrainMockEGReceivedChannel empties the received requests channel
func (env *RecacheTestEnvironment) DrainMockEGReceivedChannel() {
	for {
		select {
		case <-env.MockEGReceivedCh:
			// Drain
		default:
			return
		}
	}
}

// WaitForSchedulerTicks waits for N scheduler ticks (tick = 100ms in tests)
//
// DEPRECATED: Prefer event-based synchronization with Eventually() instead.
// This method makes timing assumptions and wastes time waiting for exact tick counts.
// Use Eventually() to poll for actual state changes (ZSET size, channel messages, etc.)
//
// Only use WaitForSchedulerTicks when specifically testing tick-based timing behavior.
func (env *RecacheTestEnvironment) WaitForSchedulerTicks(n int) {
	tickDuration := 100 * time.Millisecond
	time.Sleep(time.Duration(n) * tickDuration)
}

// PauseScheduler pauses the daemon scheduler via HTTP API
func (env *RecacheTestEnvironment) PauseScheduler() error {
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/scheduler/pause", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.Set("X-Internal-Auth", env.InternalAuthKey)

	client := &fasthttp.Client{
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return fmt.Errorf("failed to pause scheduler: %w", err)
	}

	if httpResp.StatusCode() != 200 {
		return fmt.Errorf("pause scheduler returned status %d: %s", httpResp.StatusCode(), string(httpResp.Body()))
	}

	return nil
}

// ResumeScheduler resumes the daemon scheduler via HTTP API
func (env *RecacheTestEnvironment) ResumeScheduler() error {
	httpReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(httpReq)

	httpResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(httpResp)

	url := fmt.Sprintf("http://127.0.0.1:%d/internal/scheduler/resume", env.DaemonPort)
	httpReq.SetRequestURI(url)
	httpReq.Header.SetMethod("POST")
	httpReq.Header.Set("X-Internal-Auth", env.InternalAuthKey)

	client := &fasthttp.Client{
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	if err := client.Do(httpReq, httpResp); err != nil {
		return fmt.Errorf("failed to resume scheduler: %w", err)
	}

	if httpResp.StatusCode() != 200 {
		return fmt.Errorf("resume scheduler returned status %d: %s", httpResp.StatusCode(), string(httpResp.Body()))
	}

	return nil
}

// DrainChannelUntilCount drains MockEGReceivedCh until expectedCount messages received or timeout
// Returns actual count received and list of requests
func (env *RecacheTestEnvironment) DrainChannelUntilCount(expectedCount int, overallTimeout time.Duration) (int, []RecacheRequestReceived) {
	receivedCount := 0
	requests := make([]RecacheRequestReceived, 0, expectedCount)
	timeout := time.After(overallTimeout)
	idleTimeout := 1 * time.Second // Increased from 300ms to allow for async processing delays

	for receivedCount < expectedCount {
		select {
		case req := <-env.MockEGReceivedCh:
			receivedCount++
			requests = append(requests, req)
		case <-timeout:
			return receivedCount, requests
		case <-time.After(idleTimeout):
			// No message for 1 second, stop draining
			return receivedCount, requests
		}
	}

	return receivedCount, requests
}

// WaitForRegistryReady waits for registry entries to be propagated and queryable
// This ensures the daemon can see registered services before tests proceed
func (env *RecacheTestEnvironment) WaitForRegistryReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Get daemon status which includes RS capacity info from registry
		statusResp, statusCode, err := env.SendStatusRequest()
		if err == nil && statusCode == 200 {
			var status map[string]interface{}
			if err := json.Unmarshal(statusResp, &status); err == nil {
				// Check if RS capacity info is available (indicates registry is readable)
				if rsCapacity, ok := status["rs_capacity"].(map[string]interface{}); ok {
					if totalTabs, ok := rsCapacity["total_free_tabs"].(float64); ok && totalTabs >= 0 {
						// Registry is ready and daemon can query it
						return nil
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for registry to be ready")
}

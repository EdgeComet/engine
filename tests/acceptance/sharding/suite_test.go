package sharding_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/edgecomet/engine/tests/acceptance/sharding/testutil"
)

// TestResponse represents the response from a render request
type TestResponse struct {
	StatusCode int
	Headers    http.Header
	Body       string
	Duration   time.Duration
	Error      error
}

// TestEnvironment manages the test environment
type TestEnvironment struct {
	Config           *testutil.TestEnvironmentConfig
	RedisClient      *redis.Client
	MiniRedis        *miniredis.Miniredis
	HTTPClient       *http.Client
	TestServer       *testutil.TestServer
	EdgeGateway1Cmd  *exec.Cmd
	EdgeGateway2Cmd  *exec.Cmd
	EdgeGateway3Cmd  *exec.Cmd
	RenderServiceCmd *exec.Cmd
	TempConfigDir    string
}

var testEnv *TestEnvironment

func TestShardingAcceptance(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.ParallelTotal = 1
	suiteConfig.Timeout = 60 * time.Minute

	RunSpecs(t, "Sharding Test Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	By("Initializing sharding test environment")
	testEnv = NewTestEnvironment()

	By("Starting local test services")
	err := testEnv.StartServices()
	if err != nil {
		fmt.Printf("\n❌ StartServices failed: %v\n", err)
		Fail(fmt.Sprintf("Failed to start services: %v", err))
	}

	By("Waiting for services to be healthy")
	Eventually(func() bool {
		healthy := testEnv.CheckServicesHealth()
		if !healthy {
			fmt.Printf("⏳ Services not yet healthy, retrying...\n")
		}
		return healthy
	}, 30*time.Second, 1*time.Second).Should(BeTrue())

	fmt.Printf("✅ All services started and healthy\n")
})

var _ = AfterSuite(func() {
	By("Stopping local test services")
	if testEnv != nil {
		testEnv.StopServices()
	}
})

var _ = BeforeEach(func() {
	By("Clearing cache before test")
	if testEnv != nil && testEnv.RedisClient != nil {
		testEnv.ClearCache()
	}
})

func NewTestEnvironment() *TestEnvironment {
	config, err := testutil.LoadTestConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load test config: %v", err))
	}

	return &TestEnvironment{
		Config: config,
		HTTPClient: &http.Client{
			Timeout: config.HTTPClientTimeout(),
		},
	}
}

func (te *TestEnvironment) StartServices() error {
	By("Starting embedded miniredis")
	mr, err := miniredis.Run()
	if err != nil {
		return fmt.Errorf("failed to start miniredis: %v", err)
	}
	te.MiniRedis = mr

	redisAddr := mr.Addr()
	// redisAddr := "127.0.0.1:6379"

	te.RedisClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := te.RedisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to miniredis: %v", err)
	}

	By("Starting local test pages server")
	te.TestServer = testutil.NewTestServer(te.Config.TestServer.Port)
	if err := te.TestServer.Start(); err != nil {
		return fmt.Errorf("failed to start test pages server: %v", err)
	}

	By("Creating temporary config for services")
	tempConfigDir, err := os.MkdirTemp("", "edgecomet-sharding-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	te.TempConfigDir = tempConfigDir

	configBuilder := testutil.NewConfigBuilder(te.Config, redisAddr)
	if err := configBuilder.WriteTestConfigs(tempConfigDir); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to write test configs: %v", err)
	}

	By("Starting Render Service")
	projectRoot := filepath.Join("..", "..", "..")
	renderServicePath := filepath.Join(projectRoot, "cmd", "render-service")
	rsConfigPath := filepath.Join(tempConfigDir, "render-service.yaml")

	rsCmd := exec.Command("go", "run", ".", "-c", rsConfigPath)
	rsCmd.Dir = renderServicePath
	rsCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != "" {
		rsCmd.Stdout = os.Stdout
		rsCmd.Stderr = os.Stderr
	} else {
		rsCmd.Stdout = io.Discard
		rsCmd.Stderr = io.Discard
	}

	if err := rsCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Render Service: %v", err)
	}
	te.RenderServiceCmd = rsCmd

	By("Waiting for Render Service to be ready")
	if err := te.waitForRenderService(30 * time.Second); err != nil {
		if rsCmd.Process != nil {
			rsCmd.Process.Kill()
		}
		return fmt.Errorf("Render Service failed to become ready: %v", err)
	}

	By("Starting Edge Gateway 1 (EG1)")
	edgeGatewayPath := filepath.Join(projectRoot, "cmd", "edge-gateway")
	eg1ConfigPath := filepath.Join(tempConfigDir, "eg1", "edge-gateway.yaml")

	eg1Cmd := exec.Command("go", "run", ".", "-c", eg1ConfigPath)
	eg1Cmd.Dir = edgeGatewayPath
	eg1Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create pipes to monitor output for startup message
	eg1Stdout, err := eg1Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for EG1: %w", err)
	}
	eg1Stderr, err := eg1Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe for EG1: %w", err)
	}

	debugMode := os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != ""
	eg1StartupFound := waitForLogMessage(eg1Stdout, eg1Stderr, "Edge Gateway started", debugMode)

	if err := eg1Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Edge Gateway 1: %v", err)
	}
	te.EdgeGateway1Cmd = eg1Cmd

	// Wait for startup message
	select {
	case <-eg1StartupFound:
		// Startup message detected
	case <-time.After(10 * time.Second):
		// Timeout - continue with fallback checks
	}

	By("Waiting for Edge Gateway 1 to register in Redis")
	if err := te.waitForEGRegistry("eg1", 10*time.Second); err != nil {
		if eg1Cmd.Process != nil {
			eg1Cmd.Process.Kill()
		}
		return fmt.Errorf("Edge Gateway 1 failed to register: %v", err)
	}

	By("Starting Edge Gateway 2 (EG2)")
	eg2ConfigPath := filepath.Join(tempConfigDir, "eg2", "edge-gateway.yaml")

	eg2Cmd := exec.Command("go", "run", ".", "-c", eg2ConfigPath)
	eg2Cmd.Dir = edgeGatewayPath
	eg2Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create pipes to monitor output for startup message
	eg2Stdout, err := eg2Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for EG2: %w", err)
	}
	eg2Stderr, err := eg2Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe for EG2: %w", err)
	}

	eg2StartupFound := waitForLogMessage(eg2Stdout, eg2Stderr, "Edge Gateway started", debugMode)

	if err := eg2Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Edge Gateway 2: %v", err)
	}
	te.EdgeGateway2Cmd = eg2Cmd

	// Wait for startup message
	select {
	case <-eg2StartupFound:
		// Startup message detected
	case <-time.After(10 * time.Second):
		// Timeout - continue with fallback checks
	}

	By("Waiting for Edge Gateway 2 to register in Redis")
	if err := te.waitForEGRegistry("eg2", 10*time.Second); err != nil {
		if eg2Cmd.Process != nil {
			eg2Cmd.Process.Kill()
		}
		return fmt.Errorf("Edge Gateway 2 failed to register: %v", err)
	}

	By("Starting Edge Gateway 3 (EG3)")
	eg3ConfigPath := filepath.Join(tempConfigDir, "eg3", "edge-gateway.yaml")

	eg3Cmd := exec.Command("go", "run", ".", "-c", eg3ConfigPath)
	eg3Cmd.Dir = edgeGatewayPath
	eg3Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create pipes to monitor output for startup message
	eg3Stdout, err := eg3Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for EG3: %w", err)
	}
	eg3Stderr, err := eg3Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe for EG3: %w", err)
	}

	eg3StartupFound := waitForLogMessage(eg3Stdout, eg3Stderr, "Edge Gateway started", debugMode)

	if err := eg3Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Edge Gateway 3: %v", err)
	}
	te.EdgeGateway3Cmd = eg3Cmd

	// Wait for startup message
	select {
	case <-eg3StartupFound:
		// Startup message detected
	case <-time.After(10 * time.Second):
		// Timeout - continue with fallback checks
	}

	By("Waiting for Edge Gateway 3 to register in Redis")
	if err := te.waitForEGRegistry("eg3", 10*time.Second); err != nil {
		if eg3Cmd.Process != nil {
			eg3Cmd.Process.Kill()
		}
		return fmt.Errorf("Edge Gateway 3 failed to register: %v", err)
	}

	By("Verifying cluster size is 3")
	if err := te.WaitForClusterSize(3, 10*time.Second); err != nil {
		return fmt.Errorf("failed to reach cluster size 3: %v", err)
	}

	return nil
}

func (te *TestEnvironment) StopServices() error {
	// Stop all 3 Edge Gateways
	for i, egCmd := range []*exec.Cmd{te.EdgeGateway1Cmd, te.EdgeGateway2Cmd, te.EdgeGateway3Cmd} {
		if egCmd != nil && egCmd.Process != nil {
			egNum := i + 1
			pgid, err := syscall.Getpgid(egCmd.Process.Pid)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGTERM)
			} else {
				egCmd.Process.Signal(os.Interrupt)
			}

			done := make(chan error, 1)
			go func() {
				done <- egCmd.Wait()
			}()

			select {
			case <-done:
				fmt.Printf("Edge Gateway %d stopped gracefully\n", egNum)
			case <-time.After(3 * time.Second):
				if pgid, err := syscall.Getpgid(egCmd.Process.Pid); err == nil {
					syscall.Kill(-pgid, syscall.SIGKILL)
				} else {
					egCmd.Process.Kill()
				}
				fmt.Printf("Edge Gateway %d forcefully killed\n", egNum)
			}
		}
	}

	if te.RenderServiceCmd != nil && te.RenderServiceCmd.Process != nil {
		pgid, err := syscall.Getpgid(te.RenderServiceCmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			te.RenderServiceCmd.Process.Signal(os.Interrupt)
		}

		done := make(chan error, 1)
		go func() {
			done <- te.RenderServiceCmd.Wait()
		}()

		select {
		case <-done:
			// Process exited gracefully
			// Wait for actual binary to stop (not just the 'go run' wrapper)
			te.waitForProcessExit("render-service", 5*time.Second)
		case <-time.After(5 * time.Second):
			// Force kill if graceful shutdown times out
			fmt.Println("Warning: Render Service didn't stop gracefully, forcing kill")
			if pgid, err := syscall.Getpgid(te.RenderServiceCmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				te.RenderServiceCmd.Process.Kill()
			}
			// Wait for process to actually die
			te.waitForProcessExit("render-service", 3*time.Second)
		}
	}

	// IMPORTANT: Wait for RS background goroutines to finish before closing Redis
	// The Render Service has heartbeat loops and deregistration processes that need
	// time to complete gracefully. The waitForProcessExit call above ensures the
	// actual binary (not just go run wrapper) has exited, but we add a small buffer
	// to ensure all goroutines have fully stopped before Redis closes.
	time.Sleep(1 * time.Second)

	if te.RedisClient != nil {
		te.RedisClient.Close()
	}

	if te.MiniRedis != nil {
		te.MiniRedis.Close()
	}

	if te.TempConfigDir != "" {
		os.RemoveAll(te.TempConfigDir)
	}

	if te.TestServer != nil {
		if err := te.TestServer.Stop(); err != nil {
			fmt.Printf("Warning: failed to stop test server: %v\n", err)
		}
	}

	return nil
}

func (te *TestEnvironment) waitForRenderService(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(te.Config.RSBaseURL() + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Render Service did not become ready within %v", timeout)
}

func (te *TestEnvironment) waitForEdgeGateway(egNum int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	var baseURL string

	switch egNum {
	case 1:
		baseURL = te.Config.EG1BaseURL()
	case 2:
		baseURL = te.Config.EG2BaseURL()
	case 3:
		baseURL = te.Config.EG3BaseURL()
	default:
		return fmt.Errorf("invalid EG number: %d", egNum)
	}

	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Edge Gateway %d did not become ready within %v", egNum, timeout)
}

func (te *TestEnvironment) waitForEGRegistry(egID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ctx := context.Background()

	registryKey := fmt.Sprintf("registry:eg:%s", egID)

	for time.Now().Before(deadline) {
		// Registry data is stored as JSON, not hash fields
		data, err := te.RedisClient.Get(ctx, registryKey).Result()
		if err == nil && data != "" {
			// Successfully retrieved registry entry
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("EG %s did not register in Redis within %v", egID, timeout)
}

func (te *TestEnvironment) WaitForClusterSize(expectedSize int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ctx := context.Background()

	for time.Now().Before(deadline) {
		keys, err := te.RedisClient.Keys(ctx, "registry:eg:*").Result()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if len(keys) == expectedSize {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("cluster size did not reach %d within %v", expectedSize, timeout)
}

func (te *TestEnvironment) CheckServicesHealth() bool {
	if te.RedisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := te.RedisClient.Ping(ctx).Result(); err != nil {
		return false
	}

	resp, err := te.HTTPClient.Get(te.Config.TestPagesURL() + "/static/test.html")
	if err != nil {
		return false
	}
	resp.Body.Close()

	return resp.StatusCode == 200
}

func (te *TestEnvironment) RequestRender(targetURL string) *TestResponse {
	return te.RequestRenderWithAPIKey(targetURL, te.Config.Test.ValidAPIKey)
}

func (te *TestEnvironment) RequestRenderWithAPIKey(targetURL string, apiKey string) *TestResponse {
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", apiKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")

	start := time.Now()
	resp, err := te.HTTPClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &TestResponse{Error: err, Duration: duration}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Duration:   duration,
			Error:      err,
		}
	}

	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(body),
		Duration:   duration,
	}
}

func (te *TestEnvironment) ClearCache() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	patterns := []string{"meta:cache:*", "lock:cache:*", "cache:*"}

	for _, pattern := range patterns {
		keys, err := te.RedisClient.Keys(ctx, pattern).Result()
		if err != nil {
			return fmt.Errorf("failed to get keys for pattern %s: %w", pattern, err)
		}

		if len(keys) > 0 {
			if err := te.RedisClient.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("failed to delete keys for pattern %s: %w", pattern, err)
			}
		}
	}

	return nil
}

// waitForProcessExit waits for a process with the given name to fully exit
// This is needed because 'go run' creates a wrapper that exits before the actual binary
func (te *TestEnvironment) waitForProcessExit(processName string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Use ps to check if the process is still running
		cmd := exec.Command("ps", "aux")
		output, err := cmd.Output()
		if err != nil {
			// If ps fails, assume process is gone
			return
		}

		// Check if our process name appears in the output
		if !strings.Contains(string(output), processName) {
			// Process not found, it has exited
			return
		}

		// Process still running, wait a bit
		time.Sleep(100 * time.Millisecond)
	}

	// Timeout reached - log warning but continue
	fmt.Printf("Warning: Process '%s' still running after %v timeout\n", processName, timeout)
}

// ============================================================================
// Request Helpers (via specific EGs)
// ============================================================================

// RequestViaEG1 sends a render request through Edge Gateway 1
// Optional requestID parameter can be provided for debugging/tracing
func (te *TestEnvironment) RequestViaEG1(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEG(targetURL, te.Config.EG1BaseURL(), requestID...)
}

// RequestViaEG2 sends a render request through Edge Gateway 2
// Optional requestID parameter can be provided for debugging/tracing
func (te *TestEnvironment) RequestViaEG2(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEG(targetURL, te.Config.EG2BaseURL(), requestID...)
}

// RequestViaEG3 sends a render request through Edge Gateway 3
// Optional requestID parameter can be provided for debugging/tracing
func (te *TestEnvironment) RequestViaEG3(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEG(targetURL, te.Config.EG3BaseURL(), requestID...)
}

// RequestViaEG1NoRedirect sends a request through EG1 without following redirects
// Useful for testing redirect responses (301, 302, etc.) with bypass cache
func (te *TestEnvironment) RequestViaEG1NoRedirect(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEGNoRedirect(targetURL, te.Config.EG1BaseURL(), requestID...)
}

// RequestViaEG2NoRedirect sends a request through EG2 without following redirects
func (te *TestEnvironment) RequestViaEG2NoRedirect(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEGNoRedirect(targetURL, te.Config.EG2BaseURL(), requestID...)
}

// RequestViaEG3NoRedirect sends a request through EG3 without following redirects
func (te *TestEnvironment) RequestViaEG3NoRedirect(targetURL string, requestID ...string) *TestResponse {
	return te.requestViaEGNoRedirect(targetURL, te.Config.EG3BaseURL(), requestID...)
}

// requestViaEG is the internal helper that routes requests through a specific EG
func (te *TestEnvironment) requestViaEG(targetURL string, egBaseURL string, requestID ...string) *TestResponse {
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	req, err := http.NewRequest("GET", egBaseURL+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", te.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")

	// Set X-Request-ID header if provided for debugging
	if len(requestID) > 0 && requestID[0] != "" {
		req.Header.Set("X-Request-ID", requestID[0])
	}

	start := time.Now()
	resp, err := te.HTTPClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &TestResponse{Error: err, Duration: duration}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Duration:   duration,
			Error:      err,
		}
	}

	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(body),
		Duration:   duration,
	}
}

// requestViaEGNoRedirect is the internal helper that routes requests through a specific EG without following redirects
// Useful for testing redirect responses (301, 302, etc.) with bypass cache
func (te *TestEnvironment) requestViaEGNoRedirect(targetURL string, egBaseURL string, requestID ...string) *TestResponse {
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	req, err := http.NewRequest("GET", egBaseURL+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", te.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")

	// Set X-Request-ID header if provided for debugging
	if len(requestID) > 0 && requestID[0] != "" {
		req.Header.Set("X-Request-ID", requestID[0])
	}

	// Create HTTP client that doesn't follow redirects
	noRedirectClient := &http.Client{
		Timeout: te.Config.HTTPClientTimeout(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	start := time.Now()
	resp, err := noRedirectClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &TestResponse{Error: err, Duration: duration}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Duration:   duration,
			Error:      err,
		}
	}

	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(body),
		Duration:   duration,
	}
}

// ============================================================================
// Redis Metadata Helpers
// ============================================================================

// BuildCacheKey constructs a Redis metadata key in the format meta:cache:{host_id}:{dimension_id}:{url_hash}
func (te *TestEnvironment) BuildCacheKey(hostID, dimensionID int, urlHash string) string {
	return fmt.Sprintf("meta:cache:%d:%d:%s", hostID, dimensionID, urlHash)
}

// GetRedisMetadata fetches the full metadata hash from Redis for a given cache key
func (te *TestEnvironment) GetRedisMetadata(cacheKey string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := te.RedisClient.HGetAll(ctx, cacheKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata for key %s: %w", cacheKey, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("no metadata found for key %s", cacheKey)
	}

	return data, nil
}

// GetCacheMetadata is a convenience wrapper that builds the cache key and fetches metadata
func (te *TestEnvironment) GetCacheMetadata(hostID, dimensionID int, urlHash string) (map[string]string, error) {
	cacheKey := te.BuildCacheKey(hostID, dimensionID, urlHash)
	return te.GetRedisMetadata(cacheKey)
}

// GetEGIDs parses the eg_ids field from cache metadata and returns a slice of EG IDs
func (te *TestEnvironment) GetEGIDs(cacheKey string) ([]string, error) {
	metadata, err := te.GetRedisMetadata(cacheKey)
	if err != nil {
		return nil, err
	}

	egIDsStr, exists := metadata["eg_ids"]
	if !exists || egIDsStr == "" {
		return []string{}, nil
	}

	// Split by comma and trim whitespace
	parts := strings.Split(egIDsStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result, nil
}

// WaitForStableEGIDs waits for eg_ids to stabilize (complete cluster replication)
// This handles the race condition between initial metadata write and cluster push completion
// Returns the stable eg_ids list or error if timeout occurs
func (te *TestEnvironment) WaitForStableEGIDs(cacheKey string, timeout time.Duration) ([]string, error) {
	deadline := time.Now().Add(timeout)
	var lastEgIDs []string
	var stableCount int

	// Poll with increasing delays: 50ms, 100ms, 200ms
	delays := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	delayIndex := 0

	for time.Now().Before(deadline) {
		currentEgIDs, err := te.GetEGIDs(cacheKey)
		if err != nil {
			// Metadata doesn't exist yet, wait and retry
			time.Sleep(delays[delayIndex])
			if delayIndex < len(delays)-1 {
				delayIndex++
			}
			continue
		}

		// Check if eg_ids changed since last check
		if egIDsEqual(lastEgIDs, currentEgIDs) {
			stableCount++
			// Consider stable after 2 consecutive identical reads
			if stableCount >= 2 {
				return currentEgIDs, nil
			}
		} else {
			// eg_ids changed, reset stability counter
			stableCount = 0
			lastEgIDs = currentEgIDs
		}

		// Use exponential backoff
		time.Sleep(delays[delayIndex])
		if delayIndex < len(delays)-1 {
			delayIndex++
		}
	}

	// Timeout - return last seen value if available
	if lastEgIDs != nil {
		return lastEgIDs, fmt.Errorf("timeout waiting for stable eg_ids (last seen: %v)", lastEgIDs)
	}
	return nil, fmt.Errorf("timeout waiting for eg_ids to appear")
}

// egIDsEqual compares two eg_ids slices for equality (order-independent)
func egIDsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Convert to maps for order-independent comparison
	aMap := make(map[string]bool)
	for _, id := range a {
		aMap[id] = true
	}

	for _, id := range b {
		if !aMap[id] {
			return false
		}
	}

	return true
}

// ============================================================================
// Internal API Helpers
// ============================================================================

// InternalStatusResponse represents the response from /internal/cache/status
type InternalStatusResponse struct {
	EgID                 string   `json:"eg_id"`
	ShardingEnabled      bool     `json:"sharding_enabled"`
	ReplicationFactor    int      `json:"replication_factor"`
	DistributionStrategy string   `json:"distribution_strategy"`
	LocalCacheCount      int      `json:"local_cache_count"`
	LocalCacheSizeBytes  int64    `json:"local_cache_size_bytes"`
	AvailableEGs         []string `json:"available_egs"`
	ClusterSize          int      `json:"cluster_size"`
	UptimeSeconds        int64    `json:"uptime_seconds"`
}

// GetInternalStatus queries the /internal/cache/status endpoint on a specific EG
func (te *TestEnvironment) GetInternalStatus(egNum int) (*InternalStatusResponse, error) {
	resp, err := te.CallInternalAPI(egNum, "/internal/cache/status", "GET", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var status InternalStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}

	return &status, nil
}

// CallInternalAPI makes an authenticated request to a specific EG's internal API
func (te *TestEnvironment) CallInternalAPI(egNum int, path string, method string, body []byte) (*http.Response, error) {
	return te.CallInternalAPIWithAuth(egNum, path, method, body, "test-shared-secret-key-12345678")
}

// CallInternalAPIWithAuth calls an internal API endpoint with custom auth key (for testing auth failures)
func (te *TestEnvironment) CallInternalAPIWithAuth(egNum int, path string, method string, body []byte, authKey string) (*http.Response, error) {
	var internalBaseURL string

	switch egNum {
	case 1:
		internalBaseURL = te.Config.EG1InternalBaseURL()
	case 2:
		internalBaseURL = te.Config.EG2InternalBaseURL()
	case 3:
		internalBaseURL = te.Config.EG3InternalBaseURL()
	default:
		return nil, fmt.Errorf("invalid EG number: %d (must be 1, 2, or 3)", egNum)
	}

	fullURL := internalBaseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set internal auth header (use custom auth key for testing, or skip if empty)
	if authKey != "" {
		req.Header.Set("X-Internal-Auth", authKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return te.HTTPClient.Do(req)
}

// ============================================================================
// Registry Helpers
// ============================================================================

// GetClusterSize returns the number of active EG registrations in Redis
func (te *TestEnvironment) GetClusterSize() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	keys, err := te.RedisClient.Keys(ctx, "registry:eg:*").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get registry keys: %w", err)
	}

	return len(keys), nil
}

// GetRegistryInfo fetches the registry data for a specific EG ID
func (te *TestEnvironment) GetRegistryInfo(egID string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	registryKey := fmt.Sprintf("registry:eg:%s", egID)

	data, err := te.RedisClient.Get(ctx, registryKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get registry info for %s: %w", egID, err)
	}

	var registryData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &registryData); err != nil {
		return nil, fmt.Errorf("failed to parse registry data for %s: %w", egID, err)
	}

	return registryData, nil
}

// WaitForEGOffline waits for an EG's registry entry to expire (TTL-based removal)
func (te *TestEnvironment) WaitForEGOffline(egID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ctx := context.Background()

	registryKey := fmt.Sprintf("registry:eg:%s", egID)

	for time.Now().Before(deadline) {
		exists, err := te.RedisClient.Exists(ctx, registryKey).Result()
		if err != nil {
			return fmt.Errorf("failed to check registry key existence: %w", err)
		}

		if exists == 0 {
			// Key has expired
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("EG %s did not go offline within %v", egID, timeout)
}

// ============================================================================
// Process Control Helpers
// ============================================================================

// IsEGRunning checks if a specific EG process is still running
func (te *TestEnvironment) IsEGRunning(egNum int) bool {
	var egCmd *exec.Cmd

	switch egNum {
	case 1:
		egCmd = te.EdgeGateway1Cmd
	case 2:
		egCmd = te.EdgeGateway2Cmd
	case 3:
		egCmd = te.EdgeGateway3Cmd
	default:
		return false
	}

	if egCmd == nil || egCmd.Process == nil {
		return false
	}

	// Check if process has exited
	if egCmd.ProcessState != nil {
		return false
	}

	return true
}

// isPortAvailable checks if a TCP port is available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false // Port is not available
	}
	listener.Close()
	return true // Port is available
}

// waitForPortRelease waits for a port to become available
func waitForPortRelease(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isPortAvailable(port) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("port %d was not released within %v", port, timeout)
}

// StopEG gracefully stops a specific Edge Gateway instance (SIGTERM)
func (te *TestEnvironment) StopEG(egNum int) error {
	var egCmd *exec.Cmd
	var egID string
	var port int

	switch egNum {
	case 1:
		egCmd = te.EdgeGateway1Cmd
		egID = "EG1"
		port = 9201
	case 2:
		egCmd = te.EdgeGateway2Cmd
		egID = "EG2"
		port = 9211
	case 3:
		egCmd = te.EdgeGateway3Cmd
		egID = "EG3"
		port = 9221
	default:
		return fmt.Errorf("invalid EG number: %d (must be 1, 2, or 3)", egNum)
	}

	if egCmd == nil || egCmd.Process == nil {
		return fmt.Errorf("%s is not running", egID)
	}

	// Send SIGTERM for graceful shutdown
	pgid, err := syscall.Getpgid(egCmd.Process.Pid)
	if err == nil {
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to %s: %w", egID, err)
		}
	} else {
		if err := egCmd.Process.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("failed to interrupt %s: %w", egID, err)
		}
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		done <- egCmd.Wait()
	}()

	select {
	case <-done:
		if os.Getenv("DEBUG") != "" {
			fmt.Printf("%s stopped gracefully\n", egID)
		}

		// Wait for port to be released before returning
		if err := waitForPortRelease(port, 3*time.Second); err != nil {
			return fmt.Errorf("%s process stopped but port not released: %w", egID, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("%s port %d released\n", egID, port)
		}

		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("%s did not stop within 5 seconds", egID)
	}
}

// KillEG force kills a specific Edge Gateway instance (SIGKILL)
func (te *TestEnvironment) KillEG(egNum int) error {
	var egCmd *exec.Cmd
	var egID string

	switch egNum {
	case 1:
		egCmd = te.EdgeGateway1Cmd
		egID = "EG1"
	case 2:
		egCmd = te.EdgeGateway2Cmd
		egID = "EG2"
	case 3:
		egCmd = te.EdgeGateway3Cmd
		egID = "EG3"
	default:
		return fmt.Errorf("invalid EG number: %d (must be 1, 2, or 3)", egNum)
	}

	if egCmd == nil || egCmd.Process == nil {
		return fmt.Errorf("%s is not running", egID)
	}

	// Send SIGKILL for immediate termination
	pgid, err := syscall.Getpgid(egCmd.Process.Pid)
	if err == nil {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL to %s: %w", egID, err)
		}
	} else {
		if err := egCmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill %s: %w", egID, err)
		}
	}

	// Wait for process to actually die to avoid zombie process
	done := make(chan error, 1)
	go func() {
		done <- egCmd.Wait()
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(2 * time.Second):
		// Process didn't exit, but that's okay for SIGKILL
	}

	if os.Getenv("DEBUG") != "" {
		fmt.Printf("%s killed forcefully\n", egID)
	}
	return nil
}

// waitForLogMessage monitors command output pipes for a specific message
// Returns a channel that closes when the message is found
func waitForLogMessage(stdout, stderr io.ReadCloser, targetMsg string, debugMode bool) <-chan struct{} {
	found := make(chan struct{})
	var once sync.Once

	// Helper to scan a stream and look for target message
	scanStream := func(stream io.ReadCloser, streamName string) {
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()

			// Echo to console in DEBUG mode
			if debugMode {
				if streamName == "stderr" {
					fmt.Fprintln(os.Stderr, line)
				} else {
					fmt.Println(line)
				}
			}

			// Check for target message
			if strings.Contains(line, targetMsg) {
				once.Do(func() {
					close(found)
				})
			}
		}
	}

	// Monitor both stdout and stderr in parallel
	go scanStream(stdout, "stdout")
	go scanStream(stderr, "stderr")

	return found
}

// waitForStartupOrError monitors logs for either successful startup or FATAL error
// Returns (successChannel, errorChannel, errorMessage)
func waitForStartupOrError(stdout, stderr io.ReadCloser, debugMode bool) (<-chan struct{}, <-chan string) {
	successChan := make(chan struct{})
	errorChan := make(chan string, 1)
	var successOnce, errorOnce sync.Once
	var fatalErrorMsg string

	// Helper to scan a stream
	scanStream := func(stream io.ReadCloser, streamName string) {
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()

			// Echo to console in DEBUG mode
			if debugMode {
				if streamName == "stderr" {
					fmt.Fprintln(os.Stderr, line)
				} else {
					fmt.Println(line)
				}
			}

			// Check for FATAL error first (higher priority)
			if strings.Contains(line, "FATAL") {
				errorOnce.Do(func() {
					// Extract error message from FATAL line
					if idx := strings.Index(line, "\"error\":"); idx != -1 {
						// Try to extract the error message value
						errorPart := line[idx+len("\"error\":"):]
						if endIdx := strings.Index(errorPart, "\"}"); endIdx != -1 {
							fatalErrorMsg = strings.Trim(errorPart[:endIdx], " \"")
						} else {
							fatalErrorMsg = "FATAL error detected in startup logs"
						}
					} else {
						fatalErrorMsg = "FATAL error detected in startup logs"
					}
					errorChan <- fatalErrorMsg
				})
				return
			}

			// Check for successful startup
			if strings.Contains(line, "Edge Gateway started") {
				successOnce.Do(func() {
					close(successChan)
				})
			}
		}
	}

	// Monitor both stdout and stderr in parallel
	go scanStream(stdout, "stdout")
	go scanStream(stderr, "stderr")

	return successChan, errorChan
}

// isShardingEnabled checks if cache_sharding is enabled in an EG's config
func (te *TestEnvironment) isShardingEnabled(egNum int) (bool, error) {
	var configPath string

	switch egNum {
	case 1:
		configPath = filepath.Join(te.TempConfigDir, "eg1", "edge-gateway.yaml")
	case 2:
		configPath = filepath.Join(te.TempConfigDir, "eg2", "edge-gateway.yaml")
	case 3:
		configPath = filepath.Join(te.TempConfigDir, "eg3", "edge-gateway.yaml")
	default:
		return false, fmt.Errorf("invalid EG number: %d (must be 1, 2, or 3)", egNum)
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("failed to read config: %w", err)
	}

	// Parse YAML
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(data, &configMap); err != nil {
		return false, fmt.Errorf("failed to parse config: %w", err)
	}

	// Check cache_sharding.enabled field
	if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
		if enabled, ok := cacheSharding["enabled"].(bool); ok {
			return enabled, nil
		}
	}

	// Default to true if field not found (backward compatibility)
	return true, nil
}

// StartEG starts a specific Edge Gateway instance
func (te *TestEnvironment) StartEG(egNum int) error {
	projectRoot := filepath.Join("..", "..", "..")
	edgeGatewayPath := filepath.Join(projectRoot, "cmd", "edge-gateway")

	var configPath string
	var egID string
	var egCmd **exec.Cmd

	switch egNum {
	case 1:
		configPath = filepath.Join(te.TempConfigDir, "eg1", "edge-gateway.yaml")
		egID = "EG1"
		egCmd = &te.EdgeGateway1Cmd
	case 2:
		configPath = filepath.Join(te.TempConfigDir, "eg2", "edge-gateway.yaml")
		egID = "EG2"
		egCmd = &te.EdgeGateway2Cmd
	case 3:
		configPath = filepath.Join(te.TempConfigDir, "eg3", "edge-gateway.yaml")
		egID = "EG3"
		egCmd = &te.EdgeGateway3Cmd
	default:
		return fmt.Errorf("invalid EG number: %d (must be 1, 2, or 3)", egNum)
	}

	cmd := exec.Command("go", "run", ".", "-c", configPath)
	cmd.Dir = edgeGatewayPath
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create pipes to monitor output for startup message
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for %s: %w", egID, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe for %s: %w", egID, err)
	}

	// Determine if we're in debug mode
	debugMode := os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != ""

	// Start monitoring for startup success or FATAL error
	successChan, errorChan := waitForStartupOrError(stdout, stderr, debugMode)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", egID, err)
	}

	*egCmd = cmd

	// Wait for either successful startup, FATAL error, or timeout
	select {
	case <-successChan:
		if os.Getenv("DEBUG") != "" {
			fmt.Printf("%s startup message detected\n", egID)
		}
		// Continue to health check verification below
	case fatalError := <-errorChan:
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("%s failed to start: %s", egID, fatalError)
	case <-time.After(10 * time.Second):
		// Timeout - log warning but continue with health check fallback
		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Warning: %s startup message not detected within 10s, falling back to health check\n", egID)
		}
	}

	// Wait for EG to be ready (longer timeout for restarts)
	// Skip health check if we already detected a FATAL error above
	select {
	case fatalError := <-errorChan:
		// FATAL error detected after initial check
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("%s failed to start: %s", egID, fatalError)
	default:
		// No error detected, proceed with health check
		if err := te.waitForEdgeGateway(egNum, 20*time.Second); err != nil {
			// Check once more for FATAL error before returning generic error
			select {
			case fatalError := <-errorChan:
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				return fmt.Errorf("%s failed to start: %s", egID, fatalError)
			default:
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				return fmt.Errorf("%s failed to become ready: %w", egID, err)
			}
		}
	}

	// Check if sharding is enabled for this EG
	shardingEnabled, err := te.isShardingEnabled(egNum)
	if err != nil {
		return fmt.Errorf("failed to check sharding status: %w", err)
	}

	// Only wait for registry if sharding is enabled
	// When sharding is disabled, EGs don't register in Redis
	if shardingEnabled {
		var registryEgID string
		switch egNum {
		case 1:
			registryEgID = "eg1"
		case 2:
			registryEgID = "eg2"
		case 3:
			registryEgID = "eg3"
		}

		if err := te.waitForEGRegistry(registryEgID, 10*time.Second); err != nil {
			return fmt.Errorf("%s failed to register: %w", egID, err)
		}
	} else {
		if os.Getenv("DEBUG") != "" {
			fmt.Printf("%s running with sharding disabled (skipping registry check)\n", egID)
		}
	}

	if os.Getenv("DEBUG") != "" {
		fmt.Println("================================================")
		fmt.Printf("%s started fully successfully\n", egID)
	}

	return nil
}

// RestartEG stops and then starts a specific Edge Gateway instance
func (te *TestEnvironment) RestartEG(egNum int) error {
	if err := te.StopEG(egNum); err != nil {
		return fmt.Errorf("failed to stop EG%d: %w", egNum, err)
	}

	// Wait a moment for cleanup
	time.Sleep(500 * time.Millisecond)

	if err := te.StartEG(egNum); err != nil {
		return fmt.Errorf("failed to start EG%d: %w", egNum, err)
	}

	return nil
}

// ============================================================================
// Verification Helpers
// ============================================================================

// VerifyEGIDsContain verifies that the eg_ids field contains all expected EG IDs
func (te *TestEnvironment) VerifyEGIDsContain(cacheKey string, expectedEGIDs ...string) error {
	actualEGIDs, err := te.GetEGIDs(cacheKey)
	if err != nil {
		return fmt.Errorf("failed to get eg_ids: %w", err)
	}

	// Convert actual EG IDs to a map for quick lookup
	actualMap := make(map[string]bool)
	for _, egID := range actualEGIDs {
		actualMap[egID] = true
	}

	// Check that all expected EG IDs are present
	missing := []string{}
	for _, expectedID := range expectedEGIDs {
		if !actualMap[expectedID] {
			missing = append(missing, expectedID)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("eg_ids missing expected IDs: %v (actual: %v)", missing, actualEGIDs)
	}

	return nil
}

// VerifyEGIDsCount verifies that the eg_ids field has exactly the expected count
func (te *TestEnvironment) VerifyEGIDsCount(cacheKey string, expectedCount int) error {
	actualEGIDs, err := te.GetEGIDs(cacheKey)
	if err != nil {
		return fmt.Errorf("failed to get eg_ids: %w", err)
	}

	actualCount := len(actualEGIDs)
	if actualCount != expectedCount {
		return fmt.Errorf("eg_ids count mismatch: expected %d, got %d (actual: %v)",
			expectedCount, actualCount, actualEGIDs)
	}

	return nil
}

// VerifyCacheDistribution verifies that eg_ids exactly matches the expected set (order-independent)
func (te *TestEnvironment) VerifyCacheDistribution(cacheKey string, expectedEGIDs []string) error {
	actualEGIDs, err := te.GetEGIDs(cacheKey)
	if err != nil {
		return fmt.Errorf("failed to get eg_ids: %w", err)
	}

	// Check count first
	if len(actualEGIDs) != len(expectedEGIDs) {
		return fmt.Errorf("eg_ids count mismatch: expected %d, got %d (expected: %v, actual: %v)",
			len(expectedEGIDs), len(actualEGIDs), expectedEGIDs, actualEGIDs)
	}

	// Convert to maps for order-independent comparison
	actualMap := make(map[string]bool)
	for _, egID := range actualEGIDs {
		actualMap[egID] = true
	}

	expectedMap := make(map[string]bool)
	for _, egID := range expectedEGIDs {
		expectedMap[egID] = true
	}

	// Check for missing IDs in actual
	missing := []string{}
	for expectedID := range expectedMap {
		if !actualMap[expectedID] {
			missing = append(missing, expectedID)
		}
	}

	// Check for unexpected IDs in actual
	unexpected := []string{}
	for actualID := range actualMap {
		if !expectedMap[actualID] {
			unexpected = append(unexpected, actualID)
		}
	}

	if len(missing) > 0 || len(unexpected) > 0 {
		return fmt.Errorf("eg_ids mismatch: missing=%v, unexpected=%v (expected: %v, actual: %v)",
			missing, unexpected, expectedEGIDs, actualEGIDs)
	}

	return nil
}

// ============================================================================
// Configuration Modification Helpers
// ============================================================================

// UpdateEGDistributionStrategy modifies the distribution_strategy in all 3 EG configs
func (te *TestEnvironment) UpdateEGDistributionStrategy(strategy string) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	// Update all 3 EG configs
	for i := 1; i <= 3; i++ {
		egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", i))
		configPath := filepath.Join(egDir, "edge-gateway.yaml")

		// Read existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read EG%d config: %w", i, err)
		}

		// Parse YAML
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(data), &configMap); err != nil {
			// Try YAML unmarshal if JSON fails
			if err := yaml.Unmarshal(data, &configMap); err != nil {
				return fmt.Errorf("failed to parse EG%d config: %w", i, err)
			}
		}

		// Modify distribution_strategy
		if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
			cacheSharding["distribution_strategy"] = strategy
		} else {
			return fmt.Errorf("cache_sharding section not found in EG%d config", i)
		}

		// Write back as YAML
		updatedData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal EG%d config: %w", i, err)
		}

		if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
			return fmt.Errorf("failed to write EG%d config: %w", i, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Updated EG%d config: distribution_strategy=%s\n", i, strategy)
		}
	}

	return nil
}

// RestartAllEGs restarts all 3 Edge Gateways sequentially
func (te *TestEnvironment) RestartAllEGs() error {
	for i := 1; i <= 3; i++ {
		if err := te.RestartEG(i); err != nil {
			return fmt.Errorf("failed to restart EG%d: %w", i, err)
		}

		// Small delay between restarts to avoid race conditions
		time.Sleep(200 * time.Millisecond)
	}

	// Wait for cluster to stabilize
	if err := te.WaitForClusterSize(3, 15*time.Second); err != nil {
		return fmt.Errorf("cluster did not stabilize after restart: %w", err)
	}

	if os.Getenv("DEBUG") != "" {
		fmt.Printf("All 3 EGs restarted successfully\n")
	}

	return nil
}

// RestoreDefaultEGConfigs restores distribution_strategy to hash_modulo
func (te *TestEnvironment) RestoreDefaultEGConfigs() error {
	return te.UpdateEGDistributionStrategy("hash_modulo")
}

// UpdateCacheTTL modifies the ttl in all 3 EG configs
func (te *TestEnvironment) UpdateCacheTTL(ttl time.Duration) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	// Convert duration to string format (e.g., "5s", "1h")
	ttlStr := ttl.String()

	// Update all 3 EG configs
	for i := 1; i <= 3; i++ {
		egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", i))
		configPath := filepath.Join(egDir, "edge-gateway.yaml")

		// Read existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read EG%d config: %w", i, err)
		}

		// Parse YAML
		var configMap map[string]interface{}
		if err := yaml.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("failed to parse EG%d config: %w", i, err)
		}

		// Modify render.cache.ttl and render.cache.expired.stale_ttl (if present)
		if render, ok := configMap["render"].(map[string]interface{}); ok {
			if cache, ok := render["cache"].(map[string]interface{}); ok {
				cache["ttl"] = ttlStr

				// Also update stale_ttl if expired config exists
				// This maintains the same cache_ttl:stale_ttl ratio
				if expired, ok := cache["expired"].(map[string]interface{}); ok {
					expired["stale_ttl"] = ttlStr
				}
			} else {
				return fmt.Errorf("render.cache section not found in EG%d config", i)
			}
		} else {
			return fmt.Errorf("render section not found in EG%d config", i)
		}

		// Write back as YAML
		updatedData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal EG%d config: %w", i, err)
		}

		if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
			return fmt.Errorf("failed to write EG%d config: %w", i, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Updated EG%d config: ttl=%s\n", i, ttlStr)
		}

		// Also update hosts.d configuration files (which override global TTL)
		hostsDir := filepath.Join(egDir, "hosts.d")
		hostsFiles, err := filepath.Glob(filepath.Join(hostsDir, "*.yaml"))
		if err == nil && len(hostsFiles) > 0 {
			for _, hostsFile := range hostsFiles {
				// Read hosts config
				hostsData, err := os.ReadFile(hostsFile)
				if err != nil {
					continue // Skip if can't read
				}

				// Parse YAML
				var hostsMap map[string]interface{}
				if err := yaml.Unmarshal(hostsData, &hostsMap); err != nil {
					continue // Skip if can't parse
				}

				// Update all render.cache.ttl and url_rules[*].render.cache.ttl
				updated := false
				if hosts, ok := hostsMap["hosts"].([]interface{}); ok {
					for _, host := range hosts {
						if hostMap, ok := host.(map[string]interface{}); ok {
							// Update host-level ttl and stale_ttl
							if render, ok := hostMap["render"].(map[string]interface{}); ok {
								if cache, ok := render["cache"].(map[string]interface{}); ok {
									cache["ttl"] = ttlStr
									// Also update stale_ttl if expired config exists
									if expired, ok := cache["expired"].(map[string]interface{}); ok {
										expired["stale_ttl"] = ttlStr
									}
									updated = true
								}
							}

							// Update URL rules TTL and stale_ttl
							if urlRules, ok := hostMap["url_rules"].([]interface{}); ok {
								for _, rule := range urlRules {
									if ruleMap, ok := rule.(map[string]interface{}); ok {
										if render, ok := ruleMap["render"].(map[string]interface{}); ok {
											if cache, ok := render["cache"].(map[string]interface{}); ok {
												cache["ttl"] = ttlStr
												// Also update stale_ttl if expired config exists
												if expired, ok := cache["expired"].(map[string]interface{}); ok {
													expired["stale_ttl"] = ttlStr
												}
												updated = true
											}
										}
									}
								}
							}
						}
					}
				}

				// Write back if updated
				if updated {
					updatedHostsData, err := yaml.Marshal(hostsMap)
					if err == nil {
						os.WriteFile(hostsFile, updatedHostsData, 0o644)
						if os.Getenv("DEBUG") != "" {
							fmt.Printf("Updated hosts file: %s with ttl=%s\n", hostsFile, ttlStr)
						}
					}
				}
			}
		}
	}

	return nil
}

// UpdateReplicationFactor modifies the replication_factor in all 3 EG configs
func (te *TestEnvironment) UpdateReplicationFactor(rf int) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	// Update all 3 EG configs
	for i := 1; i <= 3; i++ {
		egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", i))
		configPath := filepath.Join(egDir, "edge-gateway.yaml")

		// Read existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read EG%d config: %w", i, err)
		}

		// Parse YAML
		var configMap map[string]interface{}
		if err := yaml.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("failed to parse EG%d config: %w", i, err)
		}

		// Modify replication_factor
		if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
			cacheSharding["replication_factor"] = rf
		} else {
			return fmt.Errorf("cache_sharding section not found in EG%d config", i)
		}

		// Write back as YAML
		updatedData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal EG%d config: %w", i, err)
		}

		if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
			return fmt.Errorf("failed to write EG%d config: %w", i, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Updated EG%d config: replication_factor=%d\n", i, rf)
		}
	}

	return nil
}

// UpdatePushOnRender modifies the push_on_render flag in all 3 EG configs
func (te *TestEnvironment) UpdatePushOnRender(enabled bool) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	// Update all 3 EG configs
	for i := 1; i <= 3; i++ {
		egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", i))
		configPath := filepath.Join(egDir, "edge-gateway.yaml")

		// Read existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read EG%d config: %w", i, err)
		}

		// Parse YAML
		var configMap map[string]interface{}
		if err := yaml.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("failed to parse EG%d config: %w", i, err)
		}

		// Modify push_on_render
		if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
			cacheSharding["push_on_render"] = enabled
		} else {
			return fmt.Errorf("cache_sharding section not found in EG%d config", i)
		}

		// Write back as YAML
		updatedData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal EG%d config: %w", i, err)
		}

		if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
			return fmt.Errorf("failed to write EG%d config: %w", i, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Updated EG%d config: push_on_render=%t\n", i, enabled)
		}
	}

	return nil
}

// UpdateReplicateOnPull modifies the replicate_on_pull flag in all 3 EG configs
func (te *TestEnvironment) UpdateReplicateOnPull(enabled bool) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	// Update all 3 EG configs
	for i := 1; i <= 3; i++ {
		egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", i))
		configPath := filepath.Join(egDir, "edge-gateway.yaml")

		// Read existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read EG%d config: %w", i, err)
		}

		// Parse YAML
		var configMap map[string]interface{}
		if err := yaml.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("failed to parse EG%d config: %w", i, err)
		}

		// Modify replicate_on_pull
		if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
			cacheSharding["replicate_on_pull"] = enabled
		} else {
			return fmt.Errorf("cache_sharding section not found in EG%d config", i)
		}

		// Write back as YAML
		updatedData, err := yaml.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("failed to marshal EG%d config: %w", i, err)
		}

		if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
			return fmt.Errorf("failed to write EG%d config: %w", i, err)
		}

		if os.Getenv("DEBUG") != "" {
			fmt.Printf("Updated EG%d config: replicate_on_pull=%t\n", i, enabled)
		}
	}

	return nil
}

// DisableShardingOnEG disables cache_sharding on a specific EG
func (te *TestEnvironment) DisableShardingOnEG(egNum int) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	if egNum < 1 || egNum > 3 {
		return fmt.Errorf("invalid EG number: %d (must be 1-3)", egNum)
	}

	egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", egNum))
	configPath := filepath.Join(egDir, "edge-gateway.yaml")

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read EG%d config: %w", egNum, err)
	}

	// Parse YAML
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(data, &configMap); err != nil {
		return fmt.Errorf("failed to parse EG%d config: %w", egNum, err)
	}

	// Disable sharding
	if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
		cacheSharding["enabled"] = false
	} else {
		return fmt.Errorf("cache_sharding section not found in EG%d config", egNum)
	}

	// Write back as YAML
	updatedData, err := yaml.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal EG%d config: %w", egNum, err)
	}

	if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
		return fmt.Errorf("failed to write EG%d config: %w", egNum, err)
	}

	if os.Getenv("DEBUG") != "" {
		fmt.Printf("Disabled sharding on EG%d\n", egNum)
	}

	return nil
}

// EnableShardingOnEG enables cache_sharding on a specific EG (for restoring after DisableShardingOnEG)
func (te *TestEnvironment) EnableShardingOnEG(egNum int) error {
	if te.TempConfigDir == "" {
		return fmt.Errorf("temp config directory not set")
	}

	if egNum < 1 || egNum > 3 {
		return fmt.Errorf("invalid EG number: %d (must be 1-3)", egNum)
	}

	egDir := filepath.Join(te.TempConfigDir, fmt.Sprintf("eg%d", egNum))
	configPath := filepath.Join(egDir, "edge-gateway.yaml")

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read EG%d config: %w", egNum, err)
	}

	// Parse YAML
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(data, &configMap); err != nil {
		return fmt.Errorf("failed to parse EG%d config: %w", egNum, err)
	}

	// Enable sharding
	if cacheSharding, ok := configMap["cache_sharding"].(map[string]interface{}); ok {
		cacheSharding["enabled"] = true
	} else {
		return fmt.Errorf("cache_sharding section not found in EG%d config", egNum)
	}

	// Write back as YAML
	updatedData, err := yaml.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal EG%d config: %w", egNum, err)
	}

	if err := os.WriteFile(configPath, updatedData, 0o644); err != nil {
		return fmt.Errorf("failed to write EG%d config: %w", egNum, err)
	}

	if os.Getenv("DEBUG") != "" {
		fmt.Printf("Enabled sharding on EG%d\n", egNum)
	}

	return nil
}

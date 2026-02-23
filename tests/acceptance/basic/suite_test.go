package acceptance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
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
	Config           *testutil.TestEnvironmentConfig // Loaded from test_config.yaml
	RedisClient      *redis.Client
	MiniRedis        *miniredis.Miniredis // Embedded Redis for testing
	HTTPClient       *http.Client
	TestServer       *testutil.TestServer // Local test server for fixtures
	EdgeGatewayCmd   *exec.Cmd            // Edge Gateway process
	RenderServiceCmd *exec.Cmd            // Render Service process
	TempConfigDir    string               // Temporary config directory for services
}

var testEnv *TestEnvironment

func TestAcceptance(t *testing.T) {
	RegisterFailHandler(Fail)

	// Configure Ginkgo to run specs sequentially
	// This is required because we have a single Chrome instance in test mode
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.ParallelTotal = 1
	suiteConfig.Timeout = 60 * time.Minute
	// OR use succinct mode for minimal output
	reporterConfig.Succinct = true

	RunSpecs(t, "Acceptance Test Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	By("Initializing test environment")
	testEnv = NewTestEnvironment()

	fmt.Println("========================")
	fmt.Println("BeforeSuite")

	By("Starting local test services")
	Eventually(func() error {
		err := testEnv.StartServices()
		fmt.Println("========================")
		fmt.Println(err)
		return err
	}, 30*time.Second, 1*time.Second).Should(Succeed())

	By("Waiting for services to be healthy")
	Eventually(func() bool {
		return testEnv.CheckServicesHealth()
	}, 30*time.Second, 1*time.Second).Should(BeTrue())

	By("Verifying test pages are accessible")
	Eventually(func() bool {
		return testEnv.CheckTestPagesAvailable()
	}, 15*time.Second, 500*time.Millisecond).Should(BeTrue())

	By("Waiting for render service to register in Redis")
	err := testEnv.WaitForRenderServiceRegistration(10 * time.Second)
	if err != nil {
		panic(fmt.Sprintf("Render service failed to register: %v", err))
	}
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

// NewTestEnvironment creates a new test environment
func NewTestEnvironment() *TestEnvironment {
	// Load test configuration from test_config.yaml
	config, err := testutil.LoadTestConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load test config: %v", err))
	}

	return &TestEnvironment{
		Config: config,
		HTTPClient: &http.Client{
			Timeout: config.HTTPClientTimeout(),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects - return the redirect response itself
			},
		},
	}
}

// StartServices starts the local services (miniredis + test server + Render Service + Edge Gateway)
func (te *TestEnvironment) StartServices() error {
	By("Starting embedded miniredis")

	// Start miniredis and let it pick a free port
	// This avoids conflicts and ensures consistent addressing
	mr, err := miniredis.Run()
	if err != nil {
		return fmt.Errorf("failed to start miniredis: %v", err)
	}
	te.MiniRedis = mr

	// Initialize Redis client connected to miniredis
	te.RedisClient = redis.NewClient(&redis.Options{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := te.RedisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to miniredis: %v", err)
	}

	By("Starting local test pages server")

	// Start local test pages server using port from config
	te.TestServer = testutil.NewTestServer(te.Config.TestServer.Port, te.RedisClient)
	if err := te.TestServer.Start(); err != nil {
		return fmt.Errorf("failed to start test pages server: %v", err)
	}

	By("Creating temporary config for services")

	// Create temporary config directory
	tempConfigDir, err := os.MkdirTemp("", "edgecomet-test-config-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	te.TempConfigDir = tempConfigDir

	// Use ConfigBuilder to generate configs with miniredis address
	configBuilder := testutil.NewConfigBuilder(te.Config, mr.Addr())
	if err := configBuilder.WriteTestConfigs(tempConfigDir); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to write test configs: %v", err)
	}

	By("Creating cache directory")

	// Create cache directory with absolute path
	// ConfigBuilder converts relative paths to absolute, so use the same logic
	cacheBasePath := te.Config.EdgeGateway.Storage.BasePath
	if !filepath.IsAbs(cacheBasePath) {
		absCachePath, err := filepath.Abs(cacheBasePath)
		if err != nil {
			os.RemoveAll(tempConfigDir)
			return fmt.Errorf("failed to compute absolute cache path: %v", err)
		}
		cacheBasePath = absCachePath
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheBasePath, 0o755); err != nil {
		os.RemoveAll(tempConfigDir)
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	By("Starting Render Service")

	// Start Render Service as subprocess
	// Note: Three levels up because we're in tests/acceptance/basic/
	projectRoot := filepath.Join("..", "..", "..")
	renderServicePath := filepath.Join(projectRoot, "cmd", "render-service")

	// Build config path for RS
	rsConfigPath := filepath.Join(tempConfigDir, "render-service.yaml")
	fmt.Printf("DEBUG: Starting RS with config: %s\n", rsConfigPath)

	rsCmd := exec.Command("go", "run", ".", "-c", rsConfigPath)
	rsCmd.Dir = renderServicePath

	// Set process group so we can kill all child processes
	rsCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Capture output for debugging (only if DEBUG env var is set)
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

	// Wait for Render Service to be ready with health check
	if err := te.waitForRenderService(30 * time.Second); err != nil {
		// Clean up if Render Service fails to start
		if rsCmd.Process != nil {
			rsCmd.Process.Kill()
		}
		return fmt.Errorf("Render Service failed to become ready: %v", err)
	}

	By("Starting Edge Gateway")

	// Start Edge Gateway as subprocess
	edgeGatewayPath := filepath.Join(projectRoot, "cmd", "edge-gateway")

	// Build config path for EG
	egConfigPath := filepath.Join(tempConfigDir, "edge-gateway.yaml")
	fmt.Printf("DEBUG: Starting EG with config: %s\n", egConfigPath)

	egCmd := exec.Command("go", "run", ".", "-c", egConfigPath)
	egCmd.Dir = edgeGatewayPath

	// Set process group so we can kill all child processes
	egCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Capture output for debugging (only if DEBUG env var is set)
	if os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != "" {
		egCmd.Stdout = os.Stdout
		egCmd.Stderr = os.Stderr
	} else {
		egCmd.Stdout = io.Discard
		egCmd.Stderr = io.Discard
	}

	if err := egCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Edge Gateway: %v", err)
	}
	te.EdgeGatewayCmd = egCmd

	By("Waiting for Edge Gateway to be ready")

	// Wait for Edge Gateway to be ready with health check
	if err := te.waitForEdgeGateway(30 * time.Second); err != nil {
		// Clean up if Edge Gateway fails to start
		if egCmd.Process != nil {
			egCmd.Process.Kill()
		}
		return fmt.Errorf("Edge Gateway failed to become ready: %v", err)
	}

	return nil
}

// StopServices stops the local services
func (te *TestEnvironment) StopServices() error {
	By("Stopping local test services")

	// Stop Edge Gateway first
	if te.EdgeGatewayCmd != nil && te.EdgeGatewayCmd.Process != nil {
		By("Stopping Edge Gateway")

		// Kill the entire process group (including child processes from 'go run')
		pgid, err := syscall.Getpgid(te.EdgeGatewayCmd.Process.Pid)
		if err == nil {
			// Send SIGTERM to the entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			// Fallback to killing just the parent process
			te.EdgeGatewayCmd.Process.Signal(os.Interrupt)
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		go func() {
			done <- te.EdgeGatewayCmd.Wait()
		}()

		select {
		case <-done:
			// Process exited gracefully
			// Wait for actual binary to stop (not just the 'go run' wrapper)
			te.waitForProcessExit("edge-gateway", 2*time.Second)
		case <-time.After(3 * time.Second):
			// Force kill if graceful shutdown times out
			fmt.Println("Warning: Edge Gateway didn't stop gracefully, forcing kill")
			if pgid, err := syscall.Getpgid(te.EdgeGatewayCmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				te.EdgeGatewayCmd.Process.Kill()
			}
			// Wait for process to actually die
			te.waitForProcessExit("edge-gateway", 1*time.Second)
		}
	}

	// Stop Render Service
	if te.RenderServiceCmd != nil && te.RenderServiceCmd.Process != nil {
		By("Stopping Render Service")

		// Kill the entire process group (including child processes from 'go run')
		pgid, err := syscall.Getpgid(te.RenderServiceCmd.Process.Pid)
		if err == nil {
			// Send SIGTERM to the entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			// Fallback to killing just the parent process
			te.RenderServiceCmd.Process.Signal(os.Interrupt)
		}

		// Wait for graceful shutdown with timeout
		// Note: Keep miniredis alive until after this completes, as RS deregisters from Redis during shutdown
		done := make(chan error, 1)
		go func() {
			done <- te.RenderServiceCmd.Wait()
		}()

		select {
		case <-done:
			// Process exited gracefully
			// Wait for actual binary to stop (not just the 'go run' wrapper)
			te.waitForProcessExit("render-service", 3*time.Second)
		case <-time.After(5 * time.Second):
			// Force kill if graceful shutdown times out (RS needs more time for Chrome cleanup)
			fmt.Println("Warning: Render Service didn't stop gracefully, forcing kill")
			if pgid, err := syscall.Getpgid(te.RenderServiceCmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				te.RenderServiceCmd.Process.Kill()
			}
			// Wait for process to actually die
			te.waitForProcessExit("render-service", 1*time.Second)
		}
	}

	// IMPORTANT: Close Redis/miniredis AFTER processes have exited
	// The Render Service deregisters from Redis during its shutdown sequence

	// Wait for RS background goroutines to finish before closing Redis
	// The Render Service has heartbeat loops and deregistration processes that need
	// time to complete gracefully after receiving SIGTERM. Without this delay,
	// these goroutines will attempt to access Redis after it's already closed.
	time.Sleep(2 * time.Second)

	// Close test suite's Redis client first
	if te.RedisClient != nil {
		te.RedisClient.Close()
	}

	// Stop miniredis last (after all services have finished using it)
	if te.MiniRedis != nil {
		te.MiniRedis.Close()
	}

	// Clean up temporary config directory
	if te.TempConfigDir != "" {
		os.RemoveAll(te.TempConfigDir)
	}

	// Stop test server
	if te.TestServer != nil {
		if err := te.TestServer.Stop(); err != nil {
			fmt.Printf("Warning: failed to stop test server: %v\n", err)
		}
	}

	return nil
}

// waitForRenderService waits for Render Service to be ready by polling health endpoint
func (te *TestEnvironment) waitForRenderService(timeout time.Duration) error {
	deadline := time.Now().UTC().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().UTC().Before(deadline) {
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

// waitForEdgeGateway waits for Edge Gateway to be ready by polling health endpoint
func (te *TestEnvironment) waitForEdgeGateway(timeout time.Duration) error {
	deadline := time.Now().UTC().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().UTC().Before(deadline) {
		resp, err := client.Get(te.Config.EGBaseURL() + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Edge Gateway did not become ready within %v", timeout)
}

// CheckServicesHealth checks if all services are healthy
func (te *TestEnvironment) CheckServicesHealth() bool {
	// Check Redis
	if !te.checkRedisHealth() {
		fmt.Println("Redis health check failed")
		return false
	}

	// Check test pages server
	if !te.checkTestPagesHealth() {
		fmt.Println("Test pages health check failed")
		return false
	}

	// Check Edge Gateway (may fail until implemented)
	egHealthy := te.checkEdgeGatewayHealth()
	if !egHealthy {
		fmt.Println("Edge Gateway health check failed (expected until implemented)")
		// Don't fail the test suite if EG is not implemented yet
	}

	return true
}

// checkRedisHealth checks if Redis is responding
func (te *TestEnvironment) checkRedisHealth() bool {
	if te.RedisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := te.RedisClient.Ping(ctx).Result()
	return err == nil
}

// checkTestPagesHealth checks if the test pages server is responding
func (te *TestEnvironment) checkTestPagesHealth() bool {
	resp, err := te.HTTPClient.Get(te.Config.TestPagesURL() + "/static/simple.html")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// checkEdgeGatewayHealth checks if Edge Gateway is responding
func (te *TestEnvironment) checkEdgeGatewayHealth() bool {
	// Try to connect to EG health endpoint
	resp, err := te.HTTPClient.Get(te.Config.EGBaseURL() + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// CheckTestPagesAvailable verifies that test pages are accessible
func (te *TestEnvironment) CheckTestPagesAvailable() bool {
	testPages := []string{
		"/static/simple.html",
		"/static/with-meta.html",
		"/javascript/client-rendered.html",
		"/seo/spa-initial.html",
	}

	for _, page := range testPages {
		resp, err := te.HTTPClient.Get(te.Config.TestPagesURL() + page)
		if err != nil {
			fmt.Printf("Test page not available: %s - %v\n", page, err)
			return false
		}
		resp.Body.Close()

		if resp.StatusCode != 200 {
			fmt.Printf("Test page returned non-200 status: %s - %d\n", page, resp.StatusCode)
			return false
		}
	}

	return true
}

// RequestRender makes a render request to the Edge Gateway
func (te *TestEnvironment) RequestRender(targetURL string) *TestResponse {
	return te.RequestRenderWithAPIKey(targetURL, te.Config.Test.ValidAPIKey)
}

// RequestRenderWithHAR makes a render request with HAR capture enabled
func (te *TestEnvironment) RequestRenderWithHAR(targetURL string) *TestResponse {
	return te.RequestRenderWithHARValue(targetURL, "true")
}

// RequestRenderWithHARValue makes a render request with a specific X-HAR header value
func (te *TestEnvironment) RequestRenderWithHARValue(targetURL string, harValue string) *TestResponse {
	// Build the full URL for the test page
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	// Add required headers
	req.Header.Set("X-Render-Key", te.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")
	req.Header.Set("X-HAR", harValue)

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

// RequestRenderWithAPIKey makes a render request with a specific API key
func (te *TestEnvironment) RequestRenderWithAPIKey(targetURL string, apiKey string) *TestResponse {
	// Build the full URL for the test page
	// The Edge Gateway needs to know which upstream URL to fetch
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	// Add required headers
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

// RequestRenderWithoutAuth makes a request without authentication headers
func (te *TestEnvironment) RequestRenderWithoutAuth(targetURL string) *TestResponse {
	// Build the full URL for the test page
	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	// Don't add any auth headers (testing auth failure)
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

// RequestRenderWithTimeout makes a render request with a custom timeout
func (te *TestEnvironment) RequestRenderWithTimeout(targetURL string, timeout time.Duration) *TestResponse {
	client := &http.Client{Timeout: timeout}

	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", te.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")

	start := time.Now()
	resp, err := client.Do(req)
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

// RequestRenderNoRedirect makes a render request without following redirects
// This is useful for testing redirect status actions (301, 302, 307, etc.)
func (te *TestEnvironment) RequestRenderNoRedirect(targetURL string) *TestResponse {
	// Create client that doesn't follow redirects
	client := &http.Client{
		Timeout: te.Config.HTTPClientTimeout(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	var fullTargetURL string
	if strings.HasPrefix(targetURL, "/") {
		fullTargetURL = te.Config.TestPagesURL() + targetURL
	} else {
		fullTargetURL = targetURL
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	req.Header.Set("X-Render-Key", te.Config.Test.ValidAPIKey)
	req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")

	start := time.Now()
	resp, err := client.Do(req)
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

// GetCacheKey generates a cache key for the given URL and dimension
// This uses the actual cache key generation logic to match production behavior
func (te *TestEnvironment) GetCacheKey(targetURL string, dimensionName string) (string, error) {
	// Import dependencies inline to avoid package imports at top
	// We need to create the actual cache key using real implementation
	normalizer := hash.NewURLNormalizer()
	normalizeResult, err := normalizer.Normalize(targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to normalize URL: %w", err)
	}

	urlHash := normalizer.Hash(normalizeResult.NormalizedURL)

	// Look up dimension ID from test config
	// For now, use dimension ID 1 (desktop) as default
	// This should match the dimension used in the test config
	dimensionID := 1

	// Generate cache key using actual format: cache:{host_id}:{dimension_id}:{url_hash}
	cacheKey := fmt.Sprintf("cache:%d:%d:%s", te.Config.Test.HostID, dimensionID, urlHash)
	return cacheKey, nil
}

// CacheExists checks if a cache key exists in Redis
// Note: Cache metadata is stored with "meta:" prefix
func (te *TestEnvironment) CacheExists(cacheKey string) bool {
	if te.RedisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cache metadata is stored with "meta:" prefix
	metaKey := "meta:" + cacheKey
	exists, err := te.RedisClient.Exists(ctx, metaKey).Result()
	return err == nil && exists > 0
}

// GetCacheMetadata retrieves cache metadata from Redis for verification
// Note: Cache metadata is stored as hash with "meta:" prefix
func (te *TestEnvironment) GetCacheMetadata(cacheKey string) (map[string]string, error) {
	if te.RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cache metadata is stored with "meta:" prefix as hash
	metaKey := "meta:" + cacheKey
	metadata, err := te.RedisClient.HGetAll(ctx, metaKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache metadata: %w", err)
	}

	return metadata, nil
}

// GetAllCacheKeys returns all cache keys currently in Redis (for debugging)
// Note: Searches for cache metadata keys which have "meta:cache:*" pattern
func (te *TestEnvironment) GetAllCacheKeys() ([]string, error) {
	if te.RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Search for metadata keys (meta:cache:*)
	keys, err := te.RedisClient.Keys(ctx, "meta:cache:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache keys: %w", err)
	}

	// Also check for bare cache keys (in case implementation changes)
	bareKeys, err := te.RedisClient.Keys(ctx, "cache:*").Result()
	if err == nil && len(bareKeys) > 0 {
		keys = append(keys, bareKeys...)
	}

	// Also check for lock keys for completeness
	lockKeys, err := te.RedisClient.Keys(ctx, "lock:cache:*").Result()
	if err == nil && len(lockKeys) > 0 {
		keys = append(keys, lockKeys...)
	}

	return keys, nil
}

// GetAllRedisKeys returns all keys in Redis (for debugging)
// Use pattern "*" for all keys, or a specific pattern like "service:*"
func (te *TestEnvironment) GetAllRedisKeys(pattern string) ([]string, error) {
	if te.RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	if pattern == "" {
		pattern = "*" // Default to all keys
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	keys, err := te.RedisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys for pattern '%s': %w", pattern, err)
	}

	return keys, nil
}

// ClearCache clears all cache entries from Redis
func (te *TestEnvironment) ClearCache() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Delete all cache-related keys (metadata, locks, bare cache keys)
	// This preserves the service registry that RS maintains
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

// GetCacheSource extracts the "source" field from cache metadata
// Returns "render" or "bypass" depending on cache type
func (te *TestEnvironment) GetCacheSource(cacheKey string) (string, error) {
	metadata, err := te.GetCacheMetadata(cacheKey)
	if err != nil {
		return "", err
	}

	// Extract source field from hash
	source, exists := metadata["source"]
	if !exists {
		return "", fmt.Errorf("source field not found in metadata")
	}

	return source, nil
}

// GetCacheTimestamp extracts the creation timestamp from cache metadata
func (te *TestEnvironment) GetCacheTimestamp(cacheKey string) (time.Time, error) {
	metadata, err := te.GetCacheMetadata(cacheKey)
	if err != nil {
		return time.Time{}, err
	}

	// Extract created_at field from hash (stored as Unix timestamp)
	createdAtStr, exists := metadata["created_at"]
	if !exists {
		return time.Time{}, fmt.Errorf("created_at field not found in metadata")
	}

	// Parse Unix timestamp
	var timestamp int64
	if _, err := fmt.Sscanf(createdAtStr, "%d", &timestamp); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse created_at timestamp: %w", err)
	}

	return time.Unix(timestamp, 0), nil
}

// GetCacheStatusCode extracts the status code from cache metadata
func (te *TestEnvironment) GetCacheStatusCode(cacheKey string) (int, error) {
	metadata, err := te.GetCacheMetadata(cacheKey)
	if err != nil {
		return 0, err
	}

	// Extract status_code field from hash
	statusCodeStr, exists := metadata["status_code"]
	if !exists {
		return 0, fmt.Errorf("status_code field not found in metadata")
	}

	// Parse status code
	var statusCode int
	if _, err := fmt.Sscanf(statusCodeStr, "%d", &statusCode); err != nil {
		return 0, fmt.Errorf("failed to parse status_code: %w", err)
	}

	return statusCode, nil
}

// ExpireCache manually expires a cache entry by setting ExpiresAt to past
// This is useful for testing expired cache behavior
func (te *TestEnvironment) ExpireCache(cacheKey string) error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	// Set expires_at to a past Unix timestamp (10 minutes ago)
	pastTimestamp := time.Now().UTC().Add(-10 * time.Minute).Unix()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Update just the expires_at field in the hash
	metaKey := "meta:" + cacheKey
	if err := te.RedisClient.HSet(ctx, metaKey, "expires_at", pastTimestamp).Err(); err != nil {
		return fmt.Errorf("failed to update expires_at field: %w", err)
	}

	return nil
}

// MakeCacheStale manually sets a cache entry to stale state (expired but within stale TTL)
// This sets ExpiresAt to a past time but keeps the cache in Redis for stale serving
func (te *TestEnvironment) MakeCacheStale(cacheKey string, staleDuration time.Duration) error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set expires_at to past (cache TTL ago) so cache is stale but not fully expired
	// For example, if cache TTL is 2s and stale TTL is 10s, set expires_at to 3s ago
	// This makes the cache expired (past its TTL) but still within stale period
	staleTimestamp := time.Now().Add(-staleDuration).Unix()

	metaKey := "meta:" + cacheKey
	if err := te.RedisClient.HSet(ctx, metaKey, "expires_at", staleTimestamp).Err(); err != nil {
		return fmt.Errorf("failed to update expires_at field: %w", err)
	}

	// Advance miniredis clock to trigger TTL expiration
	if err := te.SafeFastForward(staleDuration); err != nil {
		return fmt.Errorf("failed to fast forward: %w", err)
	}

	return nil
}

// SetStatusOverride sets a one-time status code override for a given URL
// The test server will check Redis for this key and return the specified status code once
func (te *TestEnvironment) SetStatusOverride(url string, statusCode int) error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("test:status:override:%s", url)
	if err := te.RedisClient.Set(ctx, key, statusCode, 30*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to set status override: %w", err)
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

// === Render Service Failure Simulation Helpers ===

// SimulateRenderServiceFailure disables all render services by setting capacity to 0
// This simulates RS failure and triggers bypass fallback in Edge Gateway
func (te *TestEnvironment) SimulateRenderServiceFailure() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find all render service keys
	serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
	if err != nil {
		return fmt.Errorf("failed to find render service keys: %w", err)
	}

	if len(serviceKeys) == 0 {
		return fmt.Errorf("no render services found in registry")
	}

	// Disable each service by setting capacity to 0
	for _, key := range serviceKeys {
		// Get current service data
		serviceData, err := te.RedisClient.Get(ctx, key).Result()
		if err != nil {
			return fmt.Errorf("failed to get service data for %s: %w", key, err)
		}

		// Unmarshal service info
		var service registry.ServiceInfo
		if err := json.Unmarshal([]byte(serviceData), &service); err != nil {
			return fmt.Errorf("failed to unmarshal service %s: %w", key, err)
		}

		// Save original capacity in metadata for restoration
		if service.Metadata == nil {
			service.Metadata = make(map[string]string)
		}
		service.Metadata["original_capacity"] = fmt.Sprintf("%d", service.Capacity)

		// Set capacity to 0 (makes Lua script reject it)
		service.Capacity = 0

		// Marshal and write back to Redis
		updatedData, err := json.Marshal(service)
		if err != nil {
			return fmt.Errorf("failed to marshal service %s: %w", key, err)
		}

		if err := te.RedisClient.Set(ctx, key, updatedData, 0).Err(); err != nil {
			return fmt.Errorf("failed to update service %s: %w", key, err)
		}
	}

	return nil
}

// RestoreRenderServiceHealth restores render services capacity from saved metadata
// This reverses SimulateRenderServiceFailure
func (te *TestEnvironment) RestoreRenderServiceHealth() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find all render service keys
	serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
	if err != nil {
		return fmt.Errorf("failed to find render service keys: %w", err)
	}

	if len(serviceKeys) == 0 {
		return fmt.Errorf("no render services found in registry")
	}

	// Restore capacity for each service
	for _, key := range serviceKeys {
		// Get current service data
		serviceData, err := te.RedisClient.Get(ctx, key).Result()
		if err != nil {
			return fmt.Errorf("failed to get service data for %s: %w", key, err)
		}

		// Unmarshal service info
		var service registry.ServiceInfo
		if err := json.Unmarshal([]byte(serviceData), &service); err != nil {
			return fmt.Errorf("failed to unmarshal service %s: %w", key, err)
		}

		// Restore original capacity from metadata
		if originalCap, exists := service.Metadata["original_capacity"]; exists {
			capacity, err := strconv.Atoi(originalCap)
			if err == nil {
				service.Capacity = capacity
			}
			delete(service.Metadata, "original_capacity")

			// Marshal and write back to Redis
			updatedData, err := json.Marshal(service)
			if err != nil {
				return fmt.Errorf("failed to marshal service %s: %w", key, err)
			}

			if err := te.RedisClient.Set(ctx, key, updatedData, 0).Err(); err != nil {
				return fmt.Errorf("failed to update service %s: %w", key, err)
			}
		}
		// If no saved capacity, service will be restored by next heartbeat
	}

	return nil
}

// ExhaustRenderServiceCapacity reserves all tabs in Redis to simulate capacity exhaustion
// This forces Edge Gateway to fallback to bypass mode
func (te *TestEnvironment) ExhaustRenderServiceCapacity() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find all render service keys to get service IDs
	serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
	if err != nil {
		return fmt.Errorf("failed to find render service keys: %w", err)
	}

	if len(serviceKeys) == 0 {
		return fmt.Errorf("no render services found in registry")
	}

	// For each service, reserve all tabs
	for _, key := range serviceKeys {
		// Extract service ID from key: service:render:{service_id}
		parts := strings.Split(key, ":")
		if len(parts) < 3 {
			continue
		}
		serviceID := parts[2]

		// Get tabs key
		tabsKey := fmt.Sprintf("tabs:%s", serviceID)

		// Get all tab fields
		tabs, err := te.RedisClient.HGetAll(ctx, tabsKey).Result()
		if err != nil {
			return fmt.Errorf("failed to get tabs for service %s: %w", serviceID, err)
		}

		// Reserve all tabs (set to test_exhausted)
		for tabID := range tabs {
			if err := te.RedisClient.HSet(ctx, tabsKey, tabID, "test_exhausted").Err(); err != nil {
				return fmt.Errorf("failed to reserve tab %s for service %s: %w", tabID, serviceID, err)
			}
		}
	}

	return nil
}

// RestoreRenderServiceCapacity frees all tabs in Redis
// This reverses ExhaustRenderServiceCapacity
func (te *TestEnvironment) RestoreRenderServiceCapacity() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find all tabs keys
	tabsKeys, err := te.RedisClient.Keys(ctx, "tabs:*").Result()
	if err != nil {
		return fmt.Errorf("failed to find tabs keys: %w", err)
	}

	// For each tabs key, free all tabs (set to empty string)
	for _, tabsKey := range tabsKeys {
		// Get all tab fields
		tabs, err := te.RedisClient.HGetAll(ctx, tabsKey).Result()
		if err != nil {
			return fmt.Errorf("failed to get tabs for %s: %w", tabsKey, err)
		}

		// Free all tabs (set to empty string)
		for tabID := range tabs {
			if err := te.RedisClient.HSet(ctx, tabsKey, tabID, "").Err(); err != nil {
				return fmt.Errorf("failed to free tab %s in %s: %w", tabID, tabsKey, err)
			}
		}
	}

	return nil
}

// RemoveRenderServiceFromRegistry completely removes render service from Redis
// This simulates RS being offline/unavailable
func (te *TestEnvironment) RemoveRenderServiceFromRegistry() error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find all render service keys
	serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
	if err != nil {
		return fmt.Errorf("failed to find render service keys: %w", err)
	}

	if len(serviceKeys) == 0 {
		return fmt.Errorf("no render services found in registry")
	}

	// Delete all service keys
	if err := te.RedisClient.Del(ctx, serviceKeys...).Err(); err != nil {
		return fmt.Errorf("failed to delete service keys: %w", err)
	}

	return nil
}

// WaitForRenderServiceRegistration waits for render service to re-register in Redis
// This is used after RestoreRenderServiceHealth or after RS recovers
func (te *TestEnvironment) WaitForRenderServiceRegistration(timeout time.Duration) error {
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
		cancel()

		if err == nil && len(serviceKeys) > 0 {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("render service did not register within %v", timeout)
}

// SafeFastForward advances miniredis time while preserving service registry
// This is needed because service registry has a 3s TTL that would expire with FastForward
func (te *TestEnvironment) SafeFastForward(duration time.Duration) error {
	if te.MiniRedis == nil {
		return fmt.Errorf("miniredis not initialized")
	}
	if te.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Extend service registry TTL to survive the fast forward
	// Service registry has 3s TTL by default, so extend it well beyond our fast forward
	serviceKeys, err := te.RedisClient.Keys(ctx, "service:render:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get service keys: %w", err)
	}

	// Extend TTL for all service keys to 1 hour (well beyond any test duration)
	for _, key := range serviceKeys {
		if err := te.RedisClient.Expire(ctx, key, 1*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to extend TTL for %s: %w", key, err)
		}
	}

	// Also extend TTL for tabs keys
	tabsKeys, err := te.RedisClient.Keys(ctx, "tabs:*").Result()
	if err == nil {
		for _, key := range tabsKeys {
			te.RedisClient.Expire(ctx, key, 1*time.Hour)
		}
	}

	// Now it's safe to fast forward
	te.MiniRedis.FastForward(duration)

	return nil
}

// RequestRenderWithCustomBaseURL makes a render request with a custom base URL for the target.
// This is useful for testing multi-domain support where the same host config
// can be accessed via different domain names.
func (te *TestEnvironment) RequestRenderWithCustomBaseURL(path, baseURL, apiKey string) *TestResponse {
	// Build the full URL using the custom base URL
	var fullTargetURL string
	if strings.HasPrefix(path, "/") {
		fullTargetURL = baseURL + path
	} else {
		fullTargetURL = baseURL + "/" + path
	}

	// Edge Gateway expects URLs in format: GET /render?url={encoded_url}
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	// Create the request to Edge Gateway
	req, err := http.NewRequest("GET", te.Config.EGBaseURL()+egPath, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	// Add required headers
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

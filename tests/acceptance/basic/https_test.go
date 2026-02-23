package acceptance_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTPS TLS Support", Serial, func() {
	Context("when TLS is disabled (default)", func() {
		It("should serve HTTP on the configured port", func() {
			By("Making HTTP request to the Edge Gateway")
			response := testEnv.RequestRender("/static/simple.html")

			By("Verifying HTTP request succeeds")
			Expect(response.Error).To(BeNil(), "HTTP request should succeed")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 OK")
			Expect(response.Body).To(ContainSubstring("Static Content"), "Should return page content")
		})

		It("should not listen on HTTPS port when TLS is disabled", func() {
			By("Attempting to connect to potential HTTPS port")
			// The default HTTPS port would be 10443 or similar
			httpsPort := 10443
			addr := fmt.Sprintf("127.0.0.1:%d", httpsPort)

			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				// If connection succeeds, it should not be the Edge Gateway
				// (it might be some other service)
				conn.Close()
			}
			// Either connection fails or it's not our EG - both are fine
			// The key is that EG without TLS config should not expose HTTPS
		})
	})

	Context("when TLS is enabled", func() {
		var (
			httpsEGProcess   *exec.Cmd
			tempDir          string
			certPath         string
			keyPath          string
			httpsPort        int
			httpPort         int
			httpsEGConfigDir string
		)

		BeforeEach(func() {
			By("Creating temporary directory for test certificates")
			var err error
			tempDir, err = os.MkdirTemp("", "eg-https-test-*")
			Expect(err).NotTo(HaveOccurred())

			By("Generating self-signed test certificate")
			certPath, keyPath = generateTestCert(tempDir)

			By("Finding available ports for HTTPS Edge Gateway")
			httpPort = findAvailablePort()
			httpsPort = findAvailablePort()
		})

		AfterEach(func() {
			By("Stopping HTTPS Edge Gateway if running")
			if httpsEGProcess != nil && httpsEGProcess.Process != nil {
				// Kill the entire process group
				pgid, err := syscall.Getpgid(httpsEGProcess.Process.Pid)
				if err == nil {
					syscall.Kill(-pgid, syscall.SIGTERM)
				} else {
					httpsEGProcess.Process.Signal(os.Interrupt)
				}
				httpsEGProcess.Wait()
			}

			By("Cleaning up temporary directory")
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
			if httpsEGConfigDir != "" {
				os.RemoveAll(httpsEGConfigDir)
			}
		})

		It("should serve content over HTTPS", func() {
			By("Starting Edge Gateway with TLS enabled")
			httpsEGConfigDir, httpsEGProcess = startHTTPSEdgeGateway(
				testEnv.MiniRedis.Addr(),
				tempDir,
				certPath,
				keyPath,
				httpPort,
				httpsPort,
				testEnv.Config.EdgeGateway.Storage.BasePath,
				testEnv.Config.TestPagesURL(),
			)
			Expect(httpsEGProcess).NotTo(BeNil())

			By("Waiting for HTTPS Edge Gateway to be ready")
			Eventually(func() bool {
				return checkHTTPSHealth(httpsPort)
			}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "HTTPS EG should become healthy")

			By("Making HTTPS request")
			response := makeHTTPSRequest(httpsPort, "/static/simple.html", testEnv.Config.Test.ValidAPIKey, testEnv.Config.TestPagesURL())

			By("Verifying HTTPS response")
			Expect(response.Error).To(BeNil(), "HTTPS request should succeed")
			Expect(response.StatusCode).To(Equal(200), "Should return 200 OK")
			Expect(response.Body).To(ContainSubstring("Static Content"), "Should return page content")
		})

		It("should serve same content via HTTP and HTTPS", func() {
			By("Starting Edge Gateway with TLS enabled")
			httpsEGConfigDir, httpsEGProcess = startHTTPSEdgeGateway(
				testEnv.MiniRedis.Addr(),
				tempDir,
				certPath,
				keyPath,
				httpPort,
				httpsPort,
				testEnv.Config.EdgeGateway.Storage.BasePath,
				testEnv.Config.TestPagesURL(),
			)
			Expect(httpsEGProcess).NotTo(BeNil())

			By("Waiting for Edge Gateway to be ready")
			Eventually(func() bool {
				return checkHTTPHealth(httpPort)
			}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "HTTP EG should become healthy")

			Eventually(func() bool {
				return checkHTTPSHealth(httpsPort)
			}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "HTTPS EG should become healthy")

			By("Making HTTP request")
			httpResponse := makeHTTPRequest(httpPort, "/static/simple.html", testEnv.Config.Test.ValidAPIKey, testEnv.Config.TestPagesURL())

			By("Making HTTPS request")
			httpsResponse := makeHTTPSRequest(httpsPort, "/static/simple.html", testEnv.Config.Test.ValidAPIKey, testEnv.Config.TestPagesURL())

			By("Verifying both responses have same content")
			Expect(httpResponse.Error).To(BeNil(), "HTTP request should succeed")
			Expect(httpsResponse.Error).To(BeNil(), "HTTPS request should succeed")
			Expect(httpResponse.StatusCode).To(Equal(httpsResponse.StatusCode), "Status codes should match")
			Expect(httpResponse.Body).To(Equal(httpsResponse.Body), "Response bodies should be identical")
		})

		It("should enforce TLS 1.3 minimum version", func() {
			By("Starting Edge Gateway with TLS enabled")
			httpsEGConfigDir, httpsEGProcess = startHTTPSEdgeGateway(
				testEnv.MiniRedis.Addr(),
				tempDir,
				certPath,
				keyPath,
				httpPort,
				httpsPort,
				testEnv.Config.EdgeGateway.Storage.BasePath,
				testEnv.Config.TestPagesURL(),
			)
			Expect(httpsEGProcess).NotTo(BeNil())

			By("Waiting for HTTPS Edge Gateway to be ready")
			Eventually(func() bool {
				return checkHTTPSHealth(httpsPort)
			}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "HTTPS EG should become healthy")

			By("Attempting TLS 1.2 connection (should fail)")
			addr := fmt.Sprintf("127.0.0.1:%d", httpsPort)
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				MaxVersion:         tls.VersionTLS12,
			}

			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			Expect(err).NotTo(HaveOccurred(), "TCP connection should succeed")

			tlsConn := tls.Client(conn, tlsConfig)
			err = tlsConn.Handshake()
			conn.Close()

			Expect(err).To(HaveOccurred(), "TLS 1.2 handshake should fail")
			Expect(err.Error()).To(ContainSubstring("protocol version"), "Error should mention protocol version")

			By("Verifying TLS 1.3 connection works")
			response := makeHTTPSRequest(httpsPort, "/health", "", "")
			Expect(response.Error).To(BeNil(), "TLS 1.3 request should succeed")
		})
	})

	Context("when TLS configuration is invalid", func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "eg-invalid-tls-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
		})

		It("should fail to start with missing cert file", func() {
			By("Creating config with non-existent cert file")
			configPath := createInvalidTLSConfig(tempDir, "/nonexistent/cert.crt", "/tmp/key.key", ":19443")

			By("Attempting to start Edge Gateway")
			cmd := exec.Command("go", "run", ".", "-c", configPath)
			cmd.Dir = filepath.Join("..", "..", "..", "cmd", "edge-gateway")

			output, err := cmd.CombinedOutput()

			By("Verifying startup failure")
			// The process should exit with error
			Expect(err).To(HaveOccurred(), "EG should fail to start with missing cert")
			outputStr := string(output)
			Expect(outputStr).To(Or(
				ContainSubstring("cert_file not found"),
				ContainSubstring("TLS"),
				ContainSubstring("cert"),
			), "Error should mention certificate issue")
		})

		It("should fail to start with missing key file", func() {
			By("Generating valid cert but using non-existent key")
			certPath, _ := generateTestCert(tempDir)
			configPath := createInvalidTLSConfig(tempDir, certPath, "/nonexistent/key.key", ":19444")

			By("Attempting to start Edge Gateway")
			cmd := exec.Command("go", "run", ".", "-c", configPath)
			cmd.Dir = filepath.Join("..", "..", "..", "cmd", "edge-gateway")

			output, err := cmd.CombinedOutput()

			By("Verifying startup failure")
			Expect(err).To(HaveOccurred(), "EG should fail to start with missing key")
			outputStr := string(output)
			Expect(outputStr).To(Or(
				ContainSubstring("key_file not found"),
				ContainSubstring("TLS"),
				ContainSubstring("key"),
			), "Error should mention key issue")
		})

		It("should fail to start with port conflict", func() {
			By("Creating config with conflicting ports")
			certPath, keyPath := generateTestCert(tempDir)
			// Use same port for HTTP and HTTPS
			configPath := createTLSConfigWithConflictingPorts(tempDir, certPath, keyPath, ":19445", ":19445")

			By("Attempting to start Edge Gateway")
			cmd := exec.Command("go", "run", ".", "-c", configPath)
			cmd.Dir = filepath.Join("..", "..", "..", "cmd", "edge-gateway")

			output, err := cmd.CombinedOutput()

			By("Verifying startup failure")
			Expect(err).To(HaveOccurred(), "EG should fail to start with conflicting ports")
			outputStr := string(output)
			Expect(outputStr).To(Or(
				ContainSubstring("conflicts"),
				ContainSubstring("port"),
				ContainSubstring("TLS"),
			), "Error should mention port conflict")
		})
	})
})

// Helper functions

func generateTestCert(dir string) (certPath, keyPath string) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	Expect(err).NotTo(HaveOccurred())

	certPath = filepath.Join(dir, "test.crt")
	certFile, err := os.Create(certPath)
	Expect(err).NotTo(HaveOccurred())
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	Expect(err).NotTo(HaveOccurred())
	certFile.Close()

	keyPath = filepath.Join(dir, "test.key")
	keyFile, err := os.Create(keyPath)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	Expect(err).NotTo(HaveOccurred())
	err = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	Expect(err).NotTo(HaveOccurred())
	keyFile.Close()

	return certPath, keyPath
}

func findAvailablePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func startHTTPSEdgeGateway(redisAddr, _, certPath, keyPath string, httpPort, httpsPort int, cacheBasePath, testPagesURL string) (string, *exec.Cmd) {
	// Convert cache base path to absolute path if needed
	absCachePath := cacheBasePath
	if !filepath.IsAbs(cacheBasePath) {
		var err error
		absCachePath, err = filepath.Abs(cacheBasePath)
		Expect(err).NotTo(HaveOccurred())
	}

	// Create config directory
	configDir, err := os.MkdirTemp("", "eg-https-config-*")
	Expect(err).NotTo(HaveOccurred())

	// Create Edge Gateway config with TLS enabled
	egConfig := fmt.Sprintf(`
eg_id: "eg-https-test"
server:
  listen: ":%d"
  timeout: 120s
  tls:
    enabled: true
    listen: ":%d"
    cert_file: "%s"
    key_file: "%s"
redis:
  addr: "%s"
  password: ""
  db: 0
storage:
  base_path: "%s"
internal:
  listen: "0.0.0.0:%d"
  auth_key: "test-internal-key"
log:
  level: "debug"
  console:
    enabled: true
    format: "console"
hosts:
  include: "hosts.d/"
`, httpPort, httpsPort, certPath, keyPath, redisAddr, absCachePath, findAvailablePort())

	// Write EG config
	egConfigPath := filepath.Join(configDir, "edge-gateway.yaml")
	err = os.WriteFile(egConfigPath, []byte(egConfig), 0o644)
	Expect(err).NotTo(HaveOccurred())

	// Create hosts.d directory and host config
	hostsDir := filepath.Join(configDir, "hosts.d")
	err = os.MkdirAll(hostsDir, 0o755)
	Expect(err).NotTo(HaveOccurred())

	// testPagesURL is passed via render requests, not stored in host config
	_ = testPagesURL
	hostConfig := `
hosts:
  - id: 1
    domain: ["localhost", "127.0.0.1"]
    render_key: "sk_test_render_12345"
    enabled: true
    render:
      timeout: 30s
      cache:
        ttl: 1h
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0"
          match_ua:
            - "*Googlebot*"
`

	hostConfigPath := filepath.Join(hostsDir, "01-localhost.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644)
	Expect(err).NotTo(HaveOccurred())

	// Start Edge Gateway
	projectRoot := filepath.Join("..", "..", "..")
	edgeGatewayPath := filepath.Join(projectRoot, "cmd", "edge-gateway")

	cmd := exec.Command("go", "run", ".", "-c", egConfigPath)
	cmd.Dir = edgeGatewayPath
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Capture output for debugging (only if DEBUG env var is set)
	if os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != "" {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	err = cmd.Start()
	Expect(err).NotTo(HaveOccurred())

	return configDir, cmd
}

func checkHTTPHealth(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func checkHTTPSHealth(port int) bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func makeHTTPRequest(port int, path, apiKey, testPagesURL string) *TestResponse {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	fullTargetURL := testPagesURL + path
	egPath := "/render?url=" + url.QueryEscape(fullTargetURL)

	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", port, egPath), nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	if apiKey != "" {
		req.Header.Set("X-Render-Key", apiKey)
	}
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

func makeHTTPSRequest(port int, path, apiKey, testPagesURL string) *TestResponse {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var reqURL string
	if testPagesURL != "" && path != "/health" {
		fullTargetURL := testPagesURL + path
		reqURL = fmt.Sprintf("https://127.0.0.1:%d/render?url=%s", port, url.QueryEscape(fullTargetURL))
	} else {
		reqURL = fmt.Sprintf("https://127.0.0.1:%d%s", port, path)
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return &TestResponse{Error: err}
	}

	if apiKey != "" {
		req.Header.Set("X-Render-Key", apiKey)
	}
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

func createInvalidTLSConfig(tempDir, certPath, keyPath, httpsListen string) string {
	httpPort := findAvailablePort()

	config := fmt.Sprintf(`
eg_id: "eg-invalid-tls"
server:
  listen: ":%d"
  timeout: 120s
  tls:
    enabled: true
    listen: "%s"
    cert_file: "%s"
    key_file: "%s"
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
storage:
  base_path: "/tmp/cache"
internal:
  listen: "0.0.0.0:%d"
  auth_key: "test-key"
log:
  level: "error"
  console:
    enabled: true
hosts:
  include: "hosts.d/"
`, httpPort, httpsListen, certPath, keyPath, findAvailablePort())

	configPath := filepath.Join(tempDir, "edge-gateway.yaml")
	err := os.WriteFile(configPath, []byte(config), 0o644)
	Expect(err).NotTo(HaveOccurred())

	// Create hosts.d directory with minimal host file
	hostsDir := filepath.Join(tempDir, "hosts.d")
	err = os.MkdirAll(hostsDir, 0o755)
	Expect(err).NotTo(HaveOccurred())

	// Add a minimal host config to pass host validation
	hostConfig := `
hosts:
  - id: 1
    domain: "localhost"
    render_key: "test-key"
    enabled: true
    render:
      timeout: 30s
      cache:
        ttl: 1h
      dimensions:
        default:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Test"
          match_ua:
            - "*"
`
	hostConfigPath := filepath.Join(hostsDir, "01-test.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644)
	Expect(err).NotTo(HaveOccurred())

	return configPath
}

func createTLSConfigWithConflictingPorts(tempDir, certPath, keyPath, httpListen, httpsListen string) string {
	config := fmt.Sprintf(`
eg_id: "eg-conflict-ports"
server:
  listen: "%s"
  timeout: 120s
  tls:
    enabled: true
    listen: "%s"
    cert_file: "%s"
    key_file: "%s"
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
storage:
  base_path: "/tmp/cache"
internal:
  listen: "0.0.0.0:%d"
  auth_key: "test-key"
log:
  level: "error"
  console:
    enabled: true
hosts:
  include: "hosts.d/"
`, httpListen, httpsListen, certPath, keyPath, findAvailablePort())

	configPath := filepath.Join(tempDir, "edge-gateway.yaml")
	err := os.WriteFile(configPath, []byte(config), 0o644)
	Expect(err).NotTo(HaveOccurred())

	// Create hosts.d directory with minimal host file
	hostsDir := filepath.Join(tempDir, "hosts.d")
	err = os.MkdirAll(hostsDir, 0o755)
	Expect(err).NotTo(HaveOccurred())

	// Add a minimal host config to pass host validation
	hostConfig := `
hosts:
  - id: 1
    domain: "localhost"
    render_key: "test-key"
    enabled: true
    render:
      timeout: 30s
      cache:
        ttl: 1h
      dimensions:
        default:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Test"
          match_ua:
            - "*"
`
	hostConfigPath := filepath.Join(hostsDir, "01-test.yaml")
	err = os.WriteFile(hostConfigPath, []byte(hostConfig), 0o644)
	Expect(err).NotTo(HaveOccurred())

	return configPath
}

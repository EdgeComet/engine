package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type Config struct {
	URLsFile        string
	Gateways        []string
	RenderKey       string
	BaseConcurrency int
	Duration        time.Duration
	Timeout         time.Duration
}

func main() {
	rand.Seed(time.Now().UnixNano())

	urlsFile := flag.String("urls", "", "Path to CSV file containing URLs (required)")
	gatewayStr := flag.String("gateway", "", "Edge Gateway base URL(s), comma-separated (required)")
	renderKey := flag.String("key", "", "X-Render-Key for authentication (required)")
	concurrency := flag.Int("concurrency", 0, "Base number of simultaneous requests (required)")
	durationStr := flag.String("duration", "", "Test duration limit (e.g., 5m, 1h) (optional)")
	timeout := flag.Duration("timeout", 60*time.Second, "HTTP request timeout (default: 60s)")

	flag.Parse()

	config, err := validateParameters(*urlsFile, *gatewayStr, *renderKey, *concurrency, *durationStr, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Load Test Tool - Configuration\n")
	fmt.Printf("URLs file: %s\n", config.URLsFile)
	fmt.Printf("Gateways: %v\n", config.Gateways)
	fmt.Printf("Concurrency: %d\n", config.BaseConcurrency)
	if config.Duration > 0 {
		fmt.Printf("Duration: %s\n", config.Duration)
	} else {
		fmt.Printf("Duration: unlimited (press Ctrl+C to stop)\n")
	}
	fmt.Printf("Timeout: %s\n", config.Timeout)
	fmt.Printf("\nConfiguration validated successfully!\n\n")

	urls, err := loadURLs(config.URLsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading URLs: %v\n", err)
		os.Exit(1)
	}

	stats := NewGlobalStats()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if config.Duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, config.Duration)
		defer cancel()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var activeRequests int64
	stats.SetActiveRequests(&activeRequests, config.BaseConcurrency)

	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: config.BaseConcurrency * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	fmt.Printf("Starting load test with %d URLs...\n\n", len(urls))
	time.Sleep(500 * time.Millisecond)

	go requestSpawner(ctx, config, urls, stats, &activeRequests, client)
	go realTimeReporter(ctx, stats)

	select {
	case <-sigChan:
		fmt.Print("\033[2J\033[H")
		fmt.Println("Shutdown signal received...")
	case <-ctx.Done():
		fmt.Print("\033[2J\033[H")
		if config.Duration > 0 {
			fmt.Println("Duration limit reached...")
		}
	}

	cancel()

	fmt.Println("\nWaiting for active requests to complete...")
	waitForActiveRequests(&activeRequests, 5*time.Second)

	duration := time.Since(stats.startTime)
	printFinalReport(stats, duration)
}

func waitForActiveRequests(activeRequests *int64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	lastCount := atomic.LoadInt64(activeRequests)

	for {
		current := atomic.LoadInt64(activeRequests)
		if current == 0 {
			fmt.Println("All requests completed.")
			return
		}

		if time.Now().After(deadline) {
			fmt.Printf("Timeout reached. %d requests still active.\n", current)
			return
		}

		if current != lastCount {
			fmt.Printf("Waiting... %d requests still active\n", current)
			lastCount = current
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func requestSpawner(ctx context.Context, config *Config, urls []URLEntry, stats *GlobalStats, activeRequests *int64, client *http.Client) {
	var requestNum int64

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		target := calculateTargetConcurrency(config.BaseConcurrency)
		current := atomic.LoadInt64(activeRequests)

		if current < int64(target) {
			atomic.AddInt64(activeRequests, 1)
			atomic.AddInt64(&requestNum, 1)
			go executeRequestWorker(config, urls, stats, activeRequests, requestNum, client)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func calculateTargetConcurrency(baseConcurrency int) int {
	variation := rand.Intn(61) - 30
	adjusted := baseConcurrency + (baseConcurrency * variation / 100)

	if adjusted < 1 {
		adjusted = 1
	}

	return adjusted
}

func executeRequestWorker(config *Config, urls []URLEntry, stats *GlobalStats, activeRequests *int64, requestNum int64, client *http.Client) {
	defer atomic.AddInt64(activeRequests, -1)

	urlEntry := selectRandomURL(urls)
	gateway := selectGateway(config.Gateways, requestNum)
	userAgent := selectRandomUserAgent()

	targetURL, err := url.Parse(urlEntry.URL)
	if err != nil {
		stats.RecordRequest(&RequestResult{
			Success: false,
			Error:   "parse_error",
			Host:    "",
		})
		return
	}

	req, err := buildRequest(gateway, urlEntry.URL, config.RenderKey, userAgent)
	if err != nil {
		stats.RecordRequest(&RequestResult{
			Success: false,
			Error:   "request_build_error",
			Host:    targetURL.Host,
			URL:     urlEntry.URL,
		})
		return
	}

	result := executeRequest(client, req, urlEntry.ExpectedStatus, targetURL.Host, urlEntry.URL)
	stats.RecordRequest(result)
}

func selectRandomURL(urls []URLEntry) URLEntry {
	return urls[rand.Intn(len(urls))]
}

func selectGateway(gateways []string, requestNum int64) string {
	idx := requestNum % int64(len(gateways))
	return gateways[idx]
}

func validateParameters(urlsFile, gatewayStr, renderKey string, concurrency int, durationStr string, timeout time.Duration) (*Config, error) {
	if urlsFile == "" {
		return nil, fmt.Errorf("missing required parameter: -urls")
	}

	if gatewayStr == "" {
		return nil, fmt.Errorf("missing required parameter: -gateway")
	}

	if renderKey == "" {
		return nil, fmt.Errorf("missing required parameter: -key")
	}

	if concurrency <= 0 {
		return nil, fmt.Errorf("concurrency must be greater than 0")
	}

	if _, err := os.Stat(urlsFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", urlsFile)
	}

	gatewaysSplit := strings.Split(gatewayStr, ",")
	gateways := make([]string, 0, len(gatewaysSplit))
	for _, gw := range gatewaysSplit {
		trimmed := strings.TrimSpace(gw)
		if trimmed == "" {
			continue
		}

		parsedURL, err := url.Parse(trimmed)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return nil, fmt.Errorf("invalid gateway URL: %s", trimmed)
		}

		gateways = append(gateways, trimmed)
	}

	if len(gateways) == 0 {
		return nil, fmt.Errorf("no valid gateway URLs provided")
	}

	var duration time.Duration
	if durationStr != "" {
		var err error
		duration, err = time.ParseDuration(durationStr)
		if err != nil {
			return nil, fmt.Errorf("invalid duration format: %s", durationStr)
		}
		if duration <= 0 {
			return nil, fmt.Errorf("duration must be greater than 0")
		}
	}

	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than 0")
	}

	return &Config{
		URLsFile:        urlsFile,
		Gateways:        gateways,
		RenderKey:       renderKey,
		BaseConcurrency: concurrency,
		Duration:        duration,
		Timeout:         timeout,
	}, nil
}

package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// TestServer manages a local HTTP server for serving test fixtures
type TestServer struct {
	server      *http.Server
	port        int
	baseURL     string
	shutdown    chan struct{}
	redisClient *redis.Client
}

// NewTestServer creates a new test server instance
func NewTestServer(port int, redisClient *redis.Client) *TestServer {
	return &TestServer{
		port:        port,
		baseURL:     fmt.Sprintf("http://localhost:%d", port),
		shutdown:    make(chan struct{}),
		redisClient: redisClient,
	}
}

// Start starts the test server
func (ts *TestServer) Start() error {
	// Create file server for test fixtures
	fixturesDir := filepath.Join("fixtures", "test-pages")
	fileHandler := http.FileServer(http.Dir(fixturesDir))

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","server":"test-pages"}`))
	})

	// Mock API endpoint for AJAX tests - returns JSON data after a delay
	mux.HandleFunc("/api/mock-data", func(w http.ResponseWriter, r *http.Request) {
		// Parse delay parameter (default: 1500ms)
		delayMs := 1500
		if delayParam := r.URL.Query().Get("delay"); delayParam != "" {
			if parsed, err := strconv.Atoi(delayParam); err == nil && parsed >= 0 {
				delayMs = parsed
			}
		}

		// Simulate network delay
		time.Sleep(time.Duration(delayMs) * time.Millisecond)

		// Prepare mock response data
		responseData := map[string]interface{}{
			"data": map[string]interface{}{
				"title": "Items loaded from API",
				"items": []map[string]interface{}{
					{"id": 1, "name": "Item One", "description": "First API item"},
					{"id": 2, "name": "Item Two", "description": "Second API item"},
					{"id": 3, "name": "Item Three", "description": "Third API item"},
				},
				"metadata": map[string]interface{}{
					"total":    3,
					"loadTime": time.Now().Format(time.RFC3339Nano),
					"source":   "mock-api",
					"uuid":     uuid.New().String(),
				},
			},
		}

		// Set headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Encode and send response
		json.NewEncoder(w).Encode(responseData)
	})

	// Tracking pixel endpoint - for custom pattern blocking tests
	mux.HandleFunc("/api/tracking/pixel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		responseData := map[string]interface{}{
			"tracked":   true,
			"pixel_id":  "pixel-12345",
			"timestamp": time.Now().Unix(),
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// Analytics collection endpoint - for custom pattern blocking tests
	mux.HandleFunc("/api/analytics/collect", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		responseData := map[string]interface{}{
			"collected":  true,
			"event":      "pageview",
			"session_id": uuid.New().String(),
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// Ad banner endpoint - for custom pattern blocking tests
	mux.HandleFunc("/api/ads/banner", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		responseData := map[string]interface{}{
			"ad":    "Special Offer - 50% Off!",
			"shown": true,
			"ad_id": "banner-789",
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// === URL Pattern Matching Test Handlers ===

	// Search handler - /search (with or without query params)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		queryParams := r.URL.RawQuery
		queryDisplay := "No query parameters"
		if queryParams != "" {
			queryDisplay = fmt.Sprintf("Query: %s", queryParams)
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Search Results</title>
	<meta name="page-type" content="search">
</head>
<body>
	<h1>Search Results</h1>
	<p>Path: %s</p>
	<p>%s</p>
	<div class="search-form">
		<input type="text" placeholder="Search...">
		<button>Search</button>
	</div>
	<div class="results">
		<p>Search results would appear here.</p>
	</div>
</body>
</html>`, r.URL.Path, queryDisplay)

		w.Write([]byte(html))
	})

	// Blog handler - /blog/* (recursive wildcard)
	mux.HandleFunc("/blog/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		articleSlug := path[6:] // Remove "/blog/" prefix
		if articleSlug == "" {
			articleSlug = "index"
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Blog Article - %s</title>
	<meta name="page-type" content="blog">
	<meta name="description" content="Blog article for testing">
</head>
<body>
	<h1>Blog Article</h1>
	<p class="slug">Article Slug: %s</p>
	<p class="path">Path: %s</p>
	<div class="content">
		<p>This is a blog post for testing URL pattern matching.</p>
		<p>The blog pattern should match all paths under /blog/* recursively.</p>
	</div>
</body>
</html>`, articleSlug, articleSlug, path)

		w.Write([]byte(html))
	})

	// Product reviews handler - /product/*/reviews
	mux.HandleFunc("/product/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle if path ends with /reviews
		if filepath.Base(r.URL.Path) != "reviews" {
			// Generic HTML for other product pages
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Product Page</title></head>
<body><h1>Product Page</h1><p>Generic product page</p></body>
</html>`))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Extract product ID from path: /product/{id}/reviews
		path := r.URL.Path
		// Split by "/" and get the product ID
		parts := []string{}
		for _, part := range []rune(path) {
			if part == '/' {
				parts = append(parts, "")
			} else if len(parts) > 0 {
				parts[len(parts)-1] += string(part)
			}
		}

		productID := "unknown"
		for i, part := range parts {
			if part == "product" && i+1 < len(parts) {
				productID = parts[i+1]
				break
			}
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Product Reviews - %s</title>
	<meta name="page-type" content="product-reviews">
</head>
<body>
	<h1>Reviews for Product %s</h1>
	<p class="product-id">Product ID: %s</p>
	<p class="path">Path: %s</p>
	<div class="reviews">
		<div class="review">
			<h3>Great Product!</h3>
			<p>Rating: 5/5</p>
		</div>
		<div class="review">
			<h3>Good value</h3>
			<p>Rating: 4/5</p>
		</div>
	</div>
</body>
</html>`, productID, productID, productID, path)

		w.Write([]byte(html))
	})

	// Special pages handler - /special/*
	mux.HandleFunc("/special/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		pageName := path[9:] // Remove "/special/" prefix
		if pageName == "" {
			pageName = "index"
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Special Page - %s</title>
	<meta name="page-type" content="special">
</head>
<body>
	<h1>Special Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div class="content">
		<p>This is a special page for testing URL pattern matching.</p>
		<p>Special pages have custom rendering rules.</p>
	</div>
</body>
</html>`, pageName, pageName, path)

		w.Write([]byte(html))
	})

	// Generic API handler - catches /api/* not handled by specific endpoints above
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		// Parse delay parameter for timeout testing (matches /api/mock-data behavior)
		if delayParam := r.URL.Query().Get("delay"); delayParam != "" {
			if delayMs, err := strconv.Atoi(delayParam); err == nil && delayMs >= 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		responseData := map[string]interface{}{
			"api":       true,
			"path":      r.URL.Path,
			"method":    r.Method,
			"timestamp": time.Now().Unix(),
			"data": map[string]interface{}{
				"message": "Generic API endpoint",
				"status":  "ok",
			},
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// Bypass cache test endpoints - /bypass-test/*
	mux.HandleFunc("/bypass-test/", func(w http.ResponseWriter, r *http.Request) {
		// Parse status code from query param (default 200)
		statusCode := 200
		if code := r.URL.Query().Get("status"); code != "" {
			if parsed, err := strconv.Atoi(code); err == nil && parsed >= 100 && parsed < 600 {
				statusCode = parsed
			}
		}

		// Parse delay from query param (for timeout tests)
		if delayParam := r.URL.Query().Get("delay"); delayParam != "" {
			if delayMs, err := strconv.Atoi(delayParam); err == nil && delayMs >= 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}

		// Handle redirects (3xx)
		if statusCode >= 300 && statusCode < 400 {
			location := r.URL.Query().Get("location")
			if location == "" {
				// Use test server's own URL as default redirect target
				location = ts.baseURL + "/static/simple.html"
			}
			w.Header().Set("Location", location)
		}

		// Set standard headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, time.Now().UnixNano()))
		w.Header().Set("Last-Modified", time.Now().Format(time.RFC1123))
		w.Header().Set("X-Test-Request-ID", uuid.New().String())

		w.WriteHeader(statusCode)

		// Return JSON body (except for redirects)
		if statusCode < 300 || statusCode >= 400 {
			responseData := map[string]interface{}{
				"path":      r.URL.Path,
				"status":    statusCode,
				"timestamp": time.Now().Unix(),
				"message":   fmt.Sprintf("Bypass test endpoint - status %d", statusCode),
				"query":     r.URL.RawQuery,
			}
			json.NewEncoder(w).Encode(responseData)
		}
	})

	// Render cache priority test endpoints - /priority-test/*
	mux.HandleFunc("/priority-test/", func(w http.ResponseWriter, r *http.Request) {
		// Return simple HTML with dynamic timestamp
		// Allows verification of which version was served (render vs bypass cache)
		timestamp := time.Now().UnixNano()
		requestID := uuid.New().String()

		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Priority Test</title>
	<meta name="description" content="Render cache priority test page">
</head>
<body>
	<h1>Render Cache Priority Test</h1>
	<div id="timestamp">%d</div>
	<div id="request-id">%s</div>
	<div id="path">%s</div>
	<p>This page is used to test render cache vs bypass cache priority.</p>
</body>
</html>`, timestamp, requestID, r.URL.Path)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	})

	// Query parameter matching test handler - /qparam-test/*
	mux.HandleFunc("/qparam-test/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		queryParams := r.URL.Query()

		// Build query parameter list for display
		paramsList := ""
		if len(queryParams) > 0 {
			paramsList = "<h2>Query Parameters:</h2><ul>"
			for key, values := range queryParams {
				paramsList += fmt.Sprintf("<li><strong>%s</strong>: %s</li>", key, values[0])
			}
			paramsList += "</ul>"
		} else {
			paramsList = "<p>No query parameters provided</p>"
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Query Parameter Test Page</title>
	<meta name="description" content="Query parameter matching test page">
</head>
<body>
	<h1>Query Parameter Test Page</h1>
	<div id="query-params">%s</div>
	<div id="test-marker">QPARAM_TEST_PAGE</div>
	<div id="path">Path: %s</div>
</body>
</html>`, paramsList, path)

		w.Write([]byte(html))
	})

	// Stale cache test handler - /stale-test/*
	mux.HandleFunc("/stale-test/", func(w http.ResponseWriter, r *http.Request) {
		// Check Redis for one-time status override
		statusCode := 200
		if ts.redisClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			overrideKey := fmt.Sprintf("test:status:override:%s", r.URL.Path+"?"+r.URL.RawQuery)
			if val, err := ts.redisClient.Get(ctx, overrideKey).Result(); err == nil {
				if parsed, err := strconv.Atoi(val); err == nil && parsed >= 100 && parsed < 600 {
					statusCode = parsed
					// Delete the key after use (one-time override)
					ts.redisClient.Del(context.Background(), overrideKey)
				}
			}
		}

		// Parse status code from query param (default 200, overridden by Redis if set)
		if statusCode == 200 {
			if code := r.URL.Query().Get("status"); code != "" {
				if parsed, err := strconv.Atoi(code); err == nil && parsed >= 100 && parsed < 600 {
					statusCode = parsed
				}
			}
		}

		// Parse delay from query param (for timeout tests)
		if delayParam := r.URL.Query().Get("delay"); delayParam != "" {
			if delayMs, err := strconv.Atoi(delayParam); err == nil && delayMs >= 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}

		// Handle redirects (3xx)
		if statusCode >= 300 && statusCode < 400 {
			location := r.URL.Query().Get("location")
			if location == "" {
				location = ts.baseURL + "/static/simple.html"
			}
			w.Header().Set("Location", location)
		}

		// Set standard headers
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")

		w.WriteHeader(statusCode)

		// Return HTML body (except for redirects)
		if statusCode < 300 || statusCode >= 400 {
			timestamp := time.Now().UnixNano()
			requestID := uuid.New().String()

			html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Stale Cache Test</title>
	<meta name="description" content="Stale cache test page">
</head>
<body>
	<h1>Stale Cache Test Page (Status %d)</h1>
	<div id="timestamp">%d</div>
	<div id="request-id">%s</div>
	<div id="path">%s</div>
	<div id="status">%d</div>
	<p>This page is used to test stale cache behavior.</p>
</body>
</html>`, statusCode, timestamp, requestID, r.URL.Path, statusCode)

			w.Write([]byte(html))
		}
	})

	// Tracking parameters test handler - /tracking-params/*
	mux.HandleFunc("/tracking-params/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		pageName := path[len("/tracking-params/"):] // Remove "/tracking-params/" prefix
		if pageName == "" {
			pageName = "index"
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Tracking Parameter Test Page</title>
	<meta name="description" content="Tracking parameter stripping test page">
</head>
<body>
	<h1>Tracking Parameter Test Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div id="test-marker">TRACKING_PARAMS_TEST_PAGE</div>
	<div class="content">
		<p>This page is used to test tracking parameter stripping functionality.</p>
		<p>Query parameters like utm_source, gclid, etc. should be stripped from the URL.</p>
	</div>
</body>
</html>`, pageName, path)

		w.Write([]byte(html))
	})

	// Headers test handler - /headers-test/*
	// Returns pages with specific headers for testing safe headers functionality
	mux.HandleFunc("/headers-test/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Handle special edge case paths (check for patterns anywhere in path)
		switch {
		case strings.Contains(path, "/multi-value/"):
			// Multiple Set-Cookie headers (non-safe, but tests multi-value handling)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Add("Set-Cookie", "session=abc123; Path=/")
			w.Header().Add("Set-Cookie", "user=john; Path=/")
			w.Header().Set("X-Custom-Header", "custom-value-123")

		case strings.Contains(path, "/empty-value/"):
			// Headers with empty values
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "")
			w.Header().Set("X-Custom-Header", "custom-value-123")

		case strings.Contains(path, "/special-chars/"):
			// Values with special characters
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600; must-revalidate")
			w.Header().Set("X-Custom-Header", `value with "quotes", commas; and semicolons`)

		case strings.Contains(path, "/case-variation/"):
			// Headers with different casing than configured
			w.Header().Set("content-type", "text/html; charset=utf-8")
			w.Header().Set("cache-control", "public, max-age=3600")
			w.Header().Set("x-custom-header", "lowercase-header-name")

		case strings.Contains(path, "/missing-header/"):
			// Only some safe headers present
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// X-Custom-Header intentionally NOT set

		case strings.Contains(path, "/duplicate/"):
			// Same header added multiple times
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Add("X-Custom-Header", "value1")
			w.Header().Add("X-Custom-Header", "value2")
			w.Header().Set("Cache-Control", "public, max-age=3600")

		default:
			// Default behavior - standard headers
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("X-Custom-Header", "custom-value-123")
			w.Header().Set("X-Secret-Header", "secret-value")
			w.Header().Set("Set-Cookie", "session=abc123; Path=/")
			w.Header().Set("X-Internal-Debug", "debug-info")
		}

		w.WriteHeader(http.StatusOK)

		pageName := ""
		if len(path) > len("/headers-test/") {
			pageName = path[len("/headers-test/"):]
		}
		if pageName == "" {
			pageName = "index"
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Headers Test Page</title>
	<meta name="description" content="Safe headers test page">
</head>
<body>
	<h1>Headers Test Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div id="test-marker">HEADERS_TEST_PAGE</div>
</body>
</html>`, pageName, path)

		w.Write([]byte(html))
	})

	// Unmatched dimension test handler - /test-unmatched/*
	mux.HandleFunc("/test-unmatched/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		testType := ""
		if len(path) > len("/test-unmatched/") {
			parts := strings.Split(path[len("/test-unmatched/"):], "/")
			if len(parts) > 0 {
				testType = parts[0]
			}
		}

		var title, description string
		switch testType {
		case "block":
			title = "Block Test Page"
			description = "This page should return 403 when accessed with an unmatched User-Agent."
		case "desktop":
			title = "Desktop Fallback Test Page"
			description = "This page should render using desktop dimension when accessed with an unmatched User-Agent."
		case "bypass":
			title = "Bypass Test Page"
			description = "This page should bypass rendering when accessed with an unmatched User-Agent."
		case "default":
			title = "Default Test Page"
			description = "This page uses the host default unmatched_dimension setting (bypass)."
		default:
			title = "Unmatched Dimension Test"
			description = "Test page for unmatched dimension handling."
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>%s</title>
</head>
<body>
	<h1>%s</h1>
	<p>%s</p>
	<p class="path">Path: %s</p>
</body>
</html>`, title, title, description, path)

		w.Write([]byte(html))
	})

	// Compression test handler - generates > 2KB of HTML content
	mux.HandleFunc("/compression-test/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		pageName := ""
		if len(path) > len("/compression-test/") {
			pageName = path[len("/compression-test/"):]
		}
		if pageName == "" {
			pageName = "index"
		}

		// Generate content that is definitely > 1024 bytes
		var contentBuilder strings.Builder
		contentBuilder.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Compression Test Page - %s</title>
	<meta name="description" content="This page is designed to test compression functionality">
	<style>
		body { font-family: Arial, sans-serif; margin: 20px; }
		.content-block { padding: 15px; margin: 10px 0; background: #f5f5f5; }
		.data-row { display: flex; justify-content: space-between; padding: 5px 0; }
	</style>
</head>
<body>
	<h1>Compression Test Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div id="test-marker">COMPRESSION_TEST_PAGE</div>
`, pageName, pageName, path))

		// Add multiple content blocks to ensure we exceed 1024 bytes
		for i := 1; i <= 10; i++ {
			contentBuilder.WriteString(fmt.Sprintf(`
	<div class="content-block" id="block-%d">
		<h2>Content Block %d</h2>
		<p>This is paragraph content for block %d. Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>
		<div class="data-row"><span>Item %d-A:</span><span>Value for item A in block %d</span></div>
		<div class="data-row"><span>Item %d-B:</span><span>Value for item B in block %d</span></div>
		<div class="data-row"><span>Item %d-C:</span><span>Value for item C in block %d</span></div>
	</div>
`, i, i, i, i, i, i, i, i, i))
		}

		contentBuilder.WriteString(`
	<footer>
		<p>End of compression test content</p>
	</footer>
</body>
</html>`)

		w.Write([]byte(contentBuilder.String()))
	})

	// Default handler for everything else - handles PDFs, JSONs, and generic pages
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Handle PDF files - *.pdf
		if filepath.Ext(path) == ".pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.WriteHeader(http.StatusOK)

			// Minimal fake PDF content (valid PDF structure)
			pdfContent := fmt.Sprintf(`%%PDF-1.4
1 0 obj
<<
/Type /Catalog
/Pages 2 0 R
>>
endobj
2 0 obj
<<
/Type /Pages
/Kids [3 0 R]
/Count 1
>>
endobj
3 0 obj
<<
/Type /Page
/Parent 2 0 R
/Resources <<
/Font <<
/F1 <<
/Type /Font
/Subtype /Type1
/BaseFont /Helvetica
>>
>>
>>
/MediaBox [0 0 612 792]
/Contents 4 0 R
>>
endobj
4 0 obj
<<
/Length 44
>>
stream
BT
/F1 12 Tf
100 700 Td
(Test PDF: %s) Tj
ET
endstream
endobj
xref
0 5
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000315 00000 n
trailer
<<
/Size 5
/Root 1 0 R
>>
startxref
408
%%%%EOF`, path)

			w.Write([]byte(pdfContent))
			return
		}

		// Handle JSON files - *.json
		if filepath.Ext(path) == ".json" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)

			responseData := map[string]interface{}{
				"type":      "json-file",
				"path":      path,
				"timestamp": time.Now().Unix(),
				"data": map[string]interface{}{
					"message": "This is a JSON file for testing",
					"status":  "ok",
				},
			}
			json.NewEncoder(w).Encode(responseData)
			return
		}

		// Set proper content type for HTML files
		if filepath.Ext(path) == ".html" || path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		// Add headers to simulate real web server
		w.Header().Set("Server", "EdgeComet-TestServer/1.0")
		w.Header().Set("Cache-Control", "max-age=3600")

		// Try to serve from file system
		fileHandler.ServeHTTP(w, r)
	}))

	ts.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ts.port),
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		if err := ts.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Test server failed to start: %v\n", err)
		}
		close(ts.shutdown)
	}()

	// Wait for server to start
	return ts.waitForReady(30 * time.Second)
}

// Stop stops the test server
func (ts *TestServer) Stop() error {
	if ts.server == nil {
		return nil
	}

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ts.server.Shutdown(ctx)

	// Wait for shutdown to complete
	select {
	case <-ts.shutdown:
	case <-time.After(10 * time.Second):
		fmt.Println("Warning: Test server shutdown timed out")
	}

	return err
}

// BaseURL returns the base URL for the test server
func (ts *TestServer) BaseURL() string {
	return ts.baseURL
}

// waitForReady waits for the server to be ready to accept connections
func (ts *TestServer) waitForReady(timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(ts.baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("test server did not start within %v", timeout)
}

// IsRunning checks if the server is currently running
func (ts *TestServer) IsRunning() bool {
	if ts.server == nil {
		return false
	}

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(ts.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

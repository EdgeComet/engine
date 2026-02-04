package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// TestServer manages a local HTTP server for serving test fixtures
type TestServer struct {
	server   *http.Server
	port     int
	baseURL  string
	shutdown chan struct{}
}

// NewTestServer creates a new test server instance
func NewTestServer(port int) *TestServer {
	return &TestServer{
		port:     port,
		baseURL:  fmt.Sprintf("http://localhost:%d", port),
		shutdown: make(chan struct{}),
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

	// Bypass cache sharding test endpoints

	// Large JSON endpoint - returns >100 bytes for replication testing
	mux.HandleFunc("/api/large-json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, time.Now().UnixNano()))
		w.Header().Set("Last-Modified", time.Now().Format(time.RFC1123))
		w.WriteHeader(http.StatusOK)

		// Generate JSON response >100 bytes (threshold for replication)
		// This should trigger replication across EGs
		responseData := map[string]interface{}{
			"type":      "large-json-response",
			"path":      r.URL.Path,
			"query":     r.URL.RawQuery,
			"timestamp": time.Now().Unix(),
			"uuid":      uuid.New().String(),
			"data": map[string]interface{}{
				"message":     "This is a large JSON response for bypass cache sharding tests",
				"description": "Body size exceeds 100 bytes, should replicate to cluster",
				"items": []map[string]interface{}{
					{"id": 1, "name": "Item One"},
					{"id": 2, "name": "Item Two"},
					{"id": 3, "name": "Item Three"},
				},
			},
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// Small redirect endpoint - returns 302 with minimal body
	mux.HandleFunc("/api/small-redirect", func(w http.ResponseWriter, r *http.Request) {
		// Parse status code (default 302)
		statusCode := 302
		if code := r.URL.Query().Get("status"); code != "" {
			if parsed, err := strconv.Atoi(code); err == nil && parsed >= 300 && parsed < 400 {
				statusCode = parsed
			}
		}

		// Set redirect location
		location := r.URL.Query().Get("location")
		if location == "" {
			location = "http://localhost:9220/static/test.html"
		}
		w.Header().Set("Location", location)

		// Set minimal headers
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(statusCode)

		// Minimal body (< 100 bytes, should NOT replicate)
		w.Write([]byte("Redirect"))
	})

	// Small response endpoint - returns 200 with minimal body
	mux.HandleFunc("/api/small-response", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Minimal body (< 100 bytes, should NOT replicate)
		w.Write([]byte("OK"))
	})

	// Status filter endpoint - returns different status codes for testing
	mux.HandleFunc("/api/status-filter", func(w http.ResponseWriter, r *http.Request) {
		// Parse status code from query param (default 200)
		statusCode := 200
		if code := r.URL.Query().Get("status"); code != "" {
			if parsed, err := strconv.Atoi(code); err == nil && parsed >= 100 && parsed < 600 {
				statusCode = parsed
			}
		}

		// Set headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, time.Now().UnixNano()))
		w.Header().Set("Last-Modified", time.Now().Format(time.RFC1123))
		w.WriteHeader(statusCode)

		// Return JSON body (>100 bytes for replication)
		responseData := map[string]interface{}{
			"type":      "status-filter-response",
			"path":      r.URL.Path,
			"query":     r.URL.RawQuery,
			"status":    statusCode,
			"timestamp": time.Now().Unix(),
			"uuid":      uuid.New().String(),
			"message":   fmt.Sprintf("Response with status code %d", statusCode),
			"data": map[string]interface{}{
				"description": "Used to test bypass cache status code filtering",
				"cacheable":   statusCode == 200 || statusCode == 404,
			},
		}
		json.NewEncoder(w).Encode(responseData)
	})

	// Generic API handler - catches /api/* not handled by specific endpoints above
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
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
				location = "http://localhost:9000/static/simple.html"
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

	// Compression test handler - generates > 2KB of HTML content for compression tests
	mux.HandleFunc("/compression-test/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		pageName := path[len("/compression-test/"):]
		if pageName == "" {
			pageName = "index"
		}

		// Build content blocks to ensure > 1024 bytes (compression threshold)
		var contentBlocks string
		for i := 1; i <= 10; i++ {
			contentBlocks += fmt.Sprintf(`
		<div class="content-block" id="block-%d">
			<h3>Content Block %d</h3>
			<p>This is content block number %d in the compression test page.
			   It contains enough text to ensure the page exceeds the minimum compression
			   threshold of 1024 bytes. Each block adds approximately 200 bytes of content.</p>
		</div>`, i, i, i)
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Compression Test Page - %s</title>
	<meta name="description" content="Page for testing cache compression in sharding">
	<style>
		body { font-family: Arial, sans-serif; margin: 20px; }
		.content-block { background: #f5f5f5; padding: 15px; margin: 10px 0; border-radius: 5px; }
	</style>
</head>
<body>
	<h1>Compression Test Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div id="test-marker">COMPRESSION_TEST_PAGE</div>
	<div id="content-blocks">%s</div>
</body>
</html>`, pageName, pageName, path, contentBlocks)

		w.Write([]byte(html))
	})

	// Compression pull-only test handler - generates > 2KB content for pull-only compression tests
	mux.HandleFunc("/compression-pull-only/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		path := r.URL.Path
		pageName := path[len("/compression-pull-only/"):]
		if pageName == "" {
			pageName = "index"
		}

		// Build content blocks to ensure > 1024 bytes (compression threshold)
		var contentBlocks string
		for i := 1; i <= 10; i++ {
			contentBlocks += fmt.Sprintf(`
		<div class="content-block" id="block-%d">
			<h3>Content Block %d</h3>
			<p>This is content block number %d in the compression pull-only test page.
			   It contains enough text to ensure the page exceeds the minimum compression
			   threshold of 1024 bytes. Each block adds approximately 200 bytes of content.</p>
		</div>`, i, i, i)
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Compression Pull-Only Test Page - %s</title>
	<meta name="description" content="Page for testing cache compression in pull-only sharding mode">
	<style>
		body { font-family: Arial, sans-serif; margin: 20px; }
		.content-block { background: #f5f5f5; padding: 15px; margin: 10px 0; border-radius: 5px; }
	</style>
</head>
<body>
	<h1>Compression Pull-Only Test Page</h1>
	<p class="page-name">Page: %s</p>
	<p class="path">Path: %s</p>
	<div id="test-marker">COMPRESSION_PULL_ONLY_TEST_PAGE</div>
	<div id="content-blocks">%s</div>
</body>
</html>`, pageName, pageName, path, contentBlocks)

		w.Write([]byte(html))
	})

	// Large HTML generator endpoint - /static/large.html
	// Dynamically generates a 10MB+ HTML file for testing large file handling
	mux.HandleFunc("/static/large.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Write header
		header := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Large Test Page</title>
    <meta name="description" content="Large HTML file (10MB+) for testing cache push/pull with large files">
    <style>
        body {
            font-family: Arial, sans-serif;
            line-height: 1.6;
            margin: 20px;
            background-color: #f4f4f4;
        }
        .block {
            background: white;
            padding: 15px;
            margin-bottom: 10px;
            border-radius: 5px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 { color: #333; }
        h3 { color: #555; }
        .content { color: #666; }
    </style>
</head>
<body>
    <h1>Large Test Page</h1>
    <p>This is a large HTML file (approximately 10MB+) used for testing cache sharding with large files.</p>
    <p>The file contains repeated content blocks to reach the target size while remaining valid HTML.</p>

    <div id="content-blocks">
`
		w.Write([]byte(header))

		// Generate ~1KB per block, need ~10,500 blocks for >10MB
		loremIpsum := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. "

		// Repeat lorem ipsum to make ~800 bytes per block content
		blockContent := loremIpsum + loremIpsum

		// Generate 11,000 blocks (~11MB total)
		for i := 1; i <= 11000; i++ {
			block := fmt.Sprintf(`        <div class="block" id="block-%d">
            <h3>Content Block #%d</h3>
            <div class="content">
                <p>%s</p>
                <p>Block ID: %d | Timestamp: %d | Size marker for testing large file handling in cache sharding system.</p>
            </div>
        </div>
`, i, i, blockContent, i, i*12345)
			w.Write([]byte(block))
		}

		// Write footer
		footer := `    </div>

    <footer style="margin-top: 50px; padding: 20px; background: #333; color: white; text-align: center;">
        <p>Large Test Page - Generated Dynamically for Cache Sharding Tests</p>
        <p>Total Content Blocks: 11,000 | Approximate Size: 10-12 MB</p>
    </footer>
</body>
</html>
`
		w.Write([]byte(footer))
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
		w.Header().Set("Cache-Control", "no-cache")

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

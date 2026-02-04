package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RequestResult struct {
	Success        bool
	StatusCode     int
	Duration       time.Duration
	BytesReceived  int
	RenderSource   string
	RequestID      string
	Error          string
	ExpectedStatus int
	IsMismatch     bool
	Host           string
	URL            string
}

func buildRequest(gateway string, targetURL string, renderKey string, userAgent string) (*http.Request, error) {
	endpoint := fmt.Sprintf("%s/render?url=%s", gateway, url.QueryEscape(targetURL))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Render-Key", renderKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Request-ID", uuid.New().String())

	return req, nil
}

func executeRequest(client *http.Client, req *http.Request, expectedStatus int, host string, targetURL string) *RequestResult {
	start := time.Now()

	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return &RequestResult{
			Success:        false,
			Error:          categorizeError(err),
			Duration:       elapsed,
			ExpectedStatus: expectedStatus,
			Host:           host,
			URL:            targetURL,
		}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return &RequestResult{
			Success:        false,
			Error:          "body_read_error",
			Duration:       elapsed,
			StatusCode:     resp.StatusCode,
			RequestID:      resp.Header.Get("X-Request-ID"),
			ExpectedStatus: expectedStatus,
			Host:           host,
			URL:            targetURL,
		}
	}

	renderSource := resp.Header.Get("X-Render-Source")
	requestID := resp.Header.Get("X-Request-ID")

	isMismatch := false
	if expectedStatus > 0 && expectedStatus != resp.StatusCode {
		isMismatch = true
	}

	return &RequestResult{
		Success:        true,
		StatusCode:     resp.StatusCode,
		Duration:       elapsed,
		BytesReceived:  len(bodyBytes),
		RenderSource:   renderSource,
		RequestID:      requestID,
		ExpectedStatus: expectedStatus,
		IsMismatch:     isMismatch,
		Host:           host,
		URL:            targetURL,
	}
}

func categorizeError(err error) string {
	errStr := err.Error()

	if os.IsTimeout(err) || strings.Contains(errStr, "timeout") {
		return "timeout"
	}

	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "connection reset") {
		return "connection_refused"
	}

	if strings.Contains(errStr, "no such host") {
		return "dns_error"
	}

	return "network_error_other"
}

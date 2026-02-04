package configtest

import (
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/internal/edge/validate"
	"github.com/edgecomet/engine/pkg/types"
)

// URLTestResult contains the result of URL testing
type URLTestResult struct {
	URL         string
	IsAbsolute  bool
	HostResults []HostTestResult
	Error       string // Error message if host not found
}

// HostTestResult contains test result for a single host
type HostTestResult struct {
	HostID        int
	Host          string
	OriginalURL   string // URL as provided for testing (before normalization)
	NormalizedURL string
	URLHash       string
	MatchedRule   *types.URLRule // nil if no rule matched (default behavior)
	Action        string
	Config        *config.ResolvedConfig
}

// TestURL tests how a URL will be processed by the system
func TestURL(testURL string, result *validate.ValidationResult) (*URLTestResult, error) {
	// Load configuration from validated path
	egConfig, hostsConfig, err := loadConfigFromPath(result.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return testURLWithConfig(testURL, egConfig, hostsConfig)
}

// testURLWithConfig tests URL with loaded configuration
func testURLWithConfig(testURL string, egConfig *config.EgConfig, hostsConfig *config.HostsConfig) (*URLTestResult, error) {

	urlResult := &URLTestResult{
		URL: testURL,
	}

	// Parse URL to determine if it's absolute or relative
	parsedURL, err := url.Parse(testURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Determine if URL is absolute (has scheme and host)
	urlResult.IsAbsolute = parsedURL.Scheme != "" && parsedURL.Host != ""

	if urlResult.IsAbsolute {
		// Test against specific host
		return testAbsoluteURL(testURL, parsedURL, egConfig, hostsConfig)
	}

	// Test against all hosts
	return testRelativeURL(testURL, egConfig, hostsConfig)
}

// testAbsoluteURL tests an absolute URL against its specific host
func testAbsoluteURL(testURL string, parsedURL *url.URL, egConfig *config.EgConfig, hostsConfig *config.HostsConfig) (*URLTestResult, error) {
	urlResult := &URLTestResult{
		URL:        testURL,
		IsAbsolute: true,
	}

	// Find matching host by domain
	host := findHostByDomain(parsedURL.Host, hostsConfig)
	if host == nil {
		// Host not found - collect available hosts
		availableHosts := make([]string, 0, len(hostsConfig.Hosts))
		for _, h := range hostsConfig.Hosts {
			availableHosts = append(availableHosts, h.Domain)
		}

		urlResult.Error = fmt.Sprintf("Host %q not found in configuration\nAvailable hosts:\n  - %s",
			parsedURL.Host,
			strings.Join(availableHosts, "\n  - "))
		return urlResult, nil
	}

	// Test URL against this host
	hostResult := testURLAgainstHost(testURL, host, egConfig)
	urlResult.HostResults = []HostTestResult{hostResult}

	return urlResult, nil
}

// testRelativeURL tests a relative URL against all configured hosts
func testRelativeURL(testURL string, egConfig *config.EgConfig, hostsConfig *config.HostsConfig) (*URLTestResult, error) {
	urlResult := &URLTestResult{
		URL:        testURL,
		IsAbsolute: false,
	}

	// Test against all hosts
	for i := range hostsConfig.Hosts {
		host := &hostsConfig.Hosts[i]

		// Construct full URL for this host
		fullURL := fmt.Sprintf("https://%s%s", host.Domain, testURL)

		hostResult := testURLAgainstHost(fullURL, host, egConfig)
		urlResult.HostResults = append(urlResult.HostResults, hostResult)
	}

	return urlResult, nil
}

// loadConfigFromPath loads configuration using the config manager
func loadConfigFromPath(configPath string) (*config.EgConfig, *config.HostsConfig, error) {
	// Create a nop logger for testing (no output)
	logger := zap.NewNop()

	// Use the existing config manager to load config
	cm, err := config.NewEGConfigManager(configPath, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get config and hosts
	egConfig := cm.GetConfig()
	hosts := cm.GetHosts()

	return egConfig, &config.HostsConfig{Hosts: hosts}, nil
}

// testURLAgainstHost tests a URL against a specific host configuration
func testURLAgainstHost(testURL string, host *types.Host, globalConfig *config.EgConfig) HostTestResult {
	// Normalize URL
	normalizer := hash.NewURLNormalizer()
	normalizeResult, err := normalizer.Normalize(testURL, nil)

	normalizedURL := testURL
	urlHash := "error"

	if err == nil {
		normalizedURL = normalizeResult.NormalizedURL
		urlHash = normalizer.Hash(normalizeResult.NormalizedURL)
	}

	// Create resolver
	resolver := config.NewConfigResolver(&globalConfig.Render, &globalConfig.Bypass, globalConfig.TrackingParams, globalConfig.CacheSharding, globalConfig.BothitRecache, globalConfig.Headers, globalConfig.Storage.Compression, host)

	// Resolve configuration for URL
	resolvedConfig := resolver.ResolveForURL(normalizedURL)

	// Find matching rule (if any)
	matcher := config.NewPatternMatcher(host.URLRules)
	matchedRule, _ := matcher.FindMatchingRule(normalizedURL)

	// Build result
	hostResult := HostTestResult{
		HostID:        host.ID,
		Host:          host.Domain,
		OriginalURL:   testURL,
		NormalizedURL: normalizedURL,
		URLHash:       urlHash,
		MatchedRule:   matchedRule,
		Action:        string(resolvedConfig.Action),
		Config:        resolvedConfig,
	}

	return hostResult
}

// findHostByDomain finds a host by its domain name
func findHostByDomain(domain string, hostsConfig *config.HostsConfig) *types.Host {
	// Normalize domain for comparison
	domain = strings.ToLower(domain)
	domain = strings.TrimSuffix(domain, ".")

	// Remove port if present
	if idx := strings.Index(domain, ":"); idx != -1 {
		domain = domain[:idx]
	}

	for i := range hostsConfig.Hosts {
		host := &hostsConfig.Hosts[i]
		hostDomain := strings.ToLower(host.Domain)
		if hostDomain == domain {
			return host
		}
	}

	return nil
}

package configtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/edge/validate"
	"github.com/edgecomet/engine/pkg/types"
)

func TestTestURL_AbsoluteURL(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	tests := []struct {
		name           string
		url            string
		expectedHost   string
		expectedAction string
		expectError    bool
	}{
		{
			name:           "Blog post URL",
			url:            "https://example.com/blog/post-123",
			expectedHost:   "example.com",
			expectedAction: "render",
			expectError:    false,
		},
		{
			name:           "Admin URL (status action)",
			url:            "https://example.com/admin/users",
			expectedHost:   "example.com",
			expectedAction: "status_403",
			expectError:    false,
		},
		{
			name:           "Account page (bypass)",
			url:            "https://example.com/account/settings",
			expectedHost:   "example.com",
			expectedAction: "bypass",
			expectError:    false,
		},
		{
			name:           "Static file (bypass with cache)",
			url:            "https://example.com/static/main.js",
			expectedHost:   "example.com",
			expectedAction: "bypass",
			expectError:    false,
		},
		{
			name:        "Unknown host",
			url:         "https://unknown-domain.com/page",
			expectError: false, // Not an error, but will have Error field set
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlResult, err := TestURL(tt.url, result)
			require.NoError(t, err)

			assert.True(t, urlResult.IsAbsolute)
			assert.Equal(t, tt.url, urlResult.URL)

			if tt.expectError || tt.expectedHost == "" {
				// Check for "host not found" error
				if urlResult.Error != "" {
					assert.Contains(t, urlResult.Error, "not found")
				}
			} else {
				assert.Equal(t, "", urlResult.Error, "Unexpected error")
				require.Len(t, urlResult.HostResults, 1)

				hostResult := urlResult.HostResults[0]
				assert.Equal(t, tt.expectedHost, hostResult.Host)
				assert.Equal(t, tt.expectedAction, hostResult.Action)
				assert.NotEmpty(t, hostResult.URLHash)
			}
		})
	}
}

func TestTestURL_RelativeURL(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	tests := []struct {
		name           string
		url            string
		expectedHosts  int
		expectedAction string // Action for first host
	}{
		{
			name:           "Blog path",
			url:            "/blog/post",
			expectedHosts:  4, // example.com, shop.example.com, blog.example.com, some-eshop.com
			expectedAction: "render",
		},
		{
			name:           "Admin path",
			url:            "/admin/users",
			expectedHosts:  4,
			expectedAction: "status_403", // example.com should block admin
		},
		{
			name:           "Root path",
			url:            "/",
			expectedHosts:  4,
			expectedAction: "render", // example.com has root pattern
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlResult, err := TestURL(tt.url, result)
			require.NoError(t, err)

			assert.False(t, urlResult.IsAbsolute)
			assert.Equal(t, tt.url, urlResult.URL)
			assert.Empty(t, urlResult.Error)

			assert.Len(t, urlResult.HostResults, tt.expectedHosts)

			// Check first host result
			firstHost := urlResult.HostResults[0]
			assert.Equal(t, tt.expectedAction, firstHost.Action)
			assert.NotEmpty(t, firstHost.URLHash)
		})
	}
}

func TestTestURL_PatternMatching(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	tests := []struct {
		name            string
		url             string
		expectedPattern string // Expected matched pattern
	}{
		{
			name:            "Blog wildcard match",
			url:             "https://example.com/blog/2024/jan/post",
			expectedPattern: "/blog/*",
		},
		{
			name:            "Exact match",
			url:             "https://example.com/",
			expectedPattern: "/",
		},
		{
			name:            "Static files",
			url:             "https://example.com/static/css/main.css",
			expectedPattern: "/static/*",
		},
		{
			name:            "No match (default)",
			url:             "https://shop.example.com/unmatched/path",
			expectedPattern: "(default)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlResult, err := TestURL(tt.url, result)
			require.NoError(t, err)

			require.Len(t, urlResult.HostResults, 1)
			hostResult := urlResult.HostResults[0]

			if tt.expectedPattern == "(default)" {
				assert.Nil(t, hostResult.MatchedRule, "Expected no matched rule")
			} else {
				require.NotNil(t, hostResult.MatchedRule, "Expected a matched rule")
				patterns := hostResult.MatchedRule.GetMatchPatterns()
				require.NotEmpty(t, patterns)
				assert.Equal(t, tt.expectedPattern, patterns[0])
			}
		})
	}
}

func TestTestURL_ConfigResolution(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	tests := []struct {
		name        string
		url         string
		checkConfig func(*testing.T, *HostTestResult)
	}{
		{
			name: "Render action with cache TTL",
			url:  "https://example.com/blog/post",
			checkConfig: func(t *testing.T, result *HostTestResult) {
				assert.Equal(t, types.ActionRender, result.Config.Action)
				assert.NotNil(t, result.Config)
				// Blog pattern has 2h cache TTL
				assert.Equal(t, int64(7200), int64(result.Config.Cache.TTL.Seconds()))
			},
		},
		{
			name: "Bypass action with cache",
			url:  "https://example.com/static/file.js",
			checkConfig: func(t *testing.T, result *HostTestResult) {
				assert.Equal(t, types.ActionBypass, result.Config.Action)
				assert.True(t, result.Config.Bypass.Cache.Enabled)
				// Static files have 24h bypass cache
				assert.Equal(t, int64(86400), int64(result.Config.Bypass.Cache.TTL.Seconds()))
			},
		},
		{
			name: "Status action",
			url:  "https://example.com/admin/users",
			checkConfig: func(t *testing.T, result *HostTestResult) {
				assert.Equal(t, types.ActionStatus403, result.Config.Action)
				assert.Equal(t, 403, result.Config.Status.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlResult, err := TestURL(tt.url, result)
			require.NoError(t, err)
			require.Len(t, urlResult.HostResults, 1)

			hostResult := urlResult.HostResults[0]
			tt.checkConfig(t, &hostResult)
		})
	}
}

func TestTestURL_URLNormalization(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	// Test that different URLs normalize to the same hash
	url1 := "https://example.com/blog/post"
	url2 := "https://example.com/blog/post/"
	url3 := "https://example.com/blog/post?utm_source=google"

	urlResult1, err := TestURL(url1, result)
	require.NoError(t, err)

	urlResult2, err := TestURL(url2, result)
	require.NoError(t, err)

	urlResult3, err := TestURL(url3, result)
	require.NoError(t, err)

	// All should normalize to similar base URL
	assert.Equal(t, urlResult1.HostResults[0].Host, urlResult2.HostResults[0].Host)
	assert.Equal(t, urlResult1.HostResults[0].Host, urlResult3.HostResults[0].Host)

	// Hashes should be generated
	assert.NotEmpty(t, urlResult1.HostResults[0].URLHash)
	assert.NotEmpty(t, urlResult2.HostResults[0].URLHash)
	assert.NotEmpty(t, urlResult3.HostResults[0].URLHash)
}

func TestFindHostByDomain(t *testing.T) {
	// First validate to get the configuration
	result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
	require.NoError(t, err)
	require.True(t, result.Valid)

	// Load config for testing
	egConfig, hostsConfig, err := loadConfigFromPath(result.ConfigPath)
	require.NoError(t, err)
	_ = egConfig // Not used in this test

	tests := []struct {
		name     string
		domain   string
		expected string // Expected domain or empty if not found
	}{
		{"Exact match", "example.com", "example.com"},
		{"Case insensitive", "EXAMPLE.COM", "example.com"},
		{"With port", "example.com:443", "example.com"},
		{"Subdomain exists", "shop.example.com", "shop.example.com"},
		{"Unknown domain", "unknown.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := findHostByDomain(tt.domain, hostsConfig)
			if tt.expected == "" {
				assert.Nil(t, host)
			} else {
				require.NotNil(t, host)
				assert.Equal(t, tt.expected, host.Domain)
			}
		})
	}
}

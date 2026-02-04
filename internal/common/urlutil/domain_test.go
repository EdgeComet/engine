package urlutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"simple URL", "https://example.com/path", "example.com"},
		{"with port", "https://example.com:8080/path", "example.com:8080"},
		{"with subdomain", "https://www.example.com/path", "www.example.com"},
		{"uppercase", "https://EXAMPLE.COM/path", "example.com"},
		{"invalid URL", "not-a-url", ""},
		{"empty string", "", ""},
		{"just path", "/path/to/resource", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractHost(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{"no port", "example.com", "example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"subdomain with port", "www.example.com:443", "www.example.com"},
		{"different ports same result", "api.example.com:9090", "api.example.com"},
		{"ipv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"ipv4 no port", "192.168.1.1", "192.168.1.1"},
		{"ipv6 with port", "[::1]:8080", "[::1]"},
		{"ipv6 no port", "[::1]", "[::1]"},
		{"ipv6 bare", "::1", "::1"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractHostname(tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name        string
		baseHost    string
		requestHost string
		expected    bool
	}{
		{"same host", "example.com", "example.com", true},
		{"subdomain of base", "example.com", "www.example.com", true},
		{"nested subdomain", "example.com", "cdn.static.example.com", true},
		{"base is subdomain", "www.example.com", "example.com", true},
		{"different domains", "example.com", "other.com", false},
		{"similar but different", "example.com", "notexample.com", false},
		{"different TLD", "example.com", "example.org", false},
		{"empty base", "", "example.com", false},
		{"empty request", "example.com", "", false},
		{"both empty", "", "", false},
		// Port handling - ports are ignored for same-origin comparison
		{"with ports same", "example.com:8080", "example.com:8080", true},
		{"with ports different", "example.com:8080", "example.com:9090", true},
		{"one with port one without", "example.com", "example.com:8080", true},
		{"subdomain with different ports", "example.com:8080", "www.example.com:9090", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSameOrigin(tt.baseHost, tt.requestHost)
			assert.Equal(t, tt.expected, result)
		})
	}
}

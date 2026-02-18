package urlutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// Loopback
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"loopback 127.255.255.255", "127.255.255.255", true},
		{"loopback IPv6", "::1", true},

		// RFC 1918
		{"rfc1918 10.0.0.1", "10.0.0.1", true},
		{"rfc1918 10.255.255.255", "10.255.255.255", true},
		{"rfc1918 172.16.0.1", "172.16.0.1", true},
		{"rfc1918 172.31.255.255", "172.31.255.255", true},
		{"rfc1918 192.168.0.1", "192.168.0.1", true},
		{"rfc1918 192.168.255.255", "192.168.255.255", true},

		// Link-local
		{"link-local 169.254.0.1", "169.254.0.1", true},
		{"link-local 169.254.169.254", "169.254.169.254", true},
		{"link-local IPv6 fe80::1", "fe80::1", true},

		// CGNAT (RFC 6598)
		{"cgnat 100.64.0.1", "100.64.0.1", true},
		{"cgnat 100.127.255.255", "100.127.255.255", true},

		// "This" network
		{"this-network 0.0.0.0", "0.0.0.0", true},
		{"this-network 0.255.255.255", "0.255.255.255", true},

		// Multicast
		{"multicast 224.0.0.1", "224.0.0.1", true},
		{"multicast 239.255.255.255", "239.255.255.255", true},
		{"multicast IPv6 ff02::1", "ff02::1", true},

		// IPv6 unique local
		{"unique-local fd00::1", "fd00::1", true},
		{"unique-local fc00::1", "fc00::1", true},

		// Public IPs
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 93.184.216.34", "93.184.216.34", false},
		{"public 172.32.0.1", "172.32.0.1", false},
		{"public 100.128.0.1", "100.128.0.1", false},
		{"public 11.0.0.1", "11.0.0.1", false},
		{"public IPv6 2001:db8::1", "2001:db8::1", false},
		{"public IPv6 2607:f8b0:4004:800::200e", "2607:f8b0:4004:800::200e", false},

		// Nil
		{"nil IP", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ip net.IP
			if tt.ip != "" {
				ip = net.ParseIP(tt.ip)
				require.NotNil(t, ip, "failed to parse test IP: %s", tt.ip)
			}
			assert.Equal(t, tt.private, IsPrivateIP(ip))
		})
	}
}

func TestValidateHostNotPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		wantError bool
	}{
		// Private IP literals should be blocked
		{"blocks loopback", "127.0.0.1", true},
		{"blocks rfc1918", "10.0.0.1", true},
		{"blocks link-local", "169.254.169.254", true},
		{"blocks cgnat", "100.64.0.1", true},
		{"blocks IPv6 loopback", "::1", true},
		{"blocks zero", "0.0.0.0", true},

		// Public IP literals should pass
		{"allows public IP", "8.8.8.8", false},
		{"allows public IPv6", "2607:f8b0:4004:800::200e", false},

		// Domain names should pass (no DNS resolution)
		{"allows domain", "example.com", false},
		{"allows subdomain", "internal.example.com", false},
		{"allows localhost domain", "localhost", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostNotPrivateIP(tt.hostname)
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "private/reserved")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResolvedIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		wantError bool
	}{
		{"blocks private", "192.168.1.1", true},
		{"blocks loopback", "127.0.0.1", true},
		{"blocks metadata endpoint", "169.254.169.254", true},
		{"allows public", "93.184.216.34", false},
		{"allows public IPv6", "2607:f8b0:4004:800::200e", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			err := ValidateResolvedIP(ip)
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "private/reserved")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

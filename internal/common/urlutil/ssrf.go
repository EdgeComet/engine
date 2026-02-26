package urlutil

import (
	"fmt"
	"net"
)

// privateRanges contains all private and reserved IP ranges that should be blocked
// to prevent SSRF attacks.
var privateRanges []*net.IPNet

func init() {
	cidrs := []string{
		// IPv4
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local
		"100.64.0.0/10",  // CGNAT (RFC 6598)
		"0.0.0.0/8",      // "this" network
		"224.0.0.0/4",    // multicast

		// IPv6
		"::1/128",   // loopback
		"fe80::/10", // link-local
		"fc00::/7",  // unique local
		"ff00::/8",  // multicast
	}

	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR in SSRF private ranges: %s", cidr))
		}
		privateRanges = append(privateRanges, ipNet)
	}
}

// IsPrivateIP returns true if the given IP belongs to a private or reserved range.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	for _, ipNet := range privateRanges {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateHostNotPrivateIP checks if a hostname is a private IP literal.
// It does NOT perform DNS resolution -- only rejects IP addresses that parse
// directly to private ranges. Domain names pass through (use ValidateResolvedIP
// after DNS resolution for full protection).
func ValidateHostNotPrivateIP(hostname string) error {
	ip := net.ParseIP(hostname)
	if ip == nil {
		// Not an IP literal (could be a domain name); allow it through
		return nil
	}

	if IsPrivateIP(ip) {
		return fmt.Errorf("hostname resolves to private/reserved IP address: %s", hostname)
	}
	return nil
}

// ValidateResolvedIP checks if a resolved IP address belongs to a private or
// reserved range. Use this after DNS resolution to block DNS rebinding attacks.
func ValidateResolvedIP(ip net.IP) error {
	if IsPrivateIP(ip) {
		return fmt.Errorf("resolved IP is in a private/reserved range: %s", ip.String())
	}
	return nil
}

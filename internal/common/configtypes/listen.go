package configtypes

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseListenAddress parses a listen address string and returns host and port.
// Supported formats:
//   - ":10070"           -> host="", port=9070 (all interfaces)
//   - "0.0.0.0:10070"    -> host="0.0.0.0", port=9070
//   - "localhost:10070"  -> host="localhost", port=9070
//   - "192.168.1.1:10070"-> host="192.168.1.1", port=9070
func ParseListenAddress(listen string) (host string, port int, err error) {
	if listen == "" {
		return "", 0, fmt.Errorf("listen address is empty")
	}

	// Handle case where only port number is provided (without colon)
	if !strings.Contains(listen, ":") {
		p, err := strconv.Atoi(listen)
		if err != nil {
			return "", 0, fmt.Errorf("invalid listen address format: %s", listen)
		}
		return "", p, nil
	}

	host, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, fmt.Errorf("invalid listen address format: %s: %w", listen, err)
	}

	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in listen address: %s", portStr)
	}

	return host, port, nil
}

// ValidateListenAddress validates that a listen address is properly formatted
// and the port is within valid range.
func ValidateListenAddress(listen string) error {
	if listen == "" {
		return fmt.Errorf("listen address is empty")
	}

	_, port, err := ParseListenAddress(listen)
	if err != nil {
		return err
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}

	return nil
}

// GetPortFromListen extracts just the port number from a listen address.
func GetPortFromListen(listen string) (int, error) {
	_, port, err := ParseListenAddress(listen)
	return port, err
}

// NormalizeListen ensures the listen address is in host:port format.
// If only port is provided, returns ":port".
func NormalizeListen(listen string) (string, error) {
	host, port, err := ParseListenAddress(listen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, port), nil
}

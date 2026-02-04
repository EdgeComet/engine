package tls

import (
	"crypto/tls"
	"fmt"
	"net"
)

// CreateTLSListener creates a TLS-wrapped TCP listener with the specified certificate and key.
// It enforces TLS 1.3 as the minimum version.
func CreateTLSListener(address, certFile, keyFile string) (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	tcpListener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP listener: %w", err)
	}

	return tls.NewListener(tcpListener, tlsConfig), nil
}

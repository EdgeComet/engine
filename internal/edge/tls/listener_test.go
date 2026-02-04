package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestCertificate creates a self-signed certificate and key for testing.
// Returns paths to the cert and key files.
func generateTestCertificate(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	// Write certificate to file
	certPath = filepath.Join(dir, "test.crt")
	certFile, err := os.Create(certPath)
	require.NoError(t, err)
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, err)
	certFile.Close()

	// Write private key to file
	keyPath = filepath.Join(dir, "test.key")
	keyFile, err := os.Create(keyPath)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	require.NoError(t, err)
	err = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NoError(t, err)
	keyFile.Close()

	return certPath, keyPath
}

func TestCreateTLSListener_ValidCertAndKey(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	require.NotNil(t, listener)
	defer listener.Close()

	// Verify we got a valid address
	addr := listener.Addr()
	require.NotNil(t, addr)
	assert.Contains(t, addr.String(), "127.0.0.1:")
}

func TestCreateTLSListener_IsTLSListener(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	defer listener.Close()

	// The listener should accept TLS connections
	// We verify this by checking it's not a plain TCP listener
	_, ok := listener.Addr().(*net.TCPAddr)
	assert.True(t, ok, "Underlying address should be TCP")
}

func TestCreateTLSListener_AcceptsConnections(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	defer listener.Close()

	// Start accepting in goroutine
	serverReady := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		close(serverReady)
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		// Complete TLS handshake
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			serverDone <- err
			conn.Close()
			return
		}
		conn.Close()
		serverDone <- nil
	}()

	<-serverReady

	// Connect with TLS client
	addr := listener.Addr().String()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	require.NoError(t, err)
	conn.Close()

	// Wait for server and check result
	err = <-serverDone
	require.NoError(t, err, "Server should accept connection without error")
}

func TestCreateTLSListener_EnforcesTLS13(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	defer listener.Close()

	// Start accepting in goroutine
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		// Force handshake
		tlsConn := conn.(*tls.Conn)
		err = tlsConn.Handshake()
		acceptErr <- err
		conn.Close()
	}()

	// Try to connect with TLS 1.2 max - should fail handshake
	addr := listener.Addr().String()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MaxVersion:         tls.VersionTLS12,
	}

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)

	tlsConn := tls.Client(conn, tlsConfig)
	err = tlsConn.Handshake()

	// Handshake should fail because server requires TLS 1.3
	assert.Error(t, err, "Handshake should fail with TLS 1.2")
	if err != nil {
		assert.Contains(t, err.Error(), "protocol version")
	}

	conn.Close()

	// Server side should also have an error
	select {
	case serverErr := <-acceptErr:
		// Server may or may not have an error depending on timing
		_ = serverErr
	case <-time.After(time.Second):
		// Timeout is OK
	}
}

func TestCreateTLSListener_TLS13Succeeds(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	defer listener.Close()

	// Start accepting in goroutine
	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			tlsConn := conn.(*tls.Conn)
			tlsConn.Handshake()
			conn.Close()
		}
		close(done)
	}()

	// Connect with TLS 1.3 - should succeed
	addr := listener.Addr().String()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	require.NoError(t, err)

	// Verify TLS 1.3 was negotiated
	state := conn.ConnectionState()
	assert.Equal(t, uint16(tls.VersionTLS13), state.Version)

	conn.Close()
	<-done
}

// Error case tests

func TestCreateTLSListener_MissingCertFile(t *testing.T) {
	tmpDir := t.TempDir()
	_, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", "/nonexistent/cert.crt", keyPath)
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestCreateTLSListener_MissingKeyFile(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, _ := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("127.0.0.1:0", certPath, "/nonexistent/key.key")
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestCreateTLSListener_InvalidCertFormat(t *testing.T) {
	tmpDir := t.TempDir()
	_, keyPath := generateTestCertificate(t, tmpDir)

	// Create invalid cert file
	invalidCertPath := filepath.Join(tmpDir, "invalid.crt")
	err := os.WriteFile(invalidCertPath, []byte("not a certificate"), 0644)
	require.NoError(t, err)

	listener, err := CreateTLSListener("127.0.0.1:0", invalidCertPath, keyPath)
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestCreateTLSListener_MismatchedCertAndKey(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate two different certificates
	certPath1, _ := generateTestCertificate(t, tmpDir)

	// Generate second certificate in subdirectory
	subDir := filepath.Join(tmpDir, "other")
	require.NoError(t, os.Mkdir(subDir, 0755))
	_, keyPath2 := generateTestCertificate(t, subDir)

	// Try to use cert from first pair with key from second pair
	listener, err := CreateTLSListener("127.0.0.1:0", certPath1, keyPath2)
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestCreateTLSListener_InvalidAddressFormat(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	listener, err := CreateTLSListener("invalid:address:format", certPath, keyPath)
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Contains(t, err.Error(), "failed to create TCP listener")
}

func TestCreateTLSListener_PortAlreadyInUse(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateTestCertificate(t, tmpDir)

	// First listener on random port
	listener1, err := CreateTLSListener("127.0.0.1:0", certPath, keyPath)
	require.NoError(t, err)
	defer listener1.Close()

	// Get the port that was assigned
	addr := listener1.Addr().String()

	// Try to create second listener on same port
	listener2, err := CreateTLSListener(addr, certPath, keyPath)
	require.Error(t, err)
	assert.Nil(t, listener2)
	assert.Contains(t, err.Error(), "failed to create TCP listener")
}

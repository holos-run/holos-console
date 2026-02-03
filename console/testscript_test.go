package console_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/holos-run/holos-console/console"
)

func TestScripts(t *testing.T) {
	if _, err := exec.LookPath("grpcurl"); err != nil {
		t.Skip("grpcurl not installed")
	}

	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"startserver": startServer,
		},
	})
}

func startServer(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("startserver does not support negation")
	}
	if len(args) != 0 {
		ts.Fatalf("usage: startserver")
	}

	addr, err := freeAddr()
	if err != nil {
		ts.Fatalf("allocate server address: %v", err)
	}

	// Generate CA and server certificates programmatically.
	workDir := ts.Getenv("WORK")
	caCertPEM, caKey, err := generateTestCA()
	if err != nil {
		ts.Fatalf("generate test CA: %v", err)
	}
	certPEM, keyPEM, err := generateTestServerCert(caCertPEM, caKey)
	if err != nil {
		ts.Fatalf("generate test server cert: %v", err)
	}

	caCertPath := filepath.Join(workDir, "ca.pem")
	certPath := filepath.Join(workDir, "tls.crt")
	keyPath := filepath.Join(workDir, "tls.key")

	if err := os.WriteFile(caCertPath, caCertPEM, 0600); err != nil {
		ts.Fatalf("write CA cert: %v", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		ts.Fatalf("write server cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		ts.Fatalf("write server key: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := console.New(console.Config{
		ListenAddr: addr,
		CertFile:   certPath,
		KeyFile:    keyPath,
		CACertFile: caCertPath,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()

	ts.Defer(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				ts.Logf("server shutdown error: %v", err)
			}
		case <-time.After(5 * time.Second):
			ts.Fatalf("server shutdown timeout")
		}
	})

	if err := waitForTCP(addr, 5*time.Second); err != nil {
		cancel()
		ts.Fatalf("server did not start: %v", err)
	}

	ts.Setenv("SERVER_ADDR", addr)
}

// generateTestCA creates a self-signed CA certificate and returns the
// PEM-encoded certificate, the CA private key, and any error.
func generateTestCA() (caCertPEM []byte, caKey *ecdsa.PrivateKey, err error) {
	caKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Test CA"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return caCertPEM, caKey, nil
}

// generateTestServerCert creates a server certificate signed by the given CA,
// valid for 127.0.0.1 and localhost. Returns PEM-encoded cert and key.
func generateTestServerCert(caCertPEM []byte, caKey *ecdsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return nil, nil, errors.New("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Test Server"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	serverKeyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyBytes})
	return certPEM, keyPEM, nil
}

func freeAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

func waitForTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return lastErr
}

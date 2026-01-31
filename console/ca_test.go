package console

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCACertPool_Empty(t *testing.T) {
	pool, err := loadCACertPool("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool != nil {
		t.Fatal("expected nil pool for empty path")
	}
}

func TestLoadCACertPool_MissingFile(t *testing.T) {
	_, err := loadCACertPool("/nonexistent/ca.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCACertPool_InvalidPEM(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.pem")
	if err := os.WriteFile(f, []byte("not a cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCACertPool(f)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestLoadCACertPool_ValidPEM(t *testing.T) {
	// Use the mkcert CA root if available, otherwise skip
	caFile := mkcertCARootPEM(t)
	pool, err := loadCACertPool(caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

// TestHTTPClientWithCA_RejectsUnknownCA proves that TLS validation is enforced.
// A server using a self-signed cert that is NOT in the client's CA pool must be
// rejected.
func TestHTTPClientWithCA_RejectsUnknownCA(t *testing.T) {
	// Create a TLS server with a self-signed cert (not trusted by any CA pool)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Client with empty CA pool (system roots only) should reject the test server's cert
	client := httpClientWithCA(nil)
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS error when connecting to server with unknown CA, but got none")
	}
}

// TestHTTPClientWithCA_AcceptsKnownCA proves that a client with the correct CA
// pool can connect to a server whose cert is signed by that CA.
func TestHTTPClientWithCA_AcceptsKnownCA(t *testing.T) {
	// Create a TLS server with a self-signed cert
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Build a pool containing the test server's CA cert
	pool := x509.NewCertPool()
	for _, cert := range srv.TLS.Certificates {
		if cert.Leaf != nil {
			pool.AddCert(cert.Leaf)
		} else {
			parsed, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				t.Fatalf("parse cert: %v", err)
			}
			pool.AddCert(parsed)
		}
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected successful connection with known CA, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestHTTPClientWithCA_MkcertCerts verifies that httpClientWithCA can connect
// to a server using mkcert-generated certificates when the mkcert CA is loaded.
func TestHTTPClientWithCA_MkcertCerts(t *testing.T) {
	repoRoot := repoRootDir(t)
	certFile := filepath.Join(repoRoot, "certs", "tls.crt")
	keyFile := filepath.Join(repoRoot, "certs", "tls.key")

	if _, err := os.Stat(certFile); err != nil {
		t.Skip("mkcert certs not found, run 'make certs' first")
	}

	caFile := mkcertCARootPEM(t)

	// Load mkcert CA pool
	pool, err := loadCACertPool(caFile)
	if err != nil {
		t.Fatalf("load CA pool: %v", err)
	}

	// Start a TLS server using mkcert certs
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load mkcert keypair: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	// Client WITHOUT mkcert CA should fail
	badClient := httpClientWithCA(nil)
	_, err = badClient.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS error without mkcert CA, but connection succeeded")
	}

	// Client WITH mkcert CA should succeed
	goodClient := httpClientWithCA(pool)
	resp, err := goodClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected successful connection with mkcert CA, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// mkcertCARootPEM returns the path to mkcert's rootCA.pem, skipping the test
// if mkcert is not installed or the CA root is not found.
func mkcertCARootPEM(t *testing.T) string {
	t.Helper()
	caRoot := os.Getenv("MKCERT_CAROOT")
	if caRoot == "" {
		// Try default locations
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home directory")
		}
		caRoot = filepath.Join(home, ".local", "share", "mkcert")
	}
	caFile := filepath.Join(caRoot, "rootCA.pem")
	if _, err := os.Stat(caFile); err != nil {
		t.Skipf("mkcert CA root not found at %s", caFile)
	}
	return caFile
}

// repoRootDir returns the repository root directory.
func repoRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// console/ package is one level down from repo root
	return filepath.Clean(filepath.Join(wd, ".."))
}

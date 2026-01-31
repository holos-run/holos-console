package console_test

import (
	"context"
	"errors"
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

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, ".."))

	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Setup: func(env *testscript.Env) error {
			env.Setenv("REPO", repoRoot)
			return nil
		},
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

	repoRoot := ts.Getenv("REPO")
	if repoRoot == "" {
		ts.Fatalf("REPO environment variable not set")
	}

	addr, err := freeAddr()
	if err != nil {
		ts.Fatalf("allocate server address: %v", err)
	}

	certPath := filepath.Join(repoRoot, "certs", "tls.crt")
	keyPath := filepath.Join(repoRoot, "certs", "tls.key")

	// Locate mkcert CA root for TLS verification
	home, err := os.UserHomeDir()
	if err != nil {
		ts.Fatalf("user home dir: %v", err)
	}
	caRoot := os.Getenv("MKCERT_CAROOT")
	if caRoot == "" {
		caRoot = filepath.Join(home, ".local", "share", "mkcert")
	}
	caCertPath := filepath.Join(caRoot, "rootCA.pem")

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

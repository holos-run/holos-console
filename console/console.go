package console

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

//go:embed ui
var uiFS embed.FS

// Config holds the server configuration.
type Config struct {
	ListenAddr string
	CertFile   string
	KeyFile    string
}

// Server represents the console HTTPS server.
type Server struct {
	cfg Config
}

// New creates a new Server with the given configuration.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Serve starts the HTTPS server and blocks until the context is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	mux := http.NewServeMux()

	// Configure ConnectRPC interceptors
	interceptors := connect.WithInterceptors(
		rpc.MetricsInterceptor(),
		rpc.LoggingInterceptor(),
	)

	// Register VersionService
	versionHandler := rpc.NewVersionHandler(rpc.VersionInfo{
		Version:      GetVersion(),
		GitCommit:    GitCommit,
		GitTreeState: GitTreeState,
		BuildDate:    BuildDate,
	})
	path, handler := consolev1connect.NewVersionServiceHandler(versionHandler, interceptors)
	mux.Handle(path, handler)

	// Register gRPC reflection for introspection (grpcurl, etc.)
	reflector := grpcreflect.NewStaticReflector(consolev1connect.VersionServiceName)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	mux.Handle(reflectPath, reflectHandler)
	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
	mux.Handle(reflectAlphaPath, reflectAlphaHandler)

	// Redirect / to /ui/
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Redirect /ui to /ui/
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ui" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Serve embedded UI files at /ui/
	uiContent, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return fmt.Errorf("failed to create sub filesystem: %w", err)
	}
	mux.Handle("/ui/", newUIHandler(uiContent))

	// Expose Prometheus metrics at /metrics
	mux.Handle("/metrics", promhttp.Handler())

	// Wrap with h2c for HTTP/2 cleartext support (needed for gRPC over HTTP/2)
	h2cHandler := h2c.NewHandler(mux, &http2.Server{})
	loggedHandler := logRequests(h2cHandler)

	server := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: loggedHandler,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	// Configure TLS
	tlsConfig, err := s.tlsConfig()
	if err != nil {
		return fmt.Errorf("failed to configure TLS: %w", err)
	}
	server.TLSConfig = tlsConfig

	// Start server
	slog.Info("starting server", "addr", s.cfg.ListenAddr)

	errCh := make(chan error, 1)
	go func() {
		if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
			errCh <- server.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile)
		} else {
			// Use auto-generated certificate
			listener, err := tls.Listen("tcp", s.cfg.ListenAddr, tlsConfig)
			if err != nil {
				errCh <- fmt.Errorf("failed to create TLS listener: %w", err)
				return
			}
			errCh <- server.Serve(listener)
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *loggingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		writer := &loggingResponseWriter{ResponseWriter: w}

		next.ServeHTTP(writer, r)

		status := writer.statusCode
		if status == 0 {
			status = http.StatusOK
		}

		remoteAddr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			remoteAddr = host
		}

		timestamp := start.Format("02/Jan/2006:15:04:05 -0700")
		requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
		referer := r.Referer()
		if referer == "" {
			referer = "-"
		}
		userAgent := r.UserAgent()
		if userAgent == "" {
			userAgent = "-"
		}

		logLine := fmt.Sprintf(
			`%s - - [%s] "%s" %d %d "%s" "%s"`,
			remoteAddr,
			timestamp,
			requestLine,
			status,
			writer.bytes,
			referer,
			userAgent,
		)
		slog.Info(logLine)
	})
}

type uiHandler struct {
	fs fs.FS
}

func newUIHandler(uiContent fs.FS) http.Handler {
	return &uiHandler{fs: uiContent}
}

func (h *uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		http.NotFound(w, r)
		return
	}

	relativePath := strings.TrimPrefix(r.URL.Path, "/ui/")
	relativePath = strings.TrimPrefix(relativePath, "/")
	if relativePath == "" {
		h.serveIndex(w, r)
		return
	}

	if h.serveIfFile(w, r, relativePath) {
		return
	}

	h.serveIndex(w, r)
}

func (h *uiHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, "index.html")
}

func (h *uiHandler) serveIfFile(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := h.fs.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	h.serveFileWithInfo(w, r, name, file, info)
	return true
}

func (h *uiHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	file, err := h.fs.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	h.serveFileWithInfo(w, r, name, file, info)
}

func (h *uiHandler) serveFileWithInfo(w http.ResponseWriter, r *http.Request, name string, file fs.File, info fs.FileInfo) {
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
}

// tlsConfig returns the TLS configuration for the server.
func (s *Server) tlsConfig() (*tls.Config, error) {
	if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
		// Use provided certificate files
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
		}, nil
	}

	// Generate self-signed certificate
	cert, err := generateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	slog.Info("generated self-signed certificate")

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// generateSelfSignedCert generates a self-signed TLS certificate.
func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Holos Console"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
		Leaf: &x509.Certificate{
			Raw: certDER,
		},
	}, nil
}

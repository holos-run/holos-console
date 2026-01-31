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
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/holos-run/holos-console/console/oidc"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

//go:embed ui
var uiFS embed.FS

// Config holds the server configuration.
type Config struct {
	ListenAddr string
	CertFile   string
	KeyFile    string

	// PlainHTTP disables TLS, listening on plain HTTP instead.
	// Use when running behind a TLS-terminating ingress or gateway.
	PlainHTTP bool

	// Origin is the public-facing base URL of the console.
	// Used to construct OIDC redirect URIs (e.g., redirect_uri, post_logout_redirect_uri).
	// When empty, redirect URIs are derived from Issuer for backward compatibility.
	// Example: "https://holos-console.home.jeffmccune.com"
	Origin string

	// Issuer is the OIDC issuer URL for token validation.
	// This also determines the embedded Dex issuer URL.
	// Example: "https://localhost:8443/dex"
	Issuer string

	// ClientID is the expected audience for tokens.
	// Default: "holos-console"
	ClientID string

	// IDTokenTTL is the lifetime of ID tokens.
	// Default: 15 minutes
	IDTokenTTL time.Duration

	// RefreshTokenTTL is the absolute lifetime of refresh tokens.
	// After this duration, users must re-authenticate.
	// Default: 12 hours
	RefreshTokenTTL time.Duration

	// Namespace is the Kubernetes namespace for secrets.
	// Default: "holos-console"
	Namespace string

	// PlatformViewers are the OIDC groups with platform viewer role.
	// When nil, defaults to ["viewer"].
	PlatformViewers []string

	// PlatformEditors are the OIDC groups with platform editor role.
	// When nil, defaults to ["editor"].
	PlatformEditors []string

	// PlatformOwners are the OIDC groups with platform owner role.
	// When nil, defaults to ["owner"].
	PlatformOwners []string

	// CACertFile is the path to a PEM-encoded CA certificate file.
	// When set, this CA is added to the TLS root CAs used by the server's
	// internal HTTP client (e.g., for OIDC discovery). This allows the server
	// to trust certificates signed by a custom CA such as mkcert.
	CACertFile string
}

// OIDCConfig is the OIDC configuration injected into the frontend.
type OIDCConfig struct {
	Authority             string `json:"authority"`
	ClientID              string `json:"client_id"`
	RedirectURI           string `json:"redirect_uri"`
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
}

// deriveRedirectURI derives the OIDC redirect URI from the console origin.
func deriveRedirectURI(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/ui/callback"
}

// derivePostLogoutRedirectURI derives the post-logout redirect URI from the console origin.
func derivePostLogoutRedirectURI(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/ui"
}

// Server represents the console server.
type Server struct {
	cfg   Config
	ready atomic.Bool
}

// New creates a new Server with the given configuration.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Serve starts the HTTPS server and blocks until the context is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	// Load custom CA certificate pool for internal HTTP client (OIDC discovery, etc.)
	caPool, err := loadCACertPool(s.cfg.CACertFile)
	if err != nil {
		return fmt.Errorf("failed to load CA certificate: %w", err)
	}
	if caPool != nil {
		slog.Info("custom CA certificate loaded", "file", s.cfg.CACertFile)
	}
	internalClient := httpClientWithCA(caPool)

	mux := http.NewServeMux()

	// Health check endpoints for Kubernetes probes
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if s.ready.Load() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "not ready")
		}
	})

	// Configure ConnectRPC interceptors for public routes (no auth required)
	publicInterceptors := connect.WithInterceptors(
		rpc.MetricsInterceptor(),
		rpc.LoggingInterceptor(),
	)

	// Configure ConnectRPC interceptors for protected routes (auth required)
	// Note: The auth interceptor uses lazy verifier initialization since Dex
	// isn't running yet when we create the interceptor.
	var protectedInterceptors connect.Option
	if s.cfg.Issuer != "" && s.cfg.ClientID != "" {
		slog.Info("auth configured", "issuer", s.cfg.Issuer, "clientID", s.cfg.ClientID)
		protectedInterceptors = connect.WithInterceptors(
			rpc.MetricsInterceptor(),
			rpc.LoggingInterceptor(),
			rpc.LazyAuthInterceptor(s.cfg.Issuer, s.cfg.ClientID, internalClient),
		)
	} else {
		// Fallback to public interceptors if auth not configured
		protectedInterceptors = publicInterceptors
	}

	// Register VersionService
	versionHandler := rpc.NewVersionHandler(rpc.VersionInfo{
		Version:      GetVersion(),
		GitCommit:    GitCommit,
		GitTreeState: GitTreeState,
		BuildDate:    BuildDate,
	})
	path, handler := consolev1connect.NewVersionServiceHandler(versionHandler, publicInterceptors)
	mux.Handle(path, handler)

	// Initialize Kubernetes client for secrets (may be nil if no cluster available)
	k8sClientset, err := secrets.NewClientset()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	var secretsK8s *secrets.K8sClient
	if k8sClientset != nil {
		namespace := s.cfg.Namespace
		if namespace == "" {
			namespace = "holos-console"
		}
		secretsK8s = secrets.NewK8sClient(k8sClientset, namespace)
		slog.Info("kubernetes client initialized", "namespace", namespace)
	} else {
		slog.Info("no kubernetes config available, using dummy-secret only")
	}

	// Create RBAC group mapping from configuration
	groupMapping := rbac.NewGroupMapping(s.cfg.PlatformViewers, s.cfg.PlatformEditors, s.cfg.PlatformOwners)

	// Register SecretsService (protected - requires auth)
	secretsHandler := secrets.NewHandler(secretsK8s, groupMapping)
	secretsPath, secretsHTTPHandler := consolev1connect.NewSecretsServiceHandler(secretsHandler, protectedInterceptors)
	mux.Handle(secretsPath, secretsHTTPHandler)

	// Register gRPC reflection for introspection (grpcurl, etc.)
	reflector := grpcreflect.NewStaticReflector(
		consolev1connect.VersionServiceName,
		consolev1connect.SecretsServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	mux.Handle(reflectPath, reflectHandler)
	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
	mux.Handle(reflectAlphaPath, reflectAlphaHandler)

	// Initialize embedded OIDC identity provider (Dex)
	if s.cfg.Issuer != "" {
		// Derive redirect URIs from origin
		redirectURI := deriveRedirectURI(s.cfg.Origin)

		// Also allow Vite dev server redirect URI for local development
		redirectURIs := []string{redirectURI}
		viteRedirectURI := "https://localhost:5173/ui/callback"
		if redirectURI != viteRedirectURI {
			redirectURIs = append(redirectURIs, viteRedirectURI)
		}

		oidcHandler, err := oidc.NewHandler(ctx, oidc.Config{
			Issuer:          s.cfg.Issuer,
			ClientID:        s.cfg.ClientID,
			RedirectURIs:    redirectURIs,
			Logger:          slog.Default(),
			IDTokenTTL:      s.cfg.IDTokenTTL,
			RefreshTokenTTL: s.cfg.RefreshTokenTTL,
		})
		if err != nil {
			return fmt.Errorf("failed to create OIDC handler: %w", err)
		}

		// Mount Dex at /dex/ - Dex handles the full path internally since issuer includes /dex
		mux.Handle("/dex/", oidcHandler)

		slog.Info("embedded OIDC provider mounted", "path", "/dex/", "issuer", s.cfg.Issuer)
	}

	// Prepare embedded UI files
	uiContent, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return fmt.Errorf("failed to create sub filesystem: %w", err)
	}

	// Create OIDC config for frontend injection
	var oidcConfig *OIDCConfig
	if s.cfg.Issuer != "" {
		oidcConfig = &OIDCConfig{
			Authority:             s.cfg.Issuer,
			ClientID:              s.cfg.ClientID,
			RedirectURI:           deriveRedirectURI(s.cfg.Origin),
			PostLogoutRedirectURI: derivePostLogoutRedirectURI(s.cfg.Origin),
		}
	}

	uiHandler := newUIHandler(uiContent, oidcConfig)

	// Redirect / to /ui (canonical path without trailing slash)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Serve UI at /ui (canonical path without trailing slash)
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for the canonical /ui path
		uiHandler.ServeHTTP(w, r)
	})

	// Redirect /ui/ to /ui, serve SPA for deeper paths
	mux.HandleFunc("/ui/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ui/" {
			http.Redirect(w, r, "/ui", http.StatusMovedPermanently)
			return
		}
		// Handle SPA routes under /ui/
		uiHandler.ServeHTTP(w, r)
	})

	// Expose user info from oauth2-proxy forwarded headers (BFF mode)
	mux.HandleFunc("/api/userinfo", handleUserInfo)

	// Debug endpoint for OIDC investigation (dev mode only)
	if s.cfg.Issuer != "" {
		issuer := s.cfg.Issuer
		mux.HandleFunc("/api/debug/oidc", func(w http.ResponseWriter, r *http.Request) {
			handleDebugOIDC(w, r, issuer, internalClient)
		})
	}

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

	// Configure TLS (skipped for plain HTTP)
	if !s.cfg.PlainHTTP {
		tlsConfig, err := s.tlsConfig()
		if err != nil {
			return fmt.Errorf("failed to configure TLS: %w", err)
		}
		server.TLSConfig = tlsConfig
	}

	// Mark server as ready before starting the listener
	s.ready.Store(true)

	// Start server
	scheme := "https"
	if s.cfg.PlainHTTP {
		scheme = "http"
	}
	slog.Info("starting server", "addr", s.cfg.ListenAddr, "scheme", scheme)

	errCh := make(chan error, 1)
	go func() {
		if s.cfg.PlainHTTP {
			errCh <- server.ListenAndServe()
		} else if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
			errCh <- server.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile)
		} else {
			// Use auto-generated certificate
			listener, err := tls.Listen("tcp", s.cfg.ListenAddr, server.TLSConfig)
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
	fs         fs.FS
	oidcConfig *OIDCConfig
}

func newUIHandler(uiContent fs.FS, oidcConfig *OIDCConfig) http.Handler {
	return &uiHandler{fs: uiContent, oidcConfig: oidcConfig}
}

func (h *uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle both /ui (canonical) and /ui/* paths
	if r.URL.Path == "/ui" {
		h.serveIndex(w, r)
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		http.NotFound(w, r)
		return
	}

	relativePath := strings.TrimPrefix(r.URL.Path, "/ui/")
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
	// Read index.html
	data, err := fs.ReadFile(h.fs, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Inject OIDC config if available
	if h.oidcConfig != nil {
		configJSON, err := json.Marshal(h.oidcConfig)
		if err == nil {
			script := fmt.Sprintf(`<script>window.__OIDC_CONFIG__=%s;</script>`, configJSON)
			// Insert before </head>
			data = bytes.Replace(data, []byte("</head>"), []byte(script+"</head>"), 1)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
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

// handleUserInfo returns user information from oauth2-proxy forwarded headers.
// This endpoint is used by the frontend in BFF mode to get the current user.
func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-Forwarded-User")
	email := r.Header.Get("X-Forwarded-Email")

	if user == "" && email == "" {
		// Not authenticated or not running behind oauth2-proxy
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"user":  user,
		"email": email,
	})
}

// handleDebugOIDC returns debug information about OIDC configuration.
// Useful for troubleshooting OIDC issues like missing groups claims.
func handleDebugOIDC(w http.ResponseWriter, r *http.Request, issuer string, client *http.Client) {

	// Fetch the OIDC discovery document
	discoveryURL := issuer + "/.well-known/openid-configuration"
	resp, err := client.Get(discoveryURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch discovery document: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var discovery map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse discovery document: %v", err), http.StatusInternalServerError)
		return
	}

	// Add debug information
	debugInfo := map[string]interface{}{
		"discovery":        discovery,
		"configured_issuer": issuer,
		"notes": map[string]string{
			"scopes_supported": "Check if 'groups' is in scopes_supported. If not, Dex may not include groups in ID tokens.",
			"investigation":    "See holos-garage/Holos Garage/Holos/plans/holos-console-groups-claim-investigation.md",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(debugInfo)
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

// loadCACertPool loads a PEM-encoded CA certificate file and returns a cert
// pool containing both the system roots and the custom CA. If caCertFile is
// empty, nil is returned (causing http.Transport to use system roots only).
func loadCACertPool(caCertFile string) (*x509.CertPool, error) {
	if caCertFile == "" {
		return nil, nil
	}
	pemData, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("no valid certificates found in %s", caCertFile)
	}
	return pool, nil
}

// httpClientWithCA returns an *http.Client whose TLS config trusts the given
// CA pool. If pool is nil the returned client uses the default system roots.
func httpClientWithCA(pool *x509.CertPool) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}
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

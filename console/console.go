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

	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/folders"
	"github.com/holos-run/holos-console/console/oidc"
	"github.com/holos-run/holos-console/console/organizations"
	"github.com/holos-run/holos-console/console/projects"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/settings"
	"github.com/holos-run/holos-console/console/templates"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

//go:embed all:dist
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
	// Default: 1 hour
	IDTokenTTL time.Duration

	// RefreshTokenTTL is the absolute lifetime of refresh tokens.
	// After this duration, users must re-authenticate.
	// Default: 12 hours
	RefreshTokenTTL time.Duration

	// CACertFile is the path to a PEM-encoded CA certificate file.
	// When set, this CA is added to the TLS root CAs used by the server's
	// internal HTTP client (e.g., for OIDC discovery). This allows the server
	// to trust certificates signed by a custom CA such as mkcert.
	CACertFile string

	// NamespacePrefix is a global prefix prepended to all namespace names,
	// enabling multiple console instances (e.g., ci, qa, prod) in the same
	// Kubernetes cluster. Default: "" (empty, no global prefix).
	NamespacePrefix string

	// OrganizationPrefix is prepended to organization namespace names.
	// Default: "org-"
	OrganizationPrefix string

	// FolderPrefix is prepended to folder namespace names.
	// Default: "fld-"
	FolderPrefix string

	// ProjectPrefix is prepended to project namespace names.
	// Default: "prj-"
	ProjectPrefix string

	// DisableOrgCreation disables the implicit organization creation grant to all
	// authenticated principals. Explicit OrgCreatorUsers and OrgCreatorRoles are
	// still honored when this is true.
	DisableOrgCreation bool

	// OrgCreatorUsers is a list of email addresses allowed to create organizations.
	OrgCreatorUsers []string

	// OrgCreatorRoles is a list of OIDC role names allowed to create organizations.
	OrgCreatorRoles []string

	// RolesClaim is the OIDC ID token claim name for role memberships.
	// Default: "groups"
	RolesClaim string

	// EnableInsecureDex starts the built-in Dex OIDC provider with an
	// auto-login connector that authenticates users without credentials.
	// INSECURE: intended for local development only.
	EnableInsecureDex bool

	// LogHealthChecks enables logging of /healthz and /readyz requests.
	// Default: false (suppresses health check logging to reduce noise from Kubernetes probes).
	LogHealthChecks bool

	// EnableDevTools enables development tools in the web UI
	// (persona switcher, dev token panel).
	// Default: false (disabled).
	EnableDevTools bool
}

// OIDCConfig is the OIDC configuration injected into the frontend.
type OIDCConfig struct {
	Authority             string `json:"authority"`
	ClientID              string `json:"client_id"`
	RedirectURI           string `json:"redirect_uri"`
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
}

// ConsoleConfig is the console configuration injected into the frontend
// via window.__CONSOLE_CONFIG__.
type ConsoleConfig struct {
	DevToolsEnabled bool `json:"devToolsEnabled"`
}

// deriveRedirectURI derives the OIDC redirect URI from the console origin.
func deriveRedirectURI(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/pkce/verify"
}

// derivePostLogoutRedirectURI derives the post-logout redirect URI from the console origin.
func derivePostLogoutRedirectURI(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/"
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
	// Apply defaults for namespace prefixes
	if s.cfg.OrganizationPrefix == "" {
		s.cfg.OrganizationPrefix = "org-"
	}
	if s.cfg.FolderPrefix == "" {
		s.cfg.FolderPrefix = "fld-"
	}
	if s.cfg.ProjectPrefix == "" {
		s.cfg.ProjectPrefix = "prj-"
	}

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
			rpc.LazyAuthInterceptor(s.cfg.Issuer, s.cfg.ClientID, s.cfg.RolesClaim, internalClient),
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

	// Register services (protected - requires auth)
	if k8sClientset != nil {
		nsResolver := &resolver.Resolver{NamespacePrefix: s.cfg.NamespacePrefix, OrganizationPrefix: s.cfg.OrganizationPrefix, FolderPrefix: s.cfg.FolderPrefix, ProjectPrefix: s.cfg.ProjectPrefix}
		slog.Info("kubernetes client initialized")

		// Folder K8s client created first so the org handler can auto-create default folders.
		foldersK8s := folders.NewK8sClient(k8sClientset, nsResolver)

		// Organization service (projectsK8s created first for linked-project precondition check)
		orgsK8s := organizations.NewK8sClient(k8sClientset, nsResolver)
		orgGrantResolver := organizations.NewOrgGrantResolver(orgsK8s)
		projectsK8s := projects.NewK8sClient(k8sClientset, nsResolver)
		folderPrefix := nsResolver.NamespacePrefix + nsResolver.FolderPrefix
		orgsHandler := organizations.NewHandler(orgsK8s, projectsK8s, s.cfg.DisableOrgCreation, s.cfg.OrgCreatorUsers, s.cfg.OrgCreatorRoles).
			WithFolderCreator(foldersK8s, foldersK8s, folderPrefix)
		orgsPath, orgsHTTPHandler := consolev1connect.NewOrganizationServiceHandler(orgsHandler, protectedInterceptors)
		mux.Handle(orgsPath, orgsHTTPHandler)

		// Folder service
		foldersHandler := folders.NewHandler(foldersK8s)
		foldersPath, foldersHTTPHandler := consolev1connect.NewFolderServiceHandler(foldersHandler, protectedInterceptors)
		mux.Handle(foldersPath, foldersHTTPHandler)

		// Create dynamic client early so it can be shared by both the deployment
		// service and the mandatory template applier.
		dynamicClient, err := deployments.NewDynamicClient()
		if err != nil {
			return fmt.Errorf("failed to create dynamic kubernetes client: %w", err)
		}

		// Namespace hierarchy walker for ancestor chain resolution.
		// Used by the mandatory template applier, project grant resolver, and
		// the unified TemplateService handler.
		nsWalker := &resolver.Walker{Client: k8sClientset, Resolver: nsResolver}

		// Unified templates K8s client (replaces both templates.K8sClient and
		// org_templates.K8sClient from v1alpha1 — ADR 021 Decision 1).
		templatesK8s := templates.NewK8sClient(k8sClientset, nsResolver)

		// Wire defaults seeder for populate_defaults org creation flow.
		projectPrefix := nsResolver.NamespacePrefix + nsResolver.ProjectPrefix
		orgsHandler.WithDefaultsSeeder(templatesK8s, &projects.ProjectCreatorAdapter{K8s: projectsK8s}, projectPrefix)

		// Mandatory template applier for project creation: walks the project's
		// ancestor chain (org + folder ancestors) and applies all mandatory+enabled
		// templates to the project namespace (ADR 021 Decision 3).
		var orgTmplApplier projects.MandatoryTemplateApplier
		if dynamicClient != nil {
			orgTmplApplier = templates.NewMandatoryTemplateApplier(templatesK8s, nsWalker, &deployments.CueRenderer{}, deployments.NewApplier(dynamicClient))
		}

		// Project service with org grant fallback
		projectsHandler := projects.NewHandler(projectsK8s, orgGrantResolver)
		if orgTmplApplier != nil {
			projectsHandler = projectsHandler.WithMandatoryTemplateApplier(orgTmplApplier)
		}
		projectsPath, projectsHTTPHandler := consolev1connect.NewProjectServiceHandler(projectsHandler, protectedInterceptors)
		mux.Handle(projectsPath, projectsHTTPHandler)

		// Secrets service with project grant fallback and ancestor default-share cascade.
		secretsK8s := secrets.NewK8sClient(k8sClientset, nsResolver)
		projectResolver := projects.NewProjectGrantResolver(projectsK8s).WithWalker(nsWalker)
		secretsHandler := secrets.NewProjectScopedHandler(secretsK8s, projectResolver)
		secretsPath, secretsHTTPHandler := consolev1connect.NewSecretsServiceHandler(secretsHandler, protectedInterceptors)
		mux.Handle(secretsPath, secretsHTTPHandler)

		// Project settings service with org-level RBAC for deployments toggle
		settingsK8s := settings.NewK8sClient(k8sClientset, nsResolver)
		settingsHandler := settings.NewHandler(settingsK8s, projectResolver, orgGrantResolver, projectResolver)
		settingsPath, settingsHTTPHandler := consolev1connect.NewProjectSettingsServiceHandler(settingsHandler, protectedInterceptors)
		mux.Handle(settingsPath, settingsHTTPHandler)

		// Unified TemplateService handler — manages templates at org, folder, and
		// project scopes in a single service (ADR 021).
		folderGrantResolver := folders.NewFolderGrantResolver(foldersK8s)
		templatesHandler := templates.NewHandler(templatesK8s, nsResolver, templates.NewCueRendererAdapter()).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver).
			WithProjectGrantResolver(projectResolver).
			WithAncestorWalker(nsWalker)
		templatesPath, templatesHTTPHandler := consolev1connect.NewTemplateServiceHandler(templatesHandler, protectedInterceptors)
		mux.Handle(templatesPath, templatesHTTPHandler)

		// Deployment service with project grant fallback.
		// ancestorTemplateResolver wraps templatesK8s + nsWalker to satisfy
		// AncestorTemplateProvider for full ancestor-chain template resolution
		// (org + folders) at render time (ADR 019, issue #874).
		deploymentsK8s := deployments.NewK8sClient(k8sClientset, nsResolver)
		var deploymentsApplier deployments.ResourceApplier
		if dynamicClient != nil {
			deploymentsApplier = deployments.NewApplier(dynamicClient)
		}
		projectFolderResolver := projects.NewProjectFolderResolver(projectsK8s, nsWalker)
		ancestorTemplateResolver := templates.NewAncestorTemplateResolver(templatesK8s, nsWalker)
		deploymentsHandler := deployments.NewHandler(deploymentsK8s, projectResolver, settingsK8s, templates.NewProjectScopedResolver(templatesK8s), &deployments.CueRenderer{}, deploymentsApplier).
			WithAncestorWalker(projectFolderResolver).
			WithAncestorTemplateProvider(ancestorTemplateResolver)
		deploymentsPath, deploymentsHTTPHandler := consolev1connect.NewDeploymentServiceHandler(deploymentsHandler, protectedInterceptors)
		mux.Handle(deploymentsPath, deploymentsHTTPHandler)
	} else {
		slog.Info("no kubernetes config available, using dummy-secret only")
		// Fallback: secrets handler without K8s (no resolvers)
		secretsHandler := secrets.NewProjectScopedHandler(nil, nil)
		secretsPath, secretsHTTPHandler := consolev1connect.NewSecretsServiceHandler(secretsHandler, protectedInterceptors)
		mux.Handle(secretsPath, secretsHTTPHandler)
	}

	// Register gRPC reflection for introspection (grpcurl, etc.).
	// These endpoints are intentionally unauthenticated. The API surface they
	// expose (service names, method signatures, message schemas) is public
	// information available in the proto/ source files and UI bundle.
	// See ADR 009 (docs/adrs/009-grpc-reflection-unauthenticated.md).
	reflector := grpcreflect.NewStaticReflector(
		consolev1connect.VersionServiceName,
		consolev1connect.SecretsServiceName,
		consolev1connect.ProjectServiceName,
		consolev1connect.OrganizationServiceName,
		consolev1connect.ProjectSettingsServiceName,
		consolev1connect.TemplateServiceName,
		consolev1connect.FolderServiceName,
		consolev1connect.DeploymentServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	mux.Handle(reflectPath, reflectHandler)
	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
	mux.Handle(reflectAlphaPath, reflectAlphaHandler)

	// Initialize embedded OIDC identity provider (Dex).
	// Only started when explicitly enabled via --enable-insecure-dex.
	if s.cfg.EnableInsecureDex && s.cfg.Issuer != "" {
		// Derive redirect URIs from origin
		redirectURI := deriveRedirectURI(s.cfg.Origin)

		// Also allow Vite dev server redirect URI for local development
		redirectURIs := []string{redirectURI}
		viteRedirectURI := "https://localhost:5173/pkce/verify"
		if redirectURI != viteRedirectURI {
			redirectURIs = append(redirectURIs, viteRedirectURI)
		}

		oidcHandler, dexState, err := oidc.NewHandler(ctx, oidc.Config{
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

		// Mount dev token-exchange endpoint for programmatic persona tokens.
		// This endpoint mints real OIDC ID tokens signed by Dex's keys for any
		// registered test user, enabling API testing without a browser flow.
		mux.HandleFunc("/api/dev/token", oidc.HandleTokenExchange(dexState))
		slog.Info("dev token-exchange endpoint mounted", "path", "/api/dev/token")

		// Debug endpoint for OIDC investigation (insecure Dex mode only)
		issuer := s.cfg.Issuer
		mux.HandleFunc("/api/debug/oidc", func(w http.ResponseWriter, r *http.Request) {
			handleDebugOIDC(w, r, issuer, internalClient)
		})

		slog.Info("embedded OIDC provider mounted", "path", "/dex/", "issuer", s.cfg.Issuer)
	} else {
		// When Dex is disabled, register fallback handlers for dev-only API
		// endpoints so they return a proper 404 JSON error instead of falling
		// through to the SPA catch-all (which would serve index.html as HTML 200).
		// See https://github.com/holos-run/holos-console/issues/716.
		mux.HandleFunc("/api/dev/token", apiNotAvailable("/api/dev/token", "Dex"))
		mux.HandleFunc("/api/debug/oidc", apiNotAvailable("/api/debug/oidc", "Dex"))
	}

	// Prepare embedded UI files
	uiContent, err := fs.Sub(uiFS, "dist")
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

	// Create console config for frontend injection
	var consoleConfig *ConsoleConfig
	if s.cfg.EnableDevTools {
		consoleConfig = &ConsoleConfig{DevToolsEnabled: true}
	}

	uiHandler := newUIHandler(uiContent, oidcConfig, consoleConfig)

	// Redirect /ui to / for backwards compatibility
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/ui/", func(w http.ResponseWriter, r *http.Request) {
		target := strings.TrimPrefix(r.URL.Path, "/ui")
		if target == "" {
			target = "/"
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})

	// Serve SPA at / (catch-all for frontend routes and static assets).
	// More specific patterns (/dex/, /healthz, ConnectRPC services) are
	// registered first and take priority in the Go default mux.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		uiHandler.ServeHTTP(w, r)
	})

	// Expose Prometheus metrics at /metrics
	mux.Handle("/metrics", promhttp.Handler())

	// Wrap with h2c for HTTP/2 cleartext support (needed for gRPC over HTTP/2)
	h2cHandler := h2c.NewHandler(mux, &http2.Server{})
	loggedHandler := logRequests(h2cHandler, s.cfg.LogHealthChecks)

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
	slog.Info("ready", "version", GetVersion(), "url", s.cfg.Origin)

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

func logRequests(next http.Handler, logHealthChecks bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		writer := &loggingResponseWriter{ResponseWriter: w}

		next.ServeHTTP(writer, r)

		// Skip logging health check endpoints unless explicitly enabled.
		if !logHealthChecks && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
			return
		}

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

// apiNotAvailable returns an http.HandlerFunc that responds with 404 when a
// dev-only API endpoint is hit but its backing service (e.g. Dex) is not
// enabled. This prevents the request from falling through to the SPA catch-all
// which would incorrectly return index.html as HTML 200.
func apiNotAvailable(endpoint, service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, fmt.Sprintf("%s not available (%s not enabled)", endpoint, service), http.StatusNotFound)
	}
}

type uiHandler struct {
	fs            fs.FS
	oidcConfig    *OIDCConfig
	consoleConfig *ConsoleConfig
}

func newUIHandler(uiContent fs.FS, oidcConfig *OIDCConfig, consoleConfig *ConsoleConfig) *uiHandler {
	return &uiHandler{fs: uiContent, oidcConfig: oidcConfig, consoleConfig: consoleConfig}
}

func (h *uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Serve index.html for root
	if r.URL.Path == "/" {
		h.serveIndex(w, r)
		return
	}

	// Try to serve as a static file (strip leading /)
	relativePath := strings.TrimPrefix(r.URL.Path, "/")
	if relativePath != "" && h.serveIfFile(w, r, relativePath) {
		return
	}

	// Fall back to index.html for SPA client-side routing
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

	// Inject console config if available
	if h.consoleConfig != nil {
		configJSON, err := json.Marshal(h.consoleConfig)
		if err == nil {
			script := fmt.Sprintf(`<script>window.__CONSOLE_CONFIG__=%s;</script>`, configJSON)
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
		"discovery":         discovery,
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

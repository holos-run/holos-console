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
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/deployments/statuscache"
	"github.com/holos-run/holos-console/console/folders"
	"github.com/holos-run/holos-console/console/oidc"
	"github.com/holos-run/holos-console/console/organizations"
	"github.com/holos-run/holos-console/console/permissions"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/projects"
	"github.com/holos-run/holos-console/console/projects/projectapply"
	"github.com/holos-run/holos-console/console/projects/projectnspipeline"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	"github.com/holos-run/holos-console/console/settings"
	"github.com/holos-run/holos-console/console/templatedependencies"
	"github.com/holos-run/holos-console/console/templategrants"
	"github.com/holos-run/holos-console/console/templatepolicies"
	"github.com/holos-run/holos-console/console/templatepolicybindings"
	"github.com/holos-run/holos-console/console/templaterequirements"
	"github.com/holos-run/holos-console/console/templates"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
	controllermgr "github.com/holos-run/holos-console/internal/controller"
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
//
// The four prefix fields mirror the Config fields of the same name so the
// frontend can translate between logical resource names and Kubernetes
// namespace names using the same layout the backend resolver uses (see
// `console/resolver`). Per HOL-722 these are exposed unconditionally so
// deployments that override the default prefixes see the UI match the server.
type ConsoleConfig struct {
	DevToolsEnabled    bool   `json:"devToolsEnabled"`
	NamespacePrefix    string `json:"namespacePrefix"`
	OrganizationPrefix string `json:"organizationPrefix"`
	FolderPrefix       string `json:"folderPrefix"`
	ProjectPrefix      string `json:"projectPrefix"`
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
	// controllerMgr is the embedded controller-runtime manager (HOL-620).
	// The /readyz probe ANDs s.ready with controllerMgr.Ready() so the pod
	// stays 503 until the listener is up AND every informer cache has
	// completed its initial sync. Nil when Serve runs without a Kubernetes
	// config (dummy-secret-only mode).
	controllerMgr *controllermgr.Manager
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
		// HOL-620: /readyz only flips to 200 when the listener has
		// finished wiring up AND the controller-runtime manager's
		// informer cache has completed its initial sync. The cache
		// check short-circuits to `true` when the manager is nil —
		// that case is dummy-secret-only mode, which has no cluster
		// and therefore nothing to sync.
		cacheReady := true
		if s.controllerMgr != nil {
			cacheReady = s.controllerMgr.Ready()
		}
		if s.ready.Load() && cacheReady {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "not ready")
	})

	// Configure ConnectRPC interceptors for public routes (no auth required)
	publicInterceptors := connect.WithInterceptors(
		rpc.MetricsInterceptor(),
		rpc.LoggingInterceptor(),
	)

	// Resolve the base Kubernetes REST config once. Startup-scoped clients keep
	// using service-account credentials in this phase, while the auth
	// interceptor copies this config per request and fills rest.Config.Impersonate
	// from the authenticated OIDC claims.
	restConfig, err := secrets.NewRestConfig()
	if err != nil {
		return fmt.Errorf("failed to resolve kubernetes REST config: %w", err)
	}

	// Configure ConnectRPC interceptors for protected routes (auth required)
	// Note: The auth interceptor uses lazy verifier initialization since Dex
	// isn't running yet when we create the interceptor.
	var protectedInterceptors connect.Option
	if s.cfg.Issuer != "" && s.cfg.ClientID != "" {
		slog.Info("auth configured", "issuer", s.cfg.Issuer, "clientID", s.cfg.ClientID)
		protectedInterceptors = connect.WithInterceptors(
			rpc.MetricsInterceptor(),
			rpc.LoggingInterceptor(),
			rpc.LazyAuthInterceptor(
				s.cfg.Issuer,
				s.cfg.ClientID,
				s.cfg.RolesClaim,
				internalClient,
			),
			rpc.ImpersonationInterceptor(restConfig, controllermgr.Scheme),
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

	// Initialize Kubernetes client for secrets (may be nil if no cluster available).
	// We share the resolved REST config with the controller-runtime manager
	// below so there is a single loader for the cluster connection.
	k8sClientset, err := secrets.NewClientsetForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// HOL-620: embed the controller-runtime manager when a cluster config
	// is available. The manager owns the informer caches HOL-621 rewires
	// every storage client to read from; for now it lands the three
	// reconcilers (Template, TemplatePolicy, TemplatePolicyBinding) that
	// publish the Gateway-API-style status surface defined in ADR 030. We
	// gate the console /readyz probe on mgr.Ready() so the pod stays 503
	// until the cache is warm.
	if k8sClientset != nil && restConfig != nil {
		mgr, err := controllermgr.NewManager(restConfig, nil, controllermgr.Options{
			// controller-runtime enforces a process-global uniqueness
			// check on controller names to prevent Prometheus metric
			// collisions. The console metrics server is separate
			// (mux.Handle("/metrics") below) and the controller-runtime
			// metrics listener is disabled, so collisions are not a
			// concern. Skipping the guard lets `console.Server.Serve`
			// be invoked multiple times in the same test process
			// (testscript creates a fresh server per script), which
			// would otherwise trip the name-registry.
			SkipControllerNameValidation: true,
			// Mirror the namespace-prefix configuration the resolver
			// already reads from flags so the TemplatePolicyBinding
			// reconciler computes the same project namespace names
			// the console's RPC handlers use.
			NamespacePrefix:    s.cfg.NamespacePrefix,
			OrganizationPrefix: s.cfg.OrganizationPrefix,
			FolderPrefix:       s.cfg.FolderPrefix,
			ProjectPrefix:      s.cfg.ProjectPrefix,
		})
		if err != nil {
			return fmt.Errorf("failed to build controller manager: %w", err)
		}
		s.controllerMgr = mgr
		slog.Info("controller-runtime manager initialized")
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
		foldersAdapter := &folders.FolderCreatorAdapter{K8s: foldersK8s}
		orgsHandler := organizations.NewHandler(orgsK8s, projectsK8s, s.cfg.DisableOrgCreation, s.cfg.OrgCreatorUsers, s.cfg.OrgCreatorRoles).
			WithFolderCreator(foldersAdapter, foldersK8s, folderPrefix)
		orgsPath, orgsHTTPHandler := consolev1connect.NewOrganizationServiceHandler(orgsHandler, protectedInterceptors)
		mux.Handle(orgsPath, orgsHTTPHandler)

		// Folder service
		foldersHandler := folders.NewHandler(foldersK8s)
		foldersPath, foldersHTTPHandler := consolev1connect.NewFolderServiceHandler(foldersHandler, protectedInterceptors)
		mux.Handle(foldersPath, foldersHTTPHandler)

		// Dynamic client used by the deployment service's applier for
		// Server-Side Apply onto project namespaces.
		dynamicClient, err := deployments.NewDynamicClient()
		if err != nil {
			return fmt.Errorf("failed to create dynamic kubernetes client: %w", err)
		}

		// Namespace hierarchy walker for ancestor chain resolution. Used by
		// the project grant resolver, the unified TemplateService handler,
		// and the TemplatePolicy REQUIRE-rule folder resolver.
		//
		// HOL-622 routes WalkAncestors through the controller-runtime
		// cache-backed client when the embedded Manager is wired (the
		// default production path). The informer cache populated by the
		// Manager serves every per-hop Namespace Get without an apiserver
		// round-trip — the AC "render-time list latency becomes O(cache
		// lookup)" extends to the namespace lookups the walker performs.
		// The Client field is retained as a fallback for test wiring and
		// for any deployment that turns the Manager off.
		var nsGetter resolver.NamespaceGetter
		if s.controllerMgr != nil {
			nsGetter = &resolver.CtrlRuntimeNamespaceGetter{Client: s.controllerMgr.GetClient()}
		}
		nsWalker := &resolver.Walker{Getter: nsGetter, Client: k8sClientset, Resolver: nsResolver}

		// Unified templates K8s client (replaces both templates.K8sClient and
		// org_templates.K8sClient from v1alpha1 — ADR 021 Decision 1).
		//
		// HOL-621 / HOL-661: Template CRUD routes through the embedded
		// controller-runtime manager's cache-backed client.Client — reads
		// observe the shared informer cache, writes fall through to the
		// apiserver. HOL-693 (ADR 032) migrated Release CRUD to the same
		// TemplateRelease CRD path, so the client-go kubernetes.Interface
		// parameter dropped off the constructor.
		var templateCtrlClient ctrlclient.Client
		if s.controllerMgr != nil {
			templateCtrlClient = s.controllerMgr.GetClient()
		}
		templatesK8s := templates.NewK8sClient(templateCtrlClient, nsResolver)

		// TemplatePolicy resolution seam (HOL-566 Phase 4, wired in HOL-567
		// Phase 5). The real folderResolver is threaded through every render
		// path — deployments and project-scope templates — so Phase 5
		// (HOL-567) swapped the Phase 4 no-op for the TemplatePolicy-backed
		// implementation. HOL-582 removed the creation-time
		// required-template application path; render-time is now the sole
		// enforcement site for REQUIRE rules. TemplatePolicy reads route
		// through a namespace-direct lister that never consults a project
		// namespace (HOL-554 storage-isolation).
		templatePoliciesK8s := templatepolicies.NewK8sClient(templateCtrlClient, nsResolver)
		templatePolicyBindingsK8s := templatepolicybindings.NewK8sClient(templateCtrlClient, nsResolver)
		// HOL-622: wire the shared informer cache so the resolver hot path
		// (ListPoliciesInNamespace / ListBindingsInNamespace) pulls pointers
		// to cache-owned CRD objects via the indexer's NamespaceIndex — no
		// DeepCopy, no value-slice re-wrap. The acceptance criterion "no
		// defensive copy on the hot path — resolver forwards cached pointers"
		// is met by this wiring plus the indexer-backed path inside each
		// K8sClient. Tests that exercise the CRUD surface without the full
		// manager leave the cache nil and continue to use the delegating
		// client path.
		if s.controllerMgr != nil {
			cache := s.controllerMgr.GetManager().GetCache()
			templatePoliciesK8s = templatePoliciesK8s.WithCache(cache)
			templatePolicyBindingsK8s = templatePolicyBindingsK8s.WithCache(cache)
		}
		// HOL-596 wires the TemplatePolicyBinding evaluation path into the
		// render-time resolver. Bindings take precedence on conflict: a
		// binding whose target_refs match the current render target
		// dereferences its policy_ref and injects (REQUIRE) / removes
		// (EXCLUDE) the bound policy's template refs. HOL-662 removed
		// the JSON-annotation unmarshaler seams — the CRD spec carries
		// rules and bindings as structured fields, so the resolver reads
		// them directly.
		policyResolverSeam := policyresolver.NewFolderResolverWithBindings(
			templatePoliciesK8s,
			nsWalker,
			nsResolver,
			templatePolicyBindingsK8s,
		)
		// AppliedRenderStateClient persists the effective render set to the
		// owning folder namespace on successful Create/Update of a deployment
		// or project-scope template. Reads consult ONLY folder/organization
		// namespaces — any render-state artifact in a project namespace is
		// ignored (HOL-554 storage-isolation guardrail).
		// HOL-694 migrated this client off ConfigMap storage onto a dedicated
		// RenderState CRD (templates.holos.run/v1alpha1). The embedded Manager
		// primes the RenderState informer eagerly so drift checks read from the
		// shared cache alongside policy and binding reads. If the Manager is
		// disabled we fall back to a nil client; the render-state client
		// returns nil on a nil client so drift silently no-ops until a Manager
		// is re-enabled.
		var renderStateCtrlClient ctrlclient.Client
		if s.controllerMgr != nil {
			renderStateCtrlClient = s.controllerMgr.GetClient()
		}
		appliedRenderStateClient := policyresolver.NewAppliedRenderStateClient(renderStateCtrlClient, nsResolver, nsWalker)
		// driftChecker composes the real TemplatePolicy resolver with the
		// applied-render-state store so the deployments and templates
		// handlers can surface drift (DeploymentStatusSummary.policy_drift,
		// GetDeploymentPolicyState, GetProjectTemplatePolicyState) and
		// record the effective render set on successful Create/Update of a
		// render target.
		driftChecker := policyresolver.NewDriftChecker(policyResolverSeam, appliedRenderStateClient, nsResolver)
		deploymentDriftAdapter := policyresolver.NewDeploymentDriftAdapter(driftChecker)
		projectTemplateDriftAdapter := policyresolver.NewProjectTemplateDriftAdapter(driftChecker)

		// Wire defaults seeder for populate_defaults org creation flow.
		projectPrefix := nsResolver.NamespacePrefix + nsResolver.ProjectPrefix
		orgsHandler.WithDefaultsSeeder(templatesK8s, &projects.ProjectCreatorAdapter{K8s: projectsK8s}, projectPrefix)

		// Project service with org grant fallback. HOL-582 removed the
		// creation-time RequiredTemplateApplier (Layer B in the HOL-580
		// analysis); REQUIRE rules are now enforced exclusively at render
		// time via folderResolver (Layer A).
		projectsHandler := projects.NewHandler(projectsK8s, orgGrantResolver)

		// HOL-812: wire the ProjectNamespace pipeline
		// (resolve → render → apply) into CreateProject. The pipeline is
		// only active when every dependency is present — the dynamic
		// client is required for SSA, and the controller-runtime cache
		// powers the binding walker on the hot path. When any dependency
		// is missing (bootstrap, tests, dev), WithProjectNamespacePipeline
		// receives nil and the handler keeps using its existing
		// Namespace-create path unchanged.
		if dynamicClient != nil {
			ancestorBindings := policyresolver.NewAncestorBindingLister(templatePolicyBindingsK8s, nsWalker, nsResolver)
			projectNSResolver := policyresolver.NewProjectNamespaceResolver(ancestorBindings)
			pipeline := projectnspipeline.New(
				projectNSResolver,
				projectnspipeline.NewPolicyGetterAdapter(templatePoliciesK8s),
				projectnspipeline.NewTemplateGetterAdapter(templatesK8s),
				templates.NewCueRendererAdapter(),
				projectapply.NewApplier(dynamicClient, projectapply.NewDefaultGVRResolver()),
			)
			projectsHandler = projectsHandler.WithProjectNamespacePipeline(&projectNSPipelineAdapter{p: pipeline})
		}

		projectsPath, projectsHTTPHandler := consolev1connect.NewProjectServiceHandler(projectsHandler, protectedInterceptors)
		mux.Handle(projectsPath, projectsHTTPHandler)

		// PermissionsService — bulk SelfSubjectAccessReview fan-out for the
		// frontend's UI gating contract (ADR 036). Handler is stateless;
		// every call resolves its impersonating client from the request
		// context.
		permissionsHandler := permissions.NewHandler()
		permissionsPath, permissionsHTTPHandler := consolev1connect.NewPermissionsServiceHandler(permissionsHandler, protectedInterceptors)
		mux.Handle(permissionsPath, permissionsHTTPHandler)

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

		// HOL-644 / HOL-828: shared gateway-namespace resolver used by both the
		// deployments handler (project→org→annotation) and the template-preview
		// handler (org/folder→annotation). Constructed here so both handlers
		// share the same instance without duplicating the resolution logic.
		// orgsK8s and projectResolver are already available at this point.
		gatewayResolver := organizations.NewGatewayNamespaceResolver(orgsK8s, projectResolver)

		// Unified TemplateService handler — manages templates at org, folder, and
		// project scopes in a single service (ADR 021).
		folderGrantResolver := folders.NewFolderGrantResolver(foldersK8s)
		templatesHandler := templates.NewHandler(templatesK8s, nsResolver, templates.NewCueRendererAdapter(), policyResolverSeam).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver).
			WithProjectGrantResolver(projectResolver).
			WithAncestorWalker(nsWalker).
			WithProjectTemplateDriftChecker(projectTemplateDriftAdapter).
			WithOrganizationGatewayResolver(gatewayResolver)
		templatesPath, templatesHTTPHandler := consolev1connect.NewTemplateServiceHandler(templatesHandler, protectedInterceptors)
		mux.Handle(templatesPath, templatesHTTPHandler)

		// TemplateDependencyService handler manages project-namespaced
		// TemplateDependency CRDs used by ADR 032 dependency materialisation.
		// These share the Template permission family because dependency edges
		// are part of the project template authoring surface.
		templateDependenciesK8s := templatedependencies.NewK8sClient(templateCtrlClient)
		templateDependenciesHandler := templatedependencies.NewHandler(templateDependenciesK8s, nsResolver).
			WithProjectGrantResolver(projectResolver)
		templateDependenciesPath, templateDependenciesHTTPHandler := consolev1connect.NewTemplateDependencyServiceHandler(templateDependenciesHandler, protectedInterceptors)
		mux.Handle(templateDependenciesPath, templateDependenciesHTTPHandler)

		// TemplateRequirementService handler manages organization/folder scoped
		// TemplateRequirement CRDs used by ADR 032 dependency materialisation.
		templateRequirementsK8s := templaterequirements.NewK8sClient(templateCtrlClient)
		templateRequirementsHandler := templaterequirements.NewHandler(templateRequirementsK8s, nsResolver).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver)
		templateRequirementsPath, templateRequirementsHTTPHandler := consolev1connect.NewTemplateRequirementServiceHandler(templateRequirementsHandler, protectedInterceptors)
		mux.Handle(templateRequirementsPath, templateRequirementsHTTPHandler)

		// TemplateGrantService handler manages organization/folder scoped
		// TemplateGrant CRDs used by ADR 032 dependency materialisation.
		// TemplateGrants authorize cross-namespace template references, mirroring
		// the Gateway API ReferenceGrant pattern.
		templateGrantsK8s := templategrants.NewK8sClient(templateCtrlClient)
		templateGrantsHandler := templategrants.NewHandler(templateGrantsK8s, nsResolver).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver)
		templateGrantsPath, templateGrantsHTTPHandler := consolev1connect.NewTemplateGrantServiceHandler(templateGrantsHandler, protectedInterceptors)
		mux.Handle(templateGrantsPath, templateGrantsHTTPHandler)

		// TemplatePolicyService handler — manages REQUIRE/EXCLUDE policies at
		// organization and folder scopes (HOL-556). Project scope is rejected:
		// a project owner has write access to the project namespace, so any
		// policy artifact stored there could be tampered with by the very
		// actor the policy is meant to constrain (HOL-554 storage-isolation
		// design note).
		//
		// templatePoliciesK8s was constructed earlier so the folderResolver
		// could be wired; reuse the same client here so there is exactly
		// one policy reader+writer in the process.
		//
		// Render-target selection runs entirely through TemplatePolicyBinding.
		// Guardrails belong on the binding create/update path (enforced by
		// console/templatepolicybindings) rather than on the policy itself.
		templatePoliciesHandler := templatepolicies.NewHandler(templatePoliciesK8s, nsResolver).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver).
			WithTemplateExistsResolver(templates.NewTemplateExistsAdapter(templatesK8s))
		templatePoliciesPath, templatePoliciesHTTPHandler := consolev1connect.NewTemplatePolicyServiceHandler(templatePoliciesHandler, protectedInterceptors)
		mux.Handle(templatePoliciesPath, templatePoliciesHTTPHandler)

		// TemplatePolicyBindingService handler — manages the explicit,
		// non-glob binding of a TemplatePolicy to a list of project
		// templates and deployments (ADR 029 / HOL-590). Project scope
		// is rejected for the same storage-isolation reason as policies
		// (HOL-554): a project owner must not be able to tamper with
		// the binding list the platform uses to constrain them. The
		// handler's validation seams (policy-exists, ancestor-chain,
		// project-exists) are wired via adapters that lean on the
		// resources already constructed above — templatePoliciesK8s for
		// policy lookups, nsWalker for ancestor chains, and the shared
		// k8sClientset for project-namespace existence. The K8sClient
		// itself is constructed alongside the render-time resolver
		// (HOL-596) so both the handler and the resolver share a single
		// client instance.
		templatePolicyBindingsHandler := templatepolicybindings.NewHandler(templatePolicyBindingsK8s, nsResolver).
			WithOrgGrantResolver(orgGrantResolver).
			WithFolderGrantResolver(folderGrantResolver).
			WithPolicyExistsResolver(templatepolicybindings.NewPolicyExistsAdapter(templatePoliciesK8s, nsResolver)).
			WithAncestorChainResolver(templatepolicybindings.NewAncestorChainAdapter(nsWalker)).
			WithProjectExistsResolver(templatepolicybindings.NewProjectExistsAdapter(k8sClientset, nsResolver))
		templatePolicyBindingsPath, templatePolicyBindingsHTTPHandler := consolev1connect.NewTemplatePolicyBindingServiceHandler(templatePolicyBindingsHandler, protectedInterceptors)
		mux.Handle(templatePolicyBindingsPath, templatePolicyBindingsHTTPHandler)

		// Deployment service with project grant fallback.
		// ancestorTemplateResolver wraps templatesK8s + nsWalker to satisfy
		// AncestorTemplateProvider for full ancestor-chain template resolution
		// (org + folders) at render time (ADR 019, issue #874).
		deploymentsK8s := deployments.NewK8sClient(k8sClientset, nsResolver)
		var deploymentsApplier deployments.ResourceApplier
		if dynamicClient != nil {
			deploymentsApplier = deployments.NewApplier(dynamicClient)
			// Wire the same dynamic client onto the K8sClient so the
			// link aggregator (HOL-574) can scan owned resources
			// across every kind apply.go writes.
			deploymentsK8s = deploymentsK8s.WithDynamicClient(dynamicClient)
		}
		// HOL-957: wire the CRWriter so every Deployment create/update/delete
		// is dual-written to the deployments.holos.run/v1alpha1 CRD via SSA
		// alongside the existing ConfigMap proto-store. The CRWriter is only
		// active when the embedded controller-runtime manager is available —
		// the manager provides the cache-backed client.Client the CRWriter
		// uses and ensures the Deployment CRD's informer is registered before
		// /readyz goes green.
		if s.controllerMgr != nil {
			deploymentsK8s = deploymentsK8s.WithCRWriter(
				deployments.NewCRWriter(s.controllerMgr.GetClient(), nsResolver),
			)
		}
		projectFolderResolver := projects.NewProjectFolderResolver(projectsK8s, nsWalker)
		ancestorTemplateResolver := templates.NewAncestorTemplateResolver(templatesK8s, nsWalker, policyResolverSeam)
		// Deployment status informer cache: one shared watch scoped to
		// console-managed apps/v1.Deployment resources via the managed-by
		// label. The informer lifecycle is tied to the server context so it
		// stops cleanly on shutdown. Cache misses are treated as "no data
		// yet" by the handler. New is non-blocking: it starts the informer
		// in the background and reports misses until the initial sync
		// completes, so a transiently unreachable API server or a missing
		// watch RBAC rule never delays startup. If the initial sync does
		// not complete within statuscache's internal timeout, the package
		// itself cancels the reflector to avoid leaking LIST/WATCH retry
		// goroutines and logs a warning.
		deploymentStatusCache := statuscache.New(ctx, k8sClientset)
		// HOL-644: bridge the deployments handler to the organization
		// gateway-namespace annotation so PlatformInput.gatewayNamespace
		// reflects the platform engineer's configured value (set via the
		// Organization service in HOL-643) rather than the historical
		// hard-coded "istio-ingress". gatewayResolver is constructed above
		// (shared with the templates handler, HOL-828).
		deploymentsHandler := deployments.NewHandler(deploymentsK8s, projectResolver, settingsK8s, templates.NewProjectScopedResolver(templatesK8s), &deployments.CueRenderer{}, deploymentsApplier).
			WithAncestorWalker(projectFolderResolver).
			WithAncestorTemplateProvider(ancestorTemplateResolver).
			WithStatusCache(deploymentStatusCache).
			WithPolicyDriftChecker(deploymentDriftAdapter).
			WithDependencyEdgeProvider(appliedRenderStateClient).
			WithOrganizationGatewayResolver(gatewayResolver)
		if s.controllerMgr != nil {
			deploymentsHandler = deploymentsHandler.WithDependencyEdgeWriter(
				deployments.NewDependencyEdgeCRDWriter(s.controllerMgr.GetClient()),
			)
		}
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
		consolev1connect.TemplateDependencyServiceName,
		consolev1connect.TemplateRequirementServiceName,
		consolev1connect.TemplateGrantServiceName,
		consolev1connect.TemplatePolicyServiceName,
		consolev1connect.TemplatePolicyBindingServiceName,
		consolev1connect.FolderServiceName,
		consolev1connect.DeploymentServiceName,
		consolev1connect.PermissionsServiceName,
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

	// Create console config for frontend injection.
	// The namespace prefixes are always injected so the frontend's
	// scope-label helpers (see `frontend/src/lib/scope-labels.ts`) stay
	// aligned with the server resolver even when an operator overrides the
	// defaults (HOL-722). DevTools remains off by default.
	consoleConfig := &ConsoleConfig{
		DevToolsEnabled:    s.cfg.EnableDevTools,
		NamespacePrefix:    s.cfg.NamespacePrefix,
		OrganizationPrefix: s.cfg.OrganizationPrefix,
		FolderPrefix:       s.cfg.FolderPrefix,
		ProjectPrefix:      s.cfg.ProjectPrefix,
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

	errCh := make(chan error, 2)
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

	// HOL-620: run the embedded controller-runtime manager alongside the
	// HTTP listener. A manager failure (cache sync timeout, API server
	// unreachable) tears the whole process down so Kubernetes reschedules
	// the pod — the same failure mode as an HTTP listener error.
	if s.controllerMgr != nil {
		go func() {
			if err := s.controllerMgr.Start(ctx); err != nil {
				errCh <- fmt.Errorf("controller manager exited: %w", err)
			}
		}()
	}

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

// projectNSPipelineAdapter converts between the projects handler's
// ProjectNamespacePipeline interface and the concrete
// projectnspipeline.Pipeline. The interface lives in console/projects so
// the handler package does not depend on projectnspipeline (which would
// pull in console/templates → console/deployments and cycle with the
// deployments test suite that imports console/projects). The adapter
// lives here in console/console.go where both packages are already
// imported for production wiring.
type projectNSPipelineAdapter struct {
	p *projectnspipeline.Pipeline
}

// Run translates the handler-side input into the pipeline's native
// Input and maps the Outcome back. BaseNamespace and Platform pass
// through by reference/value — no defensive copy is necessary because
// the handler does not mutate them after the Run call returns.
func (a *projectNSPipelineAdapter) Run(ctx context.Context, in projects.ProjectNamespacePipelineInput) (projects.ProjectNamespacePipelineOutcome, error) {
	outcome, err := a.p.Run(ctx, projectnspipeline.Input{
		ProjectName:     in.ProjectName,
		ParentNamespace: in.ParentNamespace,
		BaseNamespace:   in.BaseNamespace,
		Platform:        in.Platform,
	})
	return mapProjectNSOutcome(outcome), err
}

// mapProjectNSOutcome converts from the pipeline's internal Outcome to
// the handler-side enum. An explicit switch surfaces a compile-time
// nudge if a new Outcome value is added to projectnspipeline and
// callers forget to extend the mapping — the default branch falls back
// to NoBindings so the handler never skips its typed Create on an
// unrecognised outcome.
func mapProjectNSOutcome(o projectnspipeline.Outcome) projects.ProjectNamespacePipelineOutcome {
	switch o {
	case projectnspipeline.OutcomeBindingsApplied:
		return projects.ProjectNamespacePipelineBindingsApplied
	case projectnspipeline.OutcomeNoBindings:
		return projects.ProjectNamespacePipelineNoBindings
	default:
		return projects.ProjectNamespacePipelineNoBindings
	}
}

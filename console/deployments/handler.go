package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments/statuscache"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const (
	auditResourceType = "deployment"
	// cueTemplateKey is the ConfigMap data key holding the CUE template source.
	// Mirrors templates.CueTemplateKey to avoid a cross-package import cycle.
	cueTemplateKey = "template.cue"
	// OutputURLAnnotation caches the template-evaluated `output.url` on the
	// deployment ConfigMap. Populated at create/update time after a
	// successful render so ListDeployments, GetDeployment, and
	// GetDeploymentStatusSummary can surface the URL without re-running the
	// CUE render pipeline per row. An empty or missing value means the
	// template did not publish a URL (or render has not yet succeeded).
	OutputURLAnnotation = "console.holos.run/output-url"
)

// dnsLabelRe validates deployment names as DNS labels.
var dnsLabelRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ProjectResolver resolves project namespace grants for access checks.
type ProjectResolver interface {
	GetProjectGrants(ctx context.Context, project string) (shareUsers, shareRoles map[string]string, err error)
}

// SettingsResolver checks if deployments are enabled for a project.
type SettingsResolver interface {
	GetSettings(ctx context.Context, project string) (*consolev1.ProjectSettings, error)
}

// TemplateResolver validates that a referenced template exists and returns its CUE source.
type TemplateResolver interface {
	GetTemplate(ctx context.Context, project, name string) (*corev1.ConfigMap, error)
}

// DefaultGatewayNamespace is the fallback namespace for the ingress gateway,
// injected into PlatformInput.GatewayNamespace when no resolver is wired,
// when the OrganizationGatewayResolver returns an error, or when the owning
// org has no `console.holos.run/gateway-namespace` annotation set (HOL-526
// makes this configurable per-organization via the Settings UI; HOL-644
// wires the resolver). It is intentionally NOT the only supported value —
// platform engineers pin a cluster-specific gateway namespace (e.g.
// "ci-private-apps-gateway") via the org annotation.
const DefaultGatewayNamespace = "istio-ingress"

// Renderer evaluates CUE templates with deployment parameters.
type Renderer interface {
	// Render evaluates the deployment template unified with zero or more
	// ancestor template CUE sources and the provided inputs, returning
	// resources grouped by origin (platform vs project). The render level is
	// controlled by inputs.ReadPlatformResources (project-level: false;
	// organization/folder-level: true); ancestorSources may be empty at
	// either level and carries additional template CUE sources unified with
	// the deployment template but does not select the render level (ADR 016
	// Decision 8).
	Render(ctx context.Context, cueSource string, ancestorSources []string, inputs RenderInputs) (*GroupedResources, error)
}

// AncestorWalker resolves the folder ancestry for a project namespace.
// It returns the list of folder user-facing names from the organization down
// to (but not including) the project (i.e. org → folder1 → folder2 → project
// yields ["folder1", "folder2"] when folder namespaces exist). Used to
// populate PlatformInput.Folders so CUE templates can reference platform.folders.
type AncestorWalker interface {
	GetProjectFolders(ctx context.Context, project string) ([]string, error)
}

// AncestorTemplateProvider resolves platform template CUE sources from the
// full ancestor chain (org + folders) for render. The projectNs is the
// starting namespace for the ancestor walk; deploymentName identifies the
// render target so the underlying PolicyResolver can key REQUIRE/EXCLUDE
// evaluation off it (HOL-566 Phase 4).
//
// The second return value is the policy-effective ref set
// (REQUIRE-injected − EXCLUDE-removed) that produced the sources.
// Exposing it here lets the
// deployments Create/Update happy paths write-through the same set to the
// applied-render-set store via PolicyDriftChecker.RecordApplied without
// invoking the resolver a second time (HOL-569). A second invocation would
// open a race with concurrent policy edits in which the stored set drifts
// from the rendered set and GetDeploymentPolicyState reports false drift.
// Callers that only need the sources can ignore the second return.
type AncestorTemplateProvider interface {
	ListAncestorTemplateSources(ctx context.Context, projectNs, deploymentName string) ([]string, []*consolev1.LinkedTemplateRef, error)
}

// ResourceApplier applies and cleans up K8s resources for a deployment.
// Each resource carries its own metadata.namespace; Apply and Reconcile use
// per-resource namespaces rather than a single namespace parameter.
// All methods accept a project identifier to scope ownership labels and
// prevent cross-project collisions in shared namespaces.
type ResourceApplier interface {
	Apply(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured) error
	// Reconcile applies desired resources via SSA then deletes owned resources
	// that are no longer in the desired set (orphan cleanup). previousNamespaces
	// lists namespaces that previously held resources for this deployment so
	// orphans from namespace moves are cleaned up.
	Reconcile(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured, previousNamespaces ...string) error
	// Cleanup deletes all owned resources across the given namespaces.
	Cleanup(ctx context.Context, namespaces []string, project, deploymentName string) error
	// DiscoverNamespaces scans the cluster for all namespaces that contain
	// resources owned by the given project and deployment. Returns an error
	// if discovery is incomplete (some resource kinds could not be listed).
	DiscoverNamespaces(ctx context.Context, project, deploymentName string) ([]string, error)
}

// OrganizationGatewayResolver resolves the configured ingress-gateway
// namespace for the organization that owns a given project. Implementations
// read the org namespace's gateway-namespace annotation
// (v1alpha2.AnnotationGatewayNamespace, set via the Organization service in
// HOL-643). Returning an empty string means the org has no override and the
// caller should fall back to DefaultGatewayNamespace.
//
// The handler treats a non-nil error as a soft failure: it logs and falls
// back to DefaultGatewayNamespace so a transient lookup failure (or a missing
// org namespace in legacy test fixtures) cannot break renders.
type OrganizationGatewayResolver interface {
	GetGatewayNamespace(ctx context.Context, project string) (string, error)
}

// Handler implements the DeploymentService.
type Handler struct {
	consolev1connect.UnimplementedDeploymentServiceHandler
	k8s                      *K8sClient
	projectResolver          ProjectResolver
	settingsResolver         SettingsResolver
	templateResolver         TemplateResolver
	renderer                 Renderer
	applier                  ResourceApplier
	logReader                LogReader
	ancestorWalker           AncestorWalker
	ancestorTemplateProvider AncestorTemplateProvider
	statusCache              statuscache.Cache
	policyDriftChecker       PolicyDriftChecker
	gatewayResolver          OrganizationGatewayResolver
}

// PolicyDriftChecker exposes the minimal surface a deployment needs from the
// TemplatePolicy resolver + applied-render-state store in order to report
// drift and serve GetDeploymentPolicyState. Implementations record the
// resolved set on successful Create/Update and read it back from the folder
// namespace on subsequent status queries. Defined as a local interface so
// tests can stub it without depending on console/policyresolver.
type PolicyDriftChecker interface {
	// Drift reports whether the current resolver output differs from the
	// applied render set last recorded for the target. Returns (drift,
	// hasAppliedState, error). When hasAppliedState is false the target
	// has not yet been rendered through the post-HOL-567 path and drift
	// is meaningless — callers SHOULD NOT surface policy_drift in that
	// case.
	Drift(ctx context.Context, project, deploymentName string) (drift, hasAppliedState bool, err error)
	// PolicyState returns the full snapshot for the deployment: applied,
	// current, added, removed, drift, has_applied_state.
	PolicyState(ctx context.Context, project, deploymentName string) (*consolev1.PolicyState, error)
	// RecordApplied persists the effective render set for the target on
	// successful Create/Update. Idempotent: second call with the same
	// refs overwrites the first.
	RecordApplied(ctx context.Context, project, deploymentName string, refs []*consolev1.LinkedTemplateRef) error
}

// NewHandler creates a DeploymentService handler.
func NewHandler(k8s *K8sClient, projectResolver ProjectResolver, settingsResolver SettingsResolver, templateResolver TemplateResolver, renderer Renderer, applier ResourceApplier) *Handler {
	return &Handler{
		k8s:              k8s,
		projectResolver:  projectResolver,
		settingsResolver: settingsResolver,
		templateResolver: templateResolver,
		renderer:         renderer,
		applier:          applier,
	}
}

// WithAncestorWalker configures the handler with an AncestorWalker for
// resolving folder ancestry. When set, PlatformInput.Folders is populated
// so CUE templates can reference platform.folders.
func (h *Handler) WithAncestorWalker(aw AncestorWalker) *Handler {
	h.ancestorWalker = aw
	return h
}

// WithAncestorTemplateProvider configures the handler with an
// AncestorTemplateProvider for resolving platform template CUE sources from
// the full ancestor chain (org + folders) at render time.
func (h *Handler) WithAncestorTemplateProvider(atp AncestorTemplateProvider) *Handler {
	h.ancestorTemplateProvider = atp
	return h
}

// WithOrganizationGatewayResolver configures the handler with a resolver
// that returns the ingress-gateway namespace configured on the project's
// owning organization (see v1alpha2.AnnotationGatewayNamespace, persisted by
// the Organization service in HOL-643). When set, buildPlatformInput
// consults the resolver and falls back to DefaultGatewayNamespace only when
// the annotation is absent (or the lookup fails). When unset, behavior is
// unchanged from the legacy hard-coded default — a nil resolver keeps
// existing test wiring working without modification.
func (h *Handler) WithOrganizationGatewayResolver(r OrganizationGatewayResolver) *Handler {
	h.gatewayResolver = r
	return h
}

// WithStatusCache configures the handler with a shared-informer-backed cache
// of apps/v1 Deployment status. When set, the listing hot path and
// GetDeploymentStatusSummary RPC read from the local cache instead of issuing
// direct API calls. A nil cache means the handler falls back to UNSPECIFIED
// status summaries (no data yet).
func (h *Handler) WithStatusCache(c statuscache.Cache) *Handler {
	h.statusCache = c
	return h
}

// WithPolicyDriftChecker wires the TemplatePolicy drift checker used by
// Create/UpdateDeployment to persist the resolved render set, by
// GetDeploymentPolicyState to surface the full snapshot, and by
// DeploymentStatusSummary.policy_drift to flag drifted deployments on the
// list view. A nil checker disables drift persistence and reporting so
// local/dev wiring without a policy resolver still works.
func (h *Handler) WithPolicyDriftChecker(c PolicyDriftChecker) *Handler {
	h.policyDriftChecker = c
	return h
}

// resolveAncestorTemplateSources resolves platform template CUE sources from
// the full ancestor chain when an AncestorTemplateProvider is configured.
// deploymentName identifies the render target so the underlying
// PolicyResolver can key REQUIRE/EXCLUDE evaluation off it.
//
// Returns (sources, effectiveRefs, true) on success — the effectiveRefs
// slice carries the policy-resolved ref set that produced the sources so
// the deployments handler can write-through the same set to the applied-
// render-set store after a successful apply/reconcile without a second
// resolver invocation (HOL-569). Returns (nil, nil, false) when no
// provider is configured or the walk fails; the caller should fall back to
// a project-only render in that case.
func (h *Handler) resolveAncestorTemplateSources(ctx context.Context, project, deploymentName string) ([]string, []*consolev1.LinkedTemplateRef, bool) {
	if h.ancestorTemplateProvider == nil {
		return nil, nil, false
	}
	projectNs := h.k8s.Resolver.ProjectNamespace(project)
	sources, effectiveRefs, err := h.ancestorTemplateProvider.ListAncestorTemplateSources(ctx, projectNs, deploymentName)
	if err != nil {
		slog.WarnContext(ctx, "ancestor template resolution failed, skipping platform template unification",
			slog.String("project", project),
			slog.String("deployment", deploymentName),
			slog.Any("error", err),
		)
		return nil, nil, false
	}
	return sources, effectiveRefs, true
}

// renderResources renders deployment resources, unifying with platform
// templates from the full ancestor chain (org + folders) when an
// AncestorTemplateProvider is configured. The effective template set is
// derived exclusively from TemplatePolicyBinding resolution.
//
// When no AncestorTemplateProvider is configured, falls back to a plain
// project-level render (deployment template only, no platform template
// unification).
//
// This helper flattens the grouped render result into a single list. Callers
// that need the per-origin split should use renderResourcesGrouped.
func (h *Handler) renderResources(ctx context.Context, project, deploymentName, cueSource string, platform v1alpha2.PlatformInput, projectInput v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	grouped, _, err := h.renderResourcesGrouped(ctx, project, deploymentName, cueSource, platform, projectInput)
	if err != nil {
		return nil, err
	}
	combined := make([]unstructured.Unstructured, 0, len(grouped.Platform)+len(grouped.Project))
	combined = append(combined, grouped.Platform...)
	combined = append(combined, grouped.Project...)
	return combined, nil
}

// renderResourcesGrouped mirrors renderResources but returns resources grouped
// by origin (platform vs project). Used by GetDeploymentRenderPreview to populate
// the per-collection response fields.
//
// When an AncestorTemplateProvider is configured this is an
// organization/folder-level render (ReadPlatformResources=true) even when the
// ancestor chain returns zero sources, so a deployment template authored with
// its own platformResources still emits them. When no provider is configured
// we fall back to a project-level render (ADR 016 Decision 8).
//
// The second return value is the policy-effective ref set the provider
// reported — callers on the Create/Update write path pass it to
// PolicyDriftChecker.RecordApplied so the applied-render-set store stays
// aligned with what was actually rendered (HOL-569). Preview callers can
// ignore it. It is nil when no AncestorTemplateProvider is configured or the
// provider returned an error (the render falls back to project-only in both
// cases, so there is no rendered set to record).
func (h *Handler) renderResourcesGrouped(ctx context.Context, project, deploymentName, cueSource string, platform v1alpha2.PlatformInput, projectInput v1alpha2.ProjectInput) (*GroupedResources, []*consolev1.LinkedTemplateRef, error) {
	var ancestorSources []string
	var effectiveRefs []*consolev1.LinkedTemplateRef
	readPlatformResources := false
	if sources, refs, ok := h.resolveAncestorTemplateSources(ctx, project, deploymentName); ok {
		ancestorSources = sources
		effectiveRefs = refs
		readPlatformResources = true
	}
	grouped, err := h.renderer.Render(ctx, cueSource, ancestorSources, RenderInputs{
		Platform:              platform,
		Project:               projectInput,
		ReadPlatformResources: readPlatformResources,
	})
	if err != nil {
		return nil, nil, err
	}
	return grouped, effectiveRefs, nil
}

// ListDeployments returns all deployments in a project.
func (h *Handler) ListDeployments(
	ctx context.Context,
	req *connect.Request[consolev1.ListDeploymentsRequest],
) (*connect.Response[consolev1.ListDeploymentsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsList); err != nil {
		return nil, err
	}

	cms, err := h.k8s.ListDeployments(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	ns := h.k8s.Resolver.ProjectNamespace(project)
	deployments := make([]*consolev1.Deployment, 0, len(cms))
	for i := range cms {
		cm := &cms[i]
		dep := configMapToDeployment(cm, project)
		summary, ok := h.summaryFromCache(ns, cm.Name)
		if !ok {
			// Synthesize a minimal summary so cached annotation-driven
			// metadata (output-url, aggregated links) still reaches the
			// client even before the informer has observed the
			// apps/v1.Deployment (first render, empty cluster, etc.).
			// Phase stays UNSPECIFIED exactly as GetDeploymentStatusSummary
			// does on a cache miss so listing and single-row polling share
			// the same derivation path.
			if cm.Annotations[OutputURLAnnotation] != "" || cm.Annotations[v1alpha2.AnnotationAggregatedLinks] != "" {
				summary = &consolev1.DeploymentStatusSummary{
					Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED,
				}
			}
		}
		if summary != nil {
			mergeOutputURLAnnotation(summary, cm)
			mergeAggregatedLinksAnnotation(summary, cm)
			dep.StatusSummary = summary
		}
		deployments = append(deployments, dep)
	}

	slog.InfoContext(ctx, "deployments listed",
		slog.String("action", "deployments_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(deployments)),
	)

	return connect.NewResponse(&consolev1.ListDeploymentsResponse{
		Deployments: deployments,
	}), nil
}

// GetDeployment returns a single deployment by name.
func (h *Handler) GetDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentRequest],
) (*connect.Response[consolev1.GetDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	cm, err := h.k8s.GetDeployment(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment read",
		slog.String("action", "deployment_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	deployment := configMapToDeployment(cm, project)
	ns := h.k8s.Resolver.ProjectNamespace(project)
	// Refresh the aggregated-links cache by scanning owned resources so
	// links stamped on workloads after the last apply (e.g. an operator
	// adding `link.argocd.argoproj.io/*` annotations out-of-band) appear
	// without waiting for the next render. The refresh is best-effort:
	// scan/write failures fall back to whatever the cached annotation
	// already held. Compute fresh values up front so they are available
	// regardless of whether the status cache fired.
	freshLinks, freshPrimary := h.refreshAggregatedLinksCache(ctx, project, name, cm)
	summary, ok := h.summaryFromCache(ns, cm.Name)
	if !ok && (cm.Annotations[OutputURLAnnotation] != "" || len(freshLinks) > 0 || freshPrimary != "") {
		// Cache miss, but we have cached metadata to surface (output URL,
		// aggregated links, or a freshly promoted primary URL). Mirror
		// ListDeployments and GetDeploymentStatusSummary by synthesizing
		// an UNSPECIFIED summary so the frontend still receives them.
		summary = &consolev1.DeploymentStatusSummary{
			Phase: consolev1.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED,
		}
	}
	if summary != nil {
		mergeOutputURLAnnotation(summary, cm)
		applyAggregatedLinks(summary, freshLinks, freshPrimary)
		deployment.StatusSummary = summary
	}

	return connect.NewResponse(&consolev1.GetDeploymentResponse{
		Deployment: deployment,
	}), nil
}

// CreateDeployment creates a new deployment.
func (h *Handler) CreateDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.CreateDeploymentRequest],
) (*connect.Response[consolev1.CreateDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if err := validateDeploymentName(name); err != nil {
		return nil, err
	}
	if req.Msg.Image == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image is required"))
	}
	if req.Msg.Tag == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("tag is required"))
	}
	if req.Msg.Template == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("template is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	// Check that deployments are enabled in project settings.
	if h.settingsResolver != nil {
		s, err := h.settingsResolver.GetSettings(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "failed to resolve project settings",
				slog.String("project", project),
				slog.Any("error", err),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check project settings"))
		}
		if !s.DeploymentsEnabled {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("deployments are not enabled for project %q", project))
		}
	}

	// Validate that the referenced template exists and get its CUE source.
	var cueSource string
	if h.templateResolver != nil {
		tmplCM, err := h.templateResolver.GetTemplate(ctx, project, req.Msg.Template)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template %q not found in project %q", req.Msg.Template, project))
		}
		cueSource = tmplCM.Data[cueTemplateKey]
	}

	envInputs, err := validateEnvVars(req.Msg.Env)
	if err != nil {
		return nil, err
	}

	displayName := ""
	if req.Msg.DisplayName != nil {
		displayName = *req.Msg.DisplayName
	}
	description := ""
	if req.Msg.Description != nil {
		description = *req.Msg.Description
	}

	_, err = h.k8s.CreateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.Template, displayName, description, req.Msg.Command, req.Msg.Args, envInputs, req.Msg.Port)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Render and apply the deployment resources. On any failure, roll back by
	// cleaning up partial K8s resources and deleting the deployment ConfigMap so
	// the operation is all-or-nothing. Uses RenderGrouped to capture the
	// evaluated `output` section so a non-empty URL can be cached as an
	// annotation on the ConfigMap for later listing calls.
	if h.renderer != nil && h.applier != nil {
		ns := h.k8s.Resolver.ProjectNamespace(project)
		platformIn := h.buildPlatformInput(ctx, project, ns, claims)
		projectIn := v1alpha2.ProjectInput{
			Name:    name,
			Image:   req.Msg.Image,
			Tag:     req.Msg.Tag,
			Command: req.Msg.Command,
			Args:    req.Msg.Args,
			Env:     envInputs,
			Port:    defaultPort(int(req.Msg.Port)),
		}
		grouped, effectiveRefs, renderErr := h.renderResourcesGrouped(ctx, project, name, cueSource, platformIn, projectIn)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed after creating deployment — rolling back",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			h.rollbackCreate(ctx, ns, project, name)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		resources := append(grouped.Platform, grouped.Project...)
		if applyErr := h.applier.Apply(ctx, project, name, resources); applyErr != nil {
			slog.WarnContext(ctx, "apply failed after creating deployment — rolling back",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", applyErr),
			)
			h.rollbackCreate(ctx, ns, project, name)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("applying deployment resources: %w", applyErr))
		}
		// Write-through the effective render set to the applied-render-set
		// store so GetDeploymentPolicyState and the list-view policy_drift
		// flag have a baseline to diff against (HOL-569). Skipped when no
		// checker is wired (local/dev bootstrap without a cluster policy
		// resolver). When the provider signaled a degraded render by
		// returning nil effectiveRefs (walker failed / no walker), record
		// a non-nil empty slice so the baseline reflects reality — zero
		// ancestor templates actually participated in this apply. This
		// keeps GetDeploymentPolicyState honest: a subsequent query whose
		// resolver returns a non-empty current set will correctly report
		// drift against the empty applied set (review round 2 P2 finding).
		// A record failure is logged at warn level and does NOT fail the
		// RPC — the deployment was rendered and applied successfully, and
		// the set can be reconstructed on the next render. This mirrors
		// the SetOutputURLAnnotation precedent immediately below.
		if h.policyDriftChecker != nil {
			refsToRecord := effectiveRefs
			if refsToRecord == nil {
				refsToRecord = []*consolev1.LinkedTemplateRef{}
			}
			if recordErr := h.policyDriftChecker.RecordApplied(ctx, project, name, refsToRecord); recordErr != nil {
				slog.WarnContext(ctx, "failed to record applied render set after create",
					slog.String("project", project),
					slog.String("name", name),
					slog.Any("error", recordErr),
				)
			}
		}
		// Cache the evaluated output.url on the ConfigMap annotation so
		// later ListDeployments/GetDeployment calls can surface it without
		// re-rendering. Skip when the template has no output block or no
		// meaningful URL; failures here are logged and do not fail the RPC
		// because the deployment itself was created successfully.
		if url := outputURLFromJSON(ctx, project, name, grouped.OutputJSON); url != "" {
			if err := h.k8s.SetOutputURLAnnotation(ctx, project, name, url); err != nil {
				slog.WarnContext(ctx, "failed to cache output URL annotation after create",
					slog.String("project", project),
					slog.String("name", name),
					slog.Any("error", err),
				)
			}
		}
		// Aggregate `external-link.*`, `primary-url`, and ArgoCD
		// `link.*` annotations across owned resources and cache the
		// merged set on the deployment ConfigMap. Sibling of the
		// SetOutputURLAnnotation cache; failures are logged and
		// non-fatal for the same reason.
		h.stampAggregatedLinks(ctx, project, name)
	}

	slog.InfoContext(ctx, "deployment created",
		slog.String("action", "deployment_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateDeploymentResponse{
		Name: name,
	}), nil
}

// rollbackCreate attempts to undo a partially-applied CreateDeployment:
//  1. Calls Cleanup to remove any K8s resources already applied.
//  2. Deletes the deployment ConfigMap metadata record.
//
// Rollback errors are logged at warn level but do not replace the original error.
func (h *Handler) rollbackCreate(ctx context.Context, ns, project, name string) {
	// Discover all namespaces with owned resources. Include the project
	// namespace as a fallback in case label discovery misses partially-applied
	// resources. During rollback, proceed even if discovery is incomplete.
	namespaces, discoverErr := h.applier.DiscoverNamespaces(ctx, project, name)
	if discoverErr != nil {
		slog.WarnContext(ctx, "rollback: namespace discovery incomplete, proceeding with partial set + project namespace",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", discoverErr),
		)
	}
	nsSet := make(map[string]struct{}, len(namespaces)+1)
	for _, n := range namespaces {
		nsSet[n] = struct{}{}
	}
	nsSet[ns] = struct{}{}
	allNS := make([]string, 0, len(nsSet))
	for n := range nsSet {
		allNS = append(allNS, n)
	}
	if cleanupErr := h.applier.Cleanup(ctx, allNS, project, name); cleanupErr != nil {
		slog.WarnContext(ctx, "rollback: cleanup failed",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", cleanupErr),
		)
	}
	if deleteErr := h.k8s.DeleteDeployment(ctx, project, name); deleteErr != nil {
		slog.WarnContext(ctx, "rollback: delete ConfigMap failed",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", deleteErr),
		)
	}
}

// UpdateDeployment updates an existing deployment.
func (h *Handler) UpdateDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateDeploymentRequest],
) (*connect.Response[consolev1.UpdateDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	envInputs, err := validateEnvVars(req.Msg.Env)
	if err != nil {
		return nil, err
	}

	updated, err := h.k8s.UpdateDeployment(ctx, project, name, req.Msg.Image, req.Msg.Tag, req.Msg.DisplayName, req.Msg.Description, req.Msg.Command, req.Msg.Args, envInputs, req.Msg.Port)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Re-render and reconcile deployment resources with updated parameters.
	// Reconcile applies the new desired set via SSA then deletes any previously
	// owned resources that are no longer in the desired set (orphan cleanup).
	if h.renderer != nil && h.applier != nil && updated != nil {
		templateName := updated.Data[TemplateKey]
		image := updated.Data[ImageKey]
		tag := updated.Data[TagKey]

		// Look up the template CUE source.
		var cueSource string
		if h.templateResolver != nil && templateName != "" {
			tmplCM, tmplErr := h.templateResolver.GetTemplate(ctx, project, templateName)
			if tmplErr != nil {
				slog.WarnContext(ctx, "template not found during update re-render",
					slog.String("project", project),
					slog.String("template", templateName),
					slog.Any("error", tmplErr),
				)
			} else {
				cueSource = tmplCM.Data[cueTemplateKey]
			}
		}

		ns := h.k8s.Resolver.ProjectNamespace(project)
		platformIn := h.buildPlatformInput(ctx, project, ns, claims)
		projectIn := v1alpha2.ProjectInput{
			Name:    name,
			Image:   image,
			Tag:     tag,
			Command: commandFromConfigMap(updated),
			Args:    argsFromConfigMap(updated),
			Env:     envFromConfigMapAsV1alpha2(updated),
			Port:    defaultPort(portFromConfigMap(updated)),
		}
		grouped, effectiveRefs, renderErr := h.renderResourcesGrouped(ctx, project, name, cueSource, platformIn, projectIn)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rendering deployment resources: %w", renderErr))
		}
		resources := append(grouped.Platform, grouped.Project...)
		// Use Reconcile instead of Apply so orphaned resources from template
		// changes (e.g. a removed HTTPRoute) are cleaned up after a successful
		// apply. Pass previously-owned namespaces so orphans from namespace
		// moves are cleaned up.
		prevNS, discoverErr := h.applier.DiscoverNamespaces(ctx, project, name)
		if discoverErr != nil {
			slog.WarnContext(ctx, "namespace discovery incomplete during update, proceeding with partial set",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", discoverErr),
			)
		}
		if reconcileErr := h.applier.Reconcile(ctx, project, name, resources, prevNS...); reconcileErr != nil {
			slog.WarnContext(ctx, "reconcile failed during deployment update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", reconcileErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reconciling deployment resources: %w", reconcileErr))
		}
		// Write-through the effective render set so subsequent policy-state
		// queries diff against what this update actually rendered (HOL-569).
		// Same contract as the create path: nil checker is a no-op, record
		// errors are logged but do not fail the RPC because reconcile
		// already succeeded. A nil effectiveRefs signals a degraded render
		// path (walker failed / no walker) — in that case overwrite the
		// stored baseline with an empty set so any previously recorded
		// policy-resolved set cannot mask the fact that this reconcile
		// applied zero ancestor templates (review round 2 P2 finding).
		if h.policyDriftChecker != nil {
			refsToRecord := effectiveRefs
			if refsToRecord == nil {
				refsToRecord = []*consolev1.LinkedTemplateRef{}
			}
			if recordErr := h.policyDriftChecker.RecordApplied(ctx, project, name, refsToRecord); recordErr != nil {
				slog.WarnContext(ctx, "failed to record applied render set after update",
					slog.String("project", project),
					slog.String("name", name),
					slog.Any("error", recordErr),
				)
			}
		}
		// Refresh the cached output URL annotation. Unlike create, update
		// always sets or clears so a template edit that drops the output
		// block (or replaces the URL) does not leave a stale value behind.
		// A failure here is logged but does not fail the RPC because the
		// reconcile itself succeeded.
		url := outputURLFromJSON(ctx, project, name, grouped.OutputJSON)
		if err := h.k8s.SetOutputURLAnnotation(ctx, project, name, url); err != nil {
			slog.WarnContext(ctx, "failed to refresh output URL annotation after update",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", err),
			)
		}
		// Refresh the aggregated-links cache so a template change that
		// adds, removes, or renames link annotations on the rendered
		// resources is reflected on the next read without waiting for
		// the GetDeployment refresh path.
		h.stampAggregatedLinks(ctx, project, name)
	}

	slog.InfoContext(ctx, "deployment updated",
		slog.String("action", "deployment_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateDeploymentResponse{}), nil
}

// DeleteDeployment deletes a deployment.
func (h *Handler) DeleteDeployment(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteDeploymentRequest],
) (*connect.Response[consolev1.DeleteDeploymentResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsDelete); err != nil {
		return nil, err
	}

	// Clean up all K8s resources owned by this deployment before removing the record.
	// Discover all namespaces with owned resources so cross-namespace resources
	// are cleaned up (not just the project namespace). On partial discovery
	// (e.g. optional CRDs not installed), include the project namespace as a
	// fallback and proceed — best-effort cleanup is preferable to blocking
	// deletion entirely.
	if h.applier != nil {
		ns := h.k8s.Resolver.ProjectNamespace(project)
		namespaces, discoverErr := h.applier.DiscoverNamespaces(ctx, project, name)
		if discoverErr != nil {
			slog.WarnContext(ctx, "namespace discovery incomplete during delete, proceeding with partial set + project namespace",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", discoverErr),
			)
		}
		// Ensure the project namespace is always in the cleanup set.
		nsSet := make(map[string]struct{}, len(namespaces)+1)
		for _, n := range namespaces {
			nsSet[n] = struct{}{}
		}
		nsSet[ns] = struct{}{}
		allNS := make([]string, 0, len(nsSet))
		for n := range nsSet {
			allNS = append(allNS, n)
		}
		if cleanupErr := h.applier.Cleanup(ctx, allNS, project, name); cleanupErr != nil {
			slog.WarnContext(ctx, "cleanup failed during deployment delete",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", cleanupErr),
			)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cleaning up deployment resources: %w", cleanupErr))
		}
	}

	if err := h.k8s.DeleteDeployment(ctx, project, name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "deployment deleted",
		slog.String("action", "deployment_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteDeploymentResponse{}), nil
}

// ListNamespaceSecrets lists Kubernetes Secrets in the project namespace available for env var references.
func (h *Handler) ListNamespaceSecrets(
	ctx context.Context,
	req *connect.Request[consolev1.ListNamespaceSecretsRequest],
) (*connect.Response[consolev1.ListNamespaceSecretsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	items, err := h.k8s.ListNamespaceSecrets(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	secrets := make([]*consolev1.NamespaceResource, 0, len(items))
	for _, item := range items {
		secrets = append(secrets, &consolev1.NamespaceResource{
			Name: item.Name,
			Keys: item.Keys,
		})
	}

	slog.InfoContext(ctx, "namespace secrets listed",
		slog.String("action", "namespace_secrets_list"),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(secrets)),
	)

	return connect.NewResponse(&consolev1.ListNamespaceSecretsResponse{
		Secrets: secrets,
	}), nil
}

// ListNamespaceConfigMaps lists Kubernetes ConfigMaps in the project namespace available for env var references.
func (h *Handler) ListNamespaceConfigMaps(
	ctx context.Context,
	req *connect.Request[consolev1.ListNamespaceConfigMapsRequest],
) (*connect.Response[consolev1.ListNamespaceConfigMapsResponse], error) {
	project := req.Msg.Project
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsWrite); err != nil {
		return nil, err
	}

	items, err := h.k8s.ListNamespaceConfigMaps(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	configMaps := make([]*consolev1.NamespaceResource, 0, len(items))
	for _, item := range items {
		configMaps = append(configMaps, &consolev1.NamespaceResource{
			Name: item.Name,
			Keys: item.Keys,
		})
	}

	slog.InfoContext(ctx, "namespace configmaps listed",
		slog.String("action", "namespace_configmaps_list"),
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("count", len(configMaps)),
	)

	return connect.NewResponse(&consolev1.ListNamespaceConfigMapsResponse{
		ConfigMaps: configMaps,
	}), nil
}

// GetDeploymentRenderPreview returns the CUE template source, platform input,
// project input, and rendered output for a deployment.
func (h *Handler) GetDeploymentRenderPreview(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentRenderPreviewRequest],
) (*connect.Response[consolev1.GetDeploymentRenderPreviewResponse], error) {
	project := req.Msg.Project
	name := req.Msg.Name
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	// Look up the deployment record.
	cm, err := h.k8s.GetDeployment(ctx, project, name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Look up the template CUE source.
	templateName := cm.Data[TemplateKey]
	if templateName == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("deployment %q has no template configured", name))
	}
	if h.templateResolver == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("template resolver not configured"))
	}
	tmplCM, err := h.templateResolver.GetTemplate(ctx, project, templateName)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template %q not found in project %q", templateName, project))
	}
	cueTemplate := tmplCM.Data[cueTemplateKey]

	// Build platform input from authenticated claims and resolved namespace.
	ns := h.k8s.Resolver.ProjectNamespace(project)
	platformIn := h.buildPlatformInput(ctx, project, ns, claims)

	// Build project input from the deployment's stored fields.
	projectIn := v1alpha2.ProjectInput{
		Name:    name,
		Image:   cm.Data[ImageKey],
		Tag:     cm.Data[TagKey],
		Command: commandFromConfigMap(cm),
		Args:    argsFromConfigMap(cm),
		Env:     envFromConfigMapAsV1alpha2(cm),
		Port:    defaultPort(portFromConfigMap(cm)),
	}

	// Format platform and project inputs as CUE strings.
	platformJSON, err := json.Marshal(platformIn)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encoding platform input: %w", err))
	}
	projectJSON, err := json.Marshal(projectIn)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encoding project input: %w", err))
	}
	cuePlatformInput := fmt.Sprintf("platform: %s", string(platformJSON))
	cueProjectInput := fmt.Sprintf("input: %s", string(projectJSON))

	// Render the template to produce YAML and JSON output, including linked
	// platform templates (ADR 019). Uses renderResourcesGrouped so the same
	// linking logic applies at preview time as at deploy time, and the per-
	// collection fields are populated.
	var renderedYAML, renderedJSON string
	var platformResourcesYAML, platformResourcesJSON string
	var projectResourcesYAML, projectResourcesJSON string
	var grouped *GroupedResources
	if h.renderer != nil {
		var renderErr error
		// Preview path: discard the effective-ref set (second return) — only
		// the write-through paths on Create/Update consume it (HOL-569).
		grouped, _, renderErr = h.renderResourcesGrouped(ctx, project, name, cueTemplate, platformIn, projectIn)
		if renderErr != nil {
			slog.WarnContext(ctx, "render failed during deployment preview",
				slog.String("project", project),
				slog.String("name", name),
				slog.Any("error", renderErr),
			)
			// Return the inputs even if render fails — the frontend can display the error.
			return connect.NewResponse(&consolev1.GetDeploymentRenderPreviewResponse{
				CueTemplate:      cueTemplate,
				CuePlatformInput: cuePlatformInput,
				CueProjectInput:  cueProjectInput,
			}), nil
		}

		// Serialize per-collection resources.
		platformResourcesYAML, platformResourcesJSON = serializeUnstructured(grouped.Platform)
		projectResourcesYAML, projectResourcesJSON = serializeUnstructured(grouped.Project)

		// Produce the unified rendered output by combining both collections.
		allResources := append(grouped.Platform, grouped.Project...)
		renderedYAML, renderedJSON = serializeUnstructured(allResources)
	}

	slog.InfoContext(ctx, "deployment render preview",
		slog.String("action", "deployment_render_preview"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", project),
		slog.String("name", name),
		slog.String("sub", claims.Sub),
	)

	// Extract structured JSON fields from the grouped result if render succeeded.
	var defaultsJSON, platformInputJSON, projectInputJSON *string
	var platformResourcesStructJSON, projectResourcesStructJSON *string
	var output *consolev1.DeploymentOutput
	if grouped != nil {
		defaultsJSON = grouped.DefaultsJSON
		platformInputJSON = grouped.PlatformInputJSON
		projectInputJSON = grouped.ProjectInputJSON
		platformResourcesStructJSON = grouped.PlatformResourcesStructJSON
		projectResourcesStructJSON = grouped.ProjectResourcesStructJSON
		output = deploymentOutputFromJSON(ctx, project, name, grouped.OutputJSON)
	}

	return connect.NewResponse(&consolev1.GetDeploymentRenderPreviewResponse{
		CueTemplate:                     cueTemplate,
		CuePlatformInput:                cuePlatformInput,
		CueProjectInput:                 cueProjectInput,
		RenderedYaml:                    renderedYAML,
		RenderedJson:                    renderedJSON,
		PlatformResourcesYaml:           platformResourcesYAML,
		PlatformResourcesJson:           platformResourcesJSON,
		ProjectResourcesYaml:            projectResourcesYAML,
		ProjectResourcesJson:            projectResourcesJSON,
		DefaultsJson:                    defaultsJSON,
		PlatformInputJson:               platformInputJSON,
		ProjectInputJson:                projectInputJSON,
		PlatformResourcesStructuredJson: platformResourcesStructJSON,
		ProjectResourcesStructuredJson:  projectResourcesStructJSON,
		Output:                          output,
	}), nil
}

// deploymentOutputFromJSON unmarshals the evaluated `output` CUE section JSON
// into a DeploymentOutput proto message. Returns nil when outputJSON is nil,
// points to empty content, or cannot be parsed as JSON. A malformed OutputJSON
// is treated as non-fatal: a warning is logged and the handler leaves the
// response field unset rather than erroring the RPC. A valid but empty JSON
// object (e.g. `{}`) produces a non-nil DeploymentOutput with zero values so
// the frontend — not the backend — decides whether to render.
//
// Both the primary `url` and the additive `links` list are preserved:
// templates that publish `output.links` alongside `output.url` have the
// full list surfaced on the render-preview path even before the
// annotation aggregator runs. The links are passed through verbatim;
// normalization and live-resource annotation harvesting belong to the
// `console/links` parser and the aggregator helpers in this package
// (`aggregateLinksFromResources`, `applyAggregatedLinks`).
func deploymentOutputFromJSON(ctx context.Context, project, name string, outputJSON *string) *consolev1.DeploymentOutput {
	if outputJSON == nil {
		return nil
	}
	raw := strings.TrimSpace(*outputJSON)
	if raw == "" {
		return nil
	}
	var parsed struct {
		Url   string `json:"url"`
		Links []struct {
			Url         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Source      string `json:"source"`
			Name        string `json:"name"`
		} `json:"links"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.WarnContext(ctx, "failed to unmarshal OutputJSON into DeploymentOutput",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
		return nil
	}
	out := &consolev1.DeploymentOutput{Url: parsed.Url}
	if len(parsed.Links) > 0 {
		out.Links = make([]*consolev1.Link, 0, len(parsed.Links))
		for _, l := range parsed.Links {
			out.Links = append(out.Links, &consolev1.Link{
				Url:         l.Url,
				Title:       l.Title,
				Description: l.Description,
				Source:      l.Source,
				Name:        l.Name,
			})
		}
	}
	return out
}

// outputURLFromJSON extracts the `url` field from a JSON-encoded `output`
// CUE section. Returns an empty string when the pointer is nil, the content
// is empty, the JSON is malformed, or the `url` field is absent. Used on the
// deployment write path to decide what to cache in the OutputURLAnnotation.
// Never returns an error: a malformed OutputJSON simply yields "" so the
// annotation is cleared (not set) rather than failing the create/update.
func outputURLFromJSON(ctx context.Context, project, name string, outputJSON *string) string {
	if outputJSON == nil {
		return ""
	}
	raw := strings.TrimSpace(*outputJSON)
	if raw == "" {
		return ""
	}
	var parsed struct {
		Url string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.WarnContext(ctx, "failed to unmarshal OutputJSON for output-url annotation cache",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
		return ""
	}
	return parsed.Url
}

// mergeOutputURLAnnotation populates summary.Output from the cached
// OutputURLAnnotation on cm when both the summary exists and the annotation
// is a non-empty string. The cache is authoritative-at-time-of-render; if
// the annotation is missing (pre-feature ConfigMaps, failed renders, or
// templates with no output block) the summary is left unchanged so callers
// fall back to rendering without a URL. Never panics on a nil summary or a
// ConfigMap with nil annotations.
func mergeOutputURLAnnotation(summary *consolev1.DeploymentStatusSummary, cm *corev1.ConfigMap) {
	if summary == nil || cm == nil {
		return
	}
	url := cm.Annotations[OutputURLAnnotation]
	if url == "" {
		return
	}
	summary.Output = &consolev1.DeploymentOutput{Url: url}
}

// mergeAggregatedLinksAnnotation populates summary.Output.Links and (when
// promoted) summary.Output.Url from the cached AnnotationAggregatedLinks
// JSON blob on cm. Sibling of mergeOutputURLAnnotation: the two helpers are
// invoked in sequence so the legacy `output-url` cache fills the URL when
// no `primary-url` was promoted, and the link aggregator extends the same
// Output with `links`. Safe to call after mergeOutputURLAnnotation; it
// allocates Output lazily when the prior helper did not.
func mergeAggregatedLinksAnnotation(summary *consolev1.DeploymentStatusSummary, cm *corev1.ConfigMap) {
	if summary == nil || cm == nil {
		return
	}
	links, primaryURL := deserializeAggregatedLinks(cm)
	applyAggregatedLinks(summary, links, primaryURL)
}

// refreshAggregatedLinksCache scans every owned resource for the deployment,
// re-derives the aggregated link set, and rewrites the cached annotation on
// the deployment ConfigMap when it disagrees with the fresh scan. This is
// the GetDeployment-time refresh path that lets resources annotated out-of-
// band (after the last render) surface without requiring a re-render. It is
// best-effort: any scan/write failure is logged and the previously cached
// state is returned unchanged so a transient API error never blocks the
// read RPC. Returns the (links, primaryURL) the caller should use when
// populating the wire DeploymentOutput.
//
// When the K8sClient has no dynamic client configured (local/dev wiring),
// the cache is treated as authoritative: there is no way to scan, so a
// preserve-cache policy avoids wiping legitimate cached metadata simply
// because the cluster is unreachable. When a dynamic client IS configured
// and the scan returns zero resources, that is a meaningful "no links"
// signal — the aggregator clears any stale cached entries so deletions
// or annotation removals propagate without requiring a re-render.
//
// Partial-scan handling: if `ListDeploymentResources` returns an error
// wrapping `ErrPartialScan`, at least one per-kind list failed and the
// returned slice is incomplete. The fresh aggregation is therefore not
// authoritative — the cache is preserved so a transient API error or a
// missing optional CRD never silently wipes legitimate cached links
// (HOL-574 review round 2 P1).
func (h *Handler) refreshAggregatedLinksCache(ctx context.Context, project, name string, cm *corev1.ConfigMap) ([]*consolev1.Link, string) {
	cachedLinks, cachedPrimary := deserializeAggregatedLinks(cm)
	if !h.k8s.HasDynamicClient() {
		// No way to scan; keep serving the cache so the dev/local
		// path (no cluster) still surfaces previously-stamped links.
		return cachedLinks, cachedPrimary
	}
	resources, err := h.k8s.ListDeploymentResources(ctx, project, name)
	if err != nil {
		// Partial-scan errors mean some kinds were not observed; the
		// returned slice is therefore not a faithful view of the
		// cluster. Preserve the cache rather than wiping it on what
		// might be a transient or RBAC-shaped failure. Total scan
		// failures (no slice at all) take the same path.
		level := slog.LevelWarn
		if errors.Is(err, ErrPartialScan) {
			level = slog.LevelDebug
		}
		slog.Log(ctx, level, "could not fully list owned resources for link refresh; using cached set",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
		return cachedLinks, cachedPrimary
	}
	freshLinks, freshPrimary := aggregateLinksFromResources(ctx, project, name, resources)
	if linksEqual(cachedLinks, freshLinks) && cachedPrimary == freshPrimary {
		return cachedLinks, cachedPrimary
	}
	// Drift detected — empty fresh result clears the annotation; a
	// non-empty fresh result overwrites it. Either way the cache
	// converges on what the live cluster actually says.
	payload := serializeAggregatedLinks(ctx, project, name, freshLinks, freshPrimary)
	if err := h.k8s.SetAggregatedLinksAnnotation(ctx, project, name, payload); err != nil {
		slog.WarnContext(ctx, "failed to update aggregated links cache after drift",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
	}
	return freshLinks, freshPrimary
}

// stampAggregatedLinks scans every owned resource for the deployment after
// a successful Apply/Reconcile, derives the aggregated link set, and writes
// it to the deployment ConfigMap as `console.holos.run/links`. Mirrors the
// SetOutputURLAnnotation precedent on the create/update write path: a
// failure here is logged at warn but does not fail the RPC because the
// deployment itself was applied successfully.
func (h *Handler) stampAggregatedLinks(ctx context.Context, project, name string) {
	resources, err := h.k8s.ListDeploymentResources(ctx, project, name)
	if err != nil {
		slog.WarnContext(ctx, "failed to list owned resources for aggregated links cache",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
		return
	}
	aggregated, primaryURL := aggregateLinksFromResources(ctx, project, name, resources)
	payload := serializeAggregatedLinks(ctx, project, name, aggregated, primaryURL)
	if err := h.k8s.SetAggregatedLinksAnnotation(ctx, project, name, payload); err != nil {
		slog.WarnContext(ctx, "failed to set aggregated links annotation after apply",
			slog.String("project", project),
			slog.String("name", name),
			slog.Any("error", err),
		)
	}
}

// checkProjectAccess verifies that the user has the given permission via project cascade grants.
func (h *Handler) checkProjectAccess(ctx context.Context, claims *rpc.Claims, project string, permission rbac.Permission) error {
	if h.projectResolver == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	users, roles, err := h.projectResolver.GetProjectGrants(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project grants",
			slog.String("project", project),
			slog.Any("error", err),
		)
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
	}
	return rbac.CheckCascadeAccess(claims.Email, claims.Roles, users, roles, permission, rbac.ProjectCascadeDeploymentPerms)
}

// serializeUnstructured converts a slice of unstructured Kubernetes resources
// into a multi-document YAML string (separated by "---\n") and a JSON array
// string. Returns an empty YAML string and "[]" for an empty or nil slice so
// that JSON fields are always valid parseable JSON arrays.
func serializeUnstructured(resources []unstructured.Unstructured) (yamlStr, jsonStr string) {
	if len(resources) == 0 {
		return "", "[]"
	}
	var buf strings.Builder
	objects := make([]map[string]any, 0, len(resources))
	for i, r := range resources {
		if i > 0 {
			buf.WriteString("---\n")
		}
		yamlBytes, yamlErr := yaml.Marshal(r.Object)
		if yamlErr == nil {
			buf.WriteString(string(yamlBytes))
		}
		if r.Object != nil {
			objects = append(objects, r.Object)
		}
	}
	jsonBytes, jsonErr := json.MarshalIndent(objects, "", "  ")
	if jsonErr == nil {
		jsonStr = string(jsonBytes)
	}
	return buf.String(), jsonStr
}

// validateDeploymentName checks that the name is a valid DNS label.
func validateDeploymentName(name string) error {
	if name == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if len(name) > 63 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be at most 63 characters"))
	}
	if !dnsLabelRe.MatchString(name) {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name must be a valid DNS label (lowercase alphanumeric and hyphens, starting with a letter)"))
	}
	return nil
}

// configMapToDeployment converts a Kubernetes ConfigMap to a Deployment protobuf message.
func configMapToDeployment(cm *corev1.ConfigMap, project string) *consolev1.Deployment {
	dep := &consolev1.Deployment{
		Name:        cm.Name,
		Project:     project,
		Image:       cm.Data[ImageKey],
		Tag:         cm.Data[TagKey],
		Template:    cm.Data[TemplateKey],
		DisplayName: cm.Annotations[v1alpha2.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha2.AnnotationDescription],
		Command:     commandFromConfigMap(cm),
		Args:        argsFromConfigMap(cm),
		Env:         envFromConfigMap(cm),
		CreatedAt:   cm.ObjectMeta.CreationTimestamp.UTC().Format(time.RFC3339),
	}
	if raw, ok := cm.Data[PortKey]; ok && raw != "" {
		if p, err := strconv.ParseInt(raw, 10, 32); err == nil {
			dep.Port = int32(p)
		}
	}
	return dep
}

// commandFromConfigMap reads the JSON-encoded command slice from a ConfigMap.
func commandFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, CommandKey)
}

// argsFromConfigMap reads the JSON-encoded args slice from a ConfigMap.
func argsFromConfigMap(cm *corev1.ConfigMap) []string {
	return stringSliceFromConfigMap(cm, ArgsKey)
}

// envFromConfigMapAsV1alpha2 reads the JSON-encoded env vars from a ConfigMap as v1alpha2.EnvVar slice.
func envFromConfigMapAsV1alpha2(cm *corev1.ConfigMap) []v1alpha2.EnvVar {
	raw, ok := cm.Data[EnvKey]
	if !ok || raw == "" {
		return nil
	}
	var inputs []v1alpha2.EnvVar
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil
	}
	return inputs
}

// envFromConfigMap reads the JSON-encoded env vars from a ConfigMap and converts them to proto messages.
func envFromConfigMap(cm *corev1.ConfigMap) []*consolev1.EnvVar {
	raw, ok := cm.Data[EnvKey]
	if !ok || raw == "" {
		return nil
	}
	var inputs []v1alpha2.EnvVar
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil
	}
	result := make([]*consolev1.EnvVar, 0, len(inputs))
	for _, e := range inputs {
		result = append(result, envVarToProto(e))
	}
	return result
}

// envVarToProto converts a v1alpha2.EnvVar to a proto EnvVar message.
func envVarToProto(e v1alpha2.EnvVar) *consolev1.EnvVar {
	ev := &consolev1.EnvVar{Name: e.Name}
	switch {
	case e.SecretKeyRef != nil:
		ev.Source = &consolev1.EnvVar_SecretKeyRef{
			SecretKeyRef: &consolev1.SecretKeyRef{Name: e.SecretKeyRef.Name, Key: e.SecretKeyRef.Key},
		}
	case e.ConfigMapKeyRef != nil:
		ev.Source = &consolev1.EnvVar_ConfigMapKeyRef{
			ConfigMapKeyRef: &consolev1.ConfigMapKeyRef{Name: e.ConfigMapKeyRef.Name, Key: e.ConfigMapKeyRef.Key},
		}
	default:
		ev.Source = &consolev1.EnvVar_Value{Value: e.Value}
	}
	return ev
}

// protoToEnvVar converts a proto EnvVar message to a v1alpha2.EnvVar.
func protoToEnvVar(e *consolev1.EnvVar) v1alpha2.EnvVar {
	input := v1alpha2.EnvVar{Name: e.GetName()}
	switch src := e.GetSource().(type) {
	case *consolev1.EnvVar_SecretKeyRef:
		if src.SecretKeyRef != nil {
			input.SecretKeyRef = &v1alpha2.KeyRef{Name: src.SecretKeyRef.GetName(), Key: src.SecretKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_ConfigMapKeyRef:
		if src.ConfigMapKeyRef != nil {
			input.ConfigMapKeyRef = &v1alpha2.KeyRef{Name: src.ConfigMapKeyRef.GetName(), Key: src.ConfigMapKeyRef.GetKey()}
		}
	case *consolev1.EnvVar_Value:
		input.Value = src.Value
	}
	return input
}

// validateEnvVars validates a list of proto EnvVar messages and converts them to v1alpha2.EnvVar.
// Returns an error if any env var has an empty name.
func validateEnvVars(envVars []*consolev1.EnvVar) ([]v1alpha2.EnvVar, error) {
	if len(envVars) == 0 {
		return nil, nil
	}
	result := make([]v1alpha2.EnvVar, 0, len(envVars))
	for _, e := range envVars {
		if e.GetName() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("env var name must not be empty"))
		}
		result = append(result, protoToEnvVar(e))
	}
	return result, nil
}

// portFromConfigMap reads the port integer from a ConfigMap data key.
func portFromConfigMap(cm *corev1.ConfigMap) int {
	raw, ok := cm.Data[PortKey]
	if !ok || raw == "" {
		return 0
	}
	p, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0
	}
	return int(p)
}

// defaultPort returns port if non-zero, otherwise returns 8080.
func defaultPort(port int) int {
	if port == 0 {
		return 8080
	}
	return port
}

// resolveGatewayNamespace returns the gateway namespace to inject into
// PlatformInput for the given project. It consults the configured
// OrganizationGatewayResolver (HOL-644) and falls back to
// DefaultGatewayNamespace when no resolver is wired, when the resolver
// errors, or when the org has no override annotation. Errors are logged at
// WARN — they MUST NOT fail the render, since a transient lookup miss
// should degrade to the historical hard-coded default rather than reject a
// deployment.
func (h *Handler) resolveGatewayNamespace(ctx context.Context, project string) string {
	if h.gatewayResolver == nil {
		return DefaultGatewayNamespace
	}
	gwNs, err := h.gatewayResolver.GetGatewayNamespace(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "could not resolve org gateway namespace, falling back to default",
			slog.String("project", project),
			slog.String("default", DefaultGatewayNamespace),
			slog.Any("error", err),
		)
		return DefaultGatewayNamespace
	}
	if gwNs == "" {
		return DefaultGatewayNamespace
	}
	return gwNs
}

// buildPlatformInput constructs a v1alpha2.PlatformInput from handler context.
// When an AncestorWalker is configured, Folders is populated with the ordered
// list of folder names in the ancestor chain (org → folders → project) so CUE
// templates can reference platform.folders.
//
// GatewayNamespace resolution (HOL-526 phase 3, HOL-644):
//
//   - When an OrganizationGatewayResolver is configured AND it returns a
//     non-empty value with no error, that value is injected. This lets a
//     platform engineer pin gateway-namespace to a cluster-specific value
//     (e.g. "ci-private-apps-gateway") via the org settings UI without
//     forcing every template author to set platform.gatewayNamespace
//     explicitly — and, critically, lets a template author who DOES set it
//     unify cleanly with the backend value (no CUE conflict on string :
//     "X" & string : "Y").
//   - When the resolver is nil, errors, or returns empty, the value falls
//     back to DefaultGatewayNamespace ("istio-ingress") so legacy clusters
//     and unconfigured test wiring keep working unchanged.
func (h *Handler) buildPlatformInput(ctx context.Context, project, namespace string, claims *rpc.Claims) v1alpha2.PlatformInput {
	pi := v1alpha2.PlatformInput{
		Project:          project,
		Namespace:        namespace,
		GatewayNamespace: h.resolveGatewayNamespace(ctx, project),
	}
	if claims != nil {
		pi.Claims = v1alpha2.Claims{
			Iss:           claims.Iss,
			Sub:           claims.Sub,
			Exp:           claims.Exp,
			Iat:           claims.Iat,
			Email:         claims.Email,
			EmailVerified: claims.EmailVerified,
			Name:          claims.Name,
			Groups:        claims.Roles,
		}
	}
	if h.ancestorWalker != nil {
		folders, err := h.ancestorWalker.GetProjectFolders(ctx, project)
		if err != nil {
			slog.WarnContext(ctx, "could not resolve folder ancestry for platform input",
				slog.String("project", project),
				slog.Any("error", err),
			)
		} else {
			folderInfos := make([]v1alpha2.FolderInfo, 0, len(folders))
			for _, name := range folders {
				folderInfos = append(folderInfos, v1alpha2.FolderInfo{Name: name})
			}
			pi.Folders = folderInfos
		}
	}
	return pi
}


// stringSliceFromConfigMap decodes a JSON string slice from the given ConfigMap data key.
func stringSliceFromConfigMap(cm *corev1.ConfigMap, key string) []string {
	raw, ok := cm.Data[key]
	if !ok || raw == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}
	return result
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors.
func mapK8sError(err error) error {
	if k8serrors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if k8serrors.IsAlreadyExists(err) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if k8serrors.IsForbidden(err) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// GetDeploymentPolicyState returns the full TemplatePolicy drift snapshot for
// a deployment. When no PolicyDriftChecker is wired (dev/local bootstrap
// without a cluster policy resolver), the response carries an empty
// PolicyState with has_applied_state=false and drift=false so clients can
// round-trip the RPC without special-casing missing wiring.
//
// Introduced in HOL-567 as the single source of truth for "is this deployment
// drifted from policy?" — the DeploymentStatusSummary.policy_drift bool on
// list responses is derived from the same checker so the two surfaces never
// disagree.
func (h *Handler) GetDeploymentPolicyState(
	ctx context.Context,
	req *connect.Request[consolev1.GetDeploymentPolicyStateRequest],
) (*connect.Response[consolev1.GetDeploymentPolicyStateResponse], error) {
	project := req.Msg.GetProject()
	name := req.Msg.GetName()
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}
	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	if _, err := h.k8s.GetDeployment(ctx, project, name); err != nil {
		return nil, mapK8sError(err)
	}

	if h.policyDriftChecker == nil {
		return connect.NewResponse(&consolev1.GetDeploymentPolicyStateResponse{
			State: &consolev1.PolicyState{},
		}), nil
	}
	state, err := h.policyDriftChecker.PolicyState(ctx, project, name)
	if err != nil {
		slog.WarnContext(ctx, "policy state computation failed",
			slog.String("project", project),
			slog.String("deployment", name),
			slog.Any("error", err),
		)
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&consolev1.GetDeploymentPolicyStateResponse{State: state}), nil
}

// PreflightCheck is a planning-time validation RPC that surfaces sibling-
// Deployment name collisions and versionConstraint conflicts before the user
// clicks Apply.  It is safe to call at any time and MUST NOT mutate cluster
// state.
//
// Introduced in HOL-962 as part of Phase 8 of the deployment-dependencies
// plan (ADR 035).
func (h *Handler) PreflightCheck(
	ctx context.Context,
	req *connect.Request[consolev1.PreflightCheckRequest],
) (*connect.Response[consolev1.PreflightCheckResponse], error) {
	project := req.Msg.GetProject()
	if project == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project is required"))
	}
	if len(req.Msg.GetPlannedDeployments()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("planned_deployments must not be empty"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}
	if err := h.checkProjectAccess(ctx, claims, project, rbac.PermissionDeploymentsRead); err != nil {
		return nil, err
	}

	// Fetch the existing deployment ConfigMaps from the project namespace so we
	// can detect name collisions without mutating anything.
	existingCMs, err := h.k8s.ListDeployments(ctx, project)
	if err != nil {
		return nil, mapK8sError(err)
	}

	// Build a name-set from the existing deployments.
	existingNames := make(map[string]bool, len(existingCMs))
	for i := range existingCMs {
		existingNames[existingCMs[i].Name] = true
	}

	planned := req.Msg.GetPlannedDeployments()

	// Run the two independent checks.
	collisions := DetectCollisions(planned, existingNames)
	versionConflicts := DetectVersionConflicts(planned)

	slog.InfoContext(ctx, "preflight check complete",
		slog.String("project", project),
		slog.String("sub", claims.Sub),
		slog.Int("planned", len(planned)),
		slog.Int("collisions", len(collisions)),
		slog.Int("version_conflicts", len(versionConflicts)),
	)

	return connect.NewResponse(&consolev1.PreflightCheckResponse{
		Collisions:       collisions,
		VersionConflicts: versionConflicts,
	}), nil
}

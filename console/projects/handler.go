package projects

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "project"

// ProjectNamespacePipelineOutcome is what a ProjectNamespacePipeline
// implementation tells the handler about the work it did. The concrete
// implementation lives in console/projects/projectnspipeline — the
// interface is defined here so the handler package does not depend on
// projectnspipeline (which transitively imports console/templates,
// console/deployments, and would collide with the deployments test
// suite that imports console/projects).
type ProjectNamespacePipelineOutcome int

const (
	// ProjectNamespacePipelineNoBindings mirrors
	// projectnspipeline.OutcomeNoBindings: no bindings matched, the
	// handler must run its existing Namespace-create path.
	ProjectNamespacePipelineNoBindings ProjectNamespacePipelineOutcome = iota
	// ProjectNamespacePipelineBindingsApplied mirrors
	// projectnspipeline.OutcomeBindingsApplied: at least one binding
	// matched and the applier completed the three-group SSA pipeline,
	// so the handler must NOT also call CreateProject on the typed k8s
	// client.
	ProjectNamespacePipelineBindingsApplied
)

// ProjectNamespacePipelineInput carries the per-RPC values a
// ProjectNamespacePipeline needs. Matches the shape of
// projectnspipeline.Input — an adapter in console/console.go converts
// between the two at wire-up time.
type ProjectNamespacePipelineInput struct {
	// ProjectName is the slug of the project being created.
	ProjectName string
	// ParentNamespace is the ancestor namespace the resolver walks from.
	ParentNamespace string
	// BaseNamespace is the Namespace object the RPC built; the applier
	// uses it as the "base" the render path unifies template-produced
	// Namespace patches into (ADR 034 Decision 1).
	BaseNamespace *corev1.Namespace
	// Platform is the platform-input block the renderer binds at the
	// CUE `platform` path.
	Platform v1alpha2.PlatformInput
}

// ProjectNamespacePipeline is the seam the handler talks to when
// CreateProject runs. The concrete implementation is
// projectnspipeline.Pipeline; a nil value leaves the handler on its
// existing Namespace-create path unchanged.
type ProjectNamespacePipeline interface {
	Run(ctx context.Context, in ProjectNamespacePipelineInput) (ProjectNamespacePipelineOutcome, error)
}

// OrgResolver resolves organization-level grants for access checks.
type OrgResolver interface {
	GetOrgGrants(ctx context.Context, org string) (users, roles map[string]string, err error)
}

// OrgDefaultShareResolver is an optional interface that an OrgResolver can also
// implement to provide default sharing grants from the organization.  These
// defaults are merged into new projects created within the organization.
type OrgDefaultShareResolver interface {
	GetOrgDefaultGrants(ctx context.Context, org string) (defaultUsers, defaultRoles []secrets.AnnotationGrant, err error)
}

// Handler implements the ProjectService.
type Handler struct {
	consolev1connect.UnimplementedProjectServiceHandler
	k8s         *K8sClient
	orgResolver OrgResolver
	// projectNSPipeline wires the HOL-812 resolve → render → apply flow.
	// Nil (the default) falls through to the existing Namespace-create
	// path so CreateProject keeps working during bootstrap or in tests
	// that do not care about the ProjectNamespace feature. The concrete
	// implementation lives in
	// console/projects/projectnspipeline.Pipeline — an interface seam
	// keeps the handler free of that package's transitive dependency on
	// console/templates (which would form an import cycle with the
	// deployments tests that import console/projects).
	projectNSPipeline ProjectNamespacePipeline
}

// NewHandler creates a new ProjectService handler.
func NewHandler(k8s *K8sClient, orgResolver OrgResolver) *Handler {
	return &Handler{k8s: k8s, orgResolver: orgResolver}
}

// WithProjectNamespacePipeline wires the HOL-812 resolve → render →
// apply pipeline into CreateProject. Passing a nil interface value
// leaves the handler on the existing Namespace-create path. Callers
// that want to "turn off" the pipeline should pass literal nil, not a
// typed nil value — a typed nil stored in an interface tests as
// non-nil at the interface level and would cause the handler to
// invoke .Run on a nil receiver.
func (h *Handler) WithProjectNamespacePipeline(p ProjectNamespacePipeline) *Handler {
	h.projectNSPipeline = p
	return h
}

// ListProjects returns all projects the user has access to.
func (h *Handler) ListProjects(
	ctx context.Context,
	req *connect.Request[consolev1.ListProjectsRequest],
) (*connect.Response[consolev1.ListProjectsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Resolve parent namespace filter when parent_type+parent_name are set.
	var parentNs string
	if req.Msg.ParentType != consolev1.ParentType_PARENT_TYPE_UNSPECIFIED && req.Msg.ParentName != "" {
		var err error
		parentNs, err = h.resolveParentNS(req.Msg.ParentType, req.Msg.ParentName)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	allProjects, err := h.k8s.ListProjects(ctx, req.Msg.Organization, parentNs)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Project
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

		// Check project-level grants
		if err := CheckProjectListAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsList); err != nil {
				continue
			}
		}

		userRole := h.bestRoleWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, ns)
		result = append(result, h.buildProject(ns, shareUsers, shareRoles, userRole))
	}

	slog.InfoContext(ctx, "projects listed",
		slog.String("action", "project_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Organization),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(result)),
	)

	return connect.NewResponse(&consolev1.ListProjectsResponse{
		Projects: result,
	}), nil
}

// GetProject retrieves a project by name.
func (h *Handler) GetProject(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectRequest],
) (*connect.Response[consolev1.GetProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsRead); err != nil {
		slog.WarnContext(ctx, "project access denied",
			slog.String("action", "project_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := h.bestRoleWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, ns)

	slog.InfoContext(ctx, "project accessed",
		slog.String("action", "project_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetProjectResponse{
		Project: h.buildProject(ns, shareUsers, shareRoles, userRole),
	}), nil
}

// CreateProject creates a new project.
// When name is empty, it is derived from display_name using slug generation.
// Uses bounded retry with random suffix on namespace collision.
func (h *Handler) CreateProject(
	ctx context.Context,
	req *connect.Request[consolev1.CreateProjectRequest],
) (*connect.Response[consolev1.CreateProjectResponse], error) {
	// Derive name from display_name when not explicitly provided.
	name := req.Msg.Name
	autoGenerated := name == ""
	prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.ProjectPrefix
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.k8s.NamespaceExists(ctx, nsName)
	}
	if autoGenerated {
		if req.Msg.DisplayName == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name or display_name is required"))
		}
		generated, err := v1alpha2.GenerateIdentifier(ctx, req.Msg.DisplayName, prefix, exists)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generating project identifier: %w", err))
		}
		name = generated
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Resolve the immediate parent namespace.
	// When no explicit parent is specified, check the org's default folder.
	// Falls back to org root if no default folder is configured or the folder is missing.
	parentName := req.Msg.ParentName
	parentType := req.Msg.ParentType
	if parentName == "" {
		parentName, parentType = h.resolveDefaultParent(ctx, req.Msg.Organization)
	}
	if parentType == consolev1.ParentType_PARENT_TYPE_UNSPECIFIED {
		parentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}
	parentNs, err := h.resolveParentNS(parentType, parentName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("resolving parent: %w", err))
	}

	// Check create access: user must be owner on at least one existing project
	// or have owner grant on the organization
	allProjects, err := h.k8s.ListProjects(ctx, "", "")
	if err != nil {
		return nil, mapK8sError(err)
	}
	if err := CheckProjectCreateAccess(claims.Email, claims.Roles, allProjects); err != nil {
		// Fall back to org-level grants for create permission
		orgUsers, orgRoles := h.resolveOrgGrants(ctx, req.Msg.Organization)
		if err := rbac.CheckAccessGrants(claims.Email, claims.Roles, orgUsers, orgRoles, rbac.PermissionProjectsCreate); err != nil {
			slog.WarnContext(ctx, "project create denied",
				slog.String("action", "project_create_denied"),
				slog.String("resource_type", auditResourceType),
				slog.String("project", name),
				slog.String("organization", req.Msg.Organization),
				slog.String("sub", claims.Sub),
				slog.String("email", claims.Email),
			)
			return nil, err
		}
	}

	// Convert proto grants to annotation grants
	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	// Merge organization-level default sharing grants (if the resolver supports it).
	// Request-supplied grants override defaults for the same principal via DeduplicateGrants
	// (highest role wins, so explicit grants at a higher role shadow lower defaults).
	// Also copy org defaults as the project's default sharing so new secrets inherit them.
	var defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant
	if req.Msg.Organization != "" {
		if ds, ok := h.orgResolver.(OrgDefaultShareResolver); ok {
			orgDefaultUsers, orgDefaultRoles, err := ds.GetOrgDefaultGrants(ctx, req.Msg.Organization)
			if err != nil {
				slog.WarnContext(ctx, "failed to resolve org default grants, proceeding without them",
					slog.String("organization", req.Msg.Organization),
					slog.Any("error", err),
				)
			} else {
				// Concatenate: request grants first so they win over defaults for the same principal.
				shareUsers = secrets.DeduplicateGrants(append(shareUsers, orgDefaultUsers...))
				shareRoles = secrets.DeduplicateGrants(append(shareRoles, orgDefaultRoles...))
				// Copy org defaults as project defaults so new secrets also inherit them.
				defaultShareUsers = orgDefaultUsers
				defaultShareRoles = orgDefaultRoles
			}
		}
	}

	// Keep the legacy project metadata grant email-shaped for UI display, but
	// bind the RBAC owner to the stable OIDC subject per ADR 036.
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)
	rbacShareUsers := removeGrantPrincipal(shareUsers, claims.Email)
	if claims.Sub != "" {
		rbacShareUsers = ensureCreatorOwner(rbacShareUsers, claims.Sub)
	}

	// Create the project namespace, retrying on AlreadyExists when the name was
	// auto-generated (race between GenerateIdentifier check and K8s create).
	// HOL-812 wires the ProjectNamespace pipeline (resolve → render → apply)
	// inside the same retry loop so the auto-generated-name retry behavior
	// covers both paths (existing typed Create and SSA-via-applier) with a
	// single branch.
	const maxCreateRetries = 3
	for attempt := range maxCreateRetries + 1 {
		err = h.createProjectOnce(ctx, name, req.Msg, parentNs, claims.Email, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles, rbacShareUsers)
		if err == nil {
			break
		}
		if !autoGenerated || !errors.IsAlreadyExists(err) || attempt >= maxCreateRetries {
			return nil, mapProjectCreateError(err)
		}
		slog.InfoContext(ctx, "project create race detected, regenerating identifier",
			slog.String("resource_type", auditResourceType),
			slog.String("collided_name", name),
			slog.Int("attempt", attempt+1),
		)
		name, err = v1alpha2.GenerateIdentifier(ctx, req.Msg.DisplayName, prefix, exists)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("regenerating project identifier: %w", err))
		}
	}

	slog.InfoContext(ctx, "project created",
		slog.String("action", "project_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", name),
		slog.String("organization", req.Msg.Organization),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateProjectResponse{
		Name: name,
	}), nil
}

// createProjectOnce executes one attempt of the project-namespace
// creation path: runs the HOL-812 ProjectNamespace pipeline when
// configured, and falls through to the typed CreateProject call when
// the pipeline returns OutcomeNoBindings (including the nil-pipeline
// default). Separated from CreateProject so the retry loop wraps both
// sides uniformly.
//
// Returns the raw error (not a connect.Error); the caller maps it via
// mapProjectCreateError so the AlreadyExists retry-on-auto-generated-name
// logic can still use errors.IsAlreadyExists. The only error category
// mapProjectCreateError handles that mapK8sError does not is the
// deadline-exceeded case — the applier's DeadlineExceededError matches
// [context.DeadlineExceeded] via errors.Is, and mapProjectCreateError
// translates that to connect.CodeDeadlineExceeded.
func (h *Handler) createProjectOnce(
	ctx context.Context,
	name string,
	msg *consolev1.CreateProjectRequest,
	parentNs string,
	creatorEmail string,
	shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant,
	rbacShareUsers []secrets.AnnotationGrant,
) error {
	// Always build the base Namespace object up front — both paths need
	// it (the typed Create call still uses it, and the pipeline needs
	// it as the "base" the render path unifies into per ADR 034).
	baseNs, err := h.k8s.BuildProjectNamespace(name, msg.DisplayName, msg.Description, msg.Organization, parentNs, creatorEmail, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	if err != nil {
		return err
	}

	// Pipeline (HOL-812): resolve ProjectNamespace bindings; if any
	// match, render and apply via SSA. A nil pipeline falls through to
	// the existing Namespace-create path so CreateProject keeps
	// working during bootstrap or in tests that do not care about the
	// ProjectNamespace feature.
	if h.projectNSPipeline != nil {
		outcome, pipeErr := h.projectNSPipeline.Run(ctx, ProjectNamespacePipelineInput{
			ProjectName:     name,
			ParentNamespace: parentAncestorNamespace(parentNs),
			BaseNamespace:   baseNs,
			Platform:        h.buildPlatformInput(ctx, msg, name, baseNs.Name),
		})
		if pipeErr != nil {
			return pipeErr
		}
		if outcome == ProjectNamespacePipelineBindingsApplied {
			// The applier already SSA'd the Namespace and every
			// associated resource. The RBAC objects below are a
			// console-owned elevation path from ADR 036 Decision 5,
			// so they are reconciled explicitly after the namespace
			// exists rather than rendered as the impersonated user.
			// Note this path does not surface AlreadyExists (SSA is
			// idempotent), so the auto-generated-name retry is a
			// no-op when the pipeline handles the create.
			return h.k8s.EnsureProjectSecretRBACForNamespace(ctx, baseNs.Name, namespaceOwnerRefs(baseNs), rbacShareUsers, shareRoles)
		}
	}

	// Existing path: typed Create. Unchanged from pre-HOL-812 behavior.
	_, err = h.k8s.client.CoreV1().Namespaces().Create(ctx, baseNs, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return h.k8s.EnsureProjectSecretRBAC(ctx, name, rbacShareUsers, shareRoles)
}

// parentAncestorNamespace returns the namespace the ProjectNamespace
// resolver walks from. For the HOL-812 scope this is the immediate
// parent namespace the RPC already resolved (an organization or folder
// namespace). A future folders-with-depth or org-hierarchy feature
// may need to rewrite this to follow a different chain — ADR 034's
// "ancestor-chain open question" is tracked against HOL-806 Phase 7.
// Centralising the computation here keeps the fix to one call site.
func parentAncestorNamespace(parentNs string) string { return parentNs }

// buildPlatformInput assembles the PlatformInput block the
// ProjectNamespace render path binds at the CUE `platform` path. The
// shape mirrors what the deployment render path produces for the same
// project (deployments/handler.go), minus per-deployment fields.
//
// GatewayNamespace and the folder chain are intentionally omitted at
// this phase. ADR 034 defers per-org gateway resolution and the
// folders-with-depth walk to HOL-806 Phase 7; when that lands, this
// helper is the one place to add them so the call site in
// createProjectOnce stays a one-liner.
func (h *Handler) buildPlatformInput(ctx context.Context, msg *consolev1.CreateProjectRequest, projectName, namespaceName string) v1alpha2.PlatformInput {
	claims := rpc.ClaimsFromContext(ctx)
	input := v1alpha2.PlatformInput{
		Project:      projectName,
		Namespace:    namespaceName,
		Organization: msg.Organization,
	}
	if claims != nil {
		input.Claims = v1alpha2.Claims{
			Iss:           claims.Iss,
			Sub:           claims.Sub,
			Exp:           claims.Exp,
			Iat:           claims.Iat,
			Email:         claims.Email,
			EmailVerified: claims.EmailVerified,
			Name:          claims.Name,
		}
	}
	return input
}

// mapProjectCreateError maps a CreateProject-layer error to a connect
// error, widening mapK8sError with the deadline-exceeded case the
// HOL-812 apply path may return. The projectapply package's structured
// DeadlineExceededError implements `Is(target error) bool` to match
// [context.DeadlineExceeded] — we match via errors.Is rather than a
// type assertion so this package can stay free of a projectapply
// import. The projectapply -> templates -> deployments chain otherwise
// collides with deployments tests that already import projects.
// Any non-deadline error falls through to mapK8sError (which classifies
// apierrors.Is*() kinds and maps the "not managed by" string into
// CodeNotFound).
func mapProjectCreateError(err error) error {
	if stderrors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return mapK8sError(err)
}

// UpdateProject updates project metadata.
func (h *Handler) UpdateProject(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectRequest],
) (*connect.Response[consolev1.UpdateProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsWrite); err != nil {
		slog.WarnContext(ctx, "project update denied",
			slog.String("action", "project_update_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	// Handle reparenting if parent_type and parent_name are set.
	if req.Msg.ParentType != nil && req.Msg.ParentName != nil {
		if err := h.reparentProject(ctx, ns, claims, *req.Msg.ParentType, *req.Msg.ParentName); err != nil {
			return nil, err
		}
	}

	// Only issue a K8s write when metadata fields are provided; skip when the
	// request is a reparent-only operation (or a no-op same-parent reparent).
	if req.Msg.DisplayName != nil || req.Msg.Description != nil {
		if _, err := h.k8s.UpdateProject(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
			return nil, mapK8sError(err)
		}
	}

	slog.InfoContext(ctx, "project updated",
		slog.String("action", "project_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateProjectResponse{}), nil
}

// reparentProject validates and executes a project reparent operation.
// Checks PERMISSION_REPARENT on both source and destination parents.
// Projects have no children so no depth or cycle checks are needed.
func (h *Handler) reparentProject(
	ctx context.Context,
	ns *corev1.Namespace,
	claims *rpc.Claims,
	newParentType consolev1.ParentType,
	newParentName string,
) error {
	projectName := ns.Labels[v1alpha2.LabelProject]
	org := GetOrganization(ns)

	// Resolve new parent namespace.
	newParentNs, err := h.resolveParentNS(newParentType, newParentName)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Resolve current parent namespace from the project's label.
	currentParentNs := ns.Labels[v1alpha2.AnnotationParent]
	if currentParentNs == "" {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("project %q is missing parent label", projectName))
	}

	// No-op: same parent, return success without a K8s write.
	if currentParentNs == newParentNs {
		return nil
	}

	// Verify new parent namespace exists.
	newParentNamespace, err := h.k8s.GetNamespace(ctx, newParentNs)
	if err != nil {
		return mapK8sError(err)
	}

	// Check PERMISSION_REPARENT on the current (source) parent.
	sourceNs, err := h.k8s.GetNamespace(ctx, currentParentNs)
	if err != nil {
		return mapK8sError(err)
	}
	if err := h.checkReparentAccess(ctx, claims, sourceNs, org, "source"); err != nil {
		slog.WarnContext(ctx, "project reparent denied on source",
			slog.String("action", "project_reparent_denied_source"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", projectName),
			slog.String("source_parent", currentParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Check PERMISSION_REPARENT on the new (destination) parent.
	// Use the destination parent's org for cascade, not the source org.
	destOrg := newParentNamespace.Labels[v1alpha2.LabelOrganization]
	if err := h.checkReparentAccess(ctx, claims, newParentNamespace, destOrg, "destination"); err != nil {
		slog.WarnContext(ctx, "project reparent denied on destination",
			slog.String("action", "project_reparent_denied_destination"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", projectName),
			slog.String("dest_parent", newParentNs),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return err
	}

	// Execute the reparent: update the parent label.
	if _, err := h.k8s.UpdateParentLabel(ctx, projectName, newParentNs); err != nil {
		return mapK8sError(err)
	}

	slog.InfoContext(ctx, "project reparented",
		slog.String("action", "project_reparent"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", projectName),
		slog.String("from_parent", currentParentNs),
		slog.String("to_parent", newParentNs),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return nil
}

// checkReparentAccess verifies that the user has PERMISSION_REPARENT on the given
// parent namespace. Uses direct grants on the parent plus org-level cascade via
// ReparentCascadePerms.
func (h *Handler) checkReparentAccess(ctx context.Context, claims *rpc.Claims, parentNs *corev1.Namespace, org, direction string) error {
	shareUsers, _ := GetShareUsers(parentNs)
	shareRoles, _ := GetShareRoles(parentNs)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	// Check direct grants on the parent resource.
	if err := rbac.CheckAccessGrants(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionReparent); err == nil {
		return nil
	}

	// Check org-level cascade: org OWNERs get REPARENT on all children via ReparentCascadePerms.
	if org != "" {
		orgUsers, orgRoles := h.resolveOrgGrants(ctx, org)
		if err := rbac.CheckCascadeAccess(claims.Email, claims.Roles, orgUsers, orgRoles, rbac.PermissionReparent, rbac.ReparentCascadePerms); err == nil {
			return nil
		}
	}

	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: reparent authorization denied on %s parent", direction))
}

// DeleteProject deletes a managed namespace.
func (h *Handler) DeleteProject(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteProjectRequest],
) (*connect.Response[consolev1.DeleteProjectResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsDelete); err != nil {
		slog.WarnContext(ctx, "project delete denied",
			slog.String("action", "project_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if err := h.k8s.DeleteProject(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project deleted",
		slog.String("action", "project_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteProjectResponse{}), nil
}

// UpdateProjectSharing updates the sharing grants on a project.
func (h *Handler) UpdateProjectSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectSharingRequest],
) (*connect.Response[consolev1.UpdateProjectSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsAdmin); err != nil {
		slog.WarnContext(ctx, "project sharing update denied",
			slog.String("action", "project_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	updated, err := h.k8s.UpdateProjectSharing(ctx, req.Msg.Name, newShareUsers, newShareRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project sharing updated",
		slog.String("action", "project_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateProjectSharingResponse{
		Project: h.buildProject(updated, updatedUsers, updatedRoles, userRole),
	}), nil
}

// UpdateProjectDefaultSharing updates the default sharing grants on a project.
func (h *Handler) UpdateProjectDefaultSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateProjectDefaultSharingRequest],
) (*connect.Response[consolev1.UpdateProjectDefaultSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsAdmin); err != nil {
		slog.WarnContext(ctx, "project default sharing update denied",
			slog.String("action", "project_default_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newDefaultUsers := shareGrantsToAnnotations(req.Msg.DefaultUserGrants)
	newDefaultRoles := shareGrantsToAnnotations(req.Msg.DefaultRoleGrants)

	updated, err := h.k8s.UpdateProjectDefaultSharing(ctx, req.Msg.Name, newDefaultUsers, newDefaultRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "project default sharing updated",
		slog.String("action", "project_default_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedShareUsers, _ := GetShareUsers(updated)
	updatedShareRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedShareUsers, now)
	updatedActiveRoles := secrets.ActiveGrantsMap(updatedShareRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveRoles)

	return connect.NewResponse(&consolev1.UpdateProjectDefaultSharingResponse{
		Project: h.buildProject(updated, updatedShareUsers, updatedShareRoles, userRole),
	}), nil
}

// GetProjectRaw retrieves the full Kubernetes Namespace object as verbatim JSON.
func (h *Handler) GetProjectRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetProjectRawRequest],
) (*connect.Response[consolev1.GetProjectRawResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetProject(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	org := GetOrganization(ns)
	if err := h.checkAccessWithOrg(claims.Email, claims.Roles, activeUsers, activeRoles, rbac.PermissionProjectsRead); err != nil {
		slog.WarnContext(ctx, "project raw access denied",
			slog.String("action", "project_raw_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("project", req.Msg.Name),
			slog.String("organization", org),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	slog.InfoContext(ctx, "project raw accessed",
		slog.String("action", "project_raw"),
		slog.String("resource_type", auditResourceType),
		slog.String("project", req.Msg.Name),
		slog.String("organization", org),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	// Set apiVersion and kind (not populated by client-go on fetched objects)
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"

	raw, err := json.Marshal(ns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshaling namespace to JSON: %w", err))
	}

	return connect.NewResponse(&consolev1.GetProjectRawResponse{
		Raw: string(raw),
	}), nil
}

// CheckProjectIdentifier checks whether a proposed project identifier is available.
func (h *Handler) CheckProjectIdentifier(
	ctx context.Context,
	req *connect.Request[consolev1.CheckProjectIdentifierRequest],
) (*connect.Response[consolev1.CheckProjectIdentifierResponse], error) {
	if req.Msg.Identifier == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("identifier is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	prefix := h.k8s.Resolver.NamespacePrefix + h.k8s.Resolver.ProjectPrefix
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.k8s.NamespaceExists(ctx, nsName)
	}

	result, err := v1alpha2.CheckIdentifier(ctx, req.Msg.Identifier, prefix, exists)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking identifier: %w", err))
	}

	slog.InfoContext(ctx, "project identifier checked",
		slog.String("action", "project_check_identifier"),
		slog.String("resource_type", auditResourceType),
		slog.String("identifier", req.Msg.Identifier),
		slog.Bool("available", result.Available),
		slog.String("suggested", result.SuggestedIdentifier),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CheckProjectIdentifierResponse{
		Available:           result.Available,
		SuggestedIdentifier: result.SuggestedIdentifier,
	}), nil
}

// buildProject creates a Project proto message from a namespace.
func (h *Handler) buildProject(ns *corev1.Namespace, shareUsers, shareRoles []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Project {
	p := &consolev1.Project{
		UserGrants: annotationGrantsToProto(shareUsers),
		RoleGrants: annotationGrantsToProto(shareRoles),
		UserRole:   consolev1.Role(userRole),
	}

	if ns.Labels != nil {
		p.Organization = ns.Labels[v1alpha2.LabelOrganization]
		p.Name = ns.Labels[v1alpha2.LabelProject]

		// Derive parent info from the parent label.
		parentNs := ns.Labels[v1alpha2.AnnotationParent]
		if parentNs != "" {
			kind, name, err := h.k8s.Resolver.ResourceTypeFromNamespace(parentNs)
			if err == nil {
				p.ParentName = name
				switch kind {
				case v1alpha2.ResourceTypeOrganization:
					p.ParentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
				case v1alpha2.ResourceTypeFolder:
					p.ParentType = consolev1.ParentType_PARENT_TYPE_FOLDER
				}
			}
		}
	}

	// Fallback: derive project name from namespace if label is missing (pre-label namespaces)
	if p.Name == "" {
		name, err := h.k8s.Resolver.ProjectFromNamespace(ns.GetName())
		if err != nil {
			slog.Warn("project namespace missing label and prefix mismatch",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelProject),
				slog.Any("error", err),
			)
		} else {
			p.Name = name
			slog.Warn("project namespace missing label, falling back to namespace parsing",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelProject),
			)
		}
	}

	if ns.Annotations != nil {
		p.DisplayName = ns.Annotations[v1alpha2.AnnotationDisplayName]
		p.Description = ns.Annotations[v1alpha2.AnnotationDescription]
		p.CreatorEmail = ns.Annotations[v1alpha2.AnnotationCreatorEmail]
	}

	if defaultUsers, err := GetDefaultShareUsers(ns); err == nil {
		p.DefaultUserGrants = annotationGrantsToProto(defaultUsers)
	}
	if defaultRoles, err := GetDefaultShareRoles(ns); err == nil {
		p.DefaultRoleGrants = annotationGrantsToProto(defaultRoles)
	}
	p.CreatedAt = ns.CreationTimestamp.UTC().Format(time.RFC3339)

	return p
}

// resolveDefaultParent determines the default parent for a new project when
// no explicit parent is specified. It reads the org's default-folder annotation
// and, if the referenced folder exists, returns it as the parent. Otherwise
// it falls back to the organization as the parent (legacy behavior).
func (h *Handler) resolveDefaultParent(ctx context.Context, org string) (string, consolev1.ParentType) {
	if org == "" {
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	// Look up the org namespace to read the default-folder annotation.
	orgNsName := h.k8s.Resolver.OrgNamespace(org)
	orgNs, err := h.k8s.GetNamespace(ctx, orgNsName)
	if err != nil {
		slog.WarnContext(ctx, "failed to read org namespace for default folder resolution, falling back to org root",
			slog.String("organization", org),
			slog.Any("error", err),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	defaultFolder := orgNs.Annotations[v1alpha2.AnnotationDefaultFolder]
	if defaultFolder == "" {
		// No default-folder annotation — legacy org, fall back to org root.
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	// Check that the referenced folder namespace actually exists.
	folderNsName := h.k8s.Resolver.FolderNamespace(defaultFolder)
	exists, err := h.k8s.NamespaceExists(ctx, folderNsName)
	if err != nil {
		slog.WarnContext(ctx, "error checking default folder existence, falling back to org root",
			slog.String("action", "default_folder_not_found"),
			slog.String("organization", org),
			slog.String("default_folder", defaultFolder),
			slog.Any("error", err),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}
	if !exists {
		slog.WarnContext(ctx, "default folder referenced by org does not exist, falling back to org root",
			slog.String("action", "default_folder_not_found"),
			slog.String("organization", org),
			slog.String("default_folder", defaultFolder),
		)
		return org, consolev1.ParentType_PARENT_TYPE_ORGANIZATION
	}

	return defaultFolder, consolev1.ParentType_PARENT_TYPE_FOLDER
}

// resolveParentNS converts a ParentType+ParentName pair to a Kubernetes namespace name.
func (h *Handler) resolveParentNS(parentType consolev1.ParentType, parentName string) (string, error) {
	switch parentType {
	case consolev1.ParentType_PARENT_TYPE_ORGANIZATION:
		return h.k8s.Resolver.OrgNamespace(parentName), nil
	case consolev1.ParentType_PARENT_TYPE_FOLDER:
		return h.k8s.Resolver.FolderNamespace(parentName), nil
	default:
		return "", fmt.Errorf("unknown parent_type %v", parentType)
	}
}

// resolveOrgGrants returns the active grant maps for the given organization.
// Returns nil maps if no org resolver is configured or org is empty.
func (h *Handler) resolveOrgGrants(ctx context.Context, org string) (map[string]string, map[string]string) {
	if h.orgResolver == nil || org == "" {
		return nil, nil
	}
	users, roles, err := h.orgResolver.GetOrgGrants(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve org grants",
			slog.String("organization", org),
			slog.Any("error", err),
		)
		return nil, nil
	}
	return users, roles
}

// checkAccessWithOrg checks project-level grants. Organization grants do not
// cascade to project operations (see docs/adrs/007-org-grants-no-cascade.md).
func (h *Handler) checkAccessWithOrg(
	email string,
	roles []string,
	projUsers, projRoles map[string]string,
	permission rbac.Permission,
) error {
	if err := rbac.CheckAccessGrants(email, roles, projUsers, projRoles, permission); err == nil {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: authorization denied"))
}

// bestRoleWithOrg returns the best role from project grants and org grants.
func (h *Handler) bestRoleWithOrg(email string, roles []string, projUsers, projRoles map[string]string, ns *corev1.Namespace) rbac.Role {
	projRole := rbac.BestRoleFromGrants(email, roles, projUsers, projRoles)
	orgUsers, orgRoles := h.resolveOrgGrants(context.Background(), GetOrganization(ns))
	orgRole := rbac.BestRoleFromGrants(email, roles, orgUsers, orgRoles)
	if rbac.RoleLevel(orgRole) > rbac.RoleLevel(projRole) {
		return orgRole
	}
	return projRole
}

// shareGrantsToAnnotations converts proto ShareGrant slices to annotation grants.
func shareGrantsToAnnotations(grants []*consolev1.ShareGrant) []secrets.AnnotationGrant {
	result := make([]secrets.AnnotationGrant, 0, len(grants))
	for _, g := range grants {
		if g.Principal != "" {
			ag := secrets.AnnotationGrant{
				Principal: g.Principal,
				Role:      strings.ToLower(g.Role.String()[len("ROLE_"):]),
			}
			if g.Nbf != nil {
				nbf := *g.Nbf
				ag.Nbf = &nbf
			}
			if g.Exp != nil {
				exp := *g.Exp
				ag.Exp = &exp
			}
			result = append(result, ag)
		}
	}
	return secrets.DeduplicateGrants(result)
}

// annotationGrantsToProto converts annotation grants to proto ShareGrant slices.
func annotationGrantsToProto(grants []secrets.AnnotationGrant) []*consolev1.ShareGrant {
	result := make([]*consolev1.ShareGrant, 0, len(grants))
	for _, g := range grants {
		sg := &consolev1.ShareGrant{
			Principal: g.Principal,
			Role:      protoRoleFromString(g.Role),
		}
		if g.Nbf != nil {
			nbf := *g.Nbf
			sg.Nbf = &nbf
		}
		if g.Exp != nil {
			exp := *g.Exp
			sg.Exp = &exp
		}
		result = append(result, sg)
	}
	return result
}

func protoRoleFromString(s string) consolev1.Role {
	switch strings.ToLower(s) {
	case "viewer":
		return consolev1.Role_ROLE_VIEWER
	case "editor":
		return consolev1.Role_ROLE_EDITOR
	case "owner":
		return consolev1.Role_ROLE_OWNER
	default:
		return consolev1.Role_ROLE_UNSPECIFIED
	}
}

// ensureCreatorOwner ensures the creator principal is in the share-users list as owner.
func ensureCreatorOwner(shareUsers []secrets.AnnotationGrant, principal string) []secrets.AnnotationGrant {
	if principal == "" {
		return shareUsers
	}
	principalLower := strings.ToLower(principal)
	for _, g := range shareUsers {
		if strings.ToLower(g.Principal) == principalLower && strings.ToLower(g.Role) == "owner" {
			return shareUsers
		}
	}
	return append(shareUsers, secrets.AnnotationGrant{Principal: principal, Role: "owner"})
}

func removeGrantPrincipal(grants []secrets.AnnotationGrant, principal string) []secrets.AnnotationGrant {
	if principal == "" {
		return append([]secrets.AnnotationGrant(nil), grants...)
	}
	principalLower := strings.ToLower(principal)
	filtered := make([]secrets.AnnotationGrant, 0, len(grants))
	for _, grant := range grants {
		if strings.ToLower(grant.Principal) == principalLower {
			continue
		}
		filtered = append(filtered, grant)
	}
	return filtered
}

// mapK8sError converts Kubernetes API errors to ConnectRPC errors.
func mapK8sError(err error) error {
	if errors.IsNotFound(err) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if errors.IsAlreadyExists(err) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if errors.IsForbidden(err) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if errors.IsUnauthorized(err) {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if errors.IsBadRequest(err) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Check for "not managed by" errors from our K8s layer
	if strings.Contains(err.Error(), "not managed by") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

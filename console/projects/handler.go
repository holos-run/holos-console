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
	"github.com/holos-run/holos-console/console/resourcerbac"
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

	var result []*consolev1.Project
	for _, ns := range allProjects {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)

		userRole := h.effectiveRoleForNamespace(ctx, claims, ns, shareUsers, shareRoles)
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

	org := GetOrganization(ns)

	userRole := h.effectiveRoleForNamespace(ctx, claims, ns, shareUsers, shareRoles)

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

	if err := validateOrganizationProjectParent(req.Msg.ParentType, req.Msg.ParentName, req.Msg.Organization); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	parentNs := h.k8s.Resolver.OrgNamespace(req.Msg.Organization)
	if rpc.HasImpersonatedClients(ctx) {
		parentNamespace, err := h.k8s.GetNamespace(ctx, parentNs)
		if err != nil {
			return nil, mapK8sError(err)
		}
		parentShareUsers, _ := GetShareUsers(parentNamespace)
		parentShareRoles, _ := GetShareRoles(parentNamespace)
		if err := h.requireNamespaceOwner(ctx, claims, parentNamespace, parentShareUsers, parentShareRoles, "create projects"); err != nil {
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
	rbacShareUsers := secrets.RBACUserGrantsForSubjects(shareUsers, secrets.UserIdentity{Email: claims.Email, Subject: claims.Sub})
	topResourceRBACUsers := rbacShareUsers

	// Create the project namespace, retrying on AlreadyExists when the name was
	// auto-generated (race between GenerateIdentifier check and K8s create).
	// HOL-812 wires the ProjectNamespace pipeline (resolve → render → apply)
	// inside the same retry loop so the auto-generated-name retry behavior
	// covers both paths (existing typed Create and SSA-via-applier) with a
	// single branch.
	const maxCreateRetries = 3
	var err error
	for attempt := range maxCreateRetries + 1 {
		err = h.createProjectOnce(ctx, name, req.Msg, parentNs, claims.Email, claims.Sub, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles, rbacShareUsers, topResourceRBACUsers)
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
	creatorSubject string,
	shareUsers, shareRoles, defaultShareUsers, defaultShareRoles []secrets.AnnotationGrant,
	rbacShareUsers []secrets.AnnotationGrant,
	topResourceRBACUsers []secrets.AnnotationGrant,
) error {
	// Always build the base Namespace object up front — both paths need
	// it (the typed Create call still uses it, and the pipeline needs
	// it as the "base" the render path unifies into per ADR 034).
	baseNs, err := h.k8s.BuildProjectNamespace(name, msg.DisplayName, msg.Description, msg.Organization, parentNs, creatorEmail, shareUsers, shareRoles, defaultShareUsers, defaultShareRoles)
	if err != nil {
		return err
	}
	if baseNs.Annotations == nil {
		baseNs.Annotations = make(map[string]string)
	}
	if len(topResourceRBACUsers) > 0 {
		raw, err := json.Marshal(topResourceRBACUsers)
		if err != nil {
			return fmt.Errorf("marshaling rbac-share-users: %w", err)
		}
		baseNs.Annotations[v1alpha2.AnnotationRBACShareUsers] = string(raw)
	}
	if creatorSubject != "" {
		baseNs.Annotations[v1alpha2.AnnotationCreatorSubject] = creatorSubject
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
			//
			// Pass baseNs directly: the SSA applier owns the
			// authoritative namespace in the cluster, and bootstrap
			// only needs name/namespace metadata to provision Roles
			// and poll SSAR. There is no cluster Get on the SSA
			// path because tests inject a fake applier that does
			// not write the namespace into the fake clientset.
			if err := resourcerbac.BootstrapResourceRBACAndWait(ctx, h.k8s.client, h.k8s.impersonatedOrNil(ctx), baseNs, resourcerbac.Projects); err != nil {
				return err
			}
			return h.k8s.EnsureProjectSecretRBACForNamespace(ctx, baseNs.Name, namespaceOwnerRefs(baseNs), rbacShareUsers, shareRoles)
		}
	}

	// Existing path: typed Create. Unchanged from pre-HOL-812 behavior.
	created, err := h.k8s.client.CoreV1().Namespaces().Create(ctx, baseNs, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if err := resourcerbac.BootstrapResourceRBACAndWait(ctx, h.k8s.client, h.k8s.impersonatedOrNil(ctx), created, resourcerbac.Projects); err != nil {
		// Roll back the just-created namespace so we do not orphan it.
		if delErr := h.k8s.client.CoreV1().Namespaces().Delete(ctx, created.Name, metav1.DeleteOptions{}); delErr != nil && !errors.IsNotFound(delErr) {
			slog.ErrorContext(ctx, "rollback: deleting project namespace after RBAC bootstrap failure",
				slog.String("namespace", created.Name),
				slog.Any("error", delErr),
			)
		}
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

	org := GetOrganization(ns)

	// Handle reparenting if parent_type and parent_name are set.
	if (req.Msg.ParentType == nil) != (req.Msg.ParentName == nil) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parent_type and parent_name must be set together"))
	}
	if req.Msg.ParentType != nil && req.Msg.ParentName != nil {
		if err := validateOrganizationProjectParent(*req.Msg.ParentType, *req.Msg.ParentName, org); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
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
// Checks Owner-level permission on both source and destination parents
// when the impersonated client is present (ADR 036), so the API server
// — not in-process Go code — gates moves between namespaces.
// Projects have no children so no depth or cycle checks are needed.
func (h *Handler) reparentProject(
	ctx context.Context,
	ns *corev1.Namespace,
	claims *rpc.Claims,
	newParentType consolev1.ParentType,
	newParentName string,
) error {
	projectName := ns.Labels[v1alpha2.LabelProject]

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

	// Fetch destination parent namespace and enforce owner-level access on
	// the impersonated path. Required so a caller cannot move a child into
	// a parent they do not control (ADR 036 + HOL-1067).
	destParent, err := h.k8s.GetNamespace(ctx, newParentNs)
	if err != nil {
		return mapK8sError(err)
	}
	if err := h.requireReparentOwner(ctx, claims, destParent, "reparent into destination"); err != nil {
		return err
	}

	// Fetch source parent namespace and enforce owner-level access on the
	// impersonated path. Required so a caller cannot evict a child from a
	// parent they do not control.
	srcParent, err := h.k8s.GetNamespace(ctx, currentParentNs)
	if err != nil {
		return mapK8sError(err)
	}
	if err := h.requireReparentOwner(ctx, claims, srcParent, "reparent from source"); err != nil {
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

	org := GetOrganization(ns)

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
	if err := h.requireNamespaceOwner(ctx, claims, ns, shareUsers, shareRoles, "project sharing update"); err != nil {
		return nil, err
	}

	org := GetOrganization(ns)

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	storedCreator := secrets.UserIdentity{
		Email:   ns.Annotations[v1alpha2.AnnotationCreatorEmail],
		Subject: ns.Annotations[v1alpha2.AnnotationCreatorSubject],
	}
	rbacShareUsers := secrets.RBACUserGrantsForSubjects(
		newShareUsers,
		storedCreator,
		secrets.UserIdentity{Email: claims.Email, Subject: claims.Sub},
	)

	updated, err := h.k8s.UpdateProjectSharing(ctx, req.Msg.Name, newShareUsers, newShareRoles, rbacShareUsers)
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
	userRole := h.effectiveRoleForNamespace(ctx, claims, updated, updatedUsers, updatedRoles)

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
	if err := h.requireNamespaceOwner(ctx, claims, ns, shareUsers, shareRoles, "project default sharing update"); err != nil {
		return nil, err
	}

	org := GetOrganization(ns)

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
	userRole := h.effectiveRoleForNamespace(ctx, claims, updated, updatedShareUsers, updatedShareRoles)

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

	org := GetOrganization(ns)

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

func (h *Handler) effectiveRoleForNamespace(ctx context.Context, claims *rpc.Claims, ns *corev1.Namespace, shareUsers, shareRoles []secrets.AnnotationGrant) rbac.Role {
	if rpc.HasImpersonatedClients(ctx) {
		if ok, err := h.k8s.canVerbNamespace(ctx, "delete", ns.Name); err == nil && ok {
			return rbac.RoleOwner
		}
		if ok, err := h.k8s.canVerbNamespace(ctx, "update", ns.Name); err == nil && ok {
			return rbac.RoleEditor
		}
		if ok, err := h.k8s.canVerbNamespace(ctx, "get", ns.Name); err == nil && ok {
			return rbac.RoleViewer
		}
		return rbac.RoleUnspecified
	}
	return rbac.BestRoleFromGrants(
		claims.Email,
		claims.Roles,
		secrets.ActiveGrantsMap(shareUsers, time.Now()),
		secrets.ActiveGrantsMap(shareRoles, time.Now()),
	)
}

// requireReparentOwner enforces Owner-level permission on a parent namespace
// for re-parent operations (HOL-1067). Owner is proven via SSAR for `delete`
// on the namespace, consistent with requireNamespaceOwner.
//
// Enforcement is gated on the presence of an impersonated client because the
// legacy in-process cascade (claims-based) is being phased out per ADR 036.
// Wiring impersonation into a request automatically arms parent-side owner
// gating; bootstrap and tests that have not migrated yet retain pre-HOL-1067
// behavior.
func (h *Handler) requireReparentOwner(ctx context.Context, claims *rpc.Claims, parent *corev1.Namespace, action string) error {
	if !rpc.HasImpersonatedClients(ctx) {
		return nil
	}
	parentShareUsers, _ := GetShareUsers(parent)
	parentShareRoles, _ := GetShareRoles(parent)
	return h.requireNamespaceOwner(ctx, claims, parent, parentShareUsers, parentShareRoles, action)
}

func (h *Handler) requireNamespaceOwner(ctx context.Context, claims *rpc.Claims, ns *corev1.Namespace, shareUsers, shareRoles []secrets.AnnotationGrant, action string) error {
	if rpc.HasImpersonatedClients(ctx) {
		ok, err := h.k8s.canVerbNamespace(ctx, "delete", ns.Name)
		if err != nil {
			return mapK8sError(err)
		}
		if ok {
			return nil
		}
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: not authorized to %s", action))
	}
	if h.effectiveRoleForNamespace(ctx, claims, ns, shareUsers, shareRoles) == rbac.RoleOwner {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: not authorized to %s", action))
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
		p.ParentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
		p.ParentName = p.Organization

		// Derive parent info from the parent label.
		parentNs := ns.Labels[v1alpha2.AnnotationParent]
		if parentNs != "" {
			kind, name, err := h.k8s.Resolver.ResourceTypeFromNamespace(parentNs)
			if err == nil {
				switch kind {
				case v1alpha2.ResourceTypeOrganization:
					p.ParentName = name
					p.ParentType = consolev1.ParentType_PARENT_TYPE_ORGANIZATION
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

// resolveParentNS converts a project ParentType+ParentName pair to a Kubernetes namespace name.
func (h *Handler) resolveParentNS(parentType consolev1.ParentType, parentName string) (string, error) {
	switch parentType {
	case consolev1.ParentType_PARENT_TYPE_ORGANIZATION:
		return h.k8s.Resolver.OrgNamespace(parentName), nil
	default:
		return "", fmt.Errorf("projects can only be parented by organizations")
	}
}

func validateOrganizationProjectParent(parentType consolev1.ParentType, parentName, organization string) error {
	if parentType != consolev1.ParentType_PARENT_TYPE_UNSPECIFIED && parentType != consolev1.ParentType_PARENT_TYPE_ORGANIZATION {
		return fmt.Errorf("projects can only be parented by organizations")
	}
	if parentName != "" && parentName != organization {
		return fmt.Errorf("project parent_name must match organization")
	}
	return nil
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

// mapK8sError converts Kubernetes API errors to ConnectRPC errors. The
// handler-specific "not managed by" sentinel runs first so the
// CodeNotFound mapping wins over the generic apierrors path; the
// apierrors -> connect.Code mapping itself is delegated to
// rpc.MapK8sError so every console handler stays in lock-step.
func mapK8sError(err error) error {
	if err == nil {
		return nil
	}
	// "not managed by" originates in our K8s layer, not from the API
	// server, so it is not an apierrors kind — surface it as
	// CodeNotFound to keep the response consistent with the
	// generic-namespace and "no such project" paths.
	if strings.Contains(err.Error(), "not managed by") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return rpc.MapK8sError(err)
}

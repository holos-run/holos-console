package organizations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/rbac"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"github.com/holos-run/holos-console/gen/holos/console/v1/consolev1connect"
)

const auditResourceType = "organization"

// ProjectLister checks for projects linked to an organization.
type ProjectLister interface {
	ListProjects(ctx context.Context, org, parentNs string) ([]*corev1.Namespace, error)
}

// FolderCreator creates a folder namespace. Used by CreateOrganization to
// auto-create the default folder without importing the folders package directly.
type FolderCreator interface {
	CreateFolder(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) (*corev1.Namespace, error)
	NamespaceExists(ctx context.Context, nsName string) (bool, error)
}

// FolderLister retrieves folder namespaces for validation.
type FolderLister interface {
	GetFolder(ctx context.Context, name string) (*corev1.Namespace, error)
}

// TemplateSeeder seeds default templates into a scope. Used by
// CreateOrganization to seed example templates when populate_defaults is true.
type TemplateSeeder interface {
	SeedOrgTemplate(ctx context.Context, org string) error
	SeedProjectTemplate(ctx context.Context, project string) error
}

// ProjectCreator creates a project namespace. Used by CreateOrganization to
// create a default project when populate_defaults is true, following the same
// pattern as FolderCreator.
type ProjectCreator interface {
	CreateProject(ctx context.Context, name, displayName, description, org, parentNs, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) error
	NamespaceExists(ctx context.Context, nsName string) (bool, error)
}

// Handler implements the OrganizationService.
type Handler struct {
	consolev1connect.UnimplementedOrganizationServiceHandler
	k8s             *K8sClient
	projectLister   ProjectLister
	folderCreator   FolderCreator
	folderLister    FolderLister
	folderPrefix    string // namespace prefix + folder prefix (e.g. "holos-fld-")
	templateSeeder  TemplateSeeder
	projectCreator  ProjectCreator
	projectPrefix   string // namespace prefix + project prefix (e.g. "holos-prj-")
	disableCreation bool
	creatorUsers    []string
	creatorRoles    []string
}

// NewHandler creates a new OrganizationService handler.
// disableCreation disables the implicit organization creation grant to all
// authenticated principals. When true, only explicit creatorUsers and
// creatorRoles are allowed to create organizations.
func NewHandler(k8s *K8sClient, projectLister ProjectLister, disableCreation bool, creatorUsers, creatorRoles []string) *Handler {
	return &Handler{k8s: k8s, projectLister: projectLister, disableCreation: disableCreation, creatorUsers: creatorUsers, creatorRoles: creatorRoles}
}

// WithFolderCreator sets the folder creator used to auto-create the default
// folder when a new organization is created.
func (h *Handler) WithFolderCreator(fc FolderCreator, fl FolderLister, folderPrefix string) *Handler {
	h.folderCreator = fc
	h.folderLister = fl
	h.folderPrefix = folderPrefix
	return h
}

// WithDefaultsSeeder sets the template seeder and project creator used to
// populate example resources when populate_defaults is true on CreateOrganization.
func (h *Handler) WithDefaultsSeeder(ts TemplateSeeder, pc ProjectCreator, projectPrefix string) *Handler {
	h.templateSeeder = ts
	h.projectCreator = pc
	h.projectPrefix = projectPrefix
	return h
}

// ListOrganizations returns all organizations the user has access to.
func (h *Handler) ListOrganizations(
	ctx context.Context,
	req *connect.Request[consolev1.ListOrganizationsRequest],
) (*connect.Response[consolev1.ListOrganizationsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	allOrgs, err := h.k8s.ListOrganizations(ctx)
	if err != nil {
		return nil, mapK8sError(err)
	}

	now := time.Now()
	var result []*consolev1.Organization
	for _, ns := range allOrgs {
		shareUsers, _ := GetShareUsers(ns)
		shareRoles, _ := GetShareRoles(ns)
		activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
		activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

		if err := CheckOrgListAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			continue
		}

		userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, activeUsers, activeRoles)
		result = append(result, buildOrganization(h.k8s, ns, shareUsers, shareRoles, userRole))
	}

	slog.InfoContext(ctx, "organizations listed",
		slog.String("action", "organization_list"),
		slog.String("resource_type", auditResourceType),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
		slog.Int("total", len(result)),
	)

	return connect.NewResponse(&consolev1.ListOrganizationsResponse{
		Organizations: result,
	}), nil
}

// GetOrganization retrieves an organization by name.
func (h *Handler) GetOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.GetOrganizationRequest],
) (*connect.Response[consolev1.GetOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckOrgReadAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "organization access denied",
			slog.String("action", "organization_read_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, activeUsers, activeRoles)

	slog.InfoContext(ctx, "organization accessed",
		slog.String("action", "organization_read"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.GetOrganizationResponse{
		Organization: buildOrganization(h.k8s, ns, shareUsers, shareRoles, userRole),
	}), nil
}

// CreateOrganization creates a new organization and its default folder.
// If default folder creation fails, the org namespace is rolled back.
func (h *Handler) CreateOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.CreateOrganizationRequest],
) (*connect.Response[consolev1.CreateOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	// Implicit grant: all authenticated principals can create orgs unless disabled.
	// Explicit grants via --org-creator-users/--org-creator-roles always apply.
	if h.disableCreation && !h.isOrgCreator(claims.Email, claims.Roles) {
		slog.WarnContext(ctx, "organization create denied",
			slog.String("action", "organization_create_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RBAC: not authorized to create organizations"))
	}

	shareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	shareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	// Ensure creator is included as owner
	shareUsers = ensureCreatorOwner(shareUsers, claims.Email)

	if _, err := h.k8s.CreateOrganization(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description, claims.Email, shareUsers, shareRoles); err != nil {
		return nil, mapK8sError(err)
	}

	// Auto-create the default folder as an immediate child of the org.
	if h.folderCreator != nil {
		folderDisplayName := "Default"
		if req.Msg.DefaultFolder != nil && *req.Msg.DefaultFolder != "" {
			folderDisplayName = *req.Msg.DefaultFolder
		}

		folderName, err := h.createDefaultFolder(ctx, req.Msg.Name, folderDisplayName, claims.Email, shareUsers, shareRoles)
		if err != nil {
			// Rollback: delete the org namespace on default folder failure.
			slog.ErrorContext(ctx, "default folder creation failed, rolling back org",
				slog.String("organization", req.Msg.Name),
				slog.Any("error", err),
			)
			if delErr := h.k8s.DeleteOrganization(ctx, req.Msg.Name); delErr != nil {
				slog.ErrorContext(ctx, "org rollback failed",
					slog.String("organization", req.Msg.Name),
					slog.Any("error", delErr),
				)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("creating default folder: %w", err))
		}

		// Store the default folder identifier on the org namespace.
		if err := h.k8s.SetDefaultFolder(ctx, req.Msg.Name, folderName); err != nil {
			slog.ErrorContext(ctx, "failed to set default folder annotation, rolling back",
				slog.String("organization", req.Msg.Name),
				slog.String("folder", folderName),
				slog.Any("error", err),
			)
			// Roll back: the org contract requires default_folder to be set.
			if delErr := h.k8s.DeleteOrganization(ctx, req.Msg.Name); delErr != nil {
				slog.ErrorContext(ctx, "org rollback after annotation failure failed",
					slog.String("organization", req.Msg.Name),
					slog.Any("error", delErr),
				)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("setting default folder annotation: %w", err))
		}
	}

	// Seed example resources when populate_defaults is requested.
	if req.Msg.GetPopulateDefaults() {
		if err := h.seedDefaults(ctx, req.Msg.Name, claims.Email, shareUsers, shareRoles); err != nil {
			slog.ErrorContext(ctx, "populate defaults failed, rolling back org",
				slog.String("organization", req.Msg.Name),
				slog.Any("error", err),
			)
			if delErr := h.k8s.DeleteOrganization(ctx, req.Msg.Name); delErr != nil {
				slog.ErrorContext(ctx, "org rollback after seed failure failed",
					slog.String("organization", req.Msg.Name),
					slog.Any("error", delErr),
				)
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("seeding default resources: %w", err))
		}
	}

	slog.InfoContext(ctx, "organization created",
		slog.String("action", "organization_create"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.CreateOrganizationResponse{
		Name: req.Msg.Name,
	}), nil
}

// createDefaultFolder generates an identifier from the display name and creates
// the folder namespace as a direct child of the organization. Returns the folder
// identifier (slug).
func (h *Handler) createDefaultFolder(ctx context.Context, orgName, displayName, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) (string, error) {
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.folderCreator.NamespaceExists(ctx, nsName)
	}
	folderName, err := v1alpha2.GenerateIdentifier(ctx, displayName, h.folderPrefix, exists)
	if err != nil {
		return "", fmt.Errorf("generating folder identifier: %w", err)
	}

	orgNsName := h.k8s.resolver.OrgNamespace(orgName)
	if _, err := h.folderCreator.CreateFolder(ctx, folderName, displayName, "", orgName, orgNsName, creatorEmail, shareUsers, shareRoles); err != nil {
		return "", fmt.Errorf("creating folder namespace: %w", err)
	}
	return folderName, nil
}

// seedDefaults creates example resources for the new organization:
//  1. An org-level platform template (HTTPRoute, enabled)
//  2. A default project in the default folder
//  3. An example project-level deployment template in the new project
//
// If any step fails, the caller is responsible for rolling back the org.
func (h *Handler) seedDefaults(ctx context.Context, orgName, creatorEmail string, shareUsers, shareRoles []secrets.AnnotationGrant) error {
	if h.templateSeeder == nil || h.projectCreator == nil {
		return fmt.Errorf("defaults seeder not configured")
	}

	// Step 1: Seed org-level platform template (enabled).
	if err := h.templateSeeder.SeedOrgTemplate(ctx, orgName); err != nil {
		return fmt.Errorf("seeding org template: %w", err)
	}

	// Step 2: Create default project in the default folder.
	// Resolve the default folder namespace from the org's annotation.
	orgNs, err := h.k8s.GetOrganization(ctx, orgName)
	if err != nil {
		return fmt.Errorf("looking up org for default folder: %w", err)
	}
	defaultFolder := orgNs.Annotations[v1alpha2.AnnotationDefaultFolder]
	if defaultFolder == "" {
		return fmt.Errorf("organization %q has no default folder set", orgName)
	}

	// Derive the parent namespace from the default folder.
	parentNs := h.k8s.resolver.FolderNamespace(defaultFolder)

	projectDisplayName := "Default"
	exists := func(ctx context.Context, nsName string) (bool, error) {
		return h.projectCreator.NamespaceExists(ctx, nsName)
	}
	projectName, err := v1alpha2.GenerateIdentifier(ctx, projectDisplayName, h.projectPrefix, exists)
	if err != nil {
		return fmt.Errorf("generating project identifier: %w", err)
	}

	if err := h.projectCreator.CreateProject(ctx, projectName, projectDisplayName, "", orgName, parentNs, creatorEmail, shareUsers, shareRoles); err != nil {
		return fmt.Errorf("creating default project: %w", err)
	}

	// Step 3: Seed project-level example deployment template.
	if err := h.templateSeeder.SeedProjectTemplate(ctx, projectName); err != nil {
		return fmt.Errorf("seeding project template: %w", err)
	}

	return nil
}

// UpdateOrganization updates organization metadata.
func (h *Handler) UpdateOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrganizationRequest],
) (*connect.Response[consolev1.UpdateOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	// Changing default_folder requires ADMIN (OWNER) permission.
	if req.Msg.DefaultFolder != nil {
		if err := CheckOrgAdminAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			slog.WarnContext(ctx, "organization update default folder denied",
				slog.String("action", "organization_update_denied"),
				slog.String("resource_type", auditResourceType),
				slog.String("organization", req.Msg.Name),
				slog.String("sub", claims.Sub),
				slog.String("email", claims.Email),
			)
			return nil, err
		}
	} else {
		if err := CheckOrgWriteAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
			slog.WarnContext(ctx, "organization update denied",
				slog.String("action", "organization_update_denied"),
				slog.String("resource_type", auditResourceType),
				slog.String("organization", req.Msg.Name),
				slog.String("sub", claims.Sub),
				slog.String("email", claims.Email),
			)
			return nil, err
		}
	}

	// Validate and update default folder if requested.
	if req.Msg.DefaultFolder != nil {
		newFolder := *req.Msg.DefaultFolder
		if newFolder == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("default_folder must not be empty"))
		}
		if err := h.validateDefaultFolder(ctx, req.Msg.Name, newFolder); err != nil {
			return nil, err
		}
		if err := h.k8s.SetDefaultFolder(ctx, req.Msg.Name, newFolder); err != nil {
			return nil, mapK8sError(err)
		}
	}

	if _, err := h.k8s.UpdateOrganization(ctx, req.Msg.Name, req.Msg.DisplayName, req.Msg.Description); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization updated",
		slog.String("action", "organization_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.UpdateOrganizationResponse{}), nil
}

// validateDefaultFolder checks that the referenced folder exists and is an
// immediate child of the organization (parent label matches org namespace).
func (h *Handler) validateDefaultFolder(ctx context.Context, orgName, folderName string) error {
	if h.folderLister == nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("folder lister not configured"))
	}
	folderNs, err := h.folderLister.GetFolder(ctx, folderName)
	if err != nil {
		if errors.IsNotFound(err) {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("folder %q not found", folderName))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("looking up folder %q: %w", folderName, err))
	}
	// Verify the folder is a direct child of the org.
	orgNsName := h.k8s.resolver.OrgNamespace(orgName)
	parentNs := folderNs.Labels[v1alpha2.AnnotationParent]
	if parentNs != orgNsName {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("folder %q is not an immediate child of organization %q", folderName, orgName))
	}
	return nil
}

// DeleteOrganization deletes a managed organization.
func (h *Handler) DeleteOrganization(
	ctx context.Context,
	req *connect.Request[consolev1.DeleteOrganizationRequest],
) (*connect.Response[consolev1.DeleteOrganizationResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckOrgDeleteAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "organization delete denied",
			slog.String("action", "organization_delete_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	if h.projectLister != nil {
		projects, err := h.projectLister.ListProjects(ctx, req.Msg.Name, "")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking for linked projects: %w", err))
		}
		if len(projects) > 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("cannot delete organization %q: %d linked project(s) must be deleted first", req.Msg.Name, len(projects)))
		}
	}

	if err := h.k8s.DeleteOrganization(ctx, req.Msg.Name); err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization deleted",
		slog.String("action", "organization_delete"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	return connect.NewResponse(&consolev1.DeleteOrganizationResponse{}), nil
}

// UpdateOrganizationSharing updates the sharing grants on an organization.
func (h *Handler) UpdateOrganizationSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrganizationSharingRequest],
) (*connect.Response[consolev1.UpdateOrganizationSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckOrgAdminAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "organization sharing update denied",
			slog.String("action", "organization_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newShareUsers := shareGrantsToAnnotations(req.Msg.UserGrants)
	newShareRoles := shareGrantsToAnnotations(req.Msg.RoleGrants)

	updated, err := h.k8s.UpdateOrganizationSharing(ctx, req.Msg.Name, newShareUsers, newShareRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization sharing updated",
		slog.String("action", "organization_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedUsers, _ := GetShareUsers(updated)
	updatedRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedUsers, now)
	updatedActiveGroups := secrets.ActiveGrantsMap(updatedRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveGroups)

	return connect.NewResponse(&consolev1.UpdateOrganizationSharingResponse{
		Organization: buildOrganization(h.k8s, updated, updatedUsers, updatedRoles, userRole),
	}), nil
}

// UpdateOrganizationDefaultSharing updates the default sharing grants on an organization.
func (h *Handler) UpdateOrganizationDefaultSharing(
	ctx context.Context,
	req *connect.Request[consolev1.UpdateOrganizationDefaultSharingRequest],
) (*connect.Response[consolev1.UpdateOrganizationDefaultSharingResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckOrgAdminAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "organization default sharing update denied",
			slog.String("action", "organization_default_sharing_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	newDefaultUsers := shareGrantsToAnnotations(req.Msg.DefaultUserGrants)
	newDefaultRoles := shareGrantsToAnnotations(req.Msg.DefaultRoleGrants)

	updated, err := h.k8s.UpdateOrganizationDefaultSharing(ctx, req.Msg.Name, newDefaultUsers, newDefaultRoles)
	if err != nil {
		return nil, mapK8sError(err)
	}

	slog.InfoContext(ctx, "organization default sharing updated",
		slog.String("action", "organization_default_sharing_update"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
		slog.String("sub", claims.Sub),
		slog.String("email", claims.Email),
	)

	updatedShareUsers, _ := GetShareUsers(updated)
	updatedShareRoles, _ := GetShareRoles(updated)
	updatedActiveUsers := secrets.ActiveGrantsMap(updatedShareUsers, now)
	updatedActiveRoles := secrets.ActiveGrantsMap(updatedShareRoles, now)
	userRole := rbac.BestRoleFromGrants(claims.Email, claims.Roles, updatedActiveUsers, updatedActiveRoles)

	return connect.NewResponse(&consolev1.UpdateOrganizationDefaultSharingResponse{
		Organization: buildOrganization(h.k8s, updated, updatedShareUsers, updatedShareRoles, userRole),
	}), nil
}

// GetOrganizationRaw retrieves the full Kubernetes Namespace object as verbatim JSON.
func (h *Handler) GetOrganizationRaw(
	ctx context.Context,
	req *connect.Request[consolev1.GetOrganizationRawRequest],
) (*connect.Response[consolev1.GetOrganizationRawResponse], error) {
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization name is required"))
	}

	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	ns, err := h.k8s.GetOrganization(ctx, req.Msg.Name)
	if err != nil {
		return nil, mapK8sError(err)
	}

	shareUsers, _ := GetShareUsers(ns)
	shareRoles, _ := GetShareRoles(ns)
	now := time.Now()
	activeUsers := secrets.ActiveGrantsMap(shareUsers, now)
	activeRoles := secrets.ActiveGrantsMap(shareRoles, now)

	if err := CheckOrgReadAccess(claims.Email, claims.Roles, activeUsers, activeRoles); err != nil {
		slog.WarnContext(ctx, "organization raw access denied",
			slog.String("action", "organization_raw_denied"),
			slog.String("resource_type", auditResourceType),
			slog.String("organization", req.Msg.Name),
			slog.String("sub", claims.Sub),
			slog.String("email", claims.Email),
		)
		return nil, err
	}

	slog.InfoContext(ctx, "organization raw accessed",
		slog.String("action", "organization_raw"),
		slog.String("resource_type", auditResourceType),
		slog.String("organization", req.Msg.Name),
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

	return connect.NewResponse(&consolev1.GetOrganizationRawResponse{
		Raw: string(raw),
	}), nil
}

// isOrgCreator checks whether the caller is authorized to create organizations
// based on the CLI-configured creator lists.
func (h *Handler) isOrgCreator(email string, roles []string) bool {
	emailLower := strings.ToLower(email)
	for _, u := range h.creatorUsers {
		if strings.ToLower(u) == emailLower {
			return true
		}
	}
	for _, r := range roles {
		rLower := strings.ToLower(r)
		for _, cr := range h.creatorRoles {
			if strings.ToLower(cr) == rLower {
				return true
			}
		}
	}
	return false
}

// buildOrganization creates an Organization proto message from a namespace.
func buildOrganization(k8s *K8sClient, ns interface{ GetName() string }, shareUsers, shareRoles []secrets.AnnotationGrant, userRole rbac.Role) *consolev1.Organization {
	org := &consolev1.Organization{
		UserGrants: annotationGrantsToProto(shareUsers),
		RoleGrants: annotationGrantsToProto(shareRoles),
		UserRole:   consolev1.Role(userRole),
	}

	type labeled interface {
		GetLabels() map[string]string
	}
	if l, ok := ns.(labeled); ok {
		labels := l.GetLabels()
		if labels != nil {
			org.Name = labels[v1alpha2.LabelOrganization]
		}
	}
	// Fallback: derive org name from namespace if label is missing (pre-label namespaces)
	if org.Name == "" {
		name, err := k8s.resolver.OrgFromNamespace(ns.GetName())
		if err != nil {
			slog.Warn("organization namespace missing label and prefix mismatch",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelOrganization),
				slog.Any("error", err),
			)
		} else {
			org.Name = name
			slog.Warn("organization namespace missing label, falling back to namespace parsing",
				slog.String("namespace", ns.GetName()),
				slog.String("label", v1alpha2.LabelOrganization),
			)
		}
	}

	type annotated interface {
		GetAnnotations() map[string]string
	}
	if a, ok := ns.(annotated); ok {
		annotations := a.GetAnnotations()
		if annotations != nil {
			org.DisplayName = annotations[v1alpha2.AnnotationDisplayName]
			org.Description = annotations[v1alpha2.AnnotationDescription]
			org.CreatorEmail = annotations[v1alpha2.AnnotationCreatorEmail]
			org.DefaultFolder = annotations[v1alpha2.AnnotationDefaultFolder]
		}
		// Populate default sharing grants and creation timestamp from typed namespace
		if nsTyped, ok := ns.(*corev1.Namespace); ok {
			if defaultUsers, err := GetDefaultShareUsers(nsTyped); err == nil {
				org.DefaultUserGrants = annotationGrantsToProto(defaultUsers)
			}
			if defaultRoles, err := GetDefaultShareRoles(nsTyped); err == nil {
				org.DefaultRoleGrants = annotationGrantsToProto(defaultRoles)
			}
			org.CreatedAt = nsTyped.CreationTimestamp.UTC().Format(time.RFC3339)
		}
	}

	return org
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

// ensureCreatorOwner ensures the creator email is in the share-users list as owner.
func ensureCreatorOwner(shareUsers []secrets.AnnotationGrant, email string) []secrets.AnnotationGrant {
	emailLower := strings.ToLower(email)
	for _, g := range shareUsers {
		if strings.ToLower(g.Principal) == emailLower && strings.ToLower(g.Role) == "owner" {
			return shareUsers
		}
	}
	return append(shareUsers, secrets.AnnotationGrant{Principal: email, Role: "owner"})
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
	if strings.Contains(err.Error(), "not managed by") || strings.Contains(err.Error(), "not an organization") {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

package organizations

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	"github.com/holos-run/holos-console/console/secrets"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// contextWithClaims creates a context with OIDC claims.
func contextWithClaims(email string, groups ...string) context.Context {
	claims := &rpc.Claims{
		Sub:           "sub-" + email,
		Email:         email,
		EmailVerified: true,
		Name:          email,
		Groups:        groups,
	}
	return rpc.ContextWithClaims(context.Background(), claims)
}

// orgNS creates an organization namespace with share-users annotation.
func orgNS(name string, shareUsersJSON string) *corev1.Namespace {
	annotations := map[string]string{}
	if shareUsersJSON != "" {
		annotations[secrets.ShareUsersAnnotation] = shareUsersJSON
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-org-" + name,
			Labels: map[string]string{
				secrets.ManagedByLabel:     secrets.ManagedByValue,
				resolver.ResourceTypeLabel: resolver.ResourceTypeOrganization,
			},
			Annotations: annotations,
		},
	}
}

func newTestHandler(namespaces ...*corev1.Namespace) *Handler {
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler
}

// ---- ListOrganizations tests ----

func TestListOrganizations_ReturnsFilteredByAccess(t *testing.T) {
	ns1 := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	ns2 := orgNS("beta", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns3 := orgNS("gamma", `[{"principal":"bob@example.com","role":"owner"}]`)

	handler := newTestHandler(ns1, ns2, ns3)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(resp.Msg.Organizations))
	}
}

func TestListOrganizations_Unauthenticated(t *testing.T) {
	handler := newTestHandler()
	_, err := handler.ListOrganizations(context.Background(), connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	assertUnauthenticated(t, err)
}

func TestListOrganizations_ReturnsOrgNameNotNamespace(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.ListOrganizations(ctx, connect.NewRequest(&consolev1.ListOrganizationsRequest{}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organizations) != 1 {
		t.Fatalf("expected 1 org, got %d", len(resp.Msg.Organizations))
	}
	if resp.Msg.Organizations[0].Name != "acme" {
		t.Errorf("expected name 'acme', got %q", resp.Msg.Organizations[0].Name)
	}
}

// ---- GetOrganization tests ----

func TestGetOrganization_Authorized(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	ns.Annotations[DisplayNameAnnotation] = "ACME Corp"
	ns.Annotations[secrets.DescriptionAnnotation] = "Test org"

	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	org := resp.Msg.Organization
	if org.Name != "acme" {
		t.Errorf("expected name 'acme', got %q", org.Name)
	}
	if org.DisplayName != "ACME Corp" {
		t.Errorf("expected display_name 'ACME Corp', got %q", org.DisplayName)
	}
	if org.UserRole != consolev1.Role_ROLE_VIEWER {
		t.Errorf("expected ROLE_VIEWER, got %v", org.UserRole)
	}
}

func TestGetOrganization_Denied(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"bob@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("nobody@example.com")

	_, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: "acme"}))
	assertPermissionDenied(t, err)
}

func TestGetOrganization_InvalidArgument(t *testing.T) {
	handler := newTestHandler()
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.GetOrganization(ctx, connect.NewRequest(&consolev1.GetOrganizationRequest{Name: ""}))
	assertInvalidArgument(t, err)
}

// ---- CreateOrganization tests ----

func TestCreateOrganization_Authorized(t *testing.T) {
	existing := orgNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name:        "new-org",
		DisplayName: "New Org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Msg.Name != "new-org" {
		t.Errorf("expected name 'new-org', got %q", resp.Msg.Name)
	}
}

func TestCreateOrganization_Denied(t *testing.T) {
	existing := orgNS("existing", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(existing)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	assertPermissionDenied(t, err)
}

func TestCreateOrganization_AutoOwner(t *testing.T) {
	existing := orgNS("existing", `[{"principal":"alice@example.com","role":"owner"}]`)

	objs := []runtime.Object{existing}
	fakeClient := fake.NewClientset(objs...)
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s)

	ctx := contextWithClaims("alice@example.com")
	_, err := handler.CreateOrganization(ctx, connect.NewRequest(&consolev1.CreateOrganizationRequest{
		Name: "new-org",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ns, err := fakeClient.CoreV1().Namespaces().Get(context.Background(), "holos-org-new-org", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected namespace to exist, got %v", err)
	}
	users, err := GetShareUsers(ns)
	if err != nil {
		t.Fatalf("failed to parse share-users: %v", err)
	}
	found := false
	for _, u := range users {
		if u.Principal == "alice@example.com" && u.Role == "owner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected creator as owner in share-users, got %v", users)
	}
}

// ---- UpdateOrganization tests ----

func TestUpdateOrganization_EditorAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:        "acme",
		DisplayName: &displayName,
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestUpdateOrganization_ViewerDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"viewer"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	displayName := "Updated"
	_, err := handler.UpdateOrganization(ctx, connect.NewRequest(&consolev1.UpdateOrganizationRequest{
		Name:        "acme",
		DisplayName: &displayName,
	}))
	assertPermissionDenied(t, err)
}

// ---- DeleteOrganization tests ----

func TestDeleteOrganization_OwnerAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDeleteOrganization_EditorDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.DeleteOrganization(ctx, connect.NewRequest(&consolev1.DeleteOrganizationRequest{Name: "acme"}))
	assertPermissionDenied(t, err)
}

// ---- UpdateOrganizationSharing tests ----

func TestUpdateOrgSharing_OwnerAllows(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	resp, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
			{Principal: "bob@example.com", Role: consolev1.Role_ROLE_EDITOR},
		},
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Msg.Organization.UserGrants) != 2 {
		t.Errorf("expected 2 user grants, got %d", len(resp.Msg.Organization.UserGrants))
	}
}

func TestUpdateOrgSharing_NonOwnerDenies(t *testing.T) {
	ns := orgNS("acme", `[{"principal":"alice@example.com","role":"editor"}]`)
	handler := newTestHandler(ns)
	ctx := contextWithClaims("alice@example.com")

	_, err := handler.UpdateOrganizationSharing(ctx, connect.NewRequest(&consolev1.UpdateOrganizationSharingRequest{
		Name: "acme",
		UserGrants: []*consolev1.ShareGrant{
			{Principal: "alice@example.com", Role: consolev1.Role_ROLE_OWNER},
		},
	}))
	assertPermissionDenied(t, err)
}

// ---- Helpers ----

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v", connectErr.Code())
	}
}

func assertPermissionDenied(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied, got %v", connectErr.Code())
	}
}

func assertInvalidArgument(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connectErr.Code())
	}
}

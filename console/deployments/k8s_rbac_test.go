package deployments

import (
	"context"
	"sort"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// rbacTestScheme returns a runtime.Scheme that knows about the
// deployments.holos.run/v1alpha1 Deployment GVR. The fake dynamic client
// resolves unstructured GETs against this scheme.
func rbacTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "deployments.holos.run", Version: "v1alpha1", Kind: "Deployment"}
	listGVK := schema.GroupVersionKind{Group: "deployments.holos.run", Version: "v1alpha1", Kind: "DeploymentList"}
	s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

// makeDeploymentCR creates an unstructured Deployment CR for the fake dynamic
// client with a stable UID so tests can assert ownerReferences carry it.
func makeDeploymentCR(namespace, name, uid string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("deployments.holos.run/v1alpha1")
	u.SetKind("Deployment")
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetUID(types.UID(uid))
	return u
}

// TestEnsureDeploymentRBAC_HappyPath asserts the three Roles and the
// creator-Owner RoleBinding are created with ownerReferences pointing at the
// Deployment CR (HOL-1033 AC #1, #3).
func TestEnsureDeploymentRBAC_HappyPath(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)

	if err := k8s.EnsureDeploymentRBAC(context.Background(), project, name, "alice@example.com", RoleOwner); err != nil {
		t.Fatalf("EnsureDeploymentRBAC: %v", err)
	}

	roles, err := fakeClient.RbacV1().Roles(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles.Items) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles.Items))
	}
	gotTiers := make(map[string]bool)
	for _, r := range roles.Items {
		gotTiers[RoleFromLabels(r.Labels)] = true
		if len(r.OwnerReferences) != 1 || r.OwnerReferences[0].UID != "uid-xyz" {
			t.Errorf("Role %q ownerRefs=%v", r.Name, r.OwnerReferences)
		}
		if len(r.Rules) != 1 || len(r.Rules[0].ResourceNames) != 1 || r.Rules[0].ResourceNames[0] != name {
			t.Errorf("Role %q resourceNames=%v", r.Name, r.Rules[0].ResourceNames)
		}
	}
	for _, want := range []string{RoleViewer, RoleEditor, RoleOwner} {
		if !gotTiers[want] {
			t.Errorf("missing tier %q", want)
		}
	}

	bindings, err := fakeClient.RbacV1().RoleBindings(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings.Items) != 1 {
		t.Fatalf("expected 1 binding (creator-Owner), got %d", len(bindings.Items))
	}
	rb := bindings.Items[0]
	if len(rb.OwnerReferences) != 1 || rb.OwnerReferences[0].UID != "uid-xyz" {
		t.Errorf("RoleBinding ownerRefs=%v", rb.OwnerReferences)
	}
	if rb.RoleRef.Name != RoleName(name, RoleOwner) {
		t.Errorf("RoleBinding roleref=%q", rb.RoleRef.Name)
	}
	if len(rb.Subjects) != 1 || rb.Subjects[0].Name != "oidc:alice@example.com" {
		t.Errorf("RoleBinding subjects=%v", rb.Subjects)
	}
}

// TestEnsureDeploymentRBAC_Idempotent asserts re-running for the same
// deployment is a no-op (label/rule reconcile in place).
func TestEnsureDeploymentRBAC_Idempotent(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := k8s.EnsureDeploymentRBAC(ctx, project, name, "alice@example.com", RoleOwner); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	roles, _ := fakeClient.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{})
	if len(roles.Items) != 3 {
		t.Errorf("expected 3 roles after 2 calls, got %d", len(roles.Items))
	}
	bindings, _ := fakeClient.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	if len(bindings.Items) != 1 {
		t.Errorf("expected 1 binding after 2 calls, got %d", len(bindings.Items))
	}
}

// TestEnsureDeploymentRBAC_DegradesWhenCRAbsent asserts a missing Deployment
// CR is not an error: provisioning gracefully no-ops so the proto-store path
// keeps working in tests and on lazy-creation paths.
func TestEnsureDeploymentRBAC_DegradesWhenCRAbsent(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme()) // no CR seeded
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)

	if err := k8s.EnsureDeploymentRBAC(context.Background(), project, name, "alice@example.com", RoleOwner); err != nil {
		t.Fatalf("expected nil err when CR absent, got %v", err)
	}
	roles, _ := fakeClient.RbacV1().Roles(ns).List(context.Background(), metav1.ListOptions{})
	if len(roles.Items) != 0 {
		t.Errorf("expected no roles when CR absent, got %d", len(roles.Items))
	}
}

// TestEnsureDeploymentRBAC_NoDynamicClient asserts that without a dynamic
// client, the call is a graceful no-op so dev/local wiring without a cluster
// keeps working.
func TestEnsureDeploymentRBAC_NoDynamicClient(t *testing.T) {
	const project, name = "acme", "web-app"
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver())

	if err := k8s.EnsureDeploymentRBAC(context.Background(), project, name, "alice@example.com", RoleOwner); err != nil {
		t.Fatalf("expected nil err with no dynamic client, got %v", err)
	}
}

// TestReconcileDeploymentRoleBindings_AddRemoveSwap exercises the desired-set
// reconciliation: missing → created, no-longer-desired → deleted, role-tier
// swap → recreate.
func TestReconcileDeploymentRoleBindings_AddRemoveSwap(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)
	ctx := context.Background()

	// Initial reconcile: alice→editor, bob→viewer.
	users := []DeploymentGrant{
		{Principal: "alice@example.com", Role: RoleEditor},
		{Principal: "bob@example.com", Role: RoleViewer},
	}
	if err := k8s.ReconcileDeploymentRoleBindings(ctx, project, name, users, nil); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	got, err := fakeClient.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(got.Items))
	}

	// Second reconcile: alice promoted to owner, bob removed, carol added.
	users = []DeploymentGrant{
		{Principal: "alice@example.com", Role: RoleOwner},
		{Principal: "carol@example.com", Role: RoleViewer},
	}
	if err := k8s.ReconcileDeploymentRoleBindings(ctx, project, name, users, nil); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	got, err = fakeClient.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 bindings after swap, got %d", len(got.Items))
	}
	subjects := make([]string, 0, len(got.Items))
	for _, rb := range got.Items {
		if len(rb.Subjects) != 1 {
			t.Fatalf("rb %q subjects=%d", rb.Name, len(rb.Subjects))
		}
		subjects = append(subjects, rb.Subjects[0].Name+"="+RoleFromLabels(rb.Labels))
	}
	sort.Strings(subjects)
	want := []string{
		"oidc:alice@example.com=" + RoleOwner,
		"oidc:carol@example.com=" + RoleViewer,
	}
	if !equalStrings(subjects, want) {
		t.Errorf("bindings: got %v want %v", subjects, want)
	}
}

// TestReconcileDeploymentRoleBindings_GroupsAndUsersBoth exercises mixing
// user and group grants in a single reconcile.
func TestReconcileDeploymentRoleBindings_GroupsAndUsersBoth(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)

	users := []DeploymentGrant{{Principal: "alice@example.com", Role: RoleEditor}}
	groups := []DeploymentGrant{{Principal: "platform-admins", Role: RoleOwner}}
	if err := k8s.ReconcileDeploymentRoleBindings(context.Background(), project, name, users, groups); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := fakeClient.RbacV1().RoleBindings(ns).List(context.Background(), metav1.ListOptions{})
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(got.Items))
	}
	var sawUser, sawGroup bool
	for _, rb := range got.Items {
		switch rb.Subjects[0].Kind {
		case rbacv1.UserKind:
			sawUser = true
		case rbacv1.GroupKind:
			sawGroup = true
		}
	}
	if !sawUser || !sawGroup {
		t.Errorf("expected user+group bindings, got user=%v group=%v", sawUser, sawGroup)
	}
}

// TestListDeploymentSharing_RoundTrip seeds RoleBindings via the reconcile
// path and asserts ListDeploymentSharing decodes them back into grants.
func TestListDeploymentSharing_RoundTrip(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)
	ctx := context.Background()

	users := []DeploymentGrant{{Principal: "alice@example.com", Role: RoleEditor}}
	groups := []DeploymentGrant{{Principal: "platform-admins", Role: RoleOwner}}
	if err := k8s.ReconcileDeploymentRoleBindings(ctx, project, name, users, groups); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	gotUsers, gotGroups, err := k8s.ListDeploymentSharing(ctx, project, name)
	if err != nil {
		t.Fatalf("list sharing: %v", err)
	}
	if len(gotUsers) != 1 || gotUsers[0].Role != RoleEditor {
		t.Errorf("users=%v", gotUsers)
	}
	if len(gotGroups) != 1 || gotGroups[0].Role != RoleOwner {
		t.Errorf("groups=%v", gotGroups)
	}
}

// TestEnsureDeploymentRBAC_GCDeletesOwnedObjects asserts that deleting the
// Deployment CR is enough to garbage-collect the per-Deployment Roles and
// RoleBindings in a real cluster — represented here by checking that every
// child object carries the required ownerReferences (Controller=true,
// BlockOwnerDeletion=true) targeting the Deployment CR. The fake clientset
// does not run the GC controller, so this is a structural assertion (AC #3).
func TestEnsureDeploymentRBAC_GCOwnerReferencesShape(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	cr := makeDeploymentCR(ns, name, "uid-xyz")
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme(), cr)
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)
	ctx := context.Background()

	if err := k8s.EnsureDeploymentRBAC(ctx, project, name, "alice@example.com", RoleOwner); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	roles, _ := fakeClient.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{})
	for _, r := range roles.Items {
		assertOwnerRefShape(t, "Role/"+r.Name, r.OwnerReferences, "uid-xyz")
	}
	bindings, _ := fakeClient.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
	for _, rb := range bindings.Items {
		assertOwnerRefShape(t, "RoleBinding/"+rb.Name, rb.OwnerReferences, "uid-xyz")
	}
}

func assertOwnerRefShape(t *testing.T, label string, refs []metav1.OwnerReference, wantUID string) {
	t.Helper()
	if len(refs) != 1 {
		t.Fatalf("%s: ownerRefs=%d want 1", label, len(refs))
	}
	or := refs[0]
	if or.UID != types.UID(wantUID) {
		t.Errorf("%s: UID=%q want %q", label, or.UID, wantUID)
	}
	if or.Controller == nil || !*or.Controller {
		t.Errorf("%s: Controller!=true", label)
	}
	if or.BlockOwnerDeletion == nil || !*or.BlockOwnerDeletion {
		t.Errorf("%s: BlockOwnerDeletion!=true", label)
	}
	if or.APIVersion != "deployments.holos.run/v1alpha1" || or.Kind != "Deployment" {
		t.Errorf("%s: APIVersion/Kind=%q/%q", label, or.APIVersion, or.Kind)
	}
}

// TestReconcileDeploymentRoleBindings_NoBindingsWhenCRAbsent asserts that
// reconcile still creates the RoleBindings when the CR is absent (degraded
// mode), but with nil ownerRefs so they will be retro-stamped on the next
// update.
func TestReconcileDeploymentRoleBindings_NilOwnerRefsWhenCRAbsent(t *testing.T) {
	const project, name = "acme", "web-app"
	ns := "prj-" + project
	dyn := dynamicfake.NewSimpleDynamicClient(rbacTestScheme()) // no CR
	fakeClient := fake.NewClientset(projectNS(project))
	k8s := NewK8sClient(fakeClient, testResolver()).WithDynamicClient(dyn)

	users := []DeploymentGrant{{Principal: "alice@example.com", Role: RoleEditor}}
	if err := k8s.ReconcileDeploymentRoleBindings(context.Background(), project, name, users, nil); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, err := fakeClient.RbacV1().RoleBindings(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(got.Items))
	}
	if len(got.Items[0].OwnerReferences) != 0 {
		t.Errorf("expected nil ownerRefs when CR absent, got %v", got.Items[0].OwnerReferences)
	}
}


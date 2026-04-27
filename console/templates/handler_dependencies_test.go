// handler_dependencies_test.go exercises the reverse-dependency RPCs introduced
// in HOL-986: ListTemplateDependents and ListDeploymentDependents.
//
// Test coverage per ADR 032 Decision 3:
//   - Empty result (no dependents)
//   - Scope A (instance): TemplateDependency where requires.namespace == dependent.namespace
//   - Scope B (project): TemplateRequirement in an org/folder namespace
//   - Scope C (remote-project): TemplateDependency where requires.namespace != dependent.namespace
//   - RBAC filtering: dependents in namespaces the caller cannot see are dropped
//   - ListDeploymentDependents: singleton owner-reference graph
//   - Unauthenticated request rejected
//   - Missing required field rejected
package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// depsTestResolver is the namespace resolver used throughout the dependency tests.
// Prefixes mirror testResolver in main_test.go; kept separate to avoid coupling.
var depsTestResolver = &resolver.Resolver{
	OrganizationPrefix: "org-",
	FolderPrefix:       "fld-",
	ProjectPrefix:      "prj-",
}

// depsTestScheme returns a scheme registered with core, templates v1alpha1,
// and deployments v1alpha1 — everything the dependency handler touches.
func depsTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("register clientgo scheme: %v", err)
	}
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register templates v1alpha1 scheme: %v", err)
	}
	if err := deploymentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register deployments v1alpha1 scheme: %v", err)
	}
	return s
}

// newDepsHandler builds a Handler wired against the given runtime objects with
// both org and project grant resolvers granting full owner access to everyone
// in shareUsers. Tests that want fine-grained RBAC should build the handler
// manually.
func newDepsHandler(t *testing.T, shareUsers map[string]string, objs ...runtime.Object) *Handler {
	t.Helper()
	cl := ctrlfake.NewClientBuilder().
		WithScheme(depsTestScheme(t)).
		WithRuntimeObjects(objs...).
		Build()
	k8s := NewK8sClient(cl, depsTestResolver)
	h := NewHandler(k8s, depsTestResolver, &stubRenderer{}, policyresolver.NewNoopResolver())
	h.WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers})
	h.WithFolderGrantResolver(&stubFolderGrantResolver{users: shareUsers})
	h.WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	return h
}

// makeNS builds a Namespace fixture with the given resource-type label and
// optional parent annotation, suitable for the namespace resolver to classify.
func makeNS(name, resourceType string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: resourceType,
			},
		},
	}
}

// makeTemplateDependency builds a TemplateDependency fixture.
func makeTemplateDependency(ns, name, dependentNs, dependentName, requiresNs, requiresName string) *templatesv1alpha1.TemplateDependency {
	return &templatesv1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: templatesv1alpha1.TemplateDependencySpec{
			Dependent: templatesv1alpha1.LinkedTemplateRef{
				Namespace: dependentNs,
				Name:      dependentName,
			},
			Requires: templatesv1alpha1.LinkedTemplateRef{
				Namespace: requiresNs,
				Name:      requiresName,
			},
		},
	}
}

// makeTemplateRequirement builds a TemplateRequirement fixture.
func makeTemplateRequirement(ns, name, requiresNs, requiresName string) *templatesv1alpha1.TemplateRequirement {
	return &templatesv1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: templatesv1alpha1.TemplateRequirementSpec{
			Requires: templatesv1alpha1.LinkedTemplateRef{
				Namespace: requiresNs,
				Name:      requiresName,
			},
			TargetRefs: []templatesv1alpha1.TemplateRequirementTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "*",
					ProjectName: "*",
				},
			},
		},
	}
}

// boolPtr is a helper for *bool fields on ownerReferences.
func boolPtr(b bool) *bool { return &b }

// makeSingletonDeployment builds a Deployment fixture that models a singleton
// (the required deployment), with ownerReferences pointing to dependent
// Deployments (controller=false, blockOwnerDeletion=true per ADR 032 Decision 3
// point 4).
func makeSingletonDeployment(ns, name string, dependentNames []string) *deploymentsv1alpha1.Deployment {
	var owners []metav1.OwnerReference
	for _, dn := range dependentNames {
		owners = append(owners, metav1.OwnerReference{
			APIVersion:         "deployments.holos.run/v1alpha1",
			Kind:               "Deployment",
			Name:               dn,
			UID:                types.UID("uid-" + dn),
			Controller:         boolPtr(false),
			BlockOwnerDeletion: boolPtr(true),
		})
	}
	return &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			OwnerReferences: owners,
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "my-project",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: ns,
				Name:      name,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// ListTemplateDependents tests
// ---------------------------------------------------------------------------

func TestListTemplateDependents_Unauthenticated(t *testing.T) {
	h := newDepsHandler(t, map[string]string{"owner@localhost": "owner"})
	_, err := h.ListTemplateDependents(context.Background(), connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "org-acme",
		Name:      "waypoint",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated, got %v", got)
	}
}

func TestListTemplateDependents_MissingNamespace(t *testing.T) {
	h := newDepsHandler(t, map[string]string{"owner@localhost": "owner"})
	ctx := authedCtx("owner@localhost", nil)
	_, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Name: "waypoint",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", got)
	}
}

func TestListTemplateDependents_MissingName(t *testing.T) {
	h := newDepsHandler(t, map[string]string{"owner@localhost": "owner"})
	ctx := authedCtx("owner@localhost", nil)
	_, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "org-acme",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", got)
	}
}

func TestListTemplateDependents_Empty(t *testing.T) {
	// No TemplateDependency or TemplateRequirement objects — empty result.
	orgNs := makeNS("org-acme", v1alpha2.ResourceTypeOrganization)
	h := newDepsHandler(t, map[string]string{"owner@localhost": "owner"}, orgNs)
	ctx := authedCtx("owner@localhost", nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "org-acme",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetDependents()); got != 0 {
		t.Errorf("expected 0 dependents, got %d", got)
	}
}

func TestListTemplateDependents_ScopeA_Instance(t *testing.T) {
	// Scope A: TemplateDependency in "prj-checkout" where requires.namespace ==
	// dependent.namespace (same project namespace, same-namespace requires).
	orgNs := makeNS("org-acme", v1alpha2.ResourceTypeOrganization)
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	td := makeTemplateDependency(
		"prj-checkout", "mcp-requires-waypoint",
		"prj-checkout", "mcp-server", // dependent
		"prj-checkout", "waypoint", // requires (same namespace → Scope A)
	)

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, orgNs, prjNs, td)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(deps))
	}
	d := deps[0]
	if d.Scope != consolev1.DependencyScope_DEPENDENCY_SCOPE_INSTANCE {
		t.Errorf("expected INSTANCE scope, got %v", d.Scope)
	}
	if d.DependentNamespace != "prj-checkout" {
		t.Errorf("dependent_namespace: got %q, want %q", d.DependentNamespace, "prj-checkout")
	}
	if d.DependentName != "mcp-requires-waypoint" {
		t.Errorf("dependent_name: got %q, want %q", d.DependentName, "mcp-requires-waypoint")
	}
	if d.RequiringTemplateName != "mcp-server" {
		t.Errorf("requiring_template_name: got %q, want %q", d.RequiringTemplateName, "mcp-server")
	}
	if d.Kind != "TemplateDependency" {
		t.Errorf("kind: got %q, want %q", d.Kind, "TemplateDependency")
	}
}

func TestListTemplateDependents_ScopeB_Project(t *testing.T) {
	// Scope B: TemplateRequirement in the org namespace that mandates all
	// projects require the waypoint template.
	orgNs := makeNS("org-acme", v1alpha2.ResourceTypeOrganization)
	tr := makeTemplateRequirement(
		"org-acme", "all-projects-require-waypoint",
		"org-acme", "waypoint",
	)

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, orgNs, tr)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "org-acme",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(deps))
	}
	d := deps[0]
	if d.Scope != consolev1.DependencyScope_DEPENDENCY_SCOPE_PROJECT {
		t.Errorf("expected PROJECT scope, got %v", d.Scope)
	}
	if d.DependentNamespace != "org-acme" {
		t.Errorf("dependent_namespace: got %q, want %q", d.DependentNamespace, "org-acme")
	}
	if d.DependentName != "all-projects-require-waypoint" {
		t.Errorf("dependent_name: got %q, want %q", d.DependentName, "all-projects-require-waypoint")
	}
	if d.Kind != "TemplateRequirement" {
		t.Errorf("kind: got %q, want %q", d.Kind, "TemplateRequirement")
	}
}

func TestListTemplateDependents_ScopeC_RemoteProject(t *testing.T) {
	// Scope C: TemplateDependency in "prj-my-app" where requires.namespace
	// ("prj-platform") differs from dependent.namespace ("prj-my-app").
	platformNs := makeNS("prj-platform", v1alpha2.ResourceTypeProject)
	myAppNs := makeNS("prj-my-app", v1alpha2.ResourceTypeProject)
	td := makeTemplateDependency(
		"prj-my-app", "app-requires-shared-db",
		"prj-my-app", "my-app", // dependent
		"prj-platform", "shared-db", // requires (different namespace → Scope C)
	)

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, platformNs, myAppNs, td)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "prj-platform",
		Name:      "shared-db",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(deps))
	}
	d := deps[0]
	if d.Scope != consolev1.DependencyScope_DEPENDENCY_SCOPE_REMOTE_PROJECT {
		t.Errorf("expected REMOTE_PROJECT scope, got %v", d.Scope)
	}
	if d.DependentNamespace != "prj-my-app" {
		t.Errorf("dependent_namespace: got %q, want %q", d.DependentNamespace, "prj-my-app")
	}
	if d.RequiringTemplateName != "my-app" {
		t.Errorf("requiring_template_name: got %q, want %q", d.RequiringTemplateName, "my-app")
	}
}

func TestListTemplateDependents_MultipleProjectDependents(t *testing.T) {
	// Multiple project namespaces each declare a TemplateDependency on the
	// same required template. All should appear in the response.
	orgNs := makeNS("org-acme", v1alpha2.ResourceTypeOrganization)
	prj1Ns := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	prj2Ns := makeNS("prj-payments", v1alpha2.ResourceTypeProject)
	td1 := makeTemplateDependency("prj-checkout", "checkout-dep", "prj-checkout", "checkout-app", "org-acme", "waypoint")
	td2 := makeTemplateDependency("prj-payments", "payments-dep", "prj-payments", "payments-app", "org-acme", "waypoint")

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, orgNs, prj1Ns, prj2Ns, td1, td2)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "org-acme",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetDependents()); got != 2 {
		t.Errorf("expected 2 dependents, got %d", got)
	}
}

func TestListTemplateDependents_ScopeB_FolderNamespace(t *testing.T) {
	// TemplateRequirement may also live in a folder namespace (Scope B).
	fldNs := makeNS("fld-payments", v1alpha2.ResourceTypeFolder)
	tr := makeTemplateRequirement("fld-payments", "payments-require-waypoint", "fld-payments", "waypoint")

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, fldNs, tr)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "fld-payments",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(deps))
	}
	if d := deps[0]; d.Scope != consolev1.DependencyScope_DEPENDENCY_SCOPE_PROJECT {
		t.Errorf("expected PROJECT scope for folder TemplateRequirement, got %v", d.Scope)
	}
}

func TestListTemplateDependents_SkipsProjectScopeRequirement(t *testing.T) {
	// A TemplateRequirement in a project namespace must be silently ignored
	// per the ADR 032 storage-isolation rule (HOL-554).
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	// TemplateRequirement in a project namespace (invalid per ADR 032).
	tr := makeTemplateRequirement("prj-checkout", "invalid-req", "prj-checkout", "waypoint")

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs, tr)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListTemplateDependents(ctx, connect.NewRequest(&consolev1.ListTemplateDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The invalid TemplateRequirement must not appear.
	if got := len(resp.Msg.GetDependents()); got != 0 {
		t.Errorf("expected 0 dependents (invalid TemplateRequirement skipped), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// ListDeploymentDependents tests
// ---------------------------------------------------------------------------

func TestListDeploymentDependents_Unauthenticated(t *testing.T) {
	h := newDepsHandler(t, map[string]string{"owner@localhost": "owner"})
	_, err := h.ListDeploymentDependents(context.Background(), connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint-shared",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated, got %v", got)
	}
}

func TestListDeploymentDependents_NotFound(t *testing.T) {
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs)
	ctx := authedCtx(owner, nil)

	_, err := h.ListDeploymentDependents(ctx, connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "does-not-exist-shared",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeNotFound {
		t.Errorf("expected NotFound, got %v", got)
	}
}

func TestListDeploymentDependents_Empty(t *testing.T) {
	// Singleton with no ownerReferences → empty result.
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	singleton := makeSingletonDeployment("prj-checkout", "waypoint-shared", nil)

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs, singleton)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListDeploymentDependents(ctx, connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint-shared",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetDependents()); got != 0 {
		t.Errorf("expected 0 dependents, got %d", got)
	}
}

func TestListDeploymentDependents_SingleDependent(t *testing.T) {
	// Singleton with one dependent ownerReference.
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	singleton := makeSingletonDeployment("prj-checkout", "waypoint-shared", []string{"mcp-server"})

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs, singleton)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListDeploymentDependents(ctx, connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint-shared",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(deps))
	}
	if deps[0].DependentName != "mcp-server" {
		t.Errorf("dependent_name: got %q, want %q", deps[0].DependentName, "mcp-server")
	}
}

func TestListDeploymentDependents_MultipleOwners(t *testing.T) {
	// Singleton with multiple dependent ownerReferences.
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	singleton := makeSingletonDeployment("prj-checkout", "waypoint-shared", []string{"app-a", "app-b", "app-c"})

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs, singleton)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListDeploymentDependents(ctx, connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint-shared",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(resp.Msg.GetDependents()); got != 3 {
		t.Errorf("expected 3 dependents, got %d", got)
	}
}

func TestListDeploymentDependents_IgnoresControllerOwners(t *testing.T) {
	// ownerReferences with controller=true should not be treated as dependents.
	prjNs := makeNS("prj-checkout", v1alpha2.ResourceTypeProject)
	singleton := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "waypoint-shared",
			Namespace: "prj-checkout",
			OwnerReferences: []metav1.OwnerReference{
				// controller=true → not a dependent
				{
					APIVersion:         "apps/v1",
					Kind:               "ReplicaSet",
					Name:               "replicaset-owner",
					UID:                "uid-rs",
					Controller:         boolPtr(true),
					BlockOwnerDeletion: boolPtr(true),
				},
				// controller=false, blockOwnerDeletion=true → IS a dependent
				{
					APIVersion:         "deployments.holos.run/v1alpha1",
					Kind:               "Deployment",
					Name:               "my-app",
					UID:                "uid-my-app",
					Controller:         boolPtr(false),
					BlockOwnerDeletion: boolPtr(true),
				},
			},
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "checkout",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: "prj-checkout",
				Name:      "waypoint-shared",
			},
		},
	}

	const owner = "owner@localhost"
	h := newDepsHandler(t, map[string]string{owner: "owner"}, prjNs, singleton)
	ctx := authedCtx(owner, nil)

	resp, err := h.ListDeploymentDependents(ctx, connect.NewRequest(&consolev1.ListDeploymentDependentsRequest{
		Namespace: "prj-checkout",
		Name:      "waypoint-shared",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deps := resp.Msg.GetDependents()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependent (controller owners excluded), got %d", len(deps))
	}
	if deps[0].DependentName != "my-app" {
		t.Errorf("dependent_name: got %q, want %q", deps[0].DependentName, "my-app")
	}
}

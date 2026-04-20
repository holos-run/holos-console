// k8s_test.go exercises the HOL-662 rewrite of the TemplatePolicyBinding
// CRUD surface against the TemplatePolicyBinding CRD. Each CRUD test
// builds a K8sClient backed by the shared envtest bootstrap in
// console/crdmgr/testing (extracted in HOL-663) and exercises one
// operation table-driven.
//
// Cache freshness is covered by TestK8sClient_ListReflectsCreate, which
// creates a TemplatePolicyBinding through the delegating client and
// asserts a subsequent List observes it within the resync window. The
// CEL ValidatingAdmissionPolicy that rejects writes into project
// namespaces (HOL-618) is regressed in
// TestCreateBindingRejectedByAdmissionInProjectNamespace.
package templatepolicybindings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// newTestResolver is the canonical resolver used by every test in this
// package. Namespace prefixes match the defaults production wires so
// namespace strings round-trip through scopeshim.FromNamespace in tests.
func newTestResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

// samplePolicyRef returns a minimal valid proto policy ref suitable for
// fixtures.
func samplePolicyRef() *consolev1.LinkedTemplatePolicyRef {
	return scopeshim.NewLinkedTemplatePolicyRef(scopeshim.ScopeOrganization, "acme", "require-http-route")
}

// sampleTargetRef returns a minimal valid proto target ref suitable for
// fixtures.
func sampleTargetRef() *consolev1.TemplatePolicyBindingTargetRef {
	return &consolev1.TemplatePolicyBindingTargetRef{
		Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
		Name:        "api",
		ProjectName: "payments-web",
	}
}

// newEnvtestK8sClient builds a K8sClient backed by the shared envtest
// bootstrap in console/crdmgr/testing. The K8sClient receives the
// manager's cache-backed client so every Get / List the CRUD tests
// exercise goes through the informer cache — the HOL-662 acceptance
// criterion the suite regresses against. Writes go straight to the API
// server (controller-runtime default), so the create-then-list
// freshness test catches any regression where the cache-backed read
// path is bypassed.
//
// The helper also applies the folder/org-only CEL admission policies
// from config/admission/ once per process and waits for the policy
// this suite depends on (templatepolicybinding-folder-or-org-only) to
// be registered so the admission-rejection regression does not race
// the CEL compiler.
func newEnvtestK8sClient(t *testing.T) (*crdmgrtesting.Env, *K8sClient) {
	t.Helper()
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme:                   testScheme(t),
		InformerObjects:          []ctrlclient.Object{&templatesv1alpha1.TemplatePolicyBinding{}},
		WaitForAdmissionPolicies: []string{"templatepolicybinding-folder-or-org-only"},
	})
	if env == nil {
		t.SkipNow()
	}
	return env, NewK8sClient(env.Client, newTestResolver())
}

// ensureNamespace creates a namespace if it does not already exist.
// Labels match the production resolver's expectations so the CEL VAP can
// classify the namespace by ResourceType when admitting writes.
func ensureNamespace(t *testing.T, c ctrlclient.Client, name, resourceType string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: resourceType,
			},
		},
	}
	if err := c.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

// eventuallyGetBinding polls K8sClient.GetBinding until it returns a
// match or the deadline expires. Used after a seed write through the
// direct client so tests observing through the cache-backed K8sClient
// tolerate the watch-propagation window.
func eventuallyGetBinding(t *testing.T, k *K8sClient, namespace, name string) *templatesv1alpha1.TemplatePolicyBinding {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := k.GetBinding(context.Background(), namespace, name)
		if err == nil {
			return b
		}
		if !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected GetBinding error for %q/%q: %v", namespace, name, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-backed GetBinding did not observe %q/%q within deadline", namespace, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// eventuallyGetBindingAtResourceVersion polls until the cache-backed
// GetBinding returns an object whose ResourceVersion matches wantRV or
// the deadline expires. Used between sequential Updates in a test so the
// next Update's internal GetBinding reads a fresh copy instead of the
// cached stale one and trips the apiserver's optimistic-concurrency
// guard ("the object has been modified; please apply ...").
func eventuallyGetBindingAtResourceVersion(t *testing.T, k *K8sClient, namespace, name, wantRV string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := k.GetBinding(context.Background(), namespace, name)
		if err == nil && b.ResourceVersion == wantRV {
			return
		}
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected GetBinding error waiting for RV %q: %v", wantRV, err)
		}
		if time.Now().After(deadline) {
			got := ""
			if b != nil {
				got = b.ResourceVersion
			}
			t.Fatalf("cache did not observe binding %q/%q at RV %q within deadline (latest seen RV=%q)", namespace, name, wantRV, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// eventuallyListBindings polls K8sClient.ListBindings until it returns at
// least wantCount items or the deadline expires.
func eventuallyListBindings(t *testing.T, k *K8sClient, namespace string, wantCount int) []templatesv1alpha1.TemplatePolicyBinding {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := k.ListBindings(context.Background(), namespace)
		if err != nil {
			t.Fatalf("ListBindings error for %q: %v", namespace, err)
		}
		if len(got) >= wantCount {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-backed ListBindings did not observe %d bindings in %q within deadline (got %d)",
				wantCount, namespace, len(got))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ------------------------------------------------------------------------
// Envtest table-driven CRUD tests.
// ------------------------------------------------------------------------

func TestListBindings(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	type row struct {
		name      string
		namespace string
		seed      []*templatesv1alpha1.TemplatePolicyBinding
		wantNames []string
	}
	cases := []row{
		{
			name:      "empty folder namespace returns empty list",
			namespace: "holos-fld-empty",
		},
		{
			name:      "returns only bindings in requested namespace",
			namespace: "holos-fld-payments",
			seed: []*templatesv1alpha1.TemplatePolicyBinding{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bind-a", Namespace: "holos-fld-payments"},
					Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
						DisplayName: "A",
						PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
							Scope: "organization", ScopeName: "acme", Name: "require-http-route",
						},
						TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
							{
								Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
								Name:        "api",
								ProjectName: "payments-web",
							},
						},
					},
				},
				// Different namespace — must not be returned.
				{
					ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "holos-fld-identity"},
					Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
						DisplayName: "Other",
						PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
							Scope: "organization", ScopeName: "acme", Name: "require-http-route",
						},
						TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
							{
								Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
								Name:        "api",
								ProjectName: "identity-web",
							},
						},
					},
				},
			},
			wantNames: []string{"bind-a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ensureNamespace(t, e.Direct, tc.namespace, v1alpha2.ResourceTypeFolder)
			for _, b := range tc.seed {
				ensureNamespace(t, e.Direct, b.Namespace, v1alpha2.ResourceTypeFolder)
				if err := e.Direct.Create(context.Background(), b); err != nil {
					t.Fatalf("seed create: %v", err)
				}
				t.Cleanup(func() {
					_ = e.Direct.Delete(context.Background(), b)
				})
			}

			got := eventuallyListBindings(t, k, tc.namespace, len(tc.wantNames))
			if len(got) != len(tc.wantNames) {
				t.Fatalf("len(got)=%d want %d (items=%v)", len(got), len(tc.wantNames), bindingNames(got))
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("item %d: name=%q want %q", i, got[i].Name, want)
				}
			}
		})
	}
}

func TestGetBinding(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-get"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind-a", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Bind A",
			Description: "Describe me",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "acme", Name: "require-http-route",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "api",
					ProjectName: "payments-web",
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	_ = eventuallyGetBinding(t, k, ns, "bind-a")

	cases := []struct {
		name        string
		bindingName string
		wantErr     bool
		errIs       func(error) bool
	}{
		{name: "existing binding returns spec", bindingName: "bind-a"},
		{name: "missing binding surfaces NotFound", bindingName: "nope", wantErr: true, errIs: apierrors.IsNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.GetBinding(context.Background(), ns, tc.bindingName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errIs != nil && !tc.errIs(err) {
					t.Fatalf("unexpected error shape: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetBinding: %v", err)
			}
			if got.Name != tc.bindingName {
				t.Errorf("name=%q want %q", got.Name, tc.bindingName)
			}
			if got.Spec.DisplayName != "Bind A" {
				t.Errorf("displayName=%q want Bind A", got.Spec.DisplayName)
			}
		})
	}
}

func TestCreateBinding(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-create"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	cases := []struct {
		name         string
		resourceName string
		displayName  string
		description  string
		creatorEmail string
		policyRef    *consolev1.LinkedTemplatePolicyRef
		targetRefs   []*consolev1.TemplatePolicyBindingTargetRef
	}{
		{
			name:         "minimal fields persisted",
			resourceName: "minimal",
			displayName:  "Minimal",
			creatorEmail: "creator@example.com",
			policyRef:    samplePolicyRef(),
			targetRefs:   []*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()},
		},
		{
			name:         "project-template target persisted",
			resourceName: "project-template",
			displayName:  "Project Template",
			creatorEmail: "creator@example.com",
			policyRef:    samplePolicyRef(),
			targetRefs: []*consolev1.TemplatePolicyBindingTargetRef{
				{
					Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
					Name:        "shared-service",
					ProjectName: "payments-web",
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.CreateBinding(
				context.Background(), ns, tc.resourceName, tc.displayName, tc.description,
				tc.creatorEmail, tc.policyRef, tc.targetRefs,
			)
			if err != nil {
				t.Fatalf("CreateBinding: %v", err)
			}
			if got.Name != tc.resourceName {
				t.Errorf("name=%q want %q", got.Name, tc.resourceName)
			}

			// Creator annotation persisted for audit.
			if got.Annotations[v1alpha2.AnnotationCreatorEmail] != tc.creatorEmail {
				t.Errorf("creator annotation=%q want %q",
					got.Annotations[v1alpha2.AnnotationCreatorEmail], tc.creatorEmail)
			}

			// Read-your-own-write via direct client Get.
			read := &templatesv1alpha1.TemplatePolicyBinding{}
			if err := e.Direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: tc.resourceName}, read); err != nil {
				t.Fatalf("Get after Create: %v", err)
			}
			if read.Spec.DisplayName != tc.displayName {
				t.Errorf("displayName=%q want %q", read.Spec.DisplayName, tc.displayName)
			}
			if len(read.Spec.TargetRefs) != len(tc.targetRefs) {
				t.Errorf("targetRefs len=%d want %d", len(read.Spec.TargetRefs), len(tc.targetRefs))
			}
			if read.Spec.PolicyRef.Name != tc.policyRef.GetName() {
				t.Errorf("policyRef name=%q want %q", read.Spec.PolicyRef.Name, tc.policyRef.GetName())
			}
		})
	}
}

func TestUpdateBinding(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-update"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Before",
			Description: "before-desc",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "acme", Name: "require-http-route",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "api",
					ProjectName: "payments-web",
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	// UpdateBinding internally calls GetBinding via the cache-backed
	// client, so block until the seed has propagated before the first
	// Update.
	_ = eventuallyGetBinding(t, k, ns, "bind")

	// Display-only update preserves description and targets.
	newDisplay := "After"
	got, err := k.UpdateBinding(context.Background(), ns, "bind", &newDisplay, nil, nil, false, nil, false)
	if err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if got.Spec.DisplayName != "After" {
		t.Errorf("displayName=%q want After", got.Spec.DisplayName)
	}
	if got.Spec.Description != "before-desc" {
		t.Errorf("description=%q want before-desc (should be unchanged)", got.Spec.Description)
	}
	if len(got.Spec.TargetRefs) != 1 {
		t.Errorf("target_refs should be unchanged when updateTargetRefs=false, got %d", len(got.Spec.TargetRefs))
	}
	// Wait for the cache to catch up so the next UpdateBinding's internal
	// GetBinding sees the new ResourceVersion and doesn't trip the
	// optimistic-concurrency guard.
	eventuallyGetBindingAtResourceVersion(t, k, ns, "bind", got.ResourceVersion)

	// Replace policy_ref.
	newPolicyRef := scopeshim.NewLinkedTemplatePolicyRef(scopeshim.ScopeFolder, "payments", "new-policy")
	got2, err := k.UpdateBinding(context.Background(), ns, "bind", nil, nil, newPolicyRef, true, nil, false)
	if err != nil {
		t.Fatalf("UpdateBinding replace policy_ref: %v", err)
	}
	if got2.Spec.PolicyRef.Name != "new-policy" || got2.Spec.PolicyRef.Scope != "folder" {
		t.Errorf("policy_ref not replaced: %+v", got2.Spec.PolicyRef)
	}
	eventuallyGetBindingAtResourceVersion(t, k, ns, "bind", got2.ResourceVersion)

	// Clearing target_refs with an empty slice is rejected by the CRD
	// schema (MinItems=1 on spec.targetRefs). The handler forwards the
	// update; the API server returns Invalid. This pins the contract: a
	// binding must always name at least one target_ref, and the "clear"
	// path is not a valid operation — callers who want to detach a
	// binding from everything must delete the binding instead.
	_, err = k.UpdateBinding(context.Background(), ns, "bind", nil, nil, nil, false, []*consolev1.TemplatePolicyBindingTargetRef{}, true)
	if err == nil {
		t.Fatal("expected CRD validation error clearing target_refs (MinItems=1); got nil")
	}
	if !apierrors.IsInvalid(err) {
		t.Errorf("expected Invalid error from apiserver for empty target_refs; got %T %v", err, err)
	}
	// Replace target_refs with a single new target (non-empty, valid).
	newTargets := []*consolev1.TemplatePolicyBindingTargetRef{
		{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			Name:        "replaced",
			ProjectName: "lilies",
		},
	}
	got3, err := k.UpdateBinding(context.Background(), ns, "bind", nil, nil, nil, false, newTargets, true)
	if err != nil {
		t.Fatalf("UpdateBinding replace target_refs: %v", err)
	}
	if len(got3.Spec.TargetRefs) != 1 || got3.Spec.TargetRefs[0].Name != "replaced" {
		t.Errorf("target_refs should be replaced with [replaced], got %+v", got3.Spec.TargetRefs)
	}

	// nonexistent binding → error.
	_, err = k.UpdateBinding(context.Background(), ns, "missing", &newDisplay, nil, nil, false, nil, false)
	if err == nil {
		t.Fatal("expected error updating missing binding")
	}
}

func TestDeleteBinding(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-delete"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "goner", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			DisplayName: "Goner",
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Scope: "organization", ScopeName: "acme", Name: "require-http-route",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					Name:        "api",
					ProjectName: "payments-web",
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	_ = eventuallyGetBinding(t, k, ns, "goner")

	if err := k.DeleteBinding(context.Background(), ns, "goner"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	read := &templatesv1alpha1.TemplatePolicyBinding{}
	err := e.Direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "goner"}, read)
	if err == nil {
		t.Fatal("expected NotFound after delete")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error after delete: %v", err)
	}

	// deleting missing → error.
	if err := k.DeleteBinding(context.Background(), ns, "already-gone"); err == nil {
		t.Fatal("expected error deleting missing binding")
	}
}

// TestK8sClient_ListReflectsCreate is the cache-freshness regression. The
// K8sClient is wired with the manager's cache-backed client, so this
// test verifies:
//
//  1. Writes through the delegating client reach the API server.
//  2. The watch populating the informer cache propagates the new object
//     so a subsequent List from the cache reflects it.
//
// Without this guarantee, post-HOL-662 TemplatePolicyBinding reads would
// lag behind writes made by the same process.
func TestK8sClient_ListReflectsCreate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-cache"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	if _, err := k.CreateBinding(
		context.Background(), ns, "fresh", "Fresh", "", "creator@example.com",
		samplePolicyRef(), []*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()},
	); err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := k.ListBindings(context.Background(), ns)
		if err != nil {
			t.Fatalf("ListBindings: %v", err)
		}
		for _, b := range got {
			if b.Name == "fresh" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ListBindings never reflected Create within deadline")
}

// TestCreateBindingRejectedByAdmissionInProjectNamespace is the admission
// regression: the CEL ValidatingAdmissionPolicy shipped with the CRDs
// (HOL-618) rejects TemplatePolicyBinding writes into project-labelled
// namespaces. Admission rejection is the authoritative enforcement point,
// and this test locks in that the policy is installed and wired to the
// storage path.
//
// The test uses a namespace labelled ResourceType=project. The CEL VAP
// reads that label to classify the namespace and reject the write.
func TestCreateBindingRejectedByAdmissionInProjectNamespace(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-prj-billing-web"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeProject)

	_, err := k.CreateBinding(
		context.Background(), ns, "binding-test", "Test", "", "creator@example.com",
		samplePolicyRef(), []*consolev1.TemplatePolicyBindingTargetRef{sampleTargetRef()},
	)
	if err == nil {
		t.Fatal("expected CEL VAP rejection for project namespace write")
	}
	// The admission rejection must mention either the namespace or the
	// binding name — the exact wording is governed by the VAP bindings
	// in config/crd. A successful rejection comes back as an Invalid
	// status.
	if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
		t.Fatalf("expected admission-rejection error (Invalid or Forbidden), got %T: %v", err, err)
	}
}

// TestListBindingsInNamespaceRejectsEmpty verifies the namespace-direct
// variant refuses an empty namespace — a programming-error guard the
// ancestor walker relies on.
func TestListBindingsInNamespaceRejectsEmpty(t *testing.T) {
	_, k := newEnvtestK8sClient(t)
	if _, err := k.ListBindingsInNamespace(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty namespace")
	}
}

// TestPackageDoesNotCallProjectNamespace is the grep-based regression
// test called out by the HOL-554 acceptance criteria. It walks every Go
// source file in this package and fails if any file references
// Resolver.ProjectNamespace (the test itself intentionally contains only
// the literal substring in this comment; bare references in other files
// would still be caught because the test excludes the test file itself
// from the search).
func TestPackageDoesNotCallProjectNamespace(t *testing.T) {
	const target = "Resolver.ProjectNamespace"
	matches := []string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "k8s_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), target) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking package sources: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("package must not call %s — found in: %v", target, matches)
	}
}

// bindingNames collects a compact slice of TemplatePolicyBinding.Name
// values for debug output.
func bindingNames(bs []templatesv1alpha1.TemplatePolicyBinding) []string {
	out := make([]string, 0, len(bs))
	for i := range bs {
		out = append(out, bs[i].Name)
	}
	return out
}

// k8s_test.go exercises the HOL-662 rewrite of the TemplatePolicy CRUD
// surface against the TemplatePolicy CRD. Each CRUD test builds a
// K8sClient backed by the shared envtest bootstrap in
// console/crdmgr/testing (extracted in HOL-663) and exercises one
// operation table-driven.
//
// Cache freshness is covered by TestK8sClient_ListReflectsCreate, which
// creates a TemplatePolicy through the delegating client and asserts a
// subsequent List observes it within the resync window. The CEL
// ValidatingAdmissionPolicy that rejects writes into project namespaces
// (HOL-618) is regressed in TestCreatePolicyRejectedByAdmissionInProjectNamespace.
package templatepolicies

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

// sampleRule returns a minimal valid rule suitable for fixtures. HOL-600
// removed the glob-based Target; a rule is now (kind, template), and
// TemplatePolicyBinding carries the render-target selector.
func sampleRule() *consolev1.TemplatePolicyRule {
	return &consolev1.TemplatePolicyRule{
		Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE,
		Template: scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "reference-grant", ""),
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
// this suite depends on (templatepolicy-folder-or-org-only) to be
// registered so the admission-rejection regression does not race the
// CEL compiler.
func newEnvtestK8sClient(t *testing.T) (*crdmgrtesting.Env, *K8sClient) {
	t.Helper()
	env := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme:                   testScheme(t),
		InformerObjects:          []ctrlclient.Object{&templatesv1alpha1.TemplatePolicy{}},
		WaitForAdmissionPolicies: []string{"templatepolicy-folder-or-org-only"},
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

// eventuallyGetPolicy polls K8sClient.GetPolicy until it returns a match
// or the deadline expires. Used after a seed write through the direct
// client so tests observing through the cache-backed K8sClient tolerate
// the watch-propagation window.
func eventuallyGetPolicy(t *testing.T, k *K8sClient, namespace, name string) *templatesv1alpha1.TemplatePolicy {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, err := k.GetPolicy(context.Background(), namespace, name)
		if err == nil {
			return p
		}
		if !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected GetPolicy error for %q/%q: %v", namespace, name, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-backed GetPolicy did not observe %q/%q within deadline", namespace, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// eventuallyGetPolicyAtResourceVersion polls until the cache-backed
// GetPolicy returns an object whose ResourceVersion matches wantRV or
// the deadline expires. Used between sequential Updates in a test so the
// next Update's internal GetPolicy reads a fresh copy instead of a stale
// cached one and trips the apiserver's optimistic-concurrency guard
// ("the object has been modified; please apply ...").
func eventuallyGetPolicyAtResourceVersion(t *testing.T, k *K8sClient, namespace, name, wantRV string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, err := k.GetPolicy(context.Background(), namespace, name)
		if err == nil && p.ResourceVersion == wantRV {
			return
		}
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("unexpected GetPolicy error waiting for RV %q: %v", wantRV, err)
		}
		if time.Now().After(deadline) {
			got := ""
			if p != nil {
				got = p.ResourceVersion
			}
			t.Fatalf("cache did not observe policy %q/%q at RV %q within deadline (latest seen RV=%q)", namespace, name, wantRV, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// eventuallyListPolicies polls K8sClient.ListPolicies until it returns at
// least wantCount items or the deadline expires.
func eventuallyListPolicies(t *testing.T, k *K8sClient, namespace string, wantCount int) []templatesv1alpha1.TemplatePolicy {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := k.ListPolicies(context.Background(), namespace)
		if err != nil {
			t.Fatalf("ListPolicies error for %q: %v", namespace, err)
		}
		if len(got) >= wantCount {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache-backed ListPolicies did not observe %d policies in %q within deadline (got %d)",
				wantCount, namespace, len(got))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ------------------------------------------------------------------------
// Envtest table-driven CRUD tests.
// ------------------------------------------------------------------------

func TestListPolicies(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	type row struct {
		name      string
		namespace string
		seed      []*templatesv1alpha1.TemplatePolicy
		wantNames []string
	}
	cases := []row{
		{
			name:      "empty folder namespace returns empty list",
			namespace: "holos-fld-empty",
		},
		{
			name:      "returns only policies in requested namespace",
			namespace: "holos-fld-payments",
			seed: []*templatesv1alpha1.TemplatePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "require-httproute", Namespace: "holos-fld-payments"},
					Spec: templatesv1alpha1.TemplatePolicySpec{
						DisplayName: "Require HTTPRoute",
						Rules: []templatesv1alpha1.TemplatePolicyRule{
							{
								Kind: templatesv1alpha1.TemplatePolicyKindRequire,
								Template: templatesv1alpha1.LinkedTemplateRef{
									Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "httproute",
								},
							},
						},
					},
				},
				// Different namespace — must not be returned.
				{
					ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "holos-fld-identity"},
					Spec: templatesv1alpha1.TemplatePolicySpec{
						DisplayName: "Other",
						Rules: []templatesv1alpha1.TemplatePolicyRule{
							{
								Kind: templatesv1alpha1.TemplatePolicyKindRequire,
								Template: templatesv1alpha1.LinkedTemplateRef{
									Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "other-template",
								},
							},
						},
					},
				},
			},
			wantNames: []string{"require-httproute"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ensureNamespace(t, e.Direct, tc.namespace, v1alpha2.ResourceTypeFolder)
			for _, p := range tc.seed {
				ensureNamespace(t, e.Direct, p.Namespace, v1alpha2.ResourceTypeFolder)
				if err := e.Direct.Create(context.Background(), p); err != nil {
					t.Fatalf("seed create: %v", err)
				}
				t.Cleanup(func() {
					_ = e.Direct.Delete(context.Background(), p)
				})
			}

			got := eventuallyListPolicies(t, k, tc.namespace, len(tc.wantNames))
			if len(got) != len(tc.wantNames) {
				t.Fatalf("len(got)=%d want %d (items=%v)", len(got), len(tc.wantNames), policyNames(got))
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("item %d: name=%q want %q", i, got[i].Name, want)
				}
			}
		})
	}
}

func TestGetPolicy(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-get"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "require-httproute", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: "Require HTTPRoute",
			Description: "Force HTTPRoute for every project",
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				{
					Kind: templatesv1alpha1.TemplatePolicyKindRequire,
					Template: templatesv1alpha1.LinkedTemplateRef{
						Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "httproute",
					},
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	_ = eventuallyGetPolicy(t, k, ns, "require-httproute")

	cases := []struct {
		name       string
		policyName string
		wantErr    bool
		errIs      func(error) bool
	}{
		{name: "existing policy returns spec", policyName: "require-httproute"},
		{name: "missing policy surfaces NotFound", policyName: "nope", wantErr: true, errIs: apierrors.IsNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.GetPolicy(context.Background(), ns, tc.policyName)
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
				t.Fatalf("GetPolicy: %v", err)
			}
			if got.Name != tc.policyName {
				t.Errorf("name=%q want %q", got.Name, tc.policyName)
			}
			if got.Spec.DisplayName != "Require HTTPRoute" {
				t.Errorf("displayName=%q want Require HTTPRoute", got.Spec.DisplayName)
			}
		})
	}
}

func TestCreatePolicy(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-create"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	cases := []struct {
		name         string
		resourceName string
		displayName  string
		description  string
		creatorEmail string
		rules        []*consolev1.TemplatePolicyRule
	}{
		{
			name:         "minimal fields persisted",
			resourceName: "minimal",
			displayName:  "Minimal",
			creatorEmail: "creator@example.com",
			rules:        []*consolev1.TemplatePolicyRule{sampleRule()},
		},
		{
			name:         "exclude rule persisted",
			resourceName: "exclude",
			displayName:  "Exclude",
			creatorEmail: "creator@example.com",
			rules: []*consolev1.TemplatePolicyRule{
				{
					Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE,
					Template: scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "legacy-httproute", ""),
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.CreatePolicy(
				context.Background(), ns, tc.resourceName, tc.displayName, tc.description,
				tc.creatorEmail, tc.rules,
			)
			if err != nil {
				t.Fatalf("CreatePolicy: %v", err)
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
			read := &templatesv1alpha1.TemplatePolicy{}
			if err := e.Direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: tc.resourceName}, read); err != nil {
				t.Fatalf("Get after Create: %v", err)
			}
			if read.Spec.DisplayName != tc.displayName {
				t.Errorf("displayName=%q want %q", read.Spec.DisplayName, tc.displayName)
			}
			if len(read.Spec.Rules) != len(tc.rules) {
				t.Errorf("rules len=%d want %d", len(read.Spec.Rules), len(tc.rules))
			}
		})
	}
}

func TestUpdatePolicy(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-update"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pol", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: "Before",
			Description: "before-desc",
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				{
					Kind: templatesv1alpha1.TemplatePolicyKindRequire,
					Template: templatesv1alpha1.LinkedTemplateRef{
						Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "reference-grant",
					},
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	// UpdatePolicy internally calls GetPolicy via the cache-backed client,
	// so block until the seed has propagated before the first Update.
	_ = eventuallyGetPolicy(t, k, ns, "pol")

	newDisplay := "After"
	got, err := k.UpdatePolicy(context.Background(), ns, "pol", &newDisplay, nil, nil, false)
	if err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if got.Spec.DisplayName != "After" {
		t.Errorf("displayName=%q want After", got.Spec.DisplayName)
	}
	if got.Spec.Description != "before-desc" {
		t.Errorf("description=%q want before-desc (should be unchanged)", got.Spec.Description)
	}
	if len(got.Spec.Rules) != 1 {
		t.Errorf("rules should be unchanged when updateRules=false, got %d", len(got.Spec.Rules))
	}
	// Wait for the cache to catch up so the next UpdatePolicy's internal
	// GetPolicy sees the new ResourceVersion and doesn't trip the
	// optimistic-concurrency guard.
	eventuallyGetPolicyAtResourceVersion(t, k, ns, "pol", got.ResourceVersion)

	// Now replace rules too.
	newRules := []*consolev1.TemplatePolicyRule{
		{
			Kind:     consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE,
			Template: scopeshim.NewLinkedTemplateRef(scopeshim.ScopeOrganization, "acme", "legacy", ""),
		},
	}
	got2, err := k.UpdatePolicy(context.Background(), ns, "pol", nil, nil, newRules, true)
	if err != nil {
		t.Fatalf("UpdatePolicy with rules: %v", err)
	}
	if len(got2.Spec.Rules) != 1 || got2.Spec.Rules[0].Kind != templatesv1alpha1.TemplatePolicyKindExclude {
		t.Errorf("rules not replaced: %+v", got2.Spec.Rules)
	}

	// nonexistent policy → error.
	_, err = k.UpdatePolicy(context.Background(), ns, "missing", &newDisplay, nil, nil, false)
	if err == nil {
		t.Fatal("expected error updating missing policy")
	}
}

func TestDeletePolicy(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-delete"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	seed := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "goner", Namespace: ns},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: "Goner",
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				{
					Kind: templatesv1alpha1.TemplatePolicyKindRequire,
					Template: templatesv1alpha1.LinkedTemplateRef{
						Scope: v1alpha2.TemplateScopeOrganization, ScopeName: "acme", Name: "t",
					},
				},
			},
		},
	}
	if err := e.Direct.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	_ = eventuallyGetPolicy(t, k, ns, "goner")

	if err := k.DeletePolicy(context.Background(), ns, "goner"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	read := &templatesv1alpha1.TemplatePolicy{}
	err := e.Direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "goner"}, read)
	if err == nil {
		t.Fatal("expected NotFound after delete")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error after delete: %v", err)
	}

	// deleting missing → error.
	if err := k.DeletePolicy(context.Background(), ns, "already-gone"); err == nil {
		t.Fatal("expected error deleting missing policy")
	}
}

// TestK8sClient_ListReflectsCreate is the cache-freshness regression. The
// K8sClient is wired with the manager's cache-backed client, so this test
// verifies:
//
//  1. Writes through the delegating client reach the API server.
//  2. The watch populating the informer cache propagates the new object
//     so a subsequent List from the cache reflects it.
//
// Without this guarantee, post-HOL-662 TemplatePolicy reads would lag
// behind writes made by the same process.
func TestK8sClient_ListReflectsCreate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-fld-cache"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeFolder)

	if _, err := k.CreatePolicy(
		context.Background(), ns, "fresh", "Fresh", "", "creator@example.com",
		[]*consolev1.TemplatePolicyRule{sampleRule()},
	); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := k.ListPolicies(context.Background(), ns)
		if err != nil {
			t.Fatalf("ListPolicies: %v", err)
		}
		for _, p := range got {
			if p.Name == "fresh" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ListPolicies never reflected Create within deadline")
}

// TestCreatePolicyRejectedByAdmissionInProjectNamespace is the admission
// regression: the CEL ValidatingAdmissionPolicy shipped with the CRDs
// (HOL-618) rejects TemplatePolicy writes into project-labelled
// namespaces. Admission rejection is the authoritative enforcement point,
// and this test locks in that the policy is installed and wired to the
// storage path.
//
// The test uses a namespace labelled ResourceType=project. The CEL VAP
// reads that label to classify the namespace and reject the write.
func TestCreatePolicyRejectedByAdmissionInProjectNamespace(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "holos-prj-billing-web"
	ensureNamespace(t, e.Direct, ns, v1alpha2.ResourceTypeProject)

	_, err := k.CreatePolicy(
		context.Background(), ns, "policy-test", "Test", "", "creator@example.com",
		[]*consolev1.TemplatePolicyRule{sampleRule()},
	)
	if err == nil {
		t.Fatal("expected CEL VAP rejection for project namespace write")
	}
	// The admission rejection must mention either the namespace or the
	// policy name — the exact wording is governed by the VAP bindings in
	// config/crd. A successful rejection comes back as an Invalid status.
	if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
		t.Fatalf("expected admission-rejection error (Invalid or Forbidden), got %T: %v", err, err)
	}
}

// TestPackageDoesNotCallProjectNamespace is the grep-based regression test
// called out by the HOL-556 acceptance criteria. It walks every Go source
// file in this package and fails if any file references
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

// policyNames collects a compact slice of TemplatePolicy.Name values for
// debug output.
func policyNames(pols []templatesv1alpha1.TemplatePolicy) []string {
	out := make([]string, 0, len(pols))
	for i := range pols {
		out = append(out, pols[i].Name)
	}
	return out
}

// k8s_test.go exercises the HOL-661 rewrite of the Template CRUD surface
// against the Template CRD. The tests run inline-envtest style: each test
// starts its own envtest.Environment with the templates.holos.run CRDs
// installed (shared-envtest extraction is the HOL-663 follow-up), builds a
// K8sClient backed by a direct controller-runtime client, and exercises one
// CRUD operation table-driven.
//
// Cache freshness is covered by TestK8sClient_ListReflectsCreate, which
// creates a Template and asserts a subsequent List reflects it within the
// resync window. The remaining fake-client tests (ListEffectiveTemplateSources,
// LinkedTemplatesAnnotation) continue to run against a fake
// controller-runtime client because their inputs are still expressed as
// ConfigMap fixtures and the bridge in testhelpers_test.go materializes
// them into CRs — this keeps HOL-661's blast radius inside k8s.go and
// k8s_test.go while the surrounding packages wait for HOL-662/HOL-663.
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// testResolver returns the canonical resolver every test in this package
// shares. Namespace prefixes match the defaults used in production wiring so
// namespace strings round-trip through scopeshim.FromNamespace in tests.
func testResolver() *resolver.Resolver {
	return &resolver.Resolver{OrganizationPrefix: "org-", FolderPrefix: "fld-", ProjectPrefix: "prj-"}
}

var (
	projectScope = scopeshim.ScopeProject
	orgScope     = scopeshim.ScopeOrganization
	folderScope  = scopeshim.ScopeFolder
)

// orgNS / folderNS / projectNS build v1alpha2-labeled Namespace fixtures so
// fake.Clientset reads and the render-time ancestor walker agree on the
// resource-type label.
func orgNS(org string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-" + org,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
				v1alpha2.LabelOrganization: org,
			},
		},
	}
}

func folderNS(folder string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fld-" + folder,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeFolder,
				v1alpha2.LabelFolder:       folder,
			},
		},
	}
}

func projectNS(project string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prj-" + project,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      project,
			},
		},
	}
}

// templateConfigMap / projectTemplateConfigMap / orgTemplateConfigMap /
// folderTemplateConfigMap remain in place so handler-level tests continue to
// compile. HOL-661 rewrote the storage substrate but kept these fixture
// helpers intact; the testhelpers_test.go bridge converts them into Template
// CRDs for the rewritten K8sClient.
func templateConfigMap(scope scopeshim.Scope, scopePrefix, scopeName, name, displayName, description, cueTemplate string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scopePrefix + scopeName,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationDisplayName: displayName,
				v1alpha2.AnnotationDescription: description,
				v1alpha2.AnnotationEnabled:     "false",
			},
		},
		Data: map[string]string{
			CueTemplateKey: cueTemplate,
		},
	}
}

func projectTemplateConfigMap(project, name, displayName, description, cueTemplate string) *corev1.ConfigMap {
	return templateConfigMap(scopeshim.ScopeProject, "prj-", project, name, displayName, description, cueTemplate)
}

// orgTemplateConfigMap builds a fixture for an org-scope template. The first
// boolean was the pre-HOL-565 "mandatory" toggle and is ignored; the second
// controls the enabled annotation.
func orgTemplateConfigMap(org, name, displayName, description, cueTemplate string, _ bool, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(scopeshim.ScopeOrganization, "org-", org, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationEnabled] = boolStr(enabled)
	return cm
}

// folderTemplateConfigMap builds a fixture for a folder-scope template. See
// orgTemplateConfigMap for the first-boolean rationale.
func folderTemplateConfigMap(folder, name, displayName, description, cueTemplate string, _ bool, enabled bool) *corev1.ConfigMap {
	cm := templateConfigMap(scopeshim.ScopeFolder, "fld-", folder, name, displayName, description, cueTemplate)
	cm.Annotations[v1alpha2.AnnotationEnabled] = boolStr(enabled)
	return cm
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// envtestEnv wraps an envtest.Environment + direct client + rest config so
// every CRUD test spins up its own isolated API server. One Environment per
// test keeps tests independent when they need custom resolver settings or
// different CRD fixtures — the shared-env helper comes in HOL-663.
type envtestEnv struct {
	env    *envtest.Environment
	cfg    *rest.Config
	client ctrlclient.Client
	core   kubernetes.Interface
}

// startEnvtest boots envtest with the templates.holos.run CRDs installed and
// returns a direct controller-runtime client + a client-go Interface. Skips
// (does not fail) when envtest binaries are not installed so developers
// without `setup-envtest use` can still run `go test ./...`.
func startEnvtest(t *testing.T) *envtestEnv {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if assets := detectEnvtestAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found; run `setup-envtest use` to download")
		}
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	e := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crd")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := e.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := e.Stop(); stopErr != nil {
			t.Logf("stopping envtest: %v", stopErr)
		}
	})

	scheme := testScheme(t)
	cl, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("constructing direct client: %v", err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("constructing core client: %v", err)
	}
	return &envtestEnv{env: e, cfg: cfg, client: cl, core: core}
}

// newEnvtestK8sClient builds a K8sClient backed by an envtest API server.
// Every test that needs to assert on real apiserver semantics (Create
// conflict handling, cache freshness after create, namespace scoping) uses
// this helper.
func newEnvtestK8sClient(t *testing.T) (*envtestEnv, *K8sClient) {
	t.Helper()
	e := startEnvtest(t)
	return e, NewK8sClient(e.core, e.client, testResolver())
}

// ensureNamespace creates a namespace if it does not already exist.
func ensureNamespace(t *testing.T, c ctrlclient.Client, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := c.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

// ------------------------------------------------------------------------
// Envtest table-driven CRUD tests.
// ------------------------------------------------------------------------

func TestListTemplates(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	type row struct {
		name       string
		namespace  string
		seed       []*templatesv1alpha1.Template
		wantNames  []string
		wantNonNil bool
	}
	cases := []row{
		{
			name:      "empty namespace returns empty list",
			namespace: "prj-empty",
		},
		{
			name:      "returns only templates in requested namespace",
			namespace: "prj-target",
			seed: []*templatesv1alpha1.Template{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: "prj-target"},
					Spec: templatesv1alpha1.TemplateSpec{
						DisplayName: "Web App", CueTemplate: "package holos\n",
					},
				},
				// Different namespace — must not be returned.
				{
					ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "prj-other"},
					Spec: templatesv1alpha1.TemplateSpec{
						DisplayName: "Other", CueTemplate: "package holos\n",
					},
				},
			},
			wantNames: []string{"web-app"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ensureNamespace(t, e.client, tc.namespace)
			for _, tmpl := range tc.seed {
				ensureNamespace(t, e.client, tmpl.Namespace)
				if err := e.client.Create(context.Background(), tmpl); err != nil {
					t.Fatalf("seed create: %v", err)
				}
				t.Cleanup(func() {
					_ = e.client.Delete(context.Background(), tmpl)
				})
			}

			got, err := k.ListTemplates(context.Background(), tc.namespace)
			if err != nil {
				t.Fatalf("ListTemplates: %v", err)
			}
			if len(got) != len(tc.wantNames) {
				t.Fatalf("len(got)=%d want %d (items=%v)", len(got), len(tc.wantNames), names(got))
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("item %d: name=%q want %q", i, got[i].Name, want)
				}
			}
		})
	}
}

func TestGetTemplate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-get"
	ensureNamespace(t, e.client, ns)

	seed := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: ns},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: "Web App",
			Description: "A web app",
			CueTemplate: "package holos\n",
			Enabled:     true,
		},
	}
	if err := e.client.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	cases := []struct {
		name     string
		tmplName string
		wantErr  bool
		errIs    func(error) bool
	}{
		{name: "existing template returns spec", tmplName: "web-app"},
		{name: "missing template surfaces NotFound", tmplName: "nope", wantErr: true, errIs: apierrors.IsNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.GetTemplate(context.Background(), ns, tc.tmplName)
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
				t.Fatalf("GetTemplate: %v", err)
			}
			if got.Name != tc.tmplName {
				t.Errorf("name=%q want %q", got.Name, tc.tmplName)
			}
			if got.Spec.DisplayName != "Web App" {
				t.Errorf("displayName=%q want Web App", got.Spec.DisplayName)
			}
		})
	}
}

func TestCreateTemplate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-create"
	ensureNamespace(t, e.client, ns)

	cases := []struct {
		name            string
		resourceName    string
		displayName     string
		description     string
		cueTemplate     string
		defaults        *consolev1.TemplateDefaults
		enabled         bool
		linkedTemplates []*consolev1.LinkedTemplateRef
	}{
		{
			name:         "minimal fields persisted",
			resourceName: "minimal",
			displayName:  "Minimal",
			cueTemplate:  "package holos\n",
		},
		{
			name:         "defaults stored in spec",
			resourceName: "with-defaults",
			displayName:  "With Defaults",
			cueTemplate:  "package holos\n",
			defaults: &consolev1.TemplateDefaults{
				Image: "ghcr.io/example/app",
				Tag:   "v1.0",
			},
		},
		{
			name:         "enabled + linked refs stored in spec",
			resourceName: "enabled",
			displayName:  "Enabled",
			cueTemplate:  "package holos\n",
			enabled:      true,
			linkedTemplates: []*consolev1.LinkedTemplateRef{
				scopeshim.NewLinkedTemplateRef(orgScope, "acme", "httproute", ""),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := k.CreateTemplate(
				context.Background(), ns, tc.resourceName, tc.displayName, tc.description,
				tc.cueTemplate, tc.defaults, tc.enabled, tc.linkedTemplates,
			)
			if err != nil {
				t.Fatalf("CreateTemplate: %v", err)
			}
			if got.Name != tc.resourceName {
				t.Errorf("name=%q want %q", got.Name, tc.resourceName)
			}

			// Read-your-own-write via direct client Get.
			read := &templatesv1alpha1.Template{}
			if err := e.client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: tc.resourceName}, read); err != nil {
				t.Fatalf("Get after Create: %v", err)
			}
			if read.Spec.DisplayName != tc.displayName {
				t.Errorf("displayName=%q want %q", read.Spec.DisplayName, tc.displayName)
			}
			if read.Spec.Enabled != tc.enabled {
				t.Errorf("enabled=%v want %v", read.Spec.Enabled, tc.enabled)
			}
			if tc.defaults != nil && read.Spec.Defaults == nil {
				t.Errorf("expected defaults to be persisted")
			}
			if len(tc.linkedTemplates) != len(read.Spec.LinkedTemplates) {
				t.Errorf("linkedTemplates len=%d want %d", len(read.Spec.LinkedTemplates), len(tc.linkedTemplates))
			}
		})
	}
}

func TestUpdateTemplate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-update"
	ensureNamespace(t, e.client, ns)

	seed := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: ns},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: "Before", Description: "before-desc", CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	newDisplay := "After"
	got, err := k.UpdateTemplate(context.Background(), ns, "tmpl", &newDisplay, nil, nil, nil, false, nil, nil, false)
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if got.Spec.DisplayName != "After" {
		t.Errorf("displayName=%q want After", got.Spec.DisplayName)
	}
	if got.Spec.Description != "before-desc" {
		t.Errorf("description=%q want before-desc (should be unchanged)", got.Spec.Description)
	}

	// nonexistent template → error.
	_, err = k.UpdateTemplate(context.Background(), ns, "missing", &newDisplay, nil, nil, nil, false, nil, nil, false)
	if err == nil {
		t.Fatal("expected error updating missing template")
	}
}

func TestDeleteTemplate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-delete"
	ensureNamespace(t, e.client, ns)

	seed := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "goner", Namespace: ns},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: "Goner", CueTemplate: "package holos\n",
		},
	}
	if err := e.client.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	if err := k.DeleteTemplate(context.Background(), ns, "goner"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	read := &templatesv1alpha1.Template{}
	err := e.client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "goner"}, read)
	if err == nil {
		t.Fatal("expected NotFound after delete")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error after delete: %v", err)
	}

	// deleting missing → error.
	if err := k.DeleteTemplate(context.Background(), ns, "already-gone"); err == nil {
		t.Fatal("expected error deleting missing template")
	}
}

// TestK8sClient_ListReflectsCreate is the cache-freshness regression the
// ticket calls out specifically. After Create, a subsequent List on the same
// namespace must reflect the new object without any manual resync nudging.
func TestK8sClient_ListReflectsCreate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-cache"
	ensureNamespace(t, e.client, ns)

	if _, err := k.CreateTemplate(
		context.Background(), ns, "fresh", "Fresh", "", "package holos\n",
		nil, false, nil,
	); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// The direct client is not cache-backed, so List must see the new
	// object on the very next call. We still wrap in Eventually because
	// envtest apiserver writes have a tiny propagation window.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := k.ListTemplates(context.Background(), ns)
		if err != nil {
			t.Fatalf("ListTemplates: %v", err)
		}
		for _, tmpl := range got {
			if tmpl.Name == "fresh" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ListTemplates never reflected Create within deadline")
}

// TestCloneTemplate drives the CRD Clone path and verifies the clone lands
// disabled with the source's CUE.
func TestCloneTemplate(t *testing.T) {
	e, k := newEnvtestK8sClient(t)

	ns := "prj-clone"
	ensureNamespace(t, e.client, ns)

	seed := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: "src", Namespace: ns},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: "Src", Description: "desc", CueTemplate: "package holos\nfoo: true\n", Enabled: true,
		},
	}
	if err := e.client.Create(context.Background(), seed); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	got, err := k.CloneTemplate(context.Background(), ns, "src", "src-copy", "Src Copy")
	if err != nil {
		t.Fatalf("CloneTemplate: %v", err)
	}
	if got.Name != "src-copy" {
		t.Errorf("name=%q want src-copy", got.Name)
	}
	if got.Spec.DisplayName != "Src Copy" {
		t.Errorf("displayName=%q want 'Src Copy'", got.Spec.DisplayName)
	}
	if got.Spec.Description != "desc" {
		t.Errorf("description=%q want desc", got.Spec.Description)
	}
	if got.Spec.CueTemplate != "package holos\nfoo: true\n" {
		t.Errorf("cueTemplate did not copy from source")
	}
	if got.Spec.Enabled {
		t.Error("clone should start disabled")
	}
}

// ------------------------------------------------------------------------
// ListEffectiveTemplateSources tests — still driven by ConfigMap fixtures
// bridged through testhelpers_test.go's newTestK8sClient because they
// exercise the render-time resolver which joins ancestor walk + per-namespace
// List. The fake controller-runtime client is enough for their reads.
// ------------------------------------------------------------------------

// stubHierarchyWalker implements RenderHierarchyWalker for testing
// ListEffectiveTemplateSources.
type stubHierarchyWalker struct {
	ancestors []*corev1.Namespace
	err       error
}

func (s *stubHierarchyWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	return s.ancestors, s.err
}

// folderLinkedRefWithConstraint builds a folder-scope LinkedTemplateRef with a version constraint.
func folderLinkedRefWithConstraint(folder, name, constraint string) *consolev1.LinkedTemplateRef {
	return scopeshim.NewLinkedTemplateRef(scopeshim.ScopeFolder, folder, name, constraint)
}

// TestListEffectiveTemplateSources exercises the unified ancestor-source
// helper that replaced the legacy per-scope helpers in HOL-564. HOL-661
// retained the contract; only the storage substrate changed, so every
// assertion here continues to cover the render-time effective-ref surface.
func TestListEffectiveTemplateSources(t *testing.T) {
	orgNsObj := orgNS("my-org")
	fldNsObj := folderNS("payments")
	prjNsObj := projectNS("my-project")
	fullAncestors := []*corev1.Namespace{prjNsObj, fldNsObj, orgNsObj}

	t.Run("nil walker returns no sources", func(t *testing.T) {
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj), testResolver())

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, nil, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources with nil walker, got %d", len(sources))
		}
	})

	t.Run("folder-only linked refs resolves from folder namespace", func(t *testing.T) {
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(folderScope, "payments", "payments-policy", ""),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != folderCue {
			t.Errorf("expected %q, got %q", folderCue, sources[0])
		}
	})

	t.Run("mixed org+folder linked refs resolves from both namespaces", func(t *testing.T) {
		orgCue := "// org httproute"
		orgCM := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", orgCue, false, true)
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, false, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(orgScope, "my-org", "httproute", ""),
			scopeshim.NewLinkedTemplateRef(folderScope, "payments", "payments-policy", ""),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(sources))
		}
	})

	t.Run("folder template with legacy mandatory annotation is NOT auto-included", func(t *testing.T) {
		mandatoryCue := "// mandatory folder template"
		fldCM := folderTemplateConfigMap("payments", "audit-policy", "Audit Policy", "", mandatoryCue, true, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources after HOL-565 removed mandatory auto-inclusion, got %d", len(sources))
		}
	})

	t.Run("disabled folder template excluded even when linked", func(t *testing.T) {
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", "// disabled", false, false)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(folderScope, "payments", "payments-policy", ""),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected 0 sources for disabled template, got %d", len(sources))
		}
	})

	t.Run("version-constrained folder linked ref resolved from release", func(t *testing.T) {
		liveCue := "// live folder template"
		releaseCue := "// folder release 1.0.0"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", liveCue, false, true)

		v, _ := ParseVersion("1.0.0")
		releaseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ReleaseConfigMapName("payments-policy", v),
				Namespace: "fld-payments",
				Labels: map[string]string{
					v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
					v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
					v1alpha2.LabelReleaseOf:     "payments-policy",
					v1alpha2.LabelTemplateScope: v1alpha2.TemplateScopeFolder,
				},
				Annotations: map[string]string{
					v1alpha2.AnnotationTemplateVersion: "1.0.0",
				},
			},
			Data: map[string]string{CueTemplateKey: releaseCue},
		}
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM, releaseCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			folderLinkedRefWithConstraint("payments", "payments-policy", ">=1.0.0"),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(sources))
		}
		if sources[0] != releaseCue {
			t.Errorf("expected release CUE %q, got %q", releaseCue, sources[0])
		}
	})

	t.Run("walker failure degrades gracefully with empty sources", func(t *testing.T) {
		k8s := newTestK8sClient(t, fake.NewClientset(), testResolver())
		walker := &stubHierarchyWalker{err: fmt.Errorf("walk failed")}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(folderScope, "payments", "payments-policy", ""),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("expected graceful degradation, got error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources on walker failure, got %d", len(sources))
		}
	})

	t.Run("no linked refs and no mandatory templates returns empty", func(t *testing.T) {
		fldCM := folderTemplateConfigMap("payments", "optional", "Optional", "", "// optional", false, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", nil, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 0 {
			t.Errorf("expected 0 sources, got %d", len(sources))
		}
	})

	t.Run("dedup key is (scope, scopeName, name) across scopes", func(t *testing.T) {
		sharedName := "shared"
		orgCue := "// org shared"
		folderCue := "// folder shared"
		orgCM := orgTemplateConfigMap("my-org", sharedName, "OrgShared", "", orgCue, false, true)
		fldCM := folderTemplateConfigMap("payments", sharedName, "FolderShared", "", folderCue, false, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(orgScope, "my-org", sharedName, ""),
			scopeshim.NewLinkedTemplateRef(folderScope, "payments", sharedName, ""),
		}
		sources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources (both scopes of the same name), got %d", len(sources))
		}
		got := map[string]bool{sources[0]: true, sources[1]: true}
		if !got[orgCue] || !got[folderCue] {
			t.Errorf("expected both org and folder sources, got %v", sources)
		}
	})

	t.Run("TargetKind does not alter resolution in Phase 2", func(t *testing.T) {
		orgCue := "// org httproute"
		orgCM := orgTemplateConfigMap("my-org", "httproute", "HTTPRoute", "", orgCue, false, true)
		folderCue := "// folder payments policy"
		fldCM := folderTemplateConfigMap("payments", "payments-policy", "Payments Policy", "", folderCue, true, true)
		k8s := newTestK8sClient(t, fake.NewClientset(orgNsObj, fldNsObj, prjNsObj, orgCM, fldCM), testResolver())
		walker := &stubHierarchyWalker{ancestors: fullAncestors}

		refs := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(orgScope, "my-org", "httproute", ""),
		}

		deploymentSources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindDeployment, "dep", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error (deployment): %v", err)
		}
		projectSources, _, err := k8s.ListEffectiveTemplateSources(context.Background(), "prj-my-project", TargetKindProjectTemplate, "tmpl", refs, walker, policyresolver.NewNoopResolver())
		if err != nil {
			t.Fatalf("unexpected error (project template): %v", err)
		}
		if len(deploymentSources) != len(projectSources) {
			t.Fatalf("preview-vs-apply slice length drift: deployment=%d projectTemplate=%d", len(deploymentSources), len(projectSources))
		}
		for i := range deploymentSources {
			if deploymentSources[i] != projectSources[i] {
				t.Errorf("preview-vs-apply drift at index %d: %q vs %q", i, deploymentSources[i], projectSources[i])
			}
		}
	})
}

// TestLinkedTemplatesAnnotation covers CreateTemplate's linked-refs handling
// through the CRD spec (post-HOL-661 the annotation round-trip is gone, but
// the bridged fixtures make the same assertions). The bridge still round-
// trips through the JSON annotation path to make sure
// unmarshalLinkedTemplates retains its public shape — the Release-rendering
// path depends on it.
func TestLinkedTemplatesAnnotation(t *testing.T) {
	_, k := newEnvtestK8sClient(t)

	ns := "prj-links"
	ctx := context.Background()

	t.Run("CreateTemplate stores linked refs in spec", func(t *testing.T) {
		ensureNamespace(t, k.client.(interface {
			Get(context.Context, ctrlclient.ObjectKey, ctrlclient.Object, ...ctrlclient.GetOption) error
		}).(ctrlclient.Client), ns)

		linked := []*consolev1.LinkedTemplateRef{
			scopeshim.NewLinkedTemplateRef(orgScope, "acme", "httproute", ""),
			scopeshim.NewLinkedTemplateRef(orgScope, "acme", "policy-floor", ""),
		}
		tmpl, err := k.CreateTemplate(ctx, ns, "web-app", "Web App", "desc", "package holos\n", nil, false, linked)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tmpl.Spec.LinkedTemplates) != 2 {
			t.Fatalf("expected 2 linked templates, got %d", len(tmpl.Spec.LinkedTemplates))
		}
		if tmpl.Spec.LinkedTemplates[0].Name != "httproute" {
			t.Errorf("expected 'httproute', got %q", tmpl.Spec.LinkedTemplates[0].Name)
		}
	})

	t.Run("CreateTemplate with nil linked list leaves spec empty", func(t *testing.T) {
		tmpl, err := k.CreateTemplate(ctx, ns, "no-links", "No Links", "desc", "package holos\n", nil, false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tmpl.Spec.LinkedTemplates) != 0 {
			t.Errorf("expected empty linked list, got %d entries", len(tmpl.Spec.LinkedTemplates))
		}
	})
}

// Ensure defaults serialize through the DefaultsKey JSON path the test
// helpers use. Covers the DefaultsKey-read path in configMapToTemplateCRD.
func TestDefaultsJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(&consolev1.TemplateDefaults{Image: "ghcr.io/app", Tag: "1.0"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "prj-x"},
		Data:       map[string]string{DefaultsKey: string(raw)},
	}
	tmpl := configMapToTemplateCRD(cm)
	if tmpl.Spec.Defaults == nil {
		t.Fatalf("expected defaults")
	}
	if tmpl.Spec.Defaults.Image != "ghcr.io/app" {
		t.Errorf("image=%q want ghcr.io/app", tmpl.Spec.Defaults.Image)
	}
}

// ------------------------------------------------------------------------
// envtest helpers — detectEnvtestAssets + findRepoRoot mirror the copies in
// internal/controller/suite_test.go and api/templates/v1alpha1/crd_test.go.
// HOL-663 will extract a shared helper; for now we duplicate so the
// templates package has zero test-only dependency on the other suites.
// ------------------------------------------------------------------------

func detectEnvtestAssets() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	base := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	for _, en := range entries {
		if !en.IsDir() {
			continue
		}
		cand := filepath.Join(base, en.Name())
		if _, err := os.Stat(filepath.Join(cand, "kube-apiserver")); err == nil {
			if best == "" || en.Name() > filepath.Base(best) {
				best = cand
			}
		}
	}
	return best
}

func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %q", file)
		}
		dir = parent
	}
}

// names collects a compact slice of Template.Name values for debug output.
func names(tmpls []templatesv1alpha1.Template) []string {
	out := make([]string, 0, len(tmpls))
	for i := range tmpls {
		out = append(out, tmpls[i].Name)
	}
	return out
}

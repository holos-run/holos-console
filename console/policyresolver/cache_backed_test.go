// cache_backed_test.go exercises the HOL-622 cache-backed read path: a
// PolicyListerInNamespace / BindingListerInNamespace implementation that
// reads through a controller-runtime client.Client feeds the real
// folderResolver end-to-end.
//
// The test uses sigs.k8s.io/controller-runtime/pkg/client/fake to build an
// in-memory client.Client. Production wires the manager's delegating
// (cache-backed) client — reads land in the informer cache, writes fall
// through to the apiserver and surface in the cache on the next watch
// event. The fake client is a stand-in for that shape: List with
// InNamespace(ns) returns every object previously Create'd under that
// namespace.
//
// Crucially, this covers the interface-seam contract: the resolver never
// sees a ConfigMap (HOL-662 migrated it to a typed CRD) and never reaches
// past the cache (HOL-622 routes every render-time list through the
// controller-runtime client). If a future refactor accidentally brings
// back a ConfigMap path or a direct REST call, the test breaks at compile
// time.

package policyresolver

import (
	"context"
	"fmt"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// cacheBackedPolicyLister implements PolicyListerInNamespace by calling
// List on a controller-runtime client.Client. This mirrors the production
// K8sClient in console/templatepolicies without importing it (which would
// create a cycle: templatepolicies depends on policyresolver via its
// handler for the drift wire-up).
type cacheBackedPolicyLister struct {
	c ctrlclient.Client
}

func (p *cacheBackedPolicyLister) ListPoliciesInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicy, error) {
	var list templatesv1alpha1.TemplatePolicyList
	if err := p.c.List(ctx, &list, ctrlclient.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("listing template policies in %q: %w", ns, err)
	}
	out := make([]*templatesv1alpha1.TemplatePolicy, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, nil
}

// cacheBackedBindingLister mirrors cacheBackedPolicyLister for
// TemplatePolicyBinding objects.
type cacheBackedBindingLister struct {
	c ctrlclient.Client
}

func (b *cacheBackedBindingLister) ListBindingsInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplatePolicyBinding, error) {
	var list templatesv1alpha1.TemplatePolicyBindingList
	if err := b.c.List(ctx, &list, ctrlclient.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("listing template policy bindings in %q: %w", ns, err)
	}
	out := make([]*templatesv1alpha1.TemplatePolicyBinding, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, nil
}

// cacheBackedTestScheme registers the types the ctrlfake client needs. core
// gets us Namespace; templates gets us TemplatePolicy /
// TemplatePolicyBinding.
func cacheBackedTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("register clientgo scheme: %v", err)
	}
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register templates scheme: %v", err)
	}
	return s
}

// TestFolderResolver_CacheBackedReadPath exercises the end-to-end cache
// path: a real ctrlclient.Client (fake build) holds CRD objects, the
// cacheBackedPolicyLister / cacheBackedBindingLister read them through
// client.List(..., InNamespace(ns)), and the folderResolver evaluates
// REQUIRE / EXCLUDE rules against that input.
//
// Writing a second policy after the resolver's first call — and seeing
// the second call observe it — is the direct regression for the HOL-622
// acceptance criterion "render-time list latency becomes O(cache lookup)
// and a fresh policy is observed within one resync interval". The fake
// client has no background watch but serves every in-memory write
// synchronously, so freshness is immediate; envtest-backed cases in
// the templatepolicies / templatepolicybindings suites exercise the real
// resync path.
func TestFolderResolver_CacheBackedReadPath(t *testing.T) {
	r := baseResolver()
	orgNs := r.OrgNamespace("acme")
	folderEngNs := r.FolderNamespace("eng")
	projectLiliesNs := r.ProjectNamespace("lilies")

	namespaceObjs := []runtime.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs),
	}
	nsClient := fake.NewClientset(namespaceObjs...)
	walker := &resolver.Walker{Client: nsClient, Resolver: r}

	// Initial snapshot: one policy + one binding targeting lilies/api.
	initialPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
			},
		},
	}
	initialBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "audit-bind", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  orgPolicyRefCRD("audit"),
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
		},
	}
	c := ctrlfake.NewClientBuilder().
		WithScheme(cacheBackedTestScheme(t)).
		WithObjects(initialPolicy, initialBinding).
		Build()

	pl := &cacheBackedPolicyLister{c: c}
	bl := &cacheBackedBindingLister{c: c}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	// Pre-write resolve: the binding injects audit-policy into the
	// effective set for deployment lilies/api.
	got, err := fr.Resolve(context.Background(), projectLiliesNs, TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve (first call): %v", err)
	}
	names := refNames(got)
	sort.Strings(names)
	if !equalStringSlices(names, []string{"audit-policy"}) {
		t.Fatalf("initial resolve mismatch: got %v, want [audit-policy]", names)
	}

	// Write a second policy + binding after the first Resolve call. The
	// fake controller-runtime client serves reads out of the same
	// in-memory store it writes to, so the next Resolve must observe
	// the new entries without any cache warmup or apiserver round-trip.
	// This is the multi-pod / multi-render freshness contract: render
	// evaluation does not hold stale results past a write.
	secondPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "net", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeFolder, "eng", "netpol"),
			},
		},
	}
	secondBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "net-bind", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  folderPolicyRefCRD("eng", "net"),
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
		},
	}
	if err := c.Create(context.Background(), secondPolicy); err != nil {
		t.Fatalf("creating second policy: %v", err)
	}
	if err := c.Create(context.Background(), secondBinding); err != nil {
		t.Fatalf("creating second binding: %v", err)
	}

	got, err = fr.Resolve(context.Background(), projectLiliesNs, TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve (post-write): %v", err)
	}
	names = refNames(got)
	sort.Strings(names)
	// Order after sort: audit-policy, netpol (alphabetical).
	if !equalStringSlices(names, []string{"audit-policy", "netpol"}) {
		t.Errorf("post-write resolve did not observe new binding: got %v, want [audit-policy netpol]", names)
	}
}

// TestFolderResolver_CacheBackedExplicitRefsAndExclude covers the mixed
// case: the caller passes an explicit ref, a REQUIRE rule (via binding)
// injects one, and a folder-level EXCLUDE rule removes a subsequently-
// injected template. The test vectors every layer of the resolver against
// a cache-backed read so a regression in any of them surfaces here.
func TestFolderResolver_CacheBackedExplicitRefsAndExclude(t *testing.T) {
	r := baseResolver()
	orgNs := r.OrgNamespace("acme")
	folderEngNs := r.FolderNamespace("eng")
	projectLiliesNs := r.ProjectNamespace("lilies")

	namespaceObjs := []runtime.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs),
	}
	nsClient := fake.NewClientset(namespaceObjs...)
	walker := &resolver.Walker{Client: nsClient, Resolver: r}

	orgPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "req", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "audit-policy"),
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
			},
		},
	}
	orgBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "req-bind", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  orgPolicyRefCRD("req"),
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
		},
	}
	folderPolicy := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "drop-extra", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				excludeRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "extra"),
			},
		},
	}
	folderBinding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "drop-extra-bind", Namespace: folderEngNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  folderPolicyRefCRD("eng", "drop-extra"),
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
		},
	}
	c := ctrlfake.NewClientBuilder().
		WithScheme(cacheBackedTestScheme(t)).
		WithObjects(orgPolicy, orgBinding, folderPolicy, folderBinding).
		Build()

	pl := &cacheBackedPolicyLister{c: c}
	bl := &cacheBackedBindingLister{c: c}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	explicit := []*consolev1.LinkedTemplateRef{orgTemplateRef("httproute")}
	got, err := fr.Resolve(context.Background(), projectLiliesNs, TargetKindDeployment, "api", explicit)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	names := refNames(got)
	sort.Strings(names)
	// httproute is explicit (owner-linked, EXCLUDE-protected);
	// audit-policy is REQUIRE-injected via binding; extra is
	// REQUIRE-then-EXCLUDE so it drops out.
	want := []string{"audit-policy", "httproute"}
	if !equalStringSlices(names, want) {
		t.Errorf("mismatch: got %v, want %v", names, want)
	}
}

// TestFolderResolver_CacheBackedSkipsProjectNamespacePolicies re-affirms
// the HOL-554 storage-isolation guardrail at the cache layer: a policy
// placed in a project namespace (which admission would reject in
// production but which the fake client accepts) must not contribute to
// the effective set.
func TestFolderResolver_CacheBackedSkipsProjectNamespacePolicies(t *testing.T) {
	r := baseResolver()
	orgNs := r.OrgNamespace("acme")
	folderEngNs := r.FolderNamespace("eng")
	projectLiliesNs := r.ProjectNamespace("lilies")

	namespaceObjs := []runtime.Object{
		mkNs(orgNs, v1alpha2.ResourceTypeOrganization, ""),
		mkNs(folderEngNs, v1alpha2.ResourceTypeFolder, orgNs),
		mkNs(projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs),
	}
	nsClient := fake.NewClientset(namespaceObjs...)
	walker := &resolver.Walker{Client: nsClient, Resolver: r}

	// Forbidden policy sitting in the project namespace; a binding in a
	// legitimate namespace points at it. The resolver must ignore the
	// project-namespace policy.
	pwned := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pwned", Namespace: projectLiliesNs},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "acme", "should-be-ignored"),
			},
		},
	}
	binding := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "pwned-bind", Namespace: orgNs},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef:  orgPolicyRefCRD("pwned"),
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{deploymentTargetCRD("lilies", "api")},
		},
	}
	c := ctrlfake.NewClientBuilder().
		WithScheme(cacheBackedTestScheme(t)).
		WithObjects(pwned, binding).
		Build()

	pl := &cacheBackedPolicyLister{c: c}
	bl := &cacheBackedBindingLister{c: c}
	fr := NewFolderResolverWithBindings(pl, walker, r, bl)

	got, err := fr.Resolve(context.Background(), projectLiliesNs, TargetKindDeployment, "api", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("project-namespace policy leaked through cache path: got %v, want empty", refNames(got))
	}
}

// cacheBackedNamespaceAssertion keeps the "test uses a real Namespace kind"
// import honest: without an explicit corev1 reference the compiler elides
// the package. The namespace objects above already use corev1 via the
// shared mkNs helper, but we keep this guard so a refactor that drops the
// mkNs dependency from this file still surfaces the missing import at
// compile time rather than silently linking against a stale binary.
var _ = corev1.SchemeGroupVersion

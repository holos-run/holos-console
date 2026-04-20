// multi_pod_freshness_test.go regresses the HOL-622 acceptance criterion
// "render evaluated from manager B sees a policy written via manager A
// within one resync interval". The cache_backed_test.go file covers the
// single-process fake-client path; this file adds the real-apiserver
// envtest case: two independent controller-runtime Managers share the
// process-singleton envtest apiserver and each builds its own informer
// cache. A write issued through manager A must become visible through
// manager B's cache via the normal watch path.
//
// The test is gated on envtest binaries (see crdmgrtesting.StartManager
// semantics — t.Skip when KUBEBUILDER_ASSETS is unset and no cached
// kubebuilder-envtest download is present) so `go test ./...` on a
// developer machine without envtest still passes.
package policyresolver

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	crdmgrtesting "github.com/holos-run/holos-console/console/crdmgr/testing"
	"github.com/holos-run/holos-console/console/resolver"
)

// TestFolderResolver_MultiPodFreshness verifies the cache-backed read
// path across two independent ctrl-runtime Managers sharing the envtest
// apiserver. Manager A's cache-backed client seeds a policy + binding;
// manager B's cache-backed client feeds a folderResolver that observes
// them on first Resolve. Manager A then writes a second policy + binding
// through the same cache-backed client, and the test polls manager B's
// resolver until the new binding becomes effective — bounding the
// acceptance criterion's "within one resync interval" clause with a
// generous 30s deadline so a slow CI host does not flake.
//
// Writing the policy and binding CRs through manager A's cache-backed
// client (envA.Client) — rather than the uncached envA.Direct — is
// deliberate: it exercises the production write path and makes this
// regression sensitive to any wiring bug in manager A's K8sClients. A
// test that created via envA.Direct would still pass even if manager A
// never plumbed a ctrl-runtime client onto its storage clients.
//
// The namespace fixtures use envA.Direct because the resolver's walker
// consumes namespaces via client-go; routing those creates through the
// cache-backed client would just add a propagation wait to the setup
// without strengthening the contract under test.
//
// This is intentionally a single test (not table-driven): the expensive
// bit is spinning up two Managers against the shared envtest, and a
// single write/observe pair is enough to regress the freshness contract.
func TestFolderResolver_MultiPodFreshness(t *testing.T) {
	// Manager A: the "writer" pod. StartManager registers a t.Cleanup that
	// shuts this manager down when the test returns. Env.Direct rolls
	// through the apiserver (uncached) so the seed writes are deterministic
	// — we want to isolate the freshness observation to manager B's cache,
	// not introduce a second cache propagation path on the write side.
	envA := crdmgrtesting.StartManager(t, crdmgrtesting.Options{
		Scheme: cacheBackedTestScheme(t),
		InformerObjects: []ctrlclient.Object{
			&templatesv1alpha1.TemplatePolicy{},
			&templatesv1alpha1.TemplatePolicyBinding{},
		},
	})
	if envA == nil {
		// StartManager already called t.Skip.
		return
	}

	// Manager B: the "reader" pod — independent Manager, independent cache.
	// Built directly (not via StartManager) so we keep strict isolation
	// from manager A: if StartManager ever reuses a cache across calls, we
	// want this test to fail rather than pass spuriously.
	mgrB, err := ctrl.NewManager(envA.Cfg, ctrl.Options{
		Scheme:                 cacheBackedTestScheme(t),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		t.Fatalf("constructing manager B: %v", err)
	}
	// Prime the informers on B before Start so the first List does not race
	// a just-issued Create while the watch is still warming. This matches
	// the production wiring in console.go.
	for _, obj := range []ctrlclient.Object{
		&templatesv1alpha1.TemplatePolicy{},
		&templatesv1alpha1.TemplatePolicyBinding{},
	} {
		if _, err := mgrB.GetCache().GetInformer(context.Background(), obj); err != nil {
			t.Fatalf("priming informer on manager B for %T: %v", obj, err)
		}
	}
	ctxB, cancelB := context.WithCancel(context.Background())
	errChB := make(chan error, 1)
	go func() {
		errChB <- mgrB.Start(ctxB)
	}()
	t.Cleanup(func() {
		cancelB()
		select {
		case err := <-errChB:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("manager B exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Logf("manager B did not shut down within deadline")
		}
	})
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	if !mgrB.GetCache().WaitForCacheSync(waitCtx) {
		t.Fatalf("manager B cache did not sync within deadline")
	}

	// Fixture: acme org, eng folder under acme, lilies project under eng.
	// The resolver's Walker needs a client-go kubernetes.Interface (it
	// queries namespaces via CoreV1), so we derive one from envA.Cfg. Both
	// managers share the same apiserver so this is the only such client
	// the test needs.
	r := &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	orgNs := r.OrgNamespace("mp-acme")
	folderEngNs := r.FolderNamespace("mp-eng")
	projectLiliesNs := r.ProjectNamespace("mp-lilies")
	ensureNamespaceForMPTest(t, envA.Direct, orgNs, v1alpha2.ResourceTypeOrganization, "")
	ensureNamespaceForMPTest(t, envA.Direct, folderEngNs, v1alpha2.ResourceTypeFolder, orgNs)
	ensureNamespaceForMPTest(t, envA.Direct, projectLiliesNs, v1alpha2.ResourceTypeProject, folderEngNs)

	// Cleanup: delete the namespaces at test-end so a re-run against the
	// same envtest binary (or a future shared-apiserver test in this
	// package) does not inherit stale state. AlreadyExists is tolerated
	// inside ensureNamespaceForMPTest; parallel Delete NotFound is
	// equivalent: treat it as success.
	t.Cleanup(func() {
		for _, name := range []string{projectLiliesNs, folderEngNs, orgNs} {
			_ = envA.Direct.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
	})

	// Seed: one policy in the org namespace and a binding that attaches it
	// to lilies/api.
	policyA := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "audit",
			Namespace: orgNs,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeOrganization, "mp-acme", "audit-policy"),
			},
		},
	}
	bindingA := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "audit-bind",
			Namespace: orgNs,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Namespace: "holos-org-mp-acme", Name: "audit",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					ProjectName: "mp-lilies",
					Name:        "api",
				},
			},
		},
	}
	// Seed via envA.Client (cache-backed) — this exercises manager A's
	// production write path. Writes fall through the delegating client to
	// the apiserver; manager A's cache observes them on the next watch
	// event, and manager B's cache observes them via its independent
	// watch. Using envA.Direct here would mask a wiring bug where manager
	// A failed to use its cache-backed client at all.
	if err := envA.Client.Create(context.Background(), policyA); err != nil {
		t.Fatalf("seed create policy (manager A cache-backed): %v", err)
	}
	if err := envA.Client.Create(context.Background(), bindingA); err != nil {
		t.Fatalf("seed create binding (manager A cache-backed): %v", err)
	}

	// Build manager B's resolver stack. The kubernetes.Interface for the
	// namespace walker is derived from the shared REST config — both
	// managers talk to the same apiserver.
	core, err := kubernetes.NewForConfig(envA.Cfg)
	if err != nil {
		t.Fatalf("constructing core client: %v", err)
	}
	walker := &resolver.Walker{Client: core, Resolver: r}
	plB := &cacheBackedPolicyLister{c: mgrB.GetClient()}
	blB := &cacheBackedBindingLister{c: mgrB.GetClient()}
	frB := NewFolderResolverWithBindings(plB, walker, r, blB)

	// First observation: B's cache should catch up on the seed within its
	// initial sync window. Poll up to 30s to tolerate slow CI hosts.
	eventuallyResolveFolderResolverNames(t, frB, projectLiliesNs, "api",
		[]string{"audit-policy"}, 30*time.Second,
		"manager B did not observe seed policy+binding")

	// Now write a second policy + binding via manager A. Manager B must
	// observe the new binding through its watch; the previous Resolve is
	// not expected to be cached.
	policyB := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "net",
			Namespace: folderEngNs,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			Rules: []templatesv1alpha1.TemplatePolicyRule{
				requireRuleCRD(v1alpha2.TemplateScopeFolder, "mp-eng", "netpol"),
			},
		},
	}
	bindingB := &templatesv1alpha1.TemplatePolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "net-bind",
			Namespace: folderEngNs,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicyBinding,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicyBindingSpec{
			PolicyRef: templatesv1alpha1.LinkedTemplatePolicyRef{
				Namespace: "holos-fld-mp-eng", Name: "net",
			},
			TargetRefs: []templatesv1alpha1.TemplatePolicyBindingTargetRef{
				{
					Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
					ProjectName: "mp-lilies",
					Name:        "api",
				},
			},
		},
	}
	if err := envA.Client.Create(context.Background(), policyB); err != nil {
		t.Fatalf("post-seed create policy (manager A cache-backed): %v", err)
	}
	if err := envA.Client.Create(context.Background(), bindingB); err != nil {
		t.Fatalf("post-seed create binding (manager A cache-backed): %v", err)
	}

	// Freshness contract: B's resolver must observe the new binding within
	// one resync interval. Default controller-runtime sync period is 10h,
	// but informer watches deliver events in sub-second time in practice;
	// 30s is a generous bound that still fails a real staleness bug.
	eventuallyResolveFolderResolverNames(t, frB, projectLiliesNs, "api",
		[]string{"audit-policy", "netpol"}, 30*time.Second,
		"manager B did not observe post-seed policy+binding within deadline")
}

// ensureNamespaceForMPTest creates a namespace with the label shape the
// resolver's walker relies on. The "MP" suffix keeps the helper distinct
// from ensureNamespace in other test files (different package or different
// signature) so a future consolidation is deliberate rather than
// accidental.
func ensureNamespaceForMPTest(t *testing.T, c ctrlclient.Client, name, resourceType, parent string) {
	t.Helper()
	labels := map[string]string{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: resourceType,
	}
	if parent != "" {
		labels[v1alpha2.AnnotationParent] = parent
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	if err := c.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %q: %v", name, err)
	}
}

// eventuallyResolveFolderResolverNames polls the folder resolver's
// Resolve output until the sorted set of template names matches want or
// the deadline expires. Used on the watch-freshness side of the test so
// the assertion tolerates whatever propagation delay the apiserver
// introduces without bloating the happy-path wall clock.
func eventuallyResolveFolderResolverNames(
	t *testing.T,
	fr PolicyResolver,
	projectNs, targetName string,
	want []string,
	deadline time.Duration,
	message string,
) {
	t.Helper()
	wantSorted := append([]string{}, want...)
	sort.Strings(wantSorted)
	end := time.Now().Add(deadline)
	var lastSeen []string
	for time.Now().Before(end) {
		got, err := fr.Resolve(context.Background(), projectNs, TargetKindDeployment, targetName, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		names := refNames(got)
		sort.Strings(names)
		if equalStringSlices(names, wantSorted) {
			return
		}
		lastSeen = names
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: want %v, last seen %v", message, wantSorted, lastSeen)
}


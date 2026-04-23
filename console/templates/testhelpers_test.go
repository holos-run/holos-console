// Test helpers shared by handler_*_test.go and k8s_test.go. HOL-661 rewrote
// the storage layer to read/write Template CRDs through a controller-runtime
// client.Client, so these helpers bridge the legacy fake.Clientset-based test
// fixtures (which still drive the Release-ConfigMap path) to a fake
// controller-runtime client seeded with the Template CRDs derived from those
// fixtures.
//
// The bridge is scoped to tests — production wiring constructs a K8sClient
// with the cache-backed client.Client from the embedded controller manager
// (see console.go around line 371). Moving to envtest for every test in this
// package is explicitly a follow-up (HOL-663 introduces the shared envtest
// helper); in the meantime we keep the existing test fixtures intact by
// reflecting them into an in-memory controller-runtime client.
package templates

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// DefaultsKey is the ConfigMap data key the legacy Template/Release
// ConfigMap fixtures used to carry the TemplateDefaults JSON payload. The
// HOL-661 / HOL-693 rewrites retired the constant from production code (the
// CRD stores structured defaults in spec.defaults), but several fixture
// helpers still round-trip the JSON form so they can keep their original
// literal shape while the underlying K8sClient reads from CRDs. Scoped to
// tests.
const DefaultsKey = "defaults"

// testScheme returns a runtime.Scheme registered with core and templates
// v1alpha1 types — enough for the fake controller-runtime client to
// List/Get/Create/Update/Delete Templates in a test.
func testScheme(t *testing.T) *runtime.Scheme {
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

// configMapIsTemplate reports whether a ConfigMap in the fake clientset
// represents a Template. Templates are identified by the presence of the
// LabelTemplateScope label stamped on every template fixture.
func configMapIsTemplate(cm *corev1.ConfigMap) bool {
	_, hasScope := cm.Labels[v1alpha2.LabelTemplateScope]
	return hasScope
}

// configMapToTemplateCRD converts a v1alpha2-labeled template ConfigMap
// fixture to the equivalent Template CRD. Preserves name, namespace,
// display/description/enabled fields, CUE payload, and structured defaults
// (decoded from the DefaultsKey JSON).
func configMapToTemplateCRD(cm *corev1.ConfigMap) *templatesv1alpha1.Template {
	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: cm.Namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy: v1alpha2.ManagedByValue,
			},
		},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName: cm.Annotations[v1alpha2.AnnotationDisplayName],
			Description: cm.Annotations[v1alpha2.AnnotationDescription],
			CueTemplate: cm.Data[CueTemplateKey],
			Enabled:     cm.Annotations[v1alpha2.AnnotationEnabled] == "true",
		},
	}
	if raw, ok := cm.Data[DefaultsKey]; ok && raw != "" {
		var d consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(raw), &d); err == nil {
			tmpl.Spec.Defaults = protoDefaultsToCRD(&d)
		}
	}
	return tmpl
}

// seedTemplatesFromClientset extracts every template-labeled ConfigMap from
// the fake clientset and returns them as Template CRDs. Used to keep the
// handler_*_test.go fixtures (all still expressed as template ConfigMaps)
// working against the rewritten K8sClient.
func seedTemplatesFromClientset(t *testing.T, cs *kfake.Clientset) []client.Object {
	t.Helper()
	cms, err := cs.CoreV1().ConfigMaps("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing ConfigMaps across all namespaces: %v", err)
	}
	var out []client.Object
	for i := range cms.Items {
		cm := &cms.Items[i]
		if !configMapIsTemplate(cm) {
			continue
		}
		out = append(out, configMapToTemplateCRD(cm))
	}
	return out
}

// seedNamespacesFromClientset extracts every Namespace from the fake clientset
// and returns deep copies as client.Object so the controller-runtime fake
// client can answer Namespace Get calls (e.g. K8sClient.GetNamespaceOrg used
// by SearchTemplates' organization filter). Tests still seed namespace
// fixtures into the kfake.Clientset; this bridge mirrors them into the
// controller-runtime client so both surfaces see the same set.
func seedNamespacesFromClientset(t *testing.T, cs *kfake.Clientset) []client.Object {
	t.Helper()
	nss, err := cs.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing Namespaces: %v", err)
	}
	out := make([]client.Object, 0, len(nss.Items))
	for i := range nss.Items {
		ns := nss.Items[i]
		out = append(out, &ns)
	}
	return out
}

// newFakeCtrlClient returns a fake controller-runtime client preloaded with
// the given Template CRD objects. The scheme includes core types plus
// templates v1alpha1 so the client.Client methods accept Template CRs.
func newFakeCtrlClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return ctrlfake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		Build()
}

// newTestK8sClient constructs a K8sClient for tests. The controller-runtime
// client is seeded from every template-labeled ConfigMap in the Clientset
// (translated to Template CRDs) plus any caller-supplied extra CRD objects
// — typically TemplateRelease fixtures seeded via makeReleaseCRD. Namespace
// reads still flow through the fake.Clientset that the fixtures populate.
// Tests that pre-date HOL-693 keep their existing Template ConfigMap
// fixtures unchanged; release fixtures move to TemplateRelease CRD shape.
func newTestK8sClient(t *testing.T, cs *kfake.Clientset, r *resolver.Resolver, extra ...client.Object) *K8sClient {
	t.Helper()
	objs := seedTemplatesFromClientset(t, cs)
	objs = append(objs, seedNamespacesFromClientset(t, cs)...)
	objs = append(objs, extra...)
	ctrl := newFakeCtrlClient(t, objs...)
	_ = cs
	return NewK8sClient(ctrl, r)
}

// makeReleaseCRD builds a TemplateRelease CRD fixture. Tests pass the result
// into newTestK8sClient's variadic seed argument so the fake
// controller-runtime client observes the release on the first List / Get.
// The label shape matches what CreateRelease writes in production (ADR 032).
func makeReleaseCRD(ns, templateName, version string) *templatesv1alpha1.TemplateRelease {
	return makeReleaseCRDWithData(ns, templateName, version, validCue, "")
}

// makeReleaseCRDWithData is a richer TemplateRelease fixture builder used by
// suites that need non-default CUE source or a defaults payload. The defaults
// argument is a JSON TemplateDefaults blob (same shape the retired release
// ConfigMap encoded) so call sites ported from the ConfigMap-era tests can
// keep their literal fixtures.
func makeReleaseCRDWithData(ns, templateName, version, cue, defaults string) *templatesv1alpha1.TemplateRelease {
	v, _ := ParseVersion(version)
	rel := &templatesv1alpha1.TemplateRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReleaseObjectName(templateName, v),
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:        v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:     "template-release",
				"console.holos.run/release-of": templateName,
			},
		},
		Spec: templatesv1alpha1.TemplateReleaseSpec{
			TemplateName: templateName,
			Version:      version,
			CueTemplate:  cue,
		},
	}
	if defaults != "" {
		// Tests pass the legacy `defaults.json` payload shape. Normalize it
		// through the same protojson path the handler uses so fixtures
		// serialize identically to release objects produced by CreateRelease.
		var d consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(defaults), &d); err == nil {
			if s, err := marshalProtoDefaults(&d); err == nil {
				rel.Spec.DefaultsJSON = s
			}
		}
	}
	return rel
}

// configMapToTemplate is a test-only bridge retained so handler_test.go's
// legacy TestConfigMapToTemplate cases — which were written against the
// ConfigMap-backed storage path — continue to exercise the proto-conversion
// surface after the HOL-661 rewrite. The real handler path no longer calls
// this helper; production uses templateCRDToProto directly.
//
// The function materializes the ConfigMap into a Template CRD and then hands
// it to templateCRDToProto so the production converter is the one under
// test. The (scope, scopeName) parameters are retained because
// templateCRDToProto still takes a scope (for CUE extraction gating — only
// project-scope templates pull defaults out of their CUE).
func configMapToTemplate(cm *corev1.ConfigMap, scope scopeKind, _ string) *consolev1.Template {
	return templateCRDToProto(configMapToTemplateCRD(cm), scope == scopeKindProject)
}

// ensure apierrors import stays used even if no other test helper touches it
// directly; several tests assert on NotFound translation through the
// apierrors.IsNotFound path.
var _ = apierrors.IsNotFound

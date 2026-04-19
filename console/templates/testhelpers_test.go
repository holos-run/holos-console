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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

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
// represents a Template (as opposed to a Release or some other resource).
// Recognizes either the canonical v1alpha2 ResourceType=template label, or
// the template-scope label (used by older fixtures that pre-date the
// ResourceType label convention but still describe templates). Release
// ConfigMaps never carry LabelTemplateScope, so the union is still precise.
func configMapIsTemplate(cm *corev1.ConfigMap) bool {
	if cm.Labels[v1alpha2.LabelResourceType] == v1alpha2.ResourceTypeTemplate {
		return true
	}
	if _, ok := cm.Labels[v1alpha2.LabelTemplateScope]; ok {
		return true
	}
	return false
}

// configMapToTemplateCRD converts a v1alpha2-labeled template ConfigMap
// fixture to the equivalent Template CRD. Preserves name, namespace,
// display/description/enabled fields, CUE payload, structured defaults
// (decoded from the DefaultsKey JSON), and linkedTemplates annotation.
// This is how the test bridge materializes existing ConfigMap fixtures
// into the CRD the rewritten K8sClient reads from.
func configMapToTemplateCRD(cm *corev1.ConfigMap) *templatesv1alpha1.Template {
	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: cm.Namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplate,
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
	if raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok && raw != "" {
		tmpl.Spec.LinkedTemplates = protoLinkedToCRD(parseLinkedAnnotation(raw))
	}
	return tmpl
}

// parseLinkedAnnotation decodes the legacy v1alpha2 linked-templates JSON
// annotation into proto LinkedTemplateRefs so ConfigMap test fixtures that
// still express linked refs through the annotation continue to work after
// HOL-661 moved production storage into Template.Spec.LinkedTemplates.
func parseLinkedAnnotation(raw string) []*consolev1.LinkedTemplateRef {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	var stored []storedRef
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil
	}
	out := make([]*consolev1.LinkedTemplateRef, 0, len(stored))
	for _, r := range stored {
		var scope scopeshim.Scope
		switch r.Scope {
		case "organization":
			scope = scopeshim.ScopeOrganization
		case "folder":
			scope = scopeshim.ScopeFolder
		case "project":
			scope = scopeshim.ScopeProject
		default:
			continue
		}
		out = append(out, scopeshim.NewLinkedTemplateRef(scope, r.ScopeName, r.Name, r.VersionConstraint))
	}
	return out
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

// newTestK8sClient constructs a K8sClient for tests. The core fake.Clientset
// continues to drive Release ConfigMap storage and Namespace reads; the
// controller-runtime client is seeded from every template-labeled ConfigMap
// in the Clientset, translated to Template CRDs. Tests that pre-date HOL-661
// keep their existing fixtures and code unchanged.
func newTestK8sClient(t *testing.T, cs *kfake.Clientset, r *resolver.Resolver) *K8sClient {
	t.Helper()
	objs := seedTemplatesFromClientset(t, cs)
	ctrl := newFakeCtrlClient(t, objs...)
	return NewK8sClient(cs, ctrl, r)
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
func configMapToTemplate(cm *corev1.ConfigMap, scope scopeshim.Scope, _ string) *consolev1.Template {
	return templateCRDToProto(configMapToTemplateCRD(cm), scope)
}

// ensure apierrors import stays used even if no other test helper touches it
// directly; several tests assert on NotFound translation through the
// apierrors.IsNotFound path.
var _ = apierrors.IsNotFound

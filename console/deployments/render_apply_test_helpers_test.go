package deployments

import (
	"context"
	"strings"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func renderFlat(r *CueRenderer, ctx context.Context, cueSource string, platform v1alpha2.PlatformInput, project v1alpha2.ProjectInput) ([]unstructured.Unstructured, error) {
	grouped, err := r.Render(ctx, cueSource, nil, RenderInputs{Platform: platform, Project: project})
	if err != nil {
		return nil, err
	}
	out := make([]unstructured.Unstructured, 0, len(grouped.Platform)+len(grouped.Project))
	out = append(out, grouped.Platform...)
	out = append(out, grouped.Project...)
	return out, nil
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}

func fakeDynamicScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, entry := range []struct {
		gvr  schema.GroupVersionResource
		kind string
	}{
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment"},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, "Service"},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, "ServiceAccount"},
		{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, "Role"},
		{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, "RoleBinding"},
		{schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, "HTTPRoute"},
		{schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}, "ReferenceGrant"},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, "ConfigMap"},
		{schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "Secret"},
	} {
		gvk := schema.GroupVersionKind{Group: entry.gvr.Group, Version: entry.gvr.Version, Kind: entry.kind}
		listGVK := schema.GroupVersionKind{Group: entry.gvr.Group, Version: entry.gvr.Version, Kind: entry.kind + "List"}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	}
	return scheme
}

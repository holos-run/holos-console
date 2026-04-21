/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package projectapply

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DefaultGVRResolver is the small in-package GVR table covering every
// kind the ProjectNamespace render path emits today: the Namespace
// itself, the cluster-scoped RBAC primitives the rule-of-thumb set in
// console/templates/render_project_namespace.go may route to
// ClusterScoped, and the namespace-scoped kinds the HOL-810 render path
// already validates via console/deployments.allowedKinds.
//
// The applier accepts a [GVRResolver] interface rather than hard-coding
// the table, so callers that extend the allowed-kinds set (e.g. a future
// CreateFolder / CreateOrganization path) can inject their own. For the
// CreateProject RPC (HOL-812) wiring, this default is sufficient.
type DefaultGVRResolver struct{}

// NewDefaultGVRResolver returns a resolver that knows the kinds the
// ProjectNamespace render path emits.
func NewDefaultGVRResolver() *DefaultGVRResolver {
	return &DefaultGVRResolver{}
}

// ResolveGVR implements GVRResolver.
func (DefaultGVRResolver) ResolveGVR(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	if gvr, ok := defaultGVRTable[gvk]; ok {
		return gvr, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("projectapply: kind %s is not allowed for project-namespace apply", gvk)
}

// defaultGVRTable is the set of kinds the ProjectNamespace render path
// legitimately emits. The cluster-scoped entries are the union of
// render_project_namespace.go's clusterScopedKinds and the common
// cluster-scoped RBAC kinds. The namespaced entries mirror
// console/deployments/apply.go's allowedKinds so a template that emits a
// Deployment or ReferenceGrant into a ProjectNamespace render is applied
// with the same SSA semantics as the deployments path uses.
var defaultGVRTable = map[schema.GroupVersionKind]schema.GroupVersionResource{
	// core/v1
	{Group: "", Version: "v1", Kind: "Namespace"}:      {Group: "", Version: "v1", Resource: "namespaces"},
	{Group: "", Version: "v1", Kind: "Service"}:        {Group: "", Version: "v1", Resource: "services"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"}: {Group: "", Version: "v1", Resource: "serviceaccounts"},
	{Group: "", Version: "v1", Kind: "ConfigMap"}:      {Group: "", Version: "v1", Resource: "configmaps"},
	{Group: "", Version: "v1", Kind: "Secret"}:         {Group: "", Version: "v1", Resource: "secrets"},
	// apps/v1
	{Group: "apps", Version: "v1", Kind: "Deployment"}: {Group: "apps", Version: "v1", Resource: "deployments"},
	// rbac.authorization.k8s.io/v1 (namespaced)
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}:        {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"}: {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"},
	// rbac.authorization.k8s.io/v1 (cluster-scoped)
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}:        {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}: {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"},
	// gateway.networking.k8s.io
	{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}:           {Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"},
	{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "ReferenceGrant"}: {Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"},
}

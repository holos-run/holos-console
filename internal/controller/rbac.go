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

// Package controller's RBAC markers. controller-gen's rbac generator only
// emits rules from markers attached to the package doc comment — struct or
// method doc comments are silently ignored by controller-gen v0.20. Keeping
// every marker in this one file makes the generated ClusterRole in
// config/holos-console/rbac/role.yaml a single source of truth for the console service
// account, and it prevents the accidental "marker on a struct, zero rules
// emitted" footgun we tripped over landing HOL-620.
//
// +kubebuilder:rbac:groups=templates.holos.run,resources=templates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templates/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicybindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatepolicybindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatereleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatereleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatereleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=renderstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=renderstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=renderstates/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=templategrants,verbs=get;list;watch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templategrants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatedependencies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatedependencies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templatedependencies/finalizers,verbs=update
// +kubebuilder:rbac:groups=templates.holos.run,resources=templaterequirements,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.holos.run,resources=templaterequirements/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=templates.holos.run,resources=templaterequirements/finalizers,verbs=update
// Deployment CRs are the source objects the DeploymentReconciler watches and
// owns. Status updates publish Accepted/Rendered/Applied conditions. Finalizer
// updates let the reconciler clean up rendered resources before the CR is
// deleted from the cluster.
// +kubebuilder:rbac:groups=deployments.holos.run,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deployments.holos.run,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deployments.holos.run,resources=deployments/finalizers,verbs=update
// Roles, RoleBindings, ClusterRoles, and ClusterRoleBindings are rendered by
// the apply pipeline for per-Deployment RBAC. escalate/bind on Roles and
// ClusterRoles are required by Kubernetes when granting verbs the controller
// itself does not yet hold.
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete;escalate;bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete;escalate;bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// ADR 036 impersonation: the RPC layer impersonates the caller (user, group,
// or service account) to enforce caller RBAC on console reads/writes. Extra
// info attributes (userextras/*) are intentionally NOT forwarded.
// +kubebuilder:rbac:groups="",resources=users;groups;serviceaccounts,verbs=impersonate
// Namespace reads back hierarchy resolution and resource context lookups.
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// Controller events surface reconcile diagnostics in the cluster.
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// Core kinds emitted by the Deployment apply pipeline: ConfigMaps hold legacy
// deployment metadata, Secrets and Services are rendered application surface,
// and ServiceAccounts are both rendered subjects and impersonation targets.
// SSA apply + cleanup needs the full create/get/list/watch/patch/update/delete
// set per resource.
// +kubebuilder:rbac:groups="",resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// apps/v1 Deployments are rendered workload manifests; SSA apply + cleanup
// needs the full verb set.
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// Gateway API HTTPRoutes and ReferenceGrants are rendered networking
// manifests managed by SSA apply + cleanup.
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes;referencegrants,verbs=get;list;watch;create;update;patch;delete
package controller

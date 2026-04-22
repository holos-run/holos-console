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
// config/secret-injector/rbac/role.yaml a single source of truth for the
// holos-secret-injector service account, and mirrors the convention the
// templates-group rbac file established in HOL-620.
//
// Notes on the least-privilege envelope we're encoding here:
//
//   - Full CRUD on every secrets.holos.run kind (the M2 reconcilers own
//     these). Status and finalizers are split out per controller-runtime
//     idiom so the reconciler can update status without needing write on
//     spec, and so finalizer removal is auditable as its own rule.
//
//   - CRUD on security.istio.io AuthorizationPolicy because the M2
//     SecretInjectionPolicyBinding reconciler synthesises AP objects that
//     enforce the caller allow-list at the mesh layer.
//
//   - `get` ONLY on core/v1 Secret at the cluster-role level. Enumeration
//     (list/watch) is the vulnerability this service is meant to close —
//     the reconciler resolves refs by name+namespace, never by listing.
//     Broader CUD on core/v1 Secret for controller-owned hash-material
//     objects is granted via a namespace-scoped Role
//     (config/secret-injector/rbac/secrets-namespace-role.yaml) so it
//     cannot escape the controller's own namespace.
//
//     HOL-751 note: the Credential reconciler creates and maintains a
//     sibling hash v1.Secret per Credential in the Credential's OWN
//     namespace via an ownerReference. The cluster-role grants `get` so
//     the reconciler can probe existence before (re-)materialising; the
//     create/update/delete verbs it needs for the hash Secret come from a
//     namespace-scoped RoleBinding that follows the Credential's
//     namespace, and the ownerReference (controller=true,
//     blockOwnerDeletion=true) guarantees the hash Secret is
//     garbage-collected atomically when the Credential is deleted. No
//     cluster-wide Secret enumeration path is opened by this design.
//
//   - `get/list/watch` on Namespace so the reconciler can resolve the
//     hierarchy labels the admission policies enforce against.
//
//   - `create/patch` on Event so the reconciler can emit standard
//     controller-runtime events.
//
// +kubebuilder:rbac:groups=secrets.holos.run,resources=credentials,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets.holos.run,resources=credentials/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets.holos.run,resources=credentials/finalizers,verbs=update
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicybindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets.holos.run,resources=secretinjectionpolicybindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=secrets.holos.run,resources=upstreamsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets.holos.run,resources=upstreamsecrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets.holos.run,resources=upstreamsecrets/finalizers,verbs=update
// +kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
package controller

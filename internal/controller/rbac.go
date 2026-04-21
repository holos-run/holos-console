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
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
package controller

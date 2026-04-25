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

// Package v1alpha1 contains API Schema definitions for the
// deployments.holos.run v1alpha1 API group. The Deployment CRD captures the
// existing proto-defined Deployment shape so both the proto store and the CR
// can coexist; Phase 3 (HOL-957) wires the dual-write path.
//
// +kubebuilder:object:generate=true
// +groupName=deployments.holos.run
package v1alpha1

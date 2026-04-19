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

// Package controller hosts the controller-runtime reconcilers for the
// secrets.holos.run/v1alpha1 API group owned by the holos-secret-injector
// binary. See docs/adrs/031-secret-injection-service.md. One reconciler per
// kind lands here under M1; this file scaffolds the package so subsequent
// phases can land types and reconcilers without tree churn. Nothing under
// this tree imports internal/controller/... and vice versa.
package controller

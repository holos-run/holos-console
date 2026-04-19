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

// Package authz hosts the ext_authz gRPC server for the holos-secret-injector
// binary. The server runs as a manager.Runnable registered on the
// controller-runtime manager so it shares the reconcilers' cache, lifecycle,
// and readiness gate. See docs/adrs/031-secret-injection-service.md §2 for
// the rationale. Nothing under this tree imports internal/controller/... and
// vice versa.
package authz

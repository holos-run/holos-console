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
	"context"
	"fmt"
)

// DeadlineExceededError is the structured error the applier returns when
// its retry window (caller context or the 30s ceiling) fires while a
// transient-failure classifier was still active. ADR 034 calls for a
// "structured error the RPC layer can map" so the CreateProject handler
// (HOL-812) can translate this to [connect.CodeDeadlineExceeded] and the
// frontend can surface an operator-actionable message (e.g. "the
// namespace controller did not mark foo Active in 30s; check apiserver
// health").
//
// The error is surfaced for two distinct callers:
//
//  1. waitForNamespaceActive returns it when the Namespace does not reach
//     status.phase == Active before the timeout.
//  2. retryNamespacedApply returns it when every attempt at SSA-applying a
//     namespace-scoped resource tripped one of the transient-failure
//     classifiers (IsNotFound / IsForbidden / IsServerTimeout /
//     IsInternalError) up until the deadline.
//
// Both callers populate Kind/Name so operators can tell which resource
// blocked. LastError and Classifier carry the last observed root cause so
// the RPC can surface it in its response.
type DeadlineExceededError struct {
	// Kind is "Namespace" for the wait path and the kind of the
	// offending resource for the apply path.
	Kind string
	// Name is the resource name.
	Name string
	// Namespace is the resource's namespace for namespace-scoped
	// resources; empty for cluster-scoped resources and the Namespace
	// wait.
	Namespace string
	// Attempts is the number of SSA-apply attempts retryNamespacedApply
	// made before the deadline fired. Unused by the wait path (the
	// wait is a poll, not an attempt count).
	Attempts int
	// LastError is the error returned by the last apiserver operation
	// (Get during wait, Patch during apply). May be nil during the wait
	// path when the Namespace exists but has not reached Active.
	LastError error
	// Classifier is the last apierrors classifier name that matched —
	// one of "NotFound", "Forbidden", "ServerTimeout", "InternalError".
	// Empty string during the wait path.
	Classifier string
	// LastPhase is the last observed Namespace.status.phase during the
	// wait. Empty when unused (apply path) or when the namespace was not
	// yet observed by the apiserver.
	LastPhase string
}

// Error implements error. The message is formatted so an operator
// reading the RPC response can see which resource blocked and why.
func (e *DeadlineExceededError) Error() string {
	switch e.Kind {
	case "Namespace":
		if e.LastPhase != "" {
			return fmt.Sprintf("deadline exceeded waiting for Namespace %q to reach Active (last phase: %q)",
				e.Name, e.LastPhase)
		}
		if e.LastError != nil {
			return fmt.Sprintf("deadline exceeded waiting for Namespace %q to reach Active (last error: %v)",
				e.Name, e.LastError)
		}
		return fmt.Sprintf("deadline exceeded waiting for Namespace %q to reach Active", e.Name)
	default:
		return fmt.Sprintf("deadline exceeded applying %s/%s/%s after %d attempts: last %s error: %v",
			e.Kind, e.Namespace, e.Name, e.Attempts, e.Classifier, e.LastError)
	}
}

// Unwrap exposes the last underlying apiserver error so callers can match
// on apierrors classifiers (e.g. [apierrors.IsForbidden]). Returns nil
// when no underlying error was captured — primarily the wait path when
// the Namespace existed but never progressed to Active.
func (e *DeadlineExceededError) Unwrap() error {
	return e.LastError
}

// Is reports deadline-exceeded identity so callers using
// errors.Is(err, context.DeadlineExceeded) continue to work unchanged.
// The RPC layer can alternatively type-assert on *DeadlineExceededError
// to reach the structured fields.
func (e *DeadlineExceededError) Is(target error) bool {
	return target == context.DeadlineExceeded
}

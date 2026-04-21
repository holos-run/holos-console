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
	"errors"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

// waitForNamespaceActive blocks until the cluster's namespace controller
// marks the given namespace `.status.phase == Active`, up to the smaller
// of the caller's context deadline and timeout.
//
// Rationale for polling `.status.phase`:
//
//   - It is the upstream-documented readiness signal produced by
//     pkg/controller/namespace
//     (https://github.com/kubernetes/kubernetes/tree/master/pkg/controller/namespace):
//     the namespace controller flips phase to Active only after the
//     admission plugins (ResourceQuota, LimitRanger, NamespaceExists)
//     have observed the object, so a follow-up SSA into the namespace
//     will not race admission policy propagation for the common case.
//   - Argo CD and Flux use the same signal for the same reason:
//     https://github.com/argoproj/argo-cd/blob/master/util/app/applyresource.go
//     https://github.com/fluxcd/kustomize-controller/blob/main/internal/reconcile/kustomization.go
//
// On poll the function issues a Get against the dynamic client (the
// applier is dynamic-only). A transient NotFound — possible on a fresh
// create if the watch cache has not observed the Namespace yet — is
// treated as "phase not yet Active" rather than an immediate failure.
// Every other error fails the wait immediately so cluster-health problems
// surface promptly. When the deadline fires while the phase is still not
// Active (or a NotFound is still in flight) the function returns a
// [DeadlineExceededError] carrying the last observed state so operators
// can distinguish "namespace never appeared" from "namespace stuck in
// Terminating" from "apiserver returning 5xx" on the RPC surface.
func waitForNamespaceActive(ctx context.Context, client dynamic.Interface, name string, timeout time.Duration) error {
	if name == "" {
		return errors.New("waitForNamespaceActive: name must not be empty")
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastPhase string
	var lastErr error
	start := time.Now()

	pollErr := wait.PollUntilContextCancel(waitCtx, namespaceReadyPollInterval, true, func(ctx context.Context) (bool, error) {
		got, err := client.Resource(namespaceGVR).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Could be watch-cache lag right after Create. Record the
				// last error so a deadline surfaces something useful and
				// keep polling.
				lastErr = err
				lastPhase = ""
				return false, nil
			}
			// Other errors are not retried here — the apiserver is
			// returning a real failure. Fail the wait so the RPC surfaces
			// it. (The namespaced-apply retry loop classifies its own
			// errors separately; that loop does not run until the wait
			// succeeds.)
			return false, fmt.Errorf("getting namespace %q: %w", name, err)
		}
		phase, ok := namespacePhase(got)
		lastPhase = phase
		if !ok {
			// No status yet — the controller has not observed the
			// namespace. Keep polling.
			return false, nil
		}
		if phase == string(corev1.NamespaceActive) {
			slog.DebugContext(ctx, "projectapply: namespace reached Active",
				slog.String("namespace", name),
				slog.Duration("elapsed", time.Since(start)),
			)
			return true, nil
		}
		// Terminating or any other non-Active phase is not retriable —
		// retrying would hang until the context times out and surface as
		// DeadlineExceeded to the operator. Fail fast so the RPC can
		// report the real reason (e.g. "namespace foo is Terminating").
		return false, fmt.Errorf("namespace %q not Active: phase=%q", name, phase)
	})
	if pollErr == nil {
		return nil
	}
	if errors.Is(pollErr, context.DeadlineExceeded) || errors.Is(pollErr, context.Canceled) {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		return &DeadlineExceededError{
			Kind:      "Namespace",
			Name:      name,
			Namespace: "",
			LastError: lastErr,
			LastPhase: lastPhase,
		}
	}
	return pollErr
}

// namespacePhase returns the .status.phase string from an unstructured
// namespace object and whether the field was present. Missing phase is
// distinguished from the empty string so the caller can log "not yet
// observed" differently from "Active" — the latter never returns empty.
func namespacePhase(u *unstructured.Unstructured) (string, bool) {
	if u == nil {
		return "", false
	}
	phase, found, err := unstructured.NestedString(u.Object, "status", "phase")
	if err != nil || !found {
		return "", false
	}
	return phase, true
}

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

// Package projectapply implements the three-group apply pipeline
// ADR 034 Decision 4 describes: cluster-scoped Server-Side Apply, then the
// unified Namespace, then — once the namespace controller marks
// `.status.phase == Active` — the namespace-scoped resources, with
// exponential-backoff retry on the documented transient-failure modes.
//
// The applier consumes a [templates.ProjectNamespaceRenderResult] produced
// by the HOL-810 render path: each bucket is already in apply order and
// every object is a validated [unstructured.Unstructured]. The
// CreateProject RPC (HOL-812) will call [Applier.Apply] exactly once per
// project creation.
//
// Ordering rationale is pinned by ADR 034 §4 Decision 4:
//
//  1. Cluster-scoped apply runs first so ClusterRole / ClusterRoleBinding
//     references the rendered Namespace may already name exist by the time
//     namespace-scoped workloads consume them.
//  2. The Namespace is applied via SSA with FieldManager "console.holos.run"
//     (matching console/deployments/apply.go) and Force: true so a
//     preexisting "empty" namespace picked up by a repeat CreateProject
//     is safely reconciled.
//  3. waitForNamespaceActive polls `ns.status.phase == Active`. This is
//     the upstream-documented readiness signal emitted by
//     pkg/controller/namespace:
//     https://github.com/kubernetes/kubernetes/tree/master/pkg/controller/namespace
//  4. Namespace-scoped SSA-apply retries on IsNotFound (RBAC cache has
//     not observed the new namespace), IsForbidden (namespace-scoped RBAC
//     has not propagated yet), IsServerTimeout (apiserver backpressure),
//     and IsInternalError (transient etcd hiccup). Argo CD and Flux
//     adopt the same classifier set in their apply loops:
//     https://github.com/argoproj/argo-cd/blob/master/util/app/applyresource.go
//     https://github.com/fluxcd/kustomize-controller/blob/main/internal/reconcile/kustomization.go
//
// The retry window is bounded by the caller's context plus a 30-second
// ceiling so a never-propagating RBAC policy surfaces a structured
// [DeadlineExceededError] instead of silently hanging the RPC.
package projectapply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"

	"github.com/holos-run/holos-console/console/templates"
)

const (
	// FieldManager is the SSA field-manager identity shared with
	// console/deployments/apply.go. Matching the value is load-bearing:
	// the deployments applier owns the same objects across subsequent
	// render cycles, so a divergent manager name would cause SSA conflict
	// errors on the next Deploy call.
	FieldManager = "console.holos.run"

	// NamespaceReadyTimeout is the hard ceiling waitForNamespaceActive
	// enforces on top of the caller's context. ADR 034 pins this to 30s:
	// a namespace that has not gone Active within 30s is not a transient
	// failure, it is a cluster-health problem, and the RPC should surface
	// it to the operator rather than block.
	NamespaceReadyTimeout = 30 * time.Second

	// namespaceReadyPollInterval is the poll cadence waitForNamespaceActive
	// uses inside the 30s ceiling. 250ms matches the Kubernetes namespace
	// controller's typical reconcile latency on a healthy cluster — fast
	// enough that the common path returns in one or two polls, slow enough
	// that a stuck namespace does not hammer the apiserver.
	namespaceReadyPollInterval = 250 * time.Millisecond
)

// retryableStatusCheckers are the apierrors classifiers
// [retryNamespacedApply] treats as transient per ADR 034 Decision 4. The
// helpers are passed by reference so the caller can observe exactly which
// classifier matched for logging / test assertions without leaking the
// classifier identity into the error message on the happy retry.
var retryableStatusCheckers = []struct {
	name  string
	match func(error) bool
}{
	{"NotFound", apierrors.IsNotFound},
	{"Forbidden", apierrors.IsForbidden},
	{"ServerTimeout", apierrors.IsServerTimeout},
	{"InternalError", apierrors.IsInternalError},
}

// namespaceGVR is the well-known GroupVersionResource for core/v1
// Namespace objects; waitForNamespaceActive uses it to construct a
// cluster-scoped resource client without pulling in the typed
// kubernetes.Interface (the applier is dynamic-only to keep the import
// graph narrow).
var namespaceGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}

// GVRResolver maps a rendered resource's GroupVersionKind to the
// GroupVersionResource the dynamic client needs for SSA. The indirection
// exists so projectapply does not re-hardcode the list of allowed kinds
// that already lives in console/deployments — the CreateProject RPC
// supplies a resolver backed by that list plus any kinds the
// ProjectNamespace render path legitimately produces (Namespace itself,
// cluster-scoped RBAC).
type GVRResolver interface {
	// ResolveGVR returns the GVR for the given GVK. Returns an error when
	// the kind is not allowed for project-namespace apply — the caller
	// has already validated the render output, so a Resolve failure
	// indicates a bug in the allowed-kinds set rather than operator input.
	ResolveGVR(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error)
}

// Applier executes the three-group apply pipeline. One [Applier] per
// CreateProject RPC call is the intended lifetime — the struct is
// stateless apart from its dependencies, so concurrent use with distinct
// render results is safe.
type Applier struct {
	client   dynamic.Interface
	resolver GVRResolver
}

// NewApplier constructs an Applier over the given dynamic client and
// GVR resolver. Callers in production wire a [config.Controller]-derived
// dynamic client and a resolver backed by the allowed-kinds table; tests
// inject a [dynamicfake.FakeDynamicClient] and a small hand-written
// resolver.
func NewApplier(client dynamic.Interface, resolver GVRResolver) *Applier {
	return &Applier{client: client, resolver: resolver}
}

// Apply runs the three-group pipeline over result. The ordering is pinned
// by ADR 034 Decision 4: cluster-scoped, then Namespace, then wait for
// Active, then namespace-scoped with transient-failure retry. Any failure
// is returned immediately — partial application leaves the cluster in a
// well-defined state (every resource already applied via SSA is owned by
// the "console.holos.run" field manager and can be reconciled on the next
// CreateProject retry). result must not be nil and result.Namespace must
// not be nil; the caller (HOL-810 render) guarantees both.
func (a *Applier) Apply(ctx context.Context, result *templates.ProjectNamespaceRenderResult) error {
	if result == nil {
		return errors.New("projectapply: result must not be nil")
	}
	if result.Namespace == nil {
		return errors.New("projectapply: result.Namespace must not be nil")
	}

	// Step 1: cluster-scoped resources first. The ADR orders these before
	// the Namespace because ClusterRole / ClusterRoleBinding / CRD
	// resources the namespaced workloads depend on must exist before the
	// namespace controller starts reconciling workloads that reference
	// them.
	for i := range result.ClusterScoped {
		if err := a.ssaApply(ctx, &result.ClusterScoped[i]); err != nil {
			return fmt.Errorf("applying cluster-scoped %s: %w", describe(&result.ClusterScoped[i]), err)
		}
	}

	// Step 2: the unified Namespace. The HOL-810 render path already merged
	// template-produced patches onto the RPC-built base, so this is a
	// single SSA-apply call.
	if err := a.ssaApply(ctx, result.Namespace); err != nil {
		return fmt.Errorf("applying namespace %q: %w", result.Namespace.GetName(), err)
	}

	// Step 3: wait for the namespace to reach Active. The poll is bounded
	// by both the caller's context AND the 30s ceiling so a slow cluster
	// does not inherit an unbounded deadline from a long-running RPC.
	nsName := result.Namespace.GetName()
	if err := waitForNamespaceActive(ctx, a.client, nsName, NamespaceReadyTimeout); err != nil {
		return err
	}

	// Step 4: namespace-scoped resources. Each SSA-apply is retried on the
	// four documented transient-failure classifiers with exponential
	// backoff bounded by the caller's context + the 30s ceiling.
	for i := range result.NamespaceScoped {
		if err := a.retryNamespacedApply(ctx, &result.NamespaceScoped[i]); err != nil {
			return fmt.Errorf("applying namespaced %s: %w", describe(&result.NamespaceScoped[i]), err)
		}
	}

	return nil
}

// ssaApply issues a single SSA patch with FieldManager+Force per ADR 034.
// Returns whatever error the apiserver surfaces; classification into
// retryable/terminal is the caller's responsibility (retryNamespacedApply
// for the namespaced pass; ssaApply's own callers for cluster-scoped and
// the Namespace itself, which are not retried per the ADR).
func (a *Applier) ssaApply(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	gvr, err := a.resolver.ResolveGVR(gvk)
	if err != nil {
		return fmt.Errorf("resolving GVR for %s: %w", gvk, err)
	}

	data, err := json.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("marshaling %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}

	// Cluster-scoped resources (namespace == "") use the unnamespaced
	// resource client; namespaced resources use .Namespace(ns). Matches
	// the split console/deployments/apply.go uses.
	var rc dynamic.ResourceInterface
	if ns := obj.GetNamespace(); ns != "" {
		rc = a.client.Resource(gvr).Namespace(ns)
	} else {
		rc = a.client.Resource(gvr)
	}

	force := true
	_, err = rc.Patch(
		ctx,
		obj.GetName(),
		types.ApplyPatchType,
		data,
		metav1.PatchOptions{
			FieldManager: FieldManager,
			Force:        &force,
		},
	)
	return err
}

// retryNamespacedApply wraps ssaApply with exponential backoff on the four
// documented transient-failure modes. The retry loop is bounded by the
// caller's context and the 30s [NamespaceReadyTimeout] ceiling — whichever
// fires first terminates the loop. A non-transient error returns
// immediately (no retry); a transient error backs off and retries. When
// the deadline fires while a transient error is active, the loop returns
// a [DeadlineExceededError] so the RPC layer can map it to
// [connect.CodeDeadlineExceeded].
func (a *Applier) retryNamespacedApply(ctx context.Context, obj *unstructured.Unstructured) error {
	// Bound the retry window by min(caller-ctx, 30s). Both deadlines are
	// respected: a short caller deadline fires before the ceiling; a long
	// caller deadline is capped by the ceiling.
	retryCtx, cancel := context.WithTimeout(ctx, NamespaceReadyTimeout)
	defer cancel()

	backoff := wait.Backoff{
		Duration: 250 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
		// Steps is the max number of attempts including the first. We do
		// not rely on it to terminate — the context does — but setting it
		// high enough that the context always wins first (rather than
		// Steps) keeps the error paths narrow.
		Steps: 30,
		Cap:   5 * time.Second,
	}

	var lastErr error
	var lastTransientClassifier string
	var attempts int
	err := wait.ExponentialBackoffWithContext(retryCtx, backoff, func(ctx context.Context) (bool, error) {
		attempts++
		applyErr := a.ssaApply(ctx, obj)
		if applyErr == nil {
			return true, nil
		}
		lastErr = applyErr
		if classifier, ok := classifyTransient(applyErr); ok {
			lastTransientClassifier = classifier
			slog.DebugContext(ctx, "projectapply: transient apply error, retrying",
				slog.String("kind", obj.GetKind()),
				slog.String("name", obj.GetName()),
				slog.String("namespace", obj.GetNamespace()),
				slog.String("classifier", classifier),
				slog.Int("attempt", attempts),
				slog.Any("error", applyErr),
			)
			return false, nil
		}
		// Non-transient: stop the loop and surface the error as-is.
		return false, applyErr
	})
	if err == nil {
		return nil
	}

	// ExponentialBackoffWithContext returns either ctx.Err() (timeout /
	// cancellation) or the non-nil error the condition returned. The
	// timeout case maps to DeadlineExceededError; otherwise pass through.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// Prefer the caller's Canceled signal when the parent ctx was
		// cancelled rather than hitting our 30s ceiling.
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		return &DeadlineExceededError{
			Kind:       obj.GetKind(),
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
			Attempts:   attempts,
			LastError:  lastErr,
			Classifier: lastTransientClassifier,
		}
	}
	return err
}

// classifyTransient returns the name of the apierrors classifier that
// matched err, or ("", false) when err is not one of the documented
// transient-failure modes. The returned name is used for logging and for
// the [DeadlineExceededError.Classifier] field so tests and operators can
// tell which class of transient was stuck.
func classifyTransient(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	for _, c := range retryableStatusCheckers {
		if c.match(err) {
			return c.name, true
		}
	}
	return "", false
}

// describe renders a "kind/namespace/name" identity string for use in
// error wrapping. Namespace is omitted when empty so cluster-scoped
// resources read as "ClusterRole/foo" rather than "ClusterRole//foo".
func describe(u *unstructured.Unstructured) string {
	if ns := u.GetNamespace(); ns != "" {
		return fmt.Sprintf("%s/%s/%s", u.GetKind(), ns, u.GetName())
	}
	return fmt.Sprintf("%s/%s", u.GetKind(), u.GetName())
}

// Package deployments — TemplateGrant cross-namespace LinkedTemplateRef validator.
//
// # Design: ReferenceGrant-style cross-namespace authorization
//
// ValidateGrant is the single public surface that any cross-namespace
// LinkedTemplateRef resolution calls into. It mirrors the Gateway API
// ReferenceGrant semantics: a TemplateGrant must live in the *source* template's
// namespace (the "to" side) and must list the *dependent*'s namespace in its
// From entries (the "from" side) before a cross-namespace reference is allowed.
//
// # Backing store
//
// The validator is backed by TemplateGrantCache, a concurrency-safe in-memory
// snapshot of every TemplateGrant and every Namespace (for label-selector
// evaluation). The cache is populated and kept current by the
// TemplateGrantController defined in internal/controller/template_grant_controller.go.
// The controller calls SetGrants and SetNamespaces on every informer event;
// the validator reads those snapshots under a read lock.
//
// # Hard-revoke semantics (ADR 035, Decision 10)
//
// When a TemplateGrant is deleted the cache is updated immediately and
// ValidateGrant rejects new materialisation attempts through the deleted grant.
// Existing materialised dependency Deployments are NOT removed — callers must
// clean up orphans manually. This file documents that invariant in the function
// header below.
//
// # Three From shapes
//
// ValidateGrant evaluates three forms of TemplateGrantFromRef.Namespace:
//
//  1. Literal namespace — exact match against the dependent's namespace.
//  2. "*" wildcard (HOL-767) — permits any dependent namespace.
//  3. NamespaceSelector — the NamespaceSelector field is evaluated against
//     the labels of the dependent's namespace (read from the Namespace cache).
//     If the dependent's Namespace is absent from the cache the selector match
//     is treated as a miss (not an error) so a missing namespace cannot be
//     used to bypass a pending label check.
package deployments

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// GrantNotFoundError is returned by ValidateGrant when no matching TemplateGrant
// permits the cross-namespace reference. Callers that want to distinguish a
// missing-grant rejection from other errors can use errors.As:
//
//	var notFound *GrantNotFoundError
//	if errors.As(err, &notFound) { ... }
type GrantNotFoundError struct {
	DependentNamespace string
	RequiresNamespace  string
	RequiresName       string
}

func (e *GrantNotFoundError) Error() string {
	return fmt.Sprintf(
		"no TemplateGrant in namespace %q permits namespace %q to reference template %q",
		e.RequiresNamespace, e.DependentNamespace, e.RequiresName,
	)
}

// Validator is the interface consumed by Phase 5 and Phase 6 reconcilers
// (TemplateDependency and TemplateRequirement). It is intentionally minimal:
// a single method that returns nil when the cross-namespace reference is
// permitted and a *GrantNotFoundError when it is not.
type Validator interface {
	// ValidateGrant checks whether the cross-namespace LinkedTemplateRef
	// described by `requires` is authorised from `dependent`'s namespace.
	//
	// Hard-revoke contract: on TemplateGrant deletion the validator
	// immediately rejects new materialisation attempts; existing materialised
	// dependency Deployments are preserved (no cascade). Callers must manage
	// orphan cleanup independently.
	//
	// Returns nil on same-namespace references (no grant required).
	ValidateGrant(ctx context.Context, dependent v1alpha1.LinkedTemplateRef, requires v1alpha1.LinkedTemplateRef) error
}

// TemplateGrantCache is a concurrency-safe in-memory snapshot of TemplateGrant
// and Namespace objects used by ValidateGrant. The TemplateGrantController
// (internal/controller/template_grant_controller.go) calls SetGrants and
// SetNamespaces on every informer event to keep the snapshots current.
//
// A nil *TemplateGrantCache is valid: ValidateGrant returns a GrantNotFoundError
// (safe default-deny) so callers that cannot supply a populated cache do not
// accidentally allow cross-namespace references.
type TemplateGrantCache struct {
	mu         sync.RWMutex
	grants     []v1alpha1.TemplateGrant      // snapshot of all TemplateGrants
	namespaces map[string]corev1.Namespace   // snapshot of Namespaces keyed by name
}

// NewTemplateGrantCache returns an initialised TemplateGrantCache. Callers
// should call SetGrants and SetNamespaces at least once before querying.
func NewTemplateGrantCache() *TemplateGrantCache {
	return &TemplateGrantCache{
		namespaces: make(map[string]corev1.Namespace),
	}
}

// SetGrants replaces the full TemplateGrant snapshot. Called by the controller
// on every TemplateGrant create / update / delete event.
func (c *TemplateGrantCache) SetGrants(grants []v1alpha1.TemplateGrant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grants = grants
}

// SetNamespaces replaces the full Namespace snapshot. Called by the controller
// on every Namespace create / update / delete event.
func (c *TemplateGrantCache) SetNamespaces(nsList []corev1.Namespace) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[string]corev1.Namespace, len(nsList))
	for _, ns := range nsList {
		m[ns.Name] = ns
	}
	c.namespaces = m
}

// ValidateGrant implements Validator. Same-namespace references are always
// allowed without consulting the cache. Cross-namespace references require a
// matching TemplateGrant in the source template's namespace.
//
// Hard-revoke: the function reads the live in-memory cache snapshot; once the
// controller removes a deleted grant from the cache this function starts
// rejecting new cross-namespace references immediately. Existing materialised
// dependency Deployments are not touched — callers own orphan cleanup.
func (c *TemplateGrantCache) ValidateGrant(ctx context.Context, dependent v1alpha1.LinkedTemplateRef, requires v1alpha1.LinkedTemplateRef) error {
	// Same-namespace references never require a TemplateGrant.
	if dependent.Namespace == requires.Namespace {
		return nil
	}

	// nil cache → safe default-deny.
	if c == nil {
		return &GrantNotFoundError{
			DependentNamespace: dependent.Namespace,
			RequiresNamespace:  requires.Namespace,
			RequiresName:       requires.Name,
		}
	}

	c.mu.RLock()
	grants := c.grants
	namespaces := c.namespaces
	c.mu.RUnlock()

	// Retrieve the dependent namespace's labels for NamespaceSelector evaluation.
	var dependentNsLabels labels.Set
	if ns, ok := namespaces[dependent.Namespace]; ok {
		dependentNsLabels = labels.Set(ns.Labels)
	}

	for _, grant := range grants {
		// The grant must live in the source template's namespace.
		if grant.Namespace != requires.Namespace {
			continue
		}

		// If the grant restricts which templates are reachable via `to`,
		// check that the requires ref is covered. An empty `to` list means
		// all templates in the namespace are reachable.
		if !grantCoversTo(grant, requires) {
			continue
		}

		// Evaluate each from entry.
		for _, from := range grant.Spec.From {
			if fromPermitsDependentNamespace(from, dependent.Namespace, dependentNsLabels) {
				return nil
			}
		}
	}

	return &GrantNotFoundError{
		DependentNamespace: dependent.Namespace,
		RequiresNamespace:  requires.Namespace,
		RequiresName:       requires.Name,
	}
}

// grantCoversTo reports whether the grant's To list covers the requires ref.
// An empty To list means all templates in the grant's namespace are reachable.
func grantCoversTo(grant v1alpha1.TemplateGrant, requires v1alpha1.LinkedTemplateRef) bool {
	if len(grant.Spec.To) == 0 {
		return true
	}
	for _, ref := range grant.Spec.To {
		if ref.Namespace == requires.Namespace && ref.Name == requires.Name {
			return true
		}
	}
	return false
}

// fromPermitsDependentNamespace reports whether a single TemplateGrantFromRef
// permits the given dependent namespace. It handles the three from shapes:
//
//  1. Literal "*" wildcard without NamespaceSelector — permits any namespace.
//  2. Literal "*" wildcard with NamespaceSelector — permits any namespace
//     whose labels match the selector (the wildcard scopes the candidate set
//     to all namespaces; the selector then filters that set).
//  3. Exact namespace match without NamespaceSelector — permits exactly the
//     named namespace.
//  4. Exact namespace match with NamespaceSelector — the named namespace must
//     additionally satisfy the selector.
//
// If dependentNsLabels is nil (the namespace is absent from the cache) any
// selector match is treated as a miss so a missing namespace cannot bypass a
// pending label check.
func fromPermitsDependentNamespace(from v1alpha1.TemplateGrantFromRef, dependentNs string, dependentNsLabels labels.Set) bool {
	// Determine whether the dependent namespace is within scope of the
	// From.Namespace field.
	namespaceInScope := false
	if from.Namespace == "*" {
		namespaceInScope = true
	} else if from.Namespace == dependentNs {
		namespaceInScope = true
	}

	if !namespaceInScope {
		return false
	}

	// Namespace is in scope. If no selector is set, the match is unconditional.
	if from.NamespaceSelector == nil {
		return true
	}

	// A NamespaceSelector is set: the dependent namespace must also satisfy it.
	if dependentNsLabels == nil {
		// Namespace not in cache — treat as miss.
		return false
	}
	selector, err := metav1.LabelSelectorAsSelector(from.NamespaceSelector)
	if err != nil {
		// Malformed selector — treat as miss rather than panic.
		return false
	}
	return selector.Matches(dependentNsLabels)
}

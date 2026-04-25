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

// Package policyresolver — AncestorRequirementLister.
//
// This file mirrors ancestor_bindings.go for TemplateRequirement objects.
// The upward ancestor walk, the skip-project-namespaces rule (HOL-554), and
// the fail-open / per-namespace-error contracts are identical. The only
// difference is the CRD kind being collected.
package policyresolver

import (
	"context"
	"log/slog"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// RequirementListerInNamespace reports the TemplateRequirement CRD objects
// stored in a specific Kubernetes namespace. The AncestorRequirementLister
// uses this to fetch requirements from each folder or organization namespace
// in the ancestor chain without importing an external package that would
// create an import cycle.
//
// Implementations MUST only read from folder and organization namespaces.
// The ancestor walker guarantees it never passes a project namespace to this
// method because the ancestor walk skips project-kind namespaces before
// calling the lister, but implementations should still treat a project
// namespace as a programming error and return an empty list.
type RequirementListerInNamespace interface {
	ListRequirementsInNamespace(ctx context.Context, ns string) ([]*templatesv1alpha1.TemplateRequirement, error)
}

// ResolvedRequirement is the decoded form of a TemplateRequirement CRD
// bundled with the owning namespace and the parsed target_refs so the
// TemplateRequirementReconciler can evaluate which Deployments each
// requirement covers.
type ResolvedRequirement struct {
	// Name is the requirement's DNS-label slug (metadata.name).
	Name string
	// Namespace is the folder or organization namespace that owns the
	// TemplateRequirement CRD.
	Namespace string
	// Requires is the LinkedTemplateRef that must be materialised as a
	// singleton Deployment for each matched target.
	Requires templatesv1alpha1.LinkedTemplateRef
	// TargetRefs enumerates the render targets that this requirement
	// applies to. The caller uses bindingAppliesTo / nameMatches (from
	// folder_resolver.go) for matching.
	TargetRefs []templatesv1alpha1.TemplateRequirementTargetRef
	// CascadeDelete mirrors spec.cascadeDelete. Defaults to true when nil.
	CascadeDelete *bool
}

// AncestorRequirementLister walks the ancestor chain of a starting namespace
// and collects every TemplateRequirement CRD stored in the folder and
// organization namespaces on that chain. Project namespaces are skipped to
// mirror the HOL-554 storage-isolation guardrail — a TemplateRequirement in a
// project namespace would allow project owners to bypass platform mandates.
//
// This helper is used by the TemplateRequirementReconciler (HOL-960) to
// evaluate which TemplateRequirements apply to a given Deployment.
type AncestorRequirementLister struct {
	requirementLister RequirementListerInNamespace
	walker            WalkerInterface
	resolver          *resolver.Resolver
}

// NewAncestorRequirementLister returns a lister wired with the given
// dependencies. Any nil dependency yields a lister whose ListRequirements
// method returns an empty slice without error (fail-open behavior).
func NewAncestorRequirementLister(
	requirementLister RequirementListerInNamespace,
	walker WalkerInterface,
	r *resolver.Resolver,
) *AncestorRequirementLister {
	return &AncestorRequirementLister{
		requirementLister: requirementLister,
		walker:            walker,
		resolver:          r,
	}
}

// ListRequirements returns every TemplateRequirement declared in a folder or
// organization namespace on the ancestor chain starting from startNs. The
// returned requirements preserve the walker's order (closest ancestor first)
// within each namespace and the lister's order within each namespace.
//
// A misconfigured lister (any nil dependency) returns (nil, nil) — the
// fail-open contract mirrors AncestorBindingLister so a bootstrap
// misconfiguration degrades to "no requirements" rather than "errors on
// every call".
//
// A walker failure returns (nil, err) so callers can decide how to surface
// the failure (same behavior as AncestorBindingLister).
//
// Individual per-namespace lister errors do not abort traversal; they are
// logged and the namespace is skipped. A single corrupted
// TemplateRequirement must not prevent legitimate requirements in peer
// namespaces from being honored.
func (a *AncestorRequirementLister) ListRequirements(ctx context.Context, startNs string) ([]*ResolvedRequirement, error) {
	if a == nil || a.requirementLister == nil || a.walker == nil || a.resolver == nil {
		slog.WarnContext(ctx, "ancestor requirement lister is misconfigured; returning no requirements",
			slog.String("startNs", startNs),
			slog.Bool("requirementListerNil", a == nil || a.requirementLister == nil),
			slog.Bool("walkerNil", a == nil || a.walker == nil),
			slog.Bool("resolverNil", a == nil || a.resolver == nil),
		)
		return nil, nil
	}

	ancestors, err := a.walker.WalkAncestors(ctx, startNs)
	if err != nil {
		return nil, err
	}

	var out []*ResolvedRequirement
	for _, ns := range ancestors {
		if ns == nil {
			continue
		}
		kind, _, kErr := a.resolver.ResourceTypeFromNamespace(ns.Name)
		if kErr != nil {
			continue
		}
		// Skip project namespaces — the HOL-554 storage-isolation guardrail
		// ensures only folder and organization namespaces host authoritative
		// TemplateRequirement objects. This is the same skip applied in
		// ancestor_bindings.go:138.
		if kind == v1alpha2.ResourceTypeProject {
			continue
		}
		items, listErr := a.requirementLister.ListRequirementsInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list template requirements in ancestor namespace",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}
		for _, req := range items {
			if req == nil {
				continue
			}
			out = append(out, &ResolvedRequirement{
				Name:          req.Name,
				Namespace:     ns.Name,
				Requires:      req.Spec.Requires,
				TargetRefs:    req.Spec.TargetRefs,
				CascadeDelete: req.Spec.CascadeDelete,
			})
		}
	}
	return out, nil
}

// RequirementTargetRefToResolved converts a TemplateRequirementTargetRef
// slice into the ResolvedBinding shape expected by bindingAppliesTo and
// nameMatches so the controller can reuse those functions without duplicating
// the matching logic.
//
// The TemplateRequirementTargetRef and TemplatePolicyBindingTargetRef share
// the same Kind enum, Name, and ProjectName fields, so a thin adapter is all
// that is needed. Exported so the TemplateRequirementReconciler can call it
// without importing an internal helper.
func RequirementTargetRefToResolved(ns, name string, refs []templatesv1alpha1.TemplateRequirementTargetRef) *ResolvedBinding {
	protoRefs := make([]*consolev1.TemplatePolicyBindingTargetRef, 0, len(refs))
	for i := range refs {
		r := &refs[i]
		protoRefs = append(protoRefs, &consolev1.TemplatePolicyBindingTargetRef{
			Kind:        targetKindCRDToProto(r.Kind),
			Name:        r.Name,
			ProjectName: r.ProjectName,
		})
	}
	return &ResolvedBinding{
		Name:       name,
		Namespace:  ns,
		TargetRefs: protoRefs,
	}
}

// BindingAppliesToDeployment is an exported wrapper around bindingAppliesTo
// for the DEPLOYMENT target kind. It lets the TemplateRequirementReconciler
// (which lives in internal/controller) reuse the same matching logic as the
// folderResolver without re-implementing the wildcard semantics.
func BindingAppliesToDeployment(b *ResolvedBinding, project, deploymentName string) bool {
	return bindingAppliesTo(b, project, TargetKindDeployment, deploymentName)
}

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

package policyresolver

import (
	"context"
	"log/slog"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ProjectNamespaceResolver answers the HOL-806 / ADR 034 question: given a new
// project being created under parent namespace `P`, which
// `TemplatePolicyBinding` entries with target kind `ProjectNamespace` apply?
//
// The resolver walks the ancestor chain above `P` (including `P` when it is a
// folder or organization namespace) via AncestorBindingLister, filters the
// returned bindings down to those that carry at least one
// `TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE` target_ref whose
// `project_name` selects `newProjectName`, and returns those bindings. Later
// phases (HOL-810 render, HOL-811 apply, HOL-812 CreateProject wire-up)
// consume the returned bindings, look up the bound TemplatePolicy, collect
// every Template named by the policy's rules, render their `platformResources`
// outputs, and apply the results against the to-be-created namespace.
//
// Ancestor-walk semantics are inherited verbatim from AncestorBindingLister:
// project namespaces are never walked as binding sources (the HOL-554
// storage-isolation guardrail — see ancestor_bindings.go:129-141). The new
// project's namespace does not exist yet at the moment this resolver runs;
// the walk starts from `parentNamespace`, which is always an organization or
// folder namespace (project-on-project nesting is unsupported, see ADR 020
// Decision 3). If a caller accidentally passes a project namespace as
// `parentNamespace`, AncestorBindingLister's skip keeps it from leaking
// project-namespace bindings into the result.
//
// A binding whose `policy_ref` is nil or empty is skipped. This mirrors the
// folder resolver's degrade-gracefully contract (see
// TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError): a
// policyless binding contributes no templates. The actual TemplatePolicy CR
// lookup (and therefore the "policy exists?" check) lives in the later
// render-time phase — this resolver is a pure filter over
// AncestorBindingLister's output so the matching semantics are easy to audit
// and unit-test in isolation.
//
// Wildcard semantics follow HOL-767 / ADR 029 exactly: a binding
// target_ref whose `project_name` is the literal `"*"` matches any non-empty
// project name within the binding's storage-scope ancestor walk. The `name`
// field on ProjectNamespace targets is not meaningful — there is exactly one
// namespace per project — and per ADR 034 the handler requires it to be set
// to `"*"`. The resolver therefore ignores the target_ref's `name` value.
type ProjectNamespaceResolver struct {
	ancestorBindings *AncestorBindingLister
}

// NewProjectNamespaceResolver wires a resolver around an AncestorBindingLister.
// A nil argument yields a resolver whose Resolve method returns an empty slice
// without error — the fail-open contract mirrors AncestorBindingLister and the
// folder resolver: a misconfigured bootstrap degrades to "no bindings match"
// rather than "every CreateProject call errors".
func NewProjectNamespaceResolver(ancestorBindings *AncestorBindingLister) *ProjectNamespaceResolver {
	return &ProjectNamespaceResolver{ancestorBindings: ancestorBindings}
}

// Resolve returns every TemplatePolicyBinding, drawn from the ancestor chain
// above `parentNamespace`, whose target_refs include at least one
// ProjectNamespace target that selects `newProjectName`.
//
// `parentNamespace` is the Kubernetes namespace the new project is being
// created under. It must be a folder or organization namespace; project
// namespaces are rejected by the ancestor walker's storage-isolation skip.
//
// `newProjectName` is the slug of the project being created (not the
// rendered namespace name — the namespace has not been created yet). The
// resolver passes this value through `nameMatches` so a binding with
// `project_name: "*"` covers it, while a binding with a literal
// `project_name` matches only that project.
//
// A nil or misconfigured resolver, or an empty `parentNamespace` /
// `newProjectName`, returns `(nil, nil)` — the fail-open branch matches the
// folder resolver (see folder_resolver.go:159-171). A walker failure returns
// `(nil, err)` so CreateProject can surface the failure to the RPC caller.
//
// The returned slice preserves AncestorBindingLister's order (closest
// ancestor first). Callers that need a deterministic evaluation order
// should sort after — two bindings at different ancestor depths may both
// name the same policy, which the render phase will dedupe by
// `(namespace, name)` when it collects Template refs.
func (r *ProjectNamespaceResolver) Resolve(
	ctx context.Context,
	parentNamespace string,
	newProjectName string,
) ([]*ResolvedBinding, error) {
	if r == nil || r.ancestorBindings == nil {
		slog.WarnContext(ctx, "project namespace resolver is misconfigured; returning no bindings",
			slog.String("parentNamespace", parentNamespace),
			slog.String("newProjectName", newProjectName),
			slog.Bool("resolverNil", r == nil),
			slog.Bool("ancestorBindingsNil", r == nil || r.ancestorBindings == nil),
		)
		return nil, nil
	}
	if parentNamespace == "" || newProjectName == "" {
		// An empty target value would let a `project_name: "*"` binding
		// match every render target that forgot to populate its project
		// slug. The same guardrail that `nameMatches` enforces at the
		// per-ref comparison is hoisted here so we don't spin the walker
		// for a request that cannot contribute anything.
		return nil, nil
	}

	bindings, err := r.ancestorBindings.ListBindings(ctx, parentNamespace)
	if err != nil {
		return nil, err
	}

	var matched []*ResolvedBinding
	for _, b := range bindings {
		if b == nil {
			continue
		}
		// A binding with no resolved policy contributes no templates; the
		// render-time phase would no-op on it anyway. Dropping it here
		// keeps the "missing policy is a no-op" AC mirroring the folder
		// resolver's behavior (see
		// TestFolderResolver_BindingsNonexistentPolicyIsNoopAndDoesNotError).
		if b.PolicyRef == nil {
			continue
		}
		if !projectNamespaceBindingAppliesTo(b, newProjectName) {
			continue
		}
		matched = append(matched, b)
	}
	return matched, nil
}

// projectNamespaceBindingAppliesTo reports whether any of a binding's
// target_refs selects the new project's namespace target.
//
// Unlike bindingAppliesTo (folder_resolver.go) — which matches on
// `(kind, name, project_name)` for PROJECT_TEMPLATE and DEPLOYMENT kinds —
// the ProjectNamespace kind has no meaningful `name` axis (there is exactly
// one namespace per project; ADR 034 requires the handler to store `"*"` in
// `name` for this kind). Match semantics collapse to:
//
//   - `kind` must equal TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE.
//   - `project_name` selects `newProjectName` via `nameMatches` — literal
//     equality or the HOL-767 `"*"` wildcard.
//   - `name` is ignored (the handler enforces it is `"*"`; accepting any
//     value here keeps the resolver resilient to any future drift).
//
// A binding with no target_refs never matches, mirroring bindingAppliesTo
// and TestFolderResolver_BindingsEmptyTargetListContributesNothing.
func projectNamespaceBindingAppliesTo(b *ResolvedBinding, newProjectName string) bool {
	if b == nil {
		return false
	}
	const wantKind = consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE
	for _, tr := range b.TargetRefs {
		if tr == nil {
			continue
		}
		if tr.GetKind() != wantKind {
			continue
		}
		if !nameMatches(tr.GetProjectName(), newProjectName) {
			continue
		}
		return true
	}
	return false
}

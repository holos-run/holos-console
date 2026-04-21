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

// Package projectnspipeline wires the HOL-809 resolver, HOL-810 renderer,
// and HOL-811 applier into a single orchestration used by the
// CreateProject RPC (HOL-812 / ADR 034 phase 6).
//
// The pipeline takes a parent namespace (the to-be-created project's
// immediate org or folder ancestor), the RPC-built base Namespace, and
// the project slug. It answers two questions via [Pipeline.Run]:
//
//  1. Are there any TemplatePolicyBindings with
//     target.kind=ProjectNamespace matching this new project?
//  2. If so, render every template the bindings name, hand the grouped
//     result (cluster-scoped, Namespace, namespace-scoped) to the
//     applier, and return [OutcomeBindingsApplied].
//  3. Otherwise return [OutcomeNoBindings] so the caller runs its
//     existing Namespace-create path unchanged.
//
// The package exposes small interface seams (BindingResolver,
// PolicyGetter, TemplateGetter, Renderer, Applier) so the handler-level
// unit tests in console/projects can inject fakes without pulling the
// full policyresolver / templates stack into test wiring. The
// production wiring in console/console.go threads the real resolvers,
// the CueRendererAdapter, and the projectapply.Applier through these
// seams via adapters.go.
package projectnspipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/projects/projectapply"
	"github.com/holos-run/holos-console/console/templates"
)

// Outcome is what Pipeline.Run tells the caller about the work it did.
// CreateProject branches on this value: NoBindings means fall through to
// the existing Namespace-create path; BindingsApplied means the applier
// already SSA'd the Namespace and every associated resource, so no
// further K8s write is required.
type Outcome int

const (
	// OutcomeNoBindings indicates the ancestor chain above the parent
	// namespace declared no TemplatePolicyBindings whose targets match
	// the new project's namespace. The caller must run its existing
	// Namespace-create path so the feature degrades to a no-op when
	// unused.
	OutcomeNoBindings Outcome = iota
	// OutcomeBindingsApplied indicates at least one binding matched and
	// the applier has completed the three-group SSA pipeline (ADR 034
	// Decision 4). The caller must NOT also call CreateProject on the
	// typed k8s client — the namespace is already SSA'd.
	OutcomeBindingsApplied
)

// BindingResolver resolves TemplatePolicyBindings whose ProjectNamespace
// targets match a to-be-created project. Implemented in production by
// [policyresolver.ProjectNamespaceResolver]; the handler tests supply a
// simple stub that returns a pre-canned slice.
type BindingResolver interface {
	Resolve(ctx context.Context, parentNamespace string, newProjectName string) ([]*policyresolver.ResolvedBinding, error)
}

// PolicyGetter fetches a single TemplatePolicy by namespace+name. The
// resolver only hands us the binding's policy_ref; we still need to
// dereference the ref to collect the rules (and therefore the Template
// refs). Production wiring uses the templatepolicies K8sClient; tests
// use an in-memory map.
type PolicyGetter interface {
	GetPolicy(ctx context.Context, namespace, name string) (*Policy, error)
}

// TemplateGetter fetches a single Template by namespace+name and
// returns its CUE source. Production wiring uses the templates
// K8sClient; tests use an in-memory map.
type TemplateGetter interface {
	GetTemplateSource(ctx context.Context, namespace, name string) (string, error)
}

// Renderer evaluates the collected template sources against the base
// namespace and returns a grouped result. Matches
// [templates.CueRendererAdapter.RenderForProjectNamespace] but wrapped
// in an interface so handler-level tests can inject a stub (useful for
// the "render error" AC).
type Renderer interface {
	RenderForProjectNamespace(ctx context.Context, in templates.ProjectNamespaceRenderInput) (*templates.ProjectNamespaceRenderResult, error)
}

// Applier runs the three-group SSA apply pipeline. Matches
// [projectapply.Applier.Apply] as an interface so tests can inject a
// stub that returns a canned [projectapply.DeadlineExceededError] for
// the apply-timeout AC.
type Applier interface {
	Apply(ctx context.Context, result *templates.ProjectNamespaceRenderResult) error
}

// Policy is the minimal decoded form the pipeline needs from a
// TemplatePolicy: the list of (template namespace, template name) refs
// its REQUIRE rules point at. EXCLUDE rules are ignored by the
// ProjectNamespace render path — ADR 034 Decision 3 leaves EXCLUDE
// semantics to the deployment-time resolver; a ProjectNamespace binding
// always contributes templates, never strips them, because there is no
// baseline set to strip from.
//
// The type lives here rather than in the templatepolicies package to
// keep handler-level tests free of the full CRD shape.
type Policy struct {
	// Namespace is the namespace the TemplatePolicy CRD lives in.
	// Carried for audit logging / error messages only.
	Namespace string
	// Name is the TemplatePolicy's name.
	Name string
	// TemplateRefs enumerates the Template CRs the policy's REQUIRE
	// rules name. Each ref uses the same (namespace, name) shape as
	// the CRD's linked_template_ref.
	TemplateRefs []TemplateRef
}

// TemplateRef identifies a Template CR by namespace+name. The render
// path dereferences each ref to the Template's CUE source string via
// TemplateGetter. VersionConstraint is carried forward for audit but
// version pinning is outside the ProjectNamespace render-phase scope
// (the deployment render path handles that; ADR 034 Decision 2 keeps
// ProjectNamespace renders simple).
type TemplateRef struct {
	Namespace         string
	Name              string
	VersionConstraint string
}

// Input carries the per-RPC values Run needs to resolve → render → apply.
type Input struct {
	// ProjectName is the slug of the new project (e.g. "frontend").
	ProjectName string
	// ParentNamespace is the immediate ancestor namespace the resolver
	// walks from (an organization or folder namespace). The new
	// project's namespace does not exist yet; the walk starts here.
	// ADR 034 open question — the ancestor namespace rule for
	// projects created under folders in a future folders-with-depth
	// feature — is deferred to HOL-806 Phase 7. For now we pass the
	// immediate parent the RPC already resolved; the helper
	// Handler.parentAncestorNamespace centralises this so the future
	// fix touches one call site.
	ParentNamespace string
	// BaseNamespace is the Namespace object the RPC has already built
	// (labels, annotations, share-grants). It becomes the "base" the
	// render path unifies template-produced Namespace patches into
	// (ADR 034 Decision 1 — "the RPC-built namespace is always the
	// base").
	BaseNamespace *corev1.Namespace
	// Platform is the platform-input block the renderer binds at the
	// CUE `platform` path. Callers fill this with
	// (organization, project, namespace, gatewayNamespace, claims,
	// folders) per the same shape the deployments render path uses.
	Platform v1alpha2.PlatformInput
}

// Pipeline orchestrates the HOL-812 resolve → render → apply flow. One
// Pipeline per process is intended (the struct is stateless apart from
// its seams) and it is safe to call Run concurrently with distinct
// Inputs.
type Pipeline struct {
	resolver  BindingResolver
	policies  PolicyGetter
	templates TemplateGetter
	renderer  Renderer
	applier   Applier
}

// New constructs a Pipeline wired with the given seams. Any nil seam
// makes Run fail open (returns OutcomeNoBindings without an error) —
// the handler treats a misconfigured pipeline the same way it would
// treat a nil pipeline: fall through to the existing Namespace-create
// path so CreateProject keeps working during a partial bootstrap.
func New(
	resolver BindingResolver,
	policies PolicyGetter,
	templateSources TemplateGetter,
	renderer Renderer,
	applier Applier,
) *Pipeline {
	return &Pipeline{
		resolver:  resolver,
		policies:  policies,
		templates: templateSources,
		renderer:  renderer,
		applier:   applier,
	}
}

// Run executes the resolve → render → apply pipeline for one
// CreateProject call. Returns OutcomeNoBindings when no bindings match
// (the caller runs its existing path); OutcomeBindingsApplied when the
// applier succeeded (the caller returns the RPC response as-is).
//
// Audit log lines are emitted at every state transition so operators
// can trace which path a CreateProject request took:
//
//   - project_ns_bindings_resolved (Info) — always, with match_count
//   - project_ns_resolve_error     (Warn) — resolver returned an error
//   - project_ns_render_ok         (Info) — render succeeded, counts
//   - project_ns_render_error      (Warn) — render failed
//   - project_ns_apply_ok          (Info) — applier returned nil
//   - project_ns_apply_timeout     (Warn) — projectapply.DeadlineExceededError
//   - project_ns_apply_error       (Warn) — any other applier error
//
// A resolver, renderer, or applier returning an error aborts the
// pipeline immediately and the error propagates to the RPC layer. The
// handler translates [projectapply.DeadlineExceededError] to
// connect.CodeDeadlineExceeded; other errors go through mapK8sError or
// connect.CodeInternal depending on their kind.
func (p *Pipeline) Run(ctx context.Context, in Input) (Outcome, error) {
	if p == nil || p.resolver == nil || p.policies == nil || p.templates == nil || p.renderer == nil || p.applier == nil {
		slog.DebugContext(ctx, "project namespace pipeline is not fully wired; skipping",
			slog.String("project", in.ProjectName),
			slog.String("parentNamespace", in.ParentNamespace),
		)
		return OutcomeNoBindings, nil
	}
	if in.BaseNamespace == nil {
		return OutcomeNoBindings, errors.New("projectnspipeline: BaseNamespace must not be nil")
	}
	if in.ProjectName == "" {
		return OutcomeNoBindings, errors.New("projectnspipeline: ProjectName must not be empty")
	}
	if in.ParentNamespace == "" {
		// No parent to walk — there can be no ancestor bindings. This
		// mirrors the ProjectNamespaceResolver's own fail-open branch
		// (project_namespace_resolver.go:116-123). Fall through without
		// contacting the cluster.
		return OutcomeNoBindings, nil
	}

	bindings, err := p.resolver.Resolve(ctx, in.ParentNamespace, in.ProjectName)
	if err != nil {
		slog.WarnContext(ctx, "project namespace bindings resolve error",
			slog.String("action", "project_ns_resolve_error"),
			slog.String("project", in.ProjectName),
			slog.String("parent_namespace", in.ParentNamespace),
			slog.Any("error", err),
		)
		return OutcomeNoBindings, fmt.Errorf("resolving project namespace bindings: %w", err)
	}

	slog.InfoContext(ctx, "project namespace bindings resolved",
		slog.String("action", "project_ns_bindings_resolved"),
		slog.String("project", in.ProjectName),
		slog.String("parent_namespace", in.ParentNamespace),
		slog.Int("match_count", len(bindings)),
	)

	if len(bindings) == 0 {
		return OutcomeNoBindings, nil
	}

	// Collect the Template sources every binding's policy points at.
	// Dedupe by (namespace, name) so two bindings that both reference
	// the same Template CR do not re-render it. Ordering follows
	// ancestor-walk order (closest ancestor first) — AncestorBindingLister's
	// documented contract — so a folder binding takes precedence over
	// the organization's default when both exist.
	sources, err := p.collectTemplateSources(ctx, bindings)
	if err != nil {
		// Collection errors (policy-not-found, template-not-found) are
		// classified as render-time errors for audit purposes: they
		// block the same AC and map to the same connect code as a CUE
		// evaluation failure. Keeping the audit action consistent keeps
		// the operator-facing error surface small.
		slog.WarnContext(ctx, "project namespace render error",
			slog.String("action", "project_ns_render_error"),
			slog.String("project", in.ProjectName),
			slog.String("parent_namespace", in.ParentNamespace),
			slog.Int("binding_count", len(bindings)),
			slog.Any("error", err),
		)
		return OutcomeNoBindings, fmt.Errorf("collecting project namespace template sources: %w", err)
	}

	renderInput := templates.ProjectNamespaceRenderInput{
		ProjectName:     in.ProjectName,
		NamespaceName:   in.BaseNamespace.Name,
		Platform:        in.Platform,
		TemplateSources: sources,
		BaseNamespace:   in.BaseNamespace,
	}
	rendered, err := p.renderer.RenderForProjectNamespace(ctx, renderInput)
	if err != nil {
		slog.WarnContext(ctx, "project namespace render error",
			slog.String("action", "project_ns_render_error"),
			slog.String("project", in.ProjectName),
			slog.String("parent_namespace", in.ParentNamespace),
			slog.Int("binding_count", len(bindings)),
			slog.Int("template_source_count", len(sources)),
			slog.Any("error", err),
		)
		return OutcomeNoBindings, fmt.Errorf("rendering project namespace: %w", err)
	}

	slog.InfoContext(ctx, "project namespace render ok",
		slog.String("action", "project_ns_render_ok"),
		slog.String("project", in.ProjectName),
		slog.String("parent_namespace", in.ParentNamespace),
		slog.Int("binding_count", len(bindings)),
		slog.Int("template_source_count", len(sources)),
		slog.Int("cluster_scoped_count", len(rendered.ClusterScoped)),
		slog.Int("namespace_scoped_count", len(rendered.NamespaceScoped)),
	)

	if err := p.applier.Apply(ctx, rendered); err != nil {
		var dl *projectapply.DeadlineExceededError
		if errors.As(err, &dl) {
			slog.WarnContext(ctx, "project namespace apply timeout",
				slog.String("action", "project_ns_apply_timeout"),
				slog.String("project", in.ProjectName),
				slog.String("parent_namespace", in.ParentNamespace),
				slog.String("blocked_kind", dl.Kind),
				slog.String("blocked_name", dl.Name),
				slog.String("blocked_namespace", dl.Namespace),
				slog.Int("attempts", dl.Attempts),
				slog.String("classifier", dl.Classifier),
				slog.Any("error", err),
			)
			return OutcomeBindingsApplied, fmt.Errorf("applying project namespace: %w", err)
		}
		slog.WarnContext(ctx, "project namespace apply error",
			slog.String("action", "project_ns_apply_error"),
			slog.String("project", in.ProjectName),
			slog.String("parent_namespace", in.ParentNamespace),
			slog.Any("error", err),
		)
		return OutcomeBindingsApplied, fmt.Errorf("applying project namespace: %w", err)
	}

	slog.InfoContext(ctx, "project namespace apply ok",
		slog.String("action", "project_ns_apply_ok"),
		slog.String("project", in.ProjectName),
		slog.String("parent_namespace", in.ParentNamespace),
		slog.Int("binding_count", len(bindings)),
		slog.Int("cluster_scoped_count", len(rendered.ClusterScoped)),
		slog.Int("namespace_scoped_count", len(rendered.NamespaceScoped)),
	)
	return OutcomeBindingsApplied, nil
}

// collectTemplateSources dereferences every binding's policy_ref, walks
// the policy's REQUIRE rules, and collects the CUE source for each
// Template ref. Dedupes by (namespace, name) so a Template named by
// multiple bindings/rules is only rendered once. A binding whose
// policy_ref does not resolve fails the collection — the operator
// pointed at a non-existent policy, which should surface to the RPC
// caller rather than silently rendering an incomplete set.
func (p *Pipeline) collectTemplateSources(ctx context.Context, bindings []*policyresolver.ResolvedBinding) ([]string, error) {
	var sources []string
	// Track (templateNamespace, templateName) so duplicates are rendered once.
	type tmplKey struct{ ns, name string }
	seenTmpl := make(map[tmplKey]bool)
	// Cache policy fetches by (policyNamespace, policyName). Multiple
	// bindings at different ancestor depths can reference the same
	// policy — a single GetPolicy call per unique ref is enough.
	type polKey struct{ ns, name string }
	policyCache := make(map[polKey]*Policy)

	for _, b := range bindings {
		if b == nil || b.PolicyRef == nil {
			// Already filtered by the resolver (project_namespace_resolver.go:140-142).
			// Defensive re-check: a future resolver refactor that stops
			// filtering must still be safe here.
			continue
		}
		pk := polKey{ns: b.PolicyRef.GetNamespace(), name: b.PolicyRef.GetName()}
		policy, cached := policyCache[pk]
		if !cached {
			fetched, err := p.policies.GetPolicy(ctx, pk.ns, pk.name)
			if err != nil {
				return nil, fmt.Errorf("loading policy %s/%s referenced by binding %s/%s: %w",
					pk.ns, pk.name, b.Namespace, b.Name, err)
			}
			policy = fetched
			policyCache[pk] = policy
		}
		if policy == nil {
			continue
		}
		for _, ref := range policy.TemplateRefs {
			if ref.Name == "" {
				continue
			}
			key := tmplKey{ns: ref.Namespace, name: ref.Name}
			if seenTmpl[key] {
				continue
			}
			seenTmpl[key] = true
			src, err := p.templates.GetTemplateSource(ctx, ref.Namespace, ref.Name)
			if err != nil {
				return nil, fmt.Errorf("loading template %s/%s (from policy %s/%s): %w",
					ref.Namespace, ref.Name, pk.ns, pk.name, err)
			}
			if src == "" {
				continue
			}
			sources = append(sources, src)
		}
	}
	return sources, nil
}

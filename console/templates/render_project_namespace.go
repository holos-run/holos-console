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

package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
)

// clusterScopedKinds is the rule-of-thumb set of Kubernetes kinds the
// ProjectNamespace render path treats as cluster-scoped when deciding apply
// order. These kinds always land in the "before namespace" group regardless
// of which CUE collection they were emitted from. ADR 034 Decision 4 orders
// cluster-scoped apply before the namespace and namespace-scoped apply after
// the namespace is Active, so a correct split here is load-bearing.
//
// The list covers the kinds a ProjectNamespace template is expected to
// produce (Namespace itself for the patch, plus the handful of common
// cluster-scoped RBAC and CRD kinds). Everything else is namespace-scoped
// and defaulted into the new project namespace when the template omits
// metadata.namespace.
var clusterScopedKinds = map[string]bool{
	"Namespace":                      true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"CustomResourceDefinition":       true,
	"APIService":                     true,
	"PersistentVolume":               true,
	"StorageClass":                   true,
	"PriorityClass":                  true,
	"ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration":   true,
}

// ProjectNamespaceRenderInput carries the values RenderForProjectNamespace
// needs to evaluate each binding's Template. Callers (the CreateProject
// RPC wire-up in HOL-812) pass the project slug, the parent ancestor
// namespace, the platform input block and the RPC-constructed base
// Namespace object.
type ProjectNamespaceRenderInput struct {
	// ProjectName is the slug of the project being created. Surfaces in
	// CUE as `input.name` (via ProjectInput) so templates can parameterise
	// on it.
	ProjectName string
	// NamespaceName is the full Kubernetes namespace name for the project
	// (e.g. "holos-prj-foo"). Used to default metadata.namespace on
	// namespace-scoped resources that omit it and as the platform.namespace
	// value. Must be non-empty.
	NamespaceName string
	// Platform is the platform-input block unified into the template at the
	// `platform` CUE path. Callers are responsible for populating it with
	// the same shape `CueRenderer.Render` expects.
	Platform v1alpha2.PlatformInput
	// TemplateSources is the ordered list of CUE template source strings
	// to evaluate. Each entry is rendered independently and its
	// platformResources outputs are merged into the result. Callers load
	// these from the resolved TemplatePolicyBinding rules (HOL-809) —
	// typically by dereferencing each rule's Template ref to the
	// `spec.cueTemplate` field on the Template CR.
	TemplateSources []string
	// BaseNamespace is the Namespace object the RPC has already constructed
	// (labels/annotations built from the CreateProject request). It is the
	// "base" side of the unification — template-produced Namespace patches
	// merge into it. Must be non-nil.
	BaseNamespace *corev1.Namespace
}

// ProjectNamespaceRenderResult groups the rendered resources into the three
// ordered buckets the HOL-811 applier needs:
//
//  1. ClusterScoped — applied before Namespace creation.
//  2. Namespace    — applied as the Namespace itself.
//  3. NamespaceScoped — applied after Namespace.status.phase == Active.
//
// Each field carries zero or more validated [unstructured.Unstructured]
// objects. Namespace is always non-nil (it is the unified merge of the
// BaseNamespace and any template-produced Namespace patches).
type ProjectNamespaceRenderResult struct {
	// Namespace is the unified Namespace object — template patches merged
	// with the RPC-built base. Never nil.
	Namespace *unstructured.Unstructured
	// ClusterScoped are resources that must be applied before the
	// namespace exists. Includes any cluster-scoped Kubernetes kinds
	// the template produces under platformResources.clusterResources,
	// and any kind in clusterScopedKinds regardless of source.
	ClusterScoped []unstructured.Unstructured
	// NamespaceScoped are resources that must be applied after the
	// namespace becomes Active. Each entry's metadata.namespace is
	// defaulted to NamespaceName when the template omits it.
	NamespaceScoped []unstructured.Unstructured
}

// RenderForProjectNamespace evaluates each binding-contributed CUE template
// under the "org/folder" render level (ReadPlatformResources == true), reads
// only `platformResources.namespacedResources` and
// `platformResources.clusterResources` (ADR 034 Decision 2), and returns
// three ordered groups suitable for the HOL-811 apply path.
//
// `projectResources` outputs are intentionally ignored: a
// TemplatePolicyBinding targeting a ProjectNamespace lives in the ancestor
// (org/folder) namespace, and the project namespace does not yet exist at
// render time — ADR 016 Decision 8 keeps project-level emissions out of the
// platform-level boundary and vice-versa.
//
// Each template source is evaluated independently (rather than unified into
// one CUE document) so two templates that independently produce a Namespace
// patch can still be merged field-by-field at the Go layer. This mirrors
// the unification semantics of org-template render in
// TestCueRenderer_OrgTemplateUnification without leaking the CUE side's
// "conflicting values are a CUE error" behavior into the operator-facing
// error surface.
//
// Returns an error when:
//
//   - in or any required field is missing;
//   - a template fails to compile or read under EvaluateGroupedCUE;
//   - two templates (or a template and the base) produce conflicting
//     values for the same Namespace field — Namespace.Labels,
//     Namespace.Annotations, or Namespace.Finalizers — with the same key
//     but different values. Same key / same value is a no-op. Partial
//     rendering is never returned.
//
// The caller is expected to pass at most the bindings matched by the
// ProjectNamespaceResolver (HOL-809). An empty TemplateSources slice is
// valid and returns the base Namespace unchanged with empty cluster and
// namespace-scoped groups.
func (a *CueRendererAdapter) RenderForProjectNamespace(ctx context.Context, in ProjectNamespaceRenderInput) (*ProjectNamespaceRenderResult, error) {
	if err := validateProjectNamespaceInput(in); err != nil {
		return nil, err
	}

	baseUnstructured, err := namespaceToUnstructured(in.BaseNamespace)
	if err != nil {
		return nil, fmt.Errorf("converting base Namespace to unstructured: %w", err)
	}

	result := &ProjectNamespaceRenderResult{
		Namespace: baseUnstructured,
	}

	platformJSON, err := platformInputCUE(in.Platform)
	if err != nil {
		return nil, err
	}
	// The render path synthesises a ProjectInput block so templates can
	// reference `input.name` when they parameterise on the project slug.
	// ProjectNamespace renders do not consume user-supplied ProjectInput
	// (the project is being created; there is no deployment form yet).
	projectJSON, err := projectInputCUE(v1alpha2.ProjectInput{Name: in.ProjectName})
	if err != nil {
		return nil, err
	}

	for i, src := range in.TemplateSources {
		combined := combineCueSource(src, nil, platformJSON, projectJSON)
		grouped, err := deployments.EvaluateGroupedCUE(ctx, combined, true)
		if err != nil {
			return nil, fmt.Errorf("rendering ProjectNamespace template %d: %w", i, err)
		}
		if err := mergeGroupedIntoResult(grouped, in.NamespaceName, result); err != nil {
			return nil, fmt.Errorf("merging ProjectNamespace template %d: %w", i, err)
		}
	}

	return result, nil
}

// validateProjectNamespaceInput enforces the required fields on the input
// struct. Returns a descriptive error for misuse so bugs in the
// CreateProject wire-up surface immediately rather than as obscure CUE
// evaluation failures downstream.
func validateProjectNamespaceInput(in ProjectNamespaceRenderInput) error {
	if in.ProjectName == "" {
		return fmt.Errorf("RenderForProjectNamespace: ProjectName must not be empty")
	}
	if in.NamespaceName == "" {
		return fmt.Errorf("RenderForProjectNamespace: NamespaceName must not be empty")
	}
	if in.BaseNamespace == nil {
		return fmt.Errorf("RenderForProjectNamespace: BaseNamespace must not be nil")
	}
	if in.BaseNamespace.Name != in.NamespaceName {
		return fmt.Errorf("RenderForProjectNamespace: BaseNamespace.Name %q does not match NamespaceName %q",
			in.BaseNamespace.Name, in.NamespaceName)
	}
	return nil
}

// platformInputCUE marshals a PlatformInput to a CUE source snippet bound
// at the `platform` path. Mirrors what deployments.evaluateWithInputs does
// via FillPath, but writes a concatenated CUE string instead so the raw-CUE
// EvaluateGroupedCUE entry point (which expects a pre-concatenated
// document) can consume the result unchanged.
func platformInputCUE(in v1alpha2.PlatformInput) (string, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("encoding platform input: %w", err)
	}
	return fmt.Sprintf("platform: %s\n", string(b)), nil
}

// projectInputCUE marshals a ProjectInput the same way. The resolver does
// not surface user-supplied project input at CreateProject time, but
// templates that reference `input.name` (a common pattern) need the slug
// bound so they compile.
func projectInputCUE(in v1alpha2.ProjectInput) (string, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("encoding project input: %w", err)
	}
	return fmt.Sprintf("input: %s\n", string(b)), nil
}

// namespaceToUnstructured converts a corev1.Namespace (the RPC-built base)
// to an *unstructured.Unstructured so the render result uses the same
// shape for both the template-side patches and the base. This keeps the
// unification helpers generic — they merge unstructured into unstructured.
func namespaceToUnstructured(ns *corev1.Namespace) (*unstructured.Unstructured, error) {
	// Ensure TypeMeta is populated so callers do not receive a Namespace
	// without apiVersion/kind (which would fail SSA downstream).
	out := ns.DeepCopy()
	if out.APIVersion == "" {
		out.APIVersion = "v1"
	}
	if out.Kind == "" {
		out.Kind = "Namespace"
	}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(out)
	if err != nil {
		return nil, err
	}
	// Drop the spec/status fields Kubernetes populates on the server side
	// so the apply request is a minimal patch.
	delete(raw, "spec")
	delete(raw, "status")
	return &unstructured.Unstructured{Object: raw}, nil
}

// mergeGroupedIntoResult walks the resources emitted by a single template
// evaluation and routes each into the correct bucket on result:
//
//   - Namespace resources are unified with result.Namespace.
//   - Other cluster-scoped resources append to result.ClusterScoped.
//   - Namespace-scoped resources append to result.NamespaceScoped after
//     their metadata.namespace is defaulted to namespaceName when empty.
//
// Inputs from projectResources (grouped.Project) are discarded per ADR 034
// Decision 2 — a ProjectNamespace render reads platformResources only.
func mergeGroupedIntoResult(grouped *deployments.GroupedResources, namespaceName string, result *ProjectNamespaceRenderResult) error {
	if grouped == nil {
		return nil
	}
	for i := range grouped.Platform {
		u := &grouped.Platform[i]
		kind := u.GetKind()
		if kind == "Namespace" {
			if err := unifyNamespace(result.Namespace, u); err != nil {
				return err
			}
			continue
		}
		if clusterScopedKinds[kind] {
			// Cluster-scoped resources must not carry a namespace. The
			// cluster-walker already enforces this for resources that
			// arrive via platformResources.clusterResources; the check
			// here covers the rare case a cluster-scoped kind lands in
			// the namespaced side of the CUE tree.
			u.SetNamespace("")
			result.ClusterScoped = append(result.ClusterScoped, *u)
			continue
		}
		// Namespace-scoped resource. Default metadata.namespace to the
		// project namespace when the template omits it (per the AC
		// rule-of-thumb); otherwise keep the explicit value — a template
		// may legitimately target a sibling namespace owned by the same
		// platform.
		if u.GetNamespace() == "" {
			u.SetNamespace(namespaceName)
		}
		result.NamespaceScoped = append(result.NamespaceScoped, *u)
	}
	return nil
}

// unifyNamespace merges a template-produced Namespace patch into the
// result's running Namespace. The merge is field-by-field with a strict
// conflict rule: same key and same value is a no-op; same key and
// different values is an error. Callers must not rely on ordering — the
// merge is commutative for valid inputs.
//
// The merge covers:
//
//   - metadata.labels
//   - metadata.annotations
//   - metadata.finalizers (unioned by value)
//
// Other fields (spec, status, name) are not mergeable — name is validated
// to match; spec/status are ignored. A template that attempts to rename
// the namespace by setting metadata.name to a different value fails the
// merge.
func unifyNamespace(base, patch *unstructured.Unstructured) error {
	if base == nil {
		return fmt.Errorf("unifyNamespace: base is nil")
	}
	if patch == nil {
		return nil
	}
	if patchName := patch.GetName(); patchName != "" && patchName != base.GetName() {
		return fmt.Errorf("namespace patch name %q does not match project namespace %q",
			patchName, base.GetName())
	}
	if err := mergeStringMap(base, patch, "metadata", "labels"); err != nil {
		return err
	}
	if err := mergeStringMap(base, patch, "metadata", "annotations"); err != nil {
		return err
	}
	if err := mergeStringSlice(base, patch, "metadata", "finalizers"); err != nil {
		return err
	}
	return nil
}

// mergeStringMap merges patch's map-typed field into base's, keyed by
// identity of the unstructured path. Missing fields are created; conflicts
// on the same key with different string values fail the merge.
func mergeStringMap(base, patch *unstructured.Unstructured, path ...string) error {
	patchMap, err := readStringMap(patch, path...)
	if err != nil {
		return err
	}
	if len(patchMap) == 0 {
		return nil
	}
	baseMap, err := readStringMap(base, path...)
	if err != nil {
		return err
	}
	if baseMap == nil {
		baseMap = map[string]string{}
	}
	for k, pv := range patchMap {
		if bv, ok := baseMap[k]; ok && bv != pv {
			return fmt.Errorf("namespace %s[%q]: conflict %q vs %q",
				joinPath(path), k, bv, pv)
		}
		baseMap[k] = pv
	}
	return writeStringMap(base, baseMap, path...)
}

// mergeStringSlice unions two string slices at the same unstructured path.
// Unlike mergeStringMap there is no concept of a "conflict" — duplicate
// entries are deduped in-order (the first occurrence wins).
func mergeStringSlice(base, patch *unstructured.Unstructured, path ...string) error {
	patchSlice, err := readStringSlice(patch, path...)
	if err != nil {
		return err
	}
	if len(patchSlice) == 0 {
		return nil
	}
	baseSlice, err := readStringSlice(base, path...)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(baseSlice)+len(patchSlice))
	merged := make([]string, 0, len(baseSlice)+len(patchSlice))
	for _, v := range baseSlice {
		if seen[v] {
			continue
		}
		seen[v] = true
		merged = append(merged, v)
	}
	for _, v := range patchSlice {
		if seen[v] {
			continue
		}
		seen[v] = true
		merged = append(merged, v)
	}
	return writeStringSlice(base, merged, path...)
}

// readStringMap reads a string-keyed, string-valued map at the given
// unstructured path. Returns (nil, nil) when the field is missing so
// callers can distinguish "unset" from "empty map".
func readStringMap(u *unstructured.Unstructured, path ...string) (map[string]string, error) {
	raw, found, err := unstructured.NestedMap(u.Object, path...)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field %s[%q] is not a string (got %T)",
				joinPath(path), k, v)
		}
		out[k] = s
	}
	return out, nil
}

// writeStringMap writes a string-to-string map at the unstructured path,
// converting through map[string]any so the unstructured representation
// stays self-consistent.
func writeStringMap(u *unstructured.Unstructured, m map[string]string, path ...string) error {
	if len(m) == 0 {
		unstructured.RemoveNestedField(u.Object, path...)
		return nil
	}
	raw := make(map[string]any, len(m))
	for k, v := range m {
		raw[k] = v
	}
	return unstructured.SetNestedMap(u.Object, raw, path...)
}

// readStringSlice reads a []string at the unstructured path.
func readStringSlice(u *unstructured.Unstructured, path ...string) ([]string, error) {
	raw, found, err := unstructured.NestedStringSlice(u.Object, path...)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return raw, nil
}

// writeStringSlice writes a []string at the unstructured path.
func writeStringSlice(u *unstructured.Unstructured, s []string, path ...string) error {
	return unstructured.SetNestedStringSlice(u.Object, s, path...)
}

// joinPath renders a path slice as a dotted string for error messages.
// The unstructured-field helpers take variadic string arguments; this
// helper lets error strings display the full CUE-ish path.
func joinPath(path []string) string {
	return strings.Join(path, ".")
}

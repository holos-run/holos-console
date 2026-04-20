// Package templates — K8sClient storage layer.
//
// HOL-621 rewrote this file to type the Template CRUD surface against the
// templates.holos.run/v1alpha1 Template CRD and read/write through a
// controller-runtime client.Client. Reads hit the cache the controller
// manager populates; writes fall through to the API server and the cache
// observes them on the next watch event, so read-your-own-write semantics
// no longer depend on a local CoreV1 ConfigMap cache.
//
// HOL-693 extended the same pattern to Release storage: releases are now
// TemplateRelease CRDs in the same group. The kubernetes.Interface (client-go)
// argument dropped off NewK8sClient at the same time — no non-Namespace call
// through it remained. Namespace reads still flow through the Resolver.
//
// Signature shape: every Template method takes a Kubernetes namespace and
// a resource name. The namespace is the authoritative identifier per
// HOL-619; callers that still think in terms of (scope, scopeName) compute
// the namespace through the package-level namespaceForScope helper or the
// resolver directly.
package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Masterminds/semver/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

const (
	CueTemplateKey = "template.cue"

	// DefaultReferenceGrantName is the name of the seeded built-in platform template.
	DefaultReferenceGrantName = "reference-grant"
)

// K8sClient performs Template and TemplateRelease CRUD against the
// templates.holos.run/v1alpha1 CRDs via a controller-runtime client.Client.
// Reads hit the informer cache populated by the embedded controller manager;
// writes fall through to the API server and the cache observes them on the
// next watch event.
type K8sClient struct {
	// client is the cache-backed controller-runtime client for Template
	// and TemplateRelease CRDs.
	client ctrlclient.Client
	// Resolver maps scope pairs to namespaces and back. Exported so
	// handlers can reuse the same resolver instance when they still think
	// in (scope, scopeName) terms.
	Resolver *resolver.Resolver
}

// NewK8sClient wires the controller-runtime client.Client used for
// Template and TemplateRelease CRDs alongside the namespace resolver.
func NewK8sClient(cl ctrlclient.Client, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: cl, Resolver: r}
}

// namespaceForScope returns the Kubernetes namespace for the given scope and
// name. Retained as a method so callers that still hold a (scope, scopeName)
// pair (notably handler.go during the HOL-619/HOL-621 transition) can ask the
// K8sClient to resolve through its own resolver.
func (k *K8sClient) namespaceForScope(scope scopeshim.Scope, scopeName string) (string, error) {
	switch scope {
	case scopeshim.ScopeOrganization:
		return k.Resolver.OrgNamespace(scopeName), nil
	case scopeshim.ScopeFolder:
		return k.Resolver.FolderNamespace(scopeName), nil
	case scopeshim.ScopeProject:
		return k.Resolver.ProjectNamespace(scopeName), nil
	default:
		return "", fmt.Errorf("unknown template scope %v", scope)
	}
}

// scopeLabelValue returns the label string for a TemplateScope enum value.
// Used when rebuilding a linkedRef key from a namespace+label pair that
// still classifies through the resolver.
func scopeLabelValue(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	case scopeshim.ScopeProject:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}

// ListTemplates returns every Template in the given namespace.
//
// Reads hit the controller-runtime cache — the informer keeps one watch
// against the cluster and serves every List/Get out of local memory, so
// ListTemplates does not pay a round-trip per call. Writes everywhere else
// in this file fall through to the API server; the cache learns about them
// on the next watch event.
func (k *K8sClient) ListTemplates(ctx context.Context, namespace string) ([]templatesv1alpha1.Template, error) {
	slog.DebugContext(ctx, "listing templates from kubernetes",
		slog.String("namespace", namespace),
	)
	var list templatesv1alpha1.TemplateList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing templates in namespace %q: %w", namespace, err)
	}
	return list.Items, nil
}

// ListAllTemplates returns every Template across every namespace the
// controller-runtime client can see, filtered down to the holos-console
// managed-by/resource-type=template label pair so unmanaged Template CRs
// don't leak into the cross-scope SearchTemplates response.
//
// Used by SearchTemplates (HOL-602). Reads hit the same informer cache as
// ListTemplates — the only difference is the absence of an InNamespace
// option, which makes this a single cluster-wide List rather than one List
// per scope namespace.
func (k *K8sClient) ListAllTemplates(ctx context.Context) ([]templatesv1alpha1.Template, error) {
	slog.DebugContext(ctx, "listing templates from kubernetes across all namespaces")
	var list templatesv1alpha1.TemplateList
	selector := ctrlclient.MatchingLabels{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplate,
	}
	if err := k.client.List(ctx, &list, selector); err != nil {
		return nil, fmt.Errorf("listing templates across all namespaces: %w", err)
	}
	return list.Items, nil
}

// GetNamespaceOrg returns the v1alpha2.LabelOrganization value on the given
// namespace, or "" if the namespace is missing or carries no such label.
// Used by SearchTemplates (HOL-602) to apply the organization filter to
// folder- and project-scope namespaces — the label is stamped on every
// managed namespace at create time and is updated on reparent, so reading
// it is enough to attribute the namespace to its root organization without
// a Walker round-trip per namespace.
func (k *K8sClient) GetNamespaceOrg(ctx context.Context, ns string) (string, error) {
	var got corev1.Namespace
	if err := k.client.Get(ctx, types.NamespacedName{Name: ns}, &got); err != nil {
		return "", err
	}
	if got.Labels == nil {
		return "", nil
	}
	return got.Labels[v1alpha2.LabelOrganization], nil
}

// GetTemplate retrieves a Template by name from the given namespace.
func (k *K8sClient) GetTemplate(ctx context.Context, namespace, name string) (*templatesv1alpha1.Template, error) {
	slog.DebugContext(ctx, "getting template from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	var tmpl templatesv1alpha1.Template
	if err := k.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &tmpl); err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// CreateTemplate creates a new Template CRD in the given namespace.
func (k *K8sClient) CreateTemplate(
	ctx context.Context,
	namespace, name, displayName, description, cueTemplate string,
	defaults *consolev1.TemplateDefaults,
	enabled bool,
	linkedTemplates []*consolev1.LinkedTemplateRef,
) (*templatesv1alpha1.Template, error) {
	slog.DebugContext(ctx, "creating template in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplate,
			},
		},
		Spec: templatesv1alpha1.TemplateSpec{
			DisplayName:     displayName,
			Description:     description,
			CueTemplate:     cueTemplate,
			Defaults:        protoDefaultsToCRD(defaults),
			Enabled:         enabled,
			LinkedTemplates: protoLinkedToCRD(linkedTemplates),
		},
	}
	if err := k.client.Create(ctx, tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

// UpdateTemplate mutates the addressable spec fields of an existing Template.
//
// Each optional pointer parameter applies only when non-nil — the helper
// preserves nil-for-"leave alone" semantics the handler relies on. The
// linked-template handling follows the same three-state contract the
// pre-HOL-621 ConfigMap path used:
//
//   - clearLinks=true          → spec.linkedTemplates is set to nil
//   - linkedTemplates != nil   → spec.linkedTemplates is overwritten
//     (including the "empty slice means clear" case
//     the caller is responsible for normalizing)
//   - linkedTemplates == nil   → spec.linkedTemplates is left untouched
//
// The contract is asymmetric with defaults on purpose: defaults have a
// clearDefaults counterpart because callers sometimes legitimately want
// a template to carry zero defaults, and distinguishing that from
// "preserve" matters for the CRD diff; links use clearLinks for symmetry
// and because the handler already encoded this distinction on the
// pre-rewrite ConfigMap path.
func (k *K8sClient) UpdateTemplate(
	ctx context.Context,
	namespace, name string,
	displayName, description, cueTemplate *string,
	defaults *consolev1.TemplateDefaults,
	clearDefaults bool,
	enabled *bool,
	linkedTemplates []*consolev1.LinkedTemplateRef,
	clearLinks bool,
) (*templatesv1alpha1.Template, error) {
	slog.DebugContext(ctx, "updating template in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	tmpl, err := k.GetTemplate(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("getting template for update: %w", err)
	}
	if displayName != nil {
		tmpl.Spec.DisplayName = *displayName
	}
	if description != nil {
		tmpl.Spec.Description = *description
	}
	if cueTemplate != nil {
		tmpl.Spec.CueTemplate = *cueTemplate
	}
	if enabled != nil {
		tmpl.Spec.Enabled = *enabled
	}
	if clearDefaults {
		tmpl.Spec.Defaults = nil
	} else if defaults != nil {
		tmpl.Spec.Defaults = protoDefaultsToCRD(defaults)
	}
	if clearLinks {
		tmpl.Spec.LinkedTemplates = nil
	} else if linkedTemplates != nil {
		if len(linkedTemplates) == 0 {
			tmpl.Spec.LinkedTemplates = nil
		} else {
			tmpl.Spec.LinkedTemplates = protoLinkedToCRD(linkedTemplates)
		}
	}
	if err := k.client.Update(ctx, tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

// DeleteTemplate deletes a Template by name from the given namespace.
func (k *K8sClient) DeleteTemplate(ctx context.Context, namespace, name string) error {
	slog.DebugContext(ctx, "deleting template from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	return k.client.Delete(ctx, tmpl)
}

// CloneTemplate copies an existing Template into a new name within the same
// namespace. Clones start with enabled=false so the operator can review the
// copy before it participates in a render.
func (k *K8sClient) CloneTemplate(ctx context.Context, namespace, sourceName, newName, newDisplayName string) (*templatesv1alpha1.Template, error) {
	source, err := k.GetTemplate(ctx, namespace, sourceName)
	if err != nil {
		return nil, fmt.Errorf("getting source template for clone: %w", err)
	}
	return k.CreateTemplate(
		ctx,
		namespace,
		newName,
		newDisplayName,
		source.Spec.Description,
		source.Spec.CueTemplate,
		crdDefaultsToProto(source.Spec.Defaults),
		false, // new clones start disabled
		crdLinkedToProto(source.Spec.LinkedTemplates),
	)
}

// ProjectScopedResolver exposes a (project, name) GetTemplate surface the
// deployments package consumes via the TemplateResolver interface. The
// deployments handler is still project-scoped, so wrapping the unified
// K8sClient keeps that package decoupled from the namespace-centric CRD API.
type ProjectScopedResolver struct {
	k8s *K8sClient
}

// NewProjectScopedResolver returns a ProjectScopedResolver suitable for the
// deployments package's TemplateResolver contract.
func NewProjectScopedResolver(k8s *K8sClient) *ProjectScopedResolver {
	return &ProjectScopedResolver{k8s: k8s}
}

// GetTemplate satisfies deployments.TemplateResolver. It returns the CUE
// source and linked-template metadata needed for an apply-time render as a
// ConfigMap shape so the deployments handler (which still thinks in
// ConfigMaps until its own rewrite lands) keeps compiling; the shape is
// synthetic — only Name, Namespace, Data[CueTemplateKey], and the linked-
// templates annotation are populated.
func (r *ProjectScopedResolver) GetTemplate(ctx context.Context, project, name string) (*corev1.ConfigMap, error) {
	ns := r.k8s.Resolver.ProjectNamespace(project)
	tmpl, err := r.k8s.GetTemplate(ctx, ns, name)
	if err != nil {
		return nil, err
	}
	return templateCRDToConfigMap(tmpl), nil
}

// templateCRDToConfigMap converts a Template CRD to the ConfigMap shape the
// deployments handler still expects. This is a transitional adapter: the
// only consumer is deployments.TemplateResolver, which receives the
// ConfigMap and reads only Name, Namespace, Data[CueTemplateKey], and the
// v1alpha2.AnnotationLinkedTemplates annotation. When the deployments
// package is rewritten against the CRD this helper can be deleted with
// ProjectScopedResolver.
func templateCRDToConfigMap(tmpl *templatesv1alpha1.Template) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tmpl.Name,
			Namespace:   tmpl.Namespace,
			Annotations: map[string]string{},
		},
		Data: map[string]string{
			CueTemplateKey: tmpl.Spec.CueTemplate,
		},
	}
	if len(tmpl.Spec.LinkedTemplates) > 0 {
		refs := crdLinkedToProto(tmpl.Spec.LinkedTemplates)
		if b, err := marshalLinkedTemplates(refs); err == nil {
			cm.Annotations[v1alpha2.AnnotationLinkedTemplates] = string(b)
		}
	}
	return cm
}

// ListTemplatesInNamespace returns every Template CRD in the given namespace.
//
// Kept as a second spelling of ListTemplates to match the pre-HOL-621 name
// used by the ancestor walker in ListEffectiveTemplateSources. With the CRD
// rewrite the two functions are effectively synonyms, but preserving the
// name localizes the later cleanup.
func (k *K8sClient) ListTemplatesInNamespace(ctx context.Context, ns string) ([]templatesv1alpha1.Template, error) {
	return k.ListTemplates(ctx, ns)
}

// TargetKind is re-exported from console/policyresolver so existing call
// sites in this package and its tests keep compiling after the PolicyResolver
// seam moved out (HOL-566). The underlying type and constants live in the
// policyresolver package because TargetKind is part of the PolicyResolver
// contract — handlers and helpers that own resolution decisions consume
// both together.
type TargetKind = policyresolver.TargetKind

const (
	// TargetKindProjectTemplate is the preview render path for project-scope
	// templates (the RenderTemplate RPC and the project-scope Create/Update
	// template handlers once they grow a render step).
	TargetKindProjectTemplate = policyresolver.TargetKindProjectTemplate
	// TargetKindDeployment is the apply render path for deployments
	// (AncestorTemplateProvider on the deployments handler).
	TargetKindDeployment = policyresolver.TargetKindDeployment
)

// RenderHierarchyWalker walks the namespace hierarchy for render-time ancestor
// template resolution. This mirrors HierarchyWalker from apply.go but is
// defined here to avoid coupling the render path to the apply path.
type RenderHierarchyWalker interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// ListEffectiveTemplateSources returns the ordered, deduplicated CUE sources
// that participate in rendering the given target, alongside the policy-
// effective ref set that produced them. The effective set at each ancestor
// namespace is:
//
//	enabled AND ref IN explicitRefs
//
// For linked templates that carry a version constraint, the CUE source is
// resolved from the latest matching release via ResolveVersionedSource
// (ADR 024); linked templates without releases fall back to the live CRD CUE
// source. Disabled templates are never included, even when explicitly
// linked.
//
// The walker drives ancestor traversal. If walker is nil, the method returns
// (nil, nil, nil) — see the pre-rewrite comment for the rationale. HOL-621
// retained the same contract; only the storage substrate changed.
func (k *K8sClient) ListEffectiveTemplateSources(
	ctx context.Context,
	projectNs string,
	targetKind TargetKind,
	targetName string,
	explicitRefs []*consolev1.LinkedTemplateRef,
	walker RenderHierarchyWalker,
	policyRes policyresolver.PolicyResolver,
) ([]string, []*consolev1.LinkedTemplateRef, error) {
	effectiveRefs := explicitRefs
	if policyRes != nil {
		resolved, resolveErr := policyRes.Resolve(ctx, projectNs, targetKind, targetName, explicitRefs)
		if resolveErr != nil {
			return nil, nil, fmt.Errorf("resolving template policy for %q: %w", targetName, resolveErr)
		}
		effectiveRefs = resolved
	}
	if effectiveRefs == nil {
		effectiveRefs = []*consolev1.LinkedTemplateRef{}
	}

	if walker == nil {
		return nil, nil, nil
	}

	ancestors, err := walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		slog.WarnContext(ctx, "failed to walk ancestor chain for render, returning empty sources",
			slog.String("projectNs", projectNs),
			slog.Any("error", err),
		)
		return nil, nil, nil
	}

	// Build a lookup from (scope, scopeName, name) -> linked ref so linked
	// templates with version constraints resolve their release source.
	linkedByKey := make(map[linkedRef]*consolev1.LinkedTemplateRef, len(effectiveRefs))
	for _, ref := range effectiveRefs {
		if ref == nil {
			continue
		}
		linkedByKey[linkedRefFromProto(ref)] = ref
	}

	var allSources []string
	seen := make(map[linkedRef]bool)
	for i := 1; i < len(ancestors); i++ {
		ns := ancestors[i]
		tmpls, listErr := k.ListTemplatesInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list templates in ancestor namespace, skipping",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}

		scope, scopeName := scopeAndNameFromNs(k.Resolver, ns.Name)
		if scope == scopeshim.ScopeUnspecified {
			continue
		}
		scopeLabel := scopeLabelValue(scope)

		for i := range tmpls {
			tmpl := &tmpls[i]
			if !tmpl.Spec.Enabled {
				continue
			}
			key := linkedRef{scope: scopeLabel, scopeName: scopeName, name: tmpl.Name}
			protoRef, isLinked := linkedByKey[key]

			if !isLinked {
				continue
			}
			if seen[key] {
				continue
			}
			seen[key] = true

			src, resolveErr := k.ResolveVersionedSource(ctx, ns.Name, tmpl.Name, protoRef.GetVersionConstraint())
			if resolveErr != nil {
				slog.WarnContext(ctx, "failed to resolve versioned source for ancestor template, falling back to live",
					slog.String("template", tmpl.Name),
					slog.String("namespace", ns.Name),
					slog.Any("error", resolveErr),
				)
			} else if src != "" {
				allSources = append(allSources, src)
				continue
			}

			if liveSrc := tmpl.Spec.CueTemplate; liveSrc != "" {
				allSources = append(allSources, liveSrc)
			}
		}
	}

	return allSources, effectiveRefs, nil
}

// ListLinkableTemplateInfos returns all enabled Templates in the given
// namespace as LinkableTemplate proto messages. The `Forced` field stays
// false here because this path does not run TemplatePolicy REQUIRE
// evaluation — render-time is the authoritative enforcement site.
func (k *K8sClient) ListLinkableTemplateInfos(ctx context.Context, namespace string) ([]*consolev1.LinkableTemplate, error) {
	tmpls, err := k.ListTemplates(ctx, namespace)
	if err != nil {
		return nil, err
	}
	result := make([]*consolev1.LinkableTemplate, 0, len(tmpls))
	for i := range tmpls {
		tmpl := &tmpls[i]
		if !tmpl.Spec.Enabled {
			continue
		}
		result = append(result, &consolev1.LinkableTemplate{
			Namespace:   tmpl.Namespace,
			Name:        tmpl.Name,
			DisplayName: tmpl.Spec.DisplayName,
			Description: tmpl.Spec.Description,
			Forced:      false,
		})
	}
	return result, nil
}

// SeedOrgTemplate seeds the built-in HTTPRoute platform template as enabled
// into the org namespace. Used by the populate_defaults flow during org
// creation.
func (k *K8sClient) SeedOrgTemplate(ctx context.Context, org string) error {
	ns := k.Resolver.OrgNamespace(org)
	_, err := k.CreateTemplate(
		ctx,
		ns,
		DefaultReferenceGrantName,
		"HTTPRoute",
		"Exposes a deployment's Service via an HTTPRoute through the gateway. Requires a ReferenceGrant in the project namespace (provided by the default deployment template).",
		DefaultReferenceGrantTemplate,
		nil,
		true, // enabled for populate_defaults flow
		nil,
	)
	return err
}

// SeedProjectTemplate seeds the example httpbin deployment template into the
// project namespace. Used by the populate_defaults flow during org creation.
func (k *K8sClient) SeedProjectTemplate(ctx context.Context, project string) error {
	ns := k.Resolver.ProjectNamespace(project)
	_, err := k.CreateTemplate(
		ctx,
		ns,
		"example-httpbin",
		"Example Httpbin",
		"Example go-httpbin project-level deployment template. Produces ServiceAccount, Deployment, and Service resources.",
		ExampleHttpbinTemplate,
		nil,
		true, // enabled for populate_defaults flow
		nil,
	)
	return err
}

// CreateRelease creates an immutable TemplateRelease CRD for a template at a
// specific semver version. The object name is a deterministic function of
// templateName and version (ReleaseObjectName), so a duplicate publish
// returns AlreadyExists from the apiserver; callers map that to a domain
// error.
func (k *K8sClient) CreateRelease(ctx context.Context, namespace, templateName string, version *semver.Version, cueTemplate string, defaults *consolev1.TemplateDefaults, changelog, upgradeAdvice string) (*templatesv1alpha1.TemplateRelease, error) {
	name := ReleaseObjectName(templateName, version)
	slog.DebugContext(ctx, "creating release in kubernetes",
		slog.String("namespace", namespace),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("name", name),
	)

	defaultsJSON, err := marshalProtoDefaults(defaults)
	if err != nil {
		return nil, fmt.Errorf("marshaling release defaults: %w", err)
	}

	rel := &templatesv1alpha1.TemplateRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: releaseResourceTypeLabelValue,
				releaseOfLabelKey:          templateName,
			},
		},
		Spec: templatesv1alpha1.TemplateReleaseSpec{
			TemplateName:  templateName,
			Version:       version.String(),
			CueTemplate:   cueTemplate,
			DefaultsJSON:  defaultsJSON,
			Changelog:     changelog,
			UpgradeAdvice: upgradeAdvice,
		},
	}
	if err := k.client.Create(ctx, rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// ListReleases returns all TemplateRelease CRDs for a template, sorted by
// version descending (newest first). Releases whose spec.version fails to
// parse as semver sort to the end.
//
// Filtering is done on `spec.templateName` rather than the
// `console.holos.run/release-of` label so a TemplateRelease that omits the
// convenience labels (say, one authored directly via `kubectl apply` or a
// migration tool) is still visible to the resolver and duplicate-publish
// detection. Labels remain on CreateRelease-produced objects as cache-side
// indexing hints for tooling like kubectl; they are not authoritative for
// visibility.
func (k *K8sClient) ListReleases(ctx context.Context, namespace, templateName string) ([]templatesv1alpha1.TemplateRelease, error) {
	slog.DebugContext(ctx, "listing releases from kubernetes",
		slog.String("namespace", namespace),
		slog.String("templateName", templateName),
	)
	var list templatesv1alpha1.TemplateReleaseList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}
	items := list.Items[:0]
	for _, rel := range list.Items {
		if rel.Spec.TemplateName == templateName {
			items = append(items, rel)
		}
	}
	sortReleasesDesc(items)
	return items, nil
}

// GetRelease retrieves a specific TemplateRelease by template name and
// version from the given namespace.
func (k *K8sClient) GetRelease(ctx context.Context, namespace, templateName string, version *semver.Version) (*templatesv1alpha1.TemplateRelease, error) {
	name := ReleaseObjectName(templateName, version)
	slog.DebugContext(ctx, "getting release from kubernetes",
		slog.String("namespace", namespace),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("name", name),
	)
	var rel templatesv1alpha1.TemplateRelease
	if err := k.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// sortReleasesDesc sorts TemplateRelease CRDs by spec.version in descending
// order (newest first). Items with invalid or missing version strings sort
// to the end.
func sortReleasesDesc(items []templatesv1alpha1.TemplateRelease) {
	type versioned struct {
		ver *semver.Version
	}
	entries := make([]versioned, len(items))
	for i, r := range items {
		v, _ := ParseVersion(r.Spec.Version)
		entries[i] = versioned{ver: v}
	}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			vi, vj := entries[i].ver, entries[j].ver
			swap := false
			if vi == nil && vj != nil {
				swap = true
			} else if vi != nil && vj != nil && vj.GreaterThan(vi) {
				swap = true
			}
			if swap {
				entries[i], entries[j] = entries[j], entries[i]
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// releaseCRDToProto converts a TemplateRelease CRD to the Release protobuf
// message. HOL-619 moved Release to namespace-keyed identity — the returned
// proto carries rel.Namespace directly.
func releaseCRDToProto(rel *templatesv1alpha1.TemplateRelease) *consolev1.Release {
	out := &consolev1.Release{
		TemplateName:  rel.Spec.TemplateName,
		Namespace:     rel.Namespace,
		Version:       rel.Spec.Version,
		Changelog:     rel.Spec.Changelog,
		UpgradeAdvice: rel.Spec.UpgradeAdvice,
		CueTemplate:   rel.Spec.CueTemplate,
		CreatedAt:     timestamppb.New(rel.CreationTimestamp.Time),
	}
	if defaults := unmarshalProtoDefaults(rel.Spec.DefaultsJSON); defaults != nil {
		out.Defaults = defaults
	}
	return out
}

// marshalProtoDefaults serializes a proto TemplateDefaults for opaque
// storage in TemplateRelease.Spec.DefaultsJSON. Returns the empty string
// for nil or fully-empty input so releases without defaults land a
// missing field rather than an empty JSON object. Using proto JSON
// preserves fidelity for env-var variants (secret_key_ref,
// config_map_key_ref) that the structured CRD TemplateDefaults does not
// model.
func marshalProtoDefaults(defaults *consolev1.TemplateDefaults) (string, error) {
	if defaults == nil {
		return "", nil
	}
	b, err := protojson.Marshal(defaults)
	if err != nil {
		return "", err
	}
	// Treat the proto zero-value as "no defaults" to keep the CRD spec
	// clean for releases that carry nothing to record.
	if string(b) == "{}" {
		return "", nil
	}
	return string(b), nil
}

// unmarshalProtoDefaults deserializes a TemplateRelease.Spec.DefaultsJSON
// blob back into a proto TemplateDefaults. Returns nil for an empty
// string or an unmarshalable payload; callers rely on a nil return as
// "no defaults recorded on this release".
func unmarshalProtoDefaults(raw string) *consolev1.TemplateDefaults {
	if raw == "" {
		return nil
	}
	var out consolev1.TemplateDefaults
	if err := protojson.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return &out
}

// ListReleaseVersions returns all parsed semver versions for a template's
// releases. Releases with invalid spec.version strings are skipped.
func (k *K8sClient) ListReleaseVersions(ctx context.Context, namespace, templateName string) ([]*semver.Version, error) {
	rels, err := k.ListReleases(ctx, namespace, templateName)
	if err != nil {
		return nil, err
	}
	var versions []*semver.Version
	for _, r := range rels {
		v, err := ParseVersion(r.Spec.Version)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// ResolveVersionedSource resolves the CUE source for a linked template. If
// releases exist and a version constraint is provided, it returns the CUE
// source from the latest matching release. If no releases exist (pre-
// versioning backwards compatibility), it falls back to the live template's
// CUE source read from the Template CRD.
func (k *K8sClient) ResolveVersionedSource(ctx context.Context, namespace, templateName, versionConstraint string) (string, error) {
	versions, err := k.ListReleaseVersions(ctx, namespace, templateName)
	if err != nil {
		return "", fmt.Errorf("listing release versions for %s: %w", templateName, err)
	}

	if len(versions) == 0 {
		tmpl, err := k.GetTemplate(ctx, namespace, templateName)
		if err != nil {
			return "", fmt.Errorf("getting live template %s: %w", templateName, err)
		}
		return tmpl.Spec.CueTemplate, nil
	}

	constraint, err := ParseConstraint(versionConstraint)
	if err != nil {
		return "", err
	}

	best := LatestMatchingVersion(versions, constraint)
	if best == nil {
		return "", fmt.Errorf("no release of %q matches constraint %q", templateName, versionConstraint)
	}

	rel, err := k.GetRelease(ctx, namespace, templateName, best)
	if err != nil {
		return "", fmt.Errorf("getting release %s@%s: %w", templateName, best.String(), err)
	}
	return rel.Spec.CueTemplate, nil
}

// releaseResourceTypeLabelValue and releaseOfLabelKey are the label values
// identifying TemplateRelease CRDs. They replace the retired
// v1alpha2.ResourceTypeTemplateRelease and v1alpha2.LabelReleaseOf constants
// (HOL-693 / ADR 032).
const (
	releaseResourceTypeLabelValue = "template-release"
	releaseOfLabelKey             = "console.holos.run/release-of"
)

// templateCRDToProto converts a Template CRD to the consolev1.Template wire
// shape. The CUE source drives the deployment-defaults extraction path for
// project-scope templates (ADR 018/ADR 025); non-project scopes return the
// CRD's structured defaults directly so the legacy ConfigMap-JSON fallback
// is no longer in play.
func templateCRDToProto(tmpl *templatesv1alpha1.Template, scope scopeshim.Scope) *consolev1.Template {
	out := &consolev1.Template{
		Name:            tmpl.Name,
		Namespace:       tmpl.Namespace,
		DisplayName:     tmpl.Spec.DisplayName,
		Description:     tmpl.Spec.Description,
		CueTemplate:     tmpl.Spec.CueTemplate,
		Enabled:         tmpl.Spec.Enabled,
		Version:         tmpl.Spec.Version,
		LinkedTemplates: crdLinkedToProto(tmpl.Spec.LinkedTemplates),
	}

	// Priority 1: CUE extraction for project-scope templates (ADR 018 design,
	// ADR 025 per-field extraction).
	if scope == scopeshim.ScopeProject && tmpl.Spec.CueTemplate != "" {
		extracted, err := ExtractDefaults(tmpl.Spec.CueTemplate)
		if err != nil {
			slog.Warn("failed to extract defaults from CUE template; falling back to spec defaults",
				slog.String("name", tmpl.Name),
				slog.String("namespace", tmpl.Namespace),
				slog.Any("error", err),
			)
		} else if extracted != nil {
			out.Defaults = extracted
			return out
		}
	}

	// Priority 2: Structured defaults from the CRD spec.
	if tmpl.Spec.Defaults != nil {
		out.Defaults = crdDefaultsToProto(tmpl.Spec.Defaults)
	}
	return out
}

// linkedRef is a deduplicated key for a cross-level template reference.
type linkedRef struct {
	scope     string // e.g. "organization", "folder", "project"
	scopeName string
	name      string
}

// linkedRefFromProto converts a proto LinkedTemplateRef to a linkedRef key.
// The proto carries a namespace today (HOL-619); classify it through the
// shim so the internal storage key stays stable until the shim itself is
// retired alongside the transitional fields.
func linkedRefFromProto(ref *consolev1.LinkedTemplateRef) linkedRef {
	return linkedRef{
		scope:     scopeLabelValue(scopeshim.RefScope(ref)),
		scopeName: scopeshim.RefScopeName(ref),
		name:      ref.Name,
	}
}

// marshalLinkedTemplates serializes LinkedTemplateRef slice to JSON. Used
// only by the ProjectScopedResolver compatibility adapter that synthesises
// a ConfigMap for the deployments package; once that package is rewritten
// against the CRD this helper disappears with it.
func marshalLinkedTemplates(refs []*consolev1.LinkedTemplateRef) ([]byte, error) {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	stored := make([]storedRef, 0, len(refs))
	for _, r := range refs {
		if r == nil {
			continue
		}
		stored = append(stored, storedRef{
			Scope:             scopeLabelValue(scopeshim.RefScope(r)),
			ScopeName:         scopeshim.RefScopeName(r),
			Name:              r.Name,
			VersionConstraint: r.VersionConstraint,
		})
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("serializing linked templates: %w", err)
	}
	return b, nil
}

// protoLinkedToCRD translates the wire LinkedTemplateRef slice into the
// CRD's structured spec representation. Nil or empty input yields a nil
// slice so the apiserver sees spec.linkedTemplates as unset rather than an
// empty array (callers that want to clear use clearLinks=true).
func protoLinkedToCRD(refs []*consolev1.LinkedTemplateRef) []templatesv1alpha1.LinkedTemplateRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.LinkedTemplateRef, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		out = append(out, templatesv1alpha1.LinkedTemplateRef{
			Scope:             scopeLabelValue(scopeshim.RefScope(ref)),
			ScopeName:         scopeshim.RefScopeName(ref),
			Name:              ref.Name,
			VersionConstraint: ref.VersionConstraint,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// crdLinkedToProto translates the CRD's structured linked-template slice into
// the wire shape. The proto LinkedTemplateRef carries a namespace (HOL-619);
// scopeshim.NewLinkedTemplateRef builds one from the legacy
// (scope, scopeName) pair and returns a ref whose namespace matches what
// the handler computes elsewhere.
func crdLinkedToProto(refs []templatesv1alpha1.LinkedTemplateRef) []*consolev1.LinkedTemplateRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*consolev1.LinkedTemplateRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, scopeshim.NewLinkedTemplateRef(
			scopeFromLabel(ref.Scope),
			ref.ScopeName,
			ref.Name,
			ref.VersionConstraint,
		))
	}
	return out
}

// protoDefaultsToCRD mirrors the proto TemplateDefaults into the CRD
// representation. Returning nil for a nil input keeps spec.defaults unset
// rather than an empty object.
func protoDefaultsToCRD(in *consolev1.TemplateDefaults) *templatesv1alpha1.TemplateDefaults {
	if in == nil {
		return nil
	}
	out := &templatesv1alpha1.TemplateDefaults{
		Image:       in.GetImage(),
		Tag:         in.GetTag(),
		Command:     append([]string(nil), in.GetCommand()...),
		Args:        append([]string(nil), in.GetArgs()...),
		Port:        in.GetPort(),
		Name:        in.GetName(),
		Description: in.GetDescription(),
	}
	if len(in.GetEnv()) > 0 {
		out.Env = make([]templatesv1alpha1.EnvVar, 0, len(in.GetEnv()))
		for _, e := range in.GetEnv() {
			if e == nil {
				continue
			}
			out.Env = append(out.Env, templatesv1alpha1.EnvVar{
				Name:  e.GetName(),
				Value: e.GetValue(),
			})
		}
	}
	return out
}

// crdDefaultsToProto is the inverse of protoDefaultsToCRD.
func crdDefaultsToProto(in *templatesv1alpha1.TemplateDefaults) *consolev1.TemplateDefaults {
	if in == nil {
		return nil
	}
	out := &consolev1.TemplateDefaults{
		Image:       in.Image,
		Tag:         in.Tag,
		Command:     append([]string(nil), in.Command...),
		Args:        append([]string(nil), in.Args...),
		Port:        in.Port,
		Name:        in.Name,
		Description: in.Description,
	}
	if len(in.Env) > 0 {
		out.Env = make([]*consolev1.EnvVar, 0, len(in.Env))
		for _, e := range in.Env {
			out.Env = append(out.Env, &consolev1.EnvVar{
				Name:   e.Name,
				Source: &consolev1.EnvVar_Value{Value: e.Value},
			})
		}
	}
	return out
}

// scopeFromLabel converts a label string back to a TemplateScope enum value.
func scopeFromLabel(label string) scopeshim.Scope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return scopeshim.ScopeOrganization
	case v1alpha2.TemplateScopeFolder:
		return scopeshim.ScopeFolder
	case v1alpha2.TemplateScopeProject:
		return scopeshim.ScopeProject
	default:
		return scopeshim.ScopeUnspecified
	}
}

// scopeAndNameFromNs infers the scope and logical name from a Kubernetes
// namespace.
func scopeAndNameFromNs(r *resolver.Resolver, ns string) (scopeshim.Scope, string) {
	if name, err := r.OrgFromNamespace(ns); err == nil {
		return scopeshim.ScopeOrganization, name
	}
	if name, err := r.FolderFromNamespace(ns); err == nil {
		return scopeshim.ScopeFolder, name
	}
	if name, err := r.ProjectFromNamespace(ns); err == nil {
		return scopeshim.ScopeProject, name
	}
	return scopeshim.ScopeUnspecified, ""
}

// AncestorTemplateResolver adapts K8sClient + a walker + a PolicyResolver
// into a single-method interface suitable for the deployments package (which
// cannot import templates directly due to the avoid-import-cycle
// convention). The walker and resolver are closed over at construction time
// so the caller only needs to pass project namespace, deployment name, and
// linked refs.
type AncestorTemplateResolver struct {
	k8s      *K8sClient
	walker   RenderHierarchyWalker
	resolver policyresolver.PolicyResolver
}

// NewAncestorTemplateResolver creates an AncestorTemplateResolver.
func NewAncestorTemplateResolver(k8s *K8sClient, walker RenderHierarchyWalker, resolver policyresolver.PolicyResolver) *AncestorTemplateResolver {
	return &AncestorTemplateResolver{k8s: k8s, walker: walker, resolver: resolver}
}

// ListAncestorTemplateSources satisfies deployments.AncestorTemplateProvider.
func (a *AncestorTemplateResolver) ListAncestorTemplateSources(ctx context.Context, projectNs, targetName string, linkedRefs []*consolev1.LinkedTemplateRef) ([]string, []*consolev1.LinkedTemplateRef, error) {
	return a.k8s.ListEffectiveTemplateSources(ctx, projectNs, TargetKindDeployment, targetName, linkedRefs, a.walker, a.resolver)
}

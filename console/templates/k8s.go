package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/Masterminds/semver/v3"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	CueTemplateKey = "template.cue"
	// DefaultsKey is the ConfigMap data key that stores TemplateDefaults as JSON.
	DefaultsKey = "defaults.json"

	// DefaultReferenceGrantName is the name of the seeded built-in platform template.
	DefaultReferenceGrantName = "reference-grant"
)

// K8sClient wraps Kubernetes client operations for unified template CRUD at
// any scope level (organization, folder, project). This replaces the separate
// templates.K8sClient and org_templates.K8sClient from v1alpha1 (ADR 021 Decision 1).
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for template operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// namespaceForScope returns the Kubernetes namespace for the given scope and name.
func (k *K8sClient) namespaceForScope(scope consolev1.TemplateScope, scopeName string) (string, error) {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return k.Resolver.OrgNamespace(scopeName), nil
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return k.Resolver.FolderNamespace(scopeName), nil
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
		return k.Resolver.ProjectNamespace(scopeName), nil
	default:
		return "", fmt.Errorf("unknown template scope %v", scope)
	}
}

// scopeLabelValue returns the label string for a TemplateScope enum value.
func scopeLabelValue(scope consolev1.TemplateScope) string {
	switch scope {
	case consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION:
		return v1alpha2.TemplateScopeOrganization
	case consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER:
		return v1alpha2.TemplateScopeFolder
	case consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}

// ListTemplates returns all template ConfigMaps for the given scope and name.
func (k *K8sClient) ListTemplates(ctx context.Context, scope consolev1.TemplateScope, scopeName string) ([]corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplate
	slog.DebugContext(ctx, "listing templates from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing templates: %w", err)
	}
	return list.Items, nil
}

// GetTemplate retrieves a template ConfigMap by name for the given scope.
func (k *K8sClient) GetTemplate(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "getting template from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateTemplate creates a new template ConfigMap at the given scope.
func (k *K8sClient) CreateTemplate(ctx context.Context, scope consolev1.TemplateScope, scopeName, name, displayName, description, cueTemplate string, defaults *consolev1.TemplateDefaults, enabled bool, linkedTemplates []*consolev1.LinkedTemplateRef) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "creating template in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	data := map[string]string{
		CueTemplateKey: cueTemplate,
	}
	if defaults != nil {
		b, err := json.Marshal(defaults)
		if err != nil {
			return nil, fmt.Errorf("serializing template defaults: %w", err)
		}
		data[DefaultsKey] = string(b)
	}
	annotations := map[string]string{
		v1alpha2.AnnotationDisplayName: displayName,
		v1alpha2.AnnotationDescription: description,
		v1alpha2.AnnotationEnabled:     strconv.FormatBool(enabled),
	}
	if len(linkedTemplates) > 0 {
		b, err := marshalLinkedTemplates(linkedTemplates)
		if err != nil {
			return nil, err
		}
		annotations[v1alpha2.AnnotationLinkedTemplates] = string(b)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplate,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: annotations,
		},
		Data: data,
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateTemplate updates an existing template ConfigMap.
// Only non-nil pointer fields are updated.
func (k *K8sClient) UpdateTemplate(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string, displayName, description, cueTemplate *string, defaults *consolev1.TemplateDefaults, clearDefaults bool, enabled *bool, linkedTemplates []*consolev1.LinkedTemplateRef, clearLinks bool) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "updating template in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting template for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if displayName != nil {
		cm.Annotations[v1alpha2.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha2.AnnotationDescription] = *description
	}
	if enabled != nil {
		cm.Annotations[v1alpha2.AnnotationEnabled] = strconv.FormatBool(*enabled)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	if cueTemplate != nil {
		cm.Data[CueTemplateKey] = *cueTemplate
	}
	if clearDefaults {
		delete(cm.Data, DefaultsKey)
	} else if defaults != nil {
		b, err := json.Marshal(defaults)
		if err != nil {
			return nil, fmt.Errorf("serializing template defaults: %w", err)
		}
		cm.Data[DefaultsKey] = string(b)
	}
	if clearLinks {
		delete(cm.Annotations, v1alpha2.AnnotationLinkedTemplates)
	} else if linkedTemplates != nil {
		if len(linkedTemplates) == 0 {
			delete(cm.Annotations, v1alpha2.AnnotationLinkedTemplates)
		} else {
			b, err := marshalLinkedTemplates(linkedTemplates)
			if err != nil {
				return nil, err
			}
			cm.Annotations[v1alpha2.AnnotationLinkedTemplates] = string(b)
		}
	}
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// DeleteTemplate deletes a template ConfigMap.
func (k *K8sClient) DeleteTemplate(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) error {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "deleting template from kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// CloneTemplate copies an existing template to a new name within the same scope.
func (k *K8sClient) CloneTemplate(ctx context.Context, scope consolev1.TemplateScope, scopeName, sourceName, newName, newDisplayName string) (*corev1.ConfigMap, error) {
	source, err := k.GetTemplate(ctx, scope, scopeName, sourceName)
	if err != nil {
		return nil, fmt.Errorf("getting source template for clone: %w", err)
	}
	// Extract defaults from source if present.
	var defaults *consolev1.TemplateDefaults
	if rawJSON, ok := source.Data[DefaultsKey]; ok && rawJSON != "" {
		var d consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(rawJSON), &d); err == nil {
			defaults = &d
		}
	}
	// Inherit linked templates from source.
	var linkedTemplates []*consolev1.LinkedTemplateRef
	if raw, ok := source.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok && raw != "" {
		linkedTemplates, _ = unmarshalLinkedTemplates(raw)
	}
	// New clones start as disabled.
	return k.CreateTemplate(
		ctx,
		scope,
		scopeName,
		newName,
		newDisplayName,
		source.Annotations[v1alpha2.AnnotationDescription],
		source.Data[CueTemplateKey],
		defaults,
		false, // new clones start disabled
		linkedTemplates,
	)
}

// ProjectScopedResolver wraps K8sClient and exposes the 2-argument GetTemplate
// and DeleteTemplate methods expected by the deployments package. This avoids
// coupling the deployments package to the unified scope-discriminated API while
// the deployment service is still project-scoped.
type ProjectScopedResolver struct {
	k8s *K8sClient
}

// NewProjectScopedResolver creates a ProjectScopedResolver from a K8sClient.
func NewProjectScopedResolver(k8s *K8sClient) *ProjectScopedResolver {
	return &ProjectScopedResolver{k8s: k8s}
}

// GetTemplate satisfies deployments.TemplateResolver using project scope.
func (r *ProjectScopedResolver) GetTemplate(ctx context.Context, project, name string) (*corev1.ConfigMap, error) {
	return r.k8s.GetTemplate(ctx, consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, project, name)
}

// ListTemplatesInNamespace returns all template ConfigMaps in a specific namespace.
// Used by the ancestry walker to collect templates from ancestor namespaces.
func (k *K8sClient) ListTemplatesInNamespace(ctx context.Context, ns string) ([]corev1.ConfigMap, error) {
	labelSelector := v1alpha2.LabelResourceType + "=" + v1alpha2.ResourceTypeTemplate
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing templates in namespace %q: %w", ns, err)
	}
	return list.Items, nil
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
// (ADR 024); linked templates without releases fall back to the live
// ConfigMap CUE source. Disabled templates are never included, even when
// explicitly linked.
//
// The "mandatory" annotation that previously contributed a
// (mandatory AND enabled) branch was removed in HOL-565 as part of the
// HOL-562 collapse. Templates that must be unconditionally applied now come
// in via TemplatePolicy REQUIRE rules — resolved by the caller and injected
// as explicit refs — rather than via an annotation baked into the template
// ConfigMap. Phase 5 (HOL-567) wires the real policy resolver; until then
// this function only surfaces the caller's explicit link set.
//
// Dedup key: (scope, scopeName, name). This uniform key replaces the
// three inconsistent dedup strategies of the legacy helpers this function
// supersedes, so a folder template named "foo" and an org template named
// "foo" correctly survive as two distinct sources.
//
// The walker drives ancestor traversal. If walker is nil, the method returns
// (nil, nil, nil): no sources AND no effective refs, because the caller
// will render project-only and must not persist an applied-render-set that
// claims ancestor templates were unified in. If the walker returns an
// error, the method degrades gracefully the same way — nil refs plus a
// warn log — so HOL-569 write-through never persists refs that did not
// actually participate in the render.
//
// resolver evaluates TemplatePolicy REQUIRE/EXCLUDE rules against the
// caller's explicitRefs before the ancestor walk. Phase 4 (HOL-566) threads
// the PolicyResolver seam through every call site with a no-op
// implementation that returns explicitRefs unchanged; Phase 5 (HOL-567)
// swaps in a real policy-backed implementation. When resolver is nil the
// call site predates the seam — the function falls back to using
// explicitRefs directly so tests that have not been updated keep working.
//
// The effectiveRefs return value is the single authoritative representation
// of "what the policy chain decided would participate in this render AND
// the ancestor walk succeeded." Every write-through to the applied-render-
// set store (HOL-569) consumes this value directly so the stored set
// always matches the rendered set without a second resolver invocation
// that could race a policy edit. Callers that need just the sources can
// ignore this value; callers that need the ref set (Create/Update write-
// through) MUST read it from here rather than re-invoking the resolver,
// and MUST treat a nil effectiveRefs as "do not record" because a nil
// signals a degraded render path where the stored set would not match
// what actually rendered.
//
// Storage-isolation note (HOL-554): the walk deliberately skips the starting
// namespace (ancestors[0]) and only reads templates, releases, and — once
// HOL-557 lands — applied-render-set state from folder and organization
// namespaces. Project owners have write access to their project namespace
// and must never be able to mutate render-set truth from there.
func (k *K8sClient) ListEffectiveTemplateSources(
	ctx context.Context,
	projectNs string,
	targetKind TargetKind,
	targetName string,
	explicitRefs []*consolev1.LinkedTemplateRef,
	walker RenderHierarchyWalker,
	resolver policyresolver.PolicyResolver,
) ([]string, []*consolev1.LinkedTemplateRef, error) {
	// Resolve the caller's explicit refs through the PolicyResolver seam
	// before walking ancestors. Phase 4 (HOL-566) wires a no-op resolver at
	// every call site; Phase 5 swaps in real REQUIRE/EXCLUDE evaluation.
	// A nil resolver means the call site predates the seam — fall back to
	// explicitRefs unchanged so tests that were written before HOL-566 keep
	// exercising the ancestor walk without modification.
	effectiveRefs := explicitRefs
	if resolver != nil {
		resolved, resolveErr := resolver.Resolve(ctx, projectNs, targetKind, targetName, explicitRefs)
		if resolveErr != nil {
			return nil, nil, fmt.Errorf("resolving template policy for %q: %w", targetName, resolveErr)
		}
		effectiveRefs = resolved
	}
	// Normalize a nil effectiveRefs to a non-nil empty slice so the happy
	// path (successful render with zero policy-effective refs) is
	// distinguishable from the degraded-render path below (nil effectiveRefs
	// signals "do not record"). A deployment template with no linked-
	// templates annotation is a perfectly valid apply and must still
	// persist an applied baseline so GetDeploymentPolicyState returns
	// has_applied_state=true (review round 2 P1a finding).
	if effectiveRefs == nil {
		effectiveRefs = []*consolev1.LinkedTemplateRef{}
	}

	if walker == nil {
		// No walker means no ancestor sources are unified in. Returning
		// nil effectiveRefs prevents the HOL-569 write-through from
		// recording a set that does not match the degraded render.
		return nil, nil, nil
	}

	ancestors, err := walker.WalkAncestors(ctx, projectNs)
	if err != nil {
		slog.WarnContext(ctx, "failed to walk ancestor chain for render, returning empty sources",
			slog.String("projectNs", projectNs),
			slog.Any("error", err),
		)
		// Walker failure degrades to project-only render. Discard
		// effectiveRefs for the same reason as the nil-walker branch:
		// callers MUST NOT persist an applied set that claims ancestor
		// templates were unified when they were not. Callers that want a
		// policy-resolved set independent of ancestor sourcing can invoke
		// the resolver directly.
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

	// Walk ancestors: skip the starting namespace itself (ancestors[0]) since
	// templates live in folder/org namespaces only (HOL-554 storage isolation).
	var allSources []string
	seen := make(map[linkedRef]bool)
	for i := 1; i < len(ancestors); i++ {
		ns := ancestors[i]
		cms, listErr := k.ListTemplatesInNamespace(ctx, ns.Name)
		if listErr != nil {
			slog.WarnContext(ctx, "failed to list templates in ancestor namespace, skipping",
				slog.String("namespace", ns.Name),
				slog.Any("error", listErr),
			)
			continue
		}

		for _, cm := range cms {
			enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
			if !enabled {
				continue
			}

			// Determine the scope for this template from its label.
			scopeLabel := cm.Labels[v1alpha2.LabelTemplateScope]
			scopeName := scopeNameFromNs(k.Resolver, ns.Name, scopeLabel)
			key := linkedRef{scope: scopeLabel, scopeName: scopeName, name: cm.Name}
			protoRef, isLinked := linkedByKey[key]

			if !isLinked {
				continue
			}
			if seen[key] {
				continue
			}
			seen[key] = true

			// For linked templates, resolve versioned source (ADR 024).
			scope := scopeFromLabel(scopeLabel)
			src, resolveErr := k.ResolveVersionedSource(ctx, scope, scopeName, cm.Name, protoRef.GetVersionConstraint())
			if resolveErr != nil {
				slog.WarnContext(ctx, "failed to resolve versioned source for ancestor template, falling back to live",
					slog.String("template", cm.Name),
					slog.String("namespace", ns.Name),
					slog.Any("error", resolveErr),
				)
				// Fall through to live ConfigMap below.
			} else if src != "" {
				allSources = append(allSources, src)
				continue
			}

			// Fallback: use the live ConfigMap CUE source.
			if liveSrc := cm.Data[CueTemplateKey]; liveSrc != "" {
				allSources = append(allSources, liveSrc)
			}
		}
	}

	// Design choice for HOL-569: the returned effectiveRefs are the
	// resolver's verbatim output, NOT the subset of refs whose ancestor
	// ConfigMaps were actually found and unified into allSources. A
	// template can be policy-effective yet produce zero sources (disabled
	// in its ancestor namespace, missing ConfigMap, or unresolvable
	// version constraint). Storing the resolver output keeps the applied-
	// and current-side reads symmetric — GetDeploymentPolicyState invokes
	// the same resolver to compute the current set, so comparing the two
	// produces no spurious drift even when some refs never materialize.
	// Filtering to "refs that produced sources" would introduce asymmetry
	// and reopen the race that option 1 of the ticket was chosen to
	// avoid: a concurrent policy edit between apply-time resolution and
	// query-time resolution could make the two disagree. Review round 2
	// P1b raised this concern; the decision to stick with the resolver
	// output is intentional.
	return allSources, effectiveRefs, nil
}

// ListLinkableTemplateInfos returns all enabled templates at the given scope
// as LinkableTemplate proto messages. Used by the TemplateService to populate
// the linking UI.
func (k *K8sClient) ListLinkableTemplateInfos(ctx context.Context, scope consolev1.TemplateScope, scopeName string) ([]*consolev1.LinkableTemplate, error) {
	cms, err := k.ListTemplates(ctx, scope, scopeName)
	if err != nil {
		return nil, err
	}
	var result []*consolev1.LinkableTemplate
	for _, cm := range cms {
		enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
		if !enabled {
			continue
		}
		// `Forced` is the transitional field that the linking UI reads to show
		// a disabled "always applied" checkbox. Since HOL-565 removed the
		// `mandatory` annotation reader, the field is always false until
		// HOL-567 populates it from TemplatePolicy REQUIRE-rule evaluation.
		result = append(result, &consolev1.LinkableTemplate{
			ScopeRef: &consolev1.TemplateScopeRef{
				Scope:     scope,
				ScopeName: scopeName,
			},
			Name:        cm.Name,
			DisplayName: cm.Annotations[v1alpha2.AnnotationDisplayName],
			Description: cm.Annotations[v1alpha2.AnnotationDescription],
			Forced:      false,
		})
	}
	return result, nil
}

// SeedOrgTemplate seeds the built-in HTTPRoute platform template as enabled into
// the org namespace. Used by the populate_defaults flow during org creation.
func (k *K8sClient) SeedOrgTemplate(ctx context.Context, org string) error {
	_, err := k.CreateTemplate(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		org,
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
	_, err := k.CreateTemplate(
		ctx,
		consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT,
		project,
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

// CreateRelease creates an immutable Release ConfigMap for a template at a
// specific semver version. The ConfigMap name follows the pattern
// {template-name}--v{major}-{minor}-{patch}. Returns AlreadyExists if the
// version has already been published.
func (k *K8sClient) CreateRelease(ctx context.Context, scope consolev1.TemplateScope, scopeName, templateName string, version *semver.Version, cueTemplate string, defaults *consolev1.TemplateDefaults, changelog, upgradeAdvice string) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	cmName := ReleaseConfigMapName(templateName, version)
	slog.DebugContext(ctx, "creating release in kubernetes",
		slog.String("scope", scope.String()),
		slog.String("scopeName", scopeName),
		slog.String("namespace", ns),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("configMapName", cmName),
	)

	data := map[string]string{
		CueTemplateKey:            cueTemplate,
		v1alpha2.ChangelogKey:     changelog,
		v1alpha2.UpgradeAdviceKey: upgradeAdvice,
	}
	if defaults != nil {
		b, err := json.Marshal(defaults)
		if err != nil {
			return nil, fmt.Errorf("serializing release defaults: %w", err)
		}
		data[DefaultsKey] = string(b)
	}

	immutable := true
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:     v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType:  v1alpha2.ResourceTypeTemplateRelease,
				v1alpha2.LabelReleaseOf:     templateName,
				v1alpha2.LabelTemplateScope: scopeLabelValue(scope),
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationTemplateVersion: version.String(),
			},
		},
		Immutable: &immutable,
		Data:      data,
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// ListReleases returns all release ConfigMaps for a template, sorted by version
// descending (newest first).
func (k *K8sClient) ListReleases(ctx context.Context, scope consolev1.TemplateScope, scopeName, templateName string) ([]corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		v1alpha2.LabelResourceType, v1alpha2.ResourceTypeTemplateRelease,
		v1alpha2.LabelReleaseOf, templateName,
	)
	slog.DebugContext(ctx, "listing releases from kubernetes",
		slog.String("namespace", ns),
		slog.String("templateName", templateName),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}

	// Sort by version descending.
	items := list.Items
	sortReleaseConfigMapsDesc(items)
	return items, nil
}

// GetRelease retrieves a specific release ConfigMap by template name and version.
func (k *K8sClient) GetRelease(ctx context.Context, scope consolev1.TemplateScope, scopeName, templateName string, version *semver.Version) (*corev1.ConfigMap, error) {
	ns, err := k.namespaceForScope(scope, scopeName)
	if err != nil {
		return nil, err
	}
	cmName := ReleaseConfigMapName(templateName, version)
	slog.DebugContext(ctx, "getting release from kubernetes",
		slog.String("namespace", ns),
		slog.String("templateName", templateName),
		slog.String("version", version.String()),
		slog.String("configMapName", cmName),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, cmName, metav1.GetOptions{})
}

// sortReleaseConfigMapsDesc sorts release ConfigMaps by their version annotation
// in descending order (newest first). ConfigMaps with invalid or missing version
// annotations sort to the end.
func sortReleaseConfigMapsDesc(items []corev1.ConfigMap) {
	type versioned struct {
		idx int
		ver *semver.Version
	}
	entries := make([]versioned, len(items))
	for i, cm := range items {
		raw := cm.Annotations[v1alpha2.AnnotationTemplateVersion]
		v, _ := ParseVersion(raw)
		entries[i] = versioned{idx: i, ver: v}
	}
	// Sort: valid versions descending, then invalid/missing at end.
	sorted := make([]corev1.ConfigMap, len(items))
	copy(sorted, items)
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
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	copy(items, sorted)
}

// configMapToRelease converts a Kubernetes ConfigMap to a Release protobuf message.
func configMapToRelease(cm *corev1.ConfigMap, scope consolev1.TemplateScope, scopeName string) *consolev1.Release {
	release := &consolev1.Release{
		TemplateName: cm.Labels[v1alpha2.LabelReleaseOf],
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     scope,
			ScopeName: scopeName,
		},
		Version:       cm.Annotations[v1alpha2.AnnotationTemplateVersion],
		Changelog:     cm.Data[v1alpha2.ChangelogKey],
		UpgradeAdvice: cm.Data[v1alpha2.UpgradeAdviceKey],
		CueTemplate:   cm.Data[CueTemplateKey],
		CreatedAt:     timestamppb.New(cm.CreationTimestamp.Time),
	}

	// Parse defaults from JSON if present.
	if rawJSON, ok := cm.Data[DefaultsKey]; ok && rawJSON != "" {
		var defaults consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(rawJSON), &defaults); err == nil {
			release.Defaults = &defaults
		}
	}

	return release
}

// ListReleaseVersions returns all parsed semver versions for a template's
// releases. Releases with invalid version annotations are skipped.
func (k *K8sClient) ListReleaseVersions(ctx context.Context, scope consolev1.TemplateScope, scopeName, templateName string) ([]*semver.Version, error) {
	cms, err := k.ListReleases(ctx, scope, scopeName, templateName)
	if err != nil {
		return nil, err
	}
	var versions []*semver.Version
	for _, cm := range cms {
		raw := cm.Annotations[v1alpha2.AnnotationTemplateVersion]
		v, err := ParseVersion(raw)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// ResolveVersionedSource resolves the CUE source for a linked template. If
// releases exist and a version constraint is provided, it returns the CUE
// source from the latest matching release. If no releases exist (pre-versioning
// backwards compatibility), it falls back to the live template ConfigMap's CUE
// source.
func (k *K8sClient) ResolveVersionedSource(ctx context.Context, scope consolev1.TemplateScope, scopeName, templateName, versionConstraint string) (string, error) {
	versions, err := k.ListReleaseVersions(ctx, scope, scopeName, templateName)
	if err != nil {
		return "", fmt.Errorf("listing release versions for %s: %w", templateName, err)
	}

	// No releases exist: fall back to live template ConfigMap.
	if len(versions) == 0 {
		cm, err := k.GetTemplate(ctx, scope, scopeName, templateName)
		if err != nil {
			return "", fmt.Errorf("getting live template %s: %w", templateName, err)
		}
		return cm.Data[CueTemplateKey], nil
	}

	// Parse the version constraint.
	constraint, err := ParseConstraint(versionConstraint)
	if err != nil {
		return "", err
	}

	// Find the latest matching version.
	best := LatestMatchingVersion(versions, constraint)
	if best == nil {
		return "", fmt.Errorf("no release of %q matches constraint %q", templateName, versionConstraint)
	}

	// Fetch the release ConfigMap.
	cm, err := k.GetRelease(ctx, scope, scopeName, templateName, best)
	if err != nil {
		return "", fmt.Errorf("getting release %s@%s: %w", templateName, best.String(), err)
	}

	return cm.Data[CueTemplateKey], nil
}

// configMapToTemplate converts a Kubernetes ConfigMap to a Template protobuf message.
// The scope and scopeName must be provided by the caller since they are encoded
// in the namespace (which the ConfigMap stores but the proto carries explicitly).
func configMapToTemplate(cm *corev1.ConfigMap, scope consolev1.TemplateScope, scopeName string) *consolev1.Template {
	cueSource := cm.Data[CueTemplateKey]
	enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
	// The `mandatory` field was removed from Template in HOL-555; the
	// annotation remains in ConfigMap storage to avoid a mass migration here,
	// but it is no longer projected into the proto. TemplatePolicy REQUIRE
	// rules (HOL-557) become the only mechanism for "always apply".
	tmpl := &consolev1.Template{
		Name:        cm.Name,
		DisplayName: cm.Annotations[v1alpha2.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha2.AnnotationDescription],
		CueTemplate: cueSource,
		Enabled:     enabled,
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     scope,
			ScopeName: scopeName,
		},
	}

	// Populate linked templates from the v1alpha2 annotation.
	if raw, ok := cm.Annotations[v1alpha2.AnnotationLinkedTemplates]; ok && raw != "" {
		refs, err := unmarshalLinkedTemplates(raw)
		if err == nil {
			tmpl.LinkedTemplates = refs
		} else {
			slog.Warn("failed to parse linked-templates annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		}
	}

	// Priority 1: CUE extraction from the template source (ADR 018 design, ADR 025 per-field extraction).
	// Only project-scope templates carry deployment defaults.
	if scope == consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT && cueSource != "" {
		extracted, err := ExtractDefaults(cueSource)
		if err != nil {
			slog.Warn("failed to extract defaults from CUE template; falling back to annotation",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		} else if extracted != nil {
			tmpl.Defaults = extracted
			return tmpl
		}
	}

	// Priority 2: Annotation fallback for templates with defaults stored as JSON.
	if rawJSON, ok := cm.Data[DefaultsKey]; ok && rawJSON != "" {
		var defaults consolev1.TemplateDefaults
		if err := json.Unmarshal([]byte(rawJSON), &defaults); err == nil {
			tmpl.Defaults = &defaults
		} else {
			slog.Warn("failed to deserialize template defaults from ConfigMap",
				slog.String("name", cm.Name),
				slog.String("namespace", cm.Namespace),
				slog.Any("error", err),
			)
		}
	}
	return tmpl
}

// linkedRef is a deduplicated key for a cross-level template reference.
type linkedRef struct {
	scope     string // e.g. "organization", "folder", "project"
	scopeName string
	name      string
}

// linkedRefFromProto converts a proto LinkedTemplateRef to a linkedRef key.
func linkedRefFromProto(ref *consolev1.LinkedTemplateRef) linkedRef {
	return linkedRef{
		scope:     scopeLabelValue(ref.Scope),
		scopeName: ref.ScopeName,
		name:      ref.Name,
	}
}

// marshalLinkedTemplates serializes LinkedTemplateRef slice to JSON for annotation storage.
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
			Scope:             scopeLabelValue(r.Scope),
			ScopeName:         r.ScopeName,
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

// unmarshalLinkedTemplates parses the AnnotationLinkedTemplates JSON into proto refs.
func unmarshalLinkedTemplates(raw string) ([]*consolev1.LinkedTemplateRef, error) {
	type storedRef struct {
		Scope             string `json:"scope"`
		ScopeName         string `json:"scope_name"`
		Name              string `json:"name"`
		VersionConstraint string `json:"version_constraint,omitempty"`
	}
	var stored []storedRef
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("parsing linked templates: %w", err)
	}
	refs := make([]*consolev1.LinkedTemplateRef, 0, len(stored))
	for _, s := range stored {
		refs = append(refs, &consolev1.LinkedTemplateRef{
			Scope:             scopeFromLabel(s.Scope),
			ScopeName:         s.ScopeName,
			Name:              s.Name,
			VersionConstraint: s.VersionConstraint,
		})
	}
	return refs, nil
}

// scopeFromLabel converts a label string back to a TemplateScope enum value.
func scopeFromLabel(label string) consolev1.TemplateScope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION
	case v1alpha2.TemplateScopeFolder:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER
	case v1alpha2.TemplateScopeProject:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT
	default:
		return consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED
	}
}

// scopeNameFromNs extracts the logical scope name from a namespace name using the resolver.
func scopeNameFromNs(r *resolver.Resolver, ns, scopeLabel string) string {
	switch scopeLabel {
	case v1alpha2.TemplateScopeOrganization:
		name, err := r.OrgFromNamespace(ns)
		if err == nil {
			return name
		}
	case v1alpha2.TemplateScopeFolder:
		name, err := r.FolderFromNamespace(ns)
		if err == nil {
			return name
		}
	case v1alpha2.TemplateScopeProject:
		name, err := r.ProjectFromNamespace(ns)
		if err == nil {
			return name
		}
	}
	return ""
}

// scopeAndNameFromNs infers the scope and logical name from a Kubernetes namespace.
func scopeAndNameFromNs(r *resolver.Resolver, ns string) (consolev1.TemplateScope, string) {
	if name, err := r.OrgFromNamespace(ns); err == nil {
		return consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION, name
	}
	if name, err := r.FolderFromNamespace(ns); err == nil {
		return consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, name
	}
	if name, err := r.ProjectFromNamespace(ns); err == nil {
		return consolev1.TemplateScope_TEMPLATE_SCOPE_PROJECT, name
	}
	return consolev1.TemplateScope_TEMPLATE_SCOPE_UNSPECIFIED, ""
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

// NewAncestorTemplateResolver creates an AncestorTemplateResolver. resolver
// is the TemplatePolicy resolution seam (HOL-566 Phase 4). Callers should
// pass policyresolver.NewNoopResolver() until Phase 5 wires a real
// implementation; nil is accepted for backwards compatibility in tests
// written before HOL-566.
func NewAncestorTemplateResolver(k8s *K8sClient, walker RenderHierarchyWalker, resolver policyresolver.PolicyResolver) *AncestorTemplateResolver {
	return &AncestorTemplateResolver{k8s: k8s, walker: walker, resolver: resolver}
}

// ListAncestorTemplateSources satisfies deployments.AncestorTemplateProvider.
// The targetName identifies the deployment driving the render so Phase 5
// REQUIRE/EXCLUDE evaluation can key off it. Returns the effective policy-
// resolved ref set alongside the sources so the deployments handler can
// write-through to the applied-render-set store without invoking the
// resolver a second time (HOL-569).
func (a *AncestorTemplateResolver) ListAncestorTemplateSources(ctx context.Context, projectNs, targetName string, linkedRefs []*consolev1.LinkedTemplateRef) ([]string, []*consolev1.LinkedTemplateRef, error) {
	return a.k8s.ListEffectiveTemplateSources(ctx, projectNs, TargetKindDeployment, targetName, linkedRefs, a.walker, a.resolver)
}

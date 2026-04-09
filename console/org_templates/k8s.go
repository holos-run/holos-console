package org_templates

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	v1alpha1 "github.com/holos-run/holos-console/api/v1alpha1"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	CueTemplateKey = "template.cue"

	// DefaultReferenceGrantName is the name of the seeded built-in template.
	DefaultReferenceGrantName = "reference-grant"
)

// K8sClient wraps Kubernetes client operations for platform templates (code: OrgTemplate).
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for platform template (OrgTemplate) operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ListOrgTemplates returns all platform template ConfigMaps in the org namespace.
func (k *K8sClient) ListOrgTemplates(ctx context.Context, org string) ([]corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	labelSelector := v1alpha1.LabelResourceType + "=" + v1alpha1.ResourceTypeOrgTemplate
	slog.DebugContext(ctx, "listing platform templates from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing platform templates: %w", err)
	}
	return list.Items, nil
}

// GetOrgTemplate retrieves a platform template ConfigMap by name.
func (k *K8sClient) GetOrgTemplate(ctx context.Context, org, name string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "getting platform template from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateOrgTemplate creates a new platform template ConfigMap in the org namespace.
func (k *K8sClient) CreateOrgTemplate(ctx context.Context, org, name, displayName, description, cueTemplate string, mandatory, enabled bool) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "creating platform template in kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha1.LabelManagedBy:    v1alpha1.ManagedByValue,
				v1alpha1.LabelResourceType: v1alpha1.ResourceTypeOrgTemplate,
			},
			Annotations: map[string]string{
				v1alpha1.AnnotationDisplayName: displayName,
				v1alpha1.AnnotationDescription: description,
				v1alpha1.AnnotationMandatory:   strconv.FormatBool(mandatory),
				v1alpha1.AnnotationEnabled:     strconv.FormatBool(enabled),
			},
		},
		Data: map[string]string{
			CueTemplateKey: cueTemplate,
		},
	}
	return k.client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
}

// UpdateOrgTemplate updates an existing platform template ConfigMap.
// Only non-nil pointer fields are updated.
func (k *K8sClient) UpdateOrgTemplate(ctx context.Context, org, name string, displayName, description, cueTemplate *string, mandatory, enabled *bool) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "updating platform template in kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting platform template for update: %w", err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	if displayName != nil {
		cm.Annotations[v1alpha1.AnnotationDisplayName] = *displayName
	}
	if description != nil {
		cm.Annotations[v1alpha1.AnnotationDescription] = *description
	}
	if mandatory != nil {
		cm.Annotations[v1alpha1.AnnotationMandatory] = strconv.FormatBool(*mandatory)
	}
	if enabled != nil {
		cm.Annotations[v1alpha1.AnnotationEnabled] = strconv.FormatBool(*enabled)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	if cueTemplate != nil {
		cm.Data[CueTemplateKey] = *cueTemplate
	}
	return k.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
}

// DeleteOrgTemplate deletes a platform template ConfigMap.
func (k *K8sClient) DeleteOrgTemplate(ctx context.Context, org, name string) error {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "deleting platform template from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// CloneOrgTemplate copies an existing platform template to a new name.
// The clone inherits the CUE template, description, and mandatory flag from the source.
// The new template starts with enabled=false regardless of the source.
func (k *K8sClient) CloneOrgTemplate(ctx context.Context, org, sourceName, newName, newDisplayName string) (*corev1.ConfigMap, error) {
	source, err := k.GetOrgTemplate(ctx, org, sourceName)
	if err != nil {
		return nil, fmt.Errorf("getting source platform template for clone: %w", err)
	}
	mandatory, _ := strconv.ParseBool(source.Annotations[v1alpha1.AnnotationMandatory])
	return k.CreateOrgTemplate(
		ctx,
		org,
		newName,
		newDisplayName,
		source.Annotations[v1alpha1.AnnotationDescription],
		source.Data[CueTemplateKey],
		mandatory,
		false, // new clones start disabled
	)
}

// SeedDefaultTemplates seeds the built-in HTTPRoute platform template into
// the org namespace if no platform templates exist. This is called on first List
// to avoid a separate migration step. The template is seeded as disabled so
// that org owners can review and configure it (e.g., set the Gateway name)
// before enabling it.
func (k *K8sClient) SeedDefaultTemplates(ctx context.Context, org string) error {
	_, err := k.CreateOrgTemplate(
		ctx,
		org,
		DefaultReferenceGrantName,
		"HTTPRoute",
		"Exposes a deployment's Service via an HTTPRoute through the gateway. Requires a ReferenceGrant in the project namespace (provided by the default deployment template).",
		DefaultReferenceGrantTemplate,
		false, // not mandatory: HTTPRoute is opt-in per deployment
		false, // disabled: configure the Gateway name before enabling
	)
	return err
}

// ListOrgTemplateSourcesForRender returns CUE sources for the effective set of
// org templates that participate in unification at render time (ADR 019).
//
// The effective set is: (mandatory AND enabled) UNION (enabled AND name IN linkedNames).
// Disabled templates are never included, even when listed in linkedNames.
// The result is deduplicated so a mandatory+explicitly-linked template appears once.
//
// linkedNames may be nil or empty to apply only the mandatory policy floor.
func (k *K8sClient) ListOrgTemplateSourcesForRender(ctx context.Context, org string, linkedNames []string) ([]string, error) {
	cms, err := k.ListOrgTemplates(ctx, org)
	if err != nil {
		return nil, err
	}

	// Build a set for O(1) lookup.
	linked := make(map[string]bool, len(linkedNames))
	for _, n := range linkedNames {
		linked[n] = true
	}

	seen := make(map[string]bool)
	var sources []string
	for _, cm := range cms {
		tmpl := configMapToOrgTemplate(&cm, org)
		if !tmpl.Enabled {
			continue // disabled templates never participate
		}
		include := (tmpl.Mandatory) || linked[tmpl.Name]
		if !include {
			continue
		}
		if seen[tmpl.Name] {
			continue // deduplicate
		}
		src := cm.Data[CueTemplateKey]
		if src == "" {
			continue
		}
		seen[tmpl.Name] = true
		sources = append(sources, src)
	}
	return sources, nil
}

// ListLinkableOrgTemplateInfos returns all enabled org templates as Template
// proto messages. Used by the TemplateService to populate the linking UI.
// Only enabled templates are returned; disabled templates cannot be linked.
func (k *K8sClient) ListLinkableOrgTemplateInfos(ctx context.Context, org string) ([]*consolev1.Template, error) {
	cms, err := k.ListOrgTemplates(ctx, org)
	if err != nil {
		return nil, err
	}
	var result []*consolev1.Template
	for _, cm := range cms {
		tmpl := configMapToOrgTemplate(&cm, org)
		if !tmpl.Enabled {
			continue
		}
		result = append(result, tmpl)
	}
	return result, nil
}

// configMapToOrgTemplate converts a Kubernetes ConfigMap to a Template protobuf message.
func configMapToOrgTemplate(cm *corev1.ConfigMap, org string) *consolev1.Template {
	mandatory, _ := strconv.ParseBool(cm.Annotations[v1alpha1.AnnotationMandatory])
	enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha1.AnnotationEnabled])
	return &consolev1.Template{
		Name: cm.Name,
		ScopeRef: &consolev1.TemplateScopeRef{
			Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
			ScopeName: org,
		},
		DisplayName: cm.Annotations[v1alpha1.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha1.AnnotationDescription],
		CueTemplate: cm.Data[CueTemplateKey],
		Mandatory:   mandatory,
		Enabled:     enabled,
	}
}

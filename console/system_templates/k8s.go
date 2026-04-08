package system_templates

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

// K8sClient wraps Kubernetes client operations for system templates.
type K8sClient struct {
	client   kubernetes.Interface
	Resolver *resolver.Resolver
}

// NewK8sClient creates a client for system template operations.
func NewK8sClient(client kubernetes.Interface, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ListSystemTemplates returns all system template ConfigMaps in the org namespace.
func (k *K8sClient) ListSystemTemplates(ctx context.Context, org string) ([]corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	labelSelector := v1alpha1.LabelResourceType + "=" + v1alpha1.ResourceTypeSystemTemplate
	slog.DebugContext(ctx, "listing system templates from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("labelSelector", labelSelector),
	)
	list, err := k.client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing system templates: %w", err)
	}
	return list.Items, nil
}

// GetSystemTemplate retrieves a system template ConfigMap by name.
func (k *K8sClient) GetSystemTemplate(ctx context.Context, org, name string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "getting system template from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

// CreateSystemTemplate creates a new system template ConfigMap in the org namespace.
func (k *K8sClient) CreateSystemTemplate(ctx context.Context, org, name, displayName, description, cueTemplate string, mandatory, enabled bool) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "creating system template in kubernetes",
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
				v1alpha1.LabelResourceType: v1alpha1.ResourceTypeSystemTemplate,
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

// UpdateSystemTemplate updates an existing system template ConfigMap.
// Only non-nil pointer fields are updated.
func (k *K8sClient) UpdateSystemTemplate(ctx context.Context, org, name string, displayName, description, cueTemplate *string, mandatory, enabled *bool) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "updating system template in kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	cm, err := k.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting system template for update: %w", err)
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

// DeleteSystemTemplate deletes a system template ConfigMap.
func (k *K8sClient) DeleteSystemTemplate(ctx context.Context, org, name string) error {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "deleting system template from kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	return k.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

// CloneSystemTemplate copies an existing system template to a new name.
// The clone inherits the CUE template, description, and mandatory flag from the source.
// The new template starts with enabled=false regardless of the source.
func (k *K8sClient) CloneSystemTemplate(ctx context.Context, org, sourceName, newName, newDisplayName string) (*corev1.ConfigMap, error) {
	source, err := k.GetSystemTemplate(ctx, org, sourceName)
	if err != nil {
		return nil, fmt.Errorf("getting source system template for clone: %w", err)
	}
	mandatory, _ := strconv.ParseBool(source.Annotations[v1alpha1.AnnotationMandatory])
	return k.CreateSystemTemplate(
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

// SeedDefaultTemplates seeds the built-in HTTPRoute system template into
// the org namespace if no system templates exist. This is called on first List
// to avoid a separate migration step. The template is seeded as disabled so
// that org owners can review and configure it (e.g., set the Gateway name)
// before enabling it.
func (k *K8sClient) SeedDefaultTemplates(ctx context.Context, org string) error {
	_, err := k.CreateSystemTemplate(
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

// ListEnabledSystemTemplateSources returns the CUE source strings for all enabled
// system templates in the org. Disabled templates are excluded. This method
// satisfies the deployments.SystemTemplateProvider interface via structural typing.
func (k *K8sClient) ListEnabledSystemTemplateSources(ctx context.Context, org string) ([]string, error) {
	cms, err := k.ListSystemTemplates(ctx, org)
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, cm := range cms {
		tmpl := configMapToSystemTemplate(&cm, org)
		if !tmpl.Enabled {
			continue
		}
		src := cm.Data[CueTemplateKey]
		if src == "" {
			continue
		}
		sources = append(sources, src)
	}
	return sources, nil
}

// configMapToSystemTemplate converts a Kubernetes ConfigMap to a SystemTemplate protobuf message.
func configMapToSystemTemplate(cm *corev1.ConfigMap, org string) *consolev1.SystemTemplate {
	mandatory, _ := strconv.ParseBool(cm.Annotations[v1alpha1.AnnotationMandatory])
	enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha1.AnnotationEnabled])
	return &consolev1.SystemTemplate{
		Name:        cm.Name,
		Org:         org,
		DisplayName: cm.Annotations[v1alpha1.AnnotationDisplayName],
		Description: cm.Annotations[v1alpha1.AnnotationDescription],
		CueTemplate: cm.Data[CueTemplateKey],
		Mandatory:   mandatory,
		Enabled:     enabled,
	}
}

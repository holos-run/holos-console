package system_templates

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ManagedByLabel        = "app.kubernetes.io/managed-by"
	ManagedByValue        = "console.holos.run"
	ResourceTypeLabel     = "console.holos.run/resource-type"
	ResourceTypeValue     = "system-template"
	DisplayNameAnnotation = "console.holos.run/display-name"
	DescriptionAnnotation = "console.holos.run/description"
	MandatoryAnnotation   = "console.holos.run/mandatory"
	GatewayNsAnnotation   = "console.holos.run/gateway-namespace"
	CueTemplateKey        = "template.cue"

	// DefaultGatewayNamespace is the default gateway namespace per Istio's
	// recommended Helm install convention.
	// See: https://istio.io/latest/docs/setup/additional-setup/gateway/
	DefaultGatewayNamespace = "istio-ingress"

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
	labelSelector := ResourceTypeLabel + "=" + ResourceTypeValue
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
func (k *K8sClient) CreateSystemTemplate(ctx context.Context, org, name, displayName, description, cueTemplate string, mandatory bool, gatewayNamespace string) (*corev1.ConfigMap, error) {
	ns := k.Resolver.OrgNamespace(org)
	slog.DebugContext(ctx, "creating system template in kubernetes",
		slog.String("org", org),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	if gatewayNamespace == "" {
		gatewayNamespace = DefaultGatewayNamespace
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				ManagedByLabel:    ManagedByValue,
				ResourceTypeLabel: ResourceTypeValue,
			},
			Annotations: map[string]string{
				DisplayNameAnnotation: displayName,
				DescriptionAnnotation: description,
				MandatoryAnnotation:   strconv.FormatBool(mandatory),
				GatewayNsAnnotation:   gatewayNamespace,
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
func (k *K8sClient) UpdateSystemTemplate(ctx context.Context, org, name string, displayName, description, cueTemplate *string, mandatory *bool, gatewayNamespace *string) (*corev1.ConfigMap, error) {
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
		cm.Annotations[DisplayNameAnnotation] = *displayName
	}
	if description != nil {
		cm.Annotations[DescriptionAnnotation] = *description
	}
	if mandatory != nil {
		cm.Annotations[MandatoryAnnotation] = strconv.FormatBool(*mandatory)
	}
	if gatewayNamespace != nil {
		gns := *gatewayNamespace
		if gns == "" {
			gns = DefaultGatewayNamespace
		}
		cm.Annotations[GatewayNsAnnotation] = gns
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

// SeedDefaultTemplates seeds the built-in ReferenceGrant system template into
// the org namespace if no system templates exist. This is called on first List
// to avoid a separate migration step.
func (k *K8sClient) SeedDefaultTemplates(ctx context.Context, org string) error {
	_, err := k.CreateSystemTemplate(
		ctx,
		org,
		DefaultReferenceGrantName,
		"ReferenceGrant",
		"Allows HTTPRoute resources in the gateway namespace to reference Services in the project namespace.",
		DefaultReferenceGrantTemplate,
		true,  // mandatory
		"istio-ingress",
	)
	return err
}

// configMapToSystemTemplate converts a Kubernetes ConfigMap to a SystemTemplate protobuf message.
func configMapToSystemTemplate(cm *corev1.ConfigMap, org string) *consolev1.SystemTemplate {
	mandatory, _ := strconv.ParseBool(cm.Annotations[MandatoryAnnotation])
	gatewayNs := cm.Annotations[GatewayNsAnnotation]
	if gatewayNs == "" {
		gatewayNs = DefaultGatewayNamespace
	}
	return &consolev1.SystemTemplate{
		Name:            cm.Name,
		Org:             org,
		DisplayName:     cm.Annotations[DisplayNameAnnotation],
		Description:     cm.Annotations[DescriptionAnnotation],
		CueTemplate:     cm.Data[CueTemplateKey],
		Mandatory:       mandatory,
		GatewayNamespace: gatewayNs,
	}
}

package templates

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
)

// TemplateExistsAdapter adapts a templates.K8sClient into the
// TemplateExistsResolver interface consumed by console/templatepolicies.
// Living in this package avoids a console/templatepolicies ->
// console/templates import cycle, since the render-time resolver
// (HOL-567) has the templates package consume templatepolicies
// indirectly through policyresolver.
//
// Post-HOL-723 the adapter speaks in Kubernetes namespaces directly; the
// legacy (scope, scopeName) pair is gone, along with the scopeshim
// compatibility layer.
type TemplateExistsAdapter struct {
	k8s *K8sClient
}

// NewTemplateExistsAdapter returns an adapter suitable for
// templatepolicies.Handler.WithTemplateExistsResolver.
func NewTemplateExistsAdapter(k8s *K8sClient) *TemplateExistsAdapter {
	return &TemplateExistsAdapter{k8s: k8s}
}

// TemplateExists reports whether a template with the given name exists in
// the given namespace. A Kubernetes NotFound response is treated as a
// definitive "does not exist"; every other error is returned so the caller
// can log and continue.
func (a *TemplateExistsAdapter) TemplateExists(ctx context.Context, namespace, name string) (bool, error) {
	if a == nil || a.k8s == nil {
		return false, nil
	}
	_, err := a.k8s.GetTemplate(ctx, namespace, name)
	if err == nil {
		return true, nil
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

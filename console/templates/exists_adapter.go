package templates

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TemplateExistsAdapter adapts a templates.K8sClient into the
// TemplateExistsResolver interface consumed by console/templatepolicies.
// Living in this package avoids a console/templatepolicies ->
// console/templates import cycle, since the render-time resolver
// (HOL-567) has the templates package consume templatepolicies
// indirectly through policyresolver.
type TemplateExistsAdapter struct {
	k8s *K8sClient
}

// NewTemplateExistsAdapter returns an adapter suitable for
// templatepolicies.Handler.WithTemplateExistsResolver.
func NewTemplateExistsAdapter(k8s *K8sClient) *TemplateExistsAdapter {
	return &TemplateExistsAdapter{k8s: k8s}
}

// TemplateExists reports whether a template with the given name exists at the
// given scope. A Kubernetes NotFound response is treated as a definitive
// "does not exist"; every other error is returned so the caller can log and
// continue.
func (a *TemplateExistsAdapter) TemplateExists(ctx context.Context, scope consolev1.TemplateScope, scopeName, name string) (bool, error) {
	if a == nil || a.k8s == nil {
		return false, nil
	}
	_, err := a.k8s.GetTemplate(ctx, scope, scopeName, name)
	if err == nil {
		return true, nil
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

package templates

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/holos-run/holos-console/console/scopeshim"
)

// TemplateExistsAdapter adapts a templates.K8sClient into the
// TemplateExistsResolver interface consumed by console/templatepolicies.
// Living in this package avoids a console/templatepolicies ->
// console/templates import cycle, since the render-time resolver
// (HOL-567) has the templates package consume templatepolicies
// indirectly through policyresolver.
//
// The adapter keeps its external (scope, scopeName, name) signature
// unchanged because the templatepolicies package still thinks in scope
// terms; the adapter translates to a namespace internally before calling
// the namespace-keyed K8sClient.GetTemplate (HOL-621).
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
func (a *TemplateExistsAdapter) TemplateExists(ctx context.Context, scope scopeshim.Scope, scopeName, name string) (bool, error) {
	if a == nil || a.k8s == nil {
		return false, nil
	}
	ns, err := a.k8s.namespaceForScope(scope, scopeName)
	if err != nil {
		return false, err
	}
	_, err = a.k8s.GetTemplate(ctx, ns, name)
	if err == nil {
		return true, nil
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

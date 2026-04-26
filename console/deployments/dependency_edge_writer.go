// Package deployments — per-edge cascade-delete writer (HOL-991).
//
// DependencyEdgeWriter is the seam the deployment-form cascade toggle calls
// into to read or write the Spec.CascadeDelete field on the originating
// CRD (TemplateDependency or TemplateRequirement). The interface is defined
// in this package so the deployments handler can stub it in tests without
// pulling controller-runtime fixtures into the unit-test path.
package deployments

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// OriginatingObjectKind enumerates the CRD kinds that can declare a
// dependency edge. Mirrors the wire-side OriginatingObject.kind string.
const (
	KindTemplateDependency  = "TemplateDependency"
	KindTemplateRequirement = "TemplateRequirement"
)

// DependencyEdgeWriter reads and writes Spec.CascadeDelete on the
// originating CRD. Implementations are responsible for dispatching by kind
// to the correct typed CRD; callers pass the (kind, namespace, name) tuple
// from the deployment-edge wire message.
type DependencyEdgeWriter interface {
	// GetCascadeDelete returns the current Spec.CascadeDelete value for
	// the addressed CRD. An unset CRD field is reported as true to match
	// the +kubebuilder:default=true on both TemplateDependency and
	// TemplateRequirement.
	GetCascadeDelete(ctx context.Context, kind, namespace, name string) (bool, error)
	// SetCascadeDelete persists the value to Spec.CascadeDelete on the
	// addressed CRD. The implementation does a Get-Update round trip so
	// the patch composes with concurrent writes from the reconcilers
	// (which only touch ownerReferences and status).
	SetCascadeDelete(ctx context.Context, kind, namespace, name string, value bool) error
}

// DependencyEdgeCRDWriter is the production DependencyEdgeWriter backed by a
// controller-runtime client. Constructed in console.go from
// controllerMgr.GetClient() so it shares the cache-backed informer the
// reconcilers use.
type DependencyEdgeCRDWriter struct {
	client ctrlclient.Client
}

// NewDependencyEdgeCRDWriter returns a writer that reads and updates
// TemplateDependency / TemplateRequirement objects via the supplied
// controller-runtime client.
func NewDependencyEdgeCRDWriter(c ctrlclient.Client) *DependencyEdgeCRDWriter {
	return &DependencyEdgeCRDWriter{client: c}
}

// GetCascadeDelete implements DependencyEdgeWriter.
func (w *DependencyEdgeCRDWriter) GetCascadeDelete(ctx context.Context, kind, namespace, name string) (bool, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	switch kind {
	case KindTemplateDependency:
		var d templatesv1alpha1.TemplateDependency
		if err := w.client.Get(ctx, key, &d); err != nil {
			return false, err
		}
		return cascadeDeleteWithDefault(d.Spec.CascadeDelete), nil
	case KindTemplateRequirement:
		var r templatesv1alpha1.TemplateRequirement
		if err := w.client.Get(ctx, key, &r); err != nil {
			return false, err
		}
		return cascadeDeleteWithDefault(r.Spec.CascadeDelete), nil
	default:
		return false, fmt.Errorf("unsupported originating-object kind %q", kind)
	}
}

// setCascadeDeleteMaxAttempts caps the conflict-retry loop in
// SetCascadeDelete. Mirrors the same bound used in
// console/policyresolver/applied_state.go so the two paths are consistent
// when fighting reconciler write traffic.
const setCascadeDeleteMaxAttempts = 3

// SetCascadeDelete implements DependencyEdgeWriter. Get/Update is retried on
// resource-version conflicts (HTTP 409) up to setCascadeDeleteMaxAttempts
// times so a concurrent reconciler write (which only touches ownerReferences
// and status) does not surface as CodeInternal to the user. Other errors are
// returned to the caller unchanged so the handler can map them through the
// shared mapK8sError helper.
func (w *DependencyEdgeCRDWriter) SetCascadeDelete(ctx context.Context, kind, namespace, name string, value bool) error {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	var lastErr error
	for attempt := 0; attempt < setCascadeDeleteMaxAttempts; attempt++ {
		switch kind {
		case KindTemplateDependency:
			var d templatesv1alpha1.TemplateDependency
			if err := w.client.Get(ctx, key, &d); err != nil {
				return err
			}
			d.Spec.CascadeDelete = &value
			lastErr = w.client.Update(ctx, &d)
		case KindTemplateRequirement:
			var r templatesv1alpha1.TemplateRequirement
			if err := w.client.Get(ctx, key, &r); err != nil {
				return err
			}
			r.Spec.CascadeDelete = &value
			lastErr = w.client.Update(ctx, &r)
		default:
			return fmt.Errorf("unsupported originating-object kind %q", kind)
		}
		if lastErr == nil {
			return nil
		}
		if !k8serrors.IsConflict(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// cascadeDeleteWithDefault treats a nil pointer as true to match the CRD's
// +kubebuilder:default=true. Without this, callers would have to know the
// CRD default themselves to interpret the response.
func cascadeDeleteWithDefault(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// IsNotFound reports whether the error from a DependencyEdgeWriter call is a
// 404. The deployments handler maps this to connect.CodeNotFound so the
// frontend can show a clear "edge no longer exists" message instead of an
// internal-server error.
func IsNotFound(err error) bool {
	return k8serrors.IsNotFound(err)
}

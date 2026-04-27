package resourcerbac

import (
	"context"
	"fmt"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// BootstrapTimeout bounds the wait for RBAC propagation to be observable to
// the impersonated caller after a fresh resource create. Declared as a var so
// tests may override the deadline to exercise the timeout branch without
// stalling for the full production window.
var BootstrapTimeout = 10 * time.Second

// BootstrapResourceRBACAndWait synchronously applies per-resource RBAC for obj
// using the privileged client, then — when an impersonated client is supplied
// — polls SelfSubjectAccessReview until the impersonated caller can `update`
// the resource. This closes the race window between Create returning and the
// async per-resource RBAC reconciler running, which would otherwise let the
// next bootstrap step (read/update/delete via the impersonated client) fail
// with Forbidden.
//
// impersonated may be nil (tests, non-impersonated paths); the wait is
// skipped in that case but RBAC is still applied.
func BootstrapResourceRBACAndWait(ctx context.Context, privileged kubernetes.Interface, impersonated kubernetes.Interface, obj metav1.Object, cfg KindConfig) error {
	if err := EnsureResourceRBAC(ctx, privileged, obj, cfg); err != nil {
		return fmt.Errorf("provisioning %s RBAC for %q: %w", cfg.Kind, objectName(obj, cfg), err)
	}
	if impersonated == nil {
		return nil
	}
	return waitForUpdateAccess(ctx, impersonated, obj, cfg, BootstrapTimeout)
}

func waitForUpdateAccess(ctx context.Context, client kubernetes.Interface, obj metav1.Object, cfg KindConfig, timeout time.Duration) error {
	name := objectName(obj, cfg)
	apiGroup := cfg.APIGroup
	if apiGroup == "" && cfg.OwnerAPIVersion == "" {
		apiGroup = TemplatesAPIGroup
	}
	attrs := &authv1.ResourceAttributes{
		Verb:     "update",
		Group:    apiGroup,
		Resource: cfg.Resource,
		Name:     name,
	}
	if !cfg.ClusterScoped {
		attrs.Namespace = rbacNamespace(obj, cfg)
	}

	deadline := time.Now().Add(timeout)
	backoff := 50 * time.Millisecond
	for {
		review := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attrs}}
		got, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err == nil && got.Status.Allowed {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("waiting for owner access to %s %q: %w", cfg.Kind, name, err)
			}
			return fmt.Errorf("timed out waiting for owner access to %s %q after %s", cfg.Kind, name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 500*time.Millisecond {
			backoff *= 2
		}
	}
}

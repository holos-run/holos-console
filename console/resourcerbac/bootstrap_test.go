package resourcerbac

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// allowReactor returns an SSAR reactor that always allows.
func allowReactor() func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		ca, ok := action.(k8stesting.CreateAction)
		if !ok {
			return false, nil, nil
		}
		ssar, ok := ca.GetObject().(*authv1.SelfSubjectAccessReview)
		if !ok {
			return false, nil, nil
		}
		ssar.Status.Allowed = true
		return true, ssar, nil
	}
}

func TestBootstrapResourceRBACAndWait_NilImpersonatedSkipsWait(t *testing.T) {
	ns := managedNamespace(t, "holos-org-platform", "organization", nil, nil)
	privileged := fake.NewClientset()
	if err := BootstrapResourceRBACAndWait(context.Background(), privileged, nil, ns, Organizations); err != nil {
		t.Fatalf("BootstrapResourceRBACAndWait: %v", err)
	}
	roles, err := privileged.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing cluster roles: %v", err)
	}
	if len(roles.Items) == 0 {
		t.Fatal("expected cluster roles to be provisioned")
	}
}

func TestBootstrapResourceRBACAndWait_WaitsForSSARAllowed(t *testing.T) {
	ns := managedNamespace(t, "holos-org-platform", "organization", nil, nil)
	privileged := fake.NewClientset()
	impersonated := fake.NewClientset()
	impersonated.PrependReactor("create", "selfsubjectaccessreviews", allowReactor())
	if err := BootstrapResourceRBACAndWait(context.Background(), privileged, impersonated, ns, Organizations); err != nil {
		t.Fatalf("BootstrapResourceRBACAndWait: %v", err)
	}
}

func TestBootstrapResourceRBACAndWait_TimesOutWhenSSARDenied(t *testing.T) {
	ns := managedNamespace(t, "holos-org-platform", "organization", nil, nil)
	privileged := fake.NewClientset()
	impersonated := fake.NewClientset()
	// Default behavior on fake is Allowed=false; nudge timeout via a tight ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := BootstrapResourceRBACAndWait(ctx, privileged, impersonated, ns, Organizations); err == nil {
		t.Fatal("expected error when impersonated caller has no access")
	}
}

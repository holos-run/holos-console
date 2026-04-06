package system_templates

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/rpc"
)

// stubResourceApplier implements ResourceApplier for tests.
type stubResourceApplier struct {
	calls []applyCall
	err   error
}

type applyCall struct {
	namespace      string
	deploymentName string
	resourceCount  int
}

func (s *stubResourceApplier) Apply(_ context.Context, namespace, deploymentName string, resources []unstructured.Unstructured) error {
	s.calls = append(s.calls, applyCall{
		namespace:      namespace,
		deploymentName: deploymentName,
		resourceCount:  len(resources),
	})
	return s.err
}

func TestApplyMandatorySystemTemplates_AppliesMandatoryTemplates(t *testing.T) {
	ns := orgNS("my-org")
	cm := sysTemplateConfigMap("my-org", DefaultReferenceGrantName, "ReferenceGrant", "desc", DefaultReferenceGrantTemplate, true, "istio-ingress")
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{
		Sub:   "user-123",
		Email: "owner@example.com",
		Iss:   "https://example.com",
		Exp:   9999999999,
		Iat:   1000000000,
		EmailVerified: true,
	}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(applier.calls) != 1 {
		t.Fatalf("expected 1 apply call, got %d", len(applier.calls))
	}
	if applier.calls[0].namespace != "prj-my-project" {
		t.Errorf("expected namespace 'prj-my-project', got %q", applier.calls[0].namespace)
	}
	if applier.calls[0].deploymentName != DefaultReferenceGrantName {
		t.Errorf("expected deployment name %q, got %q", DefaultReferenceGrantName, applier.calls[0].deploymentName)
	}
	if applier.calls[0].resourceCount < 1 {
		t.Errorf("expected at least 1 resource applied, got %d", applier.calls[0].resourceCount)
	}
}

func TestApplyMandatorySystemTemplates_SkipsNonMandatoryTemplates(t *testing.T) {
	ns := orgNS("my-org")
	// mandatory=false — should not be applied.
	cm := sysTemplateConfigMap("my-org", "optional-template", "Optional", "desc", DefaultReferenceGrantTemplate, false, "istio-ingress")
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{Sub: "user-123", Email: "owner@example.com"}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(applier.calls) != 0 {
		t.Errorf("expected 0 apply calls for non-mandatory template, got %d", len(applier.calls))
	}
}

func TestApplyMandatorySystemTemplates_NoTemplates(t *testing.T) {
	ns := orgNS("my-org")
	fakeClient := fake.NewClientset(ns)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{Sub: "user-123", Email: "owner@example.com"}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err != nil {
		t.Fatalf("expected no error when no templates exist, got %v", err)
	}

	if len(applier.calls) != 0 {
		t.Errorf("expected 0 apply calls, got %d", len(applier.calls))
	}
}

func TestApplyMandatorySystemTemplates_ApplierErrorPropagates(t *testing.T) {
	ns := orgNS("my-org")
	cm := sysTemplateConfigMap("my-org", DefaultReferenceGrantName, "ReferenceGrant", "desc", DefaultReferenceGrantTemplate, true, "istio-ingress")
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{err: fmt.Errorf("apply failed")}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{Sub: "user-123", Email: "owner@example.com"}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err == nil {
		t.Fatal("expected error when applier fails, got nil")
	}
}

func TestApplyMandatorySystemTemplates_NilApplierSkips(t *testing.T) {
	ns := orgNS("my-org")
	cm := sysTemplateConfigMap("my-org", DefaultReferenceGrantName, "ReferenceGrant", "desc", DefaultReferenceGrantTemplate, true, "istio-ingress")
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	// nil applier — should log a warning and skip without error.
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, nil)

	claims := &rpc.Claims{Sub: "user-123", Email: "owner@example.com"}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err != nil {
		t.Fatalf("expected no error with nil applier (should skip), got %v", err)
	}
}

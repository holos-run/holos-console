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

// minimalSystemTemplate is a minimal system template for testing the
// MandatoryTemplateApplier. It only references platform.namespace (not input.*)
// so it can be rendered standalone at project creation time without a deployment
// template or user input.
const minimalSystemTemplate = `

platform: {
	project:          string
	namespace:        string
	gatewayNamespace: string
	organization:     string
	claims: {
		iss:            string
		sub:            string
		exp:            int
		iat:            int
		email:          string
		email_verified: bool
	}
}

input: {
	name:  string
	image: string
	tag:   string
}

projectResources: {
	namespacedResources: {}
	clusterResources: {}
}
platformResources: {
	namespacedResources: (platform.namespace): {
		ServiceAccount: "system-sa": {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      "system-sa"
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       "system-sa"
				}
			}
		}
	}
	clusterResources: {}
}
`

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

func TestApplyMandatorySystemTemplates_AppliesMandatoryAndEnabledTemplates(t *testing.T) {
	ns := orgNS("my-org")
	// mandatory=true AND enabled=true — should be applied.
	// Use a minimal system template that can render standalone without a deployment template.
	cm := sysTemplateConfigMap("my-org", "minimal-template", "Minimal", "desc", minimalSystemTemplate, true, true)
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{
		Sub:           "user-123",
		Email:         "owner@example.com",
		Iss:           "https://example.com",
		Exp:           9999999999,
		Iat:           1000000000,
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
	if applier.calls[0].deploymentName != "minimal-template" {
		t.Errorf("expected deployment name %q, got %q", "minimal-template", applier.calls[0].deploymentName)
	}
	if applier.calls[0].resourceCount < 1 {
		t.Errorf("expected at least 1 resource applied, got %d", applier.calls[0].resourceCount)
	}
}

func TestApplyMandatorySystemTemplates_SkipsNonMandatoryTemplates(t *testing.T) {
	ns := orgNS("my-org")
	// mandatory=false — should not be applied.
	cm := sysTemplateConfigMap("my-org", "optional-template", "Optional", "desc", DefaultReferenceGrantTemplate, false, false)
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
	// mandatory=true AND enabled=true so the applier is reached and can fail.
	cm := sysTemplateConfigMap("my-org", "minimal-template", "Minimal", "desc", minimalSystemTemplate, true, true)
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	applier := &stubResourceApplier{err: fmt.Errorf("apply failed")}
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, applier)

	claims := &rpc.Claims{
		Sub:           "user-123",
		Email:         "owner@example.com",
		Iss:           "https://example.com",
		Exp:           9999999999,
		Iat:           1000000000,
		EmailVerified: true,
	}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err == nil {
		t.Fatal("expected error when applier fails, got nil")
	}
}

func TestApplyMandatorySystemTemplates_SkipsDisabledMandatoryTemplates(t *testing.T) {
	ns := orgNS("my-org")
	// mandatory=true but enabled=false — should NOT be applied.
	cm := sysTemplateConfigMap("my-org", "minimal-template", "Minimal", "desc", minimalSystemTemplate, true, false)
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
		t.Errorf("expected 0 apply calls for disabled mandatory template, got %d", len(applier.calls))
	}
}

func TestApplyMandatorySystemTemplates_NilApplierSkips(t *testing.T) {
	ns := orgNS("my-org")
	// mandatory=true AND enabled=true so the nil-applier warning path is reached.
	cm := sysTemplateConfigMap("my-org", "minimal-template", "Minimal", "desc", minimalSystemTemplate, true, true)
	fakeClient := fake.NewClientset(ns, cm)
	k8s := NewK8sClient(fakeClient, testResolver())
	// nil applier — should log a warning and skip without error.
	mta := NewMandatoryTemplateApplier(k8s, &deployments.CueRenderer{}, nil)

	claims := &rpc.Claims{
		Sub:           "user-123",
		Email:         "owner@example.com",
		Iss:           "https://example.com",
		Exp:           9999999999,
		Iat:           1000000000,
		EmailVerified: true,
	}

	err := mta.ApplyMandatorySystemTemplates(context.Background(), "my-org", "my-project", "prj-my-project", claims)
	if err != nil {
		t.Fatalf("expected no error with nil applier (should skip), got %v", err)
	}
}

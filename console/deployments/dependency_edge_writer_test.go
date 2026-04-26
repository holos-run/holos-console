package deployments

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// stubDependencyEdgeWriter records calls and returns canned responses. The
// handler does not need a real controller-runtime client to exercise the RPC
// surface — the round-trip through the typed CRD is covered by the
// DependencyEdgeCRDWriter tests below using an envtest-free in-memory client.
type stubDependencyEdgeWriter struct {
	getValue bool
	getErr   error
	setErr   error
	gotKind  string
	gotNS    string
	gotName  string
	setValue bool
	setCalls int
	getCalls int
}

func (s *stubDependencyEdgeWriter) GetCascadeDelete(_ context.Context, kind, namespace, name string) (bool, error) {
	s.getCalls++
	s.gotKind = kind
	s.gotNS = namespace
	s.gotName = name
	return s.getValue, s.getErr
}

func (s *stubDependencyEdgeWriter) SetCascadeDelete(_ context.Context, kind, namespace, name string, value bool) error {
	s.setCalls++
	s.gotKind = kind
	s.gotNS = namespace
	s.gotName = name
	s.setValue = value
	return s.setErr
}

func newOriginating(kind, ns, name string) *consolev1.OriginatingObject {
	return &consolev1.OriginatingObject{Kind: kind, Namespace: ns, Name: name}
}

func TestHandler_GetDependencyEdgeCascadeDelete(t *testing.T) {
	t.Run("returns persisted value", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		stub := &stubDependencyEdgeWriter{getValue: false}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		resp, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Msg.GetCascadeDelete() != false {
			t.Errorf("cascade_delete = %v, want false", resp.Msg.GetCascadeDelete())
		}
		if stub.getCalls != 1 {
			t.Errorf("getCalls = %d, want 1", stub.getCalls)
		}
		if stub.gotKind != KindTemplateDependency || stub.gotNS != "prj-other" || stub.gotName != "edge-1" {
			t.Errorf("dispatched (kind=%q, ns=%q, name=%q)", stub.gotKind, stub.gotNS, stub.gotName)
		}
	})

	t.Run("unauthenticated rejected", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		h := defaultHandler(fakeClient, &stubProjectResolver{}).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})

		_, err := h.GetDependencyEdgeCascadeDelete(context.Background(), connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want Unauthenticated", got)
		}
	})

	t.Run("non-grantee rejected with PermissionDenied", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		h := defaultHandler(fakeClient, &stubProjectResolver{}).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})
		ctx := authedCtx("nobody@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want PermissionDenied", got)
		}
	})

	t.Run("invalid kind rejected", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating("Template", "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want InvalidArgument", got)
		}
	})

	t.Run("missing originating_object rejected", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project: "my-project",
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want InvalidArgument", got)
		}
	})

	t.Run("missing project rejected", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		h := defaultHandler(fakeClient, &stubProjectResolver{}).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want InvalidArgument", got)
		}
	})

	t.Run("not-found mapped", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		notFoundErr := k8serrors.NewNotFound(schema.GroupResource{Group: "templates.holos.run", Resource: "templatedependencies"}, "edge-1")
		stub := &stubDependencyEdgeWriter{getErr: notFoundErr}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeNotFound {
			t.Errorf("code = %v, want NotFound", got)
		}
	})

	t.Run("writer not configured returns FailedPrecondition", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		h := defaultHandler(fakeClient, pr) // no WithDependencyEdgeWriter
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
			t.Errorf("code = %v, want FailedPrecondition", got)
		}
	})

	t.Run("writer error mapped to Internal", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		stub := &stubDependencyEdgeWriter{getErr: errors.New("boom")}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.GetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.GetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
		}))
		if got := connect.CodeOf(err); got != connect.CodeInternal {
			t.Errorf("code = %v, want Internal", got)
		}
	})
}

func TestHandler_SetDependencyEdgeCascadeDelete(t *testing.T) {
	t.Run("owner can write", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		stub := &stubDependencyEdgeWriter{}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		resp, err := h.SetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.SetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateRequirement, "prj-other", "edge-1"),
			CascadeDelete:     false,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Msg.GetCascadeDelete() != false {
			t.Errorf("response cascade_delete = %v, want false", resp.Msg.GetCascadeDelete())
		}
		if stub.setCalls != 1 || stub.setValue != false {
			t.Errorf("setCalls=%d, setValue=%v; want 1, false", stub.setCalls, stub.setValue)
		}
		if stub.gotKind != KindTemplateRequirement {
			t.Errorf("dispatched kind = %q, want %q", stub.gotKind, KindTemplateRequirement)
		}
	})

	t.Run("viewer cannot write", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "viewer"}}
		stub := &stubDependencyEdgeWriter{}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.SetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.SetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
			CascadeDelete:     true,
		}))
		if got := connect.CodeOf(err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want PermissionDenied", got)
		}
		if stub.setCalls != 0 {
			t.Errorf("setCalls = %d, want 0", stub.setCalls)
		}
	})

	t.Run("not-found mapped", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		notFoundErr := k8serrors.NewNotFound(schema.GroupResource{Group: "templates.holos.run", Resource: "templatedependencies"}, "edge-1")
		stub := &stubDependencyEdgeWriter{setErr: notFoundErr}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(stub)
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.SetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.SetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
			CascadeDelete:     true,
		}))
		if got := connect.CodeOf(err); got != connect.CodeNotFound {
			t.Errorf("code = %v, want NotFound", got)
		}
	})

	t.Run("invalid kind rejected", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		h := defaultHandler(fakeClient, pr).WithDependencyEdgeWriter(&stubDependencyEdgeWriter{})
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.SetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.SetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating("ConfigMap", "prj-other", "edge-1"),
			CascadeDelete:     true,
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want InvalidArgument", got)
		}
	})

	t.Run("writer not configured returns FailedPrecondition", func(t *testing.T) {
		fakeClient := fake.NewClientset(projectNS("my-project"))
		pr := &stubProjectResolver{users: map[string]string{"alice@example.com": "owner"}}
		h := defaultHandler(fakeClient, pr)
		ctx := authedCtx("alice@example.com", nil)

		_, err := h.SetDependencyEdgeCascadeDelete(ctx, connect.NewRequest(&consolev1.SetDependencyEdgeCascadeDeleteRequest{
			Project:           "my-project",
			OriginatingObject: newOriginating(KindTemplateDependency, "prj-other", "edge-1"),
			CascadeDelete:     true,
		}))
		if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
			t.Errorf("code = %v, want FailedPrecondition", got)
		}
	})
}

func TestCascadeDeleteWithDefault(t *testing.T) {
	if !cascadeDeleteWithDefault(nil) {
		t.Error("nil pointer should default to true (matches +kubebuilder:default=true)")
	}
	v := false
	if cascadeDeleteWithDefault(&v) {
		t.Error("explicit false should round-trip as false")
	}
	v = true
	if !cascadeDeleteWithDefault(&v) {
		t.Error("explicit true should round-trip as true")
	}
}

func TestDependencyEdgeCRDWriter_RoundTrip(t *testing.T) {
	s := runtime.NewScheme()
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	td := &templatesv1alpha1.TemplateDependency{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prj-alpha", Name: "td-1"},
	}
	tr := &templatesv1alpha1.TemplateRequirement{
		ObjectMeta: metav1.ObjectMeta{Namespace: "org-shared", Name: "tr-1"},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(s).WithObjects(td, tr).Build()
	w := NewDependencyEdgeCRDWriter(c)
	ctx := context.Background()

	// Default semantics: nil pointer reads as true.
	got, err := w.GetCascadeDelete(ctx, KindTemplateDependency, "prj-alpha", "td-1")
	if err != nil {
		t.Fatalf("GetCascadeDelete: %v", err)
	}
	if !got {
		t.Errorf("nil Spec.CascadeDelete should default to true")
	}

	// Write false, then read back.
	if err := w.SetCascadeDelete(ctx, KindTemplateDependency, "prj-alpha", "td-1", false); err != nil {
		t.Fatalf("SetCascadeDelete false: %v", err)
	}
	got, err = w.GetCascadeDelete(ctx, KindTemplateDependency, "prj-alpha", "td-1")
	if err != nil {
		t.Fatalf("GetCascadeDelete: %v", err)
	}
	if got {
		t.Errorf("after writing false, GetCascadeDelete = true")
	}

	// Write true on a TemplateRequirement.
	if err := w.SetCascadeDelete(ctx, KindTemplateRequirement, "org-shared", "tr-1", true); err != nil {
		t.Fatalf("SetCascadeDelete true: %v", err)
	}
	got, err = w.GetCascadeDelete(ctx, KindTemplateRequirement, "org-shared", "tr-1")
	if err != nil {
		t.Fatalf("GetCascadeDelete: %v", err)
	}
	if !got {
		t.Errorf("after writing true, GetCascadeDelete = false")
	}

	// Unknown kind rejected.
	if _, err := w.GetCascadeDelete(ctx, "ConfigMap", "prj-alpha", "x"); err == nil {
		t.Error("expected error for unsupported kind")
	}
	if err := w.SetCascadeDelete(ctx, "ConfigMap", "prj-alpha", "x", true); err == nil {
		t.Error("expected error for unsupported kind")
	}

	// NotFound surfaces through IsNotFound.
	_, err = w.GetCascadeDelete(ctx, KindTemplateDependency, "prj-alpha", "missing")
	if !IsNotFound(err) {
		t.Errorf("missing object: IsNotFound(err)=false, err=%v", err)
	}
}

func TestIsNotFound(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("nil should not be NotFound")
	}
	if IsNotFound(errors.New("boom")) {
		t.Error("generic error should not be NotFound")
	}
	notFoundErr := k8serrors.NewNotFound(schema.GroupResource{Group: "x", Resource: "y"}, "z")
	if !IsNotFound(notFoundErr) {
		t.Error("k8s NotFound should be reported as NotFound")
	}
}

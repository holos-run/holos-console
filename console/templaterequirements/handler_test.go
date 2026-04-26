package templaterequirements

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

type stubOrgGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubOrgGrantResolver) GetOrgGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

type stubFolderGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubFolderGrantResolver) GetFolderGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
	return s.users, s.roles, s.err
}

func authedCtx(email string, roles []string) context.Context {
	return rpc.ContextWithClaims(context.Background(), &rpc.Claims{
		Sub:   "user-test",
		Email: email,
		Roles: roles,
	})
}

func newTestResolver() *resolver.Resolver {
	return &resolver.Resolver{
		NamespacePrefix:    "holos-",
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("register client-go scheme: %v", err)
	}
	if err := templatesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("register templates scheme: %v", err)
	}
	return s
}

func newTestHandler(t *testing.T, shareUsers map[string]string, objs ...ctrlclient.Object) (*Handler, ctrlclient.Client, string) {
	t.Helper()
	r := newTestResolver()
	folderNS := r.FolderNamespace("platform")
	ctrlClient := ctrlfake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		Build()

	h := NewHandler(NewK8sClient(ctrlClient), r).
		WithOrgGrantResolver(&stubOrgGrantResolver{users: shareUsers}).
		WithFolderGrantResolver(&stubFolderGrantResolver{users: shareUsers})
	return h, ctrlClient, folderNS
}

func boolPtr(v bool) *bool {
	return &v
}

func basicRequirement(namespace string) *consolev1.TemplateRequirement {
	return &consolev1.TemplateRequirement{
		Name:      "require-platform-config",
		Namespace: namespace,
		Requires: &consolev1.LinkedTemplateRef{
			Namespace:         "holos-org-acme",
			Name:              "shared-config",
			VersionConstraint: ">=1.0.0 <2.0.0",
		},
		TargetRefs: []*consolev1.TemplateRequirementTargetRef{
			{
				Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
				Name:        "*",
				ProjectName: "*",
			},
		},
		CascadeDelete: boolPtr(false),
	}
}

func getRequirementCR(t *testing.T, c ctrlclient.Client, namespace, name string) *templatesv1alpha1.TemplateRequirement {
	t.Helper()
	var req templatesv1alpha1.TemplateRequirement
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &req); err != nil {
		t.Fatalf("get requirement CR %s/%s: %v", namespace, name, err)
	}
	return &req
}

func listRequirementCRs(t *testing.T, c ctrlclient.Client, namespace string) []templatesv1alpha1.TemplateRequirement {
	t.Helper()
	var list templatesv1alpha1.TemplateRequirementList
	if err := c.List(context.Background(), &list, ctrlclient.InNamespace(namespace)); err != nil {
		t.Fatalf("list requirement CRs: %v", err)
	}
	return list.Items
}

func TestTemplateRequirementCRUD(t *testing.T) {
	h, ctrlClient, ns := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	createResp, err := h.CreateTemplateRequirement(ctx, connect.NewRequest(&consolev1.CreateTemplateRequirementRequest{
		Namespace:   ns,
		Requirement: basicRequirement(ns),
	}))
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	if got, want := createResp.Msg.GetName(), "require-platform-config"; got != want {
		t.Fatalf("created name: got %q, want %q", got, want)
	}

	stored := getRequirementCR(t, ctrlClient, ns, "require-platform-config")
	if got := stored.Labels[v1alpha2.LabelResourceType]; got != v1alpha2.ResourceTypeTemplateRequirement {
		t.Errorf("resource type label: got %q", got)
	}
	if got := stored.Annotations[v1alpha2.AnnotationCreatorEmail]; got != "owner@example.com" {
		t.Errorf("creator annotation: got %q", got)
	}
	if stored.Spec.CascadeDelete == nil || *stored.Spec.CascadeDelete {
		t.Fatalf("cascadeDelete: got %v, want false pointer", stored.Spec.CascadeDelete)
	}
	stored.Status = templatesv1alpha1.TemplateRequirementStatus{
		ObservedGeneration: 7,
		Conditions: []metav1.Condition{
			{
				Type:               templatesv1alpha1.TemplateRequirementConditionReady,
				Status:             metav1.ConditionTrue,
				Reason:             templatesv1alpha1.TemplateRequirementReasonReady,
				Message:            "ready",
				ObservedGeneration: 7,
				LastTransitionTime: metav1.NewTime(time.Unix(100, 0)),
			},
		},
	}
	if err := ctrlClient.Update(context.Background(), stored); err != nil {
		t.Fatalf("seed status through controller-runtime fake: %v", err)
	}

	listResp, err := h.ListTemplateRequirements(ctx, connect.NewRequest(&consolev1.ListTemplateRequirementsRequest{
		Namespace: ns,
	}))
	if err != nil {
		t.Fatalf("list requirements: %v", err)
	}
	if got := len(listResp.Msg.GetRequirements()); got != 1 {
		t.Fatalf("list count: got %d, want 1", got)
	}
	if got := listResp.Msg.GetRequirements()[0].GetStatus().GetConditions()[0].GetType(); got != "Ready" {
		t.Errorf("condition type: got %q, want Ready", got)
	}

	getResp, err := h.GetTemplateRequirement(ctx, connect.NewRequest(&consolev1.GetTemplateRequirementRequest{
		Namespace: ns,
		Name:      "require-platform-config",
	}))
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if got := getResp.Msg.GetRequirement().GetRequires().GetVersionConstraint(); got != ">=1.0.0 <2.0.0" {
		t.Errorf("requires version constraint: got %q", got)
	}

	updated := basicRequirement(ns)
	updated.Requires = &consolev1.LinkedTemplateRef{Namespace: ns, Name: "gateway"}
	updated.TargetRefs = []*consolev1.TemplateRequirementTargetRef{
		{
			Kind:        consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
			Name:        "api",
			ProjectName: "checkout",
		},
	}
	updated.CascadeDelete = boolPtr(true)
	if _, err := h.UpdateTemplateRequirement(ctx, connect.NewRequest(&consolev1.UpdateTemplateRequirementRequest{
		Namespace:   ns,
		Requirement: updated,
	})); err != nil {
		t.Fatalf("update requirement: %v", err)
	}
	stored = getRequirementCR(t, ctrlClient, ns, "require-platform-config")
	if got := stored.Spec.Requires.Name; got != "gateway" {
		t.Errorf("updated requires.name: got %q, want gateway", got)
	}
	if got := stored.Spec.TargetRefs[0].Kind; got != templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate {
		t.Errorf("updated target kind: got %q", got)
	}
	if stored.Spec.CascadeDelete == nil || !*stored.Spec.CascadeDelete {
		t.Fatalf("updated cascadeDelete: got %v, want true pointer", stored.Spec.CascadeDelete)
	}

	if _, err := h.DeleteTemplateRequirement(ctx, connect.NewRequest(&consolev1.DeleteTemplateRequirementRequest{
		Namespace: ns,
		Name:      "require-platform-config",
	})); err != nil {
		t.Fatalf("delete requirement: %v", err)
	}
	if got := listRequirementCRs(t, ctrlClient, ns); len(got) != 0 {
		t.Fatalf("requirements after delete: got %d, want 0", len(got))
	}
}

func TestCreateTemplateRequirementValidation(t *testing.T) {
	r := newTestResolver()
	folderNS := r.FolderNamespace("platform")
	projectNS := r.ProjectNamespace("checkout")

	tests := []struct {
		name        string
		namespace   string
		requirement *consolev1.TemplateRequirement
		wantCode    connect.Code
		wantMsg     string
	}{
		{
			name:        "rejects project namespace",
			namespace:   projectNS,
			requirement: basicRequirement(projectNS),
			wantCode:    connect.CodeInvalidArgument,
			wantMsg:     "cannot be stored in project namespace",
		},
		{
			name:      "requirement namespace must match request",
			namespace: folderNS,
			requirement: func() *consolev1.TemplateRequirement {
				req := basicRequirement(folderNS)
				req.Namespace = r.FolderNamespace("other")
				return req
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "must match request namespace",
		},
		{
			name:      "invalid name",
			namespace: folderNS,
			requirement: func() *consolev1.TemplateRequirement {
				req := basicRequirement(folderNS)
				req.Name = "Bad_Name"
				return req
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "valid DNS label",
		},
		{
			name:      "missing requires",
			namespace: folderNS,
			requirement: func() *consolev1.TemplateRequirement {
				req := basicRequirement(folderNS)
				req.Requires = nil
				return req
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "requires is required",
		},
		{
			name:      "rejects duplicate target ref",
			namespace: folderNS,
			requirement: func() *consolev1.TemplateRequirement {
				req := basicRequirement(folderNS)
				req.TargetRefs = append(req.TargetRefs, req.TargetRefs[0])
				return req
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "duplicate",
		},
		{
			name:      "rejects invalid target kind",
			namespace: folderNS,
			requirement: func() *consolev1.TemplateRequirement {
				req := basicRequirement(folderNS)
				req.TargetRefs[0].Kind = consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE
				return req
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "PROJECT_TEMPLATE or DEPLOYMENT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ctrlClient, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
			_, err := h.CreateTemplateRequirement(authedCtx("owner@example.com", nil), connect.NewRequest(&consolev1.CreateTemplateRequirementRequest{
				Namespace:   tt.namespace,
				Requirement: tt.requirement,
			}))
			if err == nil {
				t.Fatal("expected error")
			}
			if got := connect.CodeOf(err); got != tt.wantCode {
				t.Fatalf("code: got %v, want %v (%v)", got, tt.wantCode, err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantMsg)
			}
			if got := listRequirementCRs(t, ctrlClient, folderNS); len(got) != 0 {
				t.Fatalf("stored requirements after rejected create: got %d, want 0", len(got))
			}
		})
	}
}

func TestTemplateRequirementAccess(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		grants  map[string]string
		run     func(*Handler, string, context.Context) error
		want    connect.Code
		seedObj bool
	}{
		{
			name:   "viewer can list",
			email:  "viewer@example.com",
			grants: map[string]string{"viewer@example.com": "viewer"},
			run: func(h *Handler, ns string, ctx context.Context) error {
				_, err := h.ListTemplateRequirements(ctx, connect.NewRequest(&consolev1.ListTemplateRequirementsRequest{Namespace: ns}))
				return err
			},
		},
		{
			name:   "viewer cannot create",
			email:  "viewer@example.com",
			grants: map[string]string{"viewer@example.com": "viewer"},
			run: func(h *Handler, ns string, ctx context.Context) error {
				_, err := h.CreateTemplateRequirement(ctx, connect.NewRequest(&consolev1.CreateTemplateRequirementRequest{
					Namespace:   ns,
					Requirement: basicRequirement(ns),
				}))
				return err
			},
			want: connect.CodePermissionDenied,
		},
		{
			name:    "editor cannot delete",
			email:   "editor@example.com",
			grants:  map[string]string{"editor@example.com": "editor"},
			seedObj: true,
			run: func(h *Handler, ns string, ctx context.Context) error {
				_, err := h.DeleteTemplateRequirement(ctx, connect.NewRequest(&consolev1.DeleteTemplateRequirementRequest{
					Namespace: ns,
					Name:      "require-platform-config",
				}))
				return err
			},
			want: connect.CodePermissionDenied,
		},
		{
			name:   "missing auth",
			email:  "owner@example.com",
			grants: map[string]string{"owner@example.com": "owner"},
			run: func(h *Handler, ns string, _ context.Context) error {
				_, err := h.ListTemplateRequirements(context.Background(), connect.NewRequest(&consolev1.ListTemplateRequirementsRequest{Namespace: ns}))
				return err
			},
			want: connect.CodeUnauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []ctrlclient.Object
			ns := newTestResolver().FolderNamespace("platform")
			if tt.seedObj {
				objs = append(objs, &templatesv1alpha1.TemplateRequirement{
					ObjectMeta: metav1.ObjectMeta{Name: "require-platform-config", Namespace: ns},
					Spec: templatesv1alpha1.TemplateRequirementSpec{
						Requires: templatesv1alpha1.LinkedTemplateRef{Namespace: "holos-org-acme", Name: "shared-config"},
						TargetRefs: []templatesv1alpha1.TemplateRequirementTargetRef{
							{
								Kind:        templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
								Name:        "*",
								ProjectName: "*",
							},
						},
					},
				})
			}
			h, _, ns := newTestHandler(t, tt.grants, objs...)
			err := tt.run(h, ns, authedCtx(tt.email, nil))
			if tt.want == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if got := connect.CodeOf(err); got != tt.want {
				t.Fatalf("code: got %v, want %v (%v)", got, tt.want, err)
			}
		})
	}
}

package templatedependencies

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

type stubProjectGrantResolver struct {
	users map[string]string
	roles map[string]string
	err   error
}

func (s *stubProjectGrantResolver) GetProjectGrants(_ context.Context, _ string) (map[string]string, map[string]string, error) {
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
	projectNS := r.ProjectNamespace("checkout")

	kube := fake.NewSimpleClientset()
	ns, err := kube.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: projectNS,
			Labels: map[string]string{
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
				v1alpha2.LabelProject:      "checkout",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("seed project namespace with client-go fake: %v", err)
	}

	allObjs := append([]ctrlclient.Object{ns}, objs...)
	ctrlClient := ctrlfake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(allObjs...).
		Build()

	h := NewHandler(NewK8sClient(ctrlClient), r).
		WithProjectGrantResolver(&stubProjectGrantResolver{users: shareUsers})
	return h, ctrlClient, projectNS
}

func boolPtr(v bool) *bool {
	return &v
}

func basicDependency(namespace string) *consolev1.TemplateDependency {
	return &consolev1.TemplateDependency{
		Name:      "api-needs-gateway",
		Namespace: namespace,
		Dependent: &consolev1.LinkedTemplateRef{
			Namespace: namespace,
			Name:      "api",
		},
		Requires: &consolev1.LinkedTemplateRef{
			Namespace:         "holos-org-acme",
			Name:              "gateway",
			VersionConstraint: ">=1.0.0 <2.0.0",
		},
		CascadeDelete: boolPtr(false),
	}
}

func getDependencyCR(t *testing.T, c ctrlclient.Client, namespace, name string) *templatesv1alpha1.TemplateDependency {
	t.Helper()
	var dep templatesv1alpha1.TemplateDependency
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &dep); err != nil {
		t.Fatalf("get dependency CR %s/%s: %v", namespace, name, err)
	}
	return &dep
}

func listDependencyCRs(t *testing.T, c ctrlclient.Client, namespace string) []templatesv1alpha1.TemplateDependency {
	t.Helper()
	var list templatesv1alpha1.TemplateDependencyList
	if err := c.List(context.Background(), &list, ctrlclient.InNamespace(namespace)); err != nil {
		t.Fatalf("list dependency CRs: %v", err)
	}
	return list.Items
}

func TestTemplateDependencyCRUD(t *testing.T) {
	h, ctrlClient, ns := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	createResp, err := h.CreateTemplateDependency(ctx, connect.NewRequest(&consolev1.CreateTemplateDependencyRequest{
		Namespace:  ns,
		Dependency: basicDependency(ns),
	}))
	if err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	if got, want := createResp.Msg.GetName(), "api-needs-gateway"; got != want {
		t.Fatalf("created name: got %q, want %q", got, want)
	}

	stored := getDependencyCR(t, ctrlClient, ns, "api-needs-gateway")
	if got := stored.Annotations[v1alpha2.AnnotationCreatorEmail]; got != "owner@example.com" {
		t.Errorf("creator annotation: got %q", got)
	}
	if stored.Spec.CascadeDelete == nil || *stored.Spec.CascadeDelete {
		t.Fatalf("cascadeDelete: got %v, want false pointer", stored.Spec.CascadeDelete)
	}
	stored.Status = templatesv1alpha1.TemplateDependencyStatus{
		ObservedGeneration: 7,
		Conditions: []metav1.Condition{
			{
				Type:               templatesv1alpha1.TemplateDependencyConditionReady,
				Status:             metav1.ConditionTrue,
				Reason:             templatesv1alpha1.TemplateDependencyReasonReady,
				Message:            "ready",
				ObservedGeneration: 7,
				LastTransitionTime: metav1.NewTime(time.Unix(100, 0)),
			},
		},
	}
	if err := ctrlClient.Update(context.Background(), stored); err != nil {
		t.Fatalf("seed status through controller-runtime fake: %v", err)
	}

	listResp, err := h.ListTemplateDependencies(ctx, connect.NewRequest(&consolev1.ListTemplateDependenciesRequest{
		Namespace: ns,
	}))
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}
	if got := len(listResp.Msg.GetDependencies()); got != 1 {
		t.Fatalf("list count: got %d, want 1", got)
	}
	if got := listResp.Msg.GetDependencies()[0].GetStatus().GetConditions()[0].GetType(); got != "Ready" {
		t.Errorf("condition type: got %q, want Ready", got)
	}

	getResp, err := h.GetTemplateDependency(ctx, connect.NewRequest(&consolev1.GetTemplateDependencyRequest{
		Namespace: ns,
		Name:      "api-needs-gateway",
	}))
	if err != nil {
		t.Fatalf("get dependency: %v", err)
	}
	if got := getResp.Msg.GetDependency().GetRequires().GetVersionConstraint(); got != ">=1.0.0 <2.0.0" {
		t.Errorf("requires version constraint: got %q", got)
	}

	updated := basicDependency(ns)
	updated.Requires = &consolev1.LinkedTemplateRef{Namespace: ns, Name: "cache"}
	updated.CascadeDelete = boolPtr(true)
	if _, err := h.UpdateTemplateDependency(ctx, connect.NewRequest(&consolev1.UpdateTemplateDependencyRequest{
		Namespace:  ns,
		Dependency: updated,
	})); err != nil {
		t.Fatalf("update dependency: %v", err)
	}
	stored = getDependencyCR(t, ctrlClient, ns, "api-needs-gateway")
	if got := stored.Spec.Requires.Name; got != "cache" {
		t.Errorf("updated requires.name: got %q, want cache", got)
	}
	if stored.Spec.CascadeDelete == nil || !*stored.Spec.CascadeDelete {
		t.Fatalf("updated cascadeDelete: got %v, want true pointer", stored.Spec.CascadeDelete)
	}

	if _, err := h.DeleteTemplateDependency(ctx, connect.NewRequest(&consolev1.DeleteTemplateDependencyRequest{
		Namespace: ns,
		Name:      "api-needs-gateway",
	})); err != nil {
		t.Fatalf("delete dependency: %v", err)
	}
	if got := listDependencyCRs(t, ctrlClient, ns); len(got) != 0 {
		t.Fatalf("dependencies after delete: got %d, want 0", len(got))
	}
}

func TestCreateTemplateDependencyValidation(t *testing.T) {
	r := newTestResolver()
	projectNS := r.ProjectNamespace("checkout")
	orgNS := r.OrgNamespace("acme")

	tests := []struct {
		name      string
		namespace string
		dep       *consolev1.TemplateDependency
		wantCode  connect.Code
		wantMsg   string
	}{
		{
			name:      "rejects organization namespace",
			namespace: orgNS,
			dep:       basicDependency(orgNS),
			wantCode:  connect.CodeInvalidArgument,
			wantMsg:   "project namespace",
		},
		{
			name:      "dependency namespace must match request",
			namespace: projectNS,
			dep: func() *consolev1.TemplateDependency {
				d := basicDependency(projectNS)
				d.Namespace = r.ProjectNamespace("other")
				return d
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "must match request namespace",
		},
		{
			name:      "dependent namespace must match dependency namespace",
			namespace: projectNS,
			dep: func() *consolev1.TemplateDependency {
				d := basicDependency(projectNS)
				d.Dependent.Namespace = r.ProjectNamespace("other")
				return d
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "dependent.namespace",
		},
		{
			name:      "invalid name",
			namespace: projectNS,
			dep: func() *consolev1.TemplateDependency {
				d := basicDependency(projectNS)
				d.Name = "Bad_Name"
				return d
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "valid DNS label",
		},
		{
			name:      "missing requires",
			namespace: projectNS,
			dep: func() *consolev1.TemplateDependency {
				d := basicDependency(projectNS)
				d.Requires = nil
				return d
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "requires is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ctrlClient, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
			_, err := h.CreateTemplateDependency(authedCtx("owner@example.com", nil), connect.NewRequest(&consolev1.CreateTemplateDependencyRequest{
				Namespace:  tt.namespace,
				Dependency: tt.dep,
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
			if got := listDependencyCRs(t, ctrlClient, projectNS); len(got) != 0 {
				t.Fatalf("stored dependencies after rejected create: got %d, want 0", len(got))
			}
		})
	}
}

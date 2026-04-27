package templategrants

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

func basicGrant(namespace string) *consolev1.TemplateGrant {
	return &consolev1.TemplateGrant{
		Name:      "allow-project-alpha",
		Namespace: namespace,
		From: []*consolev1.TemplateGrantFromRef{
			{Namespace: "holos-prj-alpha"},
		},
	}
}

func getGrantCR(t *testing.T, c ctrlclient.Client, namespace, name string) *templatesv1alpha1.TemplateGrant {
	t.Helper()
	var g templatesv1alpha1.TemplateGrant
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &g); err != nil {
		t.Fatalf("get grant CR %s/%s: %v", namespace, name, err)
	}
	return &g
}

func listGrantCRs(t *testing.T, c ctrlclient.Client, namespace string) []templatesv1alpha1.TemplateGrant {
	t.Helper()
	var list templatesv1alpha1.TemplateGrantList
	if err := c.List(context.Background(), &list, ctrlclient.InNamespace(namespace)); err != nil {
		t.Fatalf("list grant CRs: %v", err)
	}
	return list.Items
}

func TestTemplateGrantCRUD(t *testing.T) {
	h, ctrlClient, ns := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
	ctx := authedCtx("owner@example.com", nil)

	createResp, err := h.CreateTemplateGrant(ctx, connect.NewRequest(&consolev1.CreateTemplateGrantRequest{
		Namespace: ns,
		Grant:     basicGrant(ns),
	}))
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if got, want := createResp.Msg.GetName(), "allow-project-alpha"; got != want {
		t.Fatalf("created name: got %q, want %q", got, want)
	}

	stored := getGrantCR(t, ctrlClient, ns, "allow-project-alpha")
	if got := stored.Labels[v1alpha2.LabelResourceType]; got != v1alpha2.ResourceTypeTemplateGrant {
		t.Errorf("resource type label: got %q", got)
	}
	if got := stored.Annotations[v1alpha2.AnnotationCreatorEmail]; got != "owner@example.com" {
		t.Errorf("creator annotation: got %q", got)
	}
	if len(stored.Spec.From) != 1 || stored.Spec.From[0].Namespace != "holos-prj-alpha" {
		t.Fatalf("from refs: got %v", stored.Spec.From)
	}

	// Seed a status for the round-trip check.
	stored.Status = templatesv1alpha1.TemplateGrantStatus{
		ObservedGeneration: 3,
		Conditions: []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				Message:            "all refs resolved",
				ObservedGeneration: 3,
				LastTransitionTime: metav1.NewTime(time.Unix(100, 0)),
			},
		},
	}
	if err := ctrlClient.Update(context.Background(), stored); err != nil {
		t.Fatalf("seed status through controller-runtime fake: %v", err)
	}

	listResp, err := h.ListTemplateGrants(ctx, connect.NewRequest(&consolev1.ListTemplateGrantsRequest{
		Namespace: ns,
	}))
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if got := len(listResp.Msg.GetGrants()); got != 1 {
		t.Fatalf("list count: got %d, want 1", got)
	}
	if got := listResp.Msg.GetGrants()[0].GetStatus().GetConditions()[0].GetType(); got != "Ready" {
		t.Errorf("condition type: got %q, want Ready", got)
	}

	getResp, err := h.GetTemplateGrant(ctx, connect.NewRequest(&consolev1.GetTemplateGrantRequest{
		Namespace: ns,
		Name:      "allow-project-alpha",
	}))
	if err != nil {
		t.Fatalf("get grant: %v", err)
	}
	if got := getResp.Msg.GetGrant().GetFrom()[0].GetNamespace(); got != "holos-prj-alpha" {
		t.Errorf("from namespace: got %q", got)
	}

	// Update: add a To ref and change the From list.
	updated := basicGrant(ns)
	updated.From = []*consolev1.TemplateGrantFromRef{
		{Namespace: "*"},
	}
	updated.To = []*consolev1.TemplateGrantToRef{
		{Namespace: ns, Name: "istio-base"},
	}
	if _, err := h.UpdateTemplateGrant(ctx, connect.NewRequest(&consolev1.UpdateTemplateGrantRequest{
		Namespace: ns,
		Grant:     updated,
	})); err != nil {
		t.Fatalf("update grant: %v", err)
	}
	stored = getGrantCR(t, ctrlClient, ns, "allow-project-alpha")
	if got := stored.Spec.From[0].Namespace; got != "*" {
		t.Errorf("updated from namespace: got %q, want *", got)
	}
	if len(stored.Spec.To) != 1 || stored.Spec.To[0].Name != "istio-base" {
		t.Errorf("updated to refs: got %v", stored.Spec.To)
	}

	if _, err := h.DeleteTemplateGrant(ctx, connect.NewRequest(&consolev1.DeleteTemplateGrantRequest{
		Namespace: ns,
		Name:      "allow-project-alpha",
	})); err != nil {
		t.Fatalf("delete grant: %v", err)
	}
	if got := listGrantCRs(t, ctrlClient, ns); len(got) != 0 {
		t.Fatalf("grants after delete: got %d, want 0", len(got))
	}
}

func TestCreateTemplateGrantValidation(t *testing.T) {
	r := newTestResolver()
	folderNS := r.FolderNamespace("platform")
	projectNS := r.ProjectNamespace("checkout")

	tests := []struct {
		name      string
		namespace string
		grant     *consolev1.TemplateGrant
		wantCode  connect.Code
		wantMsg   string
	}{
		{
			name:      "rejects project namespace",
			namespace: projectNS,
			grant:     basicGrant(projectNS),
			wantCode:  connect.CodeInvalidArgument,
			wantMsg:   "cannot be stored in project namespace",
		},
		{
			name:      "grant namespace must match request",
			namespace: folderNS,
			grant: func() *consolev1.TemplateGrant {
				g := basicGrant(folderNS)
				g.Namespace = r.FolderNamespace("other")
				return g
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "must match request namespace",
		},
		{
			name:      "invalid name",
			namespace: folderNS,
			grant: func() *consolev1.TemplateGrant {
				g := basicGrant(folderNS)
				g.Name = "Bad_Name"
				return g
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "valid DNS label",
		},
		{
			name:      "empty from list",
			namespace: folderNS,
			grant: func() *consolev1.TemplateGrant {
				g := basicGrant(folderNS)
				g.From = nil
				return g
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "at least one entry",
		},
		{
			name:      "from entry with empty namespace",
			namespace: folderNS,
			grant: func() *consolev1.TemplateGrant {
				g := basicGrant(folderNS)
				g.From = []*consolev1.TemplateGrantFromRef{{Namespace: ""}}
				return g
			}(),
			wantCode: connect.CodeInvalidArgument,
			wantMsg:  "namespace is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ctrlClient, _ := newTestHandler(t, map[string]string{"owner@example.com": "owner"})
			_, err := h.CreateTemplateGrant(authedCtx("owner@example.com", nil), connect.NewRequest(&consolev1.CreateTemplateGrantRequest{
				Namespace: tt.namespace,
				Grant:     tt.grant,
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
			if got := listGrantCRs(t, ctrlClient, folderNS); len(got) != 0 {
				t.Fatalf("stored grants after rejected create: got %d, want 0", len(got))
			}
		})
	}
}

package templatepolicybindings

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// policyGetterStub satisfies PolicyExistsGetter for the PolicyExistsAdapter
// tests. The err field drives the result; when nil and found is true the
// getter returns an empty ConfigMap.
type policyGetterStub struct {
	err   error
	found bool
}

func (p *policyGetterStub) GetPolicy(_ context.Context, _ consolev1.TemplateScope, _, _ string) (*corev1.ConfigMap, error) {
	if p.err != nil {
		return nil, p.err
	}
	if !p.found {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "missing")
	}
	return &corev1.ConfigMap{}, nil
}

// TestPolicyExistsAdapter covers the NotFound / found / other-error cases.
// NotFound must degrade to (false, nil) so the handler returns
// CodeInvalidArgument to the caller; any other error must propagate so
// the handler surfaces CodeInternal.
func TestPolicyExistsAdapter(t *testing.T) {
	tests := []struct {
		name       string
		getter     PolicyExistsGetter
		wantExists bool
		wantErr    bool
	}{
		{
			name:       "policy found",
			getter:     &policyGetterStub{found: true},
			wantExists: true,
		},
		{
			name:       "not found",
			getter:     &policyGetterStub{found: false},
			wantExists: false,
		},
		{
			name:       "probe error",
			getter:     &policyGetterStub{err: errors.New("api down")},
			wantExists: false,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewPolicyExistsAdapter(tt.getter)
			got, err := a.PolicyExists(context.Background(), consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "payments", "policy")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantExists {
				t.Errorf("exists = %v, want %v", got, tt.wantExists)
			}
		})
	}
}

// stubWalker returns a canned ancestor chain so the adapter's scan logic
// can be exercised without touching the K8s API.
type stubWalker struct {
	chain []*corev1.Namespace
	err   error
}

func (s *stubWalker) WalkAncestors(_ context.Context, _ string) ([]*corev1.Namespace, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.chain, nil
}

func TestAncestorChainAdapter(t *testing.T) {
	chain := []*corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "holos-fld-payments"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "holos-org-acme"}},
	}

	tests := []struct {
		name     string
		walker   WalkerInterface
		startNs  string
		wantNs   string
		want     bool
		wantErr  bool
		contains string
	}{
		{name: "hit at self", walker: &stubWalker{chain: chain}, startNs: "holos-fld-payments", wantNs: "holos-fld-payments", want: true},
		{name: "hit at parent", walker: &stubWalker{chain: chain}, startNs: "holos-fld-payments", wantNs: "holos-org-acme", want: true},
		{name: "miss", walker: &stubWalker{chain: chain}, startNs: "holos-fld-payments", wantNs: "holos-org-other", want: false},
		{name: "walker error", walker: &stubWalker{err: errors.New("boom")}, startNs: "holos-fld-payments", wantNs: "holos-org-acme", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAncestorChainAdapter(tt.walker)
			got, err := a.AncestorChainContains(context.Background(), tt.startNs, tt.wantNs)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectExistsAdapter(t *testing.T) {
	managedProject := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-payments-web",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
			},
		},
	}
	unmanaged := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-forged",
			Labels: map[string]string{
				// No managed-by label.
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
			},
		},
	}
	deleting := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "holos-prj-retired",
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeProject,
			},
			DeletionTimestamp: &metav1.Time{},
		},
	}

	client := fake.NewClientset(managedProject, unmanaged, deleting)
	a := NewProjectExistsAdapter(client, newTestResolver())

	tests := []struct {
		name    string
		project string
		want    bool
	}{
		{name: "managed project", project: "payments-web", want: true},
		{name: "unmanaged is ignored", project: "forged", want: false},
		{name: "deleting is ignored", project: "retired", want: false},
		{name: "absent namespace", project: "no-such-project", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.ProjectExists(context.Background(), consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER, "payments", tt.project)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

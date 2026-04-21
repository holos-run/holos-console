// handler_examples_test.go exercises the ListTemplateExamples RPC introduced
// in HOL-797. The acceptance criteria are:
//
//   - The happy path returns exactly two examples (one per embedded *.cue file).
//   - Results are sorted by name ascending so the UI can rely on a stable order.
//   - Each entry has non-empty DisplayName, Description, and CueTemplate.
package templates

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/holos-run/holos-console/console/policyresolver"
	"github.com/holos-run/holos-console/console/resolver"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// newExamplesTestHandler builds a minimal Handler suitable for
// ListTemplateExamples tests. No Kubernetes fixtures are needed because the
// RPC reads only the embedded examples package — no K8s I/O takes place.
func newExamplesTestHandler(t *testing.T) *Handler {
	t.Helper()
	r := &resolver.Resolver{
		OrganizationPrefix: "org-",
		FolderPrefix:       "fld-",
		ProjectPrefix:      "prj-",
	}
	k8s := newTestK8sClient(t, fake.NewClientset(), r)
	return NewHandler(k8s, r, &stubRenderer{}, policyresolver.NewNoopResolver())
}

// TestListTemplateExamples_HappyPath verifies that ListTemplateExamples returns
// exactly two examples (matching the two *.cue files embedded in the examples
// package), each with non-empty fields, and sorted by name ascending.
func TestListTemplateExamples_HappyPath(t *testing.T) {
	handler := newExamplesTestHandler(t)
	resp, err := handler.ListTemplateExamples(
		context.Background(),
		connect.NewRequest(&consolev1.ListTemplateExamplesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListTemplateExamples() error: %v", err)
	}

	got := resp.Msg.GetExamples()
	const wantCount = 2
	if len(got) != wantCount {
		t.Fatalf("ListTemplateExamples() returned %d examples, want %d", len(got), wantCount)
	}

	// Verify each entry has non-empty required fields.
	for i, ex := range got {
		if ex.GetName() == "" {
			t.Errorf("examples[%d].Name is empty", i)
		}
		if ex.GetDisplayName() == "" {
			t.Errorf("examples[%d].DisplayName is empty (name=%q)", i, ex.GetName())
		}
		if ex.GetDescription() == "" {
			t.Errorf("examples[%d].Description is empty (name=%q)", i, ex.GetName())
		}
		if ex.GetCueTemplate() == "" {
			t.Errorf("examples[%d].CueTemplate is empty (name=%q)", i, ex.GetName())
		}
	}
}

// TestListTemplateExamples_SortedByName verifies that results are sorted by
// name ascending so the UI can rely on a stable order without re-sorting.
func TestListTemplateExamples_SortedByName(t *testing.T) {
	handler := newExamplesTestHandler(t)
	resp, err := handler.ListTemplateExamples(
		context.Background(),
		connect.NewRequest(&consolev1.ListTemplateExamplesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListTemplateExamples() error: %v", err)
	}

	got := resp.Msg.GetExamples()
	for i := 1; i < len(got); i++ {
		if got[i].GetName() < got[i-1].GetName() {
			t.Errorf(
				"examples not sorted by name: examples[%d].Name=%q < examples[%d].Name=%q",
				i, got[i].GetName(), i-1, got[i-1].GetName(),
			)
		}
	}
}

// TestListTemplateExamples_KnownNames verifies the two expected built-in
// example slugs are present, anchoring the test to the actual embedded files.
func TestListTemplateExamples_KnownNames(t *testing.T) {
	handler := newExamplesTestHandler(t)
	resp, err := handler.ListTemplateExamples(
		context.Background(),
		connect.NewRequest(&consolev1.ListTemplateExamplesRequest{}),
	)
	if err != nil {
		t.Fatalf("ListTemplateExamples() error: %v", err)
	}

	byName := make(map[string]*consolev1.TemplateExample, len(resp.Msg.GetExamples()))
	for _, ex := range resp.Msg.GetExamples() {
		byName[ex.GetName()] = ex
	}

	wantNames := []string{"allowed-project-resource-kinds-v1", "httproute-v1"}
	for _, name := range wantNames {
		if _, ok := byName[name]; !ok {
			t.Errorf("example %q not found in ListTemplateExamples response", name)
		}
	}
}

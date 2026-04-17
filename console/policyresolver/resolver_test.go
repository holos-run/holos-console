package policyresolver

import (
	"context"
	"testing"

	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TestNoopResolver_ReturnsInputsUnchanged asserts the Phase 4 invariant that
// every render path is behaviorally unchanged after the PolicyResolver seam
// is introduced: the noopResolver must return the caller's explicit refs
// verbatim regardless of target kind, project namespace, or target name.
func TestNoopResolver_ReturnsInputsUnchanged(t *testing.T) {
	orgRef := &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: "acme",
		Name:      "httproute",
	}
	folderRef := &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_FOLDER,
		ScopeName: "payments",
		Name:      "audit-policy",
	}

	tests := []struct {
		name         string
		projectNs    string
		targetKind   TargetKind
		targetName   string
		explicitRefs []*consolev1.LinkedTemplateRef
	}{
		{
			name:         "deployment with multiple refs",
			projectNs:    "prj-orders",
			targetKind:   TargetKindDeployment,
			targetName:   "api",
			explicitRefs: []*consolev1.LinkedTemplateRef{orgRef, folderRef},
		},
		{
			name:         "project template with single ref",
			projectNs:    "prj-orders",
			targetKind:   TargetKindProjectTemplate,
			targetName:   "audit-policy",
			explicitRefs: []*consolev1.LinkedTemplateRef{folderRef},
		},
		{
			name:         "deployment with nil refs",
			projectNs:    "prj-orders",
			targetKind:   TargetKindDeployment,
			targetName:   "api",
			explicitRefs: nil,
		},
		{
			name:         "project template with empty refs",
			projectNs:    "prj-orders",
			targetKind:   TargetKindProjectTemplate,
			targetName:   "audit-policy",
			explicitRefs: []*consolev1.LinkedTemplateRef{},
		},
	}

	resolver := NewNoopResolver()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolver.Resolve(context.Background(), tc.projectNs, tc.targetKind, tc.targetName, tc.explicitRefs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.explicitRefs) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tc.explicitRefs))
			}
			for i, ref := range tc.explicitRefs {
				if got[i] != ref {
					t.Errorf("ref %d: got %+v, want %+v (pointer equality)", i, got[i], ref)
				}
			}
		})
	}
}

// TestNoopResolver_DoesNotMutateInput guards against a future edit that
// starts mutating the input slice. The contract is clear: return a new or
// aliasing slice, but never modify the caller's.
func TestNoopResolver_DoesNotMutateInput(t *testing.T) {
	orgRef := &consolev1.LinkedTemplateRef{
		Scope:     consolev1.TemplateScope_TEMPLATE_SCOPE_ORGANIZATION,
		ScopeName: "acme",
		Name:      "httproute",
	}
	input := []*consolev1.LinkedTemplateRef{orgRef}
	original := make([]*consolev1.LinkedTemplateRef, len(input))
	copy(original, input)

	resolver := NewNoopResolver()
	if _, err := resolver.Resolve(context.Background(), "prj-orders", TargetKindDeployment, "api", input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(input) != len(original) {
		t.Fatalf("input slice length changed: got %d, want %d", len(input), len(original))
	}
	for i := range input {
		if input[i] != original[i] {
			t.Errorf("input[%d] changed: got %p, want %p", i, input[i], original[i])
		}
	}
}

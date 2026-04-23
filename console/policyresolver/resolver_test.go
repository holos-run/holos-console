package policyresolver

import (
	"context"
	"testing"
)

// TestNoopResolver_ReturnsEmptySet asserts that the noopResolver always returns
// an empty effective set regardless of target kind, project namespace, or target
// name. It is the placeholder wired when no real TemplatePolicy-backed resolver
// is available.
func TestNoopResolver_ReturnsEmptySet(t *testing.T) {
	tests := []struct {
		name       string
		projectNs  string
		targetKind TargetKind
		targetName string
	}{
		{
			name:       "deployment",
			projectNs:  "prj-orders",
			targetKind: TargetKindDeployment,
			targetName: "api",
		},
		{
			name:       "project template",
			projectNs:  "prj-orders",
			targetKind: TargetKindProjectTemplate,
			targetName: "audit-policy",
		},
	}

	r := NewNoopResolver()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Resolve(context.Background(), tc.projectNs, tc.targetKind, tc.targetName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("noopResolver must return empty set; got %d refs", len(got))
			}
		})
	}
}

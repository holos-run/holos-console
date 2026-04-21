// target_kind_roundtrip_test.go verifies that every TemplatePolicyBindingTargetKind
// round-trips faithfully through both conversion directions:
//
//  1. proto → CRD  (targetKindProtoToCRD)
//  2. CRD  → proto (targetKindCRDToProto)
//
// This file covers the HOL-808 acceptance criterion requiring a unit test for
// the new ProjectNamespace kind and confirms the existing values are unaffected.
package templatepolicybindings

import (
	"testing"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TestTargetKindRoundTrip_ProtoToCRD asserts that every proto enum value
// converts to the expected CRD string constant.
func TestTargetKindRoundTrip_ProtoToCRD(t *testing.T) {
	cases := []struct {
		name  string
		input consolev1.TemplatePolicyBindingTargetKind
		want  templatesv1alpha1.TemplatePolicyBindingTargetKind
	}{
		{
			name:  "project template",
			input: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
			want:  templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
		},
		{
			name:  "deployment",
			input: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
			want:  templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		},
		{
			name:  "project namespace (HOL-808)",
			input: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE,
			want:  templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace,
		},
		{
			name:  "unspecified maps to empty string",
			input: consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED,
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := targetKindProtoToCRD(tc.input)
			if got != tc.want {
				t.Errorf("targetKindProtoToCRD(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestTargetKindRoundTrip_CRDToProto asserts that every CRD string constant
// converts to the expected proto enum value.
func TestTargetKindRoundTrip_CRDToProto(t *testing.T) {
	cases := []struct {
		name  string
		input templatesv1alpha1.TemplatePolicyBindingTargetKind
		want  consolev1.TemplatePolicyBindingTargetKind
	}{
		{
			name:  "project template",
			input: templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
			want:  consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_TEMPLATE,
		},
		{
			name:  "deployment",
			input: templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
			want:  consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_DEPLOYMENT,
		},
		{
			name:  "project namespace (HOL-808)",
			input: templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace,
			want:  consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_PROJECT_NAMESPACE,
		},
		{
			name:  "unknown string maps to unspecified",
			input: templatesv1alpha1.TemplatePolicyBindingTargetKind("UnknownFuture"),
			want:  consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := targetKindCRDToProto(tc.input)
			if got != tc.want {
				t.Errorf("targetKindCRDToProto(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestTargetKindRoundTrip_Symmetry asserts the two functions are strict
// inverses for all named enum values. This catches any divergence where
// one side is updated without the other.
func TestTargetKindRoundTrip_Symmetry(t *testing.T) {
	crdKinds := []templatesv1alpha1.TemplatePolicyBindingTargetKind{
		templatesv1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
		templatesv1alpha1.TemplatePolicyBindingTargetKindDeployment,
		templatesv1alpha1.TemplatePolicyBindingTargetKindProjectNamespace,
	}
	for _, k := range crdKinds {
		t.Run(string(k), func(t *testing.T) {
			// CRD → proto → CRD must be identity.
			proto := targetKindCRDToProto(k)
			if proto == consolev1.TemplatePolicyBindingTargetKind_TEMPLATE_POLICY_BINDING_TARGET_KIND_UNSPECIFIED {
				t.Errorf("targetKindCRDToProto(%q) returned UNSPECIFIED; missing case?", k)
			}
			roundTripped := targetKindProtoToCRD(proto)
			if roundTripped != k {
				t.Errorf("round-trip failed: CRD(%q) → proto(%v) → CRD(%q)", k, proto, roundTripped)
			}
		})
	}
}

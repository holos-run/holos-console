/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1_test

import (
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// TestTemplateRequirement_RoundTrip exercises JSON and YAML round-trip
// marshal/unmarshal for TemplateRequirement and confirms that key spec fields
// survive without loss.
func TestTemplateRequirement_RoundTrip(t *testing.T) {
	boolTrue := true
	boolFalse := false
	ready := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "all refs resolved",
		LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		ObservedGeneration: 5,
	}

	tests := []struct {
		name string
		obj  v1alpha1.TemplateRequirement
	}{
		{
			name: "minimal",
			obj: v1alpha1.TemplateRequirement{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateRequirement",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "require-istio",
					Namespace: "holos-prj-alpha",
				},
				Spec: v1alpha1.TemplateRequirementSpec{
					Requires: v1alpha1.LinkedTemplateRef{
						Namespace: "holos-org-acme",
						Name:      "istio-base",
					},
					TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							Name:        "web",
							ProjectName: "alpha",
						},
					},
				},
			},
		},
		{
			name: "wildcard-project",
			obj: v1alpha1.TemplateRequirement{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateRequirement",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "require-everywhere",
					Namespace: "holos-fld-platform",
				},
				Spec: v1alpha1.TemplateRequirementSpec{
					Requires: v1alpha1.LinkedTemplateRef{
						Namespace: "holos-org-acme",
						Name:      "cert-manager",
					},
					TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							Name:        "*",
							ProjectName: "*",
						},
					},
					CascadeDelete: &boolTrue,
				},
			},
		},
		{
			name: "cascade-delete-false",
			obj: v1alpha1.TemplateRequirement{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateRequirement",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "soft-require",
					Namespace: "holos-prj-beta",
				},
				Spec: v1alpha1.TemplateRequirementSpec{
					Requires: v1alpha1.LinkedTemplateRef{
						Namespace:         "holos-org-acme",
						Name:              "optional-helper",
						VersionConstraint: ">=1.0.0 <2.0.0",
					},
					TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
							Name:        "base",
							ProjectName: "beta",
						},
					},
					CascadeDelete: &boolFalse,
				},
			},
		},
		{
			name: "multiple-target-refs",
			obj: v1alpha1.TemplateRequirement{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateRequirement",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "multi-target",
					Namespace:  "holos-fld-eng",
					Generation: 5,
				},
				Spec: v1alpha1.TemplateRequirementSpec{
					Requires: v1alpha1.LinkedTemplateRef{
						Namespace: "holos-org-acme",
						Name:      "istio-base",
					},
					TargetRefs: []v1alpha1.TemplateRequirementTargetRef{
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							Name:        "web",
							ProjectName: "alpha",
						},
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindDeployment,
							Name:        "api",
							ProjectName: "alpha",
						},
						{
							Kind:        v1alpha1.TemplatePolicyBindingTargetKindProjectTemplate,
							Name:        "infra-base",
							ProjectName: "platform",
						},
					},
					CascadeDelete: &boolTrue,
				},
				Status: v1alpha1.TemplateRequirementStatus{
					ObservedGeneration: 5,
					Conditions:         []metav1.Condition{ready},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// JSON round-trip.
			jsonBytes, err := json.Marshal(tc.obj)
			if err != nil {
				t.Fatalf("json marshal: %v", err)
			}
			var fromJSON v1alpha1.TemplateRequirement
			if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			assertTemplateRequirementEqual(t, tc.obj, fromJSON)

			// YAML round-trip.
			yamlBytes, err := yaml.Marshal(tc.obj)
			if err != nil {
				t.Fatalf("yaml marshal: %v", err)
			}
			var fromYAML v1alpha1.TemplateRequirement
			if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
				t.Fatalf("yaml unmarshal: %v", err)
			}
			assertTemplateRequirementEqual(t, tc.obj, fromYAML)
		})
	}
}

func assertTemplateRequirementEqual(t *testing.T, want, got v1alpha1.TemplateRequirement) {
	t.Helper()
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("ObjectMeta name/namespace: got (%s/%s) want (%s/%s)",
			got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if got.Spec.Requires != want.Spec.Requires {
		t.Errorf("Spec.Requires: got %+v want %+v", got.Spec.Requires, want.Spec.Requires)
	}
	if len(got.Spec.TargetRefs) != len(want.Spec.TargetRefs) {
		t.Fatalf("Spec.TargetRefs length: got %d want %d", len(got.Spec.TargetRefs), len(want.Spec.TargetRefs))
	}
	for i := range want.Spec.TargetRefs {
		if got.Spec.TargetRefs[i] != want.Spec.TargetRefs[i] {
			t.Errorf("Spec.TargetRefs[%d]: got %+v want %+v", i, got.Spec.TargetRefs[i], want.Spec.TargetRefs[i])
		}
	}
	// CascadeDelete pointer comparison.
	switch {
	case want.Spec.CascadeDelete == nil && got.Spec.CascadeDelete != nil:
		t.Errorf("Spec.CascadeDelete: want nil got %v", *got.Spec.CascadeDelete)
	case want.Spec.CascadeDelete != nil && got.Spec.CascadeDelete == nil:
		t.Errorf("Spec.CascadeDelete: want %v got nil", *want.Spec.CascadeDelete)
	case want.Spec.CascadeDelete != nil && got.Spec.CascadeDelete != nil:
		if *got.Spec.CascadeDelete != *want.Spec.CascadeDelete {
			t.Errorf("Spec.CascadeDelete: got %v want %v", *got.Spec.CascadeDelete, *want.Spec.CascadeDelete)
		}
	}
	if got.Status.ObservedGeneration != want.Status.ObservedGeneration {
		t.Errorf("Status.ObservedGeneration: got %d want %d",
			got.Status.ObservedGeneration, want.Status.ObservedGeneration)
	}
}

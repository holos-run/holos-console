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

// TestTemplateGrant_RoundTrip exercises JSON and YAML round-trip
// marshal/unmarshal for TemplateGrant and confirms that key spec fields
// survive without loss.
func TestTemplateGrant_RoundTrip(t *testing.T) {
	ready := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "all refs resolved",
		LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		ObservedGeneration: 3,
	}

	tests := []struct {
		name string
		obj  v1alpha1.TemplateGrant
	}{
		{
			name: "minimal",
			obj: v1alpha1.TemplateGrant{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateGrant",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "allow-project-alpha",
					Namespace: "holos-org-acme",
				},
				Spec: v1alpha1.TemplateGrantSpec{
					From: []v1alpha1.TemplateGrantFromRef{
						{Namespace: "holos-prj-alpha"},
					},
				},
			},
		},
		{
			name: "wildcard-from",
			obj: v1alpha1.TemplateGrant{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateGrant",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "allow-all",
					Namespace: "holos-org-acme",
				},
				Spec: v1alpha1.TemplateGrantSpec{
					From: []v1alpha1.TemplateGrantFromRef{
						{Namespace: "*"},
					},
					To: []v1alpha1.LinkedTemplateRef{
						{Namespace: "holos-org-acme", Name: "istio-base"},
					},
				},
			},
		},
		{
			name: "with-namespace-selector",
			obj: v1alpha1.TemplateGrant{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateGrant",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "allow-labeled",
					Namespace: "holos-fld-platform",
				},
				Spec: v1alpha1.TemplateGrantSpec{
					From: []v1alpha1.TemplateGrantFromRef{
						{
							Namespace: "holos-prj-edge",
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"console.holos.run/resource-type": "project",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "full-with-status",
			obj: v1alpha1.TemplateGrant{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "TemplateGrant",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "full",
					Namespace:  "holos-org-acme",
					Generation: 3,
				},
				Spec: v1alpha1.TemplateGrantSpec{
					From: []v1alpha1.TemplateGrantFromRef{
						{Namespace: "holos-prj-web"},
						{Namespace: "holos-prj-api"},
					},
					To: []v1alpha1.LinkedTemplateRef{
						{Namespace: "holos-org-acme", Name: "istio-base"},
						{Namespace: "holos-org-acme", Name: "cert-manager"},
					},
				},
				Status: v1alpha1.TemplateGrantStatus{
					ObservedGeneration: 3,
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
			var fromJSON v1alpha1.TemplateGrant
			if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			assertTemplateGrantEqual(t, tc.obj, fromJSON)

			// YAML round-trip.
			yamlBytes, err := yaml.Marshal(tc.obj)
			if err != nil {
				t.Fatalf("yaml marshal: %v", err)
			}
			var fromYAML v1alpha1.TemplateGrant
			if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
				t.Fatalf("yaml unmarshal: %v", err)
			}
			assertTemplateGrantEqual(t, tc.obj, fromYAML)
		})
	}
}

func assertTemplateGrantEqual(t *testing.T, want, got v1alpha1.TemplateGrant) {
	t.Helper()
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("ObjectMeta name/namespace: got (%s/%s) want (%s/%s)",
			got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if len(got.Spec.From) != len(want.Spec.From) {
		t.Fatalf("Spec.From length: got %d want %d", len(got.Spec.From), len(want.Spec.From))
	}
	for i := range want.Spec.From {
		if got.Spec.From[i].Namespace != want.Spec.From[i].Namespace {
			t.Errorf("Spec.From[%d].Namespace: got %q want %q", i, got.Spec.From[i].Namespace, want.Spec.From[i].Namespace)
		}
	}
	if len(got.Spec.To) != len(want.Spec.To) {
		t.Errorf("Spec.To length: got %d want %d", len(got.Spec.To), len(want.Spec.To))
	}
	if got.Status.ObservedGeneration != want.Status.ObservedGeneration {
		t.Errorf("Status.ObservedGeneration: got %d want %d",
			got.Status.ObservedGeneration, want.Status.ObservedGeneration)
	}
}

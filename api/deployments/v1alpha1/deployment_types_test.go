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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
)

// TestDeployment_RoundTrip exercises JSON and YAML round-trip
// marshal/unmarshal for Deployment and confirms that key spec fields survive
// without loss. The Deployment CRD captures the existing proto-defined shape
// (proto/holos/console/v1/deployments.proto) so this test covers the minimal
// fields required for the dual-write transition (Phase 3, HOL-957).
func TestDeployment_RoundTrip(t *testing.T) {
	ready := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "deployment synced",
		LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		ObservedGeneration: 4,
	}

	tests := []struct {
		name string
		obj  v1alpha1.Deployment
	}{
		{
			name: "minimal",
			obj: v1alpha1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "web",
					Namespace: "holos-prj-alpha",
				},
				Spec: v1alpha1.DeploymentSpec{
					ProjectName: "alpha",
					TemplateRef: v1alpha1.DeploymentTemplateRef{
						Namespace: "holos-prj-alpha",
						Name:      "httpbin",
					},
				},
			},
		},
		{
			name: "with-version-constraint",
			obj: v1alpha1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "api-v2",
					Namespace: "holos-prj-beta",
				},
				Spec: v1alpha1.DeploymentSpec{
					ProjectName: "beta",
					TemplateRef: v1alpha1.DeploymentTemplateRef{
						Namespace: "holos-org-acme",
						Name:      "restapi",
					},
					VersionConstraint: ">=2.0.0 <3.0.0",
					DisplayName:       "API v2",
					Description:       "REST API deployment pinned to v2.",
				},
			},
		},
		{
			name: "with-image-and-env",
			obj: v1alpha1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backend",
					Namespace: "holos-prj-gamma",
				},
				Spec: v1alpha1.DeploymentSpec{
					ProjectName: "gamma",
					TemplateRef: v1alpha1.DeploymentTemplateRef{
						Namespace: "holos-prj-gamma",
						Name:      "worker",
					},
					Image:   "ghcr.io/example/backend",
					Tag:     "sha-abc123",
					Port:    8080,
					Command: []string{"/app/server"},
					Args:    []string{"--port=8080", "--log-level=info"},
				},
			},
		},
		{
			name: "full-with-status",
			obj: v1alpha1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "full",
					Namespace:  "holos-prj-delta",
					Generation: 4,
				},
				Spec: v1alpha1.DeploymentSpec{
					ProjectName: "delta",
					TemplateRef: v1alpha1.DeploymentTemplateRef{
						Namespace: "holos-org-delta",
						Name:      "microservice",
					},
					VersionConstraint: ">=1.0.0 <2.0.0",
					DisplayName:       "Full deployment",
					Description:       "Full spec round-trip test.",
					Image:             "ghcr.io/example/svc",
					Tag:               "v1.5.2",
					Port:              9090,
					Command:           []string{"/svc"},
					Args:              []string{"--config=/etc/svc.yaml"},
				},
				Status: v1alpha1.DeploymentStatus{
					ObservedGeneration: 4,
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
			var fromJSON v1alpha1.Deployment
			if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			assertDeploymentEqual(t, tc.obj, fromJSON)

			// YAML round-trip.
			yamlBytes, err := yaml.Marshal(tc.obj)
			if err != nil {
				t.Fatalf("yaml marshal: %v", err)
			}
			var fromYAML v1alpha1.Deployment
			if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
				t.Fatalf("yaml unmarshal: %v", err)
			}
			assertDeploymentEqual(t, tc.obj, fromYAML)
		})
	}
}

func assertDeploymentEqual(t *testing.T, want, got v1alpha1.Deployment) {
	t.Helper()
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("ObjectMeta name/namespace: got (%s/%s) want (%s/%s)",
			got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if got.Spec.ProjectName != want.Spec.ProjectName {
		t.Errorf("Spec.ProjectName: got %q want %q", got.Spec.ProjectName, want.Spec.ProjectName)
	}
	if got.Spec.TemplateRef != want.Spec.TemplateRef {
		t.Errorf("Spec.TemplateRef: got %+v want %+v", got.Spec.TemplateRef, want.Spec.TemplateRef)
	}
	if got.Spec.VersionConstraint != want.Spec.VersionConstraint {
		t.Errorf("Spec.VersionConstraint: got %q want %q", got.Spec.VersionConstraint, want.Spec.VersionConstraint)
	}
	if got.Spec.Image != want.Spec.Image {
		t.Errorf("Spec.Image: got %q want %q", got.Spec.Image, want.Spec.Image)
	}
	if got.Spec.Tag != want.Spec.Tag {
		t.Errorf("Spec.Tag: got %q want %q", got.Spec.Tag, want.Spec.Tag)
	}
	if got.Spec.Port != want.Spec.Port {
		t.Errorf("Spec.Port: got %d want %d", got.Spec.Port, want.Spec.Port)
	}
	if len(got.Spec.Command) != len(want.Spec.Command) {
		t.Errorf("Spec.Command length: got %d want %d", len(got.Spec.Command), len(want.Spec.Command))
	}
	if len(got.Spec.Args) != len(want.Spec.Args) {
		t.Errorf("Spec.Args length: got %d want %d", len(got.Spec.Args), len(want.Spec.Args))
	}
	if got.Status.ObservedGeneration != want.Status.ObservedGeneration {
		t.Errorf("Status.ObservedGeneration: got %d want %d",
			got.Status.ObservedGeneration, want.Status.ObservedGeneration)
	}
}

func TestDeploymentConditionConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "accepted", got: v1alpha1.ConditionTypeAccepted, want: "Accepted"},
		{name: "resolved-refs", got: v1alpha1.ConditionTypeResolvedRefs, want: "ResolvedRefs"},
		{name: "rendered", got: v1alpha1.ConditionTypeRendered, want: "Rendered"},
		{name: "applied", got: v1alpha1.ConditionTypeApplied, want: "Applied"},
		{name: "ready", got: v1alpha1.ConditionTypeReady, want: "Ready"},
		{name: "deployment-render-succeeded", got: v1alpha1.DeploymentReasonRenderSucceeded, want: "RenderSucceeded"},
		{name: "deployment-render-failed", got: v1alpha1.DeploymentReasonRenderFailed, want: "RenderFailed"},
		{name: "deployment-ancestor-template-missing", got: v1alpha1.DeploymentReasonAncestorTemplateMissing, want: "AncestorTemplateMissing"},
		{name: "deployment-apply-succeeded", got: v1alpha1.DeploymentReasonApplySucceeded, want: "ApplySucceeded"},
		{name: "deployment-apply-failed", got: v1alpha1.DeploymentReasonApplyFailed, want: "ApplyFailed"},
		{name: "render-succeeded", got: v1alpha1.ReasonRenderSucceeded, want: "RenderSucceeded"},
		{name: "render-failed", got: v1alpha1.ReasonRenderFailed, want: "RenderFailed"},
		{name: "ancestor-template-missing", got: v1alpha1.ReasonAncestorTemplateMissing, want: "AncestorTemplateMissing"},
		{name: "apply-succeeded", got: v1alpha1.ReasonApplySucceeded, want: "ApplySucceeded"},
		{name: "apply-failed", got: v1alpha1.ReasonApplyFailed, want: "ApplyFailed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("constant value = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestDeploymentConditions_RoundTripThroughMeta(t *testing.T) {
	tests := []struct {
		name      string
		condType  string
		status    metav1.ConditionStatus
		reason    string
		message   string
		wantCount int
	}{
		{
			name:      "rendered-success",
			condType:  v1alpha1.ConditionTypeRendered,
			status:    metav1.ConditionTrue,
			reason:    v1alpha1.ReasonRenderSucceeded,
			message:   "rendered Kubernetes object set",
			wantCount: 1,
		},
		{
			name:      "rendered-failed-missing-ancestor-template",
			condType:  v1alpha1.ConditionTypeRendered,
			status:    metav1.ConditionFalse,
			reason:    v1alpha1.ReasonAncestorTemplateMissing,
			message:   "ancestor template was not found",
			wantCount: 1,
		},
		{
			name:      "applied-success",
			condType:  v1alpha1.ConditionTypeApplied,
			status:    metav1.ConditionTrue,
			reason:    v1alpha1.ReasonApplySucceeded,
			message:   "applied rendered Kubernetes object set",
			wantCount: 1,
		},
		{
			name:      "applied-failed",
			condType:  v1alpha1.ConditionTypeApplied,
			status:    metav1.ConditionFalse,
			reason:    v1alpha1.ReasonApplyFailed,
			message:   "server-side apply failed",
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := v1alpha1.DeploymentStatus{}
			condition := metav1.Condition{
				Type:               tc.condType,
				Status:             tc.status,
				Reason:             tc.reason,
				Message:            tc.message,
				ObservedGeneration: 7,
				LastTransitionTime: metav1.NewTime(time.Date(2026, 4, 28, 1, 2, 3, 0, time.UTC)),
			}

			meta.SetStatusCondition(&status.Conditions, condition)
			got := meta.FindStatusCondition(status.Conditions, tc.condType)
			if got == nil {
				t.Fatalf("condition %q was not found", tc.condType)
			}
			if got.Type != tc.condType {
				t.Errorf("Type = %q, want %q", got.Type, tc.condType)
			}
			if got.Status != tc.status {
				t.Errorf("Status = %q, want %q", got.Status, tc.status)
			}
			if got.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.reason)
			}
			if got.Message != tc.message {
				t.Errorf("Message = %q, want %q", got.Message, tc.message)
			}
			if got.ObservedGeneration != 7 {
				t.Errorf("ObservedGeneration = %d, want 7", got.ObservedGeneration)
			}
			if len(status.Conditions) != tc.wantCount {
				t.Errorf("condition count = %d, want %d", len(status.Conditions), tc.wantCount)
			}
		})
	}
}

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
	"regexp"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// TestUpstreamSecret_RoundTrip exercises two contracts at once:
//  1. Every required spec/status field survives a YAML and a JSON round-trip
//     (marshal + unmarshal produces a value that equals the input at every
//     required leaf).
//  2. The marshaled form carries no substrings that could represent upstream
//     credential bytes or any of the forbidden hash-related artifacts named
//     in doc.go's "no sensitive values on CRs" invariant. This is a cheap,
//     high-signal guardrail so a future field addition that accidentally
//     leaks a credential prefix or pepper version string is caught by unit
//     tests rather than by production audit.
func TestUpstreamSecret_RoundTrip(t *testing.T) {
	ready := metav1.Condition{
		Type:               v1alpha1.UpstreamSecretConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             v1alpha1.UpstreamSecretReasonReady,
		Message:            "all references resolved",
		LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		ObservedGeneration: 7,
	}

	tests := []struct {
		name string
		obj  v1alpha1.UpstreamSecret
	}{
		{
			name: "minimal",
			obj: v1alpha1.UpstreamSecret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "UpstreamSecret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minimal",
					Namespace: "holos-prj",
				},
				Spec: v1alpha1.UpstreamSecretSpec{
					SecretRef: v1alpha1.SecretKeyReference{
						Name: "upstream-api-key",
						Key:  "apiKey",
					},
					Upstream: v1alpha1.Upstream{
						Host:   "api.internal",
						Scheme: "https",
					},
					Injection: v1alpha1.Injection{
						Header: "X-Api-Key",
					},
				},
			},
		},
		{
			name: "full",
			obj: v1alpha1.UpstreamSecret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "UpstreamSecret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "full",
					Namespace:  "holos-prj",
					Generation: 7,
				},
				Spec: v1alpha1.UpstreamSecretSpec{
					SecretRef: v1alpha1.SecretKeyReference{
						Name: "vendor-token",
						Key:  "token",
					},
					Upstream: v1alpha1.Upstream{
						Host:       "api.vendor",
						Scheme:     "https",
						Port:       8443,
						PathPrefix: "/v2/",
					},
					Injection: v1alpha1.Injection{
						Header:        "Authorization",
						ValueTemplate: "Bearer {{.Value}}",
					},
				},
				Status: v1alpha1.UpstreamSecretStatus{
					ObservedGeneration: 7,
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
			var fromJSON v1alpha1.UpstreamSecret
			if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
				t.Fatalf("json unmarshal: %v", err)
			}
			assertSpecEqual(t, tc.obj, fromJSON)

			// YAML round-trip (sigs.k8s.io/yaml routes through the same
			// JSON tags so kubernetes struct tags are honoured).
			yamlBytes, err := yaml.Marshal(tc.obj)
			if err != nil {
				t.Fatalf("yaml marshal: %v", err)
			}
			var fromYAML v1alpha1.UpstreamSecret
			if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
				t.Fatalf("yaml unmarshal: %v", err)
			}
			assertSpecEqual(t, tc.obj, fromYAML)

			// Invariant: the marshaled form MUST NOT contain any
			// substring that could represent a credential byte, a hash
			// output, or a pepper-version leak.
			assertNoSensitiveMaterial(t, "json", string(jsonBytes))
			assertNoSensitiveMaterial(t, "yaml", string(yamlBytes))
		})
	}
}

// assertSpecEqual compares the fields that matter for the CR contract. We
// avoid reflect.DeepEqual on the whole struct because metav1.Time has
// nanosecond-precision fields that are normalised by JSON/YAML round trips
// differently than plain field-by-field comparisons.
func assertSpecEqual(t *testing.T, want, got v1alpha1.UpstreamSecret) {
	t.Helper()
	if got.APIVersion != want.APIVersion || got.Kind != want.Kind {
		t.Errorf("TypeMeta: got %+v want %+v", got.TypeMeta, want.TypeMeta)
	}
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("ObjectMeta name/namespace: got (%s/%s) want (%s/%s)",
			got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if got.Spec.SecretRef != want.Spec.SecretRef {
		t.Errorf("Spec.SecretRef: got %+v want %+v", got.Spec.SecretRef, want.Spec.SecretRef)
	}
	if got.Spec.Upstream != want.Spec.Upstream {
		t.Errorf("Spec.Upstream: got %+v want %+v", got.Spec.Upstream, want.Spec.Upstream)
	}
	if got.Spec.Injection != want.Spec.Injection {
		t.Errorf("Spec.Injection: got %+v want %+v", got.Spec.Injection, want.Spec.Injection)
	}
	if got.Status.ObservedGeneration != want.Status.ObservedGeneration {
		t.Errorf("Status.ObservedGeneration: got %d want %d",
			got.Status.ObservedGeneration, want.Status.ObservedGeneration)
	}
	if len(got.Status.Conditions) != len(want.Status.Conditions) {
		t.Fatalf("Status.Conditions length: got %d want %d",
			len(got.Status.Conditions), len(want.Status.Conditions))
	}
	for i := range want.Status.Conditions {
		w := want.Status.Conditions[i]
		g := got.Status.Conditions[i]
		if g.Type != w.Type || g.Status != w.Status || g.Reason != w.Reason ||
			g.Message != w.Message || g.ObservedGeneration != w.ObservedGeneration {
			t.Errorf("Status.Conditions[%d]: got %+v want %+v", i, g, w)
		}
	}
}

// assertNoSensitiveMaterial implements the doc.go invariant as a unit-test
// guardrail. It checks for:
//   - The holos-issued API-key prefix ("sih_") that would only appear if
//     plaintext leaked onto the CR.
//   - Variants of "pepper" used by the Credential hash scheme; no CR in this
//     group carries pepper material, so any appearance is a regression.
//   - "argon2id" hash tags, which also never appear on a CR.
//   - A long-base64 heuristic: any base64-ish run of 17+ chars outside an
//     allow-listed set of reference fields would signal a credential byte
//     slipped into the marshaled form. We only check outside the allow list
//     so a deliberately-named reference field ("secretRef.name: apiKey")
//     stays clean.
func assertNoSensitiveMaterial(t *testing.T, format, serialized string) {
	t.Helper()

	forbidden := []string{"sih_", "pepper", "argon2id"}
	lower := strings.ToLower(serialized)
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			t.Errorf("[%s] serialized form contains forbidden substring %q: %s",
				format, f, serialized)
		}
	}

	// Long-base64 heuristic. We scan for runs of 17+ base64-safe chars
	// and fail if any run is not part of an allow-listed sibling field.
	// The allow list covers opaque Kubernetes UIDs (we don't set one here
	// but envtest does), resourceVersion strings, and uid values that
	// upstream Kubernetes may append on round-trips. Any surprise match
	// is a regression we want to see.
	longBase64 := regexp.MustCompile(`[A-Za-z0-9+/=_-]{17,}`)
	for _, hit := range longBase64.FindAllString(serialized, -1) {
		if isAllowedLongToken(hit) {
			continue
		}
		t.Errorf("[%s] serialized form contains long base64-like run %q; "+
			"credential bytes MUST NOT appear on an UpstreamSecret CR",
			format, hit)
	}
}

// isAllowedLongToken returns true for known-safe long runs that legitimate
// serialized UpstreamSecret objects may contain. Keep the list tight — every
// new entry widens the guardrail's blind spot.
func isAllowedLongToken(tok string) bool {
	allow := []string{
		// Package path appears in the kubebuilder-generated
		// description blocks.
		"api/secrets/v1alpha1",
		// API group string.
		"secrets.holos.run",
		// Kubebuilder description substrings that run long.
		"observedGeneration",
		"lastTransitionTime",
		"creationTimestamp",
		// TypeMeta.APIVersion string.
		"secrets.holos.run/v1alpha1",
	}
	for _, a := range allow {
		if strings.Contains(tok, a) {
			return true
		}
	}
	return false
}

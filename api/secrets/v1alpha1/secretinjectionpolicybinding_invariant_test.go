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
	"reflect"
	"regexp"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// TestSecretInjectionPolicyBinding_NoSensitiveValues is the hardening unit
// test for the doc.go "no sensitive values on CRs" invariant applied to
// a SecretInjectionPolicyBinding. A fully-populated fixture is marshaled
// to JSON and YAML; the same regexes the Credential and
// SecretInjectionPolicy invariants use must produce zero matches. Self-
// test rows guard against a silently-broken regex future.
func TestSecretInjectionPolicyBinding_NoSensitiveValues(t *testing.T) {
	sipb := fullyPopulatedSecretInjectionPolicyBinding()

	jsonBytes, err := json.Marshal(sipb)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	yamlBytes, err := yaml.Marshal(sipb)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}

	syntheticAPIKey := "sih_AaBbCcDdEeFfGgHhIiJjKkLlMm"
	syntheticArgon2 := "$argon2id$v=19$m=65536,t=3,p=4$salt"

	apiKeyPattern := regexp.MustCompile(`sih_[A-Za-z0-9_-]{20,}`)
	argon2Pattern := regexp.MustCompile(`\$argon2id\$`)

	tests := []struct {
		name     string
		pattern  *regexp.Regexp
		input    string
		wantHits int
	}{
		{name: "api_key_prefix/self-test", pattern: apiKeyPattern, input: syntheticAPIKey, wantHits: 1},
		{name: "api_key_prefix/json", pattern: apiKeyPattern, input: string(jsonBytes), wantHits: 0},
		{name: "api_key_prefix/yaml", pattern: apiKeyPattern, input: string(yamlBytes), wantHits: 0},
		{name: "argon2id/self-test", pattern: argon2Pattern, input: syntheticArgon2, wantHits: 1},
		{name: "argon2id/json", pattern: argon2Pattern, input: string(jsonBytes), wantHits: 0},
		{name: "argon2id/yaml", pattern: argon2Pattern, input: string(yamlBytes), wantHits: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := len(tc.pattern.FindAllString(tc.input, -1))
			if got != tc.wantHits {
				t.Fatalf("pattern %q: got %d matches, want %d\n---input---\n%s\n---",
					tc.pattern, got, tc.wantHits, tc.input)
			}
		})
	}
}

// TestSecretInjectionPolicyBinding_FieldNames walks the exported fields of
// the SIPB types (top-level + every nested sub-struct introduced by
// HOL-701) and asserts none carries a name signalling storage of forbidden
// material on a CR. Shared rules + walker live in invariant_helper_test.go.
func TestSecretInjectionPolicyBinding_FieldNames(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(v1alpha1.SecretInjectionPolicyBinding{}),
		reflect.TypeOf(v1alpha1.SecretInjectionPolicyBindingSpec{}),
		reflect.TypeOf(v1alpha1.SecretInjectionPolicyBindingStatus{}),
		reflect.TypeOf(v1alpha1.PolicyRef{}),
		reflect.TypeOf(v1alpha1.TargetRef{}),
	}
	for _, v := range v1alpha1.WalkForbiddenFieldNames(types, v1alpha1.DefaultForbiddenFieldNameRules) {
		t.Errorf("%s.%s contains forbidden substring %q; rename the field or add it to the allowlist in invariant_helper_test.go with a GoDoc justification on the field",
			v.TypeName, v.FieldName, v.Substring)
	}
}

// TestSecretInjectionPolicyBinding_JSONYAMLRoundTrip covers the required-
// field contract: a fully-populated fixture round-trips through JSON and
// YAML with the spec preserved. Field-by-field comparison because
// metav1.Time round-trips are not reflect.DeepEqual-exact across
// sigs.k8s.io/yaml (see UpstreamSecret round-trip test for the same
// pattern).
func TestSecretInjectionPolicyBinding_JSONYAMLRoundTrip(t *testing.T) {
	orig := fullyPopulatedSecretInjectionPolicyBinding()

	jsonBytes, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var fromJSON v1alpha1.SecretInjectionPolicyBinding
	if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	assertSIPBSpecEqual(t, "json", orig, fromJSON)

	yamlBytes, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	var fromYAML v1alpha1.SecretInjectionPolicyBinding
	if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	assertSIPBSpecEqual(t, "yaml", orig, fromYAML)
}

func assertSIPBSpecEqual(t *testing.T, format string, want, got v1alpha1.SecretInjectionPolicyBinding) {
	t.Helper()
	if got.APIVersion != want.APIVersion || got.Kind != want.Kind {
		t.Errorf("[%s] TypeMeta: got %+v want %+v", format, got.TypeMeta, want.TypeMeta)
	}
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("[%s] ObjectMeta: got (%s/%s) want (%s/%s)",
			format, got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if got.Spec.PolicyRef != want.Spec.PolicyRef {
		t.Errorf("[%s] Spec.PolicyRef: got %+v want %+v", format, got.Spec.PolicyRef, want.Spec.PolicyRef)
	}
	if !reflect.DeepEqual(got.Spec.TargetRefs, want.Spec.TargetRefs) {
		t.Errorf("[%s] Spec.TargetRefs: got %+v want %+v", format, got.Spec.TargetRefs, want.Spec.TargetRefs)
	}
	if !reflect.DeepEqual(got.Spec.WorkloadSelector, want.Spec.WorkloadSelector) {
		t.Errorf("[%s] Spec.WorkloadSelector: got %+v want %+v", format, got.Spec.WorkloadSelector, want.Spec.WorkloadSelector)
	}
}

func fullyPopulatedSecretInjectionPolicyBinding() v1alpha1.SecretInjectionPolicyBinding {
	return v1alpha1.SecretInjectionPolicyBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "SecretInjectionPolicyBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "vendor-binding",
			Namespace:  "holos-folder-east",
			Generation: 1,
		},
		Spec: v1alpha1.SecretInjectionPolicyBindingSpec{
			PolicyRef: v1alpha1.PolicyRef{
				Scope:     v1alpha1.PolicyRefScopeFolder,
				Namespace: "holos-folder-east",
				Name:      "vendor-egress",
			},
			TargetRefs: []v1alpha1.TargetRef{
				{
					Kind:      v1alpha1.TargetRefKindServiceAccount,
					Namespace: "project-a",
					Name:      "api-client",
				},
			},
			WorkloadSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api-client"},
			},
		},
		Status: v1alpha1.SecretInjectionPolicyBindingStatus{
			ObservedGeneration: 1,
			Conditions: []metav1.Condition{
				{
					Type:   v1alpha1.SecretInjectionPolicyBindingConditionReady,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.SecretInjectionPolicyBindingReasonReady,
				},
			},
		},
	}
}

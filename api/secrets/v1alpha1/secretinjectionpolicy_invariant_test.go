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

// TestSecretInjectionPolicy_NoSensitiveValues is the hardening unit test
// for the doc.go "no sensitive values on CRs" invariant applied to a
// SecretInjectionPolicy. A fully-populated fixture (no sensitive bytes,
// only plausible metadata) is marshaled to JSON and YAML, and the same
// regexes the Credential invariant uses must produce zero matches on the
// bytes. Self-test rows guard against a silently-broken regex future.
func TestSecretInjectionPolicy_NoSensitiveValues(t *testing.T) {
	sip := fullyPopulatedSecretInjectionPolicy()

	jsonBytes, err := json.Marshal(sip)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	yamlBytes, err := yaml.Marshal(sip)
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

// TestSecretInjectionPolicy_FieldNames walks the exported fields of the
// SIP types (top-level + every nested sub-struct introduced by HOL-701)
// and asserts none carries a name signalling storage of forbidden
// material on a CR. Shared rules + walker live in invariant_helper_test.go.
func TestSecretInjectionPolicy_FieldNames(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(v1alpha1.SecretInjectionPolicy{}),
		reflect.TypeOf(v1alpha1.SecretInjectionPolicySpec{}),
		reflect.TypeOf(v1alpha1.SecretInjectionPolicyStatus{}),
		reflect.TypeOf(v1alpha1.Match{}),
		reflect.TypeOf(v1alpha1.CallerAuth{}),
		reflect.TypeOf(v1alpha1.UpstreamRef{}),
	}
	for _, v := range v1alpha1.WalkForbiddenFieldNames(types, v1alpha1.DefaultForbiddenFieldNameRules) {
		t.Errorf("%s.%s contains forbidden substring %q; rename the field or add it to the allowlist in invariant_helper_test.go with a GoDoc justification on the field",
			v.TypeName, v.FieldName, v.Substring)
	}
}

// TestSecretInjectionPolicy_JSONYAMLRoundTrip covers the required-field
// contract for SecretInjectionPolicy: marshaling a fully-populated
// fixture, round-tripping through JSON and YAML, and asserting the round
// trip preserves direction, match, caller-auth, and upstream-ref bytes.
// Field-by-field comparison because metav1.Time round-trips are not
// reflect.DeepEqual-exact across sigs.k8s.io/yaml (see UpstreamSecret
// round-trip test for the same pattern).
func TestSecretInjectionPolicy_JSONYAMLRoundTrip(t *testing.T) {
	orig := fullyPopulatedSecretInjectionPolicy()

	jsonBytes, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var fromJSON v1alpha1.SecretInjectionPolicy
	if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	assertSIPSpecEqual(t, "json", orig, fromJSON)

	yamlBytes, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	var fromYAML v1alpha1.SecretInjectionPolicy
	if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	assertSIPSpecEqual(t, "yaml", orig, fromYAML)
}

// assertSIPSpecEqual compares the spec contract of a SecretInjectionPolicy
// round-trip. Status is not compared because metav1.Condition round-trips
// through sigs.k8s.io/yaml renormalise zero LastTransitionTime differently
// than reflect.DeepEqual expects.
func assertSIPSpecEqual(t *testing.T, format string, want, got v1alpha1.SecretInjectionPolicy) {
	t.Helper()
	if got.APIVersion != want.APIVersion || got.Kind != want.Kind {
		t.Errorf("[%s] TypeMeta: got %+v want %+v", format, got.TypeMeta, want.TypeMeta)
	}
	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Errorf("[%s] ObjectMeta: got (%s/%s) want (%s/%s)",
			format, got.Namespace, got.Name, want.Namespace, want.Name)
	}
	if got.Spec.Direction != want.Spec.Direction {
		t.Errorf("[%s] Spec.Direction: got %q want %q", format, got.Spec.Direction, want.Spec.Direction)
	}
	if !reflect.DeepEqual(got.Spec.Match, want.Spec.Match) {
		t.Errorf("[%s] Spec.Match: got %+v want %+v", format, got.Spec.Match, want.Spec.Match)
	}
	if got.Spec.CallerAuth != want.Spec.CallerAuth {
		t.Errorf("[%s] Spec.CallerAuth: got %+v want %+v", format, got.Spec.CallerAuth, want.Spec.CallerAuth)
	}
	if got.Spec.UpstreamRef != want.Spec.UpstreamRef {
		t.Errorf("[%s] Spec.UpstreamRef: got %+v want %+v", format, got.Spec.UpstreamRef, want.Spec.UpstreamRef)
	}
}

func fullyPopulatedSecretInjectionPolicy() v1alpha1.SecretInjectionPolicy {
	return v1alpha1.SecretInjectionPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "SecretInjectionPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "vendor-egress",
			Namespace:  "holos-prj",
			Generation: 1,
		},
		Spec: v1alpha1.SecretInjectionPolicySpec{
			Direction: v1alpha1.DirectionEgress,
			Match: v1alpha1.Match{
				Hosts:        []string{"vendor.example.com"},
				PathPrefixes: []string{"/api/v1"},
				Methods:      []string{"GET", "POST"},
			},
			CallerAuth: v1alpha1.CallerAuth{
				Type: v1alpha1.AuthenticationTypeAPIKey,
			},
			UpstreamRef: v1alpha1.UpstreamRef{
				Scope:     v1alpha1.UpstreamScopeProject,
				ScopeName: "project-a",
				Name:      "vendor-upstream",
			},
		},
		Status: v1alpha1.SecretInjectionPolicyStatus{
			ObservedGeneration: 1,
			Conditions: []metav1.Condition{
				{
					Type:   v1alpha1.SecretInjectionPolicyConditionReady,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.SecretInjectionPolicyReasonReady,
				},
			},
		},
	}
}

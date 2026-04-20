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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// TestCredential_NoSensitiveValues is the hardening unit test for the
// doc.go "no sensitive values on CRs" invariant. It fully populates a
// Credential with plausible non-sensitive values, marshals it to JSON and
// YAML, and asserts that the marshaled bytes carry zero matches for
// regexes that would represent a holos-issued API key or argon2id hash
// material. Each regex is exercised against a synthetic string on a
// self-test row so a silently-broken regex (e.g., a future refactor that
// drops the raw-string literal) fails alongside the real rows rather than
// zero-matching every assertion into a false pass.
func TestCredential_NoSensitiveValues(t *testing.T) {
	cred := fullyPopulatedCredential()

	jsonBytes, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	yamlBytes, err := yaml.Marshal(cred)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}

	// Synthetic inputs that MUST match each regex. Kept short and
	// unambiguous so a failing self-test row points at a regex bug, not
	// a fixture bug.
	syntheticAPIKey := "sih_AaBbCcDdEeFfGgHhIiJjKkLlMm"      // 32 chars after prefix
	syntheticArgon2 := "$argon2id$v=19$m=65536,t=3,p=4$salt" // canonical argon2id prefix

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

// TestCredential_FieldNames walks the exported fields of Credential,
// CredentialSpec, and CredentialStatus and asserts none carries a name
// that signals storage of forbidden material on a CR. Each forbidden
// substring carries its own allowlist of exact field names that
// legitimately contain the substring — widening an allowlist requires a
// GoDoc justification at the field itself, because the allowlist is the
// concrete guard on the no-sensitive-values invariant.
func TestCredential_FieldNames(t *testing.T) {
	rules := []struct {
		substring string
		allow     []string
	}{
		{substring: "Plaintext"},
		{substring: "Token"},
		{substring: "Prefix"},
		{substring: "LastFour"},
		{substring: "Fingerprint"},
		// *Ref fields name a sibling v1.Secret. The bytes never appear on
		// the Credential CR; only the metadata reference does. Each
		// entry here must be a pure-reference field.
		{substring: "Secret", allow: []string{"HashSecretRef", "UpstreamSecretRef"}},
		{substring: "Hash", allow: []string{"HashSecretRef"}},
		{substring: "Pepper", allow: []string{"PepperVersion"}},
	}

	// Cover the top-level CR types AND every nested struct introduced by
	// HOL-699. A future Token/Secret/Hash field added under one of these
	// sub-structs must fail the invariant, not slip past it.
	types := []reflect.Type{
		reflect.TypeOf(v1alpha1.Credential{}),
		reflect.TypeOf(v1alpha1.CredentialSpec{}),
		reflect.TypeOf(v1alpha1.CredentialStatus{}),
		reflect.TypeOf(v1alpha1.Authentication{}),
		reflect.TypeOf(v1alpha1.APIKeySettings{}),
		reflect.TypeOf(v1alpha1.Rotation{}),
		reflect.TypeOf(v1alpha1.Selector{}),
		reflect.TypeOf(v1alpha1.TargetReference{}),
	}
	for _, rt := range types {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			for _, rule := range rules {
				if !strings.Contains(f.Name, rule.substring) {
					continue
				}
				if containsString(rule.allow, f.Name) {
					continue
				}
				t.Errorf("%s.%s contains forbidden substring %q; rename the field or add it to the allowlist in credential_invariant_test.go with a GoDoc justification on the field",
					rt.Name(), f.Name, rule.substring)
			}
		}
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// fullyPopulatedCredential returns a Credential whose fields are set to
// plausible values for the v1alpha1 contract but contain no sensitive
// byte pattern. This is the fixture the invariant test drives through
// both serializers.
func fullyPopulatedCredential() v1alpha1.Credential {
	expires := metav1.NewTime(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	bind := true

	return v1alpha1.Credential{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "Credential",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "vendor-apikey",
			Namespace:  "holos-prj",
			Generation: 3,
		},
		Spec: v1alpha1.CredentialSpec{
			Authentication: v1alpha1.Authentication{
				Type:   v1alpha1.AuthenticationTypeAPIKey,
				APIKey: &v1alpha1.APIKeySettings{HeaderName: "X-Api-Key"},
			},
			UpstreamSecretRef: v1alpha1.NamespacedSecretKeyReference{
				Namespace: "holos-prj",
				Name:      "vendor-upstream",
				Key:       "apiKey",
			},
			ExpiresAt:             &expires,
			Revoked:               false,
			BindToSourcePrincipal: &bind,
			Rotation:              v1alpha1.Rotation{GraceSeconds: 300},
			Selector: v1alpha1.Selector{
				TargetRefs: []v1alpha1.TargetReference{
					{Kind: "ServiceAccount", Name: "api-client"},
				},
				WorkloadSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "api-client"},
				},
			},
		},
		Status: v1alpha1.CredentialStatus{
			ObservedGeneration: 3,
			Phase:              v1alpha1.PhaseActive,
			CredentialID:       "1htmGQrYpQl3VlzKdVcG2oB5t2e",
			HashSecretRef: &v1alpha1.SecretKeyReference{
				Name: "cred-hash",
				Key:  "hash",
			},
			PepperVersion: 2,
			Conditions: []metav1.Condition{
				{
					Type:               v1alpha1.CredentialConditionReady,
					Status:             metav1.ConditionTrue,
					Reason:             v1alpha1.CredentialReasonReady,
					Message:            "credential active",
					LastTransitionTime: metav1.NewTime(time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)),
					ObservedGeneration: 3,
				},
			},
		},
	}
}

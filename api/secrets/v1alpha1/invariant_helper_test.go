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

package v1alpha1

import (
	"reflect"
	"strings"
)

// ForbiddenFieldNameRule names a substring that must not appear in any
// exported field name on a CR type in this group, plus an allowlist of
// exact field names that legitimately carry the substring (e.g.,
// HashSecretRef is a pure reference — the bytes never appear on the CR).
// Rules are consumed by WalkForbiddenFieldNames.
type ForbiddenFieldNameRule struct {
	Substring string
	Allow     []string
}

// DefaultForbiddenFieldNameRules is the shared rule set that enforces the
// "no sensitive values on CRs" invariant at the field-name level. Any kind
// in secrets.holos.run/v1alpha1 that gets a field carrying one of these
// substrings either fails the check or extends the allowlist with a
// GoDoc-justified entry at the field itself.
//
// The Secret/Hash/Pepper rows carry narrow allowlists because exactly those
// names refer to sibling v1.Secrets (HashSecretRef, UpstreamSecretRef) or
// to a monotonic counter with no pepper material (PepperVersion). Every
// other variant of those substrings is forbidden.
var DefaultForbiddenFieldNameRules = []ForbiddenFieldNameRule{
	{Substring: "Plaintext"},
	{Substring: "Token"},
	// "Prefix" targets credential-prefix leaks (e.g., "KeyPrefix",
	// "TokenPrefix"). PathPrefixes on SecretInjectionPolicy.Match is a
	// URL-path match predicate — it carries no credential entropy.
	{Substring: "Prefix", Allow: []string{"PathPrefixes"}},
	{Substring: "LastFour"},
	{Substring: "Fingerprint"},
	{Substring: "Secret", Allow: []string{"HashSecretRef", "UpstreamSecretRef"}},
	{Substring: "Hash", Allow: []string{"HashSecretRef"}},
	{Substring: "Pepper", Allow: []string{"PepperVersion"}},
}

// FieldNameViolation records one exported field whose name matches a
// forbidden substring and is not on the rule's allowlist.
type FieldNameViolation struct {
	TypeName  string
	FieldName string
	Substring string
}

// WalkForbiddenFieldNames returns every FieldNameViolation found across the
// given types. The walk is shallow — it inspects the direct fields of each
// provided reflect.Type and does not descend into referenced sub-structs,
// so callers MUST enumerate every struct type that can appear on a CR
// (top-level Kind + every nested sub-struct + every shared ref type).
// Unexported fields are skipped; fields named in a rule's Allow list are
// skipped.
//
// This helper exists so every CRD in the group can assert the invariant
// with the same rule set rather than copy/pasting the walker per kind.
func WalkForbiddenFieldNames(types []reflect.Type, rules []ForbiddenFieldNameRule) []FieldNameViolation {
	var out []FieldNameViolation
	for _, rt := range types {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			for _, rule := range rules {
				if !strings.Contains(f.Name, rule.Substring) {
					continue
				}
				if ruleAllows(rule, f.Name) {
					continue
				}
				out = append(out, FieldNameViolation{
					TypeName:  rt.Name(),
					FieldName: f.Name,
					Substring: rule.Substring,
				})
			}
		}
	}
	return out
}

func ruleAllows(rule ForbiddenFieldNameRule, name string) bool {
	for _, a := range rule.Allow {
		if a == name {
			return true
		}
	}
	return false
}

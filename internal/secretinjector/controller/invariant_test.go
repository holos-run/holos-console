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

package controller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// assertNoSensitiveValuesOnCR is the envtest marshal-scan gate for the
// dominant "no sensitive values on CRs" invariant from
// api/secrets/v1alpha1/doc.go. The helper GETs the named CR via the
// controller-runtime client, marshals the result to JSON AND YAML, and
// asserts every pattern in secretsv1alpha1.ForbiddenBytePatterns
// produces zero matches in both representations.
//
// Why both JSON and YAML. A reconciler might write a `metadata.labels`
// or `metadata.annotations` value that renders differently between
// serialisers — YAML is free to quote ambiguous values, JSON escapes
// control bytes differently, and a future strict-serialiser change
// could surface a match in one form but not the other. Running both
// scanners closes the gap; a miss in either form fails the gate.
//
// Why we never print the matched bytes. Any bytes that matched are by
// definition credential material (API key prefix, argon2id envelope);
// dumping them to the test log or the CI artifact store would undo the
// invariant we are trying to verify. The helper reports the pattern
// Name and the Kind/NamespacedName that failed so the test owner can
// reproduce the failure with the same object without the bytes ever
// appearing in a log pipeline.
//
// Callers invoke this helper after every reconcile step that produces
// observable state so a regression on any one branch fails the test
// that introduced it, not a downstream assertion that happens to walk
// the same object. The shared envtest suite ties every Get + wait
// helper to a call of this function.
func assertNoSensitiveValuesOnCR(t *testing.T, ctx context.Context, c client.Client, obj client.Object, key types.NamespacedName) {
	t.Helper()
	if err := c.Get(ctx, key, obj); err != nil {
		t.Fatalf("marshal-scan invariant: GET %T %s: %v", obj, key, err)
	}
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal-scan invariant: json.Marshal %T %s: %v", obj, key, err)
	}
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal-scan invariant: yaml.Marshal %T %s: %v", obj, key, err)
	}
	kind := fmt.Sprintf("%T", obj)
	for _, pattern := range secretsv1alpha1.ForbiddenBytePatterns {
		if pattern.Pattern.Match(jsonBytes) {
			t.Fatalf("marshal-scan invariant violated: pattern %q matched JSON form of %s %s; bytes intentionally NOT printed to avoid leaking credential material (see api/secrets/v1alpha1/doc.go)",
				pattern.Name, kind, key)
		}
		if pattern.Pattern.Match(yamlBytes) {
			t.Fatalf("marshal-scan invariant violated: pattern %q matched YAML form of %s %s; bytes intentionally NOT printed to avoid leaking credential material (see api/secrets/v1alpha1/doc.go)",
				pattern.Name, kind, key)
		}
	}
}

// assertSuiteMarshalScan sweeps every relevant kind in the supplied
// namespace and runs assertNoSensitiveValuesOnCR on each live object.
// Used from the cross-reconciler TestCredential_MarshalScanSuite below
// to lock in the invariant across every CR the injector touches,
// independent of which reconciler populated the object.
//
// The helper LISTs each kind namespace-scoped; the envtest suite only
// creates objects in the namespaces it owns, so a List-all here keeps
// the test output small and hermetic. When a CR set grows, extend this
// function rather than duplicating the scan in each test — the point
// of the gate is a single shared assertion surface.
func assertSuiteMarshalScan(t *testing.T, ctx context.Context, c client.Client, namespace string) {
	t.Helper()

	var upstreams secretsv1alpha1.UpstreamSecretList
	if err := c.List(ctx, &upstreams, client.InNamespace(namespace)); err != nil {
		t.Fatalf("marshal-scan suite: list UpstreamSecrets in %s: %v", namespace, err)
	}
	for i := range upstreams.Items {
		item := &upstreams.Items[i]
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.UpstreamSecret{},
			types.NamespacedName{Namespace: item.Namespace, Name: item.Name})
	}

	var credentials secretsv1alpha1.CredentialList
	if err := c.List(ctx, &credentials, client.InNamespace(namespace)); err != nil {
		t.Fatalf("marshal-scan suite: list Credentials in %s: %v", namespace, err)
	}
	for i := range credentials.Items {
		item := &credentials.Items[i]
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.Credential{},
			types.NamespacedName{Namespace: item.Namespace, Name: item.Name})
	}

	var bindings secretsv1alpha1.SecretInjectionPolicyBindingList
	if err := c.List(ctx, &bindings, client.InNamespace(namespace)); err != nil {
		t.Fatalf("marshal-scan suite: list SecretInjectionPolicyBindings in %s: %v", namespace, err)
	}
	for i := range bindings.Items {
		item := &bindings.Items[i]
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.SecretInjectionPolicyBinding{},
			types.NamespacedName{Namespace: item.Namespace, Name: item.Name})
	}

	var policies secretsv1alpha1.SecretInjectionPolicyList
	if err := c.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		t.Fatalf("marshal-scan suite: list SecretInjectionPolicies in %s: %v", namespace, err)
	}
	for i := range policies.Items {
		item := &policies.Items[i]
		assertNoSensitiveValuesOnCR(t, ctx, c, &secretsv1alpha1.SecretInjectionPolicy{},
			types.NamespacedName{Namespace: item.Namespace, Name: item.Name})
	}
}

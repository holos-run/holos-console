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

package controller

import (
	"fmt"
	"sort"

	istiosecurityv1beta1 "istio.io/api/security/v1beta1"
	istiotypev1beta1 "istio.io/api/type/v1beta1"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
)

// bindingAuthzProviderName is the MeshConfig extension-provider name that
// routes AuthorizationPolicy evaluations to the holos-secret-injector
// ext_authz Check service in M3. The string is a contract: operators who
// install the injector MUST declare an extensionProvider of this name in
// MeshConfig, and the reconciler stamps it onto every emitted
// AuthorizationPolicy.spec.provider.name. Changing the string would quietly
// detach every previously emitted policy from the provider, so it is
// exported for tests and kept in exactly one place.
const bindingAuthzProviderName = "holos-secret-injector"

// bindingAuthzTrustDomain is the SPIFFE trust domain prefix used when
// expanding TargetRefKindServiceAccount entries into
// source.principals strings. `cluster.local` is the upstream Istio default
// and what every in-cluster ServiceAccount credential presents unless the
// mesh has been re-pegged. A cluster that overrides the trust domain will
// need a follow-up ticket to plumb the override through a binding spec —
// tracked by the "not covered in M2" comment on WaypointNotFound below.
const bindingAuthzTrustDomain = "cluster.local"

// bindingAuthzNameSuffix is the deterministic suffix appended to the
// binding's metadata.name when synthesising the AuthorizationPolicy's
// metadata.name. The suffix makes `kubectl get authorizationpolicies`
// output obviously attributable to a binding without hiding the name
// behind a hash — the 1:1 owner-ref relationship gives operators an O(1)
// pivot from binding to AP and back.
const bindingAuthzNameSuffix = "-secret-injector"

// bindingAuthzManagedByValue is stamped onto every emitted
// AuthorizationPolicy via the app.kubernetes.io/managed-by label so an
// operator running `kubectl get authorizationpolicies -l
// app.kubernetes.io/managed-by=holos-secret-injector` sees only the
// mesh artifacts this reconciler owns.
const bindingAuthzManagedByValue = "holos-secret-injector"

// bindingAuthzBindingLabel records the owning binding's metadata.name on
// the emitted AuthorizationPolicy so operators can find every AP a single
// binding has produced even across a binding rename or re-create cycle.
const bindingAuthzBindingLabel = "secrets.holos.run/binding"

// authorizationPolicyName returns the deterministic metadata.name for the
// AuthorizationPolicy a binding owns. The reconciler emits exactly one
// AuthorizationPolicy per binding — aggregating every target into a
// single object keeps the ownerReference fan-out at 1:1 and simplifies
// delete-cascade semantics.
func authorizationPolicyName(bindingName string) string {
	return bindingName + bindingAuthzNameSuffix
}

// buildAuthorizationPolicy is the pure-Go builder used by the reconciler
// to translate a (binding, policy) pair into a single
// security.istio.io/v1 AuthorizationPolicy with action=CUSTOM and the
// holos-secret-injector extension provider.
//
// Why a pure builder:
//
//   - It keeps the reconciler's Reconcile body focused on resolve/write
//     bookkeeping and ownership. The translation rules are the messiest
//     part of this phase (caller identity derivation, Service selector
//     vs ServiceAccount principals, workload selector overlay); isolating
//     them in a client-less function means the unit tests cover every
//     shape without spinning up a fake cache.
//
//   - It is deterministic. The output AuthorizationPolicy depends only on
//     the inputs — Rule ordering follows the order targetRefs are
//     declared on the binding, and ServiceAccount names are
//     lexicographically sorted before being joined into
//     source.principals so re-runs on the same input produce the same
//     AP and the hot-loop guard can detect a no-op.
//
// The returned AuthorizationPolicy carries the binding's namespace, the
// deterministic name from authorizationPolicyName, and the stable
// labels declared above; the caller sets the ownerReference via
// ctrl.SetControllerReference before Create/Update so the runtime
// scheme is the authoritative place where the GVK is stamped.
//
// WaypointNotFound (HOL-752). M3 will introduce a waypoint lookup
// (resolve the waypoint Service for the binding's targets, stamp it on
// the AP) and surface WaypointNotFound when the lookup fails. In M2
// this reconciler does not do a waypoint lookup, so
// WaypointNotFound is never published — but the reason string remains
// in conditions.go so the M3 diff stays small. See the HOL-747 plan.
func buildAuthorizationPolicy(binding *secretsv1alpha1.SecretInjectionPolicyBinding, policy *secretsv1alpha1.SecretInjectionPolicy) (*istiosecurityv1.AuthorizationPolicy, error) {
	if binding == nil {
		return nil, fmt.Errorf("binding must not be nil")
	}
	if policy == nil {
		return nil, fmt.Errorf("policy must not be nil")
	}

	selector, err := selectorForBinding(binding)
	if err != nil {
		return nil, err
	}

	rules, err := rulesForBinding(binding)
	if err != nil {
		return nil, err
	}

	ap := &istiosecurityv1.AuthorizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authorizationPolicyName(binding.Name),
			Namespace: binding.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": bindingAuthzManagedByValue,
				bindingAuthzBindingLabel:       binding.Name,
			},
		},
		Spec: istiosecurityv1beta1.AuthorizationPolicy{
			Action: istiosecurityv1beta1.AuthorizationPolicy_CUSTOM,
			ActionDetail: &istiosecurityv1beta1.AuthorizationPolicy_Provider{
				Provider: &istiosecurityv1beta1.AuthorizationPolicy_ExtensionProvider{
					Name: bindingAuthzProviderName,
				},
			},
			Selector: selector,
			Rules:    rules,
		},
	}
	return ap, nil
}

// selectorForBinding returns the AuthorizationPolicy.spec.selector for
// the binding. When the binding carries a Service target or a
// workloadSelector, the selector narrows the AP to pods carrying the
// matching labels; when every target is a ServiceAccount and no
// workloadSelector is supplied, a nil selector is returned and the AP
// applies namespace-wide (still gated by the source.principals rules).
//
// Order-independence and determinism. The selector MatchLabels map is
// populated in two passes (workloadSelector first, then per-target
// kubernetes-service label). The helper therefore tolerates multiple
// Service targets by overwriting the single `kubernetes.io/service-name`
// label — the reconciler only emits one selector per AP because the
// selector is an AND across labels; overlapping Service targets are a
// bindings-level multi-AP ask that belongs in a future ticket, not M2.
func selectorForBinding(binding *secretsv1alpha1.SecretInjectionPolicyBinding) (*istiotypev1beta1.WorkloadSelector, error) {
	matchLabels := make(map[string]string)

	if binding.Spec.WorkloadSelector != nil {
		for k, v := range binding.Spec.WorkloadSelector.MatchLabels {
			matchLabels[k] = v
		}
		if len(binding.Spec.WorkloadSelector.MatchExpressions) > 0 {
			return nil, fmt.Errorf("spec.workloadSelector.matchExpressions is not supported in v1alpha1 — use matchLabels")
		}
	}

	var serviceTargets []string
	for _, t := range binding.Spec.TargetRefs {
		if t.Kind == secretsv1alpha1.TargetRefKindService {
			serviceTargets = append(serviceTargets, t.Name)
		}
	}
	if len(serviceTargets) == 1 {
		// Istio's upstream `kubernetes.io/service-name` label is populated
		// by the service controller on every endpointslice-backed Pod —
		// selecting on it is the canonical way to narrow an AP to a
		// single Service without teaching the AP about pods directly.
		matchLabels["kubernetes.io/service-name"] = serviceTargets[0]
	}

	if len(matchLabels) == 0 {
		return nil, nil
	}
	return &istiotypev1beta1.WorkloadSelector{MatchLabels: matchLabels}, nil
}

// rulesForBinding returns the AuthorizationPolicy.spec.rules for the
// binding. Exactly one Rule is emitted — every ServiceAccount target is
// merged into a single source.principals list so the AP matches when at
// least one of the allow-listed identities presents the request. When
// the binding carries no ServiceAccount targets (every target is a
// Service), the Rule has no source.principals and the AP applies to
// every caller matched by the selector.
//
// Empty-rule semantics. Istio treats an AuthorizationPolicy with
// action=CUSTOM and no rules as "never match", which would silently
// disable the ext_authz Check path. The reconciler short-circuits by
// emitting a single empty-but-present Rule when neither principals nor
// operations are populated, so the provider still sees the request and
// the M3 Check path is exercised.
func rulesForBinding(binding *secretsv1alpha1.SecretInjectionPolicyBinding) ([]*istiosecurityv1beta1.Rule, error) {
	principals := callerPrincipals(binding)

	rule := &istiosecurityv1beta1.Rule{}
	if len(principals) > 0 {
		rule.From = []*istiosecurityv1beta1.Rule_From{{
			Source: &istiosecurityv1beta1.Source{Principals: principals},
		}}
	}
	return []*istiosecurityv1beta1.Rule{rule}, nil
}

// callerPrincipals returns the SPIFFE principals derived from the
// binding's ServiceAccount targets, sorted lexicographically for
// determinism. Service targets do not contribute principals — they are
// encoded via the selector instead.
func callerPrincipals(binding *secretsv1alpha1.SecretInjectionPolicyBinding) []string {
	seen := make(map[string]struct{}, len(binding.Spec.TargetRefs))
	for _, t := range binding.Spec.TargetRefs {
		if t.Kind != secretsv1alpha1.TargetRefKindServiceAccount {
			continue
		}
		principal := fmt.Sprintf("%s/ns/%s/sa/%s", bindingAuthzTrustDomain, t.Namespace, t.Name)
		seen[principal] = struct{}{}
	}
	principals := make([]string, 0, len(seen))
	for p := range seen {
		principals = append(principals, p)
	}
	sort.Strings(principals)
	return principals
}

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

// Condition types and reason strings are contract — renaming any of these
// constants is a breaking change. See the per-kind tables in ADR 030
// ("Per-kind condition contract").

// Template condition types.
const (
	// TemplateConditionAccepted tracks whether the reconciler parsed .spec
	// and accepted it, or rejected it with a typed reason.
	TemplateConditionAccepted = "Accepted"
	// TemplateConditionCUEValid tracks whether the embedded CUE payload
	// parses and type-checks against its own schema.
	TemplateConditionCUEValid = "CUEValid"
	// TemplateConditionLinkedRefsResolved tracks whether every
	// LinkedTemplateRef in .spec.linkedTemplates resolves to an existing
	// Template at a compatible version.
	TemplateConditionLinkedRefsResolved = "LinkedRefsResolved"
	// TemplateConditionReady is the aggregate: Accepted && CUEValid &&
	// LinkedRefsResolved are all True.
	TemplateConditionReady = "Ready"
)

// Template condition reasons.
const (
	TemplateReasonAccepted                 = "Accepted"
	TemplateReasonInvalidSpec              = "InvalidSpec"
	TemplateReasonCUEValid                 = "CUEValid"
	TemplateReasonCUEParseError            = "CUEParseError"
	TemplateReasonCUETypeError             = "CUETypeError"
	TemplateReasonResolvedRefs             = "ResolvedRefs"
	TemplateReasonLinkedRefNotFound        = "LinkedRefNotFound"
	TemplateReasonLinkedRefVersionMismatch = "LinkedRefVersionMismatch"
	TemplateReasonReady                    = "Ready"
	TemplateReasonNotReady                 = "NotReady"
)

// TemplatePolicy condition types.
const (
	// TemplatePolicyConditionAccepted tracks whether the reconciler parsed
	// .spec.rules and accepted them, or rejected them.
	TemplatePolicyConditionAccepted = "Accepted"
	// TemplatePolicyConditionReady is the aggregate: Accepted is True.
	TemplatePolicyConditionReady = "Ready"
)

// TemplatePolicy condition reasons.
const (
	TemplatePolicyReasonAccepted     = "Accepted"
	TemplatePolicyReasonInvalidRules = "InvalidRules"
	TemplatePolicyReasonReady        = "Ready"
	TemplatePolicyReasonNotReady     = "NotReady"
)

// TemplatePolicyBinding condition types.
const (
	// TemplatePolicyBindingConditionAccepted tracks whether the reconciler
	// parsed .spec.policyRef and .spec.targetRefs and accepted the spec.
	TemplatePolicyBindingConditionAccepted = "Accepted"
	// TemplatePolicyBindingConditionResolvedRefs tracks whether every
	// target_ref kind is permitted and every policy_ref resolves to an
	// existing TemplatePolicy. The condition name is deliberately
	// identical to the Gateway API HTTPRoute.status ResolvedRefs
	// condition — platform operators already understand its meaning.
	TemplatePolicyBindingConditionResolvedRefs = "ResolvedRefs"
	// TemplatePolicyBindingConditionReady is the aggregate: Accepted &&
	// ResolvedRefs are both True.
	TemplatePolicyBindingConditionReady = "Ready"
)

// TemplatePolicyBinding condition reasons.
const (
	TemplatePolicyBindingReasonAccepted          = "Accepted"
	TemplatePolicyBindingReasonInvalidSpec       = "InvalidSpec"
	TemplatePolicyBindingReasonResolvedRefs      = "ResolvedRefs"
	TemplatePolicyBindingReasonTemplateNotFound  = "TemplateNotFound"
	TemplatePolicyBindingReasonPolicyNotFound    = "PolicyNotFound"
	TemplatePolicyBindingReasonInvalidTargetKind = "InvalidTargetKind"
	TemplatePolicyBindingReasonReady             = "Ready"
	TemplatePolicyBindingReasonNotReady          = "NotReady"
)

// Finalizer is the finalizer key used by reconcilers for the
// templates.holos.run API group when non-trivial cleanup is required before
// the API server deletes a managed object.
const Finalizer = "templates.holos.run/finalizer"

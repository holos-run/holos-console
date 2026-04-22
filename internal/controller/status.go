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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mergeCondition stamps the reconciler-observed generation onto cond and
// merges it into the supplied condition slice via meta.SetStatusCondition.
// The helper keeps each reconciler's Reconcile implementation focused on
// building conditions rather than bookkeeping ObservedGeneration and
// LastTransitionTime — both of which are populated by meta.SetStatusCondition
// for us. Callers pass the object's `metadata.generation` through generation
// so the returned conditions carry the same per-condition observedGeneration
// as the top-level status.observedGeneration written alongside them.
func mergeCondition(dst *[]metav1.Condition, generation int64, cond metav1.Condition) {
	cond.ObservedGeneration = generation
	meta.SetStatusCondition(dst, cond)
}

// conditionsEqualIgnoringTransitionTime reports whether the proposed
// conditions differ from the existing conditions in any field the HOL-620
// hot-loop-guard cares about: Type, Status, Reason, Message,
// ObservedGeneration. LastTransitionTime is intentionally ignored — it is
// set by the API server on every status write, so comparing it would force
// a write on every reconcile. Order is NOT significant because we key by
// Type, matching the kubebuilder `+listMapKey=type` marker on each CRD.
func conditionsEqualIgnoringTransitionTime(existing, proposed []metav1.Condition) bool {
	if len(existing) != len(proposed) {
		return false
	}
	// Build a lookup keyed on Type for O(N) comparison.
	ex := make(map[string]metav1.Condition, len(existing))
	for _, c := range existing {
		ex[c.Type] = c
	}
	for _, pc := range proposed {
		ec, ok := ex[pc.Type]
		if !ok {
			return false
		}
		if ec.Status != pc.Status ||
			ec.Reason != pc.Reason ||
			ec.Message != pc.Message ||
			ec.ObservedGeneration != pc.ObservedGeneration {
			return false
		}
	}
	return true
}

// aggregateReady returns ConditionTrue iff every component condition in
// components has Status=True. Used by each reconciler to derive the
// top-level Ready condition from the kind-specific component conditions.
// The reasonReady / reasonNotReady arguments keep the Ready reason string
// consistent with the per-kind contract in api/templates/v1alpha1.
func aggregateReady(components []metav1.Condition, reasonReady, reasonNotReady, messageReady, messageNotReady string) metav1.Condition {
	for _, c := range components {
		if c.Status != metav1.ConditionTrue {
			return metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  reasonNotReady,
				Message: messageNotReady,
			}
		}
	}
	return metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  reasonReady,
		Message: messageReady,
	}
}

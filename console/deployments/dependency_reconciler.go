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

// Package deployments — EnsureSingletonDependencyDeployment shared helper.
//
// # Design: singleton dependency Deployment with refcount owner-refs
//
// EnsureSingletonDependencyDeployment is the deliberate seam shared by both
// the TemplateDependency reconciler (Phase 5, HOL-959) and the
// TemplateRequirement reconciler (Phase 6, HOL-960). It takes a fully resolved
// (project, requires, dependent, cascadeDelete) tuple and does not know about
// the originating CRD kind — keeping it pure makes it easier to reason about
// and to test in isolation.
//
// # Singleton naming
//
// The singleton Deployment name is deterministic and unique within the project
// namespace. The format is:
//
//	<requires.Name>-<requires.VersionConstraint>-shared
//
// where VersionConstraint is used as a version discriminator. When
// VersionConstraint is empty, the format collapses to:
//
//	<requires.Name>-shared
//
// Example: a Requires reference to template "waypoint" with version "v1"
// produces the singleton name "waypoint-v1-shared". Phase 8 (PreflightCheck)
// surfaces collisions between user-named Deployments and singleton names.
//
// # Owner-reference model
//
// The singleton carries one non-controller ownerReference per dependent
// Deployment. Because multiple owners can co-own the singleton without a
// single controller, Controller is set to false (or nil) and BlockOwnerDeletion
// is set to true so native Kubernetes GC reaps the singleton when the last
// dependent is deleted. cascadeDelete=false skips adding the owner-ref edge so
// the singleton's lifecycle is decoupled from that dependent.
//
// # Cross-namespace validation
//
// EnsureSingletonDependencyDeployment accepts a Validator (implemented by
// TemplateGrantCache). If requires.Namespace differs from dependent.Namespace
// (cross-namespace reference), the validator is consulted before any write.
// A non-nil error from ValidateGrant causes EnsureSingletonDependencyDeployment
// to return a *GrantNotFoundError so callers can surface it as a Kubernetes
// condition on their object.
package deployments

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// SingletonName returns the deterministic singleton Deployment name for the
// given requires reference. The format is documented in the package comment.
//
// Format:
//
//	<requires.Name>-<requires.VersionConstraint>-shared   (VersionConstraint non-empty)
//	<requires.Name>-shared                                 (VersionConstraint empty)
//
// The VersionConstraint is sanitised: forward slashes, spaces, angle brackets,
// and equals signs are stripped so the result is a valid DNS label component.
func SingletonName(requires v1alpha1.LinkedTemplateRef) string {
	if requires.VersionConstraint == "" {
		return requires.Name + "-shared"
	}
	// Sanitise the version constraint for use in a Kubernetes name.
	sanitised := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '/', '<', '>', '=', '^', '~':
			return '-'
		}
		return r
	}, requires.VersionConstraint)
	// Collapse consecutive hyphens and trim leading/trailing hyphens.
	for strings.Contains(sanitised, "--") {
		sanitised = strings.ReplaceAll(sanitised, "--", "-")
	}
	sanitised = strings.Trim(sanitised, "-")
	return requires.Name + "-" + sanitised + "-shared"
}

// EnsureSingletonDependencyDeployment creates or updates the singleton
// Deployment of requires in the same namespace as dependent, adding a
// non-controller ownerReference from the dependent Deployment to the
// singleton when cascadeDelete is true.
//
// The function is idempotent: if the singleton already exists it ensures that
// the ownerReference from dependent is present (if cascadeDelete is true) and
// returns without error.
//
// Cross-namespace references (requires.Namespace != dependent.Namespace) are
// validated against the supplied Validator before any write. A nil validator
// is treated as default-deny: cross-namespace references return a
// *GrantNotFoundError without contacting the API server.
//
// The singleton is created in dependent.Namespace (the project namespace),
// mirroring the spec of a Deployment for the requires template. The spec
// fields are populated only from the requires LinkedTemplateRef: the caller
// does not need to pre-resolve the Template object.
func EnsureSingletonDependencyDeployment(
	ctx context.Context,
	c client.Client,
	validator Validator,
	requires v1alpha1.LinkedTemplateRef,
	dependent *deploymentsv1alpha1.Deployment,
	cascadeDelete bool,
) error {
	// Validate cross-namespace grants before touching the API server.
	dependentRef := v1alpha1.LinkedTemplateRef{
		Namespace: dependent.Namespace,
		Name:      dependent.Name,
	}
	if err := validator.ValidateGrant(ctx, dependentRef, requires); err != nil {
		return err
	}

	name := SingletonName(requires)
	key := types.NamespacedName{Namespace: dependent.Namespace, Name: name}

	var existing deploymentsv1alpha1.Deployment
	err := c.Get(ctx, key, &existing)
	if apierrors.IsNotFound(err) {
		// Create the singleton Deployment.
		singleton := buildSingleton(requires, dependent, name, cascadeDelete)
		if createErr := c.Create(ctx, singleton); createErr != nil {
			if apierrors.IsAlreadyExists(createErr) {
				// Race: another reconcile created it concurrently. Fetch and
				// update below.
				if getErr := c.Get(ctx, key, &existing); getErr != nil {
					return fmt.Errorf("get singleton after AlreadyExists: %w", getErr)
				}
				return ensureOwnerRef(ctx, c, &existing, dependent, cascadeDelete)
			}
			return fmt.Errorf("create singleton Deployment %s/%s: %w", dependent.Namespace, name, createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get singleton Deployment %s/%s: %w", dependent.Namespace, name, err)
	}

	// Singleton already exists: ensure the ownerReference is present.
	return ensureOwnerRef(ctx, c, &existing, dependent, cascadeDelete)
}

// buildSingleton constructs the initial singleton Deployment CR from the
// requires reference and the dependent Deployment's namespace. The singleton
// lives in the same project namespace as the dependent.
func buildSingleton(
	requires v1alpha1.LinkedTemplateRef,
	dependent *deploymentsv1alpha1.Deployment,
	name string,
	cascadeDelete bool,
) *deploymentsv1alpha1.Deployment {
	singleton := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dependent.Namespace,
			Name:      name,
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: dependent.Spec.ProjectName,
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: requires.Namespace,
				Name:      requires.Name,
			},
			VersionConstraint: requires.VersionConstraint,
			DisplayName:       name,
		},
	}
	if cascadeDelete {
		singleton.OwnerReferences = []metav1.OwnerReference{
			ownerRef(dependent),
		}
	}
	return singleton
}

// ensureOwnerRef ensures a non-controller ownerReference from dependent to
// singleton is present. It is a no-op when cascadeDelete is false or when the
// ownerReference already exists. The update is issued as a full object update
// (not a patch) because the owner-refs slice is small and conflicts are
// detected by the optimistic locking resourceVersion check.
func ensureOwnerRef(
	ctx context.Context,
	c client.Client,
	singleton *deploymentsv1alpha1.Deployment,
	dependent *deploymentsv1alpha1.Deployment,
	cascadeDelete bool,
) error {
	if !cascadeDelete {
		return nil
	}
	ref := ownerRef(dependent)
	for _, existing := range singleton.OwnerReferences {
		if existing.UID == ref.UID {
			return nil // already present
		}
	}
	updated := singleton.DeepCopy()
	updated.OwnerReferences = append(updated.OwnerReferences, ref)
	if err := c.Update(ctx, updated); err != nil {
		if apierrors.IsConflict(err) {
			// Let the caller requeue; a conflict means the singleton was
			// updated between our Get and this Update.
			return fmt.Errorf("conflict updating singleton Deployment %s/%s ownerRefs: %w", singleton.Namespace, singleton.Name, err)
		}
		return fmt.Errorf("update singleton Deployment %s/%s ownerRefs: %w", singleton.Namespace, singleton.Name, err)
	}
	return nil
}

// ownerRef builds a non-controller ownerReference for the given Deployment.
// Controller is explicitly set to false so multiple dependents can co-own the
// singleton without Kubernetes rejecting the second owner. BlockOwnerDeletion
// is set to true so GC waits for all owners to be gone before reaping the
// singleton.
func ownerRef(dep *deploymentsv1alpha1.Deployment) metav1.OwnerReference {
	f := false
	t := true
	return metav1.OwnerReference{
		APIVersion:         deploymentsv1alpha1.GroupVersion.String(),
		Kind:               "Deployment",
		Name:               dep.Name,
		UID:                dep.UID,
		Controller:         &f,
		BlockOwnerDeletion: &t,
	}
}

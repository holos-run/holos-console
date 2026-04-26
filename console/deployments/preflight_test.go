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

package deployments_test

import (
	"testing"

	"github.com/holos-run/holos-console/console/deployments"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ---------------------------------------------------------------------------
// Collision detection tests
// ---------------------------------------------------------------------------

// TestDetectCollisions_NoExisting verifies no collision is reported when the
// project namespace is empty.
func TestDetectCollisions_NoExisting(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: "v1",
		},
	}
	got := deployments.DetectCollisions(planned, map[string]bool{})
	if len(got) != 0 {
		t.Fatalf("expected no collisions, got %d", len(got))
	}
}

// TestDetectCollisions_UserNamedDeploymentMatchesSingleton verifies a
// collision is reported when the user's planned deployment's own name matches
// an existing singleton-pattern name (ending in "-shared").
//
// Example: user tries to name their deployment "waypoint-shared" but that
// name already exists as an auto-managed singleton Deployment.
func TestDetectCollisions_UserNamedDeploymentMatchesSingleton(t *testing.T) {
	existing := map[string]bool{
		"waypoint-shared": true, // existing singleton managed by the reconciler
	}
	planned := []*consolev1.PlannedDeployment{
		{
			// User tried to name their deployment "waypoint-shared" — this
			// collides with an existing auto-managed singleton.
			Name: "waypoint-shared",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "myapp",
			},
		},
	}
	got := deployments.DetectCollisions(planned, existing)
	if len(got) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(got))
	}
	if got[0].PlannedName != "waypoint-shared" {
		t.Errorf("PlannedName: got %q, want %q", got[0].PlannedName, "waypoint-shared")
	}
}

// TestDetectCollisions_NoDuplicateSingletons verifies that when a singleton
// already exists in the project namespace (materialised by a previous
// dependent), adding a second dependent that references the same template does
// NOT produce a collision.  The reconciler handles this idempotently by adding
// an ownerReference to the existing singleton.
func TestDetectCollisions_NoDuplicateSingletons(t *testing.T) {
	existing := map[string]bool{
		"waypoint-shared": true, // existing singleton (ends in "-shared")
	}
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api2",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint", // singleton would also be "waypoint-shared"
			},
		},
	}
	// "waypoint-shared" matches the computed singleton for this planned
	// deployment.  Since the planned name "api2" is not a singleton-pattern
	// name, the collision check should not report this — it is an idempotent
	// reconciler case, not a user-visible naming conflict.
	got := deployments.DetectCollisions(planned, existing)
	if len(got) != 0 {
		t.Fatalf("expected no collisions (singleton→singleton is idempotent), got %d", len(got))
	}
}

// TestDetectCollisions_PlannedNameMatchesUserDeployment verifies a collision
// is reported when the planned deployment's own non-singleton name matches an
// existing user-named Deployment.
func TestDetectCollisions_PlannedNameMatchesUserDeployment(t *testing.T) {
	existing := map[string]bool{
		"api": true, // user already has a deployment named "api"
	}
	planned := []*consolev1.PlannedDeployment{
		{
			// Create RPC would fail with AlreadyExists; PreflightCheck catches
			// it first.
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "myapp",
			},
		},
	}
	// The Create RPC would fail with AlreadyExists. PreflightCheck should not
	// specifically surface this case (it's caught by the Create RPC itself),
	// but we ensure the code doesn't panic or produce unexpected output.
	// CollisionDetail is about singleton-naming conflicts, not "name already
	// taken" errors — those are handled at write time.
	got := deployments.DetectCollisions(planned, existing)
	// "api" does not end with "-shared", so neither collision case applies.
	if len(got) != 0 {
		t.Fatalf("expected no collision for non-singleton name clash (handled at write time), got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Version conflict tests
// ---------------------------------------------------------------------------

// TestDetectVersionConflicts_SingleDependent verifies no conflict is reported
// when only one dependent pins a constraint.
func TestDetectVersionConflicts_SingleDependent(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=1.0.0",
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(got))
	}
}

// TestDetectVersionConflicts_CompatibleConstraints verifies no conflict is
// reported when two dependents pin overlapping semver ranges on the same
// shared dependency template.
func TestDetectVersionConflicts_CompatibleConstraints(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=1.0.0 <2.0.0",
		},
		{
			Name: "worker",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=1.5.0 <3.0.0",
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 0 {
		t.Fatalf("expected no conflicts for overlapping ranges, got %d", len(got))
	}
}

// TestDetectVersionConflicts_IncompatibleConstraints verifies a conflict is
// reported when two dependents pin non-overlapping semver ranges on the same
// shared dependency template (all four code paths: no overlap, no candidate
// version satisfies both constraints).
func TestDetectVersionConflicts_IncompatibleConstraints(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=1.0.0 <2.0.0",
		},
		{
			Name: "worker",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=3.0.0",
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(got))
	}
	if got[0].TemplateName != "waypoint" {
		t.Errorf("TemplateName: got %q, want %q", got[0].TemplateName, "waypoint")
	}
	if got[0].TemplateNamespace != "org-ns" {
		t.Errorf("TemplateNamespace: got %q, want %q", got[0].TemplateNamespace, "org-ns")
	}
	if len(got[0].DependentNames) != 2 {
		t.Errorf("DependentNames count: got %d, want 2", len(got[0].DependentNames))
	}
}

// TestDetectVersionConflicts_ExactPinConflict verifies a conflict is reported
// when two dependents pin different exact semver versions of the same template
// (incompatible exact pins).
func TestDetectVersionConflicts_ExactPinConflict(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "svc-a",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "database",
			},
			VersionConstraint: "=1.0.0",
		},
		{
			Name: "svc-b",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "database",
			},
			VersionConstraint: "=2.0.0",
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 1 {
		t.Fatalf("expected 1 conflict for exact-pin mismatch, got %d", len(got))
	}
}

// TestDetectVersionConflicts_NoConstraint verifies that deployments with no
// version constraint are not included in conflict detection (empty constraints
// cannot conflict with each other).
func TestDetectVersionConflicts_NoConstraint(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			// No version constraint.
		},
		{
			Name: "worker",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			// No version constraint.
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 0 {
		t.Fatalf("expected no conflicts when constraints are empty, got %d", len(got))
	}
}

// TestDetectVersionConflicts_DifferentTemplates verifies that incompatible
// constraints on different templates do not cross-pollinate into a conflict.
func TestDetectVersionConflicts_DifferentTemplates(t *testing.T) {
	planned := []*consolev1.PlannedDeployment{
		{
			Name: "api",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "waypoint",
			},
			VersionConstraint: ">=1.0.0 <2.0.0",
		},
		{
			Name: "worker",
			LinkedTemplateRef: &consolev1.LinkedTemplateRef{
				Namespace: "org-ns",
				Name:      "sidecar", // different template
			},
			VersionConstraint: ">=3.0.0",
		},
	}
	got := deployments.DetectVersionConflicts(planned)
	if len(got) != 0 {
		t.Fatalf("expected no conflicts for different templates, got %d", len(got))
	}
}

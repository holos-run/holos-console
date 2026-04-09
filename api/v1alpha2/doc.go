// Package v1alpha2 defines the versioned type system for the configuration
// management API introduced in ADR 020 and ADR 021.
//
// v1alpha2 replaces v1alpha1 outright — there are no compatibility shims and
// no dual-stack operation. All v1alpha1 service handlers, Go types, and CUE
// schema definitions are deleted when v1alpha2 is fully implemented (Phase 12
// of the v1alpha2 implementation plan, tracked by GitHub issue #622).
//
// # Key additions over v1alpha1
//
// The [Folder] type introduces optional intermediate grouping levels in the
// organization hierarchy. An Organization may contain up to three levels of
// Folders before reaching a Project. Each Folder is stored as a Kubernetes
// Namespace with a [AnnotationParent] label pointing to its immediate parent.
//
// [PlatformInput] gains a [FolderInfo] slice (the Folders field) that carries
// the folder ancestry chain from the organization root down to the immediate
// parent of the project. CUE templates can access this via platform.folders.
//
// The [Organization.Spec] remains empty. [ProjectSpec] gains a Parent field
// that is the immediate parent's slug (a folder name or the org name).
//
// # CUE schema
//
// These Go structs are the single source of truth for the template API
// contract. Each struct field carries json, yaml, and cue struct tags. CUE
// schemas are generated from these types via "cue get go", ensuring the CUE
// evaluation environment and the Go rendering pipeline always agree on the
// shape of inputs and outputs.
//
// The [APIVersion] constant is "console.holos.run/v1alpha2".
//
// Proto messages remain the RPC contract. The types in this package define what
// travels between the backend and the CUE evaluator.
package v1alpha2

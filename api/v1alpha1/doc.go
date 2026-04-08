// Package v1alpha1 defines the versioned type system for the configuration
// management API specified in ADR 014.
//
// These Go structs are the single source of truth for the template API
// contract. Each struct field carries json, yaml, and cue struct tags. CUE
// schemas are generated from these types via "cue get go", ensuring the CUE
// evaluation environment and the Go rendering pipeline always agree on the
// shape of inputs and outputs.
//
// The top-level type is [ResourceSet], which composes [PlatformInput],
// [ProjectInput], [PlatformResources], and [ProjectResources] into a single
// document that represents the complete set of Kubernetes resources produced by
// unifying templates from all hierarchy levels with their inputs.
//
// Proto messages remain the RPC contract. The types in this package define what
// travels between the backend and the CUE evaluator.
package v1alpha1

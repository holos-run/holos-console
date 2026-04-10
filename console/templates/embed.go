package templates

import _ "embed"

// DefaultTemplate is the built-in CUE deployment template.
// When a project has no templates, the UI can offer to create one from this default.
//
//go:embed default_template.cue
var DefaultTemplate string

// ExampleHttpbinTemplate is the example go-httpbin project-level deployment
// template. It produces ServiceAccount, Deployment, and Service resources.
// Pair with ExampleHttpbinPlatformTemplate (a platform template) to add an
// HTTPRoute and enforce the closed-struct kind constraint.
//
//go:embed example_httpbin.cue
var ExampleHttpbinTemplate string

// DefaultReferenceGrantTemplate is the built-in CUE platform template that
// produces a ReferenceGrant allowing HTTPRoute resources in the gateway
// namespace to reference Service resources in the project namespace.
//
//go:embed default_referencegrant.cue
var DefaultReferenceGrantTemplate string

// ExampleHttpbinPlatformTemplate is the example org-level platform template
// that provides an HTTPRoute in platformResources and closes
// projectResources.namespacedResources to Deployment, Service, and
// ServiceAccount (ADR 016 Decision 9). Pair with ExampleHttpbinTemplate for
// the project-level template that produces exactly those three kinds.
//
//go:embed example_httpbin_platform.cue
var ExampleHttpbinPlatformTemplate string

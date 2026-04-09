package org_templates

import _ "embed"

// DefaultReferenceGrantTemplate is the built-in CUE platform template
// (code: SystemTemplate) that produces a ReferenceGrant allowing HTTPRoute
// resources in the gateway namespace to reference Service resources in the
// project namespace.
//
//go:embed default_referencegrant.cue
var DefaultReferenceGrantTemplate string

// ExampleHttpbinPlatformTemplate is the example org-level platform template
// (code: SystemTemplate) that provides an HTTPRoute in platformResources and closes
// projectResources.namespacedResources to Deployment, Service, and
// ServiceAccount (ADR 016 Decision 9). Pair with ExampleHttpbinTemplate (in
// the templates package) for the project-level template that produces exactly
// those three kinds.
//
//go:embed example_httpbin_platform.cue
var ExampleHttpbinPlatformTemplate string

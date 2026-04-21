package templates

import _ "embed"

// DefaultTemplate is the built-in CUE deployment template.
// When a project has no templates, the UI can offer to create one from this default.
//
//go:embed default_template.cue
var DefaultTemplate string

// ExampleHttpbinTemplate is the example go-httpbin project-level deployment
// template. It produces ServiceAccount, Deployment, and Service resources.
// Used by SeedProjectTemplate to seed the org-creation populate_defaults flow.
//
//go:embed example_httpbin.cue
var ExampleHttpbinTemplate string

// DefaultReferenceGrantTemplate is the built-in CUE platform template that
// produces a ReferenceGrant allowing HTTPRoute resources in the gateway
// namespace to reference Service resources in the project namespace.
//
//go:embed default_referencegrant.cue
var DefaultReferenceGrantTemplate string

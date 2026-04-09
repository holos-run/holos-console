package templates

import _ "embed"

// DefaultTemplate is the built-in CUE deployment template.
// When a project has no templates, the UI can offer to create one from this default.
//
//go:embed default_template.cue
var DefaultTemplate string

// ExampleHttpbinTemplate is the example go-httpbin project-level deployment
// template. It produces ServiceAccount, Deployment, and Service resources.
// Pair with ExampleHttpbinPlatformTemplate (in the org_templates package)
// to add an HTTPRoute and enforce the closed-struct kind constraint.
//
//go:embed example_httpbin.cue
var ExampleHttpbinTemplate string

package templates

import _ "embed"

// DefaultTemplate is the built-in CUE deployment template.
// When a project has no templates, the UI can offer to create one from this default.
//
//go:embed default_template.cue
var DefaultTemplate string

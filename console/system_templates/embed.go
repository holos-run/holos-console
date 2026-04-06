package system_templates

import _ "embed"

// DefaultReferenceGrantTemplate is the built-in CUE system template that
// produces a ReferenceGrant allowing HTTPRoute resources in the gateway
// namespace to reference Service resources in the project namespace.
//
//go:embed default_referencegrant.cue
var DefaultReferenceGrantTemplate string

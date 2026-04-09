package v1alpha2

import _ "embed"

// GeneratedSchema is the CUE schema generated from the Go types in this
// package via "cue get go". It contains CUE definitions (#PlatformInput,
// #ProjectInput, #Claims, #FolderInfo, etc.) that the renderer prepends to
// template source before compilation.
//
//go:embed schema_gen.cue
var GeneratedSchema string
